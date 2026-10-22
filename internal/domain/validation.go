package domain

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas.gen.json
var schemaDocument []byte

var compiledSchemas = sync.OnceValues(compileSchemas)

// ValidateJSON validates an untrusted boundary value against the frozen schema.
// It preserves omitted optional values: research defaults are resolved only after
// binding lookup using the original replay context or a new admission snapshot.
func ValidateJSON(name string, data []byte) error {
	schemas, err := compiledSchemas()
	if err != nil {
		return err
	}
	schema, exists := schemas[name]
	if !exists {
		return fmt.Errorf("unknown contract schema %q", name)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid JSON")
	}
	if err = schema.Validate(value); err != nil {
		return fmt.Errorf("invalid %s payload: %w", name, err)
	}
	if name == "ResearchCreate" || name == "AdvancedSearchRequest" {
		request := value.(map[string]any)
		if output, exists := request["output_schema"]; exists {
			if err = validateOutputSchema(output); err != nil {
				return err
			}
		}
		if options, exists := request["provider_options"].(map[string]any); exists {
			if output, exists := options["output_schema"]; exists {
				return validateOutputSchema(output)
			}
		}
	}
	return nil
}

func compileSchemas() (map[string]*jsonschema.Schema, error) {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaDocument))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.UseLoader(denySchemaLoads{})
	const address = "https://exta.invalid/contract"
	if err = compiler.AddResource(address, value); err != nil {
		return nil, err
	}
	schemas := make(map[string]*jsonschema.Schema)
	for name := range value.(map[string]any)["$defs"].(map[string]any) {
		schemas[name], err = compiler.Compile(address + "#/$defs/" + name)
		if err != nil {
			return nil, err
		}
	}
	return schemas, nil
}

func validateOutputSchema(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 16384 {
		return errors.New("output_schema exceeds 16 KiB")
	}
	if err = checkSchemaBounds(value, 0); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denySchemaLoads{})
	const address = "https://exta.invalid/output"
	if err = compiler.AddResource(address, value); err != nil {
		return errors.New("invalid output_schema")
	}
	if _, err = compiler.Compile(address); err != nil {
		return errors.New("invalid or unsupported output_schema")
	}
	return nil
}

func checkSchemaBounds(value any, depth int) error {
	if depth > 8 {
		return errors.New("output_schema exceeds nesting depth 8")
	}
	switch item := value.(type) {
	case map[string]any:
		if len(item) > 128 {
			return errors.New("output_schema object exceeds 128 properties")
		}
		for key, nested := range item {
			if key == "$ref" {
				reference, valid := nested.(string)
				if !valid || !strings.HasPrefix(reference, "#") {
					return errors.New("output_schema permits local fragment references only")
				}
			}
			if err := checkSchemaBounds(nested, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, nested := range item {
			if err := checkSchemaBounds(nested, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

type denySchemaLoads struct{}

func (denySchemaLoads) Load(string) (any, error) {
	return nil, errors.New("external schema loading is disabled")
}
