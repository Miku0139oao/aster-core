package updater

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
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

// The gzip header name comes from the downloaded archive, so it must never be
// able to place the unpacked file outside the update directory.
func TestGzFileUnpackContainsPathTraversal(t *testing.T) {
	outDir := t.TempDir()
	escaped := filepath.Join(filepath.Dir(outDir), "escaped-core")

	archive := filepath.Join(outDir, "core.gz")
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	writer.Header.Name = "../../../escaped-core"
	_, err := writer.Write([]byte("payload"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, os.WriteFile(archive, buf.Bytes(), 0o600))

	outputName, err := DefaultCoreUpdater.gzFileUnpack(archive, outDir, 0o600)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(outDir, "escaped-core"), outputName)
	require.NoFileExists(t, escaped)
}

func TestGzFileUnpackUsesArchiveName(t *testing.T) {
	outDir := t.TempDir()
	archive := filepath.Join(outDir, "core.gz")
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	writer.Header.Name = "aster-core"
	_, err := writer.Write([]byte("payload"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, os.WriteFile(archive, buf.Bytes(), 0o600))

	outputName, err := DefaultCoreUpdater.gzFileUnpack(archive, outDir, 0o600)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(outDir, "aster-core"), outputName)
	content, err := os.ReadFile(outputName)
	require.NoError(t, err)
	require.Equal(t, "payload", string(content))
}

func TestZipFileUnpackKeepsOutputInsideDirectory(t *testing.T) {
	outDir := t.TempDir()
	archive := filepath.Join(outDir, "core.zip")
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("../../../escaped-core")
	require.NoError(t, err)
	_, err = entry.Write([]byte("payload"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, os.WriteFile(archive, buf.Bytes(), 0o600))

	outputName, err := DefaultCoreUpdater.zipFileUnpack(archive, outDir, 0o600)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(outDir, "escaped-core"), outputName)
	require.NoFileExists(t, filepath.Join(filepath.Dir(outDir), "escaped-core"))
}

func TestArchiveOutputPathRejectsUnusableNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../..", "/", "foo/..", "./"} {
		_, err := archiveOutputPath("/tmp/update", name)
		require.Errorf(t, err, "name %q must be rejected", name)
	}
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
