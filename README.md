# Exta

Exta is a Go gateway for Exa and Tavily with a shared REST/MCP service and an embedded management application.

The intended distribution is one executable for each of Windows x64 and Linux x64. Node is used only to build and test the frontend; SQLite data, configuration, and secrets remain external to the executable.

Development uses local simulated providers and synthetic test credentials. No live-provider certification or deployed service is implied.

Build and native verification instructions are in [docs/release.md](docs/release.md). Local initialization and protected state operations are documented in [docs/operations.md](docs/operations.md).
