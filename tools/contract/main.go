// Command contract validates and regenerates Exta's shared API boundary artifacts.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaAddress = "https://exta.invalid/contract"

type document map[string]any

type exampleCase struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
	Valid  bool   `json:"valid"`
	Value  any    `json:"value"`
}

type artifactManifest struct {
	ContractVersion        string            `json:"contract_version"`
	ContractSHA256         string            `json:"contract_sha256"`
	Generator              string            `json:"generator"`
	Files                  map[string]string `json:"files"`
	FrontendContractSHA256 string            `json:"frontend_contract_sha256,omitempty"`
	FrontendDeferred       bool              `json:"frontend_deferred,omitempty"`
}

// main executes the explicit generate or read-only check action.
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	backendOnly := len(arguments) == 2 && arguments[1] == "--backend-only"
	if (len(arguments) != 1 && !backendOnly) || (arguments[0] != "generate" && arguments[0] != "check") {
		return errors.New("usage: go run ./tools/contract generate|check [--backend-only] (from repository root)")
	}
	source, err := os.ReadFile("api/openapi.yaml")
	if err != nil {
		return err
	}
	var contract document
	if err = json.Unmarshal(source, &contract); err != nil {
		return fmt.Errorf("canonical OpenAPI uses the JSON subset of YAML: %w", err)
	}
	if err = validate(contract); err != nil {
		return err
	}
	artifacts, err := generate(contract, digest(source), backendOnly)
	if err != nil {
		return err
	}
	manifest := artifactManifest{ContractVersion: contract["info"].(map[string]any)["version"].(string), ContractSHA256: digest(source), Generator: "exta-contract-v1/openapi-typescript-7.13.0/defaultNonNullable=false", Files: map[string]string{}}
	if backendOnly {
		previousBytes, err := os.ReadFile("api/contract-manifest.json")
		if err != nil {
			return err
		}
		var previous artifactManifest
		if err = json.Unmarshal(previousBytes, &previous); err != nil {
			return err
		}
		manifest.Generator = "exta-contract-v1/backend-only"
		manifest.FrontendContractSHA256 = previous.FrontendContractSHA256
		if manifest.FrontendContractSHA256 == "" {
			manifest.FrontendContractSHA256 = previous.ContractSHA256
		}
		manifest.FrontendDeferred = manifest.FrontendContractSHA256 != manifest.ContractSHA256
		for path, hash := range previous.Files {
			if strings.HasPrefix(path, "web/") {
				manifest.Files[path] = hash
			}
		}
	}
	for path, content := range artifacts {
		manifest.Files[path] = digest(content)
	}
	artifacts["api/contract-manifest.json"] = jsonBytes(manifest)
	for _, path := range sortedKeys(artifacts) {
		content := artifacts[path]
		if arguments[0] == "generate" {
			if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err = os.WriteFile(path, content, 0644); err != nil {
				return err
			}
		} else {
			actual, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(canonicalText(actual), canonicalText(content)) {
				return fmt.Errorf("generated artifact drift: %s; run contract generate", path)
			}
		}
	}
	fmt.Printf("contract %s: %d schemas, %d generated artifacts; SHA256 %s\n", arguments[0], len(schemaMap(contract)), len(artifacts), digest(source))
	return nil
}

func validate(contract document) error {
	meta, err := readJSON("tools/contract/testdata/oas31.json")
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.UseLoader(offlineLoader{})
	if err = compiler.AddResource("https://spec.openapis.org/oas/3.1/schema/2025-09-15", meta); err != nil {
		return err
	}
	metaSchema, err := compiler.Compile("https://spec.openapis.org/oas/3.1/schema/2025-09-15")
	if err != nil {
		return err
	}
	if err = metaSchema.Validate(map[string]any(contract)); err != nil {
		return fmt.Errorf("OpenAPI 3.1 validation: %w", err)
	}
	if err = compiler.AddResource(schemaAddress, map[string]any(contract)); err != nil {
		return err
	}
	compiled := map[string]*jsonschema.Schema{}
	for name := range schemaMap(contract) {
		compiled[name], err = compiler.Compile(schemaAddress + "#/components/schemas/" + name)
		if err != nil {
			return fmt.Errorf("schema %s: %w", name, err)
		}
	}
	exampleBytes, err := os.ReadFile("api/examples/cases.json")
	if err != nil {
		return err
	}
	var cases []exampleCase
	if err = json.Unmarshal(exampleBytes, &cases); err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, test := range cases {
		schema, exists := compiled[test.Schema]
		if !exists {
			return fmt.Errorf("example %s references unknown schema %s", test.Name, test.Schema)
		}
		validationErr := schema.Validate(test.Value)
		if (validationErr == nil) != test.Valid {
			return fmt.Errorf("example %s: expected valid=%t; validation=%v", test.Name, test.Valid, validationErr)
		}
		if test.Valid {
			covered[test.Schema] = true
		}
	}
	for name := range compiled {
		if !covered[name] {
			return fmt.Errorf("missing valid example for %s", name)
		}
	}
	if err = validateInvariants(contract); err != nil {
		return err
	}
	fmt.Printf("OpenAPI 3.1 and %d positive/negative examples validated offline\n", len(cases))
	return nil
}

