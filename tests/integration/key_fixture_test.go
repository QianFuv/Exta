package integration

import (
	"testing"

	"exta/internal/domain"
	"exta/internal/store"
)

func addLinkedKey(t *testing.T, state *store.Store, owner string, first domain.UpstreamKey, input domain.UpstreamKeyCreate) (domain.UpstreamKey, error) {
	t.Helper()
	input.Provider = first.Provider
	input.QuotaPolicy = &first.QuotaPolicy
	key, err := state.AddKey(t.Context(), owner, input)
	if err != nil {
		return key, err
	}
	key.AssociationRef = first.AssociationRef
	key.AssociationEvidence = "operator_declared"
	key.SharedLimit = &first.QuotaPolicy
	first.SharedLimit = &first.QuotaPolicy
	first.AssociationEvidence = "operator_declared"
	if _, err = state.DB.Exec("UPDATE provider_keys SET data=? WHERE id=?", store.JSON(first), first.Id); err != nil {
		return key, err
	}
	_, err = state.DB.Exec("UPDATE provider_keys SET association_ref=?,data=? WHERE id=?", key.AssociationRef, store.JSON(key), key.Id)
	return key, err
}
