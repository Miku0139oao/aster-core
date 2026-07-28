package sing_vless

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/utils"
	"github.com/Miku0139oao/aster-core/transport/vless"

	vmess "github.com/metacubex/sing-vmess"
	"github.com/metacubex/sing/common/auth"
	"github.com/metacubex/sing/common/buf"
	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
	"github.com/stretchr/testify/require"
)

type identityHandler struct {
	identities chan string
}

func (h *identityHandler) NewConnection(ctx context.Context, conn net.Conn, metadata M.Metadata) error {
	identity, _ := auth.UserFromContext[string](ctx)
	h.identities <- identity
	return nil
}

func (h *identityHandler) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata M.Metadata) error {
	return nil
}

func (h *identityHandler) NewError(ctx context.Context, err error) {}

type blockingCloseConn struct {
	net.Conn
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingCloseConn) Close() error {
	c.once.Do(func() {
		close(c.started)
		<-c.release
	})
	return nil
}

func TestServiceUpdateUsersClosesConnectionsWithoutUsersMu(t *testing.T) {
	service := NewService[string](nil)
	conn := &blockingCloseConn{started: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-conn.release:
		default:
			close(conn.release)
		}
	}()
	service.connections.Store(&activeConnection[string]{conn: conn}, struct{}{})

	firstUpdate := make(chan error, 1)
	go func() {
		firstUpdate <- service.UpdateUsers(nil, nil, nil)
	}()
	select {
	case <-conn.started:
	case <-time.After(time.Second):
		t.Fatal("UpdateUsers did not start closing the invalid connection")
	}

	secondUpdate := make(chan error, 1)
	go func() {
		secondUpdate <- service.UpdateUsers(nil, nil, nil)
	}()
	select {
	case err := <-secondUpdate:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("UpdateUsers held usersMu while closing a connection")
	}

	close(conn.release)
	select {
	case err := <-firstUpdate:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("UpdateUsers did not complete after the connection closed")
	}
}

func TestServiceCloseClosesConnectionsWithoutUsersMu(t *testing.T) {
	service := NewService[string](nil)
	conn := &blockingCloseConn{started: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-conn.release:
		default:
			close(conn.release)
		}
	}()
	service.connections.Store(&activeConnection[string]{conn: conn}, struct{}{})

	closeDone := make(chan struct{})
	go func() {
		service.Close()
		close(closeDone)
	}()
	select {
	case <-conn.started:
	case <-time.After(time.Second):
		t.Fatal("Close did not start closing the active connection")
	}

	updateDone := make(chan error, 1)
	go func() {
		updateDone <- service.UpdateUsers(nil, nil, nil)
	}()
	select {
	case err := <-updateDone:
		require.ErrorIs(t, err, net.ErrClosed)
	case <-time.After(time.Second):
		t.Fatal("Close held usersMu while closing a connection")
	}

	close(conn.release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close did not complete after the connection closed")
	}
}

func TestServiceUpdateUsersPublishesCompleteSnapshot(t *testing.T) {
	service := NewService[string](nil)
	firstUUID := "b831381d-6324-4d53-ad4f-8cda48b30811"
	secondUUID := "ed55e0aa-94d3-4d56-a210-7d7b496a8f4c"

	require.NoError(t, service.UpdateUsers(
		[]string{"principal", "principal"},
		[]string{firstUUID, secondUUID},
		[]string{"", "xtls-rprx-vision"},
	))

	snapshot := service.users.Load()
	require.Equal(t, userCredential[string]{user: "principal"}, snapshot.byUUID[utils.UUIDMap(firstUUID)])
	require.Equal(t, userCredential[string]{user: "principal", flow: "xtls-rprx-vision"}, snapshot.byUUID[utils.UUIDMap(secondUUID)])

	require.Error(t, service.UpdateUsers([]string{"principal"}, nil, nil))
	require.Same(t, snapshot, service.users.Load())
}

func TestServiceUpdateUsersRejectsDuplicateUUID(t *testing.T) {
	service := NewService[string](nil)
	require.Error(t, service.UpdateUsers(
		[]string{"first", "second"},
		[]string{"same-id", "same-id"},
		[]string{"", ""},
	))
	require.Empty(t, service.users.Load().byUUID)
}

func TestServiceUpdateUsersRevokesActiveConnections(t *testing.T) {
	service := NewService[string](nil)
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	active := &activeConnection[string]{conn: serverConn}
	service.connections.Store(active, struct{}{})

	require.NoError(t, service.UpdateUsers(nil, nil, nil))
	_, err := clientConn.Read(make([]byte, 1))
	require.Error(t, err)
}

