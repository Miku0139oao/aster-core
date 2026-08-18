package aster

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Miku0139oao/aster-core/common/utils"
)

type CreateUserInput struct {
	Inbound  string
	Name     string
	UUID     string
	Password string
	Flow     string
	Enabled  *bool
}

type UpdateUserInput struct {
	Name     *string
	UUID     *string
	Password *string
	Flow     *string
	Enabled  *bool
}

type UserRecord struct {
	User            User  `json:"user"`
	Revision        int64 `json:"revision"`
	AppliedRevision int64 `json:"applied_revision"`
}

type ManagementSnapshot struct {
	Listeners []ListenerState
	Users     []UserRecord
}

// ListenerSummary describes a managed listener without copying its users.
type ListenerSummary struct {
	Name             string
	Protocol         string
	UserCount        int
	EnabledUserCount int
	Revision         int64
	AppliedRevision  int64
}

// Summary aggregates the managed listeners for the overview endpoint. It exists
// so that polling the overview does not clone every user only to count them.
type Summary struct {
	Listeners    []ListenerSummary
	TotalUsers   int
	EnabledUsers int
}

func (m *Manager) Summary() (Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return Summary{}, ErrDisabled
	}
	summary := Summary{Listeners: make([]ListenerSummary, 0, len(m.config.ManagedListeners))}
	for _, listenerName := range m.config.ManagedListeners {
		state := m.store.Listeners[listenerName]
		if state == nil {
			continue
		}
		listener := ListenerSummary{
			Name: state.Name, Protocol: state.Protocol, UserCount: len(state.Users),
			Revision: state.Revision, AppliedRevision: state.AppliedRevision,
		}
		for _, user := range state.Users {
			if user.Enabled {
				listener.EnabledUserCount++
			}
		}
		summary.TotalUsers += listener.UserCount
		summary.EnabledUsers += listener.EnabledUserCount
		summary.Listeners = append(summary.Listeners, listener)
	}
	sort.Slice(summary.Listeners, func(i, j int) bool {
		return summary.Listeners[i].Name < summary.Listeners[j].Name
	})
	return summary, nil
}

type userLocation struct {
	inbound string
	index   int
}

func (m *Manager) ManagementSnapshot(inbound string) (ManagementSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return ManagementSnapshot{}, ErrDisabled
	}
	m.syncTrafficLocked()

	snapshot := ManagementSnapshot{
		Listeners: make([]ListenerState, 0, len(m.config.ManagedListeners)),
	}
	for _, listenerName := range m.config.ManagedListeners {
		state := m.store.Listeners[listenerName]
		if state == nil {
			continue
		}
		snapshot.Listeners = append(snapshot.Listeners, cloneListenerState(state))
		if inbound != "" && listenerName != inbound {
			continue
		}
		for _, user := range state.Users {
			snapshot.Users = append(snapshot.Users, UserRecord{
				User: *user, Revision: state.Revision, AppliedRevision: state.AppliedRevision,
			})
		}
	}
	sort.Slice(snapshot.Listeners, func(i, j int) bool {
		return snapshot.Listeners[i].Name < snapshot.Listeners[j].Name
	})
	sort.Slice(snapshot.Users, func(i, j int) bool {
		if snapshot.Users[i].User.Inbound != snapshot.Users[j].User.Inbound {
			return snapshot.Users[i].User.Inbound < snapshot.Users[j].User.Inbound
		}
		if snapshot.Users[i].User.Name != snapshot.Users[j].User.Name {
			return snapshot.Users[i].User.Name < snapshot.Users[j].User.Name
		}
		return snapshot.Users[i].User.ID < snapshot.Users[j].User.ID
	})
	return snapshot, nil
}

