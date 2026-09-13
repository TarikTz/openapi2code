package spec

import (
	"fmt"
	"strings"
)

const (
	refPrefixV2 = "#/definitions/"
	refPrefixV3 = "#/components/schemas/"
)

// ResolveRefName extracts the schema name from an internal $ref string
// (e.g. "#/components/schemas/Pet" or "#/definitions/Pet" -> "Pet") and
// verifies it exists in the document. External or otherwise-shaped refs
// are rejected: only internal refs are supported.
func (d *RawDocument) ResolveRefName(ref string) (string, error) {
	var name string
	switch {
	case strings.HasPrefix(ref, refPrefixV2):
		name = strings.TrimPrefix(ref, refPrefixV2)
	case strings.HasPrefix(ref, refPrefixV3):
		name = strings.TrimPrefix(ref, refPrefixV3)
	default:
		return "", fmt.Errorf("resolve ref %q: unsupported or external ref (only internal #/definitions/* and #/components/schemas/* refs are supported)", ref)
	}
	if _, ok := d.Schemas[name]; !ok {
		return "", fmt.Errorf("resolve ref %q: schema %q not found", ref, name)
	}
	return name, nil
}

func (d *RawDocument) validateRefs() error {
	for name, s := range d.Schemas {
		if err := validateSchemaRefs(d, s); err != nil {
			return fmt.Errorf("schema %q: %w", name, err)
		}
	}
	return nil
}

func validateSchemaRefs(d *RawDocument, s *RawSchema) error {
	if s == nil {
		return nil
	}
	if s.Ref != "" {
		_, err := d.ResolveRefName(s.Ref)
		return err
	}
	for name, p := range s.Properties {
		if err := validateSchemaRefs(d, p); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	if s.Items != nil {
		if err := validateSchemaRefs(d, s.Items); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	if s.AdditionalProperties != nil && s.AdditionalProperties.Schema != nil {
		if err := validateSchemaRefs(d, s.AdditionalProperties.Schema); err != nil {
			return fmt.Errorf("additionalProperties: %w", err)
		}
	}
	for i, sub := range s.AllOf {
		if err := validateSchemaRefs(d, sub); err != nil {
			return fmt.Errorf("allOf[%d]: %w", i, err)
		}
	}
	for i, sub := range s.OneOf {
		if err := validateSchemaRefs(d, sub); err != nil {
			return fmt.Errorf("oneOf[%d]: %w", i, err)
		}
	}
	for i, sub := range s.AnyOf {
		if err := validateSchemaRefs(d, sub); err != nil {
			return fmt.Errorf("anyOf[%d]: %w", i, err)
		}
	}
	return nil
}

// Parse normalizes (YAML or JSON), version-detects, raw-parses, and
// validates every $ref in an OpenAPI/Swagger document. It is the single
// entry point internal/ir and pkg/engine use to go from raw bytes to a
// validated RawDocument.
func Parse(data []byte) (*RawDocument, error) {
	jsonData, err := normalizeToJSON(data)
	if err != nil {
		return nil, err
	}
	version, err := DetectVersion(jsonData)
	if err != nil {
		return nil, err
	}
	doc, err := ParseRaw(jsonData, version)
	if err != nil {
		return nil, err
	}
	if err := doc.validateRefs(); err != nil {
		return nil, err
	}
	return doc, nil
}
