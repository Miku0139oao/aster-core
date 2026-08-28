package aster

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/common/utils"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener"
)

var (
	ErrDisabled = errors.New("aster management is disabled")
	ErrNotFound = errors.New("aster resource not found")
	ErrConflict = errors.New("aster revision conflict")
	ErrInvalid  = errors.New("invalid aster request")
)

type Config struct {
	Secret           string
	PublicBaseURL    string
	StorePath        string
	ManagedListeners []string
}

type Status struct {
	Enabled          bool     `json:"enabled"`
	PublicBaseURL    string   `json:"public_base_url"`
	ManagedListeners []string `json:"managed_listeners"`
}

type trafficKey struct {
	inbound string
	userID  string
}

type trafficCounter struct {
	generation uint64
	upload     atomic.Int64
	download   atomic.Int64
}

type runtimeUser struct {
	revision int64
	user     User
}

type runtimeState struct {
	secret        []byte
	publicBaseURL string
	storePath     string
	managed       map[string]struct{}
	traffic       map[trafficKey]*trafficCounter
	users         map[string]*runtimeUser
	subscriptions map[string]string
	recorders     atomic.Uint64
	drained       chan struct{}
	drainOnce     sync.Once
}

type Manager struct {
	mu           sync.Mutex
	trafficMu    sync.RWMutex
	config       *Config
	store        *Store
	userIndex    map[string]userLocation
	storePath    string
	storeUnlock  func()
	persistStore func(string, *Store) error
	instances    map[string]uintptr
	runtime      atomic.Pointer[runtimeState]
	dirty        atomic.Bool
	flushCancel  chan struct{}
}

var Default = NewManager()

func NewManager() *Manager {
	manager := &Manager{
		store:        newStore(),
		userIndex:    make(map[string]userLocation),
		persistStore: saveStoreLocked,
		instances:    make(map[string]uintptr),
	}
	manager.runtime.Store(newRuntimeState())
	return manager
}

func (m *Manager) Configure(config *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trafficMu.Lock()
	defer m.trafficMu.Unlock()

	wasDirty := m.dirty.Swap(false)
	m.syncTrafficLocked()
	if wasDirty && m.storePath != "" {
		if err := m.persistStore(m.storePath, m.store); err != nil {
			m.dirty.Store(true)
			return fmt.Errorf("persist Aster traffic before reconfigure: %w", err)
		}
	}
	if config == nil {
		return m.disableLocked()
	}
	config = cloneConfig(config)
	if err := validateConfig(config); err != nil {
		return err
	}
	sameStore := m.storeUnlock != nil && m.storePath == config.StorePath
	var acquiredUnlock func()
	if !sameStore {
		if err := prepareStoreDirectory(config.StorePath); err != nil {
			return err
		}
		var err error
		acquiredUnlock, err = lockStore(config.StorePath)
		if err != nil {
			return err
		}
		defer func() {
			if acquiredUnlock != nil {
				acquiredUnlock()
			}
		}()
	}

	store, _, err := loadStore(config.StorePath)
	if err != nil {
		return err
	}
	changes, instances, err := m.prepareChangesLocked(config, store)
	if err != nil {
		return err
	}
	if err := validateStore(store); err != nil {
		return err
	}
	if err := applyChanges(changes); err != nil {
		if errors.Is(err, errRollbackFailed) {
			return errors.Join(err, m.failClosedStateLocked(config.ManagedListeners))
		}
		return err
	}
	for _, change := range changes {
		if _, managed := instances[change.name]; managed {
			store.Listeners[change.name].AppliedRevision = store.Listeners[change.name].Revision
		}
	}
	if err := m.persistStore(config.StorePath, store); err != nil {
		rollbackErr := rollbackChanges(changes)
		if rollbackErr != nil {
			rollbackErr = errors.Join(rollbackErr, m.failClosedStateLocked(config.ManagedListeners))
		}
		return errors.Join(fmt.Errorf("persist Aster state: %w", err), rollbackErr)
	}

	if sameStore {
		m.config = config
		m.store = store
		m.userIndex = buildUserIndex(store)
		m.storePath = config.StorePath
		m.instances = instances
		m.publishLocked()
	} else {
		oldStore := m.store
		oldStorePath := m.storePath
		next := buildRuntimeState(config, config.StorePath, store, m.runtime.Load())
		m.swapRuntimeLocked(next, oldStore)
		if oldStorePath != "" {
			if err := m.persistStore(oldStorePath, oldStore); err != nil {
				restored := buildRuntimeState(m.config, m.storePath, m.store, next)
				m.swapRuntimeLocked(restored, store)
				candidatePersistErr := m.persistStore(config.StorePath, store)
				if candidatePersistErr != nil {
					candidatePersistErr = fmt.Errorf("persist traffic recorded by replacement Aster runtime: %w", candidatePersistErr)
				}
				rollbackErr := rollbackChanges(changes)
				m.dirty.Store(true)
				if rollbackErr != nil {
					rollbackErr = errors.Join(rollbackErr, m.failClosedStateLocked(config.ManagedListeners))
				}
				return errors.Join(
					fmt.Errorf("persist retired Aster traffic before changing store: %w", err),
					candidatePersistErr,
					rollbackErr,
				)
			}
		}

		oldUnlock := m.storeUnlock
		m.storeUnlock = acquiredUnlock
		acquiredUnlock = nil
		m.config = config
		m.store = store
		m.userIndex = buildUserIndex(store)
		m.storePath = config.StorePath
		m.instances = instances
		if oldUnlock != nil {
			oldUnlock()
		}
	}
	m.startFlusherLocked()
	return nil
}

