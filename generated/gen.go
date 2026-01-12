package generated

// This package contains code generated from the OpenAPI spec.
//
// Regenerate with:
//
// 	go generate ./...
//
// The spec lives at openapi/seclai.openapi.json.

// oapi-codegen does not fully support OpenAPI 3.1 yet, and Seclai's spec uses 3.1-style nullability.
// We preprocess the spec into a 3.0-compatible form (nullable: true) before generating.
//
//go:generate go run ../cmd/specfix -in ../openapi/seclai.openapi.json -out ../build/openapi-3.0.json
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -generate types,client -package generated -o seclai.gen.go ../build/openapi-3.0.json
