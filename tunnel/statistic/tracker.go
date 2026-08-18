package statistic

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/Miku0139oao/aster-core/common/atomic"
	"github.com/Miku0139oao/aster-core/common/buf"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/common/utils"
	trafficControl "github.com/Miku0139oao/aster-core/component/trafficcontrol"
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
	manager        *Manager
	principal      *principal
	traffic        *trafficControl.Session
	ctx            context.Context
	cancel         context.CancelFunc
	portalMu       sync.Mutex
	portalResponse []byte
	portalOffset   int

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
	if tt.portalResponse != nil {
		return tt.readPortal(b)
	}
	n, err := tt.Conn.Read(b)
	download := int64(n)
	if waitErr := tt.traffic.Wait(tt.ctx, trafficControl.Download, n); waitErr != nil && err == nil {
		err = waitErr
	}
	tt.traffic.Record(trafficControl.Download, download)
	tt.pushDownloaded(download)
	return n, err
}

func (tt *tcpTracker) ReadBuffer(buffer *buf.Buffer) (err error) {
	if tt.portalResponse != nil {
		tt.portalMu.Lock()
		defer tt.portalMu.Unlock()
		if tt.portalOffset >= len(tt.portalResponse) {
			return io.EOF
		}
		_, err = buffer.Write(tt.portalResponse[tt.portalOffset:])
		tt.portalOffset = len(tt.portalResponse)
		return err
	}
	err = tt.Conn.ReadBuffer(buffer)
	download := int64(buffer.Len())
	if waitErr := tt.traffic.Wait(tt.ctx, trafficControl.Download, buffer.Len()); waitErr != nil && err == nil {
		err = waitErr
	}
	tt.traffic.Record(trafficControl.Download, download)
	tt.pushDownloaded(download)
	return
}

func (tt *tcpTracker) UnwrapReader() (io.Reader, []N.CountFunc) {
	reader := io.Reader(tt.Conn)
	if tt.portalResponse != nil {
		reader = tt
	}
	return &controlledReader{reader: reader, session: tt.traffic, ctx: tt.ctx}, []N.CountFunc{func(download int64) {
		tt.traffic.Record(trafficControl.Download, download)
		tt.pushDownloaded(download)
	}}
}

func (tt *tcpTracker) Write(b []byte) (int, error) {
	if tt.portalResponse != nil {
		return len(b), nil
	}
	if err := tt.traffic.Wait(tt.ctx, trafficControl.Upload, len(b)); err != nil {
		return 0, err
	}
	n, err := tt.Conn.Write(b)
	upload := int64(n)
	tt.traffic.Record(trafficControl.Upload, upload)
	tt.pushUploaded(upload)
	return n, err
}

func (tt *tcpTracker) WriteBuffer(buffer *buf.Buffer) (err error) {
	if tt.portalResponse != nil {
		return nil
	}
	upload := int64(buffer.Len())
	if err = tt.traffic.Wait(tt.ctx, trafficControl.Upload, buffer.Len()); err != nil {
		return err
	}
	err = tt.Conn.WriteBuffer(buffer)
	if err != nil {
		return err
	}
	tt.traffic.Record(trafficControl.Upload, upload)
	tt.pushUploaded(upload)
	return nil
}

func (tt *tcpTracker) UnwrapWriter() (io.Writer, []N.CountFunc) {
	writer := io.Writer(tt.Conn)
	if tt.portalResponse != nil {
		writer = io.Discard
	}
	return &controlledWriter{writer: writer, session: tt.traffic, ctx: tt.ctx}, []N.CountFunc{func(upload int64) {
		tt.traffic.Record(trafficControl.Upload, upload)
		tt.pushUploaded(upload)
	}}
}

func (tt *tcpTracker) Close() error {
	tt.cancel()
	tt.traffic.Close()
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
	ctx, cancel := context.WithCancel(context.Background())
	traffic := trafficControl.Default.Open(trafficFlow(metadata, rule, conn.Chains()))
	portalResponse := trafficControlPortalResponse(traffic, metadata)

	t := &tcpTracker{
		Conn:           conn,
		manager:        manager,
		principal:      newPrincipal(metadata),
		traffic:        traffic,
		ctx:            ctx,
		cancel:         cancel,
		portalResponse: portalResponse,
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
			manager.PushUploaded(uploadTotal)
			t.principal.reportUpload(manager, uploadTotal)
		}
		if downloadTotal > 0 {
			manager.PushDownloaded(downloadTotal)
			t.principal.reportDownload(manager, downloadTotal)
		}
	}
	traffic.Record(trafficControl.Upload, uploadTotal)
	traffic.Record(trafficControl.Download, downloadTotal)

	if rule != nil {
		t.TrackerInfo.Rule = rule.RuleType().String()
		t.TrackerInfo.RulePayload = rule.Payload()
	}

	manager.Join(t)
	return t
}

