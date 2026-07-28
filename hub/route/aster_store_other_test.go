//go:build !windows

package route

import (
	"path/filepath"
	"testing"
)

func asterTestStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "aster-state.json")
}
