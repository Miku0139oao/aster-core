package process

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	SOCK_DIAG_BY_FAMILY  = 20
	inetDiagRequestSize  = int(unsafe.Sizeof(inetDiagRequest{}))
	inetDiagResponseSize = int(unsafe.Sizeof(inetDiagResponse{}))
)

type inetDiagRequest struct {
	Family   byte
	Protocol byte
	Ext      byte
	Pad      byte
	States   uint32

	SrcPort [2]byte
	DstPort [2]byte
	Src     [16]byte
	Dst     [16]byte
	If      uint32
	Cookie  [2]uint32
}

type inetDiagResponse struct {
	Family  byte
	State   byte
	Timer   byte
	ReTrans byte

	SrcPort [2]byte
	DstPort [2]byte
	Src     [16]byte
	Dst     [16]byte
	If      uint32
	Cookie  [2]uint32

	Expires uint32
	RQueue  uint32
	WQueue  uint32
	UID     uint32
	INode   uint32
}

func findProcessName(network string, ip netip.Addr, srcPort int) (uint32, string, error) {
	uid, inode, err := resolveSocketByNetlink(network, ip, srcPort)
	if runtime.GOOS == "android" {
		// on Android (especially recent releases), netlink INET_DIAG can fail or return UID 0 / empty process info for some apps
		// so trying fallback to resolve /proc/net/{tcp,tcp6,udp,udp6}
		if err != nil {
			uid, inode, err = resolveSocketByProcFS(network, ip, srcPort)
		} else if uid == 0 {
			pUID, pInode, pErr := resolveSocketByProcFS(network, ip, srcPort)
			if pErr == nil && pUID != 0 {
				uid, inode, err = pUID, pInode, nil
			}
		}
	}
	if err != nil {
		return 0, "", err
	}
	pp, err := resolveProcessNameByProcSearch(inode, uid)
	if runtime.GOOS == "android" {
		// if inode-based /proc/<pid>/fd resolution fails but UID is known,
		// fall back to resolving the process/package name by UID (typical on Android where all app processes share one UID).
		if err != nil && uid != 0 {
			pp, err = resolveProcessNameByUID(uid)
		}
	}
	return uid, pp, err
}

var (
	diagMu   sync.Mutex
	diagConn *netlink.Conn
)

func resolveSocketByNetlink(network string, ip netip.Addr, srcPort int) (uid uint32, inode uint32, err error) {
	var request inetDiagRequest
	request.States = 0xffffffff
	request.Cookie = [2]uint32{0xffffffff, 0xffffffff}

	if ip.Is4() {
		request.Family = unix.AF_INET
		a := ip.As4()
		copy(request.Src[:], a[:])
	} else {
		request.Family = unix.AF_INET6
		a := ip.As16()
		copy(request.Src[:], a[:])
	}

	switch {
	case network == TCP || (len(network) >= 3 && network[0] == 't' && network[1] == 'c' && network[2] == 'p'):
		request.Protocol = unix.IPPROTO_TCP
	case network == UDP || (len(network) >= 3 && network[0] == 'u' && network[1] == 'd' && network[2] == 'p'):
		request.Protocol = unix.IPPROTO_UDP
	default:
		return 0, 0, ErrInvalidNetwork
	}

	binary.BigEndian.PutUint16(request.SrcPort[:], uint16(srcPort))

	diagMu.Lock()
	defer diagMu.Unlock()
	if diagConn == nil {
		diagConn, err = netlink.Dial(unix.NETLINK_INET_DIAG, nil)
		if err != nil {
			return 0, 0, err
		}
	}

	message := netlink.Message{
		Header: netlink.Header{
			Type:  SOCK_DIAG_BY_FAMILY,
			Flags: netlink.Request | netlink.Dump,
		},
		Data: (*(*[inetDiagRequestSize]byte)(unsafe.Pointer(&request)))[:],
	}

	messages, err := diagConn.Execute(message)
	if err != nil {
		_ = diagConn.Close()
		diagConn = nil
		return 0, 0, err
	}

	want := ip.Unmap()
	wantPort := uint16(srcPort)
	err = ErrNotFound
	for _, msg := range messages {
		if len(msg.Data) < inetDiagResponseSize {
			continue
		}

		response := (*inetDiagResponse)(unsafe.Pointer(&msg.Data[0]))

		// always set to allow fallback when check fails
		uid, inode, err = response.UID, response.INode, nil

		// check src port
		if binary.BigEndian.Uint16(response.SrcPort[:]) != wantPort {
			continue
		}

		// check src IP
		var src netip.Addr
		switch response.Family {
		case unix.AF_INET:
			src = netip.AddrFrom4(*(*[4]byte)(response.Src[:4]))
		case unix.AF_INET6:
			src = netip.AddrFrom16(response.Src).Unmap()
		default:
			continue
		}
		if src != want {
			continue
		}

		// this is the one we want
		break
	}

	return
}