func validateInvariants(contract document) error {
	extension := asMap(contract["x-exta"])
	for name, enabled := range asMap(extension["invariants"]) {
		if enabled != true {
			return fmt.Errorf("revision-2 invariant disabled: %s", name)
		}
	}
	required := []string{"binding_before_current_configuration", "replay_under_original_frozen_context", "concurrent_loser_reloads_winner_context", "observation_stop_retains_unsettled_holds", "terminal_without_cost_retains_budget", "write_off_consumes_reserved_budget", "admin_create_uses_shared_core", "admin_create_stable_principal"}
	for _, name := range required {
		if asMap(extension["invariants"])[name] != true {
			return fmt.Errorf("missing revision-2 invariant: %s", name)
		}
	}
	paths := asMap(contract["paths"])
	admin := asMap(asMap(paths["/v1/admin/research"])["post"])
	public := asMap(asMap(paths["/v1/research"])["post"])
	expectedIdentity := document{"operation": "research_create", "identity": "stable_principal", "replay_context": "original_persisted_context", "lookup_before": []string{"current_defaults", "current_route", "new_admission"}}
	if admin["x-permission"] != "admin:research:create" || admin["x-auth-before-use-case"] != true || !bytes.Equal(jsonBytes(admin["requestBody"]), jsonBytes(public["requestBody"])) || !bytes.Equal(jsonBytes(admin["x-idempotency"]), jsonBytes(expectedIdentity)) || !bytes.Equal(jsonBytes(public["x-idempotency"]), jsonBytes(expectedIdentity)) {
		return errors.New("admin/public Research must share body, stable-principal idempotency and explicit admin permission")
	}
	headers := map[string]bool{}
	for _, parameter := range admin["parameters"].([]any) {
		field := asMap(parameter)
		if field["in"] == "header" && field["required"] == true {
			headers[field["name"].(string)] = true
		}
	}
	if admin["x-origin-policy"] != "exact_configured_origin" {
		return errors.New("admin Research requires exact configured Origin validation")
	}
	for _, header := range []string{"X-Exta-CSRF", "Idempotency-Key"} {
		if !headers[header] {
			return fmt.Errorf("admin Research missing required %s header", header)
		}
	}
	if string(jsonBytes(admin["security"])) != string(jsonBytes([]any{map[string]any{"AdminSession": []any{}}})) {
		return errors.New("admin Research requires cookie authentication")
	}
	tools := map[string]bool{}
	operationIDs := map[string]bool{}
	for path, methods := range paths {
		for method, value := range asMap(methods) {
			operation := asMap(value)
			if strings.HasPrefix(path, "/v1/admin/") && method != "get" && method != "head" {
				if operation["x-origin-policy"] != "exact_configured_origin" {
					return fmt.Errorf("admin mutation %s %s lost exact Origin enforcement", method, path)
				}
				for _, parameter := range operation["parameters"].([]any) {
					field := asMap(parameter)
					if field["in"] == "header" && strings.EqualFold(field["name"].(string), "Origin") {
						return fmt.Errorf("admin mutation %s %s exposes browser-controlled Origin as caller input", method, path)
					}
				}
			}
			id := operation["operationId"].(string)
			if operationIDs[id] {
				return fmt.Errorf("duplicate operationId %s", id)
			}
			operationIDs[id] = true
			if name, ok := operation["x-mcp-tool"].(string); ok {
				tools[name] = true
			}
		}
	}
	for _, name := range []string{"search", "advanced_search", "fetch", "map", "crawl", "research_create", "research_get", "research_list", "research_cancel", "research_resume_observation", "capabilities"} {
		if !tools[name] {
			return fmt.Errorf("missing MCP tool %s", name)
		}
	}
	return nil
}

