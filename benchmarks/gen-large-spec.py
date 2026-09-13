#!/usr/bin/env python3
"""Regenerates large-synthetic.json: a synthetic OpenAPI 3.0 spec with N
cross-referencing schemas, used to benchmark generation time at scale
(as opposed to the tiny petstore.yaml fixture, which mostly measures
process-startup overhead)."""

import json

N = 300
schemas = {}
for i in range(N):
    name = f"Model{i}"
    props = {
        "id": {"type": "integer"},
        "name": {"type": "string"},
        "active": {"type": "boolean"},
        "tags": {"type": "array", "items": {"type": "string"}},
        "status": {"type": "string", "enum": ["active", "inactive", "pending"]},
    }
    if i > 0:
        props["related"] = {"$ref": f"#/components/schemas/Model{i - 1}"}
        props["children"] = {
            "type": "array",
            "items": {"$ref": f"#/components/schemas/Model{(i + 1) % N}"},
        }
    schemas[name] = {
        "type": "object",
        "properties": props,
        "required": ["id", "name"],
    }

spec = {
    "openapi": "3.0.3",
    "info": {"title": "Synthetic large spec", "version": "1.0.0"},
    "components": {"schemas": schemas},
}

with open("large-synthetic.json", "w") as f:
    json.dump(spec, f)

print(f"{N} schemas written to large-synthetic.json")
