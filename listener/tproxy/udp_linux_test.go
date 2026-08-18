//go:build linux

package tproxy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestParseDSCPSkipsNonTOSControlMessages(t *testing.T) {
	_, err := parseDSCP(&unix.SocketControlMessage{
		Header: unix.Cmsghdr{Level: unix.SOL_IP, Type: unix.IP_RECVORIGDSTADDR},
		Data:   []byte{0},
	})
	require.Error(t, err)

	dscp, err := parseDSCP(&unix.SocketControlMessage{
		Header: unix.Cmsghdr{Level: unix.SOL_IP, Type: unix.IP_TOS},
		Data:   []byte{0xB8},
	})
	require.NoError(t, err)
	require.Equal(t, uint8(0x2e), dscp)
}
