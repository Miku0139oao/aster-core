package aster

import (
	"fmt"
	"os"
)

func lockStore(path string) (func(), error) {
	lockPath := path + ".lock"
	if info, err := os.Lstat(lockPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Aster store lock is not a regular file: %s", lockPath)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := secureStoreFile(lockPath); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := lockStoreFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("%w: Aster store is already in use: %v", ErrConflict, err)
	}
	return func() {
		_ = unlockStoreFile(file)
		_ = file.Close()
	}, nil
}
