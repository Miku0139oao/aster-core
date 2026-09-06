package statistic

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/utils"

	"github.com/stretchr/testify/require"
)

func newScavengeTracker() *trackerManagerTest {
	return &trackerManagerTest{
		id:   "scavenge",
		info: &TrackerInfo{UUID: utils.NewUUIDV4()},
	}
}

func TestIdleScavengeDefaultOff(t *testing.T) {
	manager := &Manager{}
	var calls atomic.Int32
	manager.freeOSMemory = func() { calls.Add(1) }

	tracker := newScavengeTracker()
	manager.Join(tracker)
	manager.Leave(tracker)
	require.False(t, manager.maybeIdleScavenge(time.Now().Add(time.Hour)))
	require.Zero(t, calls.Load())
}

func TestIdleScavengeSkipsUntilAConnectionHasClosed(t *testing.T) {
	manager := &Manager{}
	var calls atomic.Int32
	manager.freeOSMemory = func() { calls.Add(1) }
	manager.SetIdleMemoryScavenge(true, time.Nanosecond)

	require.False(t, manager.maybeIdleScavenge(time.Now().Add(time.Hour)))
	require.Zero(t, calls.Load())
}

func TestIdleScavengeSkipsWhileConnectionsRemain(t *testing.T) {
	manager := &Manager{}
	var calls atomic.Int32
	manager.freeOSMemory = func() { calls.Add(1) }
	manager.SetIdleMemoryScavenge(true, time.Nanosecond)

	first := newScavengeTracker()
	second := newScavengeTracker()
	manager.Join(first)
	manager.Join(second)
	manager.Leave(first)
	require.False(t, manager.maybeIdleScavenge(time.Now().Add(time.Hour)))
	require.Equal(t, 1, manager.ConnectionCount())
	require.Zero(t, calls.Load())
}

func TestIdleScavengeWaitsForIdleDuration(t *testing.T) {
	manager := &Manager{}
	var calls atomic.Int32
	manager.freeOSMemory = func() { calls.Add(1) }
	manager.SetIdleMemoryScavenge(true, time.Hour)

	tracker := newScavengeTracker()
	manager.Join(tracker)
	manager.Leave(tracker)
	require.False(t, manager.maybeIdleScavenge(time.Now()))
	require.Zero(t, calls.Load())
}

func TestIdleScavengeRunsOnceThenRearmsAfterTraffic(t *testing.T) {
	manager := &Manager{}
	var calls atomic.Int32
	manager.freeOSMemory = func() { calls.Add(1) }
	manager.SetIdleMemoryScavenge(true, time.Nanosecond)

	tracker := newScavengeTracker()
	manager.Join(tracker)
	manager.Leave(tracker)
	require.True(t, manager.maybeIdleScavenge(time.Now().Add(time.Second)))
	require.False(t, manager.maybeIdleScavenge(time.Now().Add(time.Hour)))
	require.EqualValues(t, 1, calls.Load())

	next := newScavengeTracker()
	manager.Join(next)
	manager.Leave(next)
	require.True(t, manager.maybeIdleScavenge(time.Now().Add(time.Second)))
	require.EqualValues(t, 2, calls.Load())
}

func TestIdleScavengeZeroIdleUsesDefaultDuration(t *testing.T) {
	manager := &Manager{}
	manager.SetIdleMemoryScavenge(true, 0)
	require.EqualValues(t, int64(DefaultIdleMemoryScavengeIdle), manager.scavengeIdleNs.Load())
}

func TestIdleScavengeReleasesHeapToOS(t *testing.T) {
	manager := &Manager{}
	manager.SetIdleMemoryScavenge(true, time.Nanosecond)

	blobs := make([][]byte, 32)
	for i := range blobs {
		b := make([]byte, 1<<20)
		for j := 0; j < len(b); j += 4096 {
			b[j] = byte(i + 1)
		}
		blobs[i] = b
	}
	runtime.KeepAlive(blobs)
	blobs = nil

	tracker := newScavengeTracker()
	manager.Join(tracker)
	manager.Leave(tracker)

	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	require.True(t, manager.maybeIdleScavenge(time.Now().Add(time.Second)))
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	t.Logf("HeapInuse %d KiB -> %d KiB", before.HeapInuse>>10, after.HeapInuse>>10)
	t.Logf("HeapReleased %d KiB -> %d KiB", before.HeapReleased>>10, after.HeapReleased>>10)
	t.Logf("HeapIdle %d KiB -> %d KiB", before.HeapIdle>>10, after.HeapIdle>>10)

	if after.HeapInuse >= before.HeapInuse && after.HeapReleased <= before.HeapReleased {
		t.Fatalf("debug.FreeOSMemory did not shrink in-use heap or release idle spans")
	}
}
