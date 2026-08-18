package statistic

import (
	"os"
	"sync"
	stdatomic "sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/common/atomic"
	"github.com/Miku0139oao/aster-core/common/xsync"
	"github.com/Miku0139oao/aster-core/component/memory"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/gofrs/uuid/v5"
)

var DefaultManager *Manager

func init() {
	DefaultManager = &Manager{
		uploadTemp:    atomic.NewInt64(0),
		downloadTemp:  atomic.NewInt64(0),
		uploadBlip:    atomic.NewInt64(0),
		downloadBlip:  atomic.NewInt64(0),
		uploadTotal:   atomic.NewInt64(0),
		downloadTotal: atomic.NewInt64(0),
		pid:           int32(os.Getpid()),
	}

	go DefaultManager.handle()
}

// idleZeroByteTCP is how long a TCP tracker may sit with no payload before Aster
// closes it. UDP (including ePDG 500/4500) is never reaped.
//
// Trackers are registered only after outbound DialContext succeeds, so the dial
// window (C.DefaultTCPTimeout, 5s) ends before Start is recorded. Keeping this
// at 30s avoids reaping slow-but-live post-dial handshakes that have not yet
// moved payload bytes through the tracker.
var idleZeroByteTCP = 30 * time.Second

type Manager struct {
	connections   xsync.Map[uuid.UUID, Tracker]
	reapOnce      xsync.Map[uuid.UUID, *sync.Once]
	uploadTemp    atomic.Int64
	downloadTemp  atomic.Int64
	uploadBlip    atomic.Int64
	downloadBlip  atomic.Int64
	uploadTotal   atomic.Int64
	downloadTotal atomic.Int64
	pid           int32
	memory        stdatomic.Uint64
	observer      stdatomic.Pointer[trafficObserverHolder]
	principalMu   sync.RWMutex
	principals    map[Principal]int
}

type Principal struct {
	Inbound string
	UserID  string
}

type TrafficObserver interface {
	RecordTraffic(inbound, userID string, upload, download int64)
}

type trafficObserverHolder struct {
	observer TrafficObserver
}

func (m *Manager) Join(c Tracker) {
	if _, loaded := m.connections.LoadOrStore(c.Info().UUID, c); loaded {
		return
	}
	m.updatePrincipalConnections(c, 1)
}

func (m *Manager) Leave(c Tracker) {
	info := c.Info()
	if info == nil {
		return
	}
	stored, loaded := m.connections.LoadAndDelete(info.UUID)
	if !loaded {
		return
	}
	m.reapOnce.Delete(info.UUID)
	m.updatePrincipalConnections(stored, -1)
}

func (m *Manager) updatePrincipalConnections(c Tracker, delta int) {
	info := c.Info()
	if info == nil || info.Metadata == nil || info.Metadata.InName == "" || info.Metadata.InUser == "" {
		return
	}
	key := Principal{Inbound: info.Metadata.InName, UserID: info.Metadata.InUser}
	m.principalMu.Lock()
	if m.principals == nil {
		m.principals = make(map[Principal]int)
	}
	next := m.principals[key] + delta
	if next <= 0 {
		delete(m.principals, key)
	} else {
		m.principals[key] = next
	}
	m.principalMu.Unlock()
}

func (m *Manager) ConnectionCount() int {
	return m.connections.Size()
}

func (m *Manager) ActiveConnectionsByPrincipal() map[Principal]int {
	m.principalMu.RLock()
	connections := make(map[Principal]int, len(m.principals))
	for principal, count := range m.principals {
		connections[principal] = count
	}
	m.principalMu.RUnlock()
	return connections
}

func (m *Manager) Get(id string) (c Tracker) {
	parsedID, err := uuid.FromString(id)
	if err != nil {
		return nil
	}
	if value, ok := m.connections.Load(parsedID); ok {
		c = value
	}
	return
}

func (m *Manager) Range(f func(c Tracker) bool) {
	m.connections.Range(func(key uuid.UUID, value Tracker) bool {
		return f(value)
	})
}

