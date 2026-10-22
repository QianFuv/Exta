package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestServeRejectsWorldReadableMasterKey(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	if err := initialize(t.Context(), directory, &output); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(filepath.Join(directory, "master.key"), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	state, _, _, lock, err := openRuntime(t.Context(), directory)
	if err == nil {
		state.Close()
		lock.Close()
		t.Fatal("serve accepted world-readable master key")
	}
}
