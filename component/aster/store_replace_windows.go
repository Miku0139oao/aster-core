package aster

import "golang.org/x/sys/windows"

func replaceStoreFile(source, target string) error {
	return windows.Rename(source, target)
}
