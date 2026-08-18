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
