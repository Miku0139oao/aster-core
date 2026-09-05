package process

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestPidCacheLookupDeletesExpiredKey(t *testing.T) {
	resetCachesForTest()
	defer resetCachesForTest()

	storePidPath(1, "old.exe")
	pidMu.Lock()
	v := pidCache[1]
	v.exp = time.Now().UnixNano() - 1
	pidCache[1] = v
	pidMu.Unlock()
	atomic.StoreInt64(&lastSweep, time.Now().UnixNano())

	if name, ok := lookupPidPath(1); ok || name != "" {
		t.Fatalf("expired lookup hit %q ok=%v", name, ok)
	}
	pidMu.RLock()
	_, exists := pidCache[1]
	pidMu.RUnlock()
	if exists {
		t.Fatal("expired key retained after lookup")
	}
}

func TestPidCacheLookupDoesNotDeleteRefreshedKey(t *testing.T) {
	resetCachesForTest()
	defer resetCachesForTest()

	storePidPath(1, "old.exe")
	pidMu.Lock()
	v := pidCache[1]
	v.exp = time.Now().UnixNano() - 1
	pidCache[1] = v
	pidMu.Unlock()
	atomic.StoreInt64(&lastSweep, time.Now().UnixNano())

	storePidPath(1, "new.exe")
	name, ok := lookupPidPath(1)
	if !ok || name != "new.exe" {
		t.Fatalf("refreshed lookup = %q ok=%v", name, ok)
	}
	pidMu.RLock()
	cur, exists := pidCache[1]
	pidMu.RUnlock()
	if !exists || cur.name != "new.exe" {
		t.Fatalf("refreshed key missing: exists=%v name=%q", exists, cur.name)
	}
}

func TestPidCacheHitOnlySweepPreservesLive(t *testing.T) {
	resetCachesForTest()
	defer resetCachesForTest()

	storePidPath(1, "live.exe")
	storePidPath(2, "dead.exe")
	storePidPath(3, "dead2.exe")

	now := time.Now().UnixNano()
	pidMu.Lock()
	for id, v := range pidCache {
		if id != 1 {
			v.exp = now - 1
			pidCache[id] = v
		}
	}
	pidMu.Unlock()
	atomic.StoreInt64(&lastSweep, 0)

	name, ok := lookupPidPath(1)
	if !ok || name != "live.exe" {
		t.Fatalf("live hit = %q ok=%v", name, ok)
	}

	pidMu.RLock()
	defer pidMu.RUnlock()
	if _, ok := pidCache[2]; ok {
		t.Fatal("expired pid 2 retained after hit-only sweep")
	}
	if _, ok := pidCache[3]; ok {
		t.Fatal("expired pid 3 retained after hit-only sweep")
	}
	live, ok := pidCache[1]
	if !ok || live.name != "live.exe" {
		t.Fatalf("live entry missing: ok=%v name=%q", ok, live.name)
	}
}

func TestPidCacheHitNoAlloc(t *testing.T) {
	resetCachesForTest()
	defer resetCachesForTest()

	storePidPath(7, "warm.exe")
	atomic.StoreInt64(&lastSweep, time.Now().UnixNano())
	if name, ok := lookupPidPath(7); !ok || name != "warm.exe" {
		t.Fatalf("warmup = %q ok=%v", name, ok)
	}

	missed := false
	allocs := testing.AllocsPerRun(1000, func() {
		if _, ok := lookupPidPath(7); !ok {
			missed = true
		}
	})
	if missed {
		t.Fatal("hit missed")
	}
	if allocs != 0 {
		t.Fatalf("hit allocs = %v, want 0", allocs)
	}
}
