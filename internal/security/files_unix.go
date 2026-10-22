//go:build !windows

package security

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func checkLink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("state paths cannot contain symbolic links")
	}
	return nil
}

// CreatePrivateDirectory grants owner-only access from the first observable instant.
func CreatePrivateDirectory(path string) error {
	if err := CheckNoLinks(path); err != nil {
		return err
	}
	path = filepath.Clean(path)
	if err := os.Mkdir(path, 0700); err != nil {
		return err
	}
	return SyncDirectory(filepath.Dir(path))
}

// SyncDirectory persists directory entries after file creation or publication.
func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// ReplaceFile publishes a synced sibling and persists the directory entry before return.
func ReplaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	return SyncDirectory(filepath.Dir(destination))
}

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
