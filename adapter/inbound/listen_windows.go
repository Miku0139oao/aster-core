package inbound

import (
	"net"
	"os"

	"github.com/metacubex/wireguard-go/ipc/namedpipe"
	"golang.org/x/sys/windows"
)

const SupportNamedPipe = true

// windowsSDDL limits the pipe to its owner, administrators, and local system.
const windowsSDDL = "D:P(A;;GA;;;OW)(A;;GA;;;BA)(A;;GA;;;SY)"

func ListenNamedPipe(path string) (net.Listener, error) {
	sddl := os.Getenv("LISTEN_NAMEDPIPE_SDDL")
	if sddl == "" {
		sddl = windowsSDDL
	}
	securityDescriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, err
	}
	namedpipeLC := namedpipe.ListenConfig{
		SecurityDescriptor: securityDescriptor,
		InputBufferSize:    256 * 1024,
		OutputBufferSize:   256 * 1024,
	}
	return namedpipeLC.Listen(path)
}
