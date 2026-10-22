//go:build !windows

package security

// PrepareProcessOwnership needs no adjustment on Unix, where files use the process UID.
func PrepareProcessOwnership() error { return nil }
