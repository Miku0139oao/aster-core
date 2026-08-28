package encryption

import (
	"io"
	"net"
	"testing"
)

type discardConn struct {
	net.Conn
}

func (discardConn) Write(p []byte) (int, error) { return len(p), nil }
func (discardConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (discardConn) Close() error                { return nil }

type replayConn struct {
	net.Conn
	frame  []byte
	offset int
}

func (c *replayConn) Read(p []byte) (int, error) {
	if c.offset >= len(c.frame) {
		c.offset = 0
	}
	n := copy(p, c.frame[c.offset:])
	c.offset += n
	return n, nil
}

func (c *replayConn) Close() error { return nil }

func tlsRecord(n int) []byte {
	b := make([]byte, 5+n)
	EncodeHeader(b, n)
	return b
}

func BenchmarkXorConnWriteRecord(b *testing.B) {
	key := make([]byte, 32)
	iv := make([]byte, 16)
	template := tlsRecord(1400)
	payload := make([]byte, len(template))
	conn := NewXorConn(discardConn{}, NewCTR(key, iv), NewCTR(key, iv), 0, 0)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(payload, template)
		if _, err := conn.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkXorConnReadRecord(b *testing.B) {
	key := make([]byte, 32)
	iv := make([]byte, 16)
	frame := tlsRecord(1400)
	enc := NewXorConn(discardConn{}, NewCTR(key, iv), NewCTR(key, iv), 0, 0)
	if _, err := enc.Write(frame); err != nil {
		b.Fatal(err)
	}
	conn := NewXorConn(&replayConn{frame: frame}, NewCTR(key, iv), NewCTR(key, iv), 0, 0)
	out := make([]byte, len(frame))
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n, err := conn.Read(out)
		if err != nil {
			b.Fatal(err)
		}
		if n != len(frame) {
			b.Fatalf("n=%d", n)
		}
	}
}

func BenchmarkCommonConnWrite(b *testing.B) {
	united := make([]byte, 64)
	ctx := make([]byte, 32)
	payload := make([]byte, 1400)
	c := NewCommonConn(discardConn{}, true)
	c.UnitedKey = united
	c.AEAD = NewAEAD(ctx, united, true)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