func (m *Manager) disableLocked() error {
	if m.config == nil {
		m.stopFlusherLocked()
		m.releaseStoreLocked()
		return nil
	}
	names := sortedSetKeys(stringSet(m.config.ManagedListeners))
	changes := make([]listenerChange, 0, len(names))
	for _, name := range names {
		err := listener.WithManagedInboundListener(name, func(managed C.ManagedUserListener) error {
			changes = append(changes, listenerChange{
				name:     name,
				instance: listenerIdentity(managed),
				before:   managed.CurrentManagedUsers(),
				after:    managed.ConfiguredUsers(),
			})
			return nil
		})
		if err != nil {
			if errors.Is(err, listener.ErrInboundListenerNotFound) || errors.Is(err, listener.ErrInboundListenerNotManaged) {
				continue
			}
			return err
		}
	}
	if err := applyChanges(changes); err != nil {
		if errors.Is(err, errRollbackFailed) {
			return errors.Join(err, m.failClosedStateLocked(names))
		}
		return err
	}
	next := newRuntimeState()
	m.swapRuntimeLocked(next, m.store)
	if m.storePath != "" {
		if err := m.persistStore(m.storePath, m.store); err != nil {
			restored := buildRuntimeState(m.config, m.storePath, m.store, next)
			m.swapRuntimeLocked(restored, m.store)
			m.dirty.Store(true)
			rollbackErr := rollbackChanges(changes)
			if rollbackErr != nil {
				rollbackErr = errors.Join(rollbackErr, m.failClosedStateLocked(names))
			}
			return errors.Join(fmt.Errorf("persist retired Aster traffic before disabling: %w", err), rollbackErr)
		}
	}
	m.config = nil
	m.instances = make(map[string]uintptr)
	m.dirty.Store(false)
	m.stopFlusherLocked()
	m.releaseStoreLocked()
	return nil
}

type listenerChange struct {
	name     string
	instance uintptr
	before   []C.ManagedUser
	after    []C.ManagedUser
}

func (m *Manager) prepareChangesLocked(config *Config, store *Store) ([]listenerChange, map[string]uintptr, error) {
	newManaged := stringSet(config.ManagedListeners)
	allManaged := stringSet(config.ManagedListeners)
	if m.config != nil {
		for _, name := range m.config.ManagedListeners {
			allManaged[name] = struct{}{}
		}
	}
	changes := make([]listenerChange, 0, len(allManaged))
	instances := make(map[string]uintptr, len(newManaged))

	for _, name := range sortedSetKeys(allManaged) {
		_, required := newManaged[name]
		err := listener.WithManagedInboundListener(name, func(managed C.ManagedUserListener) error {
			schema := managed.ManagedUserSchema()
			identity := listenerIdentity(managed)
			configured := managed.ConfiguredUsers()
			before := managed.CurrentManagedUsers()

			after := configured
			if _, ok := newManaged[name]; ok {
				state := store.Listeners[name]
				if state == nil {
					var err error
					state, err = seedListener(name, schema, configured, store.Subscriptions)
					if err != nil {
						return err
					}
					store.Listeners[name] = state
				}
				if state.Protocol != schema.Protocol {
					return fmt.Errorf("listener %q changed protocol from %s to %s", name, state.Protocol, schema.Protocol)
				}
				after = activeManagedUsers(state)
				instances[name] = identity
			}
			changes = append(changes, listenerChange{name: name, instance: identity, before: before, after: after})
			return nil
		})
		if err != nil {
			if !required && (errors.Is(err, listener.ErrInboundListenerNotFound) || errors.Is(err, listener.ErrInboundListenerNotManaged)) {
				continue
			}
			return nil, nil, err
		}
	}
	return changes, instances, nil
}

