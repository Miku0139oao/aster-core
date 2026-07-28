package inbound

import (
	"encoding/json"
	"testing"

	LC "github.com/Miku0139oao/aster-core/listener/config"

	"github.com/stretchr/testify/require"
)

const managedUserTestUUID = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

func TestVlessManagedUserStaging(t *testing.T) {
	newListener := func(t *testing.T) *Vless {
		listener, err := NewVless(&VlessOption{
			BaseOption: BaseOption{NameStr: "managed-vless", Listen: "127.0.0.1", Port: "0"},
			Users:      []VlessUser{{Username: "user", UUID: managedUserTestUUID}},
		})
		require.NoError(t, err)
		return listener
	}

	t.Run("stage without listen preserves current users", func(t *testing.T) {
		listener := newListener(t)
		current := listener.CurrentManagedUsers()

		listener.StageManagedUsers()

		require.Equal(t, current, listener.CurrentManagedUsers())
	})

	t.Run("listen starts empty and commits after a failed attempt", func(t *testing.T) {
		listener := newListener(t)
		current := listener.CurrentManagedUsers()
		listener.StageManagedUsers()

		require.Error(t, listener.Listen(nil))
		require.Equal(t, current, listener.CurrentManagedUsers())

		listener.vs.AllowInsecure = true
		require.NoError(t, listener.Listen(nil))
		t.Cleanup(func() { require.NoError(t, listener.Close()) })
		require.Empty(t, listener.CurrentManagedUsers())

		var server LC.VlessServer
		require.NoError(t, json.Unmarshal([]byte(listener.l.Config()), &server))
		require.Empty(t, server.Users)
	})

	t.Run("update clears staging", func(t *testing.T) {
		listener := newListener(t)
		current := listener.CurrentManagedUsers()
		listener.StageManagedUsers()
		require.NoError(t, listener.UpdateManagedUsers(current))

		listener.vs.AllowInsecure = true
		require.NoError(t, listener.Listen(nil))
		t.Cleanup(func() { require.NoError(t, listener.Close()) })
		require.Equal(t, current, listener.CurrentManagedUsers())
	})
}

func TestAnyTLSManagedUserStaging(t *testing.T) {
	newListener := func(t *testing.T) *AnyTLS {
		listener, err := NewAnyTLS(&AnyTLSOption{
			BaseOption: BaseOption{NameStr: "managed-anytls", Listen: "127.0.0.1", Port: "0"},
			Users:      map[string]string{"user": "password"},
		})
		require.NoError(t, err)
		return listener
	}

	t.Run("stage without listen preserves current users", func(t *testing.T) {
		listener := newListener(t)
		current := listener.CurrentManagedUsers()

		listener.StageManagedUsers()

		require.Equal(t, current, listener.CurrentManagedUsers())
	})

	t.Run("listen starts empty and commits after a failed attempt", func(t *testing.T) {
		listener := newListener(t)
		current := listener.CurrentManagedUsers()
		listener.StageManagedUsers()

		require.Error(t, listener.Listen(nil))
		require.Equal(t, current, listener.CurrentManagedUsers())

		listener.vs.AllowInsecure = true
		require.NoError(t, listener.Listen(nil))
		t.Cleanup(func() { require.NoError(t, listener.Close()) })
		require.Empty(t, listener.CurrentManagedUsers())

		var server LC.AnyTLSServer
		require.NoError(t, json.Unmarshal([]byte(listener.l.Config()), &server))
		require.Empty(t, server.Users)
	})

	t.Run("update clears staging", func(t *testing.T) {
		listener := newListener(t)
		current := listener.CurrentManagedUsers()
		listener.StageManagedUsers()
		require.NoError(t, listener.UpdateManagedUsers(current))

		listener.vs.AllowInsecure = true
		require.NoError(t, listener.Listen(nil))
		t.Cleanup(func() { require.NoError(t, listener.Close()) })
		require.Equal(t, current, listener.CurrentManagedUsers())
	})
}
