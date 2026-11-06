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
	if err := retainRemovedRequestFields(doc); err != nil {
		fmt.Fprintf(os.Stderr, "specfix: %v\n", err)
		os.Exit(1)
	}
	omitUnsetRequestFields(doc)

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

		// In OpenAPI 3.1, exclusiveMinimum/exclusiveMaximum are numbers.
		// In 3.0, they must be booleans combined with minimum/maximum.
		transformExclusiveBound(v, "exclusiveMinimum", "minimum")
		transformExclusiveBound(v, "exclusiveMaximum", "maximum")

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

// transformExclusiveBound converts OpenAPI 3.1 numeric exclusiveMinimum/exclusiveMaximum
// into the OpenAPI 3.0 boolean form with a companion minimum/maximum field.
func transformExclusiveBound(obj map[string]any, exclusiveKey, boundKey string) {
	val, ok := obj[exclusiveKey]
	if !ok {
		return
	}
	num, ok := val.(float64)
	if !ok {
		return // already a bool (3.0 style) — leave as-is
	}
	obj[boundKey] = num
	obj[exclusiveKey] = true
}

// omitUnsetRequestFields marks every optional, nullable property of a JSON
// request-body schema with x-omitempty, so that oapi-codegen tags the field
// `json:",omitempty"`.
//
// oapi-codegen leaves omitempty off nullable properties so that a nil pointer
// can be sent as null. The API, though, reads an explicit null differently
// from an omitted field in places — a null retention_days on UpdateSource
// clears the retention window — so a request built from a struct must leave
// out what the caller did not set. The few places that do need a literal null
// send it explicitly from the client. Required properties keep the generator's
// default, and schemas reached only from responses are left alone. A schema
// shared by a request and a response, such as AcceptedAttachmentSlot, is
// tagged too, so re-marshalling a fetched response drops its nil fields
// instead of writing null; the API reads both the same for those fields, so
// passing such a value back in a request is unaffected.
//
// Call it after fix, which turns the 3.1 null unions into nullable: true.
func omitUnsetRequestFields(doc any) {
	root, ok := doc.(map[string]any)
	if !ok {
		return
	}
	var schemas map[string]any
	if comps, ok := root["components"].(map[string]any); ok {
		schemas, _ = comps["schemas"].(map[string]any)
	}
	visited := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				const prefix = "#/components/schemas/"
				if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
					name := ref[len(prefix):]
					if !visited[name] {
						visited[name] = true
						if s, ok := schemas[name]; ok {
							walk(s)
						}
					}
				}
			}
			if props, ok := v["properties"].(map[string]any); ok {
				required := map[string]bool{}
				if req, ok := v["required"].([]any); ok {
					for _, r := range req {
						if name, ok := r.(string); ok {
							required[name] = true
						}
					}
				}
				for name, p := range props {
					pm, ok := p.(map[string]any)
					if !ok {
						continue
					}
					if nullable, _ := pm["nullable"].(bool); nullable && !required[name] {
						if _, set := pm["x-omitempty"]; !set {
							pm["x-omitempty"] = true
						}
					}
					walk(pm)
				}
			}
			for _, key := range []string{"items", "additionalProperties"} {
				if child, ok := v[key]; ok {
					walk(child)
				}
			}
			for _, key := range []string{"allOf", "anyOf", "oneOf"} {
				if arr, ok := v[key].([]any); ok {
					for _, child := range arr {
						walk(child)
					}
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}

	paths, _ := root["paths"].(map[string]any)
	for _, item := range paths {
		ops, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, op := range ops {
			opm, ok := op.(map[string]any)
			if !ok {
				continue
			}
			rb, ok := opm["requestBody"].(map[string]any)
			if !ok {
				continue
			}
			content, _ := rb["content"].(map[string]any)
			if media, ok := content["application/json"].(map[string]any); ok {
				walk(media["schema"])
			}
		}
	}
}

// retainedRequestField is a request-body property the API no longer declares
// but a released SDK version exposed. Go has no way to deprecate a struct field
// other than keeping it, and the request types in package seclai are aliases of
// the generated structs, so dropping a property from the spec would remove the
// field and break every caller that sets it.
type retainedRequestField struct {
	schema, property string
	def              map[string]any
}

// retainedRequestFields lists the removed properties to keep generating. Each
// is marked deprecated, so oapi-codegen writes a "Deprecated:" comment that
// editors and staticcheck surface. Remove an entry at the next major version,
// or when the spec declares the property again: specfix then fails on the
// stale entry rather than let it hide the spec's own definition.
var retainedRequestFields = []retainedRequestField{
	{
		// Released in v1.9.0; the API ignores it since version 2026-11-04.
		schema:   "SetSlackTriggerConfigApiRequest",
		property: "input",
		def: map[string]any{
			"type":                "string",
			"nullable":            true,
			"deprecated":          true,
			"x-deprecated-reason": "Ignored by the API since 2026-11-04; set Alias to choose the name people type after /seclai.",
		},
	},
}

// retainRemovedRequestFields adds each of retainedRequestFields back to its
// schema. It returns an error, and changes nothing, when the spec already
// declares one of them: the API added the property back, so the entry is stale
// and must be removed. Call it after fix and before omitUnsetRequestFields, so
// a nullable property is tagged omitempty.
func retainRemovedRequestFields(doc any) error {
	root, _ := doc.(map[string]any)
	comps, _ := root["components"].(map[string]any)
	schemas, _ := comps["schemas"].(map[string]any)
	for _, f := range retainedRequestFields {
		schema, ok := schemas[f.schema].(map[string]any)
		if !ok {
			continue
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			continue
		}
		if _, exists := props[f.property]; exists {
			return fmt.Errorf("stale entry `%s.%s` in the retained-fields list: the spec declares it again; remove the entry", f.schema, f.property)
		}
		def := make(map[string]any, len(f.def))
		for k, v := range f.def {
			def[k] = v
		}
		props[f.property] = def
	}
	return nil
}
