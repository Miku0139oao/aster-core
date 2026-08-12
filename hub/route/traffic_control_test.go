package route

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	trafficControl "github.com/Miku0139oao/aster-core/component/trafficcontrol"

	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/require"
)

func setupTrafficControlRouteTest(t *testing.T) {
	t.Helper()
	require.NoError(t, trafficControl.Default.Configure(&trafficControl.Config{
		Enabled: true, StorePath: filepath.Join(t.TempDir(), "traffic.db"), CheckpointInterval: time.Hour,
		MaxStoreSize: trafficControl.DefaultStoreLimit,
		Reports:      trafficControl.ReportsConfig{Enabled: true, HourlyRetention: trafficControl.DefaultHourlyRetention, DailyRetention: trafficControl.DefaultDailyRetention, MonthlyRetention: trafficControl.DefaultMonthlyRetention, OrphanRetention: trafficControl.DefaultOrphanRetention},
		Policies:     []trafficControl.Policy{{ID: "global", Kind: trafficControl.PolicyGlobal, Enabled: true}},
	}))
	t.Cleanup(func() { _ = trafficControl.Default.Configure(nil) })
}

func TestTrafficControlRoutesUseControllerAuthentication(t *testing.T) {
	setupTrafficControlRouteTest(t)
	handler := router(false, "controller-secret", "", Cors{}, asterRoutePolicy{})
	for _, test := range []struct {
		token  string
		status int
	}{{"", 401}, {"wrong", 401}, {"controller-secret", 200}} {
		request := httptest.NewRequest("GET", "http://controller/api/aster/traffic-control/status", nil)
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code)
	}
}

func TestTrafficControlCapabilitiesAdvertisePersistence(t *testing.T) {
	setupTrafficControlRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{})
	request := httptest.NewRequest("GET", "http://controller/api/aster/capabilities", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"compression":"zstd"`)
	require.Contains(t, response.Body.String(), `"persistence":true`)
	require.Contains(t, response.Body.String(), `"kernel_direct"`)
	require.Contains(t, response.Body.String(), `"version":3`)
	require.Contains(t, response.Body.String(), `"ebpf-tc-lpm-lru-redirect"`)
	require.Contains(t, response.Body.String(), `"tc-tun-redirect"`)
	require.Contains(t, response.Body.String(), `"local-address-bypass"`)
}

func TestKernelDirectStatusUsesNftFallbackWithoutActiveFastPath(t *testing.T) {
	handler := router(false, "", "", Cors{}, asterRoutePolicy{})
	request := httptest.NewRequest("GET", "http://controller/api/aster/kernel-direct/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"backend":"nftables"`)
}

func TestTrafficControlPolicyUpdateRejectsStaleRevision(t *testing.T) {
	setupTrafficControlRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{})
	_, revision := trafficControl.Default.Config()
	body := fmt.Sprintf(`{"revision":%d,"config":{"enabled":false}}`, revision+1)
	request := httptest.NewRequest("PUT", "http://controller/api/aster/traffic-control/policies", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 409, response.Code)
	require.Contains(t, response.Body.String(), "revision conflict")
}

func TestTrafficControlCSVExport(t *testing.T) {
	setupTrafficControlRouteTest(t)
	session := trafficControl.Default.Open(trafficControl.Flow{})
	require.NotNil(t, session)
	session.Record(trafficControl.Upload, 12)
	session.Record(trafficControl.Download, 34)
	session.Close()

	now := time.Now().UTC()
	query := url.Values{
		"key":         []string{"global:global"},
		"granularity": []string{"hour"},
		"from":        []string{fmt.Sprint(now.Add(-time.Hour).Unix())},
		"to":          []string{fmt.Sprint(now.Add(time.Hour).Unix())},
	}
	request := httptest.NewRequest("GET", "http://controller/api/aster/traffic-control/reports/export.csv?"+query.Encode(), nil)
	response := httptest.NewRecorder()
	router(false, "", "", Cors{}, asterRoutePolicy{}).ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
	require.Equal(t, "text/csv; charset=utf-8", response.Header().Get("Content-Type"))
	require.Contains(t, response.Body.String(), "global:global,hour")
	require.Contains(t, response.Body.String(), ",12,34,1,0")
}
