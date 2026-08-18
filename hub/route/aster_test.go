package route

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	asterManager "github.com/Miku0139oao/aster-core/component/aster"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener"

	"github.com/metacubex/http/httptest"
	"github.com/stretchr/testify/require"
)

type asterRouteTestConfig string

func (c asterRouteTestConfig) Name() string {
	return string(c)
}

func (c asterRouteTestConfig) Equal(other C.InboundConfig) bool {
	value, ok := other.(asterRouteTestConfig)
	return ok && value == c
}

type asterRouteTestListener struct {
	name       string
	configured []C.ManagedUser
	current    []C.ManagedUser
}

func (l *asterRouteTestListener) Name() string            { return l.name }
func (l *asterRouteTestListener) Listen(C.Tunnel) error   { return nil }
func (l *asterRouteTestListener) Close() error            { return nil }
func (l *asterRouteTestListener) Address() string         { return "127.0.0.1:443" }
func (l *asterRouteTestListener) RawAddress() string      { return "127.0.0.1:443" }
func (l *asterRouteTestListener) Config() C.InboundConfig { return asterRouteTestConfig(l.name) }
func (l *asterRouteTestListener) ManagedUserSchema() C.ManagedUserSchema {
	return C.ManagedUserSchema{Protocol: "vless", Credential: "uuid", Flow: true}
}

func (l *asterRouteTestListener) ConfiguredUsers() []C.ManagedUser {
	return append([]C.ManagedUser(nil), l.configured...)
}

func (l *asterRouteTestListener) CurrentManagedUsers() []C.ManagedUser {
	return append([]C.ManagedUser(nil), l.current...)
}

func (l *asterRouteTestListener) UpdateManagedUsers(users []C.ManagedUser) error {
	l.current = append([]C.ManagedUser(nil), users...)
	return nil
}

func setupAsterRouteTest(t *testing.T) *asterRouteTestListener {
	t.Helper()
	managed := &asterRouteTestListener{
		name: "vless-in",
		configured: []C.ManagedUser{{
			PrincipalID: "legacy", Name: "legacy", UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197",
		}},
	}
	managed.current = append([]C.ManagedUser(nil), managed.configured...)
	listener.PatchInboundListeners(map[string]C.InboundListener{managed.name: managed}, nil, true)
	require.NoError(t, asterManager.Default.Configure(&asterManager.Config{
		Secret:           "0123456789abcdef0123456789abcdef",
		PublicBaseURL:    "https://controller.example",
		StorePath:        asterTestStorePath(t),
		ManagedListeners: []string{managed.name},
	}))
	t.Cleanup(func() {
		_ = asterManager.Default.Configure(nil)
		listener.PatchInboundListeners(map[string]C.InboundListener{}, nil, true)
	})
	return managed
}

func TestAsterRoutesUseIndependentAuthentication(t *testing.T) {
	setupAsterRouteTest(t)
	handler := router(false, "clash-secret", "", Cors{}, asterRoutePolicy{adminAllowed: true})

	tests := []struct {
		name   string
		path   string
		token  string
		status int
	}{
		{name: "missing Aster token", path: "/api/admin/overview", status: 401},
		{name: "wrong Aster token", path: "/api/admin/overview", token: "wrong", status: 401},
		{name: "Clash token cannot access Aster", path: "/api/admin/overview", token: "clash-secret", status: 401},
		{name: "Aster token accesses Aster", path: "/api/admin/overview", token: "0123456789abcdef0123456789abcdef", status: 200},
		{name: "Aster token cannot access Clash", path: "/version", token: "0123456789abcdef0123456789abcdef", status: 401},
		{name: "Clash token accesses Clash", path: "/version", token: "clash-secret", status: 200},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://controller.example"+test.path, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code)
		})
	}

	request := httptest.NewRequest("GET", "http://controller.example/api/admin/overview", nil)
	request.Header.Set("Authorization", "bearer 0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
}

func TestAsterOverviewAdvertisesUnsupportedCapabilities(t *testing.T) {
	setupAsterRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{adminAllowed: true})

	request := httptest.NewRequest("GET", "http://controller.example/api/admin/overview", nil)
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)

	var overview struct {
		Capabilities map[string]bool `json:"capabilities"`
		Users        map[string]any  `json:"users"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &overview))
	require.Equal(t, map[string]bool{"quota": false, "expiration": false}, overview.Capabilities)
	require.NotContains(t, overview.Users, "expired")
}

func TestDebugRoutesUseControllerAuthentication(t *testing.T) {
	handler := router(true, "clash-secret", "", Cors{}, asterRoutePolicy{})

	request := httptest.NewRequest("GET", "/debug/pprof/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 401, response.Code)

	request = httptest.NewRequest("GET", "/debug/pprof/", nil)
	request.Header.Set("Authorization", "Bearer clash-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
}

func TestAsterAdminOriginAndTransportGating(t *testing.T) {
	setupAsterRouteTest(t)

	blockedHandler := router(false, "", "", Cors{}, asterRoutePolicy{})
	request := httptest.NewRequest("GET", "http://controller.example/api/admin/overview", nil)
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	blockedHandler.ServeHTTP(response, request)
	require.Equal(t, 404, response.Code)

	allowedHandler := router(false, "", "", Cors{}, asterRoutePolicy{adminAllowed: true, secure: true})
	request = httptest.NewRequest("GET", "https://controller.example/api/admin/overview", nil)
	request.Host = "controller.example"
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Set("Origin", "https://evil.example")
	response = httptest.NewRecorder()
	allowedHandler.ServeHTTP(response, request)
	require.Equal(t, 403, response.Code)

	request = httptest.NewRequest("GET", "https://controller.example/api/admin/overview", nil)
	request.Host = "controller.example"
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Set("Origin", "https://controller.example")
	response = httptest.NewRecorder()
	allowedHandler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
}

func TestAsterSubscriptionRouteIsIndependentOfAdminGating(t *testing.T) {
	setupAsterRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{})

	request := httptest.NewRequest("GET", "http://controller.example/sub/aster/invalid", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 404, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
}

func TestAsterUserCRUDAndCredentialRedaction(t *testing.T) {
	managed := setupAsterRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{adminAllowed: true})
	records, err := asterManager.Default.ListUserRecords(managed.name)
	require.NoError(t, err)
	require.Len(t, records, 1)

	request := httptest.NewRequest("GET", "http://controller.example/api/admin/users", nil)
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
	require.NotContains(t, response.Body.String(), records[0].User.UUID)
	require.NotContains(t, response.Body.String(), "quota_bytes")
	require.NotContains(t, response.Body.String(), "expires_at")

	payload, err := json.Marshal(map[string]any{
		"inbound":  managed.name,
		"name":     "second",
		"revision": records[0].Revision,
	})
	require.NoError(t, err)
	request = httptest.NewRequest("POST", "http://controller.example/api/admin/users", bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 201, response.Code, response.Body.String())
	require.Len(t, managed.current, 2)
	require.True(t, strings.Contains(response.Body.String(), `"uuid"`))

	request = httptest.NewRequest("POST", "http://controller.example/api/admin/users", bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 409, response.Code)
	require.Len(t, managed.current, 2)
}

func TestAsterMutationRejectsNonPositiveRevision(t *testing.T) {
	setupAsterRouteTest(t)
	handler := router(false, "", "", Cors{}, asterRoutePolicy{adminAllowed: true})

	request := httptest.NewRequest("POST", "http://controller.example/api/admin/users", bytes.NewBufferString(`{"inbound":"vless-in","name":"invalid"}`))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, 400, response.Code)
	require.Contains(t, response.Body.String(), "revision must be a positive integer")
}
