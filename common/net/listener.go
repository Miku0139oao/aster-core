package net

import (
	"context"
	"net"
	"reflect"
	"sync"
)

type handleContextListener struct {
	net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	conns     chan handledConn
	handle    func(context.Context, net.Conn) (net.Conn, error)
	panicLog  func(any)
	startOnce sync.Once
	closeOnce sync.Once
	handlers  sync.WaitGroup
	mu        sync.Mutex
	err       error
	closeErr  error
	closed    bool
	nextID    uint64
	inFlight  map[uint64]net.Conn
}

type handledConn struct {
	conn  net.Conn
	raw   net.Conn
	rawID uint64
}

func (l *handleContextListener) init() {
	go func() {
		for {
			c, err := l.Listener.Accept()
			if err != nil {
				l.mu.Lock()
				if l.err == nil {
					l.err = err
				}
				l.mu.Unlock()
				break
			}
			l.mu.Lock()
			if l.closed {
				l.mu.Unlock()
				_ = c.Close()
				continue
			}
			l.nextID++
			id := l.nextID
			l.inFlight[id] = c
			l.handlers.Add(1)
			l.mu.Unlock()
			go func(id uint64, c net.Conn) {
				var conn net.Conn
				defer l.handlers.Done()
				defer func() {
					r := recover()
					l.mu.Lock()
					delete(l.inFlight, id)
					l.mu.Unlock()
					if r != nil {
						closeHandledConns(conn, c)
						if l.panicLog != nil {
							l.panicLog(r)
						}
					}
				}()
				var err error
				conn, err = l.handle(l.ctx, c)
				if err != nil || conn == nil {
					closeHandledConns(conn, c)
					return
				}
				select {
				case l.conns <- handledConn{conn: conn, raw: c, rawID: id}:
				case <-l.ctx.Done():
					closeHandledConns(conn, c)
				}
			}(id, c)
		}
		l.handlers.Wait()
		close(l.conns)
	}()
}

func (l *handleContextListener) Accept() (net.Conn, error) {
	l.startOnce.Do(l.init)
	select {
	case result, ok := <-l.conns:
		if !ok {
			return nil, l.acceptError()
		}
		l.mu.Lock()
		closed := l.closed
		delete(l.inFlight, result.rawID)
		l.mu.Unlock()
		if !closed {
			return result.conn, nil
		}
		closeHandledConns(result.conn, result.raw)
	case <-l.ctx.Done():
	}
	return nil, l.acceptError()
}

func (l *handleContextListener) acceptError() error {
	l.mu.Lock()
	err := l.err
	l.mu.Unlock()
	return err
}

func (l *handleContextListener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		if l.err == nil {
			l.err = net.ErrClosed
		}
		inFlight := make([]net.Conn, 0, len(l.inFlight))
		for _, c := range l.inFlight {
			inFlight = append(inFlight, c)
		}
		l.mu.Unlock()

		l.cancel()
		l.startOnce.Do(l.init)
		for _, c := range inFlight {
			_ = c.Close()
		}
		l.closeErr = l.Listener.Close()
	})
	return l.closeErr
}

func NewHandleContextListener(ctx context.Context, l net.Listener, handle func(context.Context, net.Conn) (net.Conn, error), panicLog func(any)) net.Listener {
	ctx, cancel := context.WithCancel(ctx)
	return &handleContextListener{
		Listener: l,
		ctx:      ctx,
		cancel:   cancel,
		conns:    make(chan handledConn),
		handle:   handle,
		panicLog: panicLog,
		inFlight: make(map[uint64]net.Conn),
	}
}

func closeHandledConns(conn, raw net.Conn) {
	if conn != nil {
		_ = conn.Close()
	}
	if raw == nil || sameComparableConn(conn, raw) {
		return
	}
	_ = raw.Close()
}

func sameComparableConn(left, right net.Conn) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftType := reflect.TypeOf(left)
	return leftType == reflect.TypeOf(right) && leftType.Comparable() && left == right
}

// ConnectionTrackingListener tracks accepted connections until they are closed.
type ConnectionTrackingListener struct {
	net.Listener
	mu        sync.Mutex
	conns     map[*trackedConn]struct{}
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

type trackedConn struct {
	net.Conn
	listener *ConnectionTrackingListener
	once     sync.Once
	err      error
}

func NewConnectionTrackingListener(listener net.Listener) *ConnectionTrackingListener {
	return &ConnectionTrackingListener{
		Listener: listener,
		conns:    make(map[*trackedConn]struct{}),
	}
}

func (l *ConnectionTrackingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tracked := &trackedConn{Conn: conn, listener: l}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = tracked.Close()
		return nil, net.ErrClosed
	}
	l.conns[tracked] = struct{}{}
	l.mu.Unlock()
	return tracked, nil
}

func (l *ConnectionTrackingListener) CloseConnections() {
	l.mu.Lock()
	conns := make([]*trackedConn, 0, len(l.conns))
	for conn := range l.conns {
		conns = append(conns, conn)
	}
	l.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}

func (l *ConnectionTrackingListener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
		l.CloseConnections()
		l.closeErr = l.Listener.Close()
	})
	return l.closeErr
}

func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		c.listener.mu.Lock()
		delete(c.listener.conns, c)
		c.listener.mu.Unlock()
	})
	return c.err
}
