// Package security implements local authentication, secret storage and egress policy.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// Vault encrypts credentials and retained content with entity-bound AES-256-GCM.
type Vault struct{ aead cipher.AEAD }

// NewVault accepts an explicitly supplied key; an absent key leaves the store locked.
func NewVault(key []byte) (*Vault, error) {
	if len(key) == 0 {
		return &Vault{}, nil
	}
	if len(key) != 32 {
		return nil, errors.New("master key must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	return &Vault{aead: aead}, err
}

// Unlocked reports whether cryptographic operations are available.
func (vault *Vault) Unlocked() bool { return vault != nil && vault.aead != nil }

// Seal uses a fresh nonce for each value and authenticates its identity/version.
func (vault *Vault) Seal(entity string, plaintext []byte) ([]byte, error) {
	if !vault.Unlocked() {
		return nil, errors.New("secret store is locked")
	}
	nonce := make([]byte, vault.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return vault.aead.Seal(nonce, nonce, plaintext, []byte("Exta/v1/"+entity)), nil
}

// Open fails closed on missing/wrong keys, moved ciphertext or tampering.
func (vault *Vault) Open(entity string, ciphertext []byte) ([]byte, error) {
	if !vault.Unlocked() || len(ciphertext) < vault.aead.NonceSize() {
		return nil, errors.New("secret store is locked or unreadable")
	}
	size := vault.aead.NonceSize()
	plain, err := vault.aead.Open(nil, ciphertext[:size], ciphertext[size:], []byte("Exta/v1/"+entity))
	if err != nil {
		return nil, errors.New("secret store is locked or unreadable")
	}
	return plain, nil
}
