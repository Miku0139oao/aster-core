package vision

import (
	"io"
	"net"
	"testing"

	"github.com/Miku0139oao/aster-core/common/buf"
)

type discardNetConn struct {
	net.Conn
}

func (discardNetConn) Write(p []byte) (int, error) { return len(p), nil }
func (discardNetConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (discardNetConn) Close() error                { return nil }

type nopExtendedWriter struct{}

func (nopExtendedWriter) Write(p []byte) (int, error) { return len(p), nil }
func (nopExtendedWriter) WriteBuffer(buffer *buf.Buffer) error {
	return nil
}

func BenchmarkWriteBufferPaddedRecord(b *testing.B) {
	payload := append([]byte{0x17, 0x03, 0x03, 0x00, 0x10}, make([]byte, 100)...)
	vc := &Conn{
		ExtendedWriter: nopExtendedWriter{},
		netConn:        discardNetConn{},
	}
	vc.packetsToFilter = 8
	vc.isTLS = true
	vc.isTLS12orAbove = true
	vc.writeFilterApplicationData.Store(true)
	buffer := buf.NewSize(2048)
	defer buffer.Release()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vc.packetsToFilter = 8
		vc.writeFilterApplicationData.Store(true)
		vc.writeDirect.Store(false)
		vc.ExtendedWriter = nopExtendedWriter{}
		buffer.Resize(PaddingHeaderLen, 0)
		if _, err := buffer.Write(payload); err != nil {
			b.Fatal(err)
		}
		if err := vc.WriteBuffer(buffer); err != nil {
			b.Fatal(err)
		}
	}
}
