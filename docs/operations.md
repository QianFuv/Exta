# Local backend operations

Commands use an absolute external `--data-dir`. Provider calls are never part of doctor, backup, restore or migration. Stop the service explicitly before operating on its state; these commands refuse a held directory lock and never stop another process.

On Windows, executable startup sets its own process token's default owner to the process user. This keeps files and SQLite sidecars user-owned even when an elevated process would otherwise create Administrators-owned objects. It does not enable privileges, change host policy or repair existing files; strict owner, DACL and reparse checks remain active.

Initialization creates missing directories with owner-only access at creation. An existing empty directory must already be private; an insecure directory is rejected rather than tightened in place because previously opened handles cannot be revoked. Choose a new leaf in that case. Existing nonempty directories are preserved.

- `exta doctor --data-dir <path>` reports local permission/configuration/TLS, schema/integrity, encrypted-state and point-in-time port checks. It validates embedded production assets; browser acceptance remains separate execution evidence.
- `exta backup --data-dir <path> --output <new-directory>` creates a WAL-consistent SQLite snapshot, exact configuration and verified manifest. The parent output directory must exist. A separate owner-only `<new-directory>.key-recovery` contains the master key; `--key-output <new-directory>` chooses that separate destination. Neither output may exist or overlap the runtime directory. Preserve both through an independently protected backup route. External TLS dependencies are listed, not collected.
- `exta migrate --data-dir <path> --backup <new-directory>` first verifies state and creates a recoverable backup. Schema 1 is currently the only published schema: this command reports that it is current and applies no migration. Unknown versions, missing history and checksum/schema drift are rejected. No downgrade or initialization occurs.
- `exta restore --data-dir <existing-path> --from <backup-directory>` requires an intact destination, its matching key, matching database lineage/schema/page size, and exactly equal activity generations. New acceptance, transmissions, research transitions, cleanup and reopening ordinary admission invalidate older rollback snapshots. There is no force bypass or automatic stale restore.

Restore preserves previous local recovery material and atomically publishes a pending `restore-operation.json` before database mutation. The SQLite restore transaction preserves WAL consistency. Configuration publication precedes a durable completion receipt. A pending operation blocks startup and other operations; rerun restore with the same verified backup to finish. A completed receipt is retained instead of using deletion as a commit signal. Corrupt or unreadable current state requires explicit diagnosis; it is never treated as an empty restore destination.

File hashes detect corruption, not an attacker who can rewrite both payload and manifest. The master-key fingerprint does not substitute for the database lineage identity. Missing or wrong keys cannot produce a verified recoverable backup. No key is printed by these operational commands.

`serve --maintenance` provides local inspection without provider dispatch, request recovery, research recovery or retention workers. Ordinary serving closes admission, cancels and joins workers and request finalizers before closing SQLite and releasing its lock.

Retention performs at most 100 changes per category per transaction, with a five-second cancellation bound and a one-minute interval. It records cleanup counts. Expired sessions, resolved request/attempt metadata, resolved research content, old audit/events and expired resolved idempotency bindings are eligible. Latest upstream observations and latest known amounts remain available. Unresolved research evidence and frozen replay context are preserved. Financial ledger entries, settlement deduplication, reservations, credential versions and upstream job identities are retained; cleanup never replenishes spent budget. Encrypted content removal does not securely erase old storage blocks or external backups.

Production builds embed the management UI and support Windows/Linux x64. Browser and native platform acceptance are recorded separately against each executable hash; build success alone does not establish them. Live paid-provider validation is not performed. Current schema-1 no-op migration tests do not establish a real schema-upgrade interruption result.

## Administrator password

Initialize new state with `exta init --password-stdin --data-dir <new-private-directory>`, piping one UTF-8 password of 16–512 characters from protected input. A terminal newline is optional; spaces are significant. Direct terminal stdin does not hide typing. Initialization never prints the password.

The password is persistent; logging in or out does not consume it. New state stores a salted PBKDF2-HMAC-SHA256 verifier (600,000 iterations). Existing random administrator credentials and their stored verifiers remain unchanged and reusable. An older binary cannot authenticate a newly initialized password verifier. No automatic reset, conversion or production credential change occurs. Client API/MCP access keys remain separate; the encryption master key is not a login password.
