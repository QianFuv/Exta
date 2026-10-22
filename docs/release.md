# Windows and Linux x64 candidates

Exta supports `windows/amd64` and `linux/amd64`. Historical artifacts for other targets are retained as historical evidence, not current release commitments.

Build the production management application with the pinned Node/npm versions, then build both standalone executables from the repository root:

```text
npm ci --prefix web
npm run build --prefix web
go run ./tools/release verify --output <new-directory> --version <candidate-version>
```

The frontend bridge preserves the complete relative chunk graph inside `assets/<bundle-sha256>/`. It structurally rewrites index resource URLs and records exact per-file bytes, sizes and SHA-256 values. The namespace digest hashes UTF-8 bytewise-sorted records `<relative-path>\0<file-sha256>\n`, excluding the index and metadata. The runtime independently checks the complete namespace, each file and the absence of undeclared files before serving anything. Scripts, styles, fonts and favicon run under the existing same-origin CSP. Production output rejects MSW/fixture modules. Node, adjacent frontend files and SQLite DLLs are not runtime dependencies.

`internal/webassets/production.zip` contains the bundle, frontend provenance and shipped dependency license texts. `npm run build` stages and syncs one complete archive, preserves its predecessor under `artifacts/embed`, then atomically replaces the archive file. Interruption before replacement leaves the previous archive usable; incomplete staging output is never selected. The ZIP is embedded into the executable, not read from adjacent runtime files. Source hashes are compared before and after compilation. Release verification rejects stale source sets, generated contracts, manifests or license bytes. Old clients may need a reload after an upgrade before loading previously unused lazy chunks; preserving build artifacts does not serve historical URLs.

The release tool uses CGO disabled, `-trimpath`, stripped debug symbols and the supplied version. It records exact source hashes, toolchain, flags, contract identity, Git reference, executable/ZIP sizes and SHA-256 values. A minimal empty Go executable supplies each target's footprint baseline. Actual PE imports and ELF dynamic dependencies are checked. Archives contain Go/runtime and shipped frontend dependency license evidence. The application has no declared distribution license; the tool does not invent one. No publication, signing or paid-provider call occurs.

Native smoke uses a specific executable:

```text
EXTA_SMOKE_BINARY=<absolute native executable path>
go test ./tools/release -run TestPackagedRuntime -v -count=1 -timeout=150s
```

Set the environment variable using the host shell's syntax. Smoke copies the executable into an isolated directory, creates synthetic private state and checks local REST/MCP, specific lock/port refusals, locked-key behavior, backup/current-schema migration, stale rollback refusal and maintenance mode. Research recovery fixtures have expired observation windows, invalid credentials and blocked dispatch gates. Assertions cover observation-limit reasons, unchanged dispatch counters and no dispatch events. Forced termination exercises restart recovery; it is not evidence of graceful console-signal delivery. In-process tests cover draining/finalizer joining.

Native and browser evidence must identify the executable hash. Cross-build success alone is build-only. Windows tests cannot establish Linux execution or race coverage. Missing isolated Linux prerequisites must be reported without starting or changing unrelated services. Only schema 1 exists; no-op migration evidence does not establish an actual schema-upgrade interruption result.

CI uses locked dependencies and caches, builds the frontend once, checks Go with the Linux race detector, cross-builds only the two supported targets, then runs native/browser acceptance on the corresponding x64 runners. It publishes no release or tag. Local workflow preparation is not a claim that hosted CI has run.

An interrupted output directory without final manifest/checksums is not verified. Use a new directory per candidate. Rebuild after any source change. Historical candidates and user runtime data are never cleaned by the release tool.
