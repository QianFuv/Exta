# Backend candidate verification

Run the release tool from the repository root with the pinned local Go toolchain:

```text
go run ./tools/release verify --backend-only --output <new-directory> --version <candidate-version>
```

It builds Windows amd64, Linux amd64/arm64 and macOS amd64/arm64 with CGO disabled, `-trimpath`, stripped debug symbols and the supplied version. It records the toolchain, flags, exact source hashes, contract hash, Git reference, executable/ZIP sizes and SHA-256 values. A minimal empty Go program built with the same flags supplies each target's size baseline and measured difference. Binary metadata and actual import descriptors are checked; linked dependencies require collected license evidence. Uncached modules present only in the module graph are identified explicitly. The Go runtime license is included. The application currently has no declared distribution license; the tool does not invent one or publish artifacts.

The embedded bundle declares backend-only mode. Production packaging without `--backend-only` fails until verified frontend assets are available. Static routing is tested using synthetic in-memory fixtures; those fixtures are never packaged as product UI. No frontend build, Node invocation, adjacent web directory or SQLite DLL is required to run a backend candidate.

For a native executable, set `EXTA_SMOKE_BINARY` to its absolute path and run:

```text
go test ./tools/release -run TestPackagedRuntime -v -count=1 -timeout=150s
```

The test copies the executable into an isolated directory, creates only synthetic private state, checks local REST/MCP, specific second-instance and port refusal diagnostics, locked-key behavior, backup/current-schema migration, stale rollback refusal and maintenance mode. Recovery fixtures have expired observation windows, invalid credentials and blocked dispatch gates. It verifies the observation-limit reason, unchanged dispatch counters and absence of dispatch events. Forced process termination exercises restart behavior; it is not evidence of graceful console-signal delivery. In-process lifecycle tests separately verify admission/draining and finalization joining.

Cross-build success is recorded as build-only until native execution evidence is added separately. Windows smoke does not establish Linux/macOS native acceptance or Linux race coverage. WSL is reserved for another authorized maintenance task; this task does not start, stop or use it for tests. Native non-Windows tests, Linux race, production UI acceptance, real provider calls, signing, notarization and publication are not claimed.

An interrupted release directory lacks the final release manifest/checksums and is not a verified candidate. Use a new output directory for each run. Source hashes must remain unchanged across the complete build matrix. Artifact creation does not create Git tags, branches, remote pushes or releases.
