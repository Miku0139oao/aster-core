package aster

import (
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener"

	"github.com/stretchr/testify/require"
)

type managedTestConfig string

func (c managedTestConfig) Name() string {
	return string(c)
}

func (c managedTestConfig) Equal(other C.InboundConfig) bool {
	value, ok := other.(managedTestConfig)
	return ok && value == c
}

type managedTestListener struct {
	mu         sync.Mutex
	name       string
	schema     C.ManagedUserSchema
	configured []C.ManagedUser
	current    []C.ManagedUser
	updateErr  error
	updateHook func(int, []C.ManagedUser) error
	updates    int
	closed     bool
}

func (l *managedTestListener) Name() string          { return l.name }
func (l *managedTestListener) Listen(C.Tunnel) error { return nil }
func (l *managedTestListener) Close() error {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	return nil
}
func (l *managedTestListener) Address() string         { return "127.0.0.1:443" }
func (l *managedTestListener) RawAddress() string      { return "127.0.0.1:443" }
func (l *managedTestListener) Config() C.InboundConfig { return managedTestConfig(l.name) }
func (l *managedTestListener) ManagedUserSchema() C.ManagedUserSchema {
	return l.schema
}

func (l *managedTestListener) ConfiguredUsers() []C.ManagedUser {
	return append([]C.ManagedUser(nil), l.configured...)
}

func (l *managedTestListener) CurrentManagedUsers() []C.ManagedUser {
	return l.users()
}

func (l *managedTestListener) UpdateManagedUsers(users []C.ManagedUser) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.updates++
	if l.updateHook != nil {
		if err := l.updateHook(l.updates, users); err != nil {
			return err
		}
	}
	if l.updateErr != nil {
		return l.updateErr
	}
	l.current = append([]C.ManagedUser(nil), users...)
	return nil
}

func (l *managedTestListener) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func (l *managedTestListener) users() []C.ManagedUser {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]C.ManagedUser(nil), l.current...)
}

func (l *managedTestListener) updateCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.updates
}

func registerManagedTestListener(t *testing.T, managed *managedTestListener) {
	t.Helper()
	listener.PatchInboundListeners(map[string]C.InboundListener{managed.name: managed}, nil, true)
	t.Cleanup(func() {
		listener.PatchInboundListeners(map[string]C.InboundListener{}, nil, true)
	})
}

func newManagedVLESSTestListener() *managedTestListener {
	configured := []C.ManagedUser{{
		PrincipalID: "legacy", Name: "legacy", UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197",
	}}
	return &managedTestListener{
		name:       "vless-in",
		schema:     C.ManagedUserSchema{Protocol: "vless", Credential: "uuid", Flow: true},
		configured: configured,
		current:    append([]C.ManagedUser(nil), configured...),
	}
}

func managerTestConfig(path string, listeners ...string) *Config {
	return &Config{
		Secret:           "0123456789abcdef0123456789abcdef",
		PublicBaseURL:    "https://admin.example.com",
		StorePath:        path,
		ManagedListeners: listeners,
	}
}

