package session

import (
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/transport/anytls/padding"
)

type discardConn struct{}

func (discardConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (discardConn) Write(p []byte) (int, error)      { return len(p), nil }
func (discardConn) Close() error                     { return nil }
func (discardConn) LocalAddr() net.Addr              { return nil }
func (discardConn) RemoteAddr() net.Addr             { return nil }
func (discardConn) SetDeadline(time.Time) error      { return nil }
func (discardConn) SetReadDeadline(time.Time) error  { return nil }
func (discardConn) SetWriteDeadline(time.Time) error { return nil }

func BenchmarkWriteDataFrame(b *testing.B) {
	for _, size := range []int{1024, 16 * 1024, 64 * 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			session := &Session{conn: discardConn{}, sendPadding: false}
			payload := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := session.writeDataFrame(1, payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWritePaddedFrame(b *testing.B) {
	factory := padding.NewPaddingFactory([]byte("stop=1000000\n1=64-64,c,128-128"))
	var paddingFactory atomic.Pointer[padding.PaddingFactory]
	paddingFactory.Store(factory)
	payload := make([]byte, 16)
	session := &Session{
		conn:        discardConn{},
		sendPadding: true,
		padding:     &paddingFactory,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.pktCounter.Store(0)
		session.sendPadding = true
		if _, err := session.writeDataFrame(1, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSessionUpload(b *testing.B) {
	for _, size := range []int{1024, 16 * 1024, 64 * 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			clientConn, serverConn := net.Pipe()
			var paddingFactory atomic.Pointer[padding.PaddingFactory]
			paddingFactory.Store(padding.NewPaddingFactory(padding.DefaultPaddingScheme))

			serverStreamCh := make(chan *Stream, 1)
			serverSession := NewServerSession(serverConn, func(stream *Stream) {
				serverStreamCh <- stream
			}, &paddingFactory)
			go serverSession.Run()

			clientSession := NewClientSession(clientConn, &paddingFactory, "")
			clientSession.Run()
			clientStream, err := clientSession.OpenStream()
			if err != nil {
				b.Fatal(err)
			}

			firstWrite := make(chan error, 1)
			go func() {
				_, err := clientStream.Write([]byte{0})
				firstWrite <- err
			}()
			serverStream := <-serverStreamCh
			copyDone := make(chan struct{})
			go func() {
				_, _ = io.Copy(io.Discard, serverStream)
				close(copyDone)
			}()
			if err := <-firstWrite; err != nil {
				b.Fatal(err)
			}

			b.Cleanup(func() {
				_ = clientSession.Close()
				_ = serverSession.Close()
				<-copyDone
			})

			payload := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := clientStream.Write(payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
