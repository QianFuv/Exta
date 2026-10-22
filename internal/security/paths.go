package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CheckNoLinks rejects symlinks and reparse points in every existing path component.
// A missing final component is allowed when reserving a new output path.
func CheckNoLinks(path string) error {
	if err := validateStatePath(path); err != nil {
		return err
	}
	path = filepath.Clean(path)
	first := true
	for {
		if err := checkLink(path); err != nil && !(first && errors.Is(err, os.ErrNotExist)) {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path, first = parent, false
	}
}

func validateStatePath(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("an absolute path is required")
	}
	for _, component := range strings.FieldsFunc(path, func(value rune) bool { return value == '/' || value == '\\' }) {
		if component == ".." {
			return errors.New("state paths cannot contain parent traversal")
		}
	}
	return nil
}

// PreparePrivateDirectory creates missing components with private access from birth.
// Existing directories must already be private; tightening access cannot revoke old handles.
func PreparePrivateDirectory(path string) error {
	if err := validateStatePath(path); err != nil {
		return err
	}
	path = filepath.Clean(path)
	missing := []string{}
	ancestor := path
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if !info.IsDir() {
				return errors.New("state ancestor must be a directory")
			}
			if err = CheckNoLinks(ancestor); err != nil {
				return err
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, ancestor)
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return errors.New("state directory has no existing ancestor")
		}
		ancestor = parent
	}
	if len(missing) == 0 {
		if err := CheckPrivate(path); err != nil {
			return errors.New("existing data directory is not private; use a new directory instead")
		}
		return ProtectDirectory(path)
	}
	for index := len(missing) - 1; index >= 0; index-- {
		if err := CreatePrivateDirectory(missing[index]); err != nil {
			return err
		}
	}
	return nil
}
