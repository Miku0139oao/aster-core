package sing_shadowsocks

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/listener/sing"

	shadowsocks "github.com/metacubex/sing-shadowsocks"
	"github.com/metacubex/sing-shadowsocks/shadowaead"
	"github.com/metacubex/sing-shadowsocks/shadowaead_2022"
	"github.com/metacubex/sing/common/buf"
	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
	"github.com/stretchr/testify/require"
)

var errStubPacket = errors.New("stub packet reject")

type stubPacketService struct {
	err      error
	lastCtx  context.Context
	lastMeta M.Metadata
	held     *buf.Buffer
}

func (s *stubPacketService) NewPacket(ctx context.Context, _ N.PacketConn, buffer *buf.Buffer, metadata M.Metadata) error {
	s.lastCtx = ctx
	s.lastMeta = metadata
	if s.err != nil {
		return s.err
	}
	s.held = buffer
	return nil
}

type stubPacketConn struct{}

func (stubPacketConn) ReadPacket(*buf.Buffer) (M.Socksaddr, error) {
	return M.Socksaddr{}, net.ErrClosed
}
func (stubPacketConn) WritePacket(*buf.Buffer, M.Socksaddr) error { return nil }
func (stubPacketConn) Close() error                               { return nil }
func (stubPacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}
func (stubPacketConn) SetDeadline(time.Time) error      { return nil }
func (stubPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (stubPacketConn) SetWriteDeadline(time.Time) error { return nil }

type nopSSHandler struct{}

func (nopSSHandler) NewConnection(context.Context, net.Conn, M.Metadata) error { return nil }
func (nopSSHandler) NewPacketConnection(context.Context, N.PacketConn, M.Metadata) error {
	return nil
}
func (nopSSHandler) NewError(context.Context, error) {}

func TestDispatchInboundPacketReleasesOnError(t *testing.T) {
	svc := &stubPacketService{err: errStubPacket}
	buff := buf.NewPacket()
	_, err := buff.Write([]byte("reject-me"))
	require.NoError(t, err)
	require.Greater(t, buff.RawCap(), 0)

	err = dispatchInboundPacket(context.TODO(), svc, stubPacketConn{}, buff, testUDPSource())
	require.ErrorIs(t, err, errStubPacket)
	require.Nil(t, svc.held)
	require.Equal(t, 0, buff.RawCap())
}

func TestDispatchInboundPacketKeepsSuccessfulBuffer(t *testing.T) {
	svc := &stubPacketService{}
	payload := []byte("keep-me-until-owner-releases")
	buff := buf.NewPacket()
	_, err := buff.Write(payload)
	require.NoError(t, err)

	src := testUDPSource()
	err = dispatchInboundPacket(context.TODO(), svc, stubPacketConn{}, buff, src)
	require.NoError(t, err)
	require.Same(t, buff, svc.held)
	require.Equal(t, payload, svc.held.Bytes())
	require.Greater(t, svc.held.RawCap(), 0)
	require.Equal(t, src, svc.lastMeta.Source)
	require.Equal(t, "shadowsocks", svc.lastMeta.Protocol)

	svc.held.Release()
	require.Equal(t, 0, buff.RawCap())
}

func TestDispatchInboundPacketDeliversHoistedInAddrAndKeepsSourceInMetadata(t *testing.T) {
	local := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 8388}
	pktCtx := sing.WithInAddr(context.TODO(), local)
	src := testUDPSource()
	svc := &stubPacketService{}
	buff := buf.NewPacket()
	defer buff.Release()
	_, err := buff.Write([]byte("x"))
	require.NoError(t, err)

	require.NoError(t, dispatchInboundPacket(pktCtx, svc, stubPacketConn{}, buff, src))
	// getInAddr is unexported in listener/sing; delivery is the same ctx instance.
	require.Equal(t, pktCtx, svc.lastCtx)
	require.NotEqual(t, context.TODO(), svc.lastCtx)
	require.Equal(t, src, svc.lastMeta.Source)
	require.Equal(t, "shadowsocks", svc.lastMeta.Protocol)
	require.Empty(t, sing.AdditionsFromContext(pktCtx))
}

func TestDispatchInboundPacketActualServiceRejectsShortAndBadTag(t *testing.T) {
	aead := mustAEADService(t)
	ss2022 := must2022Service(t)
	none := shadowsocks.NewNoneService(60, nopSSHandler{})

	short := []struct {
		name string
		svc  shadowsocks.Service
	}{
		{name: "none", svc: none},
		{name: "aes-128-gcm", svc: aead},
		{name: "2022-blake3-aes-128-gcm", svc: ss2022},
	}
	for _, tc := range short {
		t.Run(tc.name+"/short", func(t *testing.T) {
			assertServiceRejects(t, tc.svc, []byte{0x01})
		})
	}

	// none has no AEAD tag; 0x01 + 64 bytes is a valid IPv4 SOCKS address and would enqueue.
	t.Run("none/bad-addr", func(t *testing.T) {
		assertServiceRejects(t, none, []byte{0xff})
	})
	for _, tc := range []struct {
		name string
		svc  shadowsocks.Service
	}{
		{name: "aes-128-gcm", svc: aead},
		{name: "2022-blake3-aes-128-gcm", svc: ss2022},
	} {
		t.Run(tc.name+"/bad-tag", func(t *testing.T) {
			garbage := make([]byte, 64)
			for i := range garbage {
				garbage[i] = byte(i + 1)
			}
			assertServiceRejects(t, tc.svc, garbage)
		})
	}
}

func assertServiceRejects(t *testing.T, svc shadowsocks.Service, payload []byte) {
	t.Helper()
	buff := buf.NewPacket()
	_, err := buff.Write(payload)
	require.NoError(t, err)
	err = dispatchInboundPacket(context.TODO(), svc, stubPacketConn{}, buff, testUDPSource())
	require.Error(t, err)
	require.Equal(t, 0, buff.RawCap())
}

func BenchmarkDispatchInboundPacketError(b *testing.B) {
	svc := &stubPacketService{err: errStubPacket}
	payload := []byte("x")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buff := buf.NewPacket()
		_, _ = buff.Write(payload)
		_ = dispatchInboundPacket(context.TODO(), svc, stubPacketConn{}, buff, M.Socksaddr{})
	}
}

func testUDPSource() M.Socksaddr {
	return M.SocksaddrFromNet(&net.UDPAddr{IP: net.IPv4(198, 51, 100, 10), Port: 53})
}

func mustAEADService(t *testing.T) shadowsocks.Service {
	t.Helper()
	svc, err := shadowaead.NewService("aes-128-gcm", nil, "l21-ss-password", 60, nopSSHandler{})
	require.NoError(t, err)
	return svc
}

func must2022Service(t *testing.T) shadowsocks.Service {
	t.Helper()
	psk := make([]byte, 16)
	for i := range psk {
		psk[i] = byte(i + 3)
	}
	svc, err := shadowaead_2022.NewServiceWithPassword(
		"2022-blake3-aes-128-gcm",
		base64.StdEncoding.EncodeToString(psk),
		60,
		nopSSHandler{},
		time.Now,
	)
	require.NoError(t, err)
	return svc
}
