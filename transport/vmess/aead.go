package vmess

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"github.com/Miku0139oao/aster-core/common/pool"
)

type aeadWriter struct {
	io.Writer
	cipher.AEAD
	nonce [32]byte
	count uint16

	writeLock sync.Mutex
}

func newAEADWriter(w io.Writer, aead cipher.AEAD, iv []byte) *aeadWriter {
	aw := &aeadWriter{Writer: w, AEAD: aead}
	if len(iv) >= 12 {
		copy(aw.nonce[2:], iv[2:12])
	}
	return aw
}

func (w *aeadWriter) Write(b []byte) (n int, err error) {
	w.writeLock.Lock()
	defer w.writeLock.Unlock()
	overhead := w.Overhead()
	nonce := w.nonce[:w.NonceSize()]
	maxChunk := chunkSize - overhead
	buf := pool.Get(lenSize + chunkSize)
	defer pool.Put(buf)
	length := len(b)
	for {
		if length == 0 {
			break
		}
		readLen := maxChunk
		if length < readLen {
			readLen = length
		}

		binary.BigEndian.PutUint16(buf[:lenSize], uint16(readLen+overhead))
		binary.BigEndian.PutUint16(w.nonce[:2], w.count)
		w.Seal(buf[lenSize:lenSize], nonce, b[n:n+readLen], nil)
		w.count++

		_, err = w.Writer.Write(buf[:lenSize+readLen+overhead])
		if err != nil {
			break
		}
		n += readLen
		length -= readLen
	}
	return
}

type aeadReader struct {
	io.Reader
	cipher.AEAD
	nonce   [32]byte
	buf     []byte
	offset  int
	sizeBuf [lenSize]byte
	count   uint16
}

func newAEADReader(r io.Reader, aead cipher.AEAD, iv []byte) *aeadReader {
	ar := &aeadReader{Reader: r, AEAD: aead}
	if len(iv) >= 12 {
		copy(ar.nonce[2:], iv[2:12])
	}
	return ar
}

func (r *aeadReader) Read(b []byte) (int, error) {
	if r.buf != nil {
		n := copy(b, r.buf[r.offset:])
		r.offset += n
		if r.offset == len(r.buf) {
			pool.Put(r.buf)
			r.buf = nil
		}
		return n, nil
	}

	_, err := io.ReadFull(r.Reader, r.sizeBuf[:])
	if err != nil {
		return 0, err
	}

	size := int(binary.BigEndian.Uint16(r.sizeBuf[:]))
	overhead := r.Overhead()
	// Fast-path decrypts into b and returns size-overhead; a too-small chunk
	// would yield a negative n with a nil error (io.Reader contract break).
	if size > maxSize || size < overhead {
		return 0, errors.New("buffer is larger than standard")
	}

	binary.BigEndian.PutUint16(r.nonce[:2], r.count)
	nonce := r.nonce[:r.NonceSize()]

	if len(b) >= size {
		if _, err := io.ReadFull(r.Reader, b[:size]); err != nil {
			return 0, err
		}
		if _, err := r.Open(b[:0], nonce, b[:size], nil); err != nil {
			r.count++
			return 0, err
		}
		r.count++
		return size - overhead, nil
	}

	buf := pool.Get(size)
	_, err = io.ReadFull(r.Reader, buf[:size])
	if err != nil {
		pool.Put(buf)
		return 0, err
	}

	_, err = r.Open(buf[:0], nonce, buf[:size], nil)
	r.count++
	if err != nil {
		pool.Put(buf)
		return 0, err
	}
	realLen := size - overhead
	n := copy(b, buf[:realLen])
	if n == realLen {
		pool.Put(buf)
		return n, nil
	}

	r.offset = n
	r.buf = buf[:realLen]
	return n, nil
}