func generate(contract document, hash string, backendOnly bool) (map[string][]byte, error) {
	artifacts := map[string][]byte{}
	definitions := rewriteReferences(schemaMap(contract))
	schemas := document{"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": schemaAddress, "$defs": definitions, "x-contract-sha256": hash}
	artifacts["api/schemas/exta.json"] = jsonBytes(schemas)
	artifacts["internal/domain/schemas.gen.json"] = jsonBytes(schemas)
	goTypes, err := generateGo(contract, hash)
	if err != nil {
		return nil, err
	}
	artifacts["internal/domain/types.gen.go"] = goTypes
	tools := generateTools(contract, hash)
	if err = validateToolSchemas(tools); err != nil {
		return nil, err
	}
	artifacts["api/schemas/mcp-tools.json"] = jsonBytes(tools)
	if backendOnly {
		return artifacts, nil
	}
	temporary, err := os.MkdirTemp("", "exta-contract-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temporary)
	output := filepath.Join(temporary, "types.gen.ts")
	command := exec.Command("node", "web/node_modules/openapi-typescript/bin/cli.js", "api/openapi.yaml", "--output", output, "--alphabetize", "--default-non-nullable", "false")
	if result, commandErr := command.CombinedOutput(); commandErr != nil {
		return nil, fmt.Errorf("pinned local TypeScript generator failed (run npm ci --prefix web first): %w: %s", commandErr, result)
	}
	typescript, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	artifacts["web/src/generated/types.gen.ts"] = append([]byte("/** Generated from api/openapi.yaml; SHA256 "+hash+". Do not edit. */\n"), typescript...)
	artifacts["web/src/generated/client.gen.tsx"] = []byte(fmt.Sprintf(`/** Generated from api/openapi.yaml; SHA256 %s. Do not edit. */
import createClient from "openapi-fetch";
import type { ClientOptions } from "openapi-fetch";
import type { paths } from "./types.gen";

/**
 * Create a same-origin client; callers supply contract-required CSRF/idempotency headers.
 * @param options - Optional base URL and fetch adapter for local integration tests.
 */
export function createApiClient(options: Pick<ClientOptions, "baseUrl" | "fetch"> = {}) {
  return createClient<paths>({ baseUrl: "", ...options, credentials: "same-origin" });
}

export const CONTRACT_SHA256 = %q;
`, hash, hash))
	return artifacts, nil
}

func generateTools(contract document, hash string) []document {
	var tools []document
	paths := asMap(contract["paths"])
	for _, path := range sortedKeys(paths) {
		for _, value := range asMap(paths[path]) {
			operation := asMap(value)
			name, exists := operation["x-mcp-tool"].(string)
			if !exists {
				continue
			}
			input := document{"type": "object", "properties": map[string]any{}, "additionalProperties": false, "required": []any{}}
			if body, hasBody := operation["requestBody"]; hasBody {
				schema := asMap(asMap(asMap(body)["content"])["application/json"])["schema"]
				name := referenceName(asMap(schema)["$ref"].(string))
				input = clone(asMap(schemaMap(contract)[name]))
			}
			properties := asMap(input["properties"])
			required := input["required"].([]any)
			for _, value := range operation["parameters"].([]any) {
				parameter := asMap(value)
				name := parameter["name"].(string)
				if name == "Idempotency-Key" {
					name = "idempotency_key"
				} else if parameter["in"] == "header" {
					continue
				}
				properties[name] = parameter["schema"]
				if parameter["required"] == true {
					required = append(required, name)
				}
			}
			input["required"] = required
			input["$defs"] = referencedDefinitions(input, schemaMap(contract))
			input = asMap(rewriteReferences(input))
			responses := asMap(operation["responses"])
			var success any
			for _, status := range sortedKeys(responses) {
				if strings.HasPrefix(status, "2") {
					success = asMap(asMap(asMap(responses[status])["content"])["application/json"])["schema"]
					break
				}
			}
			output := document{"type": "object", "oneOf": []any{success, document{"$ref": "#/components/schemas/ApiError"}}}
			output["$defs"] = referencedDefinitions(output, schemaMap(contract))
			tools = append(tools, document{"name": name, "description": operation["summary"].(string) + ". Retrieval/research can incur upstream charges. Uses the same authorization, validation and service as REST.", "inputSchema": input, "outputSchema": rewriteReferences(output), "x-contract-sha256": hash, "x-operation-id": operation["operationId"], "x-permission": operation["x-permission"]})
		}
	}
	sort.Slice(tools, func(first, second int) bool { return tools[first]["name"].(string) < tools[second]["name"].(string) })
	return tools
}

func referencedDefinitions(value any, definitions document) document {
	result := document{}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case document:
			visit(map[string]any(typed))
		case map[string]any:
			for key, nested := range typed {
				if key == "$ref" {
					name := referenceName(nested.(string))
					if _, exists := result[name]; !exists {
						result[name] = definitions[name]
						visit(definitions[name])
					}
				} else {
					visit(nested)
				}
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		}
	}
	visit(value)
	return result
}

