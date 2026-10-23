package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordStdinValidationBeforeStateCreation(t *testing.T) {
	for _, password := range []string{"", "too-short", strings.Repeat("x", 513), "synthetic-password\nsecond-line", string([]byte{255})} {
		directory := filepath.Join(t.TempDir(), "absent")
		var output bytes.Buffer
		err := runWithInput(t.Context(), []string{"init", "--password-stdin", "--data-dir", directory}, strings.NewReader(password), &output, &output)
		if err == nil {
			t.Fatal("invalid password accepted")
		}
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatal("invalid password created state")
		}
	}
	for _, ending := range []string{"", "\n", "\r\n"} {
		password := "  synthetic password 密码  "
		got, err := readPassword(strings.NewReader(password + ending))
		if err != nil || got != password {
			t.Fatalf("password altered: %v", err)
		}
	}
}

func TestPasswordPersistsWithoutSecretOutput(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	password := "synthetic persistent administrator password"
	var output bytes.Buffer
	if err := runWithInput(t.Context(), []string{"init", "--password-stdin", "--data-dir", directory}, strings.NewReader(password), &output, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), password) || strings.Contains(output.String(), "pbkdf2") {
		t.Fatal("initialization exposed secret material")
	}
	var principal, verifier string
	for range 2 {
		state, _, _, lock, err := openRuntime(t.Context(), directory)
		if err != nil {
			t.Fatal(err)
		}
		var current string
		if err = state.DB.QueryRow("SELECT credential_hash FROM principals").Scan(&current); err != nil {
			t.Fatal(err)
		}
		if verifier != "" && current != verifier {
			t.Fatal("password verifier changed")
		}
		verifier = current
		session, token, err := state.Login(t.Context(), password, "fixture")
		if err != nil || (principal != "" && session.PrincipalId != principal) {
			t.Fatalf("persistent login: %v", err)
		}
		principal = session.PrincipalId
		if err = state.Logout(t.Context(), token); err != nil {
			t.Fatal(err)
		}
		state.Close()
		lock.Close()
	}
}
