package process

import (
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/Miku0139oao/aster-core/log"

	"golang.org/x/sys/windows"
)

const (
	tcpTableFunc      = "GetExtendedTcpTable"
	tcpTablePidConn   = 4
	udpTableFunc      = "GetExtendedUdpTable"
	udpTablePid       = 1
	queryProcNameFunc = "QueryFullProcessImageNameW"
)

var (
	getExTCPTable uintptr
	getExUDPTable uintptr
	queryProcName uintptr

	once sync.Once

	lastTableSize [4]uint32

	// Occupancy is not bounded by GOMAXPROCS; oversized backing arrays are dropped.
	tableScratchPools [4]sync.Pool // *tableScratch
)

type tableScratch struct {
	buf []byte
}

func resolveSocketByNetlink(network string, ip netip.Addr, srcPort int) (uint32, uint32, error) {
	return 0, 0, ErrPlatformNotSupport
}

func initWin32API() error {
	h, err := windows.LoadLibrary("iphlpapi.dll")
	if err != nil {
		return fmt.Errorf("LoadLibrary iphlpapi.dll failed: %s", err.Error())
	}

	getExTCPTable, err = windows.GetProcAddress(h, tcpTableFunc)
	if err != nil {
		return fmt.Errorf("GetProcAddress of %s failed: %s", tcpTableFunc, err.Error())
	}

	getExUDPTable, err = windows.GetProcAddress(h, udpTableFunc)
	if err != nil {
		return fmt.Errorf("GetProcAddress of %s failed: %s", udpTableFunc, err.Error())
	}

	h, err = windows.LoadLibrary("kernel32.dll")
	if err != nil {
		return fmt.Errorf("LoadLibrary kernel32.dll failed: %s", err.Error())
	}

	queryProcName, err = windows.GetProcAddress(h, queryProcNameFunc)
	if err != nil {
		return fmt.Errorf("GetProcAddress of %s failed: %s", queryProcNameFunc, err.Error())
	}

	return nil
}

func findProcessName(network string, ip netip.Addr, srcPort int) (uint32, string, error) {
	once.Do(func() {
		err := initWin32API()
		if err != nil {
			log.Errorln("Initialize PROCESS-NAME failed: %s", err.Error())
			log.Warnln("All PROCESS-NAMES rules will be skipped")
			return
		}
	})
	isV4 := !ip.Is6()
	family := windows.AF_INET
	if !isV4 {
		family = windows.AF_INET6
	}

	var class int
	var fn uintptr
	isTCP := false
	switch network {
	case TCP:
		fn = getExTCPTable
		class = tcpTablePidConn
		isTCP = true
	case UDP:
		fn = getExUDPTable
		class = udpTablePid
	default:
		return 0, "", ErrInvalidNetwork
	}

	s := cachedSearcher(isV4, isTCP)
	slot := tableSlot(family, class)
	scratch := acquireTableScratch(slot)
	used, err := fillTransportTable(scratch, fn, family, class)
	if err != nil {
		releaseTableScratch(slot, scratch, used)
		return 0, "", err
	}
	pid, err := s.Search(scratch.buf, ip, uint16(srcPort))
	releaseTableScratch(slot, scratch, used)
	if err != nil {
		return 0, "", err
	}
	pp, err := getExecPathFromPID(pid)
	return 0, pp, err
}

type searcher struct {
	itemSize int
	port     int
	ip       int
	ipSize   int
	pid      int
	tcpState int
}

func (s searcher) Search(b []byte, ip netip.Addr, port uint16) (uint32, error) {
	if len(b) < 4 || s.itemSize <= 0 {
		return 0, ErrNotFound
	}
	n := int(readNativeUint32(b))
	maxN := (len(b) - 4) / s.itemSize
	if n > maxN {
		n = maxN
	}

	wantPort := portToNativeUint32(port)
	checkState := s.tcpState >= 0
	stateOff := s.tcpState
	portOff := s.port
	ipOff := s.ip
	pidOff := s.pid
	itemSize := s.itemSize
	ipSize := s.ipSize

	fast4 := ipSize == 4 && ip.Is4()
	var want4 uint32
	if fast4 {
		*(*[4]byte)(unsafe.Pointer(&want4)) = ip.As4()
	}

	off := 4
	for i := 0; i < n; i++ {
		row := b[off:]
		off += itemSize
		if checkState && readNativeUint32(row[stateOff:]) != 5 {
			continue
		}
		if readNativeUint32(row[portOff:]) != wantPort {
			continue
		}
		if fast4 {
			got := readNativeUint32(row[ipOff:])
			if got == want4 || (!checkState && got == 0) {
				return readNativeUint32(row[pidOff:]), nil
			}
			continue
		}
		srcIP, ok := netip.AddrFromSlice(row[ipOff : ipOff+ipSize])
		if !ok {
			continue
		}
		srcIP = srcIP.Unmap()
		if ip != srcIP && (checkState || !srcIP.IsUnspecified()) {
			continue
		}
		return readNativeUint32(row[pidOff:]), nil
	}
	return 0, ErrNotFound
}

