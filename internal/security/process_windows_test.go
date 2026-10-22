package security

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestProcessOwnershipPreservesStrictFileChecks(t *testing.T) {
	if os.Getenv("EXTA_OWNER_TEST_CHILD") != "1" {
		child := exec.Command(os.Args[0], "-test.run=^TestProcessOwnershipPreservesStrictFileChecks$", "-test.v")
		child.Env = append(os.Environ(), "EXTA_OWNER_TEST_CHILD=1")
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("isolated ownership test: %v\n%s", err, output)
		}
		return
	}
	isElevated := windows.GetCurrentProcessToken().IsElevated()
	if isElevated {
		administrator, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
		if err != nil {
			t.Fatal(err)
		}
		var token windows.Token
		if err = windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_DEFAULT, &token); err != nil {
			t.Fatal(err)
		}
		owner := struct{ sid *windows.SID }{administrator}
		err = windows.SetTokenInformation(token, windows.TokenOwner, (*byte)(unsafe.Pointer(&owner)), uint32(unsafe.Sizeof(owner)))
		token.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	directory := filepath.Join(t.TempDir(), "private")
	if err := CreatePrivateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	before := filepath.Join(directory, "before")
	if err := os.WriteFile(before, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if isElevated && CheckPrivate(before) == nil {
		t.Fatal("accepted Administrators-owned state before normalization")
	}
	if err := PrepareProcessOwnership(); err != nil {
		t.Fatal(err)
	}
	after := filepath.Join(directory, "after")
	if err := os.WriteFile(after, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(after); err != nil {
		t.Fatal(err)
	}
	if isElevated && CheckPrivate(before) == nil {
		t.Fatal("normalization rehabilitated an existing group-owned file")
	}
}
