package store

import (
	"encoding/json"
	"exta/internal/domain"
	"fmt"
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

func TestAdministratorPasswordCharacterBoundaries(t *testing.T) {
	for _, character := range []string{"a", "界", "🔐"} {
		for _, length := range []int{7, 8, 10, 512, 513} {
			t.Run(fmt.Sprintf("%U/%d", []rune(character)[0], length), func(t *testing.T) {
				password := strings.Repeat(character, length)
				want := length >= 8 && length <= 512
				if ValidPassword(password) != want {
					t.Fatal("administrator character boundary mismatch")
				}
				payload, err := json.Marshal(map[string]string{"credential": password})
				if err != nil {
					t.Fatal(err)
				}
				if (domain.ValidateJSON("SessionCreate", payload) == nil) != want {
					t.Fatal("login contract and initialization disagree")
				}
				if want {
					verifier, err := hashPassword(password)
					if err != nil || !verifyPassword(password, verifier) {
						t.Fatal("valid boundary password cannot authenticate")
					}
				}
			})
		}
	}
}
