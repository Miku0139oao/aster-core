package core

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUDPMessagePackRoundTrip(t *testing.T) {
	want := udpMessage{
		SessionID: 0x10203040,
		Host:      "example.com",
		Port:      5353,
		MsgID:     42,
		FragID:    1,
		FragCount: 2,
		Data:      []byte("payload"),
	}

	packed := want.Pack()
	require.Len(t, packed, want.Size())

	var got udpMessage
	require.NoError(t, got.Unpack(packed))
	require.Equal(t, want, got)
}

func TestWriteClientRequestUDPFlag(t *testing.T) {
	var wire bytes.Buffer
	require.NoError(t, WriteClientRequest(&wire, ClientRequest{UDP: true}))

	request, err := ReadClientRequest(&wire)
	require.NoError(t, err)
	require.True(t, request.UDP)
}
