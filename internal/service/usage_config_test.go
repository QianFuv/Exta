package service

import "testing"

func TestExaUsageRejectsConflictingKeyMappings(t *testing.T) {
	configs := []ExaUsageConfig{
		{ServiceKeyId: "service_one", UpstreamKeyIds: map[string]string{"key_a": "upstream_a"}},
		{ServiceKeyId: "service_two", UpstreamKeyIds: map[string]string{"key_a": "upstream_b"}},
	}
	if err := ValidateExaUsage(configs); err == nil {
		t.Fatal("conflicting usage attribution accepted")
	}
	configs[1].UpstreamKeyIds = map[string]string{"key_b": "upstream_b"}
	if err := ValidateExaUsage(configs); err != nil {
		t.Fatal("distinct key mappings rejected", err)
	}
}
