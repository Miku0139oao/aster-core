package session

import (
	"encoding/binary"
	"sync/atomic"
	"testing"

	"github.com/Miku0139oao/aster-core/transport/anytls/padding"

	"github.com/stretchr/testify/require"
)

type captureConn struct {
	discardConn
	writes [][]byte
}

func (c *captureConn) Write(p []byte) (int, error) {
	c.writes = append(c.writes, append([]byte(nil), p...))
	return len(p), nil
}

func TestWritePaddedFrameKeepsSingleRecordAndZeroWaste(t *testing.T) {
	factory := padding.NewPaddingFactory([]byte("stop=1000000\n1=64-64"))
	var paddingFactory atomic.Pointer[padding.PaddingFactory]
	paddingFactory.Store(factory)

	conn := &captureConn{}
	session := &Session{
		conn:        conn,
		sendPadding: true,
		padding:     &paddingFactory,
	}
	payload := []byte("hello-anytls-pad") // 16 bytes
	n, err := session.writeDataFrame(7, payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Len(t, conn.writes, 1)
	got := conn.writes[0]
	require.Len(t, got, 64)
	require.Equal(t, byte(cmdPSH), got[0])
	require.Equal(t, uint32(7), binary.BigEndian.Uint32(got[1:5]))
	require.Equal(t, uint16(len(payload)), binary.BigEndian.Uint16(got[5:7]))
	require.Equal(t, payload, got[headerOverHeadSize:headerOverHeadSize+len(payload)])
	off := headerOverHeadSize + len(payload)
	require.Equal(t, byte(cmdWaste), got[off])
	require.Equal(t, uint32(0), binary.BigEndian.Uint32(got[off+1:off+5]))
	paddingLen := 64 - off - headerOverHeadSize
	require.Equal(t, uint16(paddingLen), binary.BigEndian.Uint16(got[off+5:off+7]))
	require.Equal(t, make([]byte, paddingLen), got[off+headerOverHeadSize:])
}

func pshFrameExact(sid uint32, payload []byte) []byte {
	frame := make([]byte, headerOverHeadSize+len(payload))
	frame[0] = cmdPSH
	binary.BigEndian.PutUint32(frame[1:5], sid)
	binary.BigEndian.PutUint16(frame[5:7], uint16(len(payload)))
	copy(frame[headerOverHeadSize:], payload)
	return frame
}

func TestWriteConnDropsPaddingScratchAtStop(t *testing.T) {
	factory := padding.NewPaddingFactory([]byte("stop=3\n1=64-64\n2=64-64"))
	var paddingFactory atomic.Pointer[padding.PaddingFactory]
	paddingFactory.Store(factory)

	conn := &captureConn{}
	session := &Session{
		conn:        conn,
		sendPadding: true,
		padding:     &paddingFactory,
	}
	payload := []byte("hello-anytls-pad") // 16 bytes

	frameLen := headerOverHeadSize + len(payload)
	n, err := session.writeConn(pshFrameExact(7, payload))
	require.NoError(t, err)
	require.Equal(t, frameLen, n)
	require.True(t, session.sendPadding)
	require.NotEmpty(t, session.pktSizes)
	require.Greater(t, cap(session.padBuf), 0)
	warmCap := cap(session.padBuf)
	require.Len(t, conn.writes, 1)
	require.Len(t, conn.writes[0], 64)

	n, err = session.writeConn(pshFrameExact(7, payload))
	require.NoError(t, err)
	require.Equal(t, frameLen, n)
	require.True(t, session.sendPadding)
	require.NotEmpty(t, session.pktSizes)
	require.GreaterOrEqual(t, cap(session.padBuf), warmCap)
	require.Len(t, conn.writes, 2)
	require.Len(t, conn.writes[1], 64)

	frame := pshFrameExact(7, payload)
	n, err = session.writeConn(frame)
	require.NoError(t, err)
	require.Equal(t, len(frame), n)
	require.False(t, session.sendPadding)
	require.Nil(t, session.padBuf)
	require.Nil(t, session.pktSizes)
	unpadded := conn.writes[len(conn.writes)-1]
	require.Equal(t, frame, unpadded)

	writesAfterStop := len(conn.writes)
	frame = pshFrameExact(7, payload)
	n, err = session.writeConn(frame)
	require.NoError(t, err)
	require.Equal(t, len(frame), n)
	require.False(t, session.sendPadding)
	require.Nil(t, session.padBuf)
	require.Nil(t, session.pktSizes)
	require.Len(t, conn.writes, writesAfterStop+1)
	require.Equal(t, frame, conn.writes[len(conn.writes)-1])

	n, err = session.writeDataFrame(7, payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Nil(t, session.padBuf)
	require.Nil(t, session.pktSizes)
	last := conn.writes[len(conn.writes)-1]
	require.Equal(t, byte(cmdPSH), last[0])
	require.Equal(t, uint32(7), binary.BigEndian.Uint32(last[1:5]))
	require.Equal(t, uint16(len(payload)), binary.BigEndian.Uint16(last[5:7]))
	require.Equal(t, payload, last[headerOverHeadSize:])
	require.Len(t, last, headerOverHeadSize+len(payload))
}
