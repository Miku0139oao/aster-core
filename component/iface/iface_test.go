package iface

import (
	"net"
	"net/netip"
	"sync"
	"testing"
)

func TestFlushCacheRebuilds(t *testing.T) {
	ifaces, err := Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(ifaces) == 0 {
		t.Skip("no interfaces")
	}
	FlushCache()
	again, err := Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) == 0 {
		t.Fatal("cache rebuild returned no interfaces")
	}
}

func TestIsLocalIpConcurrent(t *testing.T) {
	addr := netip.MustParseAddr("127.0.0.1")
	if _, err := IsLocalIp(addr); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if _, err := IsLocalIp(addr); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestFlushCacheConcurrentWithLookup(t *testing.T) {
	addr := netip.MustParseAddr("127.0.0.1")
	if _, err := IsLocalIp(addr); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := IsLocalIp(addr); err != nil {
					t.Error(err)
					return
				}
				if j%17 == 0 {
					FlushCache()
				}
			}
		}()
	}
	wg.Wait()
}

func TestResolveInterfaceUnknown(t *testing.T) {
	_, err := ResolveInterface("this-iface-does-not-exist-aster-l17")
	if err != ErrIfaceNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestPickIPv4AddrNoClosure(t *testing.T) {
	ifi, err := net.InterfaceByIndex(1)
	if err != nil {
		t.Skip(err)
	}
	resolved, err := ResolveInterface(ifi.Name)
	if err != nil {
		t.Skip(err)
	}
	_, err = resolved.PickIPv4Addr(netip.Addr{})
	if err != nil && err != ErrAddrNotFound {
		t.Fatal(err)
	}
}
