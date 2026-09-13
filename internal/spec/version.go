package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Version identifies which OpenAPI/Swagger major version a document uses.
type Version int

const (
	VersionUnknown Version = iota
	VersionV2
	VersionV3
)

// normalizeToJSON returns data as JSON bytes. If data is already JSON
// (starts with '{' or '['), it is returned unchanged; otherwise it is
// parsed as YAML and re-encoded as JSON so the rest of the package only
// ever deals with one format.
func normalizeToJSON(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return data, nil
	}
	var generic interface{}
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return nil, fmt.Errorf("parse spec as YAML: %w", err)
	}
	jsonData, err := json.Marshal(normalizeYAMLValue(generic))
	if err != nil {
		return nil, fmt.Errorf("convert parsed YAML to JSON: %w", err)
	}
	return jsonData, nil
}

// normalizeYAMLValue rewrites a value decoded from YAML so that it is
// JSON-encodable. YAML permits non-string mapping keys (e.g. the unquoted
// numeric HTTP status codes that are pervasive under `responses:`), which
// decode into map[interface{}]interface{} — a type encoding/json refuses
// outright. Such maps are converted to map[string]interface{} with keys
// stringified via fmt.Sprint, recursing through nested maps and slices.
func normalizeYAMLValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[fmt.Sprint(k)] = normalizeYAMLValue(item)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[k] = normalizeYAMLValue(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = normalizeYAMLValue(item)
		}
		return out
	default:
		return v
	}
}

type versionProbe struct {
	Swagger string `json:"swagger"`
	OpenAPI string `json:"openapi"`
}

// DetectVersion inspects the top-level "openapi" or "swagger" field of a
// normalized JSON document to determine which spec version it uses.
// Only OpenAPI 3.0.x and Swagger 2.0 are supported: OpenAPI 3.1+ changes
// schema semantics (notably `type: [T, "null"]` in place of `nullable`)
// in ways this parser does not model, so it is rejected explicitly rather
// than accepted and failed confusingly further downstream.
func DetectVersion(jsonData []byte) (Version, error) {
	var probe versionProbe
	if err := json.Unmarshal(jsonData, &probe); err != nil {
		return VersionUnknown, fmt.Errorf("detect spec version: %w", err)
	}
	switch {
	case strings.HasPrefix(probe.OpenAPI, "3.0"):
		return VersionV3, nil
	case strings.HasPrefix(probe.OpenAPI, "3."):
		return VersionUnknown, fmt.Errorf("detect spec version: OpenAPI 3.1+ is not yet supported (only 3.0.x and Swagger 2.0)")
	case probe.Swagger == "2.0":
		return VersionV2, nil
	default:
		return VersionUnknown, fmt.Errorf("detect spec version: missing or unrecognized 'openapi'/'swagger' field")
	}
}
