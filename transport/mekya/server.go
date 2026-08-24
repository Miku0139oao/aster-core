package mekya

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/transport/mkcp"

	"github.com/metacubex/http"
)

type Listener struct {
	outer      net.Listener
	packetConn *wrappedPacketConn
	mkcp       *mkcp.Listener
	server     *http.Server
	done       chan struct{}
	cancel     context.CancelFunc
	once       sync.Once
}

func Listen(ctx context.Context, ln net.Listener, cfg Config) (*Listener, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	listenerCtx, cancel := context.WithCancel(ctx)
	packetConn := newWrappedPacketConn(listenerCtx, ln.Addr())
	handler := newServer(listenerCtx, cfg, packetConn)
	mkcpListener, err := mkcp.Listen(listenerCtx, packetConn, cfg.KCP)
	if err != nil {
		cancel()
		_ = packetConn.Close()
		return nil, err
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: 240 * time.Second,
		ReadTimeout:       240 * time.Second,
		WriteTimeout:      240 * time.Second,
		IdleTimeout:       240 * time.Second,
	}
	l := &Listener{
		outer:      ln,
		packetConn: packetConn,
		mkcp:       mkcpListener,
		server:     server,
		done:       make(chan struct{}),
		cancel:     cancel,
	}
	go func() {
		defer close(l.done)
		err := server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			cancel()
			_ = mkcpListener.Close()
			_ = packetConn.Close()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-l.done:
		}
	}()
	return l, nil
}

func (l *Listener) Accept() (net.Conn, error) {
	return l.mkcp.Accept()
}

func (l *Listener) Close() error {
	var err error
	l.once.Do(func() {
		l.cancel()
		err = errors.Join(l.server.Close(), l.mkcp.Close(), l.packetConn.Close(), l.outer.Close())
		<-l.done
	})
	return err
}

func (l *Listener) Addr() net.Addr {
	return l.outer.Addr()
}

const (
	mekyaSessionIDSize     = 16
	mekyaSessionIdle       = 5 * time.Minute
	mekyaSessionReapPeriod = 30 * time.Second
)

var errMekyaSessionLimit = errors.New("mekya: active session limit reached")

type server struct {
	ctx          context.Context
	cfg          Config
	packetConn   *wrappedPacketConn
	sessions     sync.Map
	sessionCount atomic.Int64
}

