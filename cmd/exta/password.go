package main

import (
	"errors"
	"io"
	"strings"

	"exta/internal/store"
)

// readPassword bounds stdin before creating state and preserves significant spaces.
func readPassword(input io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(input, 2051))
	if err != nil || len(data) > 2050 {
		return "", errors.New("cannot read a valid administrator password")
	}
	password := strings.TrimSuffix(string(data), "\n")
	if len(password) < len(data) {
		password = strings.TrimSuffix(password, "\r")
	}
	if !store.ValidPassword(password) {
		return "", errors.New("password must contain 8 to 512 characters on one line")
	}
	return password, nil
}
