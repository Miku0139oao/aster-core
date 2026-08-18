package aster

import (
	"fmt"
	"strconv"
	"testing"
)

func benchmarkAsterStore(userCount int) (*Store, string) {
	store := newStore()
	state := &ListenerState{
		ID: "benchmark-listener", Name: "benchmark", Protocol: "vless",
		Revision: 1, AppliedRevision: 1, Users: make([]*User, 0, userCount),
	}
	targetID := ""
	for i := 0; i < userCount; i++ {
		userID := fmt.Sprintf("user-%d", i)
		state.Users = append(state.Users, &User{
			ID: userID, Inbound: state.Name, Protocol: state.Protocol, Name: userID,
			UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197", Enabled: true,
			TrafficGeneration: 1, CreatedAt: 1, UpdatedAt: 1,
		})
		store.Subscriptions[userID] = fmt.Sprintf("token-%d", i)
		targetID = userID
	}
	store.Listeners[state.Name] = state
	return store, targetID
}

func BenchmarkManagerGetUser(b *testing.B) {
	for _, userCount := range []int{100, 1_000, 10_000} {
		b.Run("users_"+strconv.Itoa(userCount), func(b *testing.B) {
			store, targetID := benchmarkAsterStore(userCount)
			manager := NewManager()
			manager.config = &Config{
				Secret: "0123456789abcdef0123456789abcdef", ManagedListeners: []string{"benchmark"},
			}
			manager.store = store
			manager.userIndex = buildUserIndex(store)
			manager.runtime.Store(buildRuntimeState(manager.config, "", store, newRuntimeState()))

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := manager.GetUser(targetID); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// The overview endpoint is polled by dashboards and only needs counts, so it must
// not clone every user the way a full snapshot does.
func BenchmarkManagerOverview(b *testing.B) {
	for _, userCount := range []int{100, 1_000, 10_000} {
		store, _ := benchmarkAsterStore(userCount)
		manager := NewManager()
		manager.config = &Config{
			Secret: "0123456789abcdef0123456789abcdef", ManagedListeners: []string{"benchmark"},
		}
		manager.store = store
		manager.userIndex = buildUserIndex(store)
		manager.runtime.Store(buildRuntimeState(manager.config, "", store, newRuntimeState()))

		b.Run("summary/users_"+strconv.Itoa(userCount), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := manager.Summary(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("snapshot/users_"+strconv.Itoa(userCount), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := manager.ManagementSnapshot(""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCloneStoreForListener(b *testing.B) {
	for _, userCount := range []int{100, 1_000, 10_000} {
		b.Run("users_"+strconv.Itoa(userCount), func(b *testing.B) {
			store, _ := benchmarkAsterStore(userCount)
			bulk := store.Listeners["benchmark"]
			bulk.Name = "bulk"
			store.Listeners["bulk"] = bulk
			store.Listeners["benchmark"] = &ListenerState{
				ID: "target-listener", Name: "benchmark", Protocol: "vless",
				Revision: 1, AppliedRevision: 1,
				Users: []*User{{
					ID: "target-user", Inbound: "benchmark", Protocol: "vless", Name: "target-user",
					UUID: "6d27a52f-4539-4ac1-9bd4-b8e05e53c197", Enabled: true,
					TrafficGeneration: 1, CreatedAt: 1, UpdatedAt: 1,
				}},
			}
			store.Subscriptions["target-user"] = "target-token"
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = cloneStoreForListener(store, "benchmark")
			}
		})
	}
}
