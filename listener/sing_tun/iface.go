package sing_tun

import (
	"net"
	"net/netip"
	"sync/atomic"
	"unsafe"

	"github.com/Miku0139oao/aster-core/component/iface"

	"github.com/metacubex/sing/common/control"
)

type defaultInterfaceFinder struct {
	snap atomic.Pointer[ifaceSnapshot]
}

type ifaceSnapshot struct {
	src     map[string]*iface.Interface // keeps the generation alive so srcPtr cannot be recycled
	srcPtr  uintptr
	list    []control.Interface
	byIndex map[int]*iface.Interface
}

func ifaceMapPtr(m map[string]*iface.Interface) uintptr {
	// maps are not comparable; the header pointer identifies a singledo generation.
	return *(*uintptr)(unsafe.Pointer(&m))
}

var DefaultInterfaceFinder control.InterfaceFinder = &defaultInterfaceFinder{}

func (f *defaultInterfaceFinder) Update() error {
	f.snap.Store(nil)
	iface.FlushCache()
	_, err := iface.Interfaces()
	return err
}

func (f *defaultInterfaceFinder) snapshot() (*ifaceSnapshot, error) {
	ifaces, err := iface.Interfaces()
	if err != nil {
		return nil, err
	}
	srcPtr := ifaceMapPtr(ifaces)
	if snap := f.snap.Load(); snap != nil && snap.srcPtr == srcPtr {
		return snap, nil
	}
	list := make([]control.Interface, 0, len(ifaces))
	byIndex := make(map[int]*iface.Interface, len(ifaces))
	for _, netInterface := range ifaces {
		list = append(list, control.Interface(*netInterface))
		byIndex[netInterface.Index] = netInterface
	}
	snap := &ifaceSnapshot{src: ifaces, srcPtr: srcPtr, list: list, byIndex: byIndex}
	f.snap.Store(snap)
	return snap, nil
}

// Interfaces returns a shared snapshot. Callers must treat the slice as read-only.
func (f *defaultInterfaceFinder) Interfaces() []control.Interface {
	snap, err := f.snapshot()
	if err != nil {
		return nil
	}
	return snap.list
}

func (f *defaultInterfaceFinder) ByName(name string) (*control.Interface, error) {
	netInterface, err := iface.ResolveInterface(name)
	if err == nil {
		return (*control.Interface)(netInterface), nil
	}
	if _, err := net.InterfaceByName(name); err == nil {
		err = f.Update()
		if err != nil {
			return nil, err
		}
		return f.ByName(name)
	}
	return nil, err
}

func (f *defaultInterfaceFinder) ByIndex(index int) (*control.Interface, error) {
	snap, err := f.snapshot()
	if err != nil {
		return nil, err
	}
	if netInterface, ok := snap.byIndex[index]; ok {
		return (*control.Interface)(netInterface), nil
	}
	_, err = net.InterfaceByIndex(index)
	if err == nil {
		err = f.Update()
		if err != nil {
			return nil, err
		}
		return f.ByIndex(index)
	}
	return nil, iface.ErrIfaceNotFound
}

func (f *defaultInterfaceFinder) ByAddr(addr netip.Addr) (*control.Interface, error) {
	netInterface, err := iface.ResolveInterfaceByAddr(addr)
	if err != nil {
		return nil, err
	}
	return (*control.Interface)(netInterface), nil
}
