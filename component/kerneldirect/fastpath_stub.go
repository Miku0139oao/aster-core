//go:build !linux

package kerneldirect

import "errors"

func NewFastPath(FastPathOptions) (FastPath, error) {
	return nil, errors.New("TC eBPF kernel-direct is only available on Linux")
}
