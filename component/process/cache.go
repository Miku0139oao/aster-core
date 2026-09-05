package process

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	pidCacheTTL = 5 * time.Second
	pidCacheMax = 512
)

type pidVal struct {
	name string
	exp  int64
}

var (
	pidMu     sync.RWMutex
	pidCache  = make(map[uint32]pidVal, 64)
	lastSweep int64
)

func lookupPidPath(pid uint32) (string, bool) {
	now := time.Now().UnixNano()
	maybeSweepExpired(now)

	pidMu.RLock()
	v, ok := pidCache[pid]
	pidMu.RUnlock()
	if !ok {
		return "", false
	}
	if v.exp > now {
		return v.name, true
	}

	pidMu.Lock()
	cur, exists := pidCache[pid]
	if !exists {
		pidMu.Unlock()
		return "", false
	}
	if cur.exp <= now {
		delete(pidCache, pid)
		pidMu.Unlock()
		return "", false
	}
	name := cur.name
	pidMu.Unlock()
	return name, true
}

func storePidPath(pid uint32, name string) {
	now := time.Now().UnixNano()
	v := pidVal{name: name, exp: now + int64(pidCacheTTL)}
	maybeSweepExpired(now)
	pidMu.Lock()
	if len(pidCache) >= pidCacheMax {
		for id, e := range pidCache {
			if e.exp <= now {
				delete(pidCache, id)
			}
		}
		if len(pidCache) >= pidCacheMax {
			n := 0
			for id := range pidCache {
				if id == pid {
					continue
				}
				delete(pidCache, id)
				n++
				if n >= pidCacheMax/2 {
					break
				}
			}
		}
	}
	pidCache[pid] = v
	pidMu.Unlock()
}

func maybeSweepExpired(now int64) {
	last := atomic.LoadInt64(&lastSweep)
	if now-last < int64(pidCacheTTL) {
		return
	}
	if !atomic.CompareAndSwapInt64(&lastSweep, last, now) {
		return
	}
	pidMu.Lock()
	for id, e := range pidCache {
		if e.exp <= now {
			delete(pidCache, id)
		}
	}
	pidMu.Unlock()
}

func resetCachesForTest() {
	pidMu.Lock()
	pidCache = make(map[uint32]pidVal, 64)
	atomic.StoreInt64(&lastSweep, 0)
	pidMu.Unlock()
}
