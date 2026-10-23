# Exta

Exta is a Go gateway for Exa and Tavily with a shared REST/MCP service and an embedded management application.

The intended distribution is one executable for each of Windows x64 and Linux x64. Node is used only to build and test the frontend; SQLite data, configuration, and secrets remain external to the executable.

Development uses local simulated providers and synthetic test credentials. No live-provider certification or deployed service is implied.

The management application separates upstream keys, service access keys, statistics, call logs and settings. See [key administration and shared-allowance limits](docs/keys.md). Actual provider operations are invoked through REST or MCP.

Build and native verification instructions are in [docs/release.md](docs/release.md). Local initialization and protected state operations are documented in [docs/operations.md](docs/operations.md).

Linux amd64 container and persistent Compose setup are documented in [docs/docker.md](docs/docker.md). No application license has been assigned; bundled dependency notices do not grant a license to Exta itself.
