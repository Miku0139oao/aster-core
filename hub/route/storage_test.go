package route

import (
	"strings"
	"testing"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/require"
)

func TestStorageRejectsOversizedBodyBeforeJSONParsing(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/large", strings.NewReader(strings.Repeat("x", requestBodyLimit+1)))
	recorder := httptest.NewRecorder()
	storageRouter().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Body.String(), "payload exceeds 1MB limit")
}
