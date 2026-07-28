package updater

import (
	"crypto/sha256"
	"fmt"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

func TestCoreBaseName(t *testing.T) {
	fmt.Println("Core base name =", DefaultCoreUpdater.CoreBaseName())
}

func TestCoreBaseNameUsesEmbeddedReleaseAsset(t *testing.T) {
	previous := C.ReleaseAsset
	C.ReleaseAsset = "aster-core-linux-386-softfloat"
	t.Cleanup(func() { C.ReleaseAsset = previous })
	require.Equal(t, "aster-core-linux-386-softfloat", DefaultCoreUpdater.CoreBaseName())
}

func TestParsePackageChecksum(t *testing.T) {
	content := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  ./aster-core.zip\n")
	checksum, err := parsePackageChecksum(content, "aster-core.zip")
	require.NoError(t, err)
	require.Equal(t, [sha256.Size]byte{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
	}, checksum)
	require.Error(t, func() error {
		_, err := parsePackageChecksum(content, "missing.zip")
		return err
	}())
}

func TestValidateReleaseUpdate(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		latestVersion  string
		force          bool
		wantError      bool
	}{
		{name: "upgrade", currentVersion: "v1.2.3", latestVersion: "v1.2.4"},
		{name: "upgrade without v prefix", currentVersion: "1.2.3", latestVersion: "1.3.0"},
		{name: "development build", currentVersion: "alpha-abcdef", latestVersion: "v1.2.3"},
		{name: "downgrade", currentVersion: "v1.2.3", latestVersion: "v1.2.2", wantError: true},
		{name: "forced downgrade", currentVersion: "v1.2.3", latestVersion: "v1.2.2", force: true},
		{name: "prerelease on stable channel", currentVersion: "v1.2.3", latestVersion: "v1.3.0-rc.1", wantError: true},
		{name: "build metadata on stable channel", currentVersion: "v1.2.3", latestVersion: "v1.3.0+build", wantError: true},
		{name: "invalid latest version", currentVersion: "v1.2.3", latestVersion: "latest", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateReleaseUpdate(test.currentVersion, test.latestVersion, test.force)
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
