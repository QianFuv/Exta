package store_test

import (
	"testing"

	"exta/internal/store"
)

func TestLegacyAdministratorRemainsReusableWithoutRehash(t *testing.T) {
	state, principal := openStore(t)
	legacy := store.Digest([]byte(fakeCredential))
	if _, err := state.DB.Exec("UPDATE principals SET credential_hash=? WHERE id=?", legacy, principal); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		session, token, err := state.Login(t.Context(), fakeCredential, "legacy")
		if err != nil || session.PrincipalId != principal {
			t.Fatalf("legacy login: %v", err)
		}
		if err = state.Logout(t.Context(), token); err != nil {
			t.Fatal(err)
		}
	}
	var current string
	if err := state.DB.QueryRow("SELECT credential_hash FROM principals WHERE id=?", principal).Scan(&current); err != nil || current != legacy {
		t.Fatalf("legacy verifier changed: %v", err)
	}
	if _, err := state.AuthenticateToken(t.Context(), fakeCredential); err == nil {
		t.Fatal("administrator password accepted as client token")
	}
}

