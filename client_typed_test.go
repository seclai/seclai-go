package seclai

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seclai/seclai-go/generated"
)

func TestGeneratedClient_ListSources_SetsAuthAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sources/" {
			w.WriteHeader(404)
			return
		}
		if got := r.Header.Get("x-api-key"); got != "k" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"data": [{
				"account_id": "00000000-0000-0000-0000-000000000000",
				"content_filter": "",
				"created_at": "2026-01-11T00:00:00Z",
				"id": "src_1",
				"name": "Source",
				"source_type": "custom",
				"updated_at": "2026-01-11T00:00:00Z"
			}],
			"pagination": {"has_next": false, "has_prev": false, "limit": 20, "page": 1, "pages": 1, "total": 1}
		}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	page := 1
	limit := 20
	resp, err := c.Generated().ListSourcesApiSourcesGetWithResponse(context.Background(), &generated.ListSourcesApiSourcesGetParams{Page: &page, Limit: &limit})
	if err != nil {
		t.Fatalf("ListSources...WithResponse: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode())
	}
	if resp.JSON200 == nil {
		t.Fatalf("expected JSON200")
	}
	if got := len(resp.JSON200.Data); got != 1 {
		t.Fatalf("expected 1 data item, got %d", got)
	}
	if got := resp.JSON200.Data[0].Id; got != "src_1" {
		t.Fatalf("expected id src_1, got %q", got)
	}
	if got := resp.JSON200.Pagination.Total; got != 1 {
		t.Fatalf("expected total 1, got %d", got)
	}
}

func TestGeneratedClient_ListSources_ValidationError422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = io.WriteString(w, `{
			"detail": [{"loc": ["query", "page"], "msg": "bad", "type": "value_error"}]
		}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	page := 0
	resp, err := c.Generated().ListSourcesApiSourcesGetWithResponse(context.Background(), &generated.ListSourcesApiSourcesGetParams{Page: &page})
	if err != nil {
		t.Fatalf("ListSources...WithResponse: %v", err)
	}
	if resp.StatusCode() != 422 {
		t.Fatalf("expected 422, got %d", resp.StatusCode())
	}
	if resp.JSON422 == nil || resp.JSON422.Detail == nil {
		t.Fatalf("expected JSON422 detail")
	}
	if got := len(*resp.JSON422.Detail); got != 1 {
		t.Fatalf("expected 1 validation error, got %d", got)
	}
	if got := (*resp.JSON422.Detail)[0].Msg; got != "bad" {
		t.Fatalf("expected msg=bad, got %q", got)
	}
}

func TestClient_RunAgent_Typed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/agents/") {
			w.WriteHeader(404)
			return
		}
		var got AgentRunRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			w.WriteHeader(400)
			return
		}
		// Ensure we can accept metadata as an arbitrary map.
		if got.Metadata == nil {
			w.WriteHeader(400)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"attempts": [],
			"error_count": 0,
			"priority": false,
			"run_id": "run_1",
			"status": "pending"
		}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	meta := map[string]JsonValue{"k": "v"}
	res, err := c.RunAgent(context.Background(), "agent_1", AgentRunRequest{Metadata: &meta})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res == nil {
		t.Fatalf("expected response")
	}
	if res.RunId != "run_1" {
		t.Fatalf("expected run_id run_1, got %q", res.RunId)
	}
}

func TestClient_UploadFileToSource_Multipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/sources/") || !(strings.HasSuffix(r.URL.Path, "/upload") || strings.HasSuffix(r.URL.Path, "/upload/")) {
			w.WriteHeader(404)
			return
		}

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			w.WriteHeader(400)
			return
		}
		if mediaType != "multipart/form-data" {
			w.WriteHeader(400)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		foundFile := false
		foundTitle := false
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				w.WriteHeader(400)
				return
			}
			name := part.FormName()
			if name == "title" {
				b, _ := io.ReadAll(part)
				if strings.TrimSpace(string(b)) == "My Title" {
					foundTitle = true
				}
			}
			if name == "file" {
				if part.FileName() != "a.txt" {
					w.WriteHeader(400)
					return
				}
				b, _ := io.ReadAll(part)
				if string(b) != "hello" {
					w.WriteHeader(400)
					return
				}
				foundFile = true
			}
		}
		if !foundFile || !foundTitle {
			w.WriteHeader(400)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"filename":"a.txt","status":"pending"}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := c.UploadFileToSource(context.Background(), "sc_1", UploadFileRequest{File: []byte("hello"), FileName: "a.txt", Title: "My Title"})
	if err != nil {
		t.Fatalf("UploadFileToSource: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
	if resp.Filename != "a.txt" {
		t.Fatalf("expected filename a.txt, got %q", resp.Filename)
	}
}
