package trusttunnel

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type closeCountingWriter struct {
	closed atomic.Int32
}

func (*closeCountingWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *closeCountingWriter) Close() error {
	w.closed.Add(1)
	return nil
}

type closeCountingReader struct {
	closed atomic.Int32
}

func (*closeCountingReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *closeCountingReader) Close() error {
	r.closed.Add(1)
	return nil
}

func TestHTTPConnCloseIsIdempotentAndStopsDeadline(t *testing.T) {
	writer := new(closeCountingWriter)
	reader := new(closeCountingReader)
	var cancelCalls atomic.Int32
	var closeCalls atomic.Int32
	conn := &httpConn{
		writer:   writer,
		created:  make(chan struct{}),
		cancelFn: func() { cancelCalls.Add(1) },
		closeFn:  func() { closeCalls.Add(1) },
	}
	conn.setup(reader, nil)
	require.NoError(t, conn.SetDeadline(time.Now().Add(time.Hour)))
	require.NoError(t, conn.Close())
	require.NoError(t, conn.Close())

	require.EqualValues(t, 1, writer.closed.Load())
	require.EqualValues(t, 1, reader.closed.Load())
	require.EqualValues(t, 1, cancelCalls.Load())
	require.EqualValues(t, 1, closeCalls.Load())
	require.ErrorIs(t, conn.SetDeadline(time.Now()), net.ErrClosed)
}