func TestManagerReconcileMutateAndTraffic(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))
	t.Cleanup(func() { _ = manager.Configure(nil) })

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NotEqual(t, "legacy", records[0].User.ID)
	require.Equal(t, records[0].User.ID, managed.users()[0].PrincipalID)

	manager.RecordTraffic(managed.name, records[0].User.ID, 100, 40)
	user, revision, err := manager.GetUser(records[0].User.ID)
	require.NoError(t, err)
	require.EqualValues(t, 100, user.UploadBytes)
	require.EqualValues(t, 40, user.DownloadBytes)

	created, revision, err := manager.CreateUser(CreateUserInput{Inbound: managed.name, Name: "second"}, revision)
	require.NoError(t, err)
	require.NotEmpty(t, created.UUID)
	require.Len(t, managed.users(), 2)

	_, _, err = manager.CreateUser(CreateUserInput{Inbound: managed.name, Name: "stale"}, records[0].Revision)
	require.ErrorIs(t, err, ErrConflict)
	require.Len(t, managed.users(), 2)

	disabled := false
	created, revision, err = manager.UpdateUser(created.ID, UpdateUserInput{Enabled: &disabled}, revision)
	require.NoError(t, err)
	require.False(t, created.Enabled)
	require.Len(t, managed.users(), 1)

	updatesBeforeMetadataMutations := managed.updateCount()
	reset, revision, err := manager.ResetTraffic(records[0].User.ID, revision)
	require.NoError(t, err)
	require.Zero(t, reset.UploadBytes)
	require.Zero(t, reset.DownloadBytes)
	require.EqualValues(t, 2, reset.TrafficGeneration)
	require.Equal(t, updatesBeforeMetadataMutations, managed.updateCount())
	_, revision, err = manager.RotateSubscription(records[0].User.ID, revision)
	require.NoError(t, err)
	require.Equal(t, updatesBeforeMetadataMutations, managed.updateCount())
	require.NoError(t, manager.Flush())

	persisted, err := readValidatedStore(storePath)
	require.NoError(t, err)
	require.Equal(t, revision, persisted.Listeners[managed.name].Revision)
	require.Len(t, persisted.Listeners[managed.name].Users, 2)

	require.NoError(t, manager.Configure(nil))
	require.False(t, manager.Status().Enabled)
	require.Equal(t, []C.ManagedUser{{
		PrincipalID: "legacy", Name: "legacy", UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197",
	}}, managed.users())
}

func TestGetUserOverlaysLiveCountersWithoutStoreSync(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	require.NoError(t, manager.Configure(managerTestConfig(filepath.Join(t.TempDir(), "aster-state.json"), managed.name)))
	t.Cleanup(func() { _ = manager.Configure(nil) })

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	require.Len(t, records, 1)

	manager.RecordTraffic(managed.name, records[0].User.ID, 100, 40)

	manager.mu.Lock()
	storeUser := manager.store.Listeners[managed.name].Users[0]
	require.Equal(t, records[0].User.ID, storeUser.ID)
	require.Zero(t, storeUser.UploadBytes)
	require.Zero(t, storeUser.DownloadBytes)
	manager.mu.Unlock()

	user, _, err := manager.GetUser(records[0].User.ID)
	require.NoError(t, err)
	require.EqualValues(t, 100, user.UploadBytes)
	require.EqualValues(t, 40, user.DownloadBytes)
}

func TestManagerRejectsNonPositiveMutationRevision(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	require.NoError(t, manager.Configure(managerTestConfig(filepath.Join(t.TempDir(), "aster-state.json"), managed.name)))
	t.Cleanup(func() { _ = manager.Configure(nil) })

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	_, _, err = manager.CreateUser(CreateUserInput{Inbound: managed.name, Name: "invalid"}, 0)
	require.ErrorIs(t, err, ErrInvalid)
	require.Len(t, managed.users(), 1)
	_, revision, err := manager.GetUser(records[0].User.ID)
	require.NoError(t, err)
	require.Equal(t, records[0].Revision, revision)
}

func TestManagerSnapshotAndUserIndexTrackMutations(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))
	t.Cleanup(func() { _ = manager.Configure(nil) })

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	require.Len(t, records, 1)
	created, revision, err := manager.CreateUser(
		CreateUserInput{Inbound: managed.name, Name: "indexed"},
		records[0].Revision,
	)
	require.NoError(t, err)

	location, exists := manager.userIndex[created.ID]
	require.True(t, exists)
	require.Equal(t, managed.name, location.inbound)
	indexed, indexedRevision, err := manager.GetUser(created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, indexed.ID)
	require.Equal(t, revision, indexedRevision)

	snapshot, err := manager.ManagementSnapshot(managed.name)
	require.NoError(t, err)
	require.Len(t, snapshot.Listeners, 1)
	require.Len(t, snapshot.Users, 2)
	require.Equal(t, snapshot.Listeners[0].Revision, snapshot.Users[0].Revision)
	require.Equal(t, snapshot.Listeners[0].Revision, snapshot.Users[1].Revision)

	revision, err = manager.DeleteUser(created.ID, revision)
	require.NoError(t, err)
	_, exists = manager.userIndex[created.ID]
	require.False(t, exists)
	_, _, err = manager.GetUser(created.ID)
	require.ErrorIs(t, err, ErrNotFound)

	remaining, _, err := manager.GetUser(records[0].User.ID)
	require.NoError(t, err)
	require.Equal(t, records[0].User.ID, remaining.ID)
	require.Positive(t, revision)
}

