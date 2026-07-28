package aster

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validTestStore(t *testing.T, userName string) *Store {
	t.Helper()
	token, err := randomToken()
	require.NoError(t, err)
	store := newStore()
	store.Listeners["vless-in"] = &ListenerState{
		ID: "listener-id", Name: "vless-in", Protocol: "vless", Revision: 1, AppliedRevision: 1,
		Users: []*User{{
			ID: "user-id", Inbound: "vless-in", Protocol: "vless", Name: userName,
			UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197", Enabled: true,
			TrafficGeneration: 1, CreatedAt: 1, UpdatedAt: 1,
		}},
	}
	store.Subscriptions["user-id"] = token
	return store
}

func TestStoreAtomicBackupAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aster-state.json")
	first := validTestStore(t, "first")
	require.NoError(t, saveStore(path, first))

	second := cloneStore(first)
	second.Listeners["vless-in"].Users[0].Name = "second"
	second.Listeners["vless-in"].Revision = 2
	second.Listeners["vless-in"].AppliedRevision = 2
	require.NoError(t, saveStore(path, second))
	require.FileExists(t, path+".bak")

	require.NoError(t, os.WriteFile(path, []byte(`{"version":1,"listeners":{"vless-in":null}}`), 0o600))
	recovered, fromBackup, err := loadStore(path)
	require.NoError(t, err)
	require.True(t, fromBackup)
	require.Equal(t, "second", recovered.Listeners["vless-in"].Users[0].Name)
	require.Equal(t, second.Generation, recovered.Generation)

	recovered.Listeners["vless-in"].Revision = 3
	recovered.Listeners["vless-in"].AppliedRevision = 3
	require.NoError(t, saveStoreWithRecovery(path, recovered, true))
	primary, err := readValidatedStore(path)
	require.NoError(t, err)
	require.EqualValues(t, 3, primary.Listeners["vless-in"].Revision)
	backup, err := readValidatedStore(path + ".bak")
	require.NoError(t, err)
	require.EqualValues(t, 3, backup.Listeners["vless-in"].Revision)
	require.Equal(t, primary.Generation, backup.Generation)
	_, err = os.Stat(path + ".tmp")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(path + ".bak.tmp")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadStoreRejectsInvalidPrimaryWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":1,"listeners":{"broken":null}}`), 0o600))
	_, _, err := loadStore(path)
	require.Error(t, err)
}

func TestLoadStoreRejectsVersionDowngradeThroughBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aster-state.json")
	store := validTestStore(t, "valid-backup")
	require.NoError(t, saveStore(path, store))
	require.NoError(t, os.WriteFile(path, []byte(`{"version":2,"listeners":{}}`), 0o600))

	_, _, err := loadStore(path)
	require.ErrorIs(t, err, errUnsupportedStoreVersion)
}

func TestStoreRejectsStaleWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aster-state.json")
	first := validTestStore(t, "first")
	require.NoError(t, saveStore(path, first))
	stale := cloneStore(first)

	first.Listeners["vless-in"].Users[0].Name = "committed"
	require.NoError(t, saveStore(path, first))
	stale.Listeners["vless-in"].Users[0].Name = "stale"
	require.ErrorIs(t, saveStore(path, stale), ErrConflict)

	loaded, _, err := loadStore(path)
	require.NoError(t, err)
	require.Equal(t, "committed", loaded.Listeners["vless-in"].Users[0].Name)
}

func TestStoreRejectsOutputLargerThanReadLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aster-state.json")
	store := validTestStore(t, "large")
	state := store.Listeners["vless-in"]
	state.Protocol = "anytls"
	state.Users[0].Protocol = "anytls"
	state.Users[0].UUID = ""
	state.Users[0].Password = strings.Repeat("x", maxStoreSize)

	err := saveStore(path, store)
	require.ErrorContains(t, err, "exceeds")
	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestStoreRejectsExhaustedGeneration(t *testing.T) {
	store := validTestStore(t, "generation")
	store.Generation = math.MaxUint64
	require.Error(t, validateStore(store))
}
