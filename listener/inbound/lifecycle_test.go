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

func TestNewTunKernelDirectMaxEntriesContract(t *testing.T) {
	tun, err := NewTun(&TunOption{BaseOption: BaseOption{NameStr: "tun", Listen: "127.0.0.1", Port: "0"}})
	require.NoError(t, err)
	require.Equal(t, uint32(4096), tun.tun.KernelDirectMaxEntries)
	config, ok := tun.Config().(*TunOption)
	require.True(t, ok)
	require.Equal(t, uint32(4096), config.KernelDirectMaxEntries)

	_, err = NewTun(&TunOption{
		BaseOption:             BaseOption{NameStr: "tun", Listen: "127.0.0.1", Port: "0"},
		KernelDirectMaxEntries: 65537,
	})
	require.Error(t, err)
}
