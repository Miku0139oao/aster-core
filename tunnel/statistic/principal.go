package statistic

import "sync/atomic"

// Flush principal accounting after this much pending traffic so busy
// connections do not invoke the Aster observer on every Read/Write.
const principalTrafficFlushThreshold int64 = 16 << 10

type principalAccountant struct {
	manager     *Manager
	inbound     string
	userID      string
	pendingUp   atomic.Int64
	pendingDown atomic.Int64
}

func newPrincipalAccountant(manager *Manager, inbound, userID string) principalAccountant {
	return principalAccountant{manager: manager, inbound: inbound, userID: userID}
}

func (a *principalAccountant) enabled() bool {
	return a.inbound != "" && a.userID != ""
}

func (a *principalAccountant) addUpload(n int64) {
	if n <= 0 {
		return
	}
	a.manager.PushUploaded(n)
	a.queue(n, 0)
}

func (a *principalAccountant) addDownload(n int64) {
	if n <= 0 {
		return
	}
	a.manager.PushDownloaded(n)
	a.queue(0, n)
}

func (a *principalAccountant) queue(upload, download int64) {
	if !a.enabled() {
		return
	}
	var pending int64
	if upload > 0 {
		pending = a.pendingUp.Add(upload)
	}
	if download > 0 {
		if down := a.pendingDown.Add(download); down > pending {
			pending = down
		}
	}
	if pending >= principalTrafficFlushThreshold {
		a.flush()
	}
}

func (a *principalAccountant) flush() {
	if !a.enabled() {
		return
	}
	upload := a.pendingUp.Swap(0)
	download := a.pendingDown.Swap(0)
	a.manager.RecordPrincipal(a.inbound, a.userID, upload, download)
}
