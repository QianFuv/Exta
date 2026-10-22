# Exta

Exta is a Go gateway for Exa and Tavily with a shared REST/MCP service and an embedded management application.

The intended distribution is one executable per target platform. Node is used only to build and test the frontend; SQLite data, configuration, and secrets remain external to the executable.

Development uses local simulated providers and synthetic test credentials. No live-provider certification or deployed service is implied.
