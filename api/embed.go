// Package api embeds the frozen public API contract for the Go runtime.
package api

import _ "embed"

// OpenAPI is the canonical JSON-compatible OpenAPI document.
//
//go:embed openapi.yaml
var OpenAPI []byte

// Tools contains the canonical raw MCP input/output schemas.
//
//go:embed schemas/mcp-tools.json
var Tools []byte
