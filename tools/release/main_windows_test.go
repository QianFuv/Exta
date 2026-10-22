package main

import (
	"fmt"
	"os"
	"testing"

	"exta/internal/security"
)

func TestMain(tests *testing.M) {
	if err := security.PrepareProcessOwnership(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(tests.Run())
}
