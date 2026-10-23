CREATE TABLE legacy_provider_accounts(id TEXT PRIMARY KEY, provider TEXT NOT NULL, data TEXT NOT NULL, version INTEGER NOT NULL, created_at TEXT NOT NULL);
INSERT INTO legacy_provider_accounts SELECT * FROM provider_accounts;
CREATE TABLE legacy_record_payloads(kind TEXT NOT NULL, id TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(kind,id));
INSERT INTO legacy_record_payloads SELECT 'provider_key',id,data FROM provider_keys;
INSERT INTO legacy_record_payloads SELECT 'research_job',id,json_object('data',data,'execution_snapshot',execution_snapshot) FROM research_jobs;
INSERT INTO legacy_record_payloads SELECT 'usage_snapshot',id,data FROM usage_snapshots;

CREATE TABLE provider_keys_next(id TEXT PRIMARY KEY, provider TEXT NOT NULL, association_ref TEXT NOT NULL, fingerprint TEXT NOT NULL UNIQUE, active_version TEXT NOT NULL, data TEXT NOT NULL, version INTEGER NOT NULL, created_at TEXT NOT NULL);
INSERT INTO provider_keys_next
SELECT k.id,a.provider,a.id,k.fingerprint,k.active_version,
 json_set(json_remove(k.data,'$.account_id'),
  '$.association_ref',a.id,'$.association_evidence','legacy_operator_declared',
  '$.quota_policy',json_extract(a.data,'$.quota_policy'),
  '$.inherited_shared_limit',json_extract(a.data,'$.quota_policy'),
  '$.enabled',json(CASE WHEN json_extract(k.data,'$.enabled')=1 AND json_extract(a.data,'$.enabled')=1 THEN 'true' ELSE 'false' END)),
 k.version,k.created_at
FROM provider_keys k JOIN provider_accounts a ON a.id=k.account_id;
DROP TABLE provider_keys;
ALTER TABLE provider_keys_next RENAME TO provider_keys;
CREATE INDEX keys_association ON provider_keys(association_ref,id);

CREATE TABLE budget_reservations_next(id TEXT PRIMARY KEY, parent_id TEXT NOT NULL, principal_id TEXT NOT NULL REFERENCES principals(id), association_ref TEXT NOT NULL, key_id TEXT NOT NULL REFERENCES provider_keys(id), credential_version_id TEXT NOT NULL REFERENCES credential_versions(id), unit TEXT NOT NULL, amount INTEGER NOT NULL CHECK(amount>=0), state TEXT NOT NULL DEFAULT 'held', active_slot INTEGER NOT NULL DEFAULT 0, capacity_basis TEXT NOT NULL DEFAULT 'none', created_at TEXT NOT NULL, association_gate_generation INTEGER NOT NULL DEFAULT 0, key_gate_generation INTEGER NOT NULL DEFAULT 0, UNIQUE(parent_id,id));
INSERT INTO budget_reservations_next SELECT * FROM budget_reservations;
DROP TABLE budget_reservations;
ALTER TABLE budget_reservations_next RENAME TO budget_reservations;
CREATE INDEX reservations_association ON budget_reservations(association_ref,state);
CREATE INDEX reservations_principal ON budget_reservations(principal_id,active_slot);

CREATE TABLE usage_ledger_next(id TEXT PRIMARY KEY, source TEXT NOT NULL, event_kind TEXT NOT NULL, revision INTEGER NOT NULL, association_ref TEXT NOT NULL, principal_id TEXT NOT NULL REFERENCES principals(id), unit TEXT NOT NULL, amount INTEGER NOT NULL CHECK(amount>=0), certainty TEXT NOT NULL, created_at TEXT NOT NULL, key_id TEXT REFERENCES provider_keys(id), UNIQUE(source,event_kind,revision));
INSERT INTO usage_ledger_next
SELECT u.*, (SELECT r.key_id FROM budget_reservations r WHERE r.id=u.source AND r.association_ref=u.account_id) FROM usage_ledger u;
DROP TABLE usage_ledger;
ALTER TABLE usage_ledger_next RENAME TO usage_ledger;
CREATE INDEX ledger_association ON usage_ledger(association_ref);

CREATE TABLE usage_snapshots_next(id TEXT PRIMARY KEY, association_ref TEXT NOT NULL, key_id TEXT, observed_at TEXT NOT NULL, data TEXT NOT NULL);
INSERT INTO usage_snapshots_next
SELECT id,account_id,key_id,observed_at,json_set(json_remove(data,'$.account_id'),'$.association_ref',account_id) FROM usage_snapshots;
DROP TABLE usage_snapshots;
ALTER TABLE usage_snapshots_next RENAME TO usage_snapshots;

CREATE TABLE research_jobs_next(id TEXT PRIMARY KEY, principal_id TEXT NOT NULL REFERENCES principals(id), provider TEXT NOT NULL, association_ref TEXT NOT NULL, key_id TEXT NOT NULL REFERENCES provider_keys(id), credential_version_id TEXT NOT NULL REFERENCES credential_versions(id), state TEXT NOT NULL, upstream_id TEXT, data TEXT NOT NULL, input_cipher BLOB NOT NULL, result_cipher BLOB, execution_snapshot TEXT NOT NULL, next_poll_ms INTEGER NOT NULL DEFAULT 0, lease_token TEXT, lease_until_ms INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
INSERT INTO research_jobs_next
SELECT id,principal_id,provider,account_id,key_id,credential_version_id,state,upstream_id,data,input_cipher,result_cipher,
 json_set(json_remove(execution_snapshot,'$.account_id'),'$.association_ref',account_id),
 next_poll_ms,lease_token,lease_until_ms,version,created_at,updated_at FROM research_jobs;
DROP TABLE research_jobs;
ALTER TABLE research_jobs_next RENAME TO research_jobs;
CREATE UNIQUE INDEX research_upstream ON research_jobs(provider,association_ref,upstream_id) WHERE upstream_id IS NOT NULL;
CREATE INDEX research_queue ON research_jobs(state,next_poll_ms);
CREATE INDEX research_owner ON research_jobs(principal_id,created_at DESC,id DESC);

UPDATE runtime_gates SET scope='association' WHERE scope='account';
UPDATE rate_windows SET scope='association' WHERE scope='account';
DROP TABLE provider_accounts;
