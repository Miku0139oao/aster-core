package statistic

import (
	"runtime"
	"time"

	"github.com/Miku0139oao/aster-core/log"
)

// DefaultIdleMemoryScavengeIdle is how long all trackers must stay gone
// before an enabled scavenger runs a GC. It is longer than the default UDP
// NAT timeout so a quiet association can expire first.
const DefaultIdleMemoryScavengeIdle = 5 * time.Minute

// SetIdleMemoryScavenge arms or disarms the idle scavenger. idle is ignored
// when enabled is false. A non-positive idle uses DefaultIdleMemoryScavengeIdle.
// The scavenger is off by default: it is an explicit GC intervention.
func (m *Manager) SetIdleMemoryScavenge(enabled bool, idle time.Duration) {
	m.scavengeEnabled.Store(enabled)
	if !enabled {
		return
	}
	if idle <= 0 {
		idle = DefaultIdleMemoryScavengeIdle
	}
	m.scavengeIdleNs.Store(int64(idle))
}

func (m *Manager) markBusy() {
	m.idleSinceNs.Store(0)
	m.scavenged.Store(false)
}

func (m *Manager) markIdleIfEmpty() {
	if m.connections.Size() != 0 {
		return
	}
	m.idleSinceNs.CompareAndSwap(0, time.Now().UnixNano())
}

// maybeIdleScavenge runs the idle GC on the caller. Tests use this so heap
// counters are observed after the GC finishes.
func (m *Manager) maybeIdleScavenge(now time.Time) bool {
	if !m.tryStartIdleScavenge(now) {
		return false
	}
	m.idleCollect()
	return true
}

// maybeIdleScavengeAsync starts the idle GC on another goroutine so the
// manager ticker (blip + zero-byte reap) is not the goroutine in STW.
func (m *Manager) maybeIdleScavengeAsync(now time.Time) {
	if !m.tryStartIdleScavenge(now) {
		return
	}
	go m.idleCollect()
}

func (m *Manager) tryStartIdleScavenge(now time.Time) bool {
	if !m.scavengeEnabled.Load() {
		return false
	}
	if m.connections.Size() > 0 {
		return false
	}
	idleSince := m.idleSinceNs.Load()
	if idleSince == 0 {
		return false
	}
	idle := time.Duration(m.scavengeIdleNs.Load())
	if idle <= 0 {
		idle = DefaultIdleMemoryScavengeIdle
	}
	if now.UnixNano()-idleSince < int64(idle) {
		return false
	}
	return m.scavenged.CompareAndSwap(false, true)
}

// idleCollect runs a concurrent GC so dead objects become idle spans. It
// does not call debug.FreeOSMemory: that walks idle pages under the heap
// lock and can stall new allocations for milliseconds while memory is
// returned. Go's background scavenger then returns those spans without
// an extra forced return-to-OS pass.
//
// A short stop-the-world pause is still unavoidable (Go has no zero-STW
// GC). Live connections that Join after the CAS stay reachable.
func (m *Manager) idleCollect() {
	collect := m.collect
	if collect == nil {
		collect = runtime.GC
	}
	collect()
	log.Infoln("[Memory] idle GC finished; unused heap returns to the OS in the background")
}