func TestManagerTrafficRecordingDoesNotWaitForPersistence(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))
	t.Cleanup(func() {
		manager.persistStore = saveStoreLocked
		_ = manager.Configure(nil)
	})

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	manager.RecordTraffic(managed.name, records[0].User.ID, 1, 0)

	persistStarted := make(chan struct{})
	releasePersist := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePersist) }) }
	defer release()
	manager.persistStore = func(string, *Store) error {
		close(persistStarted)
		<-releasePersist
		return nil
	}
	flushDone := make(chan error, 1)
	go func() { flushDone <- manager.Flush() }()

	select {
	case <-persistStarted:
	case <-time.After(time.Second):
		t.Fatal("flush did not reach persistence")
	}
	recordDone := make(chan struct{})
	go func() {
		manager.RecordTraffic(managed.name, records[0].User.ID, 2, 0)
		close(recordDone)
	}()
	select {
	case <-recordDone:
	case <-time.After(time.Second):
		t.Fatal("traffic recording blocked on persistence")
	}

	release()
	require.NoError(t, <-flushDone)
}

func TestManagerPreservesTrafficRecordedDuringReconfigure(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	config := managerTestConfig(storePath, managed.name)
	require.NoError(t, manager.Configure(config))
	t.Cleanup(func() {
		manager.persistStore = saveStoreLocked
		_ = manager.Configure(nil)
	})

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	manager.RecordTraffic(managed.name, records[0].User.ID, 10, 0)

	firstPersistStarted := make(chan struct{})
	releaseFirstPersist := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirstPersist) }) }
	defer release()
	var persistCalls atomic.Int32
	manager.persistStore = func(path string, store *Store) error {
		if persistCalls.Add(1) == 1 {
			close(firstPersistStarted)
			<-releaseFirstPersist
		}
		return saveStoreLocked(path, store)
	}
	reconfigureDone := make(chan error, 1)
	go func() { reconfigureDone <- manager.Configure(config) }()

	select {
	case <-firstPersistStarted:
	case <-time.After(time.Second):
		t.Fatal("reconfigure did not reach persistence")
	}
	manager.RecordTraffic(managed.name, records[0].User.ID, 5, 0)
	release()
	require.NoError(t, <-reconfigureDone)

	user, _, err := manager.GetUser(records[0].User.ID)
	require.NoError(t, err)
	require.EqualValues(t, 15, user.UploadBytes)
	require.True(t, manager.dirty.Load())
	require.NoError(t, manager.Flush())
	persisted, err := readValidatedStore(storePath)
	require.NoError(t, err)
	require.EqualValues(t, 15, persisted.Listeners[managed.name].Users[0].UploadBytes)
}

func TestManagerDisableDrainsRetiringTraffic(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	key := trafficKey{inbound: managed.name, userID: records[0].User.ID}
	retiring := manager.runtime.Load()
	require.True(t, retiring.acquireRecorder())
	released := false
	defer func() {
		if !released {
			retiring.releaseRecorder()
		}
	}()

	disableDone := make(chan error, 1)
	go func() { disableDone <- manager.Configure(nil) }()
	require.Eventually(t, func() bool {
		return retiring.recorders.Load()&runtimeRetiring != 0
	}, time.Second, time.Millisecond)
	select {
	case err := <-disableDone:
		t.Fatalf("disable completed before draining its recorder: %v", err)
	default:
	}

	addTraffic(&retiring.traffic[key].upload, 17)
	manager.dirty.Store(true)
	retiring.releaseRecorder()
	released = true
	require.NoError(t, <-disableDone)

	persisted, err := readValidatedStore(storePath)
	require.NoError(t, err)
	_, _, user := findUser(persisted, records[0].User.ID)
	require.NotNil(t, user)
	require.EqualValues(t, 17, user.UploadBytes)
}

