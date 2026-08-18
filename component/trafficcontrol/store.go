package trafficcontrol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/metacubex/bbolt"
)

const (
	storeSchemaVersion = uint64(1)
	compressedMagic    = "TCZ1"
	rawMagic           = "TCR1"
	compressionFloor   = 256
	maxDecodedBlob     = 32 << 20
)

var (
	metaBucket     = []byte("meta")
	policiesBucket = []byte("policies")
	reportsBucket  = []byte("reports")
	schemaKey      = []byte("schema")
	checkpointKey  = []byte("checkpoint")
	crcTable       = crc32.MakeTable(crc32.Castagnoli)
	ErrStoreLimit  = errors.New("traffic-control store size limit exceeded")
)

type Store struct {
	mu           sync.Mutex
	path         string
	maxSize      int64
	db           *bbolt.DB
	encoder      *zstd.Encoder
	decoder      *zstd.Decoder
	uncompressed atomic.Int64
	saveCount    uint64
}

type persistedPolicy struct {
	ID          string             `json:"id"`
	Identity    string             `json:"identity"`
	Generation  uint64             `json:"generation"`
	Counters    Counters           `json:"counters"`
	Buckets     map[int64]Counters `json:"buckets,omitempty"`
	OverQuota   bool               `json:"over_quota"`
	LastUpdated int64              `json:"last_updated"`
	LastReset   int64              `json:"last_reset"`
	LastSeen    int64              `json:"last_seen"`
}

type persistedReport struct {
	Key     string             `json:"key"`
	Updated int64              `json:"updated"`
	Hourly  map[int64]Counters `json:"hourly,omitempty"`
	Daily   map[int64]Counters `json:"daily,omitempty"`
	Monthly map[int64]Counters `json:"monthly,omitempty"`
	Rolled  map[int64]bool     `json:"rolled,omitempty"`
}

func OpenStore(path string, maxSize int64) (*Store, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	directoryInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return nil, errors.New("traffic-control store parent must be a regular directory")
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("traffic-control store must be a regular file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxDecodedBlob))
	if err != nil {
		encoder.Close()
		return nil, err
	}
	db, err := openBoltWithRecovery(path)
	if err != nil {
		encoder.Close()
		decoder.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		encoder.Close()
		decoder.Close()
		return nil, err
	}
	store := &Store{path: path, maxSize: maxSize, db: db, encoder: encoder, decoder: decoder}
	if err := store.initialize(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return err
		}
		if _, err = tx.CreateBucketIfNotExists(policiesBucket); err != nil {
			return err
		}
		if _, err = tx.CreateBucketIfNotExists(reportsBucket); err != nil {
			return err
		}
		schema := meta.Get(schemaKey)
		if schema == nil {
			return meta.Put(schemaKey, uint64Bytes(storeSchemaVersion))
		}
		if len(schema) != 8 || binary.BigEndian.Uint64(schema) != storeSchemaVersion {
			return fmt.Errorf("unsupported traffic-control store schema")
		}
		return nil
	})
}

func (s *Store) Load() (map[string]*policyState, map[string]*reportSeries, int64, error) {
	states := make(map[string]*policyState)
	reports := make(map[string]*reportSeries)
	var checkpoint int64
	err := s.db.View(func(tx *bbolt.Tx) error {
		meta := tx.Bucket(metaBucket)
		if value := meta.Get(checkpointKey); len(value) == 8 {
			checkpoint = int64(binary.BigEndian.Uint64(value))
		}
		if err := tx.Bucket(policiesBucket).ForEach(func(key, value []byte) error {
			var persisted persistedPolicy
			if err := s.decodeJSON(value, &persisted); err != nil {
				return fmt.Errorf("decode policy %q: %w", key, err)
			}
			if persisted.Buckets == nil {
				persisted.Buckets = make(map[int64]Counters)
			}
			state := &policyState{ID: persisted.ID, Identity: persisted.Identity, Generation: persisted.Generation, Counters: persisted.Counters, Buckets: persisted.Buckets, LastUpdated: persisted.LastUpdated, LastReset: persisted.LastReset, LastSeen: persisted.LastSeen}
			state.OverQuota.Store(persisted.OverQuota)
			states[persisted.ID] = state
			return nil
		}); err != nil {
			return err
		}
		return tx.Bucket(reportsBucket).ForEach(func(key, value []byte) error {
			var persisted persistedReport
			if err := s.decodeJSON(value, &persisted); err != nil {
				return fmt.Errorf("decode report %q: %w", key, err)
			}
			if persisted.Hourly == nil {
				persisted.Hourly = make(map[int64]Counters)
			}
			if persisted.Daily == nil {
				persisted.Daily = make(map[int64]Counters)
			}
			if persisted.Monthly == nil {
				persisted.Monthly = make(map[int64]Counters)
			}
			if persisted.Rolled == nil {
				persisted.Rolled = make(map[int64]bool)
			}
			reports[persisted.Key] = &reportSeries{Key: persisted.Key, Updated: persisted.Updated, Hourly: persisted.Hourly, Daily: persisted.Daily, Monthly: persisted.Monthly, Rolled: persisted.Rolled}
			return nil
		})
	})
	return states, reports, checkpoint, err
}