func (m *Manager) ListListeners() ([]ListenerState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return nil, ErrDisabled
	}
	m.syncTrafficLocked()
	listeners := make([]ListenerState, 0, len(m.config.ManagedListeners))
	for _, inbound := range m.config.ManagedListeners {
		state := m.store.Listeners[inbound]
		if state == nil {
			continue
		}
		cloned := cloneListenerState(state)
		listeners = append(listeners, cloned)
	}
	sort.Slice(listeners, func(i, j int) bool {
		return listeners[i].Name < listeners[j].Name
	})
	return listeners, nil
}

func (m *Manager) ListUsers(inbound string) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return nil, ErrDisabled
	}
	m.syncTrafficLocked()
	users := make([]User, 0)
	for _, listenerName := range m.config.ManagedListeners {
		if inbound != "" && listenerName != inbound {
			continue
		}
		state := m.store.Listeners[listenerName]
		if state == nil {
			continue
		}
		for _, user := range state.Users {
			users = append(users, *user)
		}
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].Inbound != users[j].Inbound {
			return users[i].Inbound < users[j].Inbound
		}
		if users[i].Name != users[j].Name {
			return users[i].Name < users[j].Name
		}
		return users[i].ID < users[j].ID
	})
	return users, nil
}

func (m *Manager) ListUserRecords(inbound string) ([]UserRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return nil, ErrDisabled
	}
	m.syncTrafficLocked()
	records := make([]UserRecord, 0)
	for _, listenerName := range m.config.ManagedListeners {
		if inbound != "" && listenerName != inbound {
			continue
		}
		state := m.store.Listeners[listenerName]
		if state == nil {
			continue
		}
		for _, user := range state.Users {
			records = append(records, UserRecord{User: *user, Revision: state.Revision, AppliedRevision: state.AppliedRevision})
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].User.Inbound != records[j].User.Inbound {
			return records[i].User.Inbound < records[j].User.Inbound
		}
		if records[i].User.Name != records[j].User.Name {
			return records[i].User.Name < records[j].User.Name
		}
		return records[i].User.ID < records[j].User.ID
	})
	return records, nil
}

func (m *Manager) GetUser(userID string) (User, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return User{}, 0, ErrDisabled
	}
	_, state, user := m.indexedUserLocked(userID)
	if user == nil {
		return User{}, 0, ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[user.Inbound]; !managed {
		return User{}, 0, ErrNotFound
	}
	m.syncUserTrafficLocked(user)
	return *user, state.Revision, nil
}

func (m *Manager) CreateUser(input CreateUserInput, expectedRevision int64) (User, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return User{}, 0, ErrDisabled
	}
	if _, managed := m.runtime.Load().managed[input.Inbound]; !managed {
		return User{}, 0, ErrNotFound
	}

	var createdID string
	state, err := m.mutateListenerLocked(input.Inbound, expectedRevision, func(candidate *Store, listenerState *ListenerState) error {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return fmt.Errorf("%w: user name is required", ErrInvalid)
		}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		user := &User{
			ID:                utils.NewUUIDV4().String(),
			Inbound:           input.Inbound,
			Protocol:          listenerState.Protocol,
			Name:              name,
			UUID:              input.UUID,
			Password:          input.Password,
			Flow:              input.Flow,
			Enabled:           enabled,
			TrafficGeneration: 1,
			CreatedAt:         time.Now().UnixMilli(),
			UpdatedAt:         time.Now().UnixMilli(),
		}
		if user.Protocol == "vless" && user.UUID == "" {
			user.UUID = utils.NewUUIDV4().String()
		}
		if user.Protocol == "anytls" && user.Password == "" {
			password, err := randomToken()
			if err != nil {
				return err
			}
			user.Password = password
		}
		token, err := randomToken()
		if err != nil {
			return err
		}
		candidate.Subscriptions[user.ID] = token
		listenerState.Users = append(listenerState.Users, user)
		createdID = user.ID
		return nil
	})
	if err != nil {
		return User{}, 0, err
	}
	_, _, created := m.indexedUserLocked(createdID)
	return *created, state.Revision, nil
}

