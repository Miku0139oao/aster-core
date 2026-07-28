//go:build !windows

package aster

import "os"

func replaceStoreFile(source, target string) error {
	return os.Rename(source, target)
}