func cachedSearcher(isV4, isTCP bool) searcher {
	switch {
	case isV4 && isTCP:
		// struct MIB_TCPROW_OWNER_PID
		return searcher{itemSize: 24, port: 8, ip: 4, ipSize: 4, pid: 20, tcpState: 0}
	case isV4 && !isTCP:
		// struct MIB_UDPROW_OWNER_PID
		return searcher{itemSize: 12, port: 4, ip: 0, ipSize: 4, pid: 8, tcpState: -1}
	case !isV4 && isTCP:
		// struct MIB_TCP6ROW_OWNER_PID
		return searcher{itemSize: 56, port: 20, ip: 0, ipSize: 16, pid: 52, tcpState: 48}
	default:
		// struct MIB_UDP6ROW_OWNER_PID
		return searcher{itemSize: 28, port: 20, ip: 0, ipSize: 16, pid: 24, tcpState: -1}
	}
}

func newSearcher(isV4, isTCP bool) searcher {
	return cachedSearcher(isV4, isTCP)
}

func tableSlot(family int, class int) int {
	slot := 0
	if family == windows.AF_INET6 {
		slot = 2
	}
	if class == udpTablePid {
		slot++
	}
	return slot
}

func acquireTableScratch(slot int) *tableScratch {
	if slot < 0 || slot >= len(tableScratchPools) {
		return &tableScratch{}
	}
	if v := tableScratchPools[slot].Get(); v != nil {
		return v.(*tableScratch)
	}
	return &tableScratch{}
}

func releaseTableScratch(slot int, s *tableScratch, used int) {
	if s == nil {
		return
	}
	if !shouldKeepTransportScratch(cap(s.buf), used) {
		s.buf = nil
	}
	if slot < 0 || slot >= len(tableScratchPools) {
		return
	}
	tableScratchPools[slot].Put(s)
}

func shouldKeepTransportScratch(ncap, used int) bool {
	limit := 4096
	if used > limit/2 {
		limit = used * 2
	}
	return ncap <= limit
}

// getTransportTable dumps a fresh table into a caller-owned buffer (never a pooled slice).
func getTransportTable(fn uintptr, family int, class int) ([]byte, error) {
	var s tableScratch
	if _, err := fillTransportTable(&s, fn, family, class); err != nil {
		return nil, err
	}
	return s.buf, nil
}

func fillTransportTable(s *tableScratch, fn uintptr, family int, class int) (int, error) {
	slot := tableSlot(family, class)
	size := atomic.LoadUint32(&lastTableSize[slot])
	if size < 256 {
		size = 4096
	}

	buf := s.buf
	if uint32(cap(buf)) < size {
		buf = make([]byte, size)
	} else {
		buf = buf[:size]
	}
	for {
		if len(buf) == 0 {
			buf = make([]byte, 8)
			size = 8
		}
		ptr := unsafe.Pointer(&buf[0])
		err, _, _ := syscall.Syscall6(fn, 6, uintptr(ptr), uintptr(unsafe.Pointer(&size)), 0, uintptr(family), uintptr(class), 0)

		switch err {
		case 0:
			stored := size
			if stored < ^uint32(0)-512 {
				stored += 512
			}
			atomic.StoreUint32(&lastTableSize[slot], stored)
			if size > uint32(len(buf)) {
				size = uint32(len(buf))
			}
			s.buf = buf[:size]
			return int(size), nil
		case uintptr(syscall.ERROR_INSUFFICIENT_BUFFER):
			if size <= uint32(cap(buf)) {
				// kernel asked for a size we already have; avoid a tight loop
				if size == uint32(len(buf)) {
					size++
				}
			}
			if uint32(cap(buf)) < size {
				buf = make([]byte, size)
			} else {
				buf = buf[:size]
			}
		default:
			s.buf = buf
			return 0, fmt.Errorf("syscall error: %d", err)
		}
	}
}

func readNativeUint32(b []byte) uint32 {
	return *(*uint32)(unsafe.Pointer(&b[0]))
}

func portToNativeUint32(port uint16) uint32 {
	var v uint32
	b := (*[4]byte)(unsafe.Pointer(&v))
	b[0] = byte(port >> 8)
	b[1] = byte(port)
	return v
}

func getExecPathFromPID(pid uint32) (string, error) {
	if path, ok := lookupPidPath(pid); ok {
		return path, nil
	}
	path, err := queryExecPathFromPID(pid)
	if err != nil {
		return "", err
	}
	storePidPath(pid, path)
	return path, nil
}

func queryExecPathFromPID(pid uint32) (string, error) {
	// kernel process starts with a colon in order to distinguish with normal processes
	switch pid {
	case 0:
		// reserved pid for system idle process
		return ":System Idle Process", nil
	case 4:
		// reserved pid for windows kernel image
		return ":System", nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, syscall.MAX_LONG_PATH)
	size := uint32(len(buf))
	r1, _, err := syscall.Syscall6(
		queryProcName, 4,
		uintptr(h),
		uintptr(0),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0, 0)
	if r1 == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf[:size]), nil
}
