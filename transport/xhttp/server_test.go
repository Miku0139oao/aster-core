package xhttp

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deadlineResponseWriter struct {
	header        http.Header
	writeStarted  chan struct{}
	writeReturned chan struct{}
	writeUnblock  chan struct{}
	deadlineSet   chan struct{}
	writeOnce     sync.Once
	deadlineOnce  sync.Once
}

func newDeadlineResponseWriter() *deadlineResponseWriter {
	return &deadlineResponseWriter{
		header:        make(http.Header),
		writeStarted:  make(chan struct{}),
		writeReturned: make(chan struct{}),
		writeUnblock:  make(chan struct{}),
		deadlineSet:   make(chan struct{}),
	}
}

func (w *deadlineResponseWriter) Header() http.Header { return w.header }

func (w *deadlineResponseWriter) WriteHeader(int) {}

func (w *deadlineResponseWriter) Write([]byte) (int, error) {
	w.writeOnce.Do(func() { close(w.writeStarted) })
	<-w.writeUnblock
	close(w.writeReturned)
	return 0, os.ErrDeadlineExceeded
}

func (w *deadlineResponseWriter) SetWriteDeadline(time.Time) error {
	w.deadlineOnce.Do(func() {
		close(w.deadlineSet)
		close(w.writeUnblock)
	})
	return nil
}

func TestHTTPServerConnCloseInterruptsBlockedWrite(t *testing.T) {
	writer := newDeadlineResponseWriter()
	conn := newHTTPServerConn(writer, io.NopCloser(http.NoBody))

	writeResult := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("blocked"))
		writeResult <- err
	}()

	select {
	case <-writer.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("ResponseWriter.Write did not start")
	}

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- conn.Close()
	}()

	select {
	case err := <-closeResult:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("httpServerConn.Close blocked behind ResponseWriter.Write")
	}

	select {
	case <-writer.deadlineSet:
	default:
		t.Fatal("Close did not set the response write deadline")
	}
	select {
	case <-writer.writeReturned:
	default:
		t.Fatal("Close returned while the ResponseWriter was still in use")
	}
	require.ErrorIs(t, <-writeResult, os.ErrDeadlineExceeded)
	_, err := conn.Write([]byte("after close"))
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

type countingWriteCloser struct {
	closed atomic.Int32
}

func (*countingWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (c *countingWriteCloser) Close() error {
	c.closed.Add(1)
	return nil
}

type countingReadCloser struct {
	closed atomic.Int32
}

func (*countingReadCloser) Read([]byte) (int, error) { return 0, io.EOF }
func (c *countingReadCloser) Close() error {
	c.closed.Add(1)
	return nil
}

func TestConnCloseIsIdempotentAndDoesNotDeleteReplacementSession(t *testing.T) {
	handler := &requestHandler{sessions: make(map[string]*httpSession)}
	const sessionID = "session"
	original := newHTTPSession(1, 1024)
	handler.sessions[sessionID] = original
	writer := new(countingWriteCloser)
	reader := new(countingReadCloser)
	var closeCallbacks atomic.Int32
	conn := &Conn{
		writer: writer,
		reader: reader,
		onClose: func() {
			closeCallbacks.Add(1)
			handler.deleteSession(sessionID, original)
		},
	}
	require.NoError(t, conn.SetDeadline(time.Now().Add(time.Hour)))
	require.NoError(t, conn.Close())

	replacement := newHTTPSession(1, 1024)
	handler.sessions[sessionID] = replacement
	require.NoError(t, conn.Close())

	require.EqualValues(t, 1, writer.closed.Load())
	require.EqualValues(t, 1, reader.closed.Load())
	require.EqualValues(t, 1, closeCallbacks.Load())
	require.Same(t, replacement, handler.getSession(sessionID))
	require.ErrorIs(t, conn.SetDeadline(time.Now()), net.ErrClosed)
}

func TestRequestHandlerBoundsSessionsWithoutPerSessionReapers(t *testing.T) {
	handler := &requestHandler{
		scMaxBufferedPosts: Range{Min: 1, Max: 1},
		sessions:           make(map[string]*httpSession),
		lastReap:           time.Now(),
	}
	require.True(t, handler.reserveQueueBytes(maxXHTTPGlobalQueuedBytes))
	require.False(t, handler.reserveQueueBytes(1))
	handler.releaseQueueBytes(maxXHTTPGlobalQueuedBytes)
	require.Zero(t, handler.globalQueuedBytes.Load())

	for i := 0; i < maxXHTTPSessions; i++ {
		_, err := handler.upsertSession(fmt.Sprintf("session-%d", i))
		require.NoError(t, err)
	}
	_, err := handler.upsertSession("overflow")
	require.ErrorIs(t, err, errXHTTPSessionLimit)

	orphan := handler.sessions["session-0"]
	orphan.lastActivity.Store(time.Now().Add(-3 * time.Minute).UnixNano())
	handler.lastReap = time.Now().Add(-time.Minute)
	_, err = handler.upsertSession("replacement")
	require.NoError(t, err)
	require.Nil(t, handler.getSession("session-0"))
	for id, session := range handler.sessions {
		handler.deleteSession(id, session)
	}
}

func TestInvalidRouteDoesNotCreateXHTTPSession(t *testing.T) {
	config := Config{Path: "/xhttp", Mode: "stream-one"}
	handlerValue, err := NewServerHandler(ServerOption{Config: config, ConnHandler: func(net.Conn) {}})
	require.NoError(t, err)
	handler := handlerValue.(*requestHandler)
	request := httptest.NewRequest(http.MethodGet, "https://example.com/xhttp/probe", http.NoBody)
	require.NoError(t, config.FillStreamRequest(request, ""))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Result().StatusCode)
	require.Empty(t, handler.sessions)
}