func (m *Manager) UpdateUser(userID string, input UpdateUserInput, expectedRevision int64) (User, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return User{}, 0, ErrDisabled
	}
	inbound, _, existing := m.indexedUserLocked(userID)
	if existing == nil {
		return User{}, 0, ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[inbound]; !managed {
		return User{}, 0, ErrNotFound
	}

	state, err := m.mutateListenerLocked(inbound, expectedRevision, func(_ *Store, listenerState *ListenerState) error {
		user := findUserInListener(listenerState, userID)
		if input.Name != nil {
			user.Name = strings.TrimSpace(*input.Name)
		}
		if input.UUID != nil {
			user.UUID = *input.UUID
		}
		if input.Password != nil {
			user.Password = *input.Password
		}
		if input.Flow != nil {
			user.Flow = *input.Flow
		}
		if input.Enabled != nil {
			user.Enabled = *input.Enabled
		}
		user.UpdatedAt = time.Now().UnixMilli()
		return nil
	})
	if err != nil {
		return User{}, 0, err
	}
	_, _, updated := m.indexedUserLocked(userID)
	return *updated, state.Revision, nil
}

func (m *Manager) DeleteUser(userID string, expectedRevision int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return 0, ErrDisabled
	}
	inbound, _, existing := m.indexedUserLocked(userID)
	if existing == nil {
		return 0, ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[inbound]; !managed {
		return 0, ErrNotFound
	}
	state, err := m.mutateListenerLocked(inbound, expectedRevision, func(candidate *Store, listenerState *ListenerState) error {
		for i, user := range listenerState.Users {
			if user.ID == userID {
				listenerState.Users = append(listenerState.Users[:i], listenerState.Users[i+1:]...)
				delete(candidate.Subscriptions, userID)
				return nil
			}
		}
		return ErrNotFound
	})
	if err != nil {
		return 0, err
	}
	return state.Revision, nil
}

func (m *Manager) ResetTraffic(userID string, expectedRevision int64) (User, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return User{}, 0, ErrDisabled
	}
	inbound, _, existing := m.indexedUserLocked(userID)
	if existing == nil {
		return User{}, 0, ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[inbound]; !managed {
		return User{}, 0, ErrNotFound
	}
	state, err := m.mutateListenerLocked(inbound, expectedRevision, func(_ *Store, listenerState *ListenerState) error {
		user := findUserInListener(listenerState, userID)
		user.UploadBytes = 0
		user.DownloadBytes = 0
		user.TrafficGeneration++
		user.UpdatedAt = time.Now().UnixMilli()
		return nil
	})
	if err != nil {
		return User{}, 0, err
	}
	_, _, updated := m.indexedUserLocked(userID)
	return *updated, state.Revision, nil
}

