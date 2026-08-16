package statistic

import (
	"io"
	"net"
	"time"

	"github.com/Miku0139oao/aster-core/common/atomic"
	"github.com/Miku0139oao/aster-core/common/buf"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/common/utils"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/gofrs/uuid/v5"
)

type Tracker interface {
	ID() string
	Close() error
	Info() *TrackerInfo
	C.Connection
}

// Reporting every read and write to the traffic observer serialises the whole
// data path on state shared by the manager. Each connection instead accumulates
// its own bytes and reports once per threshold, plus a final report on close, so
// per-user totals stay exact while the shared state is touched rarely.
const principalReportThreshold = 64 << 10

type principal struct {
	inbound  string
	userID   string
	upload   atomic.Int64
	download atomic.Int64
}

func newPrincipal(metadata *C.Metadata) *principal {
	if metadata == nil || metadata.InName == "" || metadata.InUser == "" {
		return nil
	}
	return &principal{inbound: metadata.InName, userID: metadata.InUser}
}

// accumulate adds size to pending and returns the amount to report, which is
// zero until the threshold is reached.
func accumulate(pending *atomic.Int64, size int64) int64 {
	if size <= 0 {
		return 0
	}
	total := pending.Add(size)
	if total < principalReportThreshold {
		return 0
	}
	if pending.CompareAndSwap(total, 0) {
		return total
	}
	return 0
}

func (p *principal) reportUpload(manager *Manager, size int64) {
	if p == nil {
		return
	}
	if pending := accumulate(&p.upload, size); pending > 0 {
		manager.recordPrincipal(p.inbound, p.userID, pending, 0)
	}
}

func (p *principal) reportDownload(manager *Manager, size int64) {
	if p == nil {
		return
	}
	if pending := accumulate(&p.download, size); pending > 0 {
		manager.recordPrincipal(p.inbound, p.userID, 0, pending)
	}
}

// flush reports the bytes that never reached the threshold.
func (p *principal) flush(manager *Manager) {
	if p == nil {
		return
	}
	upload, download := p.upload.Swap(0), p.download.Swap(0)
	if upload > 0 || download > 0 {
		manager.recordPrincipal(p.inbound, p.userID, upload, download)
	}
}

type TrackerInfo struct {
	UUID          uuid.UUID    `json:"id"`
	Metadata      *C.Metadata  `json:"metadata"`
	UploadTotal   atomic.Int64 `json:"upload"`
	DownloadTotal atomic.Int64 `json:"download"`
	Start         time.Time    `json:"start"`
	Chain         C.Chain      `json:"chains"`
	ProviderChain C.Chain      `json:"providerChains"`
	Rule          string       `json:"rule"`
	RulePayload   string       `json:"rulePayload"`
}

type tcpTracker struct {
	C.Conn `json:"-"`
	*TrackerInfo
	manager   *Manager
	principal *principal

	pushToManager bool `json:"-"`
}

func (tt *tcpTracker) ID() string {
	return tt.UUID.String()
}

func (tt *tcpTracker) Info() *TrackerInfo {
	return tt.TrackerInfo
}

func (tt *tcpTracker) pushDownloaded(download int64) {
	if tt.pushToManager {
		tt.manager.PushDownloaded(download)
		tt.principal.reportDownload(tt.manager, download)
	}
	tt.DownloadTotal.Add(download)
}

func (tt *tcpTracker) pushUploaded(upload int64) {
	if tt.pushToManager {
		tt.manager.PushUploaded(upload)
		tt.principal.reportUpload(tt.manager, upload)
	}
	tt.UploadTotal.Add(upload)
}

func (tt *tcpTracker) Read(b []byte) (int, error) {
	n, err := tt.Conn.Read(b)
	tt.pushDownloaded(int64(n))
	return n, err
}

func (tt *tcpTracker) ReadBuffer(buffer *buf.Buffer) (err error) {
	err = tt.Conn.ReadBuffer(buffer)
	tt.pushDownloaded(int64(buffer.Len()))
	return
}

func (tt *tcpTracker) UnwrapReader() (io.Reader, []N.CountFunc) {
	return tt.Conn, []N.CountFunc{tt.pushDownloaded}
}

func (tt *tcpTracker) Write(b []byte) (int, error) {
	n, err := tt.Conn.Write(b)
	tt.pushUploaded(int64(n))
	return n, err
}

func (tt *tcpTracker) WriteBuffer(buffer *buf.Buffer) (err error) {
	upload := int64(buffer.Len())
	err = tt.Conn.WriteBuffer(buffer)
	if err != nil {
		return err
	}
	tt.pushUploaded(upload)
	return nil
}