func TestManagerStorePathChangeDrainsRetiringTraffic(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	oldStorePath := filepath.Join(t.TempDir(), "old-state.json")
	newStorePath := filepath.Join(t.TempDir(), "new-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(oldStorePath, managed.name)))
	t.Cleanup(func() { _ = manager.Configure(nil) })

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	key := trafficKey{inbound: managed.name, userID: records[0].User.ID}
	retiring := manager.runtime.Load()
	require.True(t, retiring.acquireRecorder())
	released := false
	defer func() {
		if !released {
			retiring.releaseRecorder()
		}
	}()

	reconfigureDone := make(chan error, 1)
	go func() {
		reconfigureDone <- manager.Configure(managerTestConfig(newStorePath, managed.name))
	}()
	require.Eventually(t, func() bool {
		return retiring.recorders.Load()&runtimeRetiring != 0
	}, time.Second, time.Millisecond)
	select {
	case err := <-reconfigureDone:
		t.Fatalf("store-path change completed before draining its recorder: %v", err)
	default:
	}

	addTraffic(&retiring.traffic[key].download, 23)
	manager.dirty.Store(true)
	retiring.releaseRecorder()
	released = true
	require.NoError(t, <-reconfigureDone)
	require.Equal(t, newStorePath, manager.runtime.Load().storePath)

	persisted, err := readValidatedStore(oldStorePath)
	require.NoError(t, err)
	_, _, user := findUser(persisted, records[0].User.ID)
	require.NotNil(t, user)
	require.EqualValues(t, 23, user.DownloadBytes)
}

func TestManagerDisableClearsRuntimeStateAndFlusher(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))
	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	require.NotNil(t, manager.flushCancel)

	require.NoError(t, manager.Configure(nil))
	runtime := manager.runtime.Load()
	require.Empty(t, runtime.secret)
	require.Empty(t, runtime.storePath)
	require.Empty(t, runtime.managed)
	require.Empty(t, runtime.traffic)
	require.Empty(t, runtime.users)
	require.Empty(t, runtime.subscriptions)
	require.Empty(t, manager.storePath)
	require.Empty(t, manager.store.Listeners)
	require.Nil(t, manager.flushCancel)

	_, _, err = manager.GetUser(records[0].User.ID)
	require.ErrorIs(t, err, ErrDisabled)
	manager.RecordTraffic(managed.name, records[0].User.ID, 1, 1)
	require.False(t, manager.dirty.Load())
}

func TestManagerMutationRollsBackRuntimeAndDirtyState(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))
	t.Cleanup(func() {
		manager.persistStore = saveStoreLocked
		_ = manager.Configure(nil)
	})

	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	before := managed.users()
	manager.RecordTraffic(managed.name, records[0].User.ID, 12, 3)
	manager.persistStore = func(string, *Store) error { return errors.New("injected persistence failure") }
	_, _, err = manager.CreateUser(CreateUserInput{Inbound: managed.name, Name: "rollback"}, records[0].Revision)
	require.ErrorContains(t, err, "injected persistence failure")
	require.Equal(t, before, managed.users())
	require.True(t, manager.dirty.Load())

	persisted, err := readValidatedStore(storePath)
	require.NoError(t, err)
	require.Len(t, persisted.Listeners[managed.name].Users, 1)
}

func TestRetiredRuntimeRejectsLateRecorders(t *testing.T) {
	runtime := newRuntimeState()
	runtime.retireRecorders()
	for i := 0; i < 10; i++ {
		require.False(t, runtime.acquireRecorder())
	}
}

func BenchmarkManagerRecordTraffic(b *testing.B) {
	manager := NewManager()
	runtime := newRuntimeState()
	key := trafficKey{inbound: "vless-in", userID: "user-id"}
	runtime.traffic[key] = &trafficCounter{generation: 1}
	manager.runtime.Store(runtime)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.RecordTraffic(key.inbound, key.userID, 1500, 0)
	}
}

func BenchmarkManagerRecordTrafficParallel(b *testing.B) {
	manager := NewManager()
	runtime := newRuntimeState()
	key := trafficKey{inbound: "vless-in", userID: "user-id"}
	runtime.traffic[key] = &trafficCounter{generation: 1}
	manager.runtime.Store(runtime)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			manager.RecordTraffic(key.inbound, key.userID, 1500, 0)
		}
	})
}

