package seclai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// errNotAList marks a 2xx body that is neither shape of a list response.
var errNotAList = errors.New("the response is not a list")

// flatPagingFields are the legacy top-level counters the canonical envelope
// carries under "pagination" instead.
var flatPagingFields = []string{"total", "page", "limit"}

// jsonKind names the JSON value raw holds, for error messages.
func jsonKind(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "an empty body"
	}
	if !json.Valid(trimmed) {
		return "a body that is not JSON"
	}
	switch trimmed[0] {
	case '{':
		return "an object without a list"
	case '[':
		return "an array"
	case '"':
		return "a string"
	case 'n':
		return "null"
	default:
		return "a scalar"
	}
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func isJSONArray(raw json.RawMessage) bool {
	var items []json.RawMessage
	return bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) && json.Unmarshal(raw, &items) == nil
}

// normalizeList reads either shape a version-gated list endpoint serves and
// returns the items array plus an object carrying them where both shapes'
// types look: under key (the endpoint's legacy key, "" for a bare array) and
// under "data", with total/page/limit filled from "pagination" when absent.
func normalizeList(raw []byte, key string) (json.RawMessage, map[string]json.RawMessage, error) {
	empty := json.RawMessage("[]")
	object := map[string]json.RawMessage{}
	var items json.RawMessage

	if isJSONArray(raw) {
		items = bytes.TrimSpace(raw)
	} else {
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, nil, fmt.Errorf("%w: %w", errNotAList, err)
		}
		if object == nil {
			return nil, nil, errNotAList
		}
		// A real list wins over an explicit null, so a body carrying both keys
		// is never read as empty while one of them holds the items.
		data, hasData := object["data"]
		switch {
		case isJSONArray(data):
			items = data
		case key != "" && isJSONArray(object[key]):
			items = object[key]
		case hasData && isJSONNull(data):
			items = empty
		default:
			return nil, nil, errNotAList
		}
		if hasData {
			object["data"] = items
		}
	}
	if key != "" {
		object[key] = items
	}

	var pagination map[string]json.RawMessage
	if json.Unmarshal(object["pagination"], &pagination) == nil {
		for _, field := range flatPagingFields {
			if _, set := object[field]; !set && pagination[field] != nil {
				object[field] = pagination[field]
			}
		}
	}
	return items, object, nil
}

// decodeList decodes either shape of a version-gated list response into out.
//
// With key "" the endpoint's default shape is a bare array and out is a pointer
// to a slice. Otherwise key is the default shape's list key and out a pointer
// to the struct declaring it.
func decodeList(raw []byte, key string, out any) error {
	items, object, err := normalizeList(raw, key)
	if err != nil {
		return err
	}
	if key == "" {
		return json.Unmarshal(items, out)
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return err
	}
	return json.Unmarshal(normalized, out)
}

// listError wraps a decodeList failure so a caller never sees a raw json error
// or mistakes a non-list body for an empty list.
func listError(err error, method, where string, raw []byte) error {
	out := &UnexpectedResponseError{Method: method, URL: where, ResponseText: string(bytes.TrimSpace(raw))}
	switch {
	case err == errNotAList:
		out.Message = "expected a list, got " + jsonKind(raw)
	case errors.Is(err, errNotAList):
		out.Message, out.cause = "expected a list, got "+jsonKind(raw), err
	default:
		out.Message, out.cause = "the list could not be decoded: "+err.Error(), err
	}
	return out
}

// doList issues a request to a version-gated list endpoint and decodes either
// response shape into out; see decodeList for key and out.
func (c *Client) doList(ctx context.Context, method, apiPath string, query map[string]string, body any, key string, out any) error {
	raw, reqURL, err := c.doBytes(ctx, method, apiPath, queryValues(query), body, nil)
	if err != nil {
		return err
	}
	return decodeListBody(method, reqURL, raw, key, out)
}

// decodeListBody decodes the 2xx body doBytes returned for reqURL. This is the
// only place a response becomes an *UnexpectedResponseError: an error from
// doBytes itself never reaches it.
func decodeListBody(method string, reqURL *url.URL, body []byte, key string, out any) error {
	if err := decodeList(body, key, out); err != nil {
		return listError(err, method, reqURL.String(), body)
	}
	return nil
}

// rawJSON is what a json.RawMessage method returns for a doBytes result: the
// error unchanged, nil for an empty body, and the JSON error for a body that
// is not JSON.
func rawJSON(body []byte, err error) (json.RawMessage, error) {
	if err != nil || len(body) == 0 {
		return nil, err
	}
	var out json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