func (tt *tcpTracker) UnwrapWriter() (io.Writer, []N.CountFunc) {
	return tt.Conn, []N.CountFunc{tt.pushUploaded}
}

func (tt *tcpTracker) Close() error {
	tt.manager.Leave(tt)
	if tt.pushToManager {
		tt.principal.flush(tt.manager)
	}
	return tt.Conn.Close()
}

func (tt *tcpTracker) Upstream() any {
	return tt.Conn
}

func NewTCPTracker(conn C.Conn, manager *Manager, metadata *C.Metadata, rule C.Rule, uploadTotal int64, downloadTotal int64, pushToManager bool) *tcpTracker {
	metadata.RemoteDst = conn.RemoteDestination()

	t := &tcpTracker{
		Conn:      conn,
		manager:   manager,
		principal: newPrincipal(metadata),
		TrackerInfo: &TrackerInfo{
			UUID:          utils.NewUUIDV4(),
			Start:         time.Now(),
			Metadata:      metadata,
			Chain:         conn.Chains(),
			ProviderChain: conn.ProviderChains(),
			Rule:          "",
			UploadTotal:   atomic.NewInt64(uploadTotal),
			DownloadTotal: atomic.NewInt64(downloadTotal),
		},
		pushToManager: pushToManager,
	}

	if pushToManager {
		if uploadTotal > 0 {
			manager.PushUploadedFor(metadata.InName, metadata.InUser, uploadTotal)
		}
		if downloadTotal > 0 {
			manager.PushDownloadedFor(metadata.InName, metadata.InUser, downloadTotal)
		}
	}

	if rule != nil {
		t.TrackerInfo.Rule = rule.RuleType().String()
		t.TrackerInfo.RulePayload = rule.Payload()
	}

	manager.Join(t)
	return t
}

type udpTracker struct {
	C.PacketConn `json:"-"`
	*TrackerInfo
	manager   *Manager
	principal *principal

	pushToManager bool `json:"-"`
}

func (ut *udpTracker) ID() string {
	return ut.UUID.String()
}

func (ut *udpTracker) Info() *TrackerInfo {
	return ut.TrackerInfo
}

func (ut *udpTracker) pushDownloaded(download int64) {
	if ut.pushToManager {
		ut.manager.PushDownloaded(download)
		ut.principal.reportDownload(ut.manager, download)
	}
	ut.DownloadTotal.Add(download)
}

func (ut *udpTracker) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := ut.PacketConn.ReadFrom(b)
	ut.pushDownloaded(int64(n))
	return n, addr, err
}

func (ut *udpTracker) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	data, put, addr, err = ut.PacketConn.WaitReadFrom()
	ut.pushDownloaded(int64(len(data)))
	return
}

func (ut *udpTracker) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := ut.PacketConn.WriteTo(b, addr)
	upload := int64(n)
	if ut.pushToManager {
		ut.manager.PushUploaded(upload)
		ut.principal.reportUpload(ut.manager, upload)
	}
	ut.UploadTotal.Add(upload)
	return n, err
}

func (ut *udpTracker) Close() error {
	ut.manager.Leave(ut)
	if ut.pushToManager {
		ut.principal.flush(ut.manager)
	}
	return ut.PacketConn.Close()
}

func (ut *udpTracker) Upstream() any {
	return ut.PacketConn
}

func NewUDPTracker(conn C.PacketConn, manager *Manager, metadata *C.Metadata, rule C.Rule, uploadTotal int64, downloadTotal int64, pushToManager bool) *udpTracker {
	metadata.RemoteDst = conn.RemoteDestination()

	ut := &udpTracker{
		PacketConn: conn,
		manager:    manager,
		principal:  newPrincipal(metadata),
		TrackerInfo: &TrackerInfo{
			UUID:          utils.NewUUIDV4(),
			Start:         time.Now(),
			Metadata:      metadata,
			Chain:         conn.Chains(),
			ProviderChain: conn.ProviderChains(),
			Rule:          "",
			UploadTotal:   atomic.NewInt64(uploadTotal),
			DownloadTotal: atomic.NewInt64(downloadTotal),
		},
		pushToManager: pushToManager,
	}

	if pushToManager {
		if uploadTotal > 0 {
			manager.PushUploadedFor(metadata.InName, metadata.InUser, uploadTotal)
		}
		if downloadTotal > 0 {
			manager.PushDownloadedFor(metadata.InName, metadata.InUser, downloadTotal)
		}
	}

	if rule != nil {
		ut.TrackerInfo.Rule = rule.RuleType().String()
		ut.TrackerInfo.RulePayload = rule.Payload()
	}

	manager.Join(ut)
	return ut
}
