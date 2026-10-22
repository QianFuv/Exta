// Package mcpapi adapts the frozen tools to the shared authenticated service.
package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"exta/api"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Backend is the same application entry point used by REST handlers.
type Backend interface {
	CallTool(context.Context, store.Identity, string, json.RawMessage, string) (any, error)
}

type definition struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Permission   string          `json:"x-permission"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

type requestContext struct {
	identity store.Identity
	id       string
	original context.Context
}
type requestKey struct{}
type noSchemaNetwork struct{}

type privateResponse struct {
	http.ResponseWriter
	wroteHeader bool
}

func (writer *privateResponse) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	value := writer.Header().Get("Cache-Control")
	if !strings.Contains(value, "no-store") {
		if value != "" {
			value += ", "
		}
		writer.Header().Set("Cache-Control", value+"no-store")
	}
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *privateResponse) Write(data []byte) (int, error) {
	writer.WriteHeader(http.StatusOK)
	return writer.ResponseWriter.Write(data)
}

func (writer *privateResponse) Flush() {
	writer.WriteHeader(http.StatusOK)
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *privateResponse) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (noSchemaNetwork) Load(string) (any, error) {
	return nil, errors.New("external schemas are disabled")
}

// New registers raw schemas without applying defaults, and fixes protocol support.
func New(auth *security.Auth, backend Backend, version string) (http.Handler, error) {
	var definitions []definition
	if err := json.Unmarshal(api.Tools, &definitions); err != nil {
		return nil, err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "exta", Version: version}, &mcp.ServerOptions{SupportedProtocolVersions: []string{"2026-07-28", "2025-11-25"}})
	for _, entry := range definitions {
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.AssertFormat()
		compiler.UseLoader(noSchemaNetwork{})
		value, err := jsonschema.UnmarshalJSON(bytes.NewReader(entry.InputSchema))
		if err != nil {
			return nil, err
		}
		address := "https://exta.invalid/tools/" + entry.Name
		if err = compiler.AddResource(address, value); err != nil {
			return nil, err
		}
		schema, err := compiler.Compile(address)
		if err != nil {
			return nil, err
		}
		server.AddTool(&mcp.Tool{Name: entry.Name, Description: entry.Description, InputSchema: entry.InputSchema, OutputSchema: entry.OutputSchema}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			identity, ok := ctx.Value(requestKey{}).(requestContext)
			if !ok {
				return toolResult(service.ErrorResponse(store.Reject("authentication_required", 401, "Bearer principal is required"), store.ID("request")), true), nil
			}
			if err := security.RequireScope(identity.identity, entry.Permission); err != nil {
				return toolResult(service.ErrorResponse(err, identity.id), true), nil
			}
			arguments := request.Params.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			value, err := jsonschema.UnmarshalJSON(bytes.NewReader(arguments))
			if err == nil {
				err = schema.Validate(value)
			}
			if err != nil {
				return toolResult(service.ErrorResponse(store.Reject("invalid_request", 400, "Tool arguments do not match the frozen schema"), identity.id), true), nil
			}
			bounded, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(identity.original, cancel)
			defer stop()
			defer cancel()
			if identity.original.Err() != nil {
				cancel()
			}
			result, err := backend.CallTool(bounded, identity.identity, entry.Name, arguments, identity.id)
			if err != nil {
				return toolResult(service.ErrorResponse(err, identity.id), true), nil
			}
			return toolResult(result, false), nil
		})
	}
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true, JSONResponse: true, MaxRequestBodyBytes: 1 << 20})
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		identity, err := auth.Bearer(request)
		if err != nil {
			security.WriteError(writer, request, err)
			return
		}
		if request.Method == http.MethodPost {
			settings, err := auth.Store.Settings(request.Context())
			if err != nil {
				security.WriteError(writer, request, err)
				return
			}
			request.Body = http.MaxBytesReader(writer, request.Body, int64(settings.Limits.RequestBytes))
			if request.Header.Get("Mcp-Protocol-Version") == "" {
				data, err := io.ReadAll(request.Body)
				var body struct {
					Method string          `json:"method"`
					ID     json.RawMessage `json:"id"`
				}
				if err != nil {
					security.WriteError(writer, request, store.Reject("request_too_large", 413, "MCP request exceeds the configured limit"))
					return
				}
				if json.Unmarshal(data, &body) != nil || body.Method != "initialize" {
					writer.Header().Set("Content-Type", "application/json")
					writer.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "error": map[string]any{"code": -32600, "message": "Mcp-Protocol-Version is required outside legacy initialization"}})
					return
				}
				request.Body = io.NopCloser(bytes.NewReader(data))
			}
		}
		info := requestContext{identity: identity, id: security.RequestID(request), original: request.Context()}
		transport.ServeHTTP(&privateResponse{ResponseWriter: writer}, request.WithContext(context.WithValue(request.Context(), requestKey{}, info)))
	}), nil
}

func toolResult(value any, isError bool) *mcp.CallToolResult {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(`{"error":{"code":"storage_unavailable","message":"Result cannot be encoded"}}`)
		isError = true
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, StructuredContent: json.RawMessage(data), IsError: isError}
}