func validateToolSchemas(tools []document) error {
	for _, tool := range tools {
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.UseLoader(offlineLoader{})
		for _, direction := range []string{"inputSchema", "outputSchema"} {
			address := "https://exta.invalid/" + tool["name"].(string) + "/" + direction
			if err := compiler.AddResource(address, map[string]any(asMap(tool[direction]))); err != nil {
				return err
			}
			if _, err := compiler.Compile(address); err != nil {
				return fmt.Errorf("MCP %s %s: %w", tool["name"], direction, err)
			}
		}
	}
	return nil
}

func generateGo(contract document, hash string) ([]byte, error) {
	var output strings.Builder
	fmt.Fprintf(&output, "// Code generated by tools/contract from api/openapi.yaml; DO NOT EDIT.\n// Contract SHA256: %s\n\n// Package domain contains validated, generated Exta boundary types.\npackage domain\n\nimport \"encoding/json\"\n\nconst ContractSHA256 = %q\n", hash, hash)
	for _, name := range sortedKeys(schemaMap(contract)) {
		schema := asMap(schemaMap(contract)[name])
		fmt.Fprintf(&output, "\n// %s represents the canonical %s schema. Validate JSON before decoding.\n", name, name)
		if schemaType(schema) != "object" || schema["properties"] == nil {
			typeName := goType(schema)
			if typeName == "json.RawMessage" || typeName == "Integer" {
				fmt.Fprintf(&output, "type %s = %s\n", name, typeName)
			} else {
				fmt.Fprintf(&output, "type %s %s\n", name, typeName)
			}
			continue
		}
		fmt.Fprintf(&output, "type %s struct {\n", name)
		required := map[string]bool{}
		for _, field := range schema["required"].([]any) {
			required[field.(string)] = true
		}
		properties := asMap(schema["properties"])
		for _, field := range sortedKeys(properties) {
			property := asMap(properties[field])
			typeName := goType(property)
			jsonName := field
			if !required[field] {
				jsonName += ",omitempty"
				if property["anyOf"] != nil {
					typeName = "json.RawMessage"
				} else if !strings.HasPrefix(typeName, "*") {
					typeName = "*" + typeName
				}
			}
			fmt.Fprintf(&output, "%s %s `json:%q`\n", exportedName(field), typeName, jsonName)
		}
		output.WriteString("}\n")
	}
	return format.Source([]byte(output.String()))
}

func goType(schema document) string {
	if reference, exists := schema["$ref"].(string); exists {
		return referenceName(reference)
	}
	if variants, exists := schema["anyOf"].([]any); exists && len(variants) == 2 && asMap(variants[1])["type"] == "null" {
		return "*" + goType(asMap(variants[0]))
	}
	if types, ok := schema["type"].([]any); ok && len(types) == 2 && types[1] == "null" {
		return "*" + goType(document{"type": types[0]})
	}
	switch schema["type"] {
	case "string":
		return "string"
	case "integer":
		return "Integer"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		return "[]" + goType(asMap(schema["items"]))
	default:
		return "json.RawMessage"
	}
}

// schemaType identifies nullable OpenAPI 3.1 object definitions without losing their fields.
func schemaType(schema document) any {
	if types, ok := schema["type"].([]any); ok && len(types) == 2 && types[1] == "null" {
		return types[0]
	}
	return schema["type"]
}

func exportedName(value string) string {
	var output strings.Builder
	for _, part := range strings.Split(value, "_") {
		output.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return output.String()
}

func rewriteReferences(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, nested := range typed {
			if key == "$ref" {
				result[key] = strings.ReplaceAll(nested.(string), "#/components/schemas/", "#/$defs/")
			} else {
				result[key] = rewriteReferences(nested)
			}
		}
		return result
	case document:
		return rewriteReferences(map[string]any(typed))
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = rewriteReferences(nested)
		}
		return result
	default:
		return value
	}
}

func referenceName(reference string) string { return reference[strings.LastIndex(reference, "/")+1:] }
func schemaMap(contract document) document  { return asMap(asMap(contract["components"])["schemas"]) }
func asMap(value any) document {
	if result, ok := value.(document); ok {
		return result
	}
	return document(value.(map[string]any))
}

func clone(value document) document {
	var result document
	_ = json.Unmarshal(jsonBytes(value), &result)
	return result
}

func readJSON(path string) (any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(content))
}

func jsonBytes(value any) []byte {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(content, '\n')
}

func digest(content []byte) string {
	hash := sha256.Sum256(canonicalText(content))
	return hex.EncodeToString(hash[:])
}

// canonicalText makes generated text and its identity independent of checkout line endings.
func canonicalText(content []byte) []byte {
	return bytes.ReplaceAll(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type offlineLoader struct{}

func (offlineLoader) Load(address string) (any, error) {
	return nil, fmt.Errorf("network/file schema loading disabled: %s", address)
}
