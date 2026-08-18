package mixed

import (
	"net"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

type stubTunnel struct{}

func (stubTunnel) HandleTCPConn(net.Conn, *C.Metadata)      {}
func (stubTunnel) HandleUDPPacket(C.UDPPacket, *C.Metadata) {}
func (stubTunnel) NatTable() C.NatTable                     { return nil }

func TestListenerCloseStopsAcceptLoop(t *testing.T) {
	l, err := New("127.0.0.1:0", stubTunnel{})
	if err != nil {
		t.Skip(err.Error())
	}
	addr := l.Address()
	require.NoError(t, l.Close())

	_, err = net.Dial("tcp", addr)
	require.Error(t, err)
}

func TestListenerCloseRacesWithAccept(t *testing.T) {
	for i := 0; i < 16; i++ {
		l, err := New("127.0.0.1:0", stubTunnel{})
		if err != nil {
			t.Skip(err.Error())
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, err := net.Dial("tcp", l.Address())
			if err == nil {
				_ = conn.Close()
			}
		}()
		require.NoError(t, l.Close())
		<-done
	}
}
