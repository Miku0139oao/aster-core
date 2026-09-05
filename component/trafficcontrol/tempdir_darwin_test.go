package trafficcontrol

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// macOS commonly exposes its temporary directory through /var, a link
	// to /private/var. Use the physical root for test fixtures only; explicit
	// symlinks created by security tests must still be rejected by OpenStore.
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
