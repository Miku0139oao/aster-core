package process

import (
	"sync"
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
	pidMu    sync.RWMutex
	pidCache = make(map[uint32]pidVal, 64)
)

func lookupPidPath(pid uint32) (string, bool) {
	now := time.Now().UnixNano()
	pidMu.RLock()
	v, ok := pidCache[pid]
	pidMu.RUnlock()
	if !ok || v.exp <= now {
		return "", false
	}
	return v.name, true
}

func storePidPath(pid uint32, name string) {
	v := pidVal{name: name, exp: time.Now().Add(pidCacheTTL).UnixNano()}
	pidMu.Lock()
	if len(pidCache) >= pidCacheMax {
		now := time.Now().UnixNano()
		for id, e := range pidCache {
			if e.exp <= now {
				delete(pidCache, id)
			}
		}
		if len(pidCache) >= pidCacheMax {
			n := 0
			for id := range pidCache {
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

func resetCachesForTest() {
	pidMu.Lock()
	pidCache = make(map[uint32]pidVal, 64)
	pidMu.Unlock()
}
