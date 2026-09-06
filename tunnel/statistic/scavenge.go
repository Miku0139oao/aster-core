package statistic

import (
	"runtime/debug"
	"time"

	"github.com/Miku0139oao/aster-core/log"
)

// DefaultIdleMemoryScavengeIdle is how long all trackers must stay gone
// before an enabled scavenger calls debug.FreeOSMemory. It is longer than
// the default UDP NAT timeout so a quiet association can expire first.
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

// maybeIdleScavenge runs FreeOSMemory on the caller. Tests use this so heap
// counters are observed after the GC finishes.
func (m *Manager) maybeIdleScavenge(now time.Time) bool {
	if !m.tryStartIdleScavenge(now) {
		return false
	}
	m.freeUnusedMemory()
	return true
}

// maybeIdleScavengeAsync starts FreeOSMemory on another goroutine so the
// manager ticker (blip + zero-byte reap) is not stuck in a STW pause.
func (m *Manager) maybeIdleScavengeAsync(now time.Time) {
	if !m.tryStartIdleScavenge(now) {
		return
	}
	go m.freeUnusedMemory()
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

// freeUnusedMemory is memory-safe: debug.FreeOSMemory only collects
// unreachable objects. Live connections that Join after the CAS stay
// reachable and are not freed. The cost is a GC STW plus returning idle
// spans; that is why production calls this off the ticker goroutine.
func (m *Manager) freeUnusedMemory() {
	free := m.freeOSMemory
	if free == nil {
		free = debug.FreeOSMemory
	}
	free()
	log.Infoln("[Memory] idle scavenge returned unused heap to the OS")
}
