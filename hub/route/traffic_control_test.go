package route

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/kerneldirect"
	trafficControl "github.com/Miku0139oao/aster-core/component/trafficcontrol"
	"github.com/Miku0139oao/aster-core/listener"
	LC "github.com/Miku0139oao/aster-core/listener/config"

	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/require"
)

type trafficControlCapabilitiesResponse struct {
	TrafficControl struct {
		Compression string `json:"compression"`
		Persistence bool   `json:"persistence"`
	} `json:"traffic_control"`
	KernelDirect struct {
		Version          int      `json:"version"`
		Backends         []string `json:"backends"`
		Features         []string `json:"features"`
		DeprecatedFields []string `json:"deprecated_fields"`
	} `json:"kernel_direct"`
}

type kernelDirectStatusResponse struct {
	Backend     string                     `json:"backend"`
	FastPaths   []json.RawMessage          `json:"fast_paths"`
	LearnedSets []kernelDirectLearnedSet   `json:"learned_sets"`
	Process     kernelDirectProcessStatus  `json:"process"`
	Aster       *kernelDirectTrafficStatus `json:"aster_traffic"`
	Proxy       *kernelDirectTrafficStatus `json:"proxy_traffic"`
}

type kernelDirectLearnedSet struct {
	MaxEntries       *uint32 `json:"max_entries"`
	MaxRecords       *uint64 `json:"max_records"`
	LearnedAddresses *int    `json:"learned_addresses"`
	DirectAddresses  *int    `json:"direct_addresses"`
	ProxyAddresses   *int    `json:"proxy_addresses"`
	LearnedDomains   *int    `json:"learned_domains"`
	Evictions        *uint64 `json:"evictions"`
}

type kernelDirectProcessStatus struct {
	PID *int `json:"pid"`
}

type kernelDirectTrafficStatus struct {
	UploadBytes                int64 `json:"upload_bytes"`
	DownloadBytes              int64 `json:"download_bytes"`
	UploadRateBytesPerSecond   int64 `json:"upload_rate_bytes_per_second"`
	DownloadRateBytesPerSecond int64 `json:"download_rate_bytes_per_second"`
	ActiveConnections          int   `json:"active_connections"`
}

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

	var capabilities trafficControlCapabilitiesResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&capabilities))
	require.Equal(t, "zstd", capabilities.TrafficControl.Compression)
	require.True(t, capabilities.TrafficControl.Persistence)
	require.Equal(t, 4, capabilities.KernelDirect.Version, "hub/route/traffic_control.go must advertise kernel_direct.version 4")
	require.Contains(t, capabilities.KernelDirect.Backends, "ebpf-tc-lpm-lru-redirect")
	require.Contains(t, capabilities.KernelDirect.Features, "tc-tun-redirect")
	require.Contains(t, capabilities.KernelDirect.Features, "local-address-bypass")
	require.Contains(t, capabilities.KernelDirect.Features, "control-plane-traffic-estimate")
	require.Contains(t, capabilities.KernelDirect.Features, "bounded-learned-set")
	require.Contains(t, capabilities.KernelDirect.DeprecatedFields, "proxy_traffic", "hub/route/traffic_control.go must declare the compatibility alias")
}

func TestKernelDirectStatusUsesNftFallbackWithoutActiveFastPath(t *testing.T) {
	controller := kerneldirect.Register(func(string, netip.Addr) bool { return true }, func(kerneldirect.DecisionSets) {}, kerneldirect.ControllerOptions{MaxEntries: 123})
	t.Cleanup(func() { require.NoError(t, controller.Close()) })

	handler := router(false, "", "", Cors{}, asterRoutePolicy{})
	request := httptest.NewRequest("GET", "http://controller/api/aster/kernel-direct/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)

	var status kernelDirectStatusResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&status))
	require.Equal(t, "nftables", status.Backend)
	require.NotNil(t, status.FastPaths, "fast_paths must be a JSON array")
	require.Empty(t, status.FastPaths)
	require.NotNil(t, status.LearnedSets, "learned_sets must be a JSON array")
	require.Len(t, status.LearnedSets, 1)

	learned := status.LearnedSets[0]
	require.NotNil(t, learned.MaxEntries, "learned_sets item is missing max_entries")
	require.NotNil(t, learned.MaxRecords, "learned_sets item is missing max_records; update component/kerneldirect/controller.go")
	require.NotNil(t, learned.LearnedAddresses, "learned_sets item is missing learned_addresses")
	require.NotNil(t, learned.DirectAddresses, "learned_sets item is missing direct_addresses")
	require.NotNil(t, learned.ProxyAddresses, "learned_sets item is missing proxy_addresses")
	require.NotNil(t, learned.LearnedDomains, "learned_sets item is missing learned_domains")
	require.NotNil(t, learned.Evictions, "learned_sets item is missing evictions")
	require.Equal(t, uint32(123), *learned.MaxEntries)
	require.Equal(t, uint64(492), *learned.MaxRecords)
	require.Zero(t, *learned.LearnedAddresses)
	require.Zero(t, *learned.DirectAddresses)
	require.Zero(t, *learned.ProxyAddresses)
	require.Zero(t, *learned.LearnedDomains)
	require.Zero(t, *learned.Evictions)

	require.NotNil(t, status.Process.PID, "process.pid must be present")
	require.Positive(t, *status.Process.PID)
	require.NotNil(t, status.Aster, "aster_traffic must be an object")
	require.NotNil(t, status.Proxy, "proxy_traffic must be an object")
	require.Equal(t, status.Aster, status.Proxy, "aster_traffic and proxy_traffic must be identical")
}

