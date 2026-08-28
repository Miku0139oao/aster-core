package provider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var MrsMagicBytes = [4]byte{'M', 'R', 'S', 1} // MRSv1

const (
	maxMRSDecodedBytes  = int64(64 << 20)
	maxMRSReservedBytes = int64(1 << 20)
	maxMRSRuleCount     = int64(1 << 24)
	mrsHeaderSize       = 4 + 1 + 8 + 8
)

var errMRSDecodedTooLarge = errors.New("decoded MRS exceeds size limit")

type boundedMRSReader struct {
	reader    io.Reader
	remaining int64
}

func (r *boundedMRSReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n > 0 {
			return 0, errMRSDecodedTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

var (
	mrsDecoderOnce sync.Once
	mrsDecoder     *zstd.Decoder
	mrsDecoderErr  error
	mrsDecodedPool = sync.Pool{New: func() any {
		b := make([]byte, 0, 64<<10)
		return &b
	}}
)

func getMRSDecoder() (*zstd.Decoder, error) {
	mrsDecoderOnce.Do(func() {
		mrsDecoder, mrsDecoderErr = zstd.NewReader(nil,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxMemory(uint64(maxMRSDecodedBytes)),
			zstd.WithDecoderMaxWindow(uint64(maxMRSDecodedBytes)),
		)
	})
	return mrsDecoder, mrsDecoderErr
}

func recycleMRSDecoded(slot *[]byte, decoded []byte) {
	if slot == nil {
		return
	}
	buf := decoded
	if cap(buf) == 0 || cap(buf) > int(maxMRSDecodedBytes) {
		buf = *slot
	}
	if cap(buf) == 0 || cap(buf) > int(maxMRSDecodedBytes) {
		buf = make([]byte, 0, 64<<10)
	}
	*slot = buf[:0]
	mrsDecodedPool.Put(slot)
}

func rulesMrsParse(buf []byte, strategy ruleStrategy) (ruleStrategy, error) {
	_strategy, ok := strategy.(mrsRuleStrategy)
	if !ok {
		return nil, ErrInvalidFormat
	}
	decoder, err := getMRSDecoder()
	if err != nil {
		return nil, err
	}
	slot := mrsDecodedPool.Get().(*[]byte)
	decoded, err := decoder.DecodeAll(buf, (*slot)[:0])
	if err != nil {
		recycleMRSDecoded(slot, decoded)
		if errors.Is(err, zstd.ErrDecoderSizeExceeded) {
			return nil, errMRSDecodedTooLarge
		}
		return nil, err
	}
	defer recycleMRSDecoded(slot, decoded)
	return parseMRSDecoded(decoded, _strategy)
}

func parseMRSDecoded(decoded []byte, strategy mrsRuleStrategy) (ruleStrategy, error) {
	if len(decoded) < mrsHeaderSize {
		return nil, io.ErrUnexpectedEOF
	}
	if !bytes.Equal(decoded[:4], MrsMagicBytes[:]) {
		return nil, fmt.Errorf("invalid MrsMagic bytes")
	}
	if decoded[4] != strategy.Behavior().Byte() {
		return nil, fmt.Errorf("invalid behavior")
	}
	count := int64(binary.BigEndian.Uint64(decoded[5:13]))
	if count < 0 || count > maxMRSRuleCount {
		return nil, fmt.Errorf("MRS rule count %d is outside 0..%d", count, maxMRSRuleCount)
	}
	length := int64(binary.BigEndian.Uint64(decoded[13:21]))
	if length < 0 || length > maxMRSReservedBytes {
		return nil, fmt.Errorf("MRS reserved length %d is outside 0..%d", length, maxMRSReservedBytes)
	}
	body := decoded[mrsHeaderSize:]
	if length > 0 {
		if int64(len(body)) < length {
			return nil, io.ErrUnexpectedEOF
		}
		body = body[length:]
	}
	reader := bytes.NewReader(body)
	if err := strategy.FromMrs(reader, int(count)); err != nil {
		return nil, err
	}
	if reader.Len() != 0 {
		return nil, errors.New("MRS contains trailing data")
	}
	return strategy, nil
}
