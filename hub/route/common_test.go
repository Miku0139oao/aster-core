package route

import (
	"strings"
	"testing"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/require"
)

// The controller must not buffer an unbounded request body into memory.
func TestDecodeRequestJSONRejectsOversizedBody(t *testing.T) {
	body := `{"path":"` + strings.Repeat("a", requestBodyLimit) + `"}`
	request := httptest.NewRequest(http.MethodPut, "/configs", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	var decoded struct {
		Path string `json:"path"`
	}
	require.Error(t, decodeRequestJSON(recorder, request, &decoded))
}

func TestDecodeRequestJSONAcceptsBodyWithinLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/configs", strings.NewReader(`{"path":"/etc/config.yaml"}`))
	recorder := httptest.NewRecorder()

	var decoded struct {
		Path string `json:"path"`
	}
	require.NoError(t, decodeRequestJSON(recorder, request, &decoded))
	require.Equal(t, "/etc/config.yaml", decoded.Path)
}
