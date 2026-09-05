package route

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Traffic-control fixtures require a symlink-free store ancestry.
	// Canonicalize the OS temporary root, not user-supplied store paths.
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err == nil {
		err = os.Setenv("TMPDIR", tempRoot)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve test temporary directory: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