func (tt *tcpTracker) readPortal(buffer []byte) (int, error) {
	tt.portalMu.Lock()
	defer tt.portalMu.Unlock()
	if tt.portalOffset >= len(tt.portalResponse) {
		return 0, io.EOF
	}
	n := copy(buffer, tt.portalResponse[tt.portalOffset:])
	tt.portalOffset += n
	return n, nil
}

func trafficControlPortalResponse(session *trafficControl.Session, metadata *C.Metadata) []byte {
	if metadata == nil || metadata.DstPort != 80 {
		return nil
	}
	location := session.PortalURL()
	if location == "" {
		return nil
	}
	body := "Traffic quota exceeded. Open " + location + "\n"
	return []byte(fmt.Sprintf("HTTP/1.1 302 Found\r\nLocation: %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n%s", location, len(body), body))
}

type udpTracker struct {
	C.PacketConn `json:"-"`
	*TrackerInfo
	manager   *Manager
	principal *principal
	traffic   *trafficControl.Session

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
	for {
		n, addr, err := ut.PacketConn.ReadFrom(b)
		if n > 0 && !ut.traffic.AllowPacket(trafficControl.Download, n) {
			if err != nil {
				return n, addr, err
			}
			continue
		}
		download := int64(n)
		ut.traffic.Record(trafficControl.Download, download)
		ut.pushDownloaded(download)
		return n, addr, err
	}
}

func (ut *udpTracker) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	for {
		data, put, addr, err = ut.PacketConn.WaitReadFrom()
		if len(data) > 0 && !ut.traffic.AllowPacket(trafficControl.Download, len(data)) {
			if put != nil {
				put()
			}
			if err != nil {
				return nil, nil, addr, err
			}
			continue
		}
		download := int64(len(data))
		ut.traffic.Record(trafficControl.Download, download)
		ut.pushDownloaded(download)
		return
	}
}

func (ut *udpTracker) WriteTo(b []byte, addr net.Addr) (int, error) {
	if !ut.traffic.AllowPacket(trafficControl.Upload, len(b)) {
		return len(b), nil
	}
	n, err := ut.PacketConn.WriteTo(b, addr)
	upload := int64(n)
	ut.traffic.Record(trafficControl.Upload, upload)
	if ut.pushToManager {
		ut.manager.PushUploaded(upload)
		ut.principal.reportUpload(ut.manager, upload)
	}
	ut.UploadTotal.Add(upload)
	return n, err
}

func (ut *udpTracker) Close() error {
	ut.traffic.Close()
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
	traffic := trafficControl.Default.Open(trafficFlow(metadata, rule, conn.Chains()))

	ut := &udpTracker{
		PacketConn: conn,
		manager:    manager,
		principal:  newPrincipal(metadata),
		traffic:    traffic,
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
			manager.PushUploaded(uploadTotal)
			ut.principal.reportUpload(manager, uploadTotal)
		}
		if downloadTotal > 0 {
			manager.PushDownloaded(downloadTotal)
			ut.principal.reportDownload(manager, downloadTotal)
		}
	}
	traffic.Record(trafficControl.Upload, uploadTotal)
	traffic.Record(trafficControl.Download, downloadTotal)

	if rule != nil {
		ut.TrackerInfo.Rule = rule.RuleType().String()
		ut.TrackerInfo.RulePayload = rule.Payload()
	}

	manager.Join(ut)
	return ut
}

func trafficFlow(metadata *C.Metadata, rule C.Rule, chains C.Chain) trafficControl.Flow {
	flow := trafficControl.Flow{SourceIP: metadata.SrcIP, Chains: append([]string(nil), chains...)}
	if rule != nil {
		flow.RuleType = rule.RuleType().String()
		flow.RulePayload = rule.Payload()
		flow.RuleTarget = rule.Adapter()
	}
	return flow
}

type controlledReader struct {
	reader  io.Reader
	session *trafficControl.Session
	ctx     context.Context
}

func (r *controlledReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	if waitErr := r.session.Wait(r.ctx, trafficControl.Download, n); waitErr != nil && err == nil {
		err = waitErr
	}
	return n, err
}

type controlledWriter struct {
	writer  io.Writer
	session *trafficControl.Session
	ctx     context.Context
}

func (w *controlledWriter) Write(buffer []byte) (int, error) {
	if err := w.session.Wait(w.ctx, trafficControl.Upload, len(buffer)); err != nil {
		return 0, err
	}
	return w.writer.Write(buffer)
}
