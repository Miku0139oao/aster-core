//go:build !windows

package aster

import (
	"fmt"
	"os"
	"syscall"
)

func secureStoreFile(path string) error {
	return os.Chmod(path, 0o600)
}

func validateStoreDirectorySecurity(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("aster store directory is not owned by the current user: %s", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("aster store directory is writable by another user: %s", path)
	}
	return nil
}

func validateStoreFileSecurity(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("aster state is not owned by the current user: %s", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("aster state permissions are too broad: %s", path)
	}
	return nil
}
