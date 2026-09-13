package spec

import (
	"encoding/json"
	"fmt"
)

// RawSchema is the raw, version-agnostic shape of an OpenAPI v2/v3 schema
// object, unmarshaled directly from JSON before any $ref resolution or
// IR construction happens.
type RawSchema struct {
	Ref                  string                `json:"$ref,omitempty"`
	Type                 string                `json:"type,omitempty"`
	Properties           map[string]*RawSchema `json:"properties,omitempty"`
	Required             []string              `json:"required,omitempty"`
	Items                *RawSchema            `json:"items,omitempty"`
	Enum                 []interface{}         `json:"enum,omitempty"`
	Nullable             bool                  `json:"nullable,omitempty"`
	XNullable            bool                  `json:"x-nullable,omitempty"`
	AllOf                []*RawSchema          `json:"allOf,omitempty"`
	OneOf                []*RawSchema          `json:"oneOf,omitempty"`
	AnyOf                []*RawSchema          `json:"anyOf,omitempty"`
	AdditionalProperties *AdditionalProperties `json:"additionalProperties,omitempty"`
}

// IsNullable reports whether the schema is nullable under either the v3
// "nullable" keyword or the v2 "x-nullable" vendor extension.
func (s *RawSchema) IsNullable() bool {
	return s.Nullable || s.XNullable
}

// AdditionalProperties represents JSON Schema's additionalProperties
// keyword, which is polymorphic in raw JSON: a boolean ("are extra
// properties allowed at all") or a schema object ("extra properties
// must look like this"). Schema is non-nil only for the schema-object
// form; Allowed carries the boolean form's value (and is true whenever
// Schema is set, since providing a schema implies extra properties are
// allowed).
type AdditionalProperties struct {
	Schema  *RawSchema
	Allowed bool
}

// UnmarshalJSON tries the boolean form first, then falls back to a
// schema object — a schema object always unmarshals successfully into
// *RawSchema (every field is optional), so trying it first would never
// even attempt the boolean form.
func (a *AdditionalProperties) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		a.Allowed = b
		a.Schema = nil
		return nil
	}
	var s RawSchema
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("additionalProperties: %w", err)
	}
	a.Schema = &s
	a.Allowed = true
	return nil
}

// RawDocument is the version-normalized set of named schemas from an
// OpenAPI/Swagger document: v3's components.schemas and v2's definitions
// are both flattened into the same Schemas map, keyed by schema name.
type RawDocument struct {
	Version Version
	Schemas map[string]*RawSchema
}

type rawDocV3 struct {
	Components struct {
		Schemas map[string]*RawSchema `json:"schemas"`
	} `json:"components"`
}

type rawDocV2 struct {
	Definitions map[string]*RawSchema `json:"definitions"`
}

// ParseRaw unmarshals a normalized JSON document of the given version into
// a RawDocument. It does not resolve or validate $refs.
func ParseRaw(jsonData []byte, version Version) (*RawDocument, error) {
	switch version {
	case VersionV3:
		var doc rawDocV3
		if err := json.Unmarshal(jsonData, &doc); err != nil {
			return nil, fmt.Errorf("parse v3 spec: %w", err)
		}
		return &RawDocument{Version: version, Schemas: doc.Components.Schemas}, nil
	case VersionV2:
		var doc rawDocV2
		if err := json.Unmarshal(jsonData, &doc); err != nil {
			return nil, fmt.Errorf("parse v2 spec: %w", err)
		}
		return &RawDocument{Version: version, Schemas: doc.Definitions}, nil
	default:
		return nil, fmt.Errorf("parse spec: unsupported version")
	}
}
