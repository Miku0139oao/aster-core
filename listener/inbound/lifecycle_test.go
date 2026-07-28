package inbound

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailedInterfaceBackedListenersRemainClosable(t *testing.T) {
	vmess, err := NewVmess(&VmessOption{
		BaseOption:  BaseOption{NameStr: "vmess", Listen: "127.0.0.1", Port: "0"},
		Users:       []VmessUser{{UUID: "00000000-0000-0000-0000-000000000001"}},
		Certificate: "invalid certificate",
		PrivateKey:  "invalid private key",
	})
	require.NoError(t, err)
	require.Error(t, vmess.Listen(nil))
	require.Nil(t, vmess.l)
	require.NoError(t, vmess.Close())

	shadowsocks, err := NewShadowSocks(&ShadowSocksOption{
		BaseOption: BaseOption{NameStr: "shadowsocks", Listen: "127.0.0.1", Port: "0"},
		Cipher:     "invalid",
	})
	require.NoError(t, err)
	require.Error(t, shadowsocks.Listen(nil))
	require.Nil(t, shadowsocks.l)
	require.NoError(t, shadowsocks.Close())
}

func TestTunAddressIsEmptyWhenStopped(t *testing.T) {
	tun, err := NewTun(&TunOption{BaseOption: BaseOption{NameStr: "tun", Listen: "127.0.0.1", Port: "0"}})
	require.NoError(t, err)
	require.Empty(t, tun.Address())
	require.NoError(t, tun.Close())
	require.Empty(t, tun.Address())
}
