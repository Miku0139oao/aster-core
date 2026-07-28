package anytls

import (
	"crypto/sha256"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingCloseConn struct {
	net.Conn
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingCloseConn) Close() error {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return nil
}

func TestUpdateUsersPublishesCompleteSnapshot(t *testing.T) {
	listener := &Listener{}
	initial, err := buildUserSnapshot(map[string]string{"first": "old-password"})
	require.NoError(t, err)
	listener.users.Store(initial)

	require.NoError(t, listener.UpdateUsers(map[string]string{"second": "new-password"}))
	updated := listener.users.Load()
	require.NotSame(t, initial, updated)
	require.NotContains(t, updated.byPasswordHash, sha256.Sum256([]byte("old-password")))
	require.Equal(t, "second", updated.byPasswordHash[sha256.Sum256([]byte("new-password"))])
}

func TestUpdateUsersRejectsDuplicatePassword(t *testing.T) {
	listener := &Listener{}
	initial, err := buildUserSnapshot(map[string]string{"first": "password"})
	require.NoError(t, err)
	listener.users.Store(initial)

	require.Error(t, listener.UpdateUsers(map[string]string{
		"first":  "duplicate",
		"second": "duplicate",
	}))
	require.Same(t, initial, listener.users.Load())
}

func TestUpdateUsersRevokesActiveConnections(t *testing.T) {
	listener := &Listener{}
	listener.users.Store(&userSnapshot{byPasswordHash: map[[32]byte]string{}})
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	listener.connections.Store(&activeConnection{conn: serverConn}, struct{}{})

	require.NoError(t, listener.UpdateUsers(nil))
	_, err := clientConn.Read(make([]byte, 1))
	require.Error(t, err)
}

func TestUpdateUsersPreservesUnaffectedConnections(t *testing.T) {
	listener := &Listener{}
	initial, err := buildUserSnapshot(map[string]string{"first": "old-password", "second": "same-password"})
	require.NoError(t, err)
	listener.users.Store(initial)
	firstServer, firstClient := net.Pipe()
	defer firstClient.Close()
	secondServer, secondClient := net.Pipe()
	defer secondClient.Close()
	defer secondServer.Close()
	firstHash := sha256.Sum256([]byte("old-password"))
	secondHash := sha256.Sum256([]byte("same-password"))
	first := &activeConnection{conn: firstServer, passwordHash: firstHash, user: "first"}
	second := &activeConnection{conn: secondServer, passwordHash: secondHash, user: "second"}
	listener.connections.Store(first, struct{}{})
	listener.connections.Store(second, struct{}{})

	require.NoError(t, listener.UpdateUsers(map[string]string{"second": "same-password"}))
	_, firstActive := listener.connections.Load(first)
	_, secondActive := listener.connections.Load(second)
	require.False(t, firstActive)
	require.True(t, secondActive)
}

func TestCloseRevokesUsersAndCannotBeReopened(t *testing.T) {
	listener := &Listener{}
	initial, err := buildUserSnapshot(map[string]string{"first": "password"})
	require.NoError(t, err)
	listener.users.Store(initial)

	require.NoError(t, listener.Close())
	require.Empty(t, listener.users.Load().byPasswordHash)
	require.ErrorIs(t, listener.UpdateUsers(map[string]string{"second": "password"}), net.ErrClosed)
}

func TestPendingConnectionsAreRevoked(t *testing.T) {
	tests := map[string]func(*Listener) error{
		"update users": func(listener *Listener) error {
			return listener.UpdateUsers(nil)
		},
		"close listener": func(listener *Listener) error {
			return listener.Close()
		},
	}

	for name, revoke := range tests {
		t.Run(name, func(t *testing.T) {
			listener := &Listener{}
			listener.users.Store(&userSnapshot{byPasswordHash: map[[32]byte]string{}})
			serverConn, clientConn := net.Pipe()
			defer clientConn.Close()
			done := make(chan struct{})
			go func() {
				listener.HandleConn(serverConn, nil)
				close(done)
			}()

			require.Eventually(t, func() bool {
				tracked := false
				listener.pending.Range(func(_, _ any) bool {
					tracked = true
					return false
				})
				return tracked
			}, time.Second, time.Millisecond)
			require.NoError(t, revoke(listener))
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("pending AnyTLS connection was not closed")
			}
			_, err := clientConn.Read(make([]byte, 1))
			require.Error(t, err)
		})
	}
}

func TestUpdateUsersClosesConnectionsWithoutUsersMu(t *testing.T) {
	listener := &Listener{}
	listener.users.Store(&userSnapshot{byPasswordHash: map[[32]byte]string{}})
	conn := &blockingCloseConn{started: make(chan struct{}), release: make(chan struct{})}
	listener.pending.Store(&pendingConnection{conn: conn}, struct{}{})

	done := make(chan error, 1)
	go func() {
		done <- listener.UpdateUsers(nil)
	}()

	select {
	case <-conn.started:
	case <-time.After(time.Second):
		t.Fatal("UpdateUsers did not start closing the pending connection")
	}

	locked := make(chan struct{})
	go func() {
		listener.usersMu.Lock()
		listener.usersMu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(time.Second):
		close(conn.release)
		t.Fatal("UpdateUsers held usersMu while closing a connection")
	}

	close(conn.release)
	require.NoError(t, <-done)
}