func (s *Store) Save(states map[string]*policyState, reports map[string]*reportSeries, checkpoint int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var uncompressed int64
	err := s.db.Update(func(tx *bbolt.Tx) error {
		if err := replaceBucket(tx, policiesBucket, func(bucket *bbolt.Bucket) error {
			for id, state := range states {
				state.mu.Lock()
				persisted := persistedPolicy{ID: state.ID, Identity: state.Identity, Generation: state.Generation, Counters: state.Counters, Buckets: cloneCountersMap(state.Buckets), OverQuota: state.OverQuota.Load(), LastUpdated: state.LastUpdated, LastReset: state.LastReset, LastSeen: state.LastSeen}
				state.mu.Unlock()
				encoded, rawSize, err := s.encodeJSON(persisted)
				if err != nil {
					return err
				}
				uncompressed += int64(rawSize)
				if err := bucket.Put([]byte(id), encoded); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if err := replaceBucket(tx, reportsBucket, func(bucket *bbolt.Bucket) error {
			for key, series := range reports {
				series.mu.Lock()
				persisted := persistedReport{Key: series.Key, Updated: series.Updated, Hourly: cloneCountersMap(series.Hourly), Daily: cloneCountersMap(series.Daily), Monthly: cloneCountersMap(series.Monthly), Rolled: cloneBoolMap(series.Rolled)}
				series.mu.Unlock()
				encoded, rawSize, err := s.encodeJSON(persisted)
				if err != nil {
					return err
				}
				uncompressed += int64(rawSize)
				if err := bucket.Put([]byte(key), encoded); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		return tx.Bucket(metaBucket).Put(checkpointKey, uint64Bytes(uint64(checkpoint)))
	})
	if err != nil {
		return err
	}
	s.uncompressed.Store(uncompressed)
	s.saveCount++
	if s.saveCount == 1 || s.saveCount%12 == 0 {
		if err := s.backupLocked(s.path + ".bak"); err != nil {
			return fmt.Errorf("backup traffic-control store: %w", err)
		}
	}
	if size := s.sizeLocked(); s.maxSize > 0 && size > s.maxSize {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrStoreLimit, size, s.maxSize)
	}
	return nil
}

func replaceBucket(tx *bbolt.Tx, name []byte, fill func(*bbolt.Bucket) error) error {
	if err := tx.DeleteBucket(name); err != nil && !errors.Is(err, bbolt.ErrBucketNotFound) {
		return err
	}
	bucket, err := tx.CreateBucket(name)
	if err != nil {
		return err
	}
	return fill(bucket)
}

func (s *Store) encodeJSON(value any) ([]byte, int, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, 0, err
	}
	header := make([]byte, 12)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(raw)))
	binary.BigEndian.PutUint32(header[8:12], crc32.Checksum(raw, crcTable))
	if len(raw) < compressionFloor {
		copy(header[:4], rawMagic)
		return append(header, raw...), len(raw), nil
	}
	copy(header[:4], compressedMagic)
	return append(header, s.encoder.EncodeAll(raw, nil)...), len(raw), nil
}

func (s *Store) decodeJSON(value []byte, target any) error {
	if len(value) < 12 {
		return io.ErrUnexpectedEOF
	}
	length := binary.BigEndian.Uint32(value[4:8])
	if length > maxDecodedBlob {
		return errors.New("traffic-control blob exceeds decode limit")
	}
	var raw []byte
	var err error
	switch string(value[:4]) {
	case rawMagic:
		raw = append([]byte(nil), value[12:]...)
	case compressedMagic:
		raw, err = s.decoder.DecodeAll(value[12:], make([]byte, 0, int(length)))
	default:
		return errors.New("unknown traffic-control blob codec")
	}
	if err != nil {
		return err
	}
	if len(raw) != int(length) {
		return errors.New("traffic-control blob length mismatch")
	}
	if crc32.Checksum(raw, crcTable) != binary.BigEndian.Uint32(value[8:12]) {
		return errors.New("traffic-control blob checksum mismatch")
	}
	return json.Unmarshal(raw, target)
}

func (s *Store) Backup(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backupLocked(path)
}

func (s *Store) backupLocked(path string) error {
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	err = s.db.View(func(tx *bbolt.Tx) error { _, copyErr := tx.WriteTo(file); return copyErr })
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	previous := path + ".old"
	_ = os.Remove(previous)
	if renameErr := os.Rename(path, previous); renameErr != nil && !errors.Is(renameErr, os.ErrNotExist) {
		_ = os.Remove(tmp)
		return renameErr
	}
	if renameErr := os.Rename(tmp, path); renameErr != nil {
		_ = os.Rename(previous, path)
		return renameErr
	}
	_ = os.Remove(previous)
	return nil
}

func (s *Store) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp := s.path + ".compact"
	_ = os.Remove(tmp)
	destination, err := bbolt.Open(tmp, 0o600, &bbolt.Options{Timeout: time.Second, NoStatistics: true})
	if err != nil {
		return err
	}
	err = bbolt.Compact(destination, s.db, 4<<20)
	closeDestinationErr := destination.Close()
	if err == nil {
		err = closeDestinationErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = s.db.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.db = nil
	previous := s.path + ".precompact"
	_ = os.Remove(previous)
	if err = os.Rename(s.path, previous); err != nil {
		reopenErr := s.reopenLocked()
		return errors.Join(err, reopenErr)
	}
	if err = os.Rename(tmp, s.path); err != nil {
		restoreErr := os.Rename(previous, s.path)
		reopenErr := s.reopenLocked()
		return errors.Join(err, restoreErr, reopenErr)
	}
	if err = s.reopenLocked(); err != nil {
		failed := s.path + ".failedcompact"
		_ = os.Remove(failed)
		moveFailedErr := os.Rename(s.path, failed)
		restoreErr := os.Rename(previous, s.path)
		reopenErr := s.reopenLocked()
		return errors.Join(err, moveFailedErr, restoreErr, reopenErr)
	}
	_ = os.Remove(previous)
	return nil
}

func (s *Store) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sizeLocked()
}

func (s *Store) sizeLocked() int64 {
	info, err := os.Stat(s.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (s *Store) reopenLocked() error {
	db, err := bbolt.Open(s.path, 0o600, &bbolt.Options{Timeout: time.Second, NoStatistics: true})
	if err == nil {
		s.db = db
	}
	return err
}

func (s *Store) UncompressedBytes() int64 { return s.uncompressed.Load() }

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.db != nil {
		err = s.db.Close()
		s.db = nil
	}
	if s.encoder != nil {
		s.encoder.Close()
	}
	if s.decoder != nil {
		s.decoder.Close()
	}
	return err
}

func cloneCountersMap(source map[int64]Counters) map[int64]Counters {
	result := make(map[int64]Counters, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneBoolMap(source map[int64]bool) map[int64]bool {
	result := make(map[int64]bool, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func uint64Bytes(value uint64) []byte {
	result := make([]byte, 8)
	binary.BigEndian.PutUint64(result, value)
	return result
}

func compactBolt(source *bbolt.DB, destination string) error {
	var buffer bytes.Buffer
	if err := source.View(func(tx *bbolt.Tx) error { _, err := tx.WriteTo(&buffer); return err }); err != nil {
		return err
	}
	return os.WriteFile(destination, buffer.Bytes(), 0o600)
}

func openBoltWithRecovery(path string) (*bbolt.DB, error) {
	options := &bbolt.Options{Timeout: time.Second, NoStatistics: true}
	db, err := bbolt.Open(path, 0o600, options)
	if err == nil {
		return db, nil
	}
	if !errors.Is(err, bbolt.ErrInvalid) && !errors.Is(err, bbolt.ErrChecksum) && !errors.Is(err, bbolt.ErrVersionMismatch) {
		return nil, err
	}
	backup := path + ".bak"
	if _, statErr := os.Stat(backup); statErr != nil {
		return nil, err
	}
	corrupt := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
	if renameErr := os.Rename(path, corrupt); renameErr != nil && !errors.Is(renameErr, os.ErrNotExist) {
		return nil, errors.Join(err, renameErr)
	}
	if copyErr := copyStoreFile(backup, path); copyErr != nil {
		return nil, errors.Join(err, copyErr)
	}
	return bbolt.Open(path, 0o600, options)
}

func copyStoreFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	tmp := destination + ".recover"
	_ = os.Remove(tmp)
	output, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	if err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destination)
}
