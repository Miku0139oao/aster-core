package memconservative

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

var (
	errFailedToReadBytes            = errors.New("failed to read bytes")
	errFailedToReadExpectedLenBytes = errors.New("failed to read expected length of bytes")
	errInvalidGeodataFile           = errors.New("invalid geodata file")
	errInvalidGeodataVarintLength   = errors.New("invalid geodata varint length")
	errCodeNotFound                 = errors.New("code not found")
)

func emitBytes(f io.ReadSeeker, code string) ([]byte, error) {
	fileSize, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, errFailedToReadBytes
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, errFailedToReadBytes
	}
	// Buffer the file reads so that the many small reads performed by the
	// varint/name state machine below do not each turn into a syscall.
	reader := bufio.NewReaderSize(f, 16*1024)
	readError := func(err error) error {
		switch err {
		case io.EOF:
			return errCodeNotFound
		case io.ErrUnexpectedEOF:
			return errFailedToReadExpectedLenBytes
		default:
			return errFailedToReadBytes
		}
	}
	var lengthBytes []byte
	readLength := func() (uint64, error) {
		lengthBytes = lengthBytes[:0]
		for {
			b, err := reader.ReadByte()
			if err != nil {
				return 0, readError(err)
			}
			lengthBytes = append(lengthBytes, b)
			if b < 128 { // max one-byte-length byte `7F`(0FFF FFFF) equals to `127` in decimal
				n, size := protowire.ConsumeVarint(lengthBytes)
				if size < 0 {
					return 0, errInvalidGeodataVarintLength
				}
				return n, nil
			}
		}
	}
	for {
		// data type ((field_number << 3) | wire_type), byte `0A` equals to `10` in decimal
		b, err := reader.ReadByte()
		if err != nil {
			return nil, readError(err)
		}
		if b != 10 {
			return nil, errInvalidGeodataFile
		}
		length, err := readLength()
		if err != nil {
			return nil, err
		}
		// data type of the inner message (GeoIP country_code / GeoSite country_code)
		b, err = reader.ReadByte()
		if err != nil {
			return nil, readError(err)
		}
		if b != 10 {
			return nil, errInvalidGeodataFile
		}
		codeLength, err := readLength()
		if err != nil {
			return nil, err
		}
		// The length is attacker-controlled varint data. Bound both the name and
		// the record body by the actual file size before allocating, so a
		// crafted header cannot request a multi-gigabyte allocation and abort
		// the process. A real record can never exceed the file holding it.
		if codeLength > uint64(fileSize) || length > uint64(fileSize) {
			return nil, errInvalidGeodataFile
		}
		name := make([]byte, codeLength)
		if _, err := io.ReadFull(reader, name); err != nil {
			return nil, readError(err)
		}
		// The bytes already consumed from the message body: inner tag + code length varint + name.
		prefixLength := 1 + uint64(len(lengthBytes)) + codeLength
		if prefixLength > length {
			return nil, errInvalidGeodataFile
		}
		if strings.EqualFold(string(name), code) {
			// Reconstruct the matched GeoIP or GeoSite message body, wrapping the
			// already buffered prefix instead of seeking backward.
			result := make([]byte, length)
			n := copy(result, []byte{10})
			n += copy(result[n:], lengthBytes)
			n += copy(result[n:], name)
			if _, err := io.ReadFull(reader, result[n:]); err != nil {
				if err == io.EOF {
					return nil, errFailedToReadExpectedLenBytes
				}
				return nil, readError(err)
			}
			return result, nil
		}
		// Skip the unmatched GeoIP or GeoSite varint, reusing bytes that are
		// already prefetched in the buffer when possible.
		offset := int64(length) - int64(prefixLength)
		if offset <= int64(reader.Buffered()) {
			_, err = reader.Discard(int(offset))
		} else {
			_, err = f.Seek(offset-int64(reader.Buffered()), io.SeekCurrent)
			reader.Reset(f)
		}
		if err != nil {
			return nil, errFailedToReadBytes
		}
	}
}

func Decode(filename, code string) ([]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %s, base error: %s", filename, err.Error())
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	geoBytes, err := emitBytes(f, code)
	if err != nil {
		return nil, err
	}
	return geoBytes, nil
}
