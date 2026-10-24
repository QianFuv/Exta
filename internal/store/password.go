package store

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

const passwordIterations = 600000

// ValidPassword matches the login contract without trimming significant spaces.
func ValidPassword(password string) bool {
	length := utf8.RuneCountInString(password)
	return utf8.ValidString(password) && length >= 8 && length <= 512 && !strings.ContainsAny(password, "\r\n")
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	digest, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2-sha256$v1$600000$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(digest), nil
}

// verifyPassword accepts the fixed versioned format and unchanged legacy random credentials.
func verifyPassword(password, verifier string) bool {
	if !ValidPassword(password) {
		return false
	}
	if len(verifier) == 64 && strings.ToLower(verifier) == verifier {
		if decoded, err := hex.DecodeString(verifier); err == nil && len(decoded) == 32 {
			return subtle.ConstantTimeCompare([]byte(Digest([]byte(password))), []byte(verifier)) == 1
		}
		return false
	}
	fields := strings.Split(verifier, "$")
	if len(fields) != 5 || fields[0] != "pbkdf2-sha256" || fields[1] != "v1" || fields[2] != "600000" {
		return false
	}
	salt, saltErr := base64.RawStdEncoding.Strict().DecodeString(fields[3])
	want, hashErr := base64.RawStdEncoding.Strict().DecodeString(fields[4])
	if saltErr != nil || hashErr != nil || len(salt) != 16 || len(want) != 32 {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	return err == nil && subtle.ConstantTimeCompare(actual, want) == 1
}
