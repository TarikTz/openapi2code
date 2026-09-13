// examples.js — a small built-in gallery of example specs, copied
// verbatim from this repo's own test fixtures (pkg/engine/testdata/) so
// a first-time visitor can see results without needing their own spec.
// The Swagger 2.0 example omits its source fixture's `paths` section:
// this engine only ever reads `definitions`/`components.schemas`, so
// paths add nothing to what the playground demonstrates.
export const EXAMPLES = [
  {
    id: "petstore",
    label: "Petstore (OpenAPI 3.0)",
    description: "Nested objects, arrays, $ref, and enums",
    spec: `openapi: "3.0.3"
info:
  title: Petstore-style fixture
  version: "1.0.0"
components:
  schemas:
    Category:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
      required:
        - name
    Tag:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
      required:
        - name
    Pet:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
        category:
          $ref: "#/components/schemas/Category"
        tags:
          type: array
          items:
            $ref: "#/components/schemas/Tag"
        photoUrls:
          type: array
          items:
            type: string
        status:
          type: string
          enum:
            - available
            - pending
            - sold
      required:
        - name
        - photoUrls
    Error:
      type: object
      properties:
        code:
          type: integer
        message:
          type: string
      required:
        - code
        - message
`,
  },
  {
    id: "swagger2",
    label: "Petstore (Swagger 2.0)",
    description: "Same models, older Swagger 2.0 syntax",
    spec: `swagger: "2.0"
info:
  title: Petstore-style fixture (Swagger 2.0)
  version: "1.0.0"
definitions:
  Category:
    type: object
    properties:
      id:
        type: integer
      name:
        type: string
    required:
      - name
  Tag:
    type: object
    properties:
      id:
        type: integer
      name:
        type: string
    required:
      - name
  Pet:
    type: object
    properties:
      id:
        type: integer
      name:
        type: string
      nickname:
        type: string
        x-nullable: true
      category:
        $ref: "#/definitions/Category"
      tags:
        type: array
        items:
          $ref: "#/definitions/Tag"
      photoUrls:
        type: array
        items:
          type: string
      status:
        type: string
        enum:
          - available
          - pending
          - sold
    required:
      - name
      - photoUrls
      - nickname
  Error:
    type: object
    properties:
      code:
        type: integer
      message:
        type: string
    required:
      - code
      - message
`,
  },
  {
    id: "circular",
    label: "Circular references",
    description: "Self-referencing and mutually-referencing types",
    spec: `openapi: "3.0.3"
info:
  title: Circular fixture
  version: "1.0.0"
components:
  schemas:
    TreeNode:
      type: object
      properties:
        value:
          type: string
        children:
          type: array
          items:
            $ref: "#/components/schemas/TreeNode"
      required:
        - value
    A:
      type: object
      properties:
        b:
          $ref: "#/components/schemas/B"
    B:
      type: object
      properties:
        a:
          $ref: "#/components/schemas/A"
`,
  },
  {
    id: "composition",
    label: "allOf / oneOf / anyOf",
    description: "Schema composition: merged, union, and either-of types",
    spec: `openapi: "3.0.3"
info:
  title: Composition fixture
  version: "1.0.0"
components:
  schemas:
    Animal:
      type: object
      properties:
        name:
          type: string
      required:
        - name
    Dog:
      allOf:
        - $ref: "#/components/schemas/Animal"
        - type: object
          properties:
            breed:
              type: string
          required:
            - breed
    StringOrNumber:
      oneOf:
        - type: string
        - type: number
    Pet:
      anyOf:
        - $ref: "#/components/schemas/Dog"
        - $ref: "#/components/schemas/Animal"
`,
  },
];