func (m *Manager) PushUploaded(size int64) {
	m.uploadTemp.Add(size)
	m.uploadTotal.Add(size)
}

func (m *Manager) PushDownloaded(size int64) {
	m.downloadTemp.Add(size)
	m.downloadTotal.Add(size)
}

func (m *Manager) PushUploadedFor(inbound, userID string, size int64) {
	m.PushUploaded(size)
	if userID == "" {
		return
	}
	if observer := m.observer.Load(); observer != nil {
		observer.observer.RecordTraffic(inbound, userID, size, 0)
	}
}

func (m *Manager) PushDownloadedFor(inbound, userID string, size int64) {
	m.PushDownloaded(size)
	if userID == "" {
		return
	}
	if observer := m.observer.Load(); observer != nil {
		observer.observer.RecordTraffic(inbound, userID, 0, size)
	}
}

func (m *Manager) SetTrafficObserver(observer TrafficObserver) {
	if observer == nil {
		m.observer.Store(nil)
		return
	}
	m.observer.Store(&trafficObserverHolder{observer: observer})
}

func (m *Manager) Now() (up int64, down int64) {
	return m.uploadBlip.Load(), m.downloadBlip.Load()
}

func (m *Manager) Total() (up, down int64) {
	return m.uploadTotal.Load(), m.downloadTotal.Load()
}

func (m *Manager) Memory() uint64 {
	m.updateMemory()
	return m.memory.Load()
}

func (m *Manager) Snapshot() *Snapshot {
	var connections []*TrackerInfo
	m.Range(func(c Tracker) bool {
		connections = append(connections, c.Info())
		return true
	})
	return &Snapshot{
		UploadTotal:   m.uploadTotal.Load(),
		DownloadTotal: m.downloadTotal.Load(),
		Connections:   connections,
		Memory:        m.memory.Load(),
	}
}

func (m *Manager) updateMemory() {
	stat, err := memory.GetMemoryInfo(m.pid)
	if err != nil {
		return
	}
	m.memory.Store(stat.RSS)
}

func (m *Manager) ResetStatistic() {
	m.uploadTemp.Store(0)
	m.uploadBlip.Store(0)
	m.uploadTotal.Store(0)
	m.downloadTemp.Store(0)
	m.downloadBlip.Store(0)
	m.downloadTotal.Store(0)
}

func (m *Manager) handle() {
	ticker := time.NewTicker(time.Second)

	for now := range ticker.C {
		m.uploadBlip.Store(m.uploadTemp.Swap(0))
		m.downloadBlip.Store(m.downloadTemp.Swap(0))
		m.reapIdleZeroByteTCP(now)
	}
}

func (m *Manager) reapIdleZeroByteTCP(now time.Time) int {
	var stale []Tracker
	m.Range(func(c Tracker) bool {
		if trackerEligibleForZeroByteReap(c.Info(), now) {
			stale = append(stale, c)
		}
		return true
	})
	for _, c := range stale {
		m.safeReapClose(c)
	}
	return len(stale)
}

func (m *Manager) safeReapClose(c Tracker) {
	info := c.Info()
	if info == nil {
		return
	}
	once, _ := m.reapOnce.LoadOrStore(info.UUID, &sync.Once{})
	once.Do(func() {
		_ = c.Close()
	})
}

func trackerEligibleForZeroByteReap(info *TrackerInfo, now time.Time) bool {
	if info == nil || info.Metadata == nil {
		return false
	}
	if info.Metadata.NetWork != C.TCP {
		return false
	}
	if info.Metadata.DstPort == 500 || info.Metadata.DstPort == 4500 {
		return false
	}
	if info.UploadTotal.Load() != 0 || info.DownloadTotal.Load() != 0 {
		return false
	}
	return !now.Before(info.Start.Add(idleZeroByteTCP))
}

type Snapshot struct {
	DownloadTotal int64          `json:"downloadTotal"`
	UploadTotal   int64          `json:"uploadTotal"`
	Connections   []*TrackerInfo `json:"connections"`
	Memory        uint64         `json:"memory"`
}
