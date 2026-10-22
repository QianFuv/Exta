package security

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProtectDirectory installs a protected owner-only DACL; chmod alone is insufficient.
func ProtectDirectory(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, user.User.Sid, nil, dacl, nil)
}

// CheckPrivate requires current-user ownership and rejects every access grant to
// another identity. Inherited owner-only file ACLs remain valid.
func CheckPrivate(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("runtime state cannot use reparse points")
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(user.User.Sid) {
		return errors.New("runtime state must be owned by the service user")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return errors.New("runtime state has a permissive DACL")
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, index, &entry); err != nil {
			return err
		}
		if entry.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if entry.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("runtime state has an unsupported access rule")
		}
		sid := (*windows.SID)(unsafe.Pointer(&entry.SidStart))
		if entry.Mask != 0 && !sid.Equals(user.User.Sid) {
			return errors.New("runtime state grants access to another identity")
		}
	}
	return nil
}

// LockDirectory uses a process-scoped file lock released automatically after a crash.
func LockDirectory(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlap windows.Overlapped
	if err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlap); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