func TestTrafficControlKernelDirectPatchMaxEntriesContract(t *testing.T) {
	t.Run("zero defaults and persists", func(t *testing.T) {
		zero := uint32(0)
		patched := pointerOrDefaultTun(&tunSchema{KernelDirectMaxEntries: &zero}, LC.Tun{KernelDirectMaxEntries: 17})
		require.Equal(t, uint32(4096), patched.KernelDirectMaxEntries)
	})

	t.Run("omitted enable preserves existing tun", func(t *testing.T) {
		eightK := uint32(8192)
		patched := pointerOrDefaultTun(&tunSchema{KernelDirectMaxEntries: &eightK}, LC.Tun{Enable: true, KernelDirectMaxEntries: 4096})
		require.True(t, patched.Enable, "PATCH {tun.kernel-direct-max-entries} must not flip Enable to false")
		require.Equal(t, uint32(8192), patched.KernelDirectMaxEntries)
	})

	t.Run("explicit enable false still disables", func(t *testing.T) {
		disabled := false
		patched := pointerOrDefaultTun(&tunSchema{Enable: &disabled}, LC.Tun{Enable: true})
		require.False(t, patched.Enable)
	})

	t.Run("explicit enable true keeps tun", func(t *testing.T) {
		enabled := true
		eightK := uint32(8192)
		patched := pointerOrDefaultTun(&tunSchema{Enable: &enabled, KernelDirectMaxEntries: &eightK}, LC.Tun{Enable: false, KernelDirectMaxEntries: 4096})
		require.True(t, patched.Enable)
		require.Equal(t, uint32(8192), patched.KernelDirectMaxEntries)
	})

	t.Run("over maximum errors", func(t *testing.T) {
		request := httptest.NewRequest("PATCH", "http://controller/configs", strings.NewReader(`{"tun":{"kernel-direct-max-entries":65537}}`))
		response := httptest.NewRecorder()
		patchConfigs(response, request)
		require.Equal(t, 400, response.Code, "PATCH must reject kernel-direct-max-entries above 65536")
	})
}

func TestPatchConfigsRejectsKernelDirectWithoutAutoRedirect(t *testing.T) {
	previous := listener.LastTunConf
	t.Cleanup(func() { listener.LastTunConf = previous })
	listener.LastTunConf = LC.Tun{Enable: true, AutoRoute: true, AutoRedirect: false, KernelDirect: false}

	request := httptest.NewRequest("PATCH", "http://controller/configs", strings.NewReader(`{"tun":{"kernel-direct":true}}`))
	response := httptest.NewRecorder()
	patchConfigs(response, request)
	require.Equal(t, 400, response.Code, "PATCH must reject kernel-direct without auto-route and auto-redirect")
	require.Contains(t, response.Body.String(), "tun kernel-direct requires auto-route and auto-redirect")
	require.False(t, listener.LastTunConf.KernelDirect, "failed PATCH must not persist kernel-direct")
	require.True(t, listener.LastTunConf.Enable, "failed PATCH must not disable TUN")
}

func TestPatchConfigsReportsTunStartFailure(t *testing.T) {
	previous := listener.LastTunConf
	previousAllowLan := listener.AllowLan()
	t.Cleanup(func() {
		listener.LastTunConf = previous
		listener.SetAllowLan(previousAllowLan)
	})
	listener.LastTunConf = LC.Tun{Enable: true, AutoRoute: true, DNSHijack: []string{"8.8.8.8:53"}}
	listener.SetAllowLan(false)

	request := httptest.NewRequest("PATCH", "http://controller/configs", strings.NewReader(`{"allow-lan":true,"tun":{"dns-hijack":["not-an-addrport"]}}`))
	response := httptest.NewRecorder()
	patchConfigs(response, request)
	require.NotEqual(t, 204, response.Code, "PATCH must not report success after sing_tun.New fails")
	require.Equal(t, 500, response.Code)
	require.Contains(t, response.Body.String(), "dns-hijack")
	require.True(t, listener.LastTunConf.Enable, "failed recreate must keep the previous Enable")
	require.Equal(t, []string{"8.8.8.8:53"}, listener.LastTunConf.DNSHijack)
	require.True(t, listener.GetTunConf().Enable)
	require.False(t, listener.AllowLan(), "failed TUN activation must not commit unrelated PATCH fields")
}

