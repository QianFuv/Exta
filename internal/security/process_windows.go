package security

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PrepareProcessOwnership makes future default-created objects belong to the
// process user, including SQLite sidecars when Windows defaults to a group owner.
// It changes only this process token's default owner, not privileges or ACLs.
func PrepareProcessOwnership() error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_ADJUST_DEFAULT, &token); err != nil {
		return err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	owner := struct{ sid *windows.SID }{user.User.Sid}
	err = windows.SetTokenInformation(token, windows.TokenOwner, (*byte)(unsafe.Pointer(&owner)), uint32(unsafe.Sizeof(owner)))
	runtime.KeepAlive(user)
	return err
}
