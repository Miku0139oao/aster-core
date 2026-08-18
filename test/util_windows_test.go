//go:build windows

package main

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

func defaultRouteIP() (netip.Addr, error) {
	output, err := exec.Command(
		"docker", "run", "--rm", "--entrypoint", "/bin/sh", ImageVmess,
		"-c", "getent ahostsv4 host.docker.internal | head -n 1",
	).CombinedOutput()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve Docker Desktop host address: %w: %s", err, strings.TrimSpace(string(output)))
	}

	for _, field := range strings.Fields(string(output)) {
		addr, parseErr := netip.ParseAddr(field)
		if parseErr == nil && addr.Is4() {
			return addr, nil
		}
	}

	return netip.Addr{}, fmt.Errorf("Docker Desktop returned no IPv4 host address: %q", strings.TrimSpace(string(output)))
}
