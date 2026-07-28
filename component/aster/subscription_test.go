package aster

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"path/filepath"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener"
	LI "github.com/Miku0139oao/aster-core/listener/inbound"

	"github.com/stretchr/testify/require"
)

type subscriptionTestListener struct {
	*managedTestListener
	config  C.InboundConfig
	address string
}

func (l *subscriptionTestListener) Config() C.InboundConfig { return l.config }
func (l *subscriptionTestListener) Address() string         { return l.address }

func TestAnyTLSRealitySubscription(t *testing.T) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	privateKeyText := base64.RawURLEncoding.EncodeToString(privateKey.Bytes())
	publicKeyText := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	option := &LI.AnyTLSOption{
		BaseOption: LI.BaseOption{NameStr: "anytls-in", Listen: "0.0.0.0", Port: "443"},
		RealityConfig: LI.RealityConfig{
			PrivateKey: privateKeyText,
			ShortID:    []string{"0123456789abcdef"},
			ServerNames: []string{
				"reality.example.com",
			},
		},
	}
	configured := []C.ManagedUser{{PrincipalID: "legacy", Name: "alice", Password: "correct horse battery staple"}}
	managed := &subscriptionTestListener{
		managedTestListener: &managedTestListener{
			name: "anytls-in", schema: C.ManagedUserSchema{Protocol: "anytls", Credential: "password"},
			configured: configured, current: append([]C.ManagedUser(nil), configured...),
		},
		config:  option,
		address: "0.0.0.0:8443",
	}
	listener.PatchInboundListeners(map[string]C.InboundListener{managed.name: managed}, nil, true)
	manager := NewManager()
	require.NoError(t, manager.Configure(managerTestConfig(filepath.Join(t.TempDir(), "state.json"), managed.name)))
	t.Cleanup(func() {
		_ = manager.Configure(nil)
		listener.PatchInboundListeners(map[string]C.InboundListener{}, nil, true)
	})

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	require.Len(t, records, 1)
	token, err := manager.SubscriptionToken(records[0].User.ID)
	require.NoError(t, err)
	link, err := manager.SubscriptionLink(token)
	require.NoError(t, err)

	parsed, err := url.Parse(link)
	require.NoError(t, err)
	require.Equal(t, "anytls", parsed.Scheme)
	require.Equal(t, "correct horse battery staple", parsed.User.Username())
	require.Equal(t, "admin.example.com", parsed.Hostname())
	require.Equal(t, "8443", parsed.Port())
	require.Equal(t, "reality", parsed.Query().Get("security"))
	require.Equal(t, publicKeyText, parsed.Query().Get("pbk"))
	require.Equal(t, "0123456789abcdef", parsed.Query().Get("sid"))
	require.Equal(t, "reality.example.com", parsed.Query().Get("sni"))
	require.Equal(t, "chrome", parsed.Query().Get("fp"))

	subscriptionURL, err := manager.SubscriptionURL(records[0].User.ID)
	require.NoError(t, err)
	require.Equal(t, "https://admin.example.com/sub/aster/"+token, subscriptionURL)
}

func TestSubscriptionRejectsUnsupportedSecurityWrapper(t *testing.T) {
	option := &LI.VlessOption{
		BaseOption: LI.BaseOption{NameStr: "vless-in", Port: "443"},
		ShadowTLS:  LI.ShadowTLS{Enable: true},
	}
	managed := &subscriptionTestListener{
		managedTestListener: &managedTestListener{name: "vless-in"},
		config:              option,
		address:             "127.0.0.1:443",
	}
	_, err := buildSubscriptionLink("https://admin.example.com", managed, &User{
		Inbound: "vless-in", Protocol: "vless", Name: "alice", UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197",
	})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestVLESSSubscriptionDoesNotAdvertiseTLSWithoutPrivateKey(t *testing.T) {
	option := &LI.VlessOption{
		BaseOption:  LI.BaseOption{NameStr: "vless-in", Port: "443"},
		Certificate: "certificate.pem",
	}
	managed := &subscriptionTestListener{
		managedTestListener: &managedTestListener{name: "vless-in"},
		config:              option,
		address:             "127.0.0.1:443",
	}
	link, err := buildSubscriptionLink("https://admin.example.com", managed, &User{
		Inbound: "vless-in", Protocol: "vless", Name: "alice", UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197",
	})
	require.NoError(t, err)
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	require.Empty(t, parsed.Query().Get("security"))
}
