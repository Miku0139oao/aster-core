package aster

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Miku0139oao/aster-core/log"
)

const (
	storeVersion = 1
	maxStoreSize = 16 << 20
)

var errUnsupportedStoreVersion = errors.New("unsupported Aster state version")

type Store struct {
	Version       int                       `json:"version"`
	Generation    uint64                    `json:"generation"`
	Listeners     map[string]*ListenerState `json:"listeners"`
	Subscriptions map[string]string         `json:"subscriptions,omitempty"`
}

type ListenerState struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Protocol        string  `json:"protocol"`
	Users           []*User `json:"users"`
	Revision        int64   `json:"revision"`
	AppliedRevision int64   `json:"applied_revision"`
}

type User struct {
	ID                string `json:"id"`
	Inbound           string `json:"inbound"`
	Protocol          string `json:"protocol"`
	Name              string `json:"name"`
	UUID              string `json:"uuid,omitempty"`
	Password          string `json:"password,omitempty"`
	Flow              string `json:"flow,omitempty"`
	Enabled           bool   `json:"enabled"`
	UploadBytes       int64  `json:"upload_bytes"`
	DownloadBytes     int64  `json:"download_bytes"`
	TrafficGeneration uint64 `json:"traffic_generation"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

func newStore() *Store {
	return &Store{
		Version:       storeVersion,
		Listeners:     make(map[string]*ListenerState),
		Subscriptions: make(map[string]string),
	}
}

func loadStore(path string) (*Store, bool, error) {
	primary, primaryErr := readValidatedStore(path)
	backup, backupErr := readValidatedStore(path + ".bak")
	if errors.Is(primaryErr, errUnsupportedStoreVersion) || errors.Is(backupErr, errUnsupportedStoreVersion) {
		return nil, false, fmt.Errorf("load Aster state: %w", errors.Join(primaryErr, backupErr))
	}
	if primaryErr == nil && backupErr == nil {
		if backup.Generation > primary.Generation {
			return backup, true, nil
		}
		return primary, false, nil
	}
	if primaryErr == nil {
		return primary, false, nil
	}
	if backupErr == nil {
		return backup, true, nil
	}
	if errors.Is(primaryErr, os.ErrNotExist) && errors.Is(backupErr, os.ErrNotExist) {
		return newStore(), false, nil
	}
	return nil, false, fmt.Errorf("load Aster state: %w", errors.Join(primaryErr, backupErr))
}

func readValidatedStore(path string) (*Store, error) {
	store, err := readStore(path)
	if err != nil {
		return nil, err
	}
	if err := validateStore(store); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}
	return store, nil
}

func readStore(path string) (*Store, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("Aster state is not a regular file: %s", path)
	}
	if err := validateStoreFileSecurity(path, pathInfo); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(pathInfo, info) {
		return nil, fmt.Errorf("Aster state changed while opening: %s", path)
	}
	if info.Size() > maxStoreSize {
		return nil, fmt.Errorf("Aster state exceeds %d bytes", maxStoreSize)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStoreSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStoreSize {
		return nil, fmt.Errorf("Aster state exceeds %d bytes", maxStoreSize)
	}
	store := newStore()
	if err := json.Unmarshal(data, store); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if store.Version != storeVersion {
		return nil, fmt.Errorf("%w %d", errUnsupportedStoreVersion, store.Version)
	}
	if store.Listeners == nil {
		store.Listeners = make(map[string]*ListenerState)
	}
	if store.Subscriptions == nil {
		store.Subscriptions = make(map[string]string)
	}
	return store, nil
}

func saveStore(path string, store *Store) error {
	if err := prepareStoreDirectory(path); err != nil {
		return err
	}
	unlock, err := lockStore(path)
	if err != nil {
		return err
	}
	defer unlock()
	return saveStoreLocked(path, store)
}

func saveStoreWithRecovery(path string, store *Store, _ bool) error {
	return saveStore(path, store)
}

func prepareStoreDirectory(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("Aster store parent is not a directory: %s", dir)
	}
	return validateStoreDirectorySecurity(dir, info)
}

func saveStoreLocked(path string, store *Store) error {
	committed, recovered, err := loadStore(path)
	if err != nil {
		return err
	}
	if committed.Generation != store.Generation {
		return fmt.Errorf("%w: Aster store generation changed from %d to %d", ErrConflict, store.Generation, committed.Generation)
	}

	if store.Generation == math.MaxUint64 {
		return fmt.Errorf("%w: Aster store generation exhausted", ErrConflict)
	}
	candidate := cloneStore(store)
	candidate.Generation++
	if err := validateStore(candidate); err != nil {
		return err
	}
	data, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxStoreSize {
		return fmt.Errorf("Aster state exceeds %d bytes", maxStoreSize)
	}

	firstPath, secondPath := path+".bak", path
	if recovered {
		firstPath, secondPath = path, path+".bak"
	}
	if err := writeStoreFile(firstPath, data); err != nil {
		return err
	}
	store.Generation = candidate.Generation
	if err := writeStoreFile(secondPath, data); err != nil {
		log.Warnln("Aster redundant state update failed; latest state remains in %s: %s", firstPath, err)
	}
	return nil
}

func writeStoreFile(path string, data []byte) (err error) {
	tmpPath := path + ".tmp"
	dir := filepath.Dir(path)
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err = os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err = secureStoreFile(tmpPath); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = replaceStoreFile(tmpPath, path); err != nil {
		return err
	}
	if err = syncDirectory(dir); err != nil {
		log.Warnln("Aster state directory sync failed after committing %s: %s", path, err)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil && runtime.GOOS != "windows" {
		return err
	}
	return nil
}

func cloneStore(store *Store) *Store {
	cloned := newStore()
	cloned.Version = store.Version
	cloned.Generation = store.Generation
	for name, listener := range store.Listeners {
		clonedListener := *listener
		clonedListener.Users = make([]*User, len(listener.Users))
		for i, user := range listener.Users {
			clonedUser := *user
			clonedListener.Users[i] = &clonedUser
		}
		cloned.Listeners[name] = &clonedListener
	}
	for name, token := range store.Subscriptions {
		cloned.Subscriptions[name] = token
	}
	return cloned
}
