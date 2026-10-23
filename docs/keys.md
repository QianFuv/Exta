# Key administration

An upstream key is the smallest configuration and scheduling unit. Create Exa or
Tavily keys directly; there is no account resource, account creation step, or
separately managed quota group. Service access keys are separate credentials for
clients calling Exta and have their own page and permissions.

Each upstream key has a label, enabled state, local safety allocation, per-call
reservation and research concurrency ceiling. Local safety limits are operator
configuration, not a claim about the provider's balance. Missing, null or zero local limits mean no local cap; only positive caps restrict
admission. A zero per-call reservation uses the operation cost estimate. Unknown
provider balance does not prevent selection. This also applies to
existing stored zero limits; charges and unsettled holds remain recorded. Unknown
upstream balances remain unknown and are never presented as unlimited funds.

Identical secrets are rejected using a fingerprint. Distinct keys are retained
and can be rotated independently. Rotation and retirement never erase charges,
unsettled reservations, or credentials still needed by unresolved research.

Exta does not identify common ownership or shared quota pools between different
keys, and does not reject different keys on that basis. Only identical credential
fingerprints are rejected. No ownership-verification or linking workflow is exposed.
Historical accounting metadata and unsettled reservations remain intact.

The inventory shows each key's usage against a comparable total. A positive local
allocation uses local lifetime usage; otherwise a verified key-level upstream
total may be used for that same observation period. Unknown totals have no
percentage. Remaining balances are not treated as totals, and different keys'
upstream balances are never added into a promised available total.

This development redesign updates the initial schema and management contract
directly. It does not convert existing stores. An incompatible existing store is
rejected without alteration; use isolated synthetic state for development and
verification. No user database is deleted or migrated by this change.