func newServer(ctx context.Context, cfg Config, packetConn *wrappedPacketConn) *server {
	s := &server{ctx: ctx, cfg: cfg, packetConn: packetConn}
	go s.reapLoop()
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Session-ID"))
	if err != nil || len(sessionID) != mekyaSessionIDSize {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	body, err := readMekyaRequestBody(r.Body, s.cfg.MaxRequestSize)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errMekyaRequestTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	packetCount, err := validatePacketBundleBody(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	key := string(sessionID)
	existing, loaded := s.sessions.Load(key)
	if !loaded && packetCount == 0 {
		// Polling an unknown ID must not allocate a session and reader goroutine.
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	var session *serverSession
	if loaded {
		session = existing.(*serverSession)
	} else {
		session, err = s.getSession(sessionID, parseRemoteAddr(r.RemoteAddr))
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errMekyaSessionLimit) {
				status = http.StatusServiceUnavailable
			}
			http.Error(w, err.Error(), status)
			return
		}
	}
	session.touch()
	if packetCount > 0 {
		if err := session.ingest(r.Context(), body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	session.writeResponse(r.Context(), w)
}

var errMekyaRequestTooLarge = errors.New("mekya: request body exceeds max-request-size")

func readMekyaRequestBody(body io.ReadCloser, limit int) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	reader := io.LimitReader(body, int64(limit)+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errMekyaRequestTooLarge
	}
	return data, nil
}

func validatePacketBundleBody(body []byte) (int, error) {
	count := 0
	for offset := 0; offset < len(body); count++ {
		if len(body)-offset < packetBundleOverhead {
			return 0, io.ErrUnexpectedEOF
		}
		length := int(binary.BigEndian.Uint16(body[offset : offset+packetBundleOverhead]))
		offset += packetBundleOverhead
		if length > len(body)-offset {
			return 0, io.ErrUnexpectedEOF
		}
		offset += length
	}
	return count, nil
}

func (s *server) getSession(sessionID []byte, remoteAddr *net.TCPAddr) (*serverSession, error) {
	if len(sessionID) != mekyaSessionIDSize {
		return nil, errors.New("mekya: invalid session id length")
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	key := string(sessionID)
	if session, ok := s.sessions.Load(key); ok {
		return session.(*serverSession), nil
	}
	for {
		count := s.sessionCount.Load()
		if count >= int64(s.cfg.MaxSessions) {
			return nil, errMekyaSessionLimit
		}
		if s.sessionCount.CompareAndSwap(count, count+1) {
			break
		}
	}

	sessionCtx, cancel := context.WithCancel(s.ctx)
	session := &serverSession{
		ctx:                            sessionCtx,
		cancel:                         cancel,
		sessionID:                      append([]byte(nil), sessionID...),
		remoteAddr:                     remoteAddr,
		server:                         s,
		writerChan:                     make(chan []byte, s.cfg.PacketWritingBuffer),
		readerChan:                     make(chan []byte, 256),
		maxWriteSize:                   s.cfg.MaxWriteSize,
		maxWriteDuration:               time.Duration(s.cfg.MaxWriteDurationMs) * time.Millisecond,
		maxSimultaneousWriteConnection: s.cfg.MaxSimultaneousWriteConnection,
	}
	session.touch()
	actual, loaded := s.sessions.LoadOrStore(key, session)
	if loaded {
		s.sessionCount.Add(-1)
		cancel()
		return actual.(*serverSession), nil
	}
	if err := s.packetConn.addSession(session); err != nil {
		s.removeSession(session)
		cancel()
		return nil, err
	}
	return session, nil
}

func (s *server) removeSession(session *serverSession) {
	if s.sessions.CompareAndDelete(string(session.sessionID), session) {
		s.sessionCount.Add(-1)
	}
}

func (s *server) reapLoop() {
	ticker := time.NewTicker(mekyaSessionReapPeriod)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			s.reapInactive(now, mekyaSessionIdle)
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *server) reapInactive(now time.Time, idle time.Duration) {
	cutoff := now.Add(-idle).UnixNano()
	s.sessions.Range(func(_, value any) bool {
		session := value.(*serverSession)
		if session.lastActivity.Load() < cutoff {
			_ = session.Close()
		}
		return true
	})
}

func parseRemoteAddr(addr string) *net.TCPAddr {
	if tcpAddr, err := net.ResolveTCPAddr("tcp", addr); err == nil {
		return tcpAddr
	}
	return &net.TCPAddr{}
}

type serverSession struct {
	ctx                            context.Context
	cancel                         context.CancelFunc
	sessionID                      []byte
	remoteAddr                     *net.TCPAddr
	server                         *server
	writerChan                     chan []byte
	readerChan                     chan []byte
	maxWriteSize                   int
	maxWriteDuration               time.Duration
	maxSimultaneousWriteConnection int
	writingMu                      sync.Mutex
	writingConns                   []*writingConnection
	lastActivity                   atomic.Int64
	closeOnce                      sync.Once
}

func (s *serverSession) touch() {
	s.lastActivity.Store(time.Now().UnixNano())
}

func (s *serverSession) ingest(ctx context.Context, body []byte) error {
	reader := bytes.NewReader(body)
	for reader.Len() > 0 {
		packet, err := readPacketBundle(reader)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		case s.readerChan <- packet:
			s.touch()
		}
	}
	return nil
}

type writingConnection struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *serverSession) beginWritingConnection(ctx context.Context) (*writingConnection, context.Context) {
	writeCtx, cancel := context.WithCancel(ctx)
	conn := &writingConnection{ctx: writeCtx, cancel: cancel}

	var stale []*writingConnection
	s.writingMu.Lock()
	s.writingConns = append(s.writingConns, conn)
	if s.maxSimultaneousWriteConnection > 0 {
		for len(s.writingConns) > s.maxSimultaneousWriteConnection {
			old := s.writingConns[0]
			s.writingConns[0] = nil
			s.writingConns = s.writingConns[1:]
			stale = append(stale, old)
		}
	}
	s.writingMu.Unlock()

	for _, old := range stale {
		old.cancel()
	}
	return conn, writeCtx
}

func (s *serverSession) finishWritingConnection(conn *writingConnection) {
	conn.cancel()

	s.writingMu.Lock()
	for i, item := range s.writingConns {
		if item == conn {
			copy(s.writingConns[i:], s.writingConns[i+1:])
			s.writingConns[len(s.writingConns)-1] = nil
			s.writingConns = s.writingConns[:len(s.writingConns)-1]
			break
		}
	}
	s.writingMu.Unlock()
}

func (s *serverSession) writeResponse(ctx context.Context, w http.ResponseWriter) {
	writeConn, writeCtx := s.beginWritingConnection(ctx)
	defer s.finishWritingConnection(writeConn)

	flusher, _ := w.(http.Flusher)
	timer := time.NewTimer(s.maxWriteDuration)
	defer timer.Stop()
	bytesSent := 0
	for {
		select {
		case <-writeCtx.Done():
			return
		case <-s.ctx.Done():
			return
		case packet := <-s.writerChan:
			if err := writePacketBundle(w, packet); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			bytesSent += packetBundleOverhead + len(packet)
			if s.maxWriteSize > 0 && bytesSent >= s.maxWriteSize {
				return
			}
		case <-timer.C:
			return
		}
	}
}

func (s *serverSession) Read(p []byte) (int, error) {
	select {
	case <-s.ctx.Done():
		return 0, s.ctx.Err()
	case packet := <-s.readerChan:
		s.touch()
		return copy(p, packet), nil
	}
}

func (s *serverSession) Write(p []byte) (int, error) {
	packet := append([]byte(nil), p...)
	select {
	case <-s.ctx.Done():
		return 0, s.ctx.Err()
	case s.writerChan <- packet:
		s.touch()
		return len(p), nil
	default:
		return len(p), nil
	}
}

func (s *serverSession) Close() error {
	s.closeOnce.Do(func() {
		s.server.removeSession(s)
		s.server.packetConn.removeSession(s)
		s.cancel()
		s.writingMu.Lock()
		writing := append([]*writingConnection(nil), s.writingConns...)
		s.writingConns = nil
		s.writingMu.Unlock()
		for _, connection := range writing {
			connection.cancel()
		}
	})
	return nil
}

func (s *serverSession) Network() string {
	if s.remoteAddr == nil {
		return ""
	}
	return s.remoteAddr.Network()
}

func (s *serverSession) String() string {
	if s.remoteAddr == nil {
		return ""
	}
	return s.remoteAddr.String()
}

var (
	_ net.Listener       = (*Listener)(nil)
	_ net.Addr           = (*serverSession)(nil)
	_ io.ReadWriteCloser = (*serverSession)(nil)
)
