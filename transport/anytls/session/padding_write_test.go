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
