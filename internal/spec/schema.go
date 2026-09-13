package spec

import (
	"encoding/json"
	"fmt"
)

// RawSchema is the raw, version-agnostic shape of an OpenAPI v2/v3 schema
// object, unmarshaled directly from JSON before any $ref resolution or
// IR construction happens.
type RawSchema struct {
	Ref        string                `json:"$ref,omitempty"`
	Type       string                `json:"type,omitempty"`
	Properties map[string]*RawSchema `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
	Items      *RawSchema            `json:"items,omitempty"`
	Enum       []interface{}         `json:"enum,omitempty"`
	Nullable   bool                  `json:"nullable,omitempty"`
	XNullable  bool                  `json:"x-nullable,omitempty"`

	// TypeUnrepresentable is set by UnmarshalJSON when "type" was an
	// OpenAPI 3.1-style array (JSON Schema's `type: [...]`) that doesn't
	// resolve to exactly one non-null type — either none (`["null"]`, an
	// empty array) or more than one (`["string", "integer"]`). Neither
	// has a single generated type in any of this project's five targets,
	// so buildNode treats this the same as an unrecognized primitive:
	// skipped with a comment rather than guessed at. Type itself is left
	// "" in this case, same zero value as a schema with no "type" field
	// at all — TypeUnrepresentable is what tells buildNode the two
	// apart, since an absent type is legitimately object-shaped (see
	// buildNode's `case "object", "":`) while this is not.
	TypeUnrepresentable bool `json:"-"`

	AllOf                []*RawSchema          `json:"allOf,omitempty"`
	OneOf                []*RawSchema          `json:"oneOf,omitempty"`
	AnyOf                []*RawSchema          `json:"anyOf,omitempty"`
	AdditionalProperties *AdditionalProperties `json:"additionalProperties,omitempty"`
}

// IsNullable reports whether the schema is nullable under the v3
// "nullable" keyword, the v2 "x-nullable" vendor extension, or (see
// UnmarshalJSON) an OpenAPI 3.1-style `type: [T, "null"]`.
func (s *RawSchema) IsNullable() bool {
	return s.Nullable || s.XNullable
}

// UnmarshalJSON delegates every field except "type" to the default
// struct-tag-driven unmarshaling, and handles "type" itself specially:
// OpenAPI 3.0 and Swagger 2.0 always give it as a single string, but
// OpenAPI 3.1 (adopting JSON Schema 2020-12) also allows an array of
// strings, using the literal member "null" in place of the `nullable`
// keyword 3.1 removed. A ["string", "null"] array is normalized here
// into the same Type="string"/Nullable=true shape a 3.0 schema would
// produce with `type: string, nullable: true` — so every consumer of
// RawSchema, all the way through the IR and every generator, needs no
// awareness that 3.1 exists at all.
func (s *RawSchema) UnmarshalJSON(data []byte) error {
	type rawSchemaAlias RawSchema
	aux := &struct {
		Type json.RawMessage `json:"type,omitempty"`
		*rawSchemaAlias
	}{
		rawSchemaAlias: (*rawSchemaAlias)(s),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if len(aux.Type) == 0 {
		return nil
	}

	var typeStr string
	if err := json.Unmarshal(aux.Type, &typeStr); err == nil {
		s.Type = typeStr
		return nil
	}

	var typeArr []string
	if err := json.Unmarshal(aux.Type, &typeArr); err != nil {
		return fmt.Errorf(`schema "type": must be a string or an array of strings, got %s`, aux.Type)
	}
	var nonNull []string
	for _, t := range typeArr {
		if t == "null" {
			s.Nullable = true
			continue
		}
		nonNull = append(nonNull, t)
	}
	if len(nonNull) == 1 {
		s.Type = nonNull[0]
	} else {
		s.TypeUnrepresentable = true
	}
	return nil
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
