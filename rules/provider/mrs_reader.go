package provider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

var MrsMagicBytes = [4]byte{'M', 'R', 'S', 1} // MRSv1

const (
	maxMRSDecodedBytes  = int64(64 << 20)
	maxMRSReservedBytes = int64(1 << 20)
	maxMRSRuleCount     = int64(1 << 24)
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

func rulesMrsParse(buf []byte, strategy ruleStrategy) (ruleStrategy, error) {
	if _strategy, ok := strategy.(mrsRuleStrategy); ok {
		decoder, err := zstd.NewReader(
			bytes.NewReader(buf),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxMemory(uint64(maxMRSDecodedBytes)),
			zstd.WithDecoderMaxWindow(uint64(maxMRSDecodedBytes)),
			zstd.WithDecodeBuffersBelow(0),
		)
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		reader := &boundedMRSReader{reader: decoder, remaining: maxMRSDecodedBytes}

		// header
		var header [4]byte
		_, err = io.ReadFull(reader, header[:])
		if err != nil {
			return nil, err
		}
		if header != MrsMagicBytes {
			return nil, fmt.Errorf("invalid MrsMagic bytes")
		}

		// behavior
		var _behavior [1]byte
		_, err = io.ReadFull(reader, _behavior[:])
		if err != nil {
			return nil, err
		}
		if _behavior[0] != strategy.Behavior().Byte() {
			return nil, fmt.Errorf("invalid behavior")
		}

		// count
		var count int64
		err = binary.Read(reader, binary.BigEndian, &count)
		if err != nil {
			return nil, err
		}

		if count < 0 || count > maxMRSRuleCount {
			return nil, fmt.Errorf("MRS rule count %d is outside 0..%d", count, maxMRSRuleCount)
		}

		// extra (reserved for future use). Skip it without allocating based on
		// untrusted input, and keep it within the decoded-document budget.
		var length int64
		err = binary.Read(reader, binary.BigEndian, &length)
		if err != nil {
			return nil, err
		}
		if length < 0 || length > maxMRSReservedBytes {
			return nil, fmt.Errorf("MRS reserved length %d is outside 0..%d", length, maxMRSReservedBytes)
		}
		if length > 0 {
			if _, err = io.CopyN(io.Discard, reader, length); err != nil {
				return nil, err
			}
		}

		if err = _strategy.FromMrs(reader, int(count)); err != nil {
			return nil, err
		}
		var trailing [1]byte
		if _, err = io.ReadFull(reader, trailing[:]); err == nil {
			return nil, errors.New("MRS contains trailing data")
		} else if !errors.Is(err, io.EOF) {
			return nil, err
		}
		return strategy, nil
	} else {
		return nil, ErrInvalidFormat
	}
}