func (m *Manager) FailClosed(names []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trafficMu.Lock()
	defer m.trafficMu.Unlock()

	wasDirty := m.dirty.Swap(false)
	m.syncTrafficLocked()
	var closeErr error
	if wasDirty && m.storePath != "" {
		if err := m.persistStore(m.storePath, m.store); err != nil {
			m.dirty.Store(true)
			closeErr = errors.Join(closeErr, fmt.Errorf("persist Aster traffic while failing closed: %w", err))
		}
	}
	return errors.Join(closeErr, m.failClosedStateLocked(names))
}

func (m *Manager) failClosedStateLocked(names []string) error {
	var closeErr error
	managedNames := stringSet(names)
	if m.config != nil {
		for _, name := range m.config.ManagedListeners {
			managedNames[name] = struct{}{}
		}
	}
	for _, name := range sortedSetKeys(managedNames) {
		err := listener.FailClosedManagedInboundListener(name)
		if err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("fail closed listener %q: %w", name, err))
		}
	}
	m.swapRuntimeLocked(newRuntimeState(), m.store)
	var persistErr error
	if m.storePath != "" {
		if err := m.persistStore(m.storePath, m.store); err != nil {
			persistErr = fmt.Errorf("persist retired Aster traffic while failing closed: %w", err)
		}
	}
	m.config = nil
	m.instances = make(map[string]uintptr)
	if persistErr != nil {
		m.dirty.Store(true)
		return errors.Join(closeErr, persistErr)
	}
	m.dirty.Store(false)
	m.stopFlusherLocked()
	m.releaseStoreLocked()
	return closeErr
}

func (m *Manager) releaseStoreLocked() {
	if m.storeUnlock != nil {
		m.storeUnlock()
		m.storeUnlock = nil
	}
	m.store = newStore()
	m.userIndex = make(map[string]userLocation)
	m.storePath = ""
}

func applyChanges(changes []listenerChange) error {
	applied := make([]listenerChange, 0, len(changes))
	for _, change := range changes {
		err := listener.WithManagedInboundListener(change.name, func(managed C.ManagedUserListener) error {
			if listenerIdentity(managed) != change.instance {
				return errors.New("listener instance changed")
			}
			return managed.UpdateManagedUsers(change.after)
		})
		if err != nil {
			applied = append(applied, change)
			rollbackErr := rollbackChanges(applied)
			if rollbackErr != nil {
				return errors.Join(
					fmt.Errorf("apply managed users to listener %q: %w", change.name, err),
					fmt.Errorf("%w: %v", errRollbackFailed, rollbackErr),
					rollbackErr,
				)
			}
			return fmt.Errorf("apply managed users to listener %q: %w", change.name, err)
		}
		applied = append(applied, change)
	}
	return nil
}

var errRollbackFailed = errors.New("aster listener rollback failed")

func rollbackChanges(changes []listenerChange) error {
	var rollbackErr error
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		err := listener.WithManagedInboundListener(change.name, func(managed C.ManagedUserListener) error {
			if listenerIdentity(managed) != change.instance {
				return errors.New("listener instance changed")
			}
			return managed.UpdateManagedUsers(change.before)
		})
		if err != nil {
			closeErr := listener.FailClosedManagedInboundListener(change.name)
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("roll back listener %q: %w", change.name, err), closeErr)
		}
	}
	return rollbackErr
}

