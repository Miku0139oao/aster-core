package aster

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	tempRoot, err := os.MkdirTemp("", "aster-tests-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Aster test temp root: %v\n", err)
		os.Exit(1)
	}
	if err := secureWindowsTestDirectory(tempRoot); err != nil {
		fmt.Fprintf(os.Stderr, "secure Aster test temp root: %v\n", err)
		_ = os.RemoveAll(tempRoot)
		os.Exit(1)
	}

	oldTemp, hadTemp := os.LookupEnv("TEMP")
	oldTMP, hadTMP := os.LookupEnv("TMP")
	_ = os.Setenv("TEMP", tempRoot)
	_ = os.Setenv("TMP", tempRoot)
	code := m.Run()
	if hadTemp {
		_ = os.Setenv("TEMP", oldTemp)
	} else {
		_ = os.Unsetenv("TEMP")
	}
	if hadTMP {
		_ = os.Setenv("TMP", oldTMP)
	} else {
		_ = os.Unsetenv("TMP")
	}
	_ = os.RemoveAll(tempRoot)
	os.Exit(code)
}

func TestSecureStoreFileReplacesPermissiveDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

	userSID := testCurrentUserSID(t)
	worldSID, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	require.NoError(t, err)
	setTestDACL(t, path, []windows.EXPLICIT_ACCESS{
		testAccessEntry(userSID, windows.GENERIC_ALL, windows.NO_INHERITANCE),
		testAccessEntry(worldSID, windows.GENERIC_READ, windows.NO_INHERITANCE),
	})
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Error(t, validateStoreFileSecurity(path, info))

	require.NoError(t, secureStoreFile(path))
	require.NoError(t, validateStoreFileSecurity(path, info))
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	require.NoError(t, err)
	control, _, err := descriptor.Control()
	require.NoError(t, err)
	require.NotZero(t, control&windows.SE_DACL_PROTECTED)
}

func TestValidateStoreFileRejectsNullDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
	require.NoError(t, windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		nil,
		nil,
	))

	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.ErrorContains(t, validateStoreFileSecurity(path, info), "missing or null")
}

func TestValidateStoreDirectoryRejectsUntrustedWrite(t *testing.T) {
	dir := t.TempDir()
	userSID := testCurrentUserSID(t)
	worldSID, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	require.NoError(t, err)
	info, err := os.Lstat(dir)
	require.NoError(t, err)

	setTestDACL(t, dir, []windows.EXPLICIT_ACCESS{
		testAccessEntry(userSID, windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
		testAccessEntry(worldSID, windows.GENERIC_READ, windows.NO_INHERITANCE),
	})
	require.NoError(t, validateStoreDirectorySecurity(dir, info))

	setTestDACL(t, dir, []windows.EXPLICIT_ACCESS{
		testAccessEntry(userSID, windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
		testAccessEntry(worldSID, windows.FILE_WRITE_DATA, windows.OBJECT_INHERIT_ACE|windows.INHERIT_ONLY_ACE),
	})
	require.ErrorContains(t, validateStoreDirectorySecurity(dir, info), "unsafe inherited access")

	setTestDACL(t, dir, []windows.EXPLICIT_ACCESS{
		testAccessEntry(userSID, windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
		testAccessEntry(worldSID, windows.FILE_WRITE_DATA, windows.CONTAINER_INHERIT_ACE|windows.INHERIT_ONLY_ACE),
	})
	require.NoError(t, validateStoreDirectorySecurity(dir, info))

	setTestDACL(t, dir, []windows.EXPLICIT_ACCESS{
		testAccessEntry(userSID, windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
		testAccessEntry(worldSID, windows.FILE_WRITE_DATA, windows.NO_INHERITANCE),
	})
	require.ErrorContains(t, validateStoreDirectorySecurity(dir, info), "untrusted SID")

	parent := t.TempDir()
	setTestDACL(t, parent, []windows.EXPLICIT_ACCESS{
		testAccessEntry(userSID, windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
		testAccessEntry(worldSID, windows.FILE_WRITE_DATA, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
	})
	inheritedDir := filepath.Join(parent, "inherited")
	require.NoError(t, os.Mkdir(inheritedDir, 0o700))
	inheritedInfo, err := os.Lstat(inheritedDir)
	require.NoError(t, err)
	require.ErrorContains(t, validateStoreDirectorySecurity(inheritedDir, inheritedInfo), "untrusted SID")
}

func TestValidateStoreSecurityRejectsQueryFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	require.ErrorContains(t, validateStoreFileSecurity(path, nil), "query Aster store security")
}

func secureWindowsTestDirectory(path string) error {
	userSID, err := currentStoreUserSID()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		testAccessEntry(userSID, windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
	}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
}

func testCurrentUserSID(t *testing.T) *windows.SID {
	t.Helper()
	sid, err := currentStoreUserSID()
	require.NoError(t, err)
	return sid
}

func testAccessEntry(sid *windows.SID, permissions windows.ACCESS_MASK, inheritance uint32) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: permissions,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}

func setTestDACL(t *testing.T, path string, entries []windows.EXPLICIT_ACCESS) {
	t.Helper()
	acl, err := windows.ACLFromEntries(entries, nil)
	require.NoError(t, err)
	require.NoError(t, windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	))
}
