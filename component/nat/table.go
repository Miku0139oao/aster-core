package nat

import (
	"errors"
	"net"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/common/xsync"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/constant/features"
)

type Table struct {
	mapping    xsync.Map[C.UDPNatKey, *entry]
	maxEntries int64
	size       atomic.Int64
	rejected   atomic.Uint64
}

type entry struct {
	PacketSender    C.PacketSender
	LocalUDPConnMap xsync.Map[string, *net.UDPConn]
	LocalPendingMap xsync.Map[string, *localConnPromise]
}

type localConnPromise struct {
	done chan struct{}
	conn *net.UDPConn
	err  error
}

func (t *Table) GetOrCreate(key C.UDPNatKey, maker func() C.PacketSender) (C.PacketSender, bool, bool) {
	if item, loaded := t.mapping.Load(key); loaded {
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

func (t *Table) Delete(key C.UDPNatKey) {
	if _, loaded := t.mapping.LoadAndDelete(key); loaded {
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
