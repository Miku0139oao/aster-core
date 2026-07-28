package aster

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/log"
)

const trafficFlushInterval = 5 * time.Minute

// The high bit closes recorder admission; the remaining bits count recorders
// that must finish before the runtime's counters can be synchronized.
const runtimeRetiring = uint64(1) << 63

func (m *Manager) RecordTraffic(inbound, userID string, upload, download int64) {
	if inbound == "" || userID == "" || (upload <= 0 && download <= 0) {
		return
	}
	key := trafficKey{inbound: inbound, userID: userID}
	for {
		runtime := m.runtime.Load()
		if !runtime.acquireRecorder() {
			continue
		}
		counter := runtime.traffic[key]
		if counter == nil {
			runtime.releaseRecorder()
			return
		}
		if upload > 0 {
			addTraffic(&counter.upload, upload)
		}
		if download > 0 {
			addTraffic(&counter.download, download)
		}
		m.dirty.Store(true)
		runtime.releaseRecorder()
		return
	}
}

func (runtime *runtimeState) acquireRecorder() bool {
	state := runtime.recorders.Add(1)
	if state&runtimeRetiring == 0 {
		return true
	}
	runtime.releaseRecorder()
	return false
}

func (runtime *runtimeState) releaseRecorder() {
	if runtime.recorders.Add(^uint64(0)) == runtimeRetiring {
		runtime.drainOnce.Do(func() { close(runtime.drained) })
	}
}

func (runtime *runtimeState) retireRecorders() {
	for {
		state := runtime.recorders.Load()
		if state&runtimeRetiring != 0 {
			<-runtime.drained
			return
		}
		if runtime.recorders.CompareAndSwap(state, state|runtimeRetiring) {
			if state == 0 {
				runtime.drainOnce.Do(func() { close(runtime.drained) })
			}
			<-runtime.drained
			return
		}
	}
}

func (m *Manager) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trafficMu.Lock()
	defer m.trafficMu.Unlock()
	if !m.dirty.Swap(false) {
		if m.config == nil && m.storePath != "" {
			m.stopFlusherLocked()
			m.releaseStoreLocked()
		}
		return nil
	}
	if m.storePath == "" {
		m.dirty.Store(true)
		return nil
	}
	m.syncTrafficLocked()
	if err := m.persistStore(m.storePath, m.store); err != nil {
		m.dirty.Store(true)
		return fmt.Errorf("persist Aster traffic: %w", err)
	}
	if m.config == nil {
		m.stopFlusherLocked()
		m.releaseStoreLocked()
	}
	return nil
}

func addTraffic(counter *atomic.Int64, value int64) {
	for {
		current := counter.Load()
		next := current + value
		if current > math.MaxInt64-value {
			next = math.MaxInt64
		}
		if counter.CompareAndSwap(current, next) {
			return
		}
	}
}

func (m *Manager) syncTrafficLocked() {
	syncTrafficStore(m.store, m.runtime.Load())
}

func (m *Manager) syncUserTrafficLocked(user *User) {
	runtime := m.runtime.Load()
	counter := runtime.traffic[trafficKey{inbound: user.Inbound, userID: user.ID}]
	if counter == nil || counter.generation != user.TrafficGeneration {
		return
	}
	user.UploadBytes = counter.upload.Load()
	user.DownloadBytes = counter.download.Load()
}

func syncTrafficStore(store *Store, runtime *runtimeState) {
	for inbound, state := range store.Listeners {
		for _, user := range state.Users {
			counter := runtime.traffic[trafficKey{inbound: inbound, userID: user.ID}]
			if counter == nil || counter.generation != user.TrafficGeneration {
				continue
			}
			user.UploadBytes = counter.upload.Load()
			user.DownloadBytes = counter.download.Load()
		}
	}
}

func (m *Manager) startFlusherLocked() {
	if m.flushCancel != nil {
		return
	}
	cancel := make(chan struct{})
	m.flushCancel = cancel
	go func() {
		ticker := time.NewTicker(trafficFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := m.Flush(); err != nil {
					log.Warnln("Aster traffic flush failed: %s", err)
				}
			case <-cancel:
				return
			}
		}
	}()
}

func (m *Manager) stopFlusherLocked() {
	if m.flushCancel == nil {
		return
	}
	close(m.flushCancel)
	m.flushCancel = nil
}
