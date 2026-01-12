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
