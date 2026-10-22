//go:build !windows

package security

import (
	"errors"
	"os"
	"syscall"
)

// ProtectDirectory restricts a runtime directory to its owner.
func ProtectDirectory(path string) error { return os.Chmod(path, 0700) }

// CheckPrivate rejects symlinks, other owners and group/world mode bits.
func CheckPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != uint32(os.Geteuid()) || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("runtime state must be owned and accessible only by the service user")
	}
	return nil
}

// LockDirectory retains an OS lock until the file is closed or the process exits.
func LockDirectory(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
