package session

import (
	"context"
	"io"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type closeTrackingConn struct {
	closeOnce sync.Once
	closed    chan struct{}
}

func newCloseTrackingConn() *closeTrackingConn {
	return &closeTrackingConn{closed: make(chan struct{})}
}

func (c *closeTrackingConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.ErrClosedPipe
}

func (c *closeTrackingConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *closeTrackingConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *closeTrackingConn) LocalAddr() net.Addr              { return nil }
func (c *closeTrackingConn) RemoteAddr() net.Addr             { return nil }
func (c *closeTrackingConn) SetDeadline(time.Time) error      { return nil }
func (c *closeTrackingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *closeTrackingConn) SetWriteDeadline(time.Time) error { return nil }

func TestRecycleSessionDoesNotReinsertClosedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ctx, nil, nil, "", 0, 0, 0, false)
	defer client.Close()

	session := NewClientSession(discardConn{}, nil, "")
	session.seq = 1
	session.Close()

	client.recycleSession(session)
	require.True(t, client.idleSession.IsEmpty())
}

func TestGetIdleSessionSkipsClosedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ctx, nil, nil, "", 0, 0, 0, false)
	defer client.Close()

	session := NewClientSession(discardConn{}, nil, "")
	session.seq = 1
	session.Close()

	client.idleSessionLock.Lock()
	client.idleSession.Insert(math.MaxUint64-session.seq, session)
	client.idleSessionLock.Unlock()

	require.Nil(t, client.getIdleSession())
	require.True(t, client.idleSession.IsEmpty())
}

func TestIdleCleanupConcurrentWithRecycle(t *testing.T) {
	// Regression: idleCleanupExpTime used to size its slice with
	// c.idleSession.Len() before taking idleSessionLock, racing with the
	// Insert/Remove calls made under that lock by recycleSession and the
	// session die hooks. Run the cleanup concurrently with those mutations so
	// `go test -race` catches any reintroduced out-of-lock access.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ctx, nil, nil, "", 0, 0, 0, false)
	defer client.Close()

	// Any session created before this time is considered active, so the
	// cleanup below only ever observes the list and never closes a session.
	expTime := time.Now().Add(time.Hour)

	const inserts = 200
	firstIteration := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		close(firstIteration)
		for i := 0; i < inserts; i++ {
			client.idleCleanupExpTime(expTime)
		}
	}()
	go func() {
		defer wg.Done()
		<-firstIteration
		for i := 0; i < inserts; i++ {
			session := NewClientSession(discardConn{}, nil, "")
			session.seq = uint64(i) + 1
			client.recycleSession(session)

			// Mutate the list under the lock as the production paths do. Use a
			// key range that never collides with the real sessions, which are
			// inserted at math.MaxUint64-session.seq.
			client.idleSessionLock.Lock()
			scratch := NewClientSession(discardConn{}, nil, "")
			key := uint64(i)
			client.idleSession.Insert(key, scratch)
			client.idleSession.Remove(key)
			client.idleSessionLock.Unlock()
		}
	}()
	wg.Wait()

	// The cleanup is allowed to remove sessions (it keeps minIdleSession of the
	// ones it judges active), so the count is not deterministic. What this test
	// asserts is that concurrent Len/Insert/Remove under the lock does not
	// corrupt the list: the walk above must complete, and the list must still be
	// readable and consistent afterwards. With -race, an out-of-lock Len() is
	// reported here.
	client.idleSessionLock.Lock()
	length := client.idleSession.Len()
	iterations := 0
	it := client.idleSession.Iterate()
	for it.IsNotEnd() {
		iterations++
		it.MoveToNext()
	}
	client.idleSessionLock.Unlock()
	require.Equal(t, length, iterations, "list length disagrees with iteration after concurrent mutation")
	require.LessOrEqual(t, length, inserts)
	require.GreaterOrEqual(t, length, 0)
}

func TestCreateSessionAbortsAfterClientClose(t *testing.T) {
	dialStarted := make(chan struct{})
	releaseDial := make(chan struct{})
	underlying := newCloseTrackingConn()
	client := NewClient(context.Background(), func(context.Context) (net.Conn, error) {
		close(dialStarted)
		<-releaseDial
		return underlying, nil
	}, nil, "", 0, 0, 0, true)

	result := make(chan struct {
		session *Session
		err     error
	}, 1)
	go func() {
		session, err := client.createSession(context.Background())
		result <- struct {
			session *Session
			err     error
		}{session, err}
	}()

	<-dialStarted
	require.NoError(t, client.Close())
	close(releaseDial)

	select {
	case got := <-result:
		require.Nil(t, got.session)
		require.ErrorIs(t, got.err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("createSession did not observe the closed client")
	}

	select {
	case <-underlying.closed:
	case <-time.After(time.Second):
		t.Fatal("underlying connection was not closed")
	}
}
