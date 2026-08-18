package net

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/metacubex/sing/common/network"
)

type countedWriter struct {
	buffer *bytes.Buffer
	count  *int64
}

func (w *countedWriter) Write(payload []byte) (int, error) {
	return w.buffer.Write(payload)
}

func (w *countedWriter) UnwrapWriter() (io.Writer, []network.CountFunc) {
	return w.buffer, []network.CountFunc{func(n int64) { *w.count += n }}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

type writeError struct {
	written int
	err     error
}

func (w writeError) Write([]byte) (int, error) {
	return w.written, w.err
}

type countedWriteError struct {
	writer writeError
	count  *int64
}

func (w *countedWriteError) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *countedWriteError) UnwrapWriter() (io.Writer, []network.CountFunc) {
	return w.writer, []network.CountFunc{func(n int64) { *w.count += n }}
}

func TestCopyConnPlainStreamsAndCounters(t *testing.T) {
	payload := bytes.Repeat([]byte("aster"), 10_000)
	var destination bytes.Buffer
	var counted int64
	n, err := copyConn(&countedWriter{buffer: &destination, count: &counted}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || counted != n {
		t.Fatalf("copied=%d counted=%d want=%d", n, counted, len(payload))
	}
	if !bytes.Equal(destination.Bytes(), payload) {
		t.Fatal("copied payload differs")
	}
}

func TestCopyConnDetectsShortWrite(t *testing.T) {
	_, err := copyConn(shortWriter{}, bytes.NewReader([]byte("payload")))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

func TestCopyConnReportsBytesWrittenWithWriteError(t *testing.T) {
	payload := bytes.Repeat([]byte("aster"), 10_000)
	writeErr := errors.New("write failed after progress")

	for _, test := range []struct {
		name    string
		written int
	}{
		{name: "full write", written: len(payload)},
		{name: "partial write", written: len(payload) / 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var counted int64
			n, err := copyConn(&countedWriteError{
				writer: writeError{written: test.written, err: writeErr},
				count:  &counted,
			}, bytes.NewReader(payload))
			if !errors.Is(err, writeErr) {
				t.Fatalf("error = %v, want %v", err, writeErr)
			}
			if n != int64(test.written) || counted != n {
				t.Fatalf("copied=%d counted=%d want=%d", n, counted, test.written)
			}
		})
	}
}
