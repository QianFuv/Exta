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

Production browser acceptance uses the existing Playwright installation and the real executable:

```text
EXTA_E2E_BINARY=<absolute native executable path>
npm run test:e2e --prefix web
```

The harness creates private temporary state, seeds offline Research bindings, blocks credential dispatch, and starts only its own process. It verifies cookie login/logout, CSRF rejection, the five management destinations and embedded chunks, concurrent key edits, token creation/revocation, persistent light/dark/system appearance, keyboard controls and request-detail drawers. Axe checks and desktop/mobile captures cover the exercised screens. Cleanup checks task/reservation counts and zero dispatch evidence. Research execution remains an API/MCP capability and is not exposed as a browser workspace. The browser suite does not certify live provider responses or paid execution. `EXTA_GO_BINARY` optionally selects the installed Go executable for fixture setup.

CI uses locked dependencies and caches, builds the frontend once, checks Go with the Linux race detector, cross-builds only the two supported targets, then runs native/browser acceptance on the corresponding x64 runners. It publishes no release or tag. Local workflow preparation is not a claim that hosted CI has run.

The separate manually dispatched `Release` workflow is restricted to `QianFuv/Exta` on `main`. It reuses all verification gates with version `0.1.3`, including isolated Linux container acceptance, before uploading the two archives/checksums and the tested Linux image to `ghcr.io/qianfuv/exta`. Its concurrency policy never cancels a release in progress. Artifact names identify platform and source SHA; manifests supply binary/archive SHA-256 values, and job summaries retain the source and verification scope. A publication failure requires checking the existing release, tags and package before retrying. Package public visibility must be verified separately. See [container persistence and proxy setup](docker.md).

Publication resolves any existing version tag to the accepted commit and verifies existing container config/digests before pushing missing tags. Release uploads use a draft; existing completed assets must match exact hashes, and only incomplete `starter` uploads on a matching draft may be removed and retried. Final publication follows a complete asset readback. Rerun the failed publication job to reuse its exact accepted artifact set; a fresh build with different manifests is deliberately rejected against existing assets. Authentication or network failure is never treated as evidence of package absence.

An interrupted output directory without final manifest/checksums is not verified. Use a new directory per candidate. Rebuild after any source change. Historical candidates and user runtime data are never cleaned by the release tool.

Version 0.1.1 changes the development initial schema/checksum and does not support an in-place 0.1.0 database upgrade. Existing runtime data must remain intact; initialize a separate directory for a new deployment. See [the changelog](../CHANGELOG.md).
