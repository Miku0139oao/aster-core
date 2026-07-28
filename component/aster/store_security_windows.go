package aster

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	storeACLRevision   = 2
	storeACLRevisionDS = 4
	fileDeleteChild    = 0x00000040
)

const storeDirectoryWriteMask windows.ACCESS_MASK = windows.FILE_WRITE_DATA |
	windows.FILE_APPEND_DATA |
	windows.FILE_WRITE_EA |
	windows.FILE_WRITE_ATTRIBUTES |
	fileDeleteChild |
	windows.DELETE |
	windows.WRITE_DAC |
	windows.WRITE_OWNER |
	windows.GENERIC_WRITE |
	windows.GENERIC_ALL |
	windows.MAXIMUM_ALLOWED

type storeACLHeader struct {
	revision  byte
	reserved  byte
	size      uint16
	aceCount  uint16
	reserved2 uint16
}

func secureStoreFile(path string) error {
	userSID, err := currentStoreUserSID()
	if err != nil {
		return fmt.Errorf("get current user for Aster state: %w", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;GA;;;%s)", userSID.String()))
	if err != nil {
		return fmt.Errorf("create Aster state security descriptor: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read Aster state security descriptor: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("secure Aster state %s: %w", path, err)
	}
	return validateStoreObjectSecurity(path, false)
}

func validateStoreDirectorySecurity(path string, _ os.FileInfo) error {
	return validateStoreObjectSecurity(path, true)
}

func validateStoreFileSecurity(path string, _ os.FileInfo) error {
	return validateStoreObjectSecurity(path, false)
}

func validateStoreObjectSecurity(path string, directory bool) error {
	userSID, err := currentStoreUserSID()
	if err != nil {
		return fmt.Errorf("get current user for Aster store security: %w", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("query Aster store security for %s: %w", path, err)
	}
	if descriptor == nil || !descriptor.IsValid() {
		return fmt.Errorf("Aster store security descriptor is invalid: %s", path)
	}
	control, revision, err := descriptor.Control()
	if err != nil || revision != 1 || control&windows.SE_SELF_RELATIVE == 0 || control&windows.SE_DACL_PRESENT == 0 {
		return fmt.Errorf("Aster store security descriptor is malformed: %s", path)
	}

	descriptorStart := unsafe.Pointer(descriptor)
	descriptorSize := uintptr(descriptor.Length())
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		return fmt.Errorf("Aster store owner is unavailable: %s", path)
	}
	if _, ok := storeSIDLength(descriptorStart, descriptorSize, unsafe.Pointer(owner)); !ok {
		return fmt.Errorf("Aster store owner SID is malformed: %s", path)
	}
	if !owner.Equals(userSID) {
		return fmt.Errorf("Aster store object is not owned by the current user: %s", path)
	}

	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("Aster store DACL is missing or null: %s", path)
	}
	return validateStoreDACL(path, descriptorStart, descriptorSize, dacl, userSID, directory)
}

func validateStoreDACL(path string, descriptorStart unsafe.Pointer, descriptorSize uintptr, dacl *windows.ACL, userSID *windows.SID, directory bool) error {
	aclStart := unsafe.Pointer(dacl)
	aclHeaderSize := unsafe.Sizeof(storeACLHeader{})
	if !storeMemoryContains(descriptorStart, descriptorSize, aclStart, aclHeaderSize) {
		return fmt.Errorf("Aster store DACL is malformed: %s", path)
	}
	header := (*storeACLHeader)(unsafe.Pointer(dacl))
	aclSize := uintptr(header.size)
	if (header.revision != storeACLRevision && header.revision != storeACLRevisionDS) ||
		aclSize < aclHeaderSize || !storeMemoryContains(descriptorStart, descriptorSize, aclStart, aclSize) ||
		uintptr(header.aceCount) > (aclSize-aclHeaderSize)/unsafe.Sizeof(windows.ACE_HEADER{}) {
		return fmt.Errorf("Aster store DACL is malformed: %s", path)
	}

	nextACEOffset := aclHeaderSize
	for i := uint16(0); i < header.aceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil {
			return fmt.Errorf("read Aster store DACL entry %d for %s: %w", i, path, err)
		}
		if ace == nil {
			return fmt.Errorf("Aster store DACL entry %d is null: %s", i, path)
		}
		aceStart := unsafe.Pointer(ace)
		if !storeMemoryContains(aclStart, aclSize, aceStart, unsafe.Sizeof(windows.ACE_HEADER{})) ||
			uintptr(aceStart)-uintptr(aclStart) != nextACEOffset {
			return fmt.Errorf("Aster store DACL entry %d is malformed: %s", i, path)
		}
		aceSize := uintptr(ace.Header.AceSize)
		if aceSize%4 != 0 || !storeMemoryContains(aclStart, aclSize, aceStart, aceSize) {
			return fmt.Errorf("Aster store DACL entry %d is malformed: %s", i, path)
		}
		nextACEOffset += aceSize

		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE && ace.Header.AceType != windows.ACCESS_DENIED_ACE_TYPE {
			return fmt.Errorf("Aster store DACL entry %d has an unsupported type: %s", i, path)
		}
		if ace.Header.AceFlags & ^uint8(windows.VALID_INHERIT_FLAGS) != 0 {
			return fmt.Errorf("Aster store DACL entry %d has invalid flags: %s", i, path)
		}
		sidOffset := unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart)
		if sidOffset > aceSize {
			return fmt.Errorf("Aster store DACL entry %d has a malformed SID: %s", i, path)
		}
		sidStart := unsafe.Add(aceStart, sidOffset)
		sidSize, ok := storeSIDLength(aceStart, aceSize, sidStart)
		if !ok || sidOffset+sidSize != aceSize {
			return fmt.Errorf("Aster store DACL entry %d has a malformed SID: %s", i, path)
		}
		aceSID := (*windows.SID)(sidStart)

		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if isTrustedStoreSID(aceSID, userSID) {
			continue
		}
		// Object-inheritable allows become effective on newly created temp and lock files.
		if directory && ace.Header.AceFlags&windows.OBJECT_INHERIT_ACE != 0 && ace.Mask != 0 {
			return fmt.Errorf("Aster store DACL grants untrusted SID %s unsafe inherited access: %s", aceSID.String(), path)
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if (!directory && ace.Mask != 0) || (directory && ace.Mask&storeDirectoryWriteMask != 0) {
			return fmt.Errorf("Aster store DACL grants untrusted SID %s unsafe access: %s", aceSID.String(), path)
		}
	}
	return nil
}

func currentStoreUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	if user == nil || user.User.Sid == nil || !user.User.Sid.IsValid() {
		return nil, windows.ERROR_INVALID_SID
	}
	return user.User.Sid.Copy()
}

func isTrustedStoreSID(sid, userSID *windows.SID) bool {
	// SYSTEM and Administrators are privileged equivalents of root on Windows.
	return sid.Equals(userSID) ||
		sid.IsWellKnown(windows.WinLocalSystemSid) ||
		sid.IsWellKnown(windows.WinBuiltinAdministratorsSid)
}

func storeSIDLength(base unsafe.Pointer, size uintptr, sidStart unsafe.Pointer) (uintptr, bool) {
	const sidHeaderSize = uintptr(8)
	if !storeMemoryContains(base, size, sidStart, sidHeaderSize) {
		return 0, false
	}
	sidHeader := unsafe.Slice((*byte)(sidStart), sidHeaderSize)
	sidSize := sidHeaderSize + uintptr(sidHeader[1])*4
	if !storeMemoryContains(base, size, sidStart, sidSize) {
		return 0, false
	}
	sid := (*windows.SID)(sidStart)
	if !sid.IsValid() || uintptr(sid.Len()) != sidSize {
		return 0, false
	}
	return sidSize, true
}

func storeMemoryContains(base unsafe.Pointer, size uintptr, address unsafe.Pointer, length uintptr) bool {
	baseAddress := uintptr(base)
	addressValue := uintptr(address)
	if addressValue < baseAddress {
		return false
	}
	offset := addressValue - baseAddress
	return offset <= size && length <= size-offset
}
