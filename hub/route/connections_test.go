package route

import (
	"strings"
	"testing"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/require"
)

func TestConnectionRouterRejectsMassDelete(t *testing.T) {
	router := connectionRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(""))
	router.ServeHTTP(rec, req)

	require.NotEqual(t, http.StatusNoContent, rec.Code, "DELETE /connections must not close all connections")
	// GET / exists, so an unsupported DELETE should be 405, not 204.
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestConnectionRouterKeepsIndividualClose(t *testing.T) {
	router := connectionRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/nonexistent-id", strings.NewReader(""))
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}
