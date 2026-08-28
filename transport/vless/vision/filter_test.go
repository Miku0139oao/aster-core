package vision

import (
	"testing"
)

func tlsServerHello13() []byte {
	// Minimal record that trips FilterTLS TLS 1.3 detection without being a
	// valid handshake. Layout matches the offsets FilterTLS reads.
	buf := make([]byte, 128)
	copy(buf, tlsServerHandshakeStart)
	buf[5] = tlsHandshakeTypeServerHello
	buf[3] = 0
	buf[4] = 120 // remainingServerHello = 125
	buf[43] = 0  // session ID length
	buf[44] = 0x13
	buf[45] = 0x01 // TLS_AES_128_GCM_SHA256
	copy(buf[50:], tls13SupportedVersions)
	return buf
}

func BenchmarkFilterTLSServerHello(b *testing.B) {
	hello := tlsServerHello13()
	vc := &Conn{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vc.packetsToFilter = 8
		vc.remainingServerHello = 0
		vc.isTLS = false
		vc.isTLS12orAbove = false
		vc.enableXTLS = false
		vc.cipher = 0
		vc.FilterTLS(hello)
	}
}

func BenchmarkFilterTLSDone(b *testing.B) {
	payload := make([]byte, 1400)
	vc := &Conn{packetsToFilter: 0}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vc.FilterTLS(payload)
	}
}
