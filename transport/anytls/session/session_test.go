package session

import (
	"bytes"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Miku0139oao/aster-core/transport/anytls/padding"
	"github.com/Miku0139oao/aster-core/transport/anytls/util"
)

type sessionTestConn struct {
	reader *bytes.Reader

	mu     sync.Mutex
	writes bytes.Buffer
}

func (c *sessionTestConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (c *sessionTestConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes.Write(p)
}

func (c *sessionTestConn) Close() error                     { return nil }
func (c *sessionTestConn) LocalAddr() net.Addr              { return nil }
func (c *sessionTestConn) RemoteAddr() net.Addr             { return nil }
func (c *sessionTestConn) SetDeadline(time.Time) error      { return nil }
func (c *sessionTestConn) SetReadDeadline(time.Time) error  { return nil }
func (c *sessionTestConn) SetWriteDeadline(time.Time) error { return nil }

func appendSessionTestFrame(dst []byte, cmd byte, sid uint32, data []byte) []byte {
	header := make([]byte, headerOverHeadSize)
	header[0] = cmd
	binary.BigEndian.PutUint32(header[1:5], sid)
	binary.BigEndian.PutUint16(header[5:7], uint16(len(data)))
	dst = append(dst, header...)
	return append(dst, data...)
}

func TestRecvLoopConsumesUnexpectedFramePayloads(t *testing.T) {
	paddingFactory := padding.NewPaddingFactory(padding.DefaultPaddingScheme)
	var paddingPtr atomic.Pointer[padding.PaddingFactory]
	paddingPtr.Store(paddingFactory)
	settings := util.StringMap{
		"v":           "2",
		"padding-md5": paddingFactory.Md5,
	}.ToBytes()

	var input []byte
	input = appendSessionTestFrame(input, cmdSettings, 0, settings)
	input = appendSessionTestFrame(input, cmdSYN, 1, []byte("unexpected syn payload"))
	input = appendSessionTestFrame(input, cmdFIN, 1, []byte("unexpected fin payload"))
	input = appendSessionTestFrame(input, cmdHeartRequest, 0, []byte("unexpected heartbeat payload"))
	input = appendSessionTestFrame(input, cmdHeartResponse, 0, []byte("unexpected response payload"))
	input = appendSessionTestFrame(input, 0xfe, 0, []byte("unknown command payload"))
	input = appendSessionTestFrame(input, cmdSYN, 2, nil)

	conn := &sessionTestConn{reader: bytes.NewReader(input)}
	streams := make(chan uint32, 2)
	s := NewServerSession(conn, func(stream *Stream) {
		streams <- stream.id
	}, &paddingPtr)

	s.recvLoop()

	got := make(map[uint32]bool)
	for i := 0; i < 2; i++ {
		select {
		case sid := <-streams:
			got[sid] = true
		case <-time.After(time.Second):
			t.Fatal("recvLoop stopped before consuming an unexpected frame payload")
		}
	}
	require.True(t, got[1])
	require.True(t, got[2])
}

func TestRecvLoopStopsOnEmptyAlert(t *testing.T) {
	var input []byte
	input = appendSessionTestFrame(input, cmdAlert, 0, nil)
	input = appendSessionTestFrame(input, cmdSYN, 1, nil)

	conn := &sessionTestConn{reader: bytes.NewReader(input)}
	streams := make(chan struct{}, 1)
	paddingFactory := padding.NewPaddingFactory(padding.DefaultPaddingScheme)
	var paddingPtr atomic.Pointer[padding.PaddingFactory]
	paddingPtr.Store(paddingFactory)
	s := NewServerSession(conn, func(*Stream) { streams <- struct{}{} }, &paddingPtr)

	s.recvLoop()
	select {
	case <-streams:
		t.Fatal("session continued after an empty alert")
	case <-time.After(100 * time.Millisecond):
	}
}