func resolveProcessNameByProcSearch(inode, uid uint32) (string, error) {
	files, err := os.ReadDir("/proc")
	if err != nil {
		return "", err
	}

	buffer := make([]byte, unix.PathMax)
	socket := make([]byte, 0, 24)
	socket = append(socket, "socket:["...)
	socket = strconv.AppendUint(socket, uint64(inode), 10)
	socket = append(socket, ']')

	for _, f := range files {
		if !f.IsDir() || !isPid(f.Name()) {
			continue
		}

		info, err := f.Info()
		if err != nil {
			continue
		}
		if info.Sys().(*syscall.Stat_t).Uid != uid {
			continue
		}

		processPath := "/proc/" + f.Name()
		fdPath := processPath + "/fd"

		fds, err := os.ReadDir(fdPath)
		if err != nil {
			continue
		}

		for _, fd := range fds {
			n, err := unix.Readlink(fdPath+"/"+fd.Name(), buffer)
			if err != nil {
				continue
			}

			if !bytes.Equal(buffer[:n], socket) {
				continue
			}
			var name string
			if runtime.GOOS == "android" {
				cmdline, err := os.ReadFile(path.Join(processPath, "cmdline"))
				if err != nil {
					return "", err
				}
				name = splitCmdline(cmdline)
			} else {
				name, err = os.Readlink(processPath + "/exe")
				if err != nil {
					return "", err
				}
			}
			return name, nil
		}
	}

	return "", fmt.Errorf("process of uid(%d),inode(%d) not found", uid, inode)
}

// resolveProcessNameByUID returns a process name for any process with uid.
// On Android all processes of one app share the same UID; used when inode
// lookup fails (socket closed / TIME_WAIT).
func resolveProcessNameByUID(uid uint32) (string, error) {
	files, err := os.ReadDir("/proc")
	if err != nil {
		return "", err
	}

	for _, f := range files {
		if !f.IsDir() || !isPid(f.Name()) {
			continue
		}

		info, err := f.Info()
		if err != nil {
			continue
		}
		if info.Sys().(*syscall.Stat_t).Uid != uid {
			continue
		}

		processPath := filepath.Join("/proc", f.Name())
		if runtime.GOOS == "android" {
			cmdline, err := os.ReadFile(path.Join(processPath, "cmdline"))
			if err != nil {
				continue
			}
			if name := splitCmdline(cmdline); name != "" {
				return name, nil
			}
		} else {
			if exe, err := os.Readlink(filepath.Join(processPath, "exe")); err == nil {
				return exe, nil
			}
		}
	}

	return "", fmt.Errorf("no process found with uid %d", uid)
}

// resolveSocketByProcFS finds UID and inode from /proc/net/{tcp,tcp6,udp,udp6}.
// In TUN mode metadata sourceIP is often the gateway (e.g. fake-ip range), not
// the socket's real local address; we match by local port first and prefer
// exact IP+port when it matches.
func resolveSocketByProcFS(network string, ip netip.Addr, srcPort int) (uint32, uint32, error) {
	var proto string
	switch {
	case network == TCP || (len(network) >= 3 && network[0] == 't' && network[1] == 'c' && network[2] == 'p'):
		proto = "tcp"
	case network == UDP || (len(network) >= 3 && network[0] == 'u' && network[1] == 'd' && network[2] == 'p'):
		proto = "udp"
	default:
		return 0, 0, ErrInvalidNetwork
	}

	targetPort := uint16(srcPort)
	unmapped := ip.Unmap()
	files := []string{"/proc/net/" + proto, "/proc/net/" + proto + "6"}

	var bestUID, bestInode uint32
	found := false

	for _, path := range files {
		isV6 := strings.HasSuffix(path, "6")

		var matchIP netip.Addr
		if unmapped.Is4() {
			if isV6 {
				matchIP = netip.AddrFrom16(unmapped.As16())
			} else {
				matchIP = unmapped
			}
		} else {
			if !isV6 {
				continue
			}
			matchIP = unmapped
		}

		uid, inode, exact, err := searchProcNetFileByPort(path, matchIP, targetPort)
		if err != nil {
			continue
		}

		if exact {
			return uid, inode, nil
		}

		if !found || (bestUID == 0 && uid != 0) {
			bestUID = uid
			bestInode = inode
			found = true
		}
	}

	if found {
		return bestUID, bestInode, nil
	}
	return 0, 0, ErrNotFound
}

// searchProcNetFileByPort scans /proc/net/* for local_address matching targetPort.
// Exact IP+port wins; else port-only (skips inode==0 entries used by TIME_WAIT).
func searchProcNetFileByPort(path string, targetIP netip.Addr, targetPort uint16) (uid, inode uint32, exact bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer f.Close()

	isV6 := strings.HasSuffix(path, "6")
	scanner := bufio.NewScanner(f)

	if !scanner.Scan() {
		return 0, 0, false, ErrNotFound
	}

	var bestUID, bestInode uint32
	found := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		lineIP, linePort, parseErr := parseHexAddrPort(fields[1], isV6)
		if parseErr != nil {
			continue
		}
		if linePort != targetPort {
			continue
		}

		lineUID, parseErr := strconv.ParseUint(fields[7], 10, 32)
		if parseErr != nil {
			continue
		}
		lineInode, parseErr := strconv.ParseUint(fields[9], 10, 32)
		if parseErr != nil {
			continue
		}

		if lineIP == targetIP {
			return uint32(lineUID), uint32(lineInode), true, nil
		}

		if lineInode == 0 {
			continue
		}

		if !found || (bestUID == 0 && lineUID != 0) {
			bestUID = uint32(lineUID)
			bestInode = uint32(lineInode)
			found = true
		}
	}

	if found {
		return bestUID, bestInode, false, nil
	}
	return 0, 0, false, ErrNotFound
}
