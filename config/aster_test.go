package config

import (
	"path/filepath"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

type testInboundConfig string

func (c testInboundConfig) Name() string {
	return string(c)
}

func (c testInboundConfig) Equal(other C.InboundConfig) bool {
	value, ok := other.(testInboundConfig)
	return ok && value == c
}

type testInboundListener struct {
	name    string
	managed bool
}

func (l *testInboundListener) Name() string            { return l.name }
func (l *testInboundListener) Listen(C.Tunnel) error   { return nil }
func (l *testInboundListener) Close() error            { return nil }
func (l *testInboundListener) Address() string         { return "" }
func (l *testInboundListener) RawAddress() string      { return "" }
func (l *testInboundListener) Config() C.InboundConfig { return testInboundConfig(l.name) }
func (l *testInboundListener) ManagedUserSchema() C.ManagedUserSchema {
	return C.ManagedUserSchema{Protocol: "vless", Credential: "uuid", Flow: true}
}
func (l *testInboundListener) ConfiguredUsers() []C.ManagedUser { return nil }
func (l *testInboundListener) CurrentManagedUsers() []C.ManagedUser {
	return nil
}

func (l *testInboundListener) UpdateManagedUsers([]C.ManagedUser) error {
	return nil
}

type unmanagedTestListener struct {
	name string
}

func (l *unmanagedTestListener) Name() string            { return l.name }
func (l *unmanagedTestListener) Listen(C.Tunnel) error   { return nil }
func (l *unmanagedTestListener) Close() error            { return nil }
func (l *unmanagedTestListener) Address() string         { return "" }
func (l *unmanagedTestListener) RawAddress() string      { return "" }
func (l *unmanagedTestListener) Config() C.InboundConfig { return testInboundConfig(l.name) }

func TestParseAster(t *testing.T) {
	originalHome := C.Path.HomeDir()
	home := t.TempDir()
	C.SetHomeDir(home)
	t.Cleanup(func() { C.SetHomeDir(originalHome) })

	listeners := map[string]C.InboundListener{
		"managed":   &testInboundListener{name: "managed", managed: true},
		"unmanaged": &unmanagedTestListener{name: "unmanaged"},
	}

	aster, err := parseAster(&RawAster{
		Secret:           "0123456789abcdef0123456789abcdef",
		PublicBaseURL:    "https://admin.example.com/",
		ManagedListeners: []string{"managed"},
	}, listeners)
	require.NoError(t, err)
	require.Equal(t, "https://admin.example.com", aster.PublicBaseURL)
	require.Equal(t, filepath.Join(home, "aster-state.json"), aster.StorePath)
	require.Equal(t, []string{"managed"}, aster.ManagedListeners)

	tests := []struct {
		name string
		raw  RawAster
	}{
		{name: "empty secret", raw: RawAster{}},
		{name: "secret whitespace", raw: RawAster{Secret: " secret "}},
		{name: "plain public URL", raw: RawAster{Secret: "secret", PublicBaseURL: "http://admin.example.com"}},
		{name: "relative public URL", raw: RawAster{Secret: "secret", PublicBaseURL: "/admin"}},
		{name: "public URL whitespace", raw: RawAster{Secret: "secret", PublicBaseURL: " https://admin.example.com"}},
		{name: "public URL query", raw: RawAster{Secret: "secret", PublicBaseURL: "https://admin.example.com?token=bad"}},
		{name: "missing listener", raw: RawAster{Secret: "secret", ManagedListeners: []string{"missing"}}},
		{name: "unsupported listener", raw: RawAster{Secret: "secret", ManagedListeners: []string{"unmanaged"}}},
		{name: "duplicate listener", raw: RawAster{Secret: "secret", ManagedListeners: []string{"managed", "managed"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseAster(&test.raw, listeners)
			require.Error(t, err)
		})
	}
}

func TestParseAsterDisabled(t *testing.T) {
	aster, err := parseAster(nil, nil)
	require.NoError(t, err)
	require.Nil(t, aster)
}
