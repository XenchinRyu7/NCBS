package openapi

import _ "embed"

// SpecYAML holds the embedded OpenAPI 3.1 YAML specification so it is always bundled inside the Vercel Serverless binary.
//
//go:embed openapi.yaml
var SpecYAML []byte

// PostmanJSON holds the embedded Postman Collection v2.1 JSON pre-configured for the /trial/v1 Sandbox.
//
//go:embed postman.json
var PostmanJSON []byte

