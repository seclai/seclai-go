package main

import (
	"encoding/json"
	"testing"
)

func TestFix_DowngradesOpenAPIVersion(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "x", "version": "0"},
	}
	fix(doc)

	if got, _ := doc["openapi"].(string); got != "3.0.3" {
		t.Fatalf("expected openapi 3.0.3, got %q", got)
	}
}

func TestFix_TransformsNullableAnyOf(t *testing.T) {
	input := []byte(`{
		"openapi": "3.1.0",
		"info": {"title": "x", "version": "0"},
		"components": {
			"schemas": {
				"Example": {
					"description": "a nullable string",
					"anyOf": [
						{"type": "null"},
						{"type": "string", "minLength": 1}
					]
				}
			}
		}
	}`)

	var doc any
	if err := json.Unmarshal(input, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fix(doc)

	root := doc.(map[string]any)
	if root["openapi"].(string) != "3.0.3" {
		t.Fatalf("expected version downgrade")
	}

	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	ex := schemas["Example"].(map[string]any)

	if _, ok := ex["anyOf"]; ok {
		t.Fatalf("expected anyOf removed")
	}
	if got, _ := ex["type"].(string); got != "string" {
		t.Fatalf("expected type string, got %v", ex["type"])
	}
	if got, _ := ex["nullable"].(bool); !got {
		t.Fatalf("expected nullable true")
	}
	if got, _ := ex["minLength"].(float64); got != 1 {
		t.Fatalf("expected minLength preserved, got %v", ex["minLength"])
	}
	if got, _ := ex["description"].(string); got != "a nullable string" {
		t.Fatalf("expected description preserved, got %q", got)
	}
}

func TestFix_TransformsNullableOneOf(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "x", "version": "0"},
		"components": map[string]any{
			"schemas": map[string]any{
				"Example": map[string]any{
					"oneOf": []any{
						map[string]any{"type": "integer"},
						map[string]any{"type": "null"},
					},
				},
			},
		},
	}
	fix(doc)

	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	ex := schemas["Example"].(map[string]any)

	if _, ok := ex["oneOf"]; ok {
		t.Fatalf("expected oneOf removed")
	}
	if got, _ := ex["type"].(string); got != "integer" {
		t.Fatalf("expected type integer, got %v", ex["type"])
	}
	if got, _ := ex["nullable"].(bool); !got {
		t.Fatalf("expected nullable true")
	}
}

func TestFix_TransformsNullableTypeArray(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "x", "version": "0"},
		"components": map[string]any{
			"schemas": map[string]any{
				"Example": map[string]any{
					"type": []any{"string", "null"},
				},
			},
		},
	}
	fix(doc)

	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	ex := schemas["Example"].(map[string]any)

	if got, _ := ex["type"].(string); got != "string" {
		t.Fatalf("expected type string, got %v", ex["type"])
	}
	if got, _ := ex["nullable"].(bool); !got {
		t.Fatalf("expected nullable true")
	}
}

func TestFix_TransformsExclusiveBounds(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "x", "version": "0"},
		"components": map[string]any{
			"schemas": map[string]any{
				"Example": map[string]any{
					"type":             "number",
					"exclusiveMinimum": float64(0),
					"exclusiveMaximum": float64(100),
				},
			},
		},
	}
	fix(doc)

	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	ex := schemas["Example"].(map[string]any)

	// exclusiveMinimum (number) → minimum + exclusiveMinimum (bool)
	if got, ok := ex["minimum"].(float64); !ok || got != 0 {
		t.Fatalf("expected minimum 0, got %v", ex["minimum"])
	}
	if got, ok := ex["exclusiveMinimum"].(bool); !ok || !got {
		t.Fatalf("expected exclusiveMinimum true, got %v", ex["exclusiveMinimum"])
	}

	// exclusiveMaximum (number) → maximum + exclusiveMaximum (bool)
	if got, ok := ex["maximum"].(float64); !ok || got != 100 {
		t.Fatalf("expected maximum 100, got %v", ex["maximum"])
	}
	if got, ok := ex["exclusiveMaximum"].(bool); !ok || !got {
		t.Fatalf("expected exclusiveMaximum true, got %v", ex["exclusiveMaximum"])
	}
}

func TestFix_LeavesExclusiveBoundBoolAlone(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "x", "version": "0"},
		"components": map[string]any{
			"schemas": map[string]any{
				"Example": map[string]any{
					"type":             "integer",
					"minimum":          float64(0),
					"exclusiveMinimum": true,
				},
			},
		},
	}
	fix(doc)

	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	ex := schemas["Example"].(map[string]any)

	// Already boolean — should be unchanged
	if got, ok := ex["exclusiveMinimum"].(bool); !ok || !got {
		t.Fatalf("expected exclusiveMinimum to remain true, got %v", ex["exclusiveMinimum"])
	}
	if got, ok := ex["minimum"].(float64); !ok || got != 0 {
		t.Fatalf("expected minimum to remain 0, got %v", ex["minimum"])
	}
}

func TestOmitUnsetRequestFields_MarksOptionalNullableRequestProperties(t *testing.T) {
	input := []byte(`{
		"openapi": "3.1.0",
		"paths": {
			"/x": {"put": {"requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Req"}}}}}}
		},
		"components": {"schemas": {
			"Req": {
				"required": ["must"],
				"properties": {
					"must": {"anyOf": [{"type": "string"}, {"type": "null"}]},
					"maybe": {"anyOf": [{"type": "string"}, {"type": "null"}]},
					"plain": {"type": "string"},
					"nested": {"items": {"$ref": "#/components/schemas/Inner"}, "type": "array"}
				}
			},
			"Inner": {"properties": {"opt": {"anyOf": [{"type": "integer"}, {"type": "null"}]}}},
			"Resp": {"properties": {"opt": {"anyOf": [{"type": "integer"}, {"type": "null"}]}}}
		}}
	}`)
	var doc any
	if err := json.Unmarshal(input, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fix(doc)
	omitUnsetRequestFields(doc)

	schemas := doc.(map[string]any)["components"].(map[string]any)["schemas"].(map[string]any)
	prop := func(schema, name string) map[string]any {
		return schemas[schema].(map[string]any)["properties"].(map[string]any)[name].(map[string]any)
	}
	if prop("Req", "maybe")["x-omitempty"] != true {
		t.Fatalf("optional nullable request property not marked")
	}
	if _, ok := prop("Req", "must")["x-omitempty"]; ok {
		t.Fatalf("required property must keep its null")
	}
	if _, ok := prop("Req", "plain")["x-omitempty"]; ok {
		t.Fatalf("non-nullable property needs no mark")
	}
	if prop("Inner", "opt")["x-omitempty"] != true {
		t.Fatalf("nested request schema not marked")
	}
	if _, ok := prop("Resp", "opt")["x-omitempty"]; ok {
		t.Fatalf("response-only schema must be left alone")
	}
}
