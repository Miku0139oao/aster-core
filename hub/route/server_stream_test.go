package route

import (
	"bytes"
	"testing"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/require"
)

type nonFlushingResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *nonFlushingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *nonFlushingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *nonFlushingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func TestWriteStreamingResponseDoesNotRequireFlusher(t *testing.T) {
	writer := &nonFlushingResponseWriter{}
	require.NoError(t, writeStreamingResponse(writer, []byte("payload")))
	require.Equal(t, "payload", writer.body.String())
}

func TestAuthenticationAcceptsCaseInsensitiveWebSocketAndBearerHeaders(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	handler := authentication("secret")(next)

	request := httptest.NewRequest(http.MethodGet, "http://controller.example/?token=secret", nil)
	request.Header.Set("Upgrade", "WebSocket")
	handler.ServeHTTP(&nonFlushingResponseWriter{}, request)
	require.True(t, called)

	called = false
	request = httptest.NewRequest(http.MethodGet, "http://controller.example/", nil)
	request.Header.Set("Authorization", "bearer secret")
	handler.ServeHTTP(&nonFlushingResponseWriter{}, request)
	require.True(t, called)
}
