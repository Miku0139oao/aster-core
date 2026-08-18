package route

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/tunnel/statistic"

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

func TestConnectionIntervalValidation(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: time.Second},
		{name: "positive", query: "250", want: 250 * time.Millisecond},
		{name: "zero", query: "0", wantErr: true},
		{name: "negative", query: "-1", wantErr: true},
		{name: "overflow", query: strconv.FormatInt(maxConnectionIntervalMilliseconds+1, 10), wantErr: true},
		{name: "integer overflow", query: "9223372036854775808", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/connections?interval="+test.query, nil)
			got, err := connectionInterval(request)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestConnectionSnapshotUsesEmptyArray(t *testing.T) {
	snapshot := normalizedConnectionSnapshot(&statistic.Snapshot{})
	body, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.Contains(t, string(body), `"connections":[]`)
}

func TestConnectionRouterRecognizesCaseInsensitiveUpgrade(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/?interval=0", nil)
	request.Header.Set("Upgrade", "WebSocket")
	recorder := httptest.NewRecorder()
	connectionRouter().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "interval must be a positive integer")
}
