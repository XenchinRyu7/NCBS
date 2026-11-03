package openapi

import _ "embed"

// SpecYAML holds the embedded OpenAPI 3.1 YAML specification so it is always bundled inside the Vercel Serverless binary.
//
//go:embed openapi.yaml
var SpecYAML []byte
