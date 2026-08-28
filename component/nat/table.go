package nat

import (
	"errors"
	"net"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/common/xsync"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/constant/features"
)

const natHitSlots = 64

type Table struct {
	mapping    xsync.Map[C.UDPNatKey, *entry]
	hits       [natHitSlots]atomic.Pointer[lastHit]
	maxEntries int64
	size       atomic.Int64
	rejected   atomic.Uint64
}

type lastHit struct {
	key   C.UDPNatKey
	entry *entry
}

type entry struct {
	PacketSender    C.PacketSender
	dead            atomic.Bool
	LocalUDPConnMap xsync.Map[string, *net.UDPConn]
	LocalPendingMap xsync.Map[string, *localConnPromise]
}

type localConnPromise struct {
	done chan struct{}
	conn *net.UDPConn
	err  error
}

func natCacheSlot(key C.UDPNatKey) uint {
	// Client source ports are the NAT key's hot discriminator and need no extra
	// hashing. Full-key equality still rejects a slot occupant from another flow.
	if key.AddrPort.IsValid() {
		return uint(key.AddrPort.Port()) & (natHitSlots - 1)
	}
	if n := len(key.Raw); n > 0 {
		return uint(key.Raw[0]) & (natHitSlots - 1)
	}
	return uint(key.IngressType) & (natHitSlots - 1)
}

func (t *Table) GetOrCreate(key C.UDPNatKey, maker func() C.PacketSender) (C.PacketSender, bool, bool) {
	slot := natCacheSlot(key)
	if hit := t.hits[slot].Load(); hit != nil && hit.key == key && !hit.entry.dead.Load() {
		return hit.entry.PacketSender, true, true
	}
	if item, loaded := t.mapping.Load(key); loaded {
		t.remember(slot, key, item)
		return item.PacketSender, true, true
	}
	for {
		count := t.size.Load()
		if count >= t.maxEntries {
			t.rejected.Add(1)
			return nil, false, false
		}
		if t.size.CompareAndSwap(count, count+1) {
			break
		}
	}
	item, loaded := t.mapping.LoadOrStoreFn(key, func() *entry {
		return &entry{PacketSender: maker()}
	})
	if loaded {
		t.size.Add(-1)
	}
	return item.PacketSender, loaded, true
}

func (t *Table) remember(slot uint, key C.UDPNatKey, item *entry) {
	if item == nil || item.dead.Load() {
		return
	}
	hit := t.hits[slot].Load()
	if hit != nil && !hit.entry.dead.Load() {
		// Sticky per slot: a live occupant is left in place so interleaved
		// flows cannot allocate a lastHit on every packet.
		return
	}
	t.hits[slot].Store(&lastHit{key: key, entry: item})
}

func (t *Table) Delete(key C.UDPNatKey) {
	if item, loaded := t.mapping.LoadAndDelete(key); loaded {
		item.dead.Store(true)
		t.size.Add(-1)
	}
}

func (t *Table) Size() int64       { return t.size.Load() }
func (t *Table) MaxEntries() int64 { return t.maxEntries }
func (t *Table) Rejected() uint64  { return t.rejected.Load() }

// GetOrCreateLocalConn uses a close-once promise instead of sync.Cond. A
// waiter cannot miss completion if the creator finishes between lookup and
// waiting, and all waiters observe the same connection or error.
func (t *Table) GetOrCreateLocalConn(flow C.UDPNatKey, remote string, maker func() (*net.UDPConn, error)) (*net.UDPConn, error) {
	entry, exists := t.getEntry(flow)
	if !exists {
		return nil, errors.New("UDP NAT entry no longer exists")
	}
	if conn, loaded := entry.LocalUDPConnMap.Load(remote); loaded {
		return conn, nil
	}
	promise, loaded := entry.LocalPendingMap.LoadOrStoreFn(remote, func() *localConnPromise {
		return &localConnPromise{done: make(chan struct{})}
	})
	if loaded {
		<-promise.done
		return promise.conn, promise.err
	}
	// A caller can miss the connection in its first lookup, then reach this
	// point after the preceding promise published the connection and removed
	// itself. Recheck before invoking the maker.
	if conn, loaded := entry.LocalUDPConnMap.Load(remote); loaded {
		promise.conn = conn
		close(promise.done)
		entry.LocalPendingMap.Delete(remote)
		return conn, nil
	}

	conn, err := maker()
	if err == nil {
		if current, active := t.getEntry(flow); !active || current != entry {
			_ = conn.Close()
			conn = nil
			err = errors.New("UDP NAT entry closed while creating local connection")
		} else {
			entry.LocalUDPConnMap.Store(remote, conn)
		}
	}
	promise.conn, promise.err = conn, err
	close(promise.done)
	entry.LocalPendingMap.Delete(remote)
	return conn, err
}

func (t *Table) RangeForLocalConn(flow C.UDPNatKey, f func(key string, value *net.UDPConn) bool) {
	entry, exist := t.getEntry(flow)
	if !exist {
		return
	}
	entry.LocalUDPConnMap.Range(f)
}

func (t *Table) getEntry(key C.UDPNatKey) (*entry, bool) {
	return t.mapping.Load(key)
}

// New returns a bounded UDP NAT table. The optional limit is intended for
// tests; production uses a lower default under with_low_memory.
func New(limit ...int) *Table {
	maxEntries := int64(8192)
	if features.WithLowMemory {
		maxEntries = 2048
	}
	if len(limit) > 0 && limit[0] > 0 {
		maxEntries = int64(limit[0])
	}
	return &Table{maxEntries: maxEntries}
}
