# Key administration

An upstream key is the smallest configuration and scheduling unit. Create Exa or
Tavily keys directly; there is no account resource, account creation step, or
separately managed quota group. Service access keys are separate credentials for
clients calling Exta and have their own page and permissions.

Each upstream key has a label, enabled state, local safety allocation, per-call
reservation and research concurrency ceiling. Local safety limits are operator
configuration, not a claim about the provider's balance. A key without positive
allocation and reservation remains pending configuration. Unknown provider
ownership does not independently prevent a configured key from being selected.

Identical secrets are rejected using a fingerprint. Distinct keys are retained
and can be rotated independently. Rotation and retirement never erase charges,
unsettled reservations, or credentials still needed by unresolved research.

Shared-allowance observations are displayed separately from key observations.
Unknown shared balances are not summed into an available total. Equal balances,
labels, plans or timestamps are not ownership evidence. The current implementation
does not automatically discover or link common ownership, and provides no public
linking workflow. It can enforce reliable key-association metadata without a
parent management entity; synthetic tests verify shared budget and gate behavior.

This development redesign updates the initial schema and management contract
directly. It does not convert existing stores. An incompatible existing store is
rejected without alteration; use isolated synthetic state for development and
verification. No user database is deleted or migrated by this change.
