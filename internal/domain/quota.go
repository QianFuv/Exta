package domain

import "encoding/json"

// Input preserves a canonical policy when sending optional management fields.
func (policy QuotaPolicy) Input() *QuotaPolicyInput {
	return &QuotaPolicyInput{Unit: &policy.Unit, Allocation: &policy.Allocation, PerOperationReservation: &policy.PerOperationReservation, ActiveJobs: &policy.ActiveJobs}
}

// UnmarshalJSON distinguishes an omitted policy from an explicit unlimited reset.
func (patch *UpstreamKeyPatch) UnmarshalJSON(data []byte) error {
	type plain UpstreamKeyPatch
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if policy, present := fields["quota_policy"]; present && string(policy) == "null" {
		value.QuotaPolicy = &QuotaPolicyInput{}
	}
	*patch = UpstreamKeyPatch(value)
	return nil
}