func TestServerHandlerModeRestrictions(t *testing.T) {
	testCases := []struct {
		name       string
		mode       string
		method     string
		target     string
		wantStatus int
	}{
		{
			name:       "StreamOneAcceptsStreamOne",
			mode:       "stream-one",
			method:     http.MethodPost,
			target:     "https://example.com/xhttp/",
			wantStatus: http.StatusOK,
		},
		{
			name:       "StreamOneRejectsSessionDownload",
			mode:       "stream-one",
			method:     http.MethodGet,
			target:     "https://example.com/xhttp/session",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "StreamUpAcceptsStreamOne",
			mode:       "stream-up",
			method:     http.MethodPost,
			target:     "https://example.com/xhttp/",
			wantStatus: http.StatusOK,
		},
		{
			name:       "StreamUpAllowsDownloadEndpoint",
			mode:       "stream-up",
			method:     http.MethodGet,
			target:     "https://example.com/xhttp/session",
			wantStatus: http.StatusOK,
		},
		{
			name:       "StreamUpRejectsPacketUpload",
			mode:       "stream-up",
			method:     http.MethodPost,
			target:     "https://example.com/xhttp/session/0",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "PacketUpAllowsDownloadEndpoint",
			mode:       "packet-up",
			method:     http.MethodGet,
			target:     "https://example.com/xhttp/session",
			wantStatus: http.StatusOK,
		},
		{
			name:       "PacketUpRejectsStreamOne",
			mode:       "packet-up",
			method:     http.MethodPost,
			target:     "https://example.com/xhttp/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "PacketUpRejectsStreamUpUpload",
			mode:       "packet-up",
			method:     http.MethodPost,
			target:     "https://example.com/xhttp/session",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			config := Config{
				Path: "/xhttp",
				Mode: testCase.mode,
			}
			handler, err := NewServerHandler(ServerOption{
				Config: config,
				ConnHandler: func(conn net.Conn) {
					_ = conn.Close()
				},
			})
			assert.NoError(t, err)

			req := httptest.NewRequest(testCase.method, testCase.target, io.NopCloser(http.NoBody))
			recorder := httptest.NewRecorder()

			err = config.FillStreamRequest(req, "")
			assert.NoError(t, err)

			handler.ServeHTTP(recorder, req)

			assert.Equal(t, testCase.wantStatus, recorder.Result().StatusCode)
		})
	}
}
