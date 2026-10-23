package store

import (
	"strings"
	"testing"
)

func TestPasswordSaltAndStrictVerification(t *testing.T) {
	password := "  synthetic long password 密码  "
	first, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !verifyPassword(password, first) || verifyPassword(strings.TrimSpace(password), first) || verifyPassword("wrong synthetic password", first) {
		t.Fatal("salt or exact password verification failed")
	}
	if !verifyPassword(password, Digest([]byte(password))) {
		t.Fatal("legacy random credential rejected")
	}
	for _, bad := range []string{"", first + "$extra", strings.Replace(first, "600000", "1", 1), strings.Replace(first, "v1", "v2", 1), strings.ToUpper(Digest([]byte(password)))} {
		if verifyPassword(password, bad) {
			t.Fatal("malformed verifier accepted")
		}
	}
}