func (m *Manager) RotateSubscription(userID string, expectedRevision int64) (string, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return "", 0, ErrDisabled
	}
	inbound, _, existing := m.indexedUserLocked(userID)
	if existing == nil {
		return "", 0, ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[inbound]; !managed {
		return "", 0, ErrNotFound
	}
	var token string
	state, err := m.mutateListenerLocked(inbound, expectedRevision, func(candidate *Store, _ *ListenerState) error {
		var err error
		token, err = randomToken()
		if err != nil {
			return err
		}
		candidate.Subscriptions[userID] = token
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return token, state.Revision, nil
}

func (m *Manager) SubscriptionUser(token string) (User, error) {
	runtime := m.runtime.Load()
	userID, exists := runtime.subscriptions[token]
	if !exists || token == "" {
		return User{}, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil || m.store.Subscriptions[userID] != token {
		return User{}, ErrNotFound
	}
	_, _, user := m.indexedUserLocked(userID)
	if user == nil || !user.Enabled {
		return User{}, ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[user.Inbound]; !managed {
		return User{}, ErrNotFound
	}
	return *user, nil
}

func (m *Manager) SubscriptionToken(userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return "", ErrDisabled
	}
	_, _, user := m.indexedUserLocked(userID)
	if user == nil {
		return "", ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[user.Inbound]; !managed {
		return "", ErrNotFound
	}
	return m.store.Subscriptions[userID], nil
}

func (m *Manager) mutateListenerLocked(inbound string, expectedRevision int64, mutate func(*Store, *ListenerState) error) (*ListenerState, error) {
	if expectedRevision <= 0 {
		return nil, fmt.Errorf("%w: revision must be a positive integer", ErrInvalid)
	}
	current := m.store.Listeners[inbound]
	if current == nil {
		return nil, ErrNotFound
	}
	if current.Revision != expectedRevision {
		return nil, fmt.Errorf("%w: expected %d, current %d", ErrConflict, expectedRevision, current.Revision)
	}
	m.trafficMu.Lock()
	defer m.trafficMu.Unlock()

	wasDirty := m.dirty.Swap(false)
	committed := false
	defer func() {
		if !committed && wasDirty {
			m.dirty.Store(true)
		}
	}()
	m.syncTrafficLocked()
	candidate := cloneStoreForListener(m.store, inbound)
	candidateState := candidate.Listeners[inbound]
	if err := mutate(candidate, candidateState); err != nil {
		return nil, err
	}
	candidateState.Revision = nextRevision(current.Revision)
	candidateState.AppliedRevision = candidateState.Revision
	if err := validateStore(candidate); err != nil {
		return nil, err
	}

	change := listenerChange{
		name:     inbound,
		instance: m.instances[inbound],
		before:   activeManagedUsers(current),
		after:    activeManagedUsers(candidateState),
	}
	changes := []listenerChange{change}
	if reflect.DeepEqual(change.before, change.after) {
		changes = nil
	}
	if err := applyChanges(changes); err != nil {
		if errors.Is(err, errRollbackFailed) {
			return nil, errors.Join(err, m.failClosedStateLocked(nil))
		}
		return nil, err
	}
	if err := m.persistStore(m.storePath, candidate); err != nil {
		rollbackErr := rollbackChanges(changes)
		if rollbackErr != nil {
			rollbackErr = errors.Join(rollbackErr, m.failClosedStateLocked(nil))
		}
		return nil, errors.Join(fmt.Errorf("persist Aster mutation: %w", err), rollbackErr)
	}

	m.store = candidate
	m.reindexListenerLocked(current, candidateState)
	m.publishLocked()
	committed = true
	return candidateState, nil
}

func buildUserIndex(store *Store) map[string]userLocation {
	index := make(map[string]userLocation)
	for inbound, state := range store.Listeners {
		for position, user := range state.Users {
			index[user.ID] = userLocation{inbound: inbound, index: position}
		}
	}
	return index
}

func (m *Manager) indexedUserLocked(userID string) (string, *ListenerState, *User) {
	location, exists := m.userIndex[userID]
	if !exists {
		return "", nil, nil
	}
	state := m.store.Listeners[location.inbound]
	if state == nil || location.index < 0 || location.index >= len(state.Users) {
		return "", nil, nil
	}
	user := state.Users[location.index]
	if user == nil || user.ID != userID {
		return "", nil, nil
	}
	return location.inbound, state, user
}

func (m *Manager) reindexListenerLocked(previous, current *ListenerState) {
	for _, user := range previous.Users {
		delete(m.userIndex, user.ID)
	}
	for position, user := range current.Users {
		m.userIndex[user.ID] = userLocation{inbound: current.Name, index: position}
	}
}

func findUser(store *Store, userID string) (string, *ListenerState, *User) {
	for inbound, state := range store.Listeners {
		if user := findUserInListener(state, userID); user != nil {
			return inbound, state, user
		}
	}
	return "", nil, nil
}

func findUserInListener(state *ListenerState, userID string) *User {
	for _, user := range state.Users {
		if user.ID == userID {
			return user
		}
	}
	return nil
}

func cloneListenerState(state *ListenerState) ListenerState {
	cloned := *state
	cloned.Users = make([]*User, len(state.Users))
	for i, user := range state.Users {
		clonedUser := *user
		cloned.Users[i] = &clonedUser
	}
	return cloned
}
