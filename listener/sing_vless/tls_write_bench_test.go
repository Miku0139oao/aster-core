package sing_vless

import (
	"crypto/tls"
	"io"
	"net"
	"testing"

	"github.com/Miku0139oao/aster-core/component/ca"

	"github.com/metacubex/sing/common/bufio"
	M "github.com/metacubex/sing/common/metadata"
)

func tlsTestConfigs(tb testing.TB) (*tls.Config, *tls.Config) {
	tb.Helper()
	certPEM, keyPEM, _, err := ca.NewRandomTLSKeyPair(ca.KeyPairTypeP256)
	if err != nil {
		tb.Fatal(err)
	}
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		tb.Fatal(err)
	}
	server := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	client := &tls.Config{ServerName: "localhost", MinVersion: tls.VersionTLS13, InsecureSkipVerify: true} //nolint:gosec // self-signed benchmark cert
	return server, client
}

// BenchmarkWriteToOverTLS measures one VLESS UDP datagram end to end through a
// real TLS connection, which is the transport this code actually runs on. The
// plain discardNetConn benchmark cannot price the TLS record boundary, and that
// boundary is the entire reason two Write calls were suspected of costing extra.
func BenchmarkWriteToOverTLS(b *testing.B) {
	serverCfg, clientCfg := tlsTestConfigs(b)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()

	clientDone := make(chan net.Conn, 1)
	go func() {
		raw, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			clientDone <- nil
			return
		}
		c := tls.Client(raw, clientCfg)
		if err := c.Handshake(); err != nil {
			clientDone <- nil
			return
		}
		clientDone <- c
	}()

	rawServer, err := ln.Accept()
	if err != nil {
		b.Fatal(err)
	}
	serverTLS := tls.Server(rawServer, serverCfg)
	if err := serverTLS.Handshake(); err != nil {
		b.Fatal(err)
	}
	client := <-clientDone
	if client == nil {
		b.Fatal("client handshake failed")
	}
	defer client.Close()

	// Drain whatever the server writes.
	go func() {
		_, _ = io.Copy(io.Discard, client)
	}()

	conn := newServerPacketConn(bufio.NewExtendedConn(serverTLS), M.ParseSocksaddr("192.0.2.1:443"))
	payload := make([]byte, 512)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := conn.WriteTo(payload, nil); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	_ = serverTLS.Close()
}