// Recording for distinct users must not contend, otherwise state shared by the
// whole manager rather than the per-user counter is the bottleneck.
func BenchmarkManagerRecordTrafficParallelDistinctUsers(b *testing.B) {
	manager := NewManager()
	runtime := newRuntimeState()
	const userCount = 64
	for i := 0; i < userCount; i++ {
		runtime.traffic[trafficKey{inbound: "vless-in", userID: "user-" + strconv.Itoa(i)}] = &trafficCounter{generation: 1}
	}
	manager.runtime.Store(runtime)
	var seed atomic.Int64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		userID := "user-" + strconv.FormatInt(seed.Add(1)%userCount, 10)
		for pb.Next() {
			manager.RecordTraffic("vless-in", userID, 1500, 0)
		}
	})
}

func TestManagerAllowsPreviouslyManagedListenerRemoval(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "aster-state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))

	listener.PatchInboundListeners(map[string]C.InboundListener{}, nil, true)
	require.NoError(t, manager.Configure(managerTestConfig(storePath)))
	require.True(t, manager.Status().Enabled)
	require.Empty(t, manager.Status().ManagedListeners)
	require.NoError(t, manager.Configure(nil))
}

func TestManagerFailClosedClearsCredentials(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	require.NoError(t, manager.Configure(managerTestConfig(filepath.Join(t.TempDir(), "state.json"), managed.name)))
	require.NotEmpty(t, managed.users())
	require.NoError(t, manager.FailClosed([]string{managed.name}))
	require.Empty(t, managed.users())
	require.False(t, manager.Status().Enabled)
}

func TestManagerFailClosedRemovesListenerThatCannotClear(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	require.NoError(t, manager.Configure(managerTestConfig(filepath.Join(t.TempDir(), "state.json"), managed.name)))
	managed.updateErr = errors.New("injected clear failure")

	err := manager.FailClosed([]string{managed.name})
	require.ErrorContains(t, err, "injected clear failure")
	require.True(t, managed.isClosed())
	require.False(t, manager.Status().Enabled)
	require.ErrorIs(t, listener.WithManagedInboundListener(managed.name, func(C.ManagedUserListener) error { return nil }), listener.ErrInboundListenerNotFound)
}

func TestManagerRollbackFailureClearsListener(t *testing.T) {
	managed := newManagedVLESSTestListener()
	registerManagedTestListener(t, managed)
	manager := NewManager()
	storePath := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, manager.Configure(managerTestConfig(storePath, managed.name)))
	t.Cleanup(func() {
		manager.persistStore = saveStoreLocked
		_ = manager.Configure(nil)
	})
	records, err := manager.ListUserRecords(managed.name)
	require.NoError(t, err)
	managed.updateHook = func(call int, _ []C.ManagedUser) error {
		if call == 3 {
			return errors.New("injected rollback failure")
		}
		return nil
	}
	manager.persistStore = func(string, *Store) error { return errors.New("injected persistence failure") }

	_, _, err = manager.CreateUser(CreateUserInput{Inbound: managed.name, Name: "rollback"}, records[0].Revision)
	require.ErrorContains(t, err, "injected rollback failure")
	require.Empty(t, managed.users())
	require.False(t, manager.Status().Enabled)
}

func TestManagerHoldsExclusiveStoreLockWhileEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first := NewManager()
	second := NewManager()
	require.NoError(t, first.Configure(managerTestConfig(path)))
	t.Cleanup(func() { _ = first.Configure(nil) })

	err := second.Configure(managerTestConfig(path))
	require.ErrorIs(t, err, ErrConflict)
	require.False(t, second.Status().Enabled)

	require.NoError(t, first.Configure(nil))
	require.NoError(t, second.Configure(managerTestConfig(path)))
	require.NoError(t, second.Configure(nil))
}

func TestManagerRejectsListenerUpdateFailure(t *testing.T) {
	managed := newManagedVLESSTestListener()
	managed.updateErr = errors.New("injected update failure")
	registerManagedTestListener(t, managed)
	manager := NewManager()
	err := manager.Configure(managerTestConfig(filepath.Join(t.TempDir(), "state.json"), managed.name))
	require.ErrorContains(t, err, "injected update failure")
	require.False(t, manager.Status().Enabled)
}