func seedListener(name string, schema C.ManagedUserSchema, configured []C.ManagedUser, subscriptions map[string]string) (*ListenerState, error) {
	now := time.Now().UnixMilli()
	state := &ListenerState{
		ID:       utils.NewUUIDV4().String(),
		Name:     name,
		Protocol: schema.Protocol,
		Users:    make([]*User, 0, len(configured)),
		Revision: nextRevision(0),
	}
	names := make(map[string]struct{}, len(configured))
	for i, user := range configured {
		userID := utils.NewUUIDV4().String()
		nameCandidate := strings.TrimSpace(user.Name)
		if nameCandidate == "" {
			nameCandidate = fmt.Sprintf("user-%d", i+1)
		}
		baseName := nameCandidate
		for suffix := 2; ; suffix++ {
			key := strings.ToLower(nameCandidate)
			if _, exists := names[key]; !exists {
				names[key] = struct{}{}
				break
			}
			nameCandidate = fmt.Sprintf("%s-%d", baseName, suffix)
		}
		state.Users = append(state.Users, &User{
			ID:                userID,
			Inbound:           name,
			Protocol:          schema.Protocol,
			Name:              nameCandidate,
			UUID:              user.UUID,
			Password:          user.Password,
			Flow:              user.Flow,
			Enabled:           true,
			TrafficGeneration: 1,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		token, err := randomToken()
		if err != nil {
			return nil, err
		}
		subscriptions[userID] = token
	}
	return state, nil
}

func validateConfig(config *Config) error {
	if len(config.Secret) < 32 || strings.TrimSpace(config.Secret) != config.Secret {
		return fmt.Errorf("%w: secret must contain at least 32 bytes without leading or trailing whitespace", ErrInvalid)
	}
	if config.StorePath == "" {
		return fmt.Errorf("%w: store path is required", ErrInvalid)
	}
	if config.PublicBaseURL != "" {
		if strings.TrimSpace(config.PublicBaseURL) != config.PublicBaseURL {
			return fmt.Errorf("%w: public base URL cannot have leading or trailing whitespace", ErrInvalid)
		}
		publicURL, err := url.Parse(config.PublicBaseURL)
		if err != nil || publicURL.Scheme != "https" || publicURL.Host == "" || publicURL.User != nil || publicURL.RawQuery != "" || publicURL.ForceQuery || publicURL.Fragment != "" || publicURL.Opaque != "" {
			return fmt.Errorf("%w: public base URL must be an absolute HTTPS URL without user information, query, or fragment", ErrInvalid)
		}
	}
	seen := make(map[string]struct{}, len(config.ManagedListeners))
	for _, name := range config.ManagedListeners {
		if name == "" {
			return fmt.Errorf("%w: managed listener name is empty", ErrInvalid)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate managed listener %q", ErrInvalid, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateStore(store *Store) error {
	if store.Version != storeVersion {
		return fmt.Errorf("%w: unsupported store version %d", ErrInvalid, store.Version)
	}
	if store.Generation == math.MaxUint64 {
		return fmt.Errorf("%w: store generation is exhausted", ErrInvalid)
	}
	userIDs := make(map[string]struct{})
	tokens := make(map[string]struct{})
	for inbound, state := range store.Listeners {
		if state == nil || state.ID == "" || state.Name != inbound || state.Protocol == "" || state.Revision <= 0 || state.AppliedRevision < 0 || state.AppliedRevision > state.Revision {
			return fmt.Errorf("%w: invalid listener state for %q", ErrInvalid, inbound)
		}
		names := make(map[string]struct{}, len(state.Users))
		credentials := make(map[string]struct{}, len(state.Users))
		for _, user := range state.Users {
			if user == nil || user.ID == "" || user.Inbound != inbound || user.Protocol != state.Protocol || strings.TrimSpace(user.Name) == "" || strings.TrimSpace(user.Name) != user.Name || len(user.Name) > 256 {
				return fmt.Errorf("%w: invalid user in listener %q", ErrInvalid, inbound)
			}
			if _, exists := userIDs[user.ID]; exists {
				return fmt.Errorf("%w: duplicate user ID %q", ErrInvalid, user.ID)
			}
			userIDs[user.ID] = struct{}{}
			nameKey := strings.ToLower(user.Name)
			if _, exists := names[nameKey]; exists {
				return fmt.Errorf("%w: duplicate user name %q in listener %q", ErrInvalid, user.Name, inbound)
			}
			names[nameKey] = struct{}{}
			credential, err := userCredentialKey(user)
			if err != nil {
				return err
			}
			if _, exists := credentials[credential]; exists {
				return fmt.Errorf("%w: duplicate credential in listener %q", ErrInvalid, inbound)
			}
			credentials[credential] = struct{}{}
			if user.TrafficGeneration == 0 || user.UploadBytes < 0 || user.DownloadBytes < 0 || user.CreatedAt <= 0 || user.UpdatedAt < user.CreatedAt {
				return fmt.Errorf("%w: invalid metadata for user %q", ErrInvalid, user.ID)
			}
			token := store.Subscriptions[user.ID]
			if !validSubscriptionToken(token) {
				return fmt.Errorf("%w: missing subscription token for user %q", ErrInvalid, user.ID)
			}
			if _, exists := tokens[token]; exists {
				return fmt.Errorf("%w: duplicate subscription token", ErrInvalid)
			}
			tokens[token] = struct{}{}
		}
	}
	for userID := range store.Subscriptions {
		if _, exists := userIDs[userID]; !exists {
			return fmt.Errorf("%w: subscription token references unknown user %q", ErrInvalid, userID)
		}
	}
	return nil
}

func userCredentialKey(user *User) (string, error) {
	switch user.Protocol {
	case "vless":
		if user.UUID == "" || user.Password != "" {
			return "", fmt.Errorf("%w: VLESS user %q requires a UUID", ErrInvalid, user.ID)
		}
		if user.Flow != "" && user.Flow != "xtls-rprx-vision" {
			return "", fmt.Errorf("%w: unsupported VLESS flow %q", ErrInvalid, user.Flow)
		}
		return utils.UUIDMap(user.UUID).String(), nil
	case "anytls":
		if user.Password == "" || user.UUID != "" || user.Flow != "" {
			return "", fmt.Errorf("%w: AnyTLS user %q requires a password", ErrInvalid, user.ID)
		}
		return user.Password, nil
	default:
		return "", fmt.Errorf("%w: unsupported managed protocol %q", ErrInvalid, user.Protocol)
	}
}

func activeManagedUsers(state *ListenerState) []C.ManagedUser {
	users := make([]C.ManagedUser, 0, len(state.Users))
	for _, user := range state.Users {
		if !user.Enabled {
			continue
		}
		users = append(users, C.ManagedUser{
			PrincipalID: user.ID,
			Name:        user.Name,
			UUID:        user.UUID,
			Password:    user.Password,
			Flow:        user.Flow,
		})
	}
	return users
}

func listenerIdentity(managed C.ManagedUserListener) uintptr {
	value := reflect.ValueOf(managed)
	if value.Kind() == reflect.Pointer {
		return value.Pointer()
	}
	return 0
}

func cloneConfig(config *Config) *Config {
	cloned := *config
	cloned.ManagedListeners = append([]string(nil), config.ManagedListeners...)
	return &cloned
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sortedSetKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nextRevision(previous int64) int64 {
	next := time.Now().UnixMilli()
	if next <= previous {
		next = previous + 1
	}
	return next
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validSubscriptionToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == token
}

func (m *Manager) Status() Status {
	runtime := m.runtime.Load()
	listeners := sortedSetKeys(runtime.managed)
	return Status{
		Enabled:          len(runtime.secret) != 0,
		PublicBaseURL:    runtime.publicBaseURL,
		ManagedListeners: listeners,
	}
}

func (m *Manager) Enabled() bool {
	return len(m.runtime.Load().secret) != 0
}

func (m *Manager) Authenticate(secret string) bool {
	runtime := m.runtime.Load()
	return len(runtime.secret) != 0 && subtle.ConstantTimeCompare(runtime.secret, []byte(secret)) == 1
}

func newRuntimeState() *runtimeState {
	return &runtimeState{
		managed:       make(map[string]struct{}),
		traffic:       make(map[trafficKey]*trafficCounter),
		users:         make(map[string]*runtimeUser),
		subscriptions: make(map[string]string),
		drained:       make(chan struct{}),
	}
}

func buildRuntimeState(config *Config, storePath string, store *Store, previous *runtimeState) *runtimeState {
	runtime := newRuntimeState()
	if config == nil {
		return runtime
	}
	runtime.storePath = storePath
	runtime.secret = []byte(config.Secret)
	runtime.publicBaseURL = config.PublicBaseURL
	for _, name := range config.ManagedListeners {
		runtime.managed[name] = struct{}{}
	}
	for inbound, state := range store.Listeners {
		_, managed := runtime.managed[inbound]
		for _, user := range state.Users {
			key := trafficKey{inbound: inbound, userID: user.ID}
			var counter *trafficCounter
			if previous.storePath == runtime.storePath {
				counter = previous.traffic[key]
			}
			if counter == nil || counter.generation != user.TrafficGeneration {
				counter = &trafficCounter{generation: user.TrafficGeneration}
				counter.upload.Store(user.UploadBytes)
				counter.download.Store(user.DownloadBytes)
			}
			runtime.traffic[key] = counter
			if managed {
				copied := *user
				if counter.generation == copied.TrafficGeneration {
					copied.UploadBytes = counter.upload.Load()
					copied.DownloadBytes = counter.download.Load()
				}
				runtime.users[user.ID] = &runtimeUser{revision: state.Revision, user: copied}
			}
		}
	}
	for userID, token := range store.Subscriptions {
		if token != "" {
			runtime.subscriptions[token] = userID
		}
	}
	return runtime
}

func (m *Manager) publishLocked() {
	previous := m.runtime.Load()
	m.swapRuntimeLocked(buildRuntimeState(m.config, m.storePath, m.store, previous), m.store)
}

func (m *Manager) swapRuntimeLocked(runtime *runtimeState, retiringStore *Store) {
	previous := m.runtime.Swap(runtime)
	previous.retireRecorders()
	syncTrafficStore(retiringStore, previous)
}