func TestServiceUpdateUsersPreservesUnaffectedConnections(t *testing.T) {
	service := NewService[string](nil)
	firstUUID := "b831381d-6324-4d53-ad4f-8cda48b30811"
	secondUUID := "ed55e0aa-94d3-4d56-a210-7d7b496a8f4c"
	require.NoError(t, service.UpdateUsers(
		[]string{"first", "second"},
		[]string{firstUUID, secondUUID},
		[]string{"", ""},
	))
	snapshot := service.users.Load()
	firstServer, firstClient := net.Pipe()
	defer firstClient.Close()
	secondServer, secondClient := net.Pipe()
	defer secondClient.Close()
	defer secondServer.Close()
	firstID := utils.UUIDMap(firstUUID)
	secondID := utils.UUIDMap(secondUUID)
	first := &activeConnection[string]{conn: firstServer, uuid: firstID, credential: snapshot.byUUID[firstID]}
	second := &activeConnection[string]{conn: secondServer, uuid: secondID, credential: snapshot.byUUID[secondID]}
	service.connections.Store(first, struct{}{})
	service.connections.Store(second, struct{}{})

	require.NoError(t, service.UpdateUsers([]string{"second"}, []string{secondUUID}, []string{""}))
	_, firstActive := service.connections.Load(first)
	_, secondActive := service.connections.Load(second)
	require.False(t, firstActive)
	require.True(t, secondActive)
}

func TestServiceUpdateUsersRotatesCredentialsAndPreservesIdentity(t *testing.T) {
	handler := &identityHandler{identities: make(chan string, 1)}
	service := NewService[string](handler)
	oldUUID := "b831381d-6324-4d53-ad4f-8cda48b30811"
	newUUID := "ed55e0aa-94d3-4d56-a210-7d7b496a8f4c"
	require.NoError(t, service.UpdateUsers([]string{"principal-id"}, []string{oldUUID}, []string{""}))
	require.NoError(t, authenticate(service, oldUUID))
	require.Equal(t, "principal-id", <-handler.identities)

	require.NoError(t, service.UpdateUsers([]string{"principal-id"}, []string{newUUID}, []string{""}))
	require.Error(t, authenticate(service, oldUUID))
	require.NoError(t, authenticate(service, newUUID))
	require.Equal(t, "principal-id", <-handler.identities)
}

func TestServiceCloseRevokesUsersAndCannotBeReopened(t *testing.T) {
	service := NewService[string](nil)
	userUUID := "b831381d-6324-4d53-ad4f-8cda48b30811"
	require.NoError(t, service.UpdateUsers([]string{"principal"}, []string{userUUID}, []string{""}))

	service.Close()
	require.Empty(t, service.users.Load().byUUID)
	require.ErrorIs(t, service.UpdateUsers([]string{"principal"}, []string{userUUID}, []string{""}), net.ErrClosed)
	require.Error(t, authenticate(service, userUUID))
}

func TestListenerRevokesPendingConnections(t *testing.T) {
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
			listener := &Listener{service: NewService[string](nil)}
			serverConn, clientConn := net.Pipe()
			defer clientConn.Close()
			done := make(chan struct{})
			go func() {
				listener.HandleConn(serverConn, nil)
				close(done)
			}()

			require.Eventually(t, func() bool {
				tracked := false
				listener.service.pending.Range(func(_, _ any) bool {
					tracked = true
					return false
				})
				return tracked
			}, time.Second, time.Millisecond)
			require.NoError(t, revoke(listener))
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("pending VLESS connection was not closed")
			}
			_, err := clientConn.Read(make([]byte, 1))
			require.Error(t, err)
		})
	}
}

func authenticate(service *Service[string], userUUID string) error {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	result := make(chan error, 1)
	go func() {
		result <- service.NewConnection(context.Background(), serverConn, M.Metadata{})
	}()

	request := buf.New()
	defer request.Release()
	request.WriteByte(vless.Version)
	userID := utils.UUIDMap(userUUID)
	request.Write(userID[:])
	request.WriteByte(0)
	request.WriteByte(vless.CommandTCP)
	if err := vmess.AddressSerializer.WriteAddrPort(request, M.ParseSocksaddr("example.com:80")); err != nil {
		return err
	}
	writeResult := make(chan error, 1)
	go func() {
		_, err := request.WriteTo(clientConn)
		writeResult <- err
	}()
	err := <-result
	_ = serverConn.Close()
	<-writeResult
	return err
}
