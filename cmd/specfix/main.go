package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var inPath string
	var outPath string
	flag.StringVar(&inPath, "in", "", "Input OpenAPI JSON path")
	flag.StringVar(&outPath, "out", "", "Output OpenAPI JSON path")
	flag.Parse()

	if inPath == "" || outPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: specfix -in <openapi.json> -out <out.json>")
		os.Exit(2)
	}

	raw, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "specfix: read: %v\n", err)
		os.Exit(1)
	}

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "specfix: parse json: %v\n", err)
		os.Exit(1)
	}

	fix(doc)

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "specfix: mkdir: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "specfix: marshal: %v\n", err)
		os.Exit(1)
	}
	out = append(out, '\n')

	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "specfix: write: %v\n", err)
		os.Exit(1)
	}
}

func fix(node any) {
	switch v := node.(type) {
	case map[string]any:
		// Downgrade the OpenAPI version string.
		if s, ok := v["openapi"].(string); ok {
			// e.g. "3.1.0" -> "3.0.3" for better generator compatibility.
			if len(s) >= 3 && s[:3] == "3.1" {
				v["openapi"] = "3.0.3"
			}
		}

		// Normalize nullable patterns.
		if transformNullableAnyOf(v, "anyOf") {
			// continue walking the transformed node
		}
		if transformNullableAnyOf(v, "oneOf") {
			// continue walking the transformed node
		}
		if transformNullableTypeArray(v) {
			// continue walking the transformed node
		}

		for _, child := range v {
			fix(child)
		}
	case []any:
		for _, child := range v {
			fix(child)
		}
	}
}

func transformNullableAnyOf(obj map[string]any, key string) bool {
	arr, ok := obj[key].([]any)
	if !ok || len(arr) != 2 {
		return false
	}

	nullIdx := -1
	nonNullIdx := -1
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if isNullSchema(m) {
			nullIdx = i
			continue
		}
		nonNullIdx = i
	}
	if nullIdx == -1 || nonNullIdx == -1 {
		return false
	}

	nonNull, ok := arr[nonNullIdx].(map[string]any)
	if !ok {
		return false
	}

	// Preserve outer keys like description/title and merge into the non-null schema.
	merged := map[string]any{}
	for k, v := range obj {
		if k == key {
			continue
		}
		merged[k] = v
	}
	for k, v := range nonNull {
		merged[k] = v
	}
	merged["nullable"] = true

	for k := range obj {
		delete(obj, k)
	}
	for k, v := range merged {
		obj[k] = v
	}
	return true
}

func transformNullableTypeArray(obj map[string]any) bool {
	t, ok := obj["type"].([]any)
	if !ok || len(t) != 2 {
		return false
	}
	nullFound := false
	other := ""
	for _, item := range t {
		s, ok := item.(string)
		if !ok {
			return false
		}
		if s == "null" {
			nullFound = true
		} else {
			other = s
		}
	}
	if !nullFound || other == "" {
		return false
	}
	obj["type"] = other
	obj["nullable"] = true
	return true
}

func isNullSchema(schema map[string]any) bool {
	if t, ok := schema["type"].(string); ok && t == "null" {
		return true
	}
	if t, ok := schema["type"].([]any); ok {
		for _, item := range t {
			if s, ok := item.(string); ok && s == "null" {
				return true
			}
		}
	}
	return false
}