func TestUpdateConfigsReportsTunStartFailureFromApplyConfig(t *testing.T) {
	previous := listener.LastTunConf
	t.Cleanup(func() { listener.LastTunConf = previous })
	listener.LastTunConf = LC.Tun{}

	body, err := json.Marshal(map[string]string{
		"payload": "tun:\n  enable: true\n  dns-hijack:\n    - not-an-addrport\ndns:\n  enable: false\n",
	})
	require.NoError(t, err)
	request := httptest.NewRequest("PUT", "http://controller/configs", strings.NewReader(string(body)))
	response := httptest.NewRecorder()

	updateConfigs(response, request)

	require.Equal(t, 500, response.Code)
	require.Contains(t, response.Body.String(), "dns-hijack")
	require.False(t, listener.LastTunConf.Enable, "failed ApplyConfig must not persist the requested TUN config")
}

func TestPatchConfigsRejectsKernelDirectEBPFWithoutInterfaces(t *testing.T) {
	previous := listener.LastTunConf
	t.Cleanup(func() { listener.LastTunConf = previous })
	listener.LastTunConf = LC.Tun{Enable: true, AutoRoute: true, AutoRedirect: true, KernelDirect: true}

	request := httptest.NewRequest("PATCH", "http://controller/configs", strings.NewReader(`{"tun":{"kernel-direct-ebpf":true}}`))
	response := httptest.NewRecorder()
	patchConfigs(response, request)
	require.Equal(t, 400, response.Code)
	require.Contains(t, response.Body.String(), "tun kernel-direct-ebpf requires kernel-direct-ebpf-interfaces")
	require.False(t, listener.LastTunConf.KernelDirectEBPF)
}

func TestPatchConfigsRejectsKernelDirectEBPFDirectPrefixesWithoutEBPF(t *testing.T) {
	previous := listener.LastTunConf
	t.Cleanup(func() { listener.LastTunConf = previous })
	listener.LastTunConf = LC.Tun{Enable: true, AutoRoute: true, AutoRedirect: true, KernelDirect: true}

	request := httptest.NewRequest("PATCH", "http://controller/configs", strings.NewReader(`{"tun":{"kernel-direct-ebpf-direct-prefixes":["8.8.8.0/24"]}}`))
	response := httptest.NewRecorder()
	patchConfigs(response, request)
	require.Equal(t, 400, response.Code)
	require.Contains(t, response.Body.String(), "tun kernel-direct-ebpf-direct-prefixes requires kernel-direct-ebpf")
	require.Empty(t, listener.LastTunConf.KernelDirectEBPFDirectPrefixes)
}

func TestPatchTuicEnablePreservesOmittedValue(t *testing.T) {
	listen := "127.0.0.1:8443"
	patched := pointerOrDefaultTuicServer(&tuicServerSchema{Listen: &listen}, LC.TuicServer{Enable: true, Listen: "127.0.0.1:443"})
	require.True(t, patched.Enable, "PATCH {tuic-server.listen} must not flip Enable to false")
	require.Equal(t, listen, patched.Listen)

	disabled := false
	patched = pointerOrDefaultTuicServer(&tuicServerSchema{Enable: &disabled}, LC.TuicServer{Enable: true})
	require.False(t, patched.Enable)
}

func TestTrafficControlPoliciesGetPutRoundTripKeepsEnabled(t *testing.T) {
	setupTrafficControlRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{})

	getRequest := httptest.NewRequest("GET", "http://controller/api/aster/traffic-control/policies", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	require.Equal(t, 200, getResponse.Code)

	var got struct {
		Revision uint64          `json:"revision"`
		Config   json.RawMessage `json:"config"`
	}
	require.NoError(t, json.NewDecoder(getResponse.Body).Decode(&got))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got.Config, &raw))
	require.Contains(t, raw, "enabled", "GET must emit kebab-case RawConfig so PUT can round-trip")
	require.NotContains(t, raw, "Enabled")
	require.Equal(t, "true", strings.TrimSpace(string(raw["enabled"])))
	require.Contains(t, raw, "store", "GET must emit store so PUT can keep the same database")

	before, _ := trafficControl.Default.Config()
	putBody, err := json.Marshal(map[string]interface{}{"revision": got.Revision, "config": got.Config})
	require.NoError(t, err)
	putRequest := httptest.NewRequest("PUT", "http://controller/api/aster/traffic-control/policies", strings.NewReader(string(putBody)))
	putRequest.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	require.Equal(t, 200, putResponse.Code, "PUT of GET body must succeed: %s", putResponse.Body.String())
	require.True(t, trafficControl.Default.Enabled(), "GET→PUT must not disable traffic-control")
	after, _ := trafficControl.Default.Config()
	require.Equal(t, before.StorePath, after.StorePath, "GET→PUT must keep the same store path")
}

func TestTrafficControlReportsUnknownKeyUsesEmptyArray(t *testing.T) {
	setupTrafficControlRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{})
	request := httptest.NewRequest("GET", "http://controller/api/aster/traffic-control/reports?key=missing:key", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)

	var payload struct {
		Buckets json.RawMessage `json:"buckets"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&payload))
	require.Equal(t, "[]", string(payload.Buckets), "missing report series must encode buckets as [] not null")
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
