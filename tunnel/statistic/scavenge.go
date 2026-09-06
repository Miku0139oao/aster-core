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

// maybeIdleScavenge returns unused heap to the OS once after every busy
// period. It does not run on a timer while connections exist, does not
// change GOGC, and does not fire at process start (idleSince stays 0 until
// a tracker has joined and then left).
//
// HeapInuse < RSS/2 is not used as a gate. Unreachable objects from a
// closed burst keep HeapInuse high until a GC runs, which is exactly the
// case FreeOSMemory must handle.
func (m *Manager) maybeIdleScavenge(now time.Time) bool {
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
	if !m.scavenged.CompareAndSwap(false, true) {
		return false
	}
	free := m.freeOSMemory
	if free == nil {
		free = debug.FreeOSMemory
	}
	free()
	log.Infoln("[Memory] idle scavenge returned unused heap to the OS")
	return true
}
