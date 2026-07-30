package seclai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/seclai/seclai-go/generated"
)

func TestClient_RunStreamingAgentAndWait_Done(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		if r.URL.Path != "/agents/agent_1/runs/stream" {
			w.WriteHeader(404)
			return
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "text/event-stream") {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)

		_, _ = io.WriteString(w, ": keepalive\n\n")
		if fl != nil {
			fl.Flush()
		}
		_, _ = io.WriteString(w, "event: init\n")
		_, _ = io.WriteString(w, "data: {\"attempts\":[],\"error_count\":0,\"priority\":false,\"run_id\":\"run_1\",\"status\":\"processing\"}\n\n")
		if fl != nil {
			fl.Flush()
		}
		_, _ = io.WriteString(w, "event: done\n")
		_, _ = io.WriteString(w, "data: {\"attempts\":[],\"error_count\":0,\"priority\":false,\"run_id\":\"run_1\",\"status\":\"completed\",\"output\":\"ok\"}\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	meta := map[string]JsonValue{"k": "v"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := c.RunStreamingAgentAndWait(ctx, "agent_1", AgentRunStreamRequest{Input: nil, Metadata: &meta})
	if err != nil {
		t.Fatalf("RunStreamingAgentAndWait: %v", err)
	}
	if res == nil {
		t.Fatalf("expected response")
	}
	if res.RunId != "run_1" {
		t.Fatalf("expected run_id run_1, got %q", res.RunId)
	}
	if res.Output == nil || *res.Output != "ok" {
		t.Fatalf("expected output ok, got %#v", res.Output)
	}
}

func TestClient_RunStreamingAgentAndWait_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agents/agent_1/runs/stream" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "event: init\n")
		_, _ = io.WriteString(w, "data: {}\n\n")
		if fl != nil {
			fl.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = c.RunStreamingAgentAndWait(ctx, "agent_1", AgentRunStreamRequest{Input: nil, Metadata: &map[string]JsonValue{}})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %T %v", err, err)
	}
}

func TestGeneratedClient_ListSources_SetsAuthAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sources" {
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
		if !strings.HasPrefix(r.URL.Path, "/agents/") {
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

func TestClient_GetAgentRunWithOptions_IncludeStepOutputs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		if r.URL.Path != "/agents/runs/run_1" {
			w.WriteHeader(404)
			return
		}
		if got := r.URL.Query().Get("include_step_outputs"); got != "true" {
			w.WriteHeader(400)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"attempts": [],
			"error_count": 0,
			"priority": false,
			"run_id": "run_1",
			"status": "completed",
			"steps": []
		}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	res, err := c.GetAgentRun(context.Background(), "run_1", &GetAgentRunOptions{IncludeStepOutputs: true})
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
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
		if !strings.HasPrefix(r.URL.Path, "/sources/") || !strings.HasSuffix(r.URL.Path, "/upload") {
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
		foundMetadata := false
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
			if name == "metadata" {
				b, _ := io.ReadAll(part)
				var m map[string]any
				if json.Unmarshal(b, &m) == nil {
					if m["category"] == "docs" && m["author"] == "Ada" {
						foundMetadata = true
					}
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
		if !foundFile || !foundTitle || !foundMetadata {
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

	resp, err := c.UploadFileToSource(context.Background(), "sc_1", UploadFileRequest{File: []byte("hello"), FileName: "a.txt", Title: "My Title", Metadata: map[string]any{"category": "docs", "author": "Ada"}})
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

func TestClient_UploadFileToContent_Multipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/contents/") || !strings.HasSuffix(r.URL.Path, "/upload") {
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
		foundMetadata := false
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
			if name == "metadata" {
				b, _ := io.ReadAll(part)
				var m map[string]any
				if json.Unmarshal(b, &m) == nil {
					if m["revision"] == float64(2) {
						foundMetadata = true
					}
				}
			}
			if name == "file" {
				if part.FileName() != "updated.pdf" {
					w.WriteHeader(400)
					return
				}
				b, _ := io.ReadAll(part)
				if len(b) != 3 {
					w.WriteHeader(400)
					return
				}
				foundFile = true
			}
		}
		if !foundFile || !foundMetadata {
			w.WriteHeader(400)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"filename":"updated.pdf","status":"pending"}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := c.UploadFileToContent(context.Background(), "cv_1", UploadFileRequest{File: []byte{1, 2, 3}, FileName: "updated.pdf", MimeType: "application/pdf", Metadata: map[string]any{"revision": 2}})
	if err != nil {
		t.Fatalf("UploadFileToContent: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
	if resp.Filename != "updated.pdf" {
		t.Fatalf("expected filename updated.pdf, got %q", resp.Filename)
	}
}

func TestClient_ListSources_PathMatchesSpec(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sources" {
			w.WriteHeader(404)
			return
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

	resp, err := c.ListSources(context.Background(), ListSourcesOptions{SortableListOptions: SortableListOptions{ListOptions: ListOptions{Page: 1, Limit: 20}}})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response")
	}
}

// ── Agent CRUD tests ────────────────────────────────────────────────────────

func TestClient_ListAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"pages":1,"total":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ListAgents(context.Background(), ListOptions{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_CreateAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents" {
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(400)
			return
		}
		if body["name"] != "test-agent" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a_1","name":"test-agent"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.CreateAgent(context.Background(), CreateAgentRequest{Name: "test-agent"})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if resp.Id != "a_1" {
		t.Fatalf("expected id a_1, got %q", resp.Id)
	}
}

func TestClient_GetAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/a_1" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a_1","name":"test-agent"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.GetAgent(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if resp.Id != "a_1" {
		t.Fatalf("expected id a_1, got %q", resp.Id)
	}
}

func TestClient_DeleteAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/agents/a_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeleteAgent(context.Background(), "a_1"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
}

// ── Agent Export tests ──────────────────────────────────────────────────────

func TestClient_ExportAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/a_1/export" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("download") != "true" {
			t.Errorf("expected download=true, got %q", r.URL.Query().Get("download"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"export_version":"2","exported_at":"2026-01-01T00:00:00Z","software_version":"1.0.0","agent":{"name":"test"}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ExportAgent(context.Background(), "a_1", true)
	if err != nil {
		t.Fatalf("ExportAgent: %v", err)
	}
	if resp.ExportVersion != "2" {
		t.Fatalf("expected export_version 2, got %q", resp.ExportVersion)
	}
}

func TestClient_ExportAgent_DownloadFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("download") != "false" {
			t.Errorf("expected download=false, got %q", r.URL.Query().Get("download"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"export_version":"2","exported_at":"2026-01-01T00:00:00Z","software_version":"1.0.0","agent":{"name":"test"}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	_, err := c.ExportAgent(context.Background(), "a_1", false)
	if err != nil {
		t.Fatalf("ExportAgent download=false: %v", err)
	}
}

func TestClient_PreviewImportAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/preview-import" {
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(400)
			return
		}
		if _, ok := body["agent_definition"].(map[string]any); !ok {
			t.Errorf("expected agent_definition object in body, got %T", body["agent_definition"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"agent_name":"n","description":null,"step_count":0,"schedules":0,"alert_configs":0,"evaluation_criteria":0,"governance_policies":0}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.PreviewImportAgent(context.Background(), AgentImportPreviewRequest{
		AgentDefinition: map[string]any{"agent": map[string]any{"name": "n"}},
	})
	if err != nil {
		t.Fatalf("PreviewImportAgent: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("expected ok=true")
	}
	if resp.AgentName == nil || *resp.AgentName != "n" {
		t.Fatalf("expected agent_name n")
	}
}

// TestClient_PreviewImportAgent_422 verifies that the import-specific 422 body
// (AgentDefinitionImportErrorResponse, which has no `detail` field) is delivered
// intact via APIValidationError.ResponseText, and that ValidationError is left
// nil because the body is not an HTTPValidationError.
func TestClient_PreviewImportAgent_422(t *testing.T) {
	const errBody = `{"error":"invalid_agent_definition","message":"missing required field","errors":[{"line":3,"column":5,"path":"agent.name","message":"required"}],"source":"{\n  \"agent\": {}\n}"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = io.WriteString(w, errBody)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	_, err := c.PreviewImportAgent(context.Background(), AgentImportPreviewRequest{
		AgentDefinition: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error on 422")
	}
	var verr *APIValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *APIValidationError, got %T: %v", err, err)
	}
	if verr.ValidationError != nil {
		t.Errorf("ValidationError should be nil for non-HTTPValidationError 422 body, got %+v", verr.ValidationError)
	}
	if verr.ResponseText != errBody {
		t.Errorf("ResponseText mismatch:\nwant: %s\ngot:  %s", errBody, verr.ResponseText)
	}
	// Caller decodes the import-error shape themselves.
	var imp AgentDefinitionImportErrorResponse
	if err := json.Unmarshal([]byte(verr.ResponseText), &imp); err != nil {
		t.Fatalf("unmarshal AgentDefinitionImportErrorResponse: %v", err)
	}
	if imp.Message != "missing required field" {
		t.Errorf("unexpected message: %q", imp.Message)
	}
	if len(imp.Errors) != 1 || imp.Errors[0].Line != 3 || imp.Errors[0].Column != 5 {
		t.Errorf("unexpected errors: %+v", imp.Errors)
	}
}

// ── Agent Definition tests ──────────────────────────────────────────────────

func TestClient_GetAgentDefinition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/a_1/definition" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"change_id":"c_1","definition":{},"schema_version":"1"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.GetAgentDefinition(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("GetAgentDefinition: %v", err)
	}
	if resp.ChangeId != "c_1" {
		t.Fatalf("expected change_id c_1, got %q", resp.ChangeId)
	}
}

// ── Agent Run tests ─────────────────────────────────────────────────────────

func TestClient_GetAgentRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/runs/run_1" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"completed"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.GetAgentRun(context.Background(), "run_1", nil)
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if resp.RunId != "run_1" {
		t.Fatalf("expected run_1, got %q", resp.RunId)
	}
}

func TestClient_DeleteAgentRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/agents/runs/run_1" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"failed"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	//nolint:staticcheck // deprecated alias kept for compatibility; still covered
	if err := c.DeleteAgentRun(context.Background(), "run_1"); err != nil {
		t.Fatalf("DeleteAgentRun: %v", err)
	}
}

func TestClient_CancelAgentRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cancellation is DELETE on the run resource. This test asserted
		// POST /agents/runs/{id}/cancel, a path the API has never had, so it
		// confirmed a 404-ing method rather than catching it.
		if r.Method != http.MethodDelete || r.URL.Path != "/agents/runs/run_1" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"failed"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.CancelAgentRun(context.Background(), "run_1")
	if err != nil {
		t.Fatalf("CancelAgentRun: %v", err)
	}
	if resp.RunId != "run_1" {
		t.Fatalf("expected run_1, got %q", resp.RunId)
	}
}

func TestClient_SearchAgentRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/runs/search" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"matches":[],"total":0}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.SearchAgentRuns(context.Background(), AgentTraceSearchRequest{})
	if err != nil {
		t.Fatalf("SearchAgentRuns: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

// ── Knowledge Base tests ────────────────────────────────────────────────────

func TestClient_ListKnowledgeBases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/knowledge_bases" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"pages":1,"total":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ListKnowledgeBases(context.Background(), SortableListOptions{})
	if err != nil {
		t.Fatalf("ListKnowledgeBases: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_CreateKnowledgeBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/knowledge_bases" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"kb_1","name":"My KB"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.CreateKnowledgeBase(context.Background(), CreateKnowledgeBaseBody{Name: "My KB"})
	if err != nil {
		t.Fatalf("CreateKnowledgeBase: %v", err)
	}
	if resp.Id != "kb_1" {
		t.Fatalf("expected kb_1, got %q", resp.Id)
	}
}

func TestClient_DeleteKnowledgeBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/knowledge_bases/kb_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeleteKnowledgeBase(context.Background(), "kb_1"); err != nil {
		t.Fatalf("DeleteKnowledgeBase: %v", err)
	}
}

// ── Memory Bank tests ───────────────────────────────────────────────────────

func TestClient_ListMemoryBanks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/memory_banks" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"pages":1,"total":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ListMemoryBanks(context.Background(), SortableListOptions{})
	if err != nil {
		t.Fatalf("ListMemoryBanks: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_CompactMemoryBank(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/memory_banks/mb_1/compact" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.CompactMemoryBank(context.Background(), "mb_1"); err != nil {
		t.Fatalf("CompactMemoryBank: %v", err)
	}
}

// ── Source CRUD tests ───────────────────────────────────────────────────────

func TestClient_CreateSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sources" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"src_1","name":"My Source"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.CreateSource(context.Background(), CreateSourceBody{Name: "My Source"})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if resp.Id != "src_1" {
		t.Fatalf("expected src_1, got %q", resp.Id)
	}
}

func TestClient_GetSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/sources/src_1" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"src_1","name":"My Source"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.GetSource(context.Background(), "src_1")
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if resp.Id != "src_1" {
		t.Fatalf("expected src_1, got %q", resp.Id)
	}
}

func TestClient_DeleteSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/sources/src_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeleteSource(context.Background(), "src_1"); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
}

// ── Source Export tests ──────────────────────────────────────────────────────

func TestClient_ListSourceExports(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/sources/src_1/exports" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"pages":1,"total":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ListSourceExports(context.Background(), "src_1", ListOptions{})
	if err != nil {
		t.Fatalf("ListSourceExports: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

// ── Content tests ───────────────────────────────────────────────────────────

func TestClient_ReplaceContentWithInlineText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/contents/cv_1" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content_version_id":"cv_1","filename":"test.txt"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ReplaceContentWithInlineText(context.Background(), "cv_1", InlineTextReplaceRequest{})
	if err != nil {
		t.Fatalf("ReplaceContentWithInlineText: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_ListContentEmbeddings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/contents/cv_1/embeddings" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"pages":1,"total":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ListContentEmbeddings(context.Background(), "cv_1", ListOptions{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("ListContentEmbeddings: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

// ── Solution tests ──────────────────────────────────────────────────────────

func TestClient_ListSolutions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/solutions" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"pages":1,"total":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ListSolutions(context.Background(), SortableListOptions{})
	if err != nil {
		t.Fatalf("ListSolutions: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_DeleteSolution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/solutions/sol_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeleteSolution(context.Background(), "sol_1"); err != nil {
		t.Fatalf("DeleteSolution: %v", err)
	}
}

// ── Alerts tests ────────────────────────────────────────────────────────────

func TestClient_ListAlerts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/alerts" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"total":0}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.ListAlerts(context.Background(), ListAlertsOptions{})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_DeleteAlertConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/alerts/configs/cfg_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeleteAlertConfig(context.Background(), "cfg_1"); err != nil {
		t.Fatalf("DeleteAlertConfig: %v", err)
	}
}

// ── Models tests ────────────────────────────────────────────────────────────

func TestClient_MarkAllModelAlertsRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/models/alerts/mark-all-read" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.MarkAllModelAlertsRead(context.Background()); err != nil {
		t.Fatalf("MarkAllModelAlertsRead: %v", err)
	}
}

// ── Search test ─────────────────────────────────────────────────────────────

func TestClient_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/search" {
			w.WriteHeader(404)
			return
		}
		if got := r.URL.Query().Get("q"); got != "hello" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.Search(context.Background(), SearchOptions{Query: "hello"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

// ── RunStreamingAgent (channel-based) tests ─────────────────────────────────

func TestClient_RunStreamingAgent_Done(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/agent_1/runs/stream" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "event: init\n")
		_, _ = io.WriteString(w, "data: {\"attempts\":[],\"error_count\":0,\"priority\":false,\"run_id\":\"run_1\",\"status\":\"processing\"}\n\n")
		if fl != nil {
			fl.Flush()
		}
		_, _ = io.WriteString(w, "event: done\n")
		_, _ = io.WriteString(w, "data: {\"attempts\":[],\"error_count\":0,\"priority\":false,\"run_id\":\"run_1\",\"status\":\"completed\",\"output\":\"ok\"}\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, errCh := c.RunStreamingAgent(ctx, "agent_1", AgentRunStreamRequest{})
	var events []AgentRunEvent
	for evt := range ch {
		events = append(events, evt)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Event != "init" {
		t.Fatalf("expected init event, got %q", events[0].Event)
	}
	if events[1].Event != "done" {
		t.Fatalf("expected done event, got %q", events[1].Event)
	}
	if events[1].Run == nil || events[1].Run.RunId != "run_1" {
		t.Fatal("expected done event to have run with id run_1")
	}
}

func TestClient_RunStreamingAgent_JSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"completed"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, errCh := c.RunStreamingAgent(ctx, "agent_1", AgentRunStreamRequest{})
	var events []AgentRunEvent
	for evt := range ch {
		events = append(events, evt)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != "done" {
		t.Fatalf("expected done event, got %q", events[0].Event)
	}
	if events[0].Run == nil || events[0].Run.RunId != "run_1" {
		t.Fatal("expected done event to have run")
	}
}

func TestClient_RunStreamingAgent_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `internal error`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, errCh := c.RunStreamingAgent(ctx, "agent_1", AgentRunStreamRequest{})
	for range ch {
		// drain
	}
	err := <-errCh
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIStatusError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIStatusError, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", apiErr.StatusCode)
	}
}

// ── RunAgentAndPoll tests ───────────────────────────────────────────────────

func TestClient_RunAgentAndPoll_CompletesImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"completed","output":"done"}`)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.RunAgentAndPoll(context.Background(), "a_1", AgentRunRequest{}, nil)
	if err != nil {
		t.Fatalf("RunAgentAndPoll: %v", err)
	}
	if resp.RunId != "run_1" {
		t.Fatalf("expected run_1, got %q", resp.RunId)
	}
	if resp.Output == nil || *resp.Output != "done" {
		t.Fatalf("expected output 'done'")
	}
}

func TestClient_RunAgentAndPoll_PollsUntilComplete(t *testing.T) {
	pollCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs") {
			_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"pending"}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/agents/runs/run_1" {
			pollCount++
			if pollCount >= 2 {
				_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"completed","output":"polled"}`)
			} else {
				_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"processing"}`)
			}
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.RunAgentAndPoll(ctx, "a_1", AgentRunRequest{}, &RunAgentAndPollOptions{PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("RunAgentAndPoll: %v", err)
	}
	if resp.RunId != "run_1" {
		t.Fatalf("expected run_1, got %q", resp.RunId)
	}
	if resp.Output == nil || *resp.Output != "polled" {
		t.Fatal("expected output 'polled'")
	}
	if pollCount < 2 {
		t.Fatalf("expected at least 2 polls, got %d", pollCount)
	}
}

// ── Top-Level AI Assistant tests ────────────────────────────────────────────

func TestClient_AcceptAiAssistantPlan(t *testing.T) {
	convID := "00000000-0000-0000-0000-000000000001"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ai-assistant/"+convID+"/accept" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"conversation_id":"`+convID+`","executed_actions":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.AcceptAiAssistantPlan(context.Background(), convID, AiAssistantAcceptRequest{})
	if err != nil {
		t.Fatalf("AcceptAiAssistantPlan: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_DeclineAiAssistantPlan(t *testing.T) {
	convID := "00000000-0000-0000-0000-000000000001"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ai-assistant/"+convID+"/decline" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeclineAiAssistantPlan(context.Background(), convID); err != nil {
		t.Fatalf("DeclineAiAssistantPlan: %v", err)
	}
}

// ── Governance AI tests ─────────────────────────────────────────────────────

func TestClient_AcceptGovernanceAiPlan(t *testing.T) {
	convID := "00000000-0000-0000-0000-000000000002"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/governance/ai-assistant/"+convID+"/accept" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"conversation_id":"`+convID+`","executed_actions":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.AcceptGovernanceAiPlan(context.Background(), convID)
	if err != nil {
		t.Fatalf("AcceptGovernanceAiPlan: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

// ── Source Embedding Migration tests ────────────────────────────────────────

func TestClient_GetSourceEmbeddingMigration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/sources/src_1/embedding-migration" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"completed"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.GetSourceEmbeddingMigration(context.Background(), "src_1")
	if err != nil {
		t.Fatalf("GetSourceEmbeddingMigration: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestClient_DefaultHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test-value" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `missing X-Custom header`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{
		APIKey:         "k",
		BaseURL:        srv.URL,
		DefaultHeaders: map[string]string{"X-Custom": "test-value"},
	})
	_, err := c.ListAgents(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("expected default headers to be sent: %v", err)
	}
}

func TestStreamingError(t *testing.T) {
	var err error = &StreamingError{Message: "stream ended early", RunID: "run_123"}
	se, ok := err.(*StreamingError)
	if !ok {
		t.Fatal("expected *StreamingError")
	}
	if se.RunID != "run_123" {
		t.Fatalf("expected RunID run_123, got %s", se.RunID)
	}
	if !strings.Contains(se.Error(), "run_123") {
		t.Fatalf("expected error to contain RunID, got %s", se.Error())
	}

	// Without RunID
	err2 := &StreamingError{Message: "timeout"}
	if strings.Contains(err2.Error(), "run") {
		t.Fatal("expected no run mention without RunID")
	}
}

// ── Agent attachment / experiment sync tests ────────────────────────────────

func TestClient_GetAgentAttachmentReferences(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/a_1/attachment-references" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"requires_uploads":true,"agent":{"exact_names":["report.pdf"]}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.GetAgentAttachmentReferences(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("GetAgentAttachmentReferences: %v", err)
	}
	if !resp.RequiresUploads {
		t.Fatalf("expected requires_uploads true")
	}
}

func TestClient_DownloadAgentRunAttachment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/agent-runs/run_1/attachments/att_1" {
			w.WriteHeader(404)
			return
		}
		if got := r.URL.Query().Get("download_name"); got != "report.pdf" {
			t.Errorf("expected download_name report.pdf, got %q", got)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("file-bytes"))
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := c.DownloadAgentRunAttachment(context.Background(), "run_1", "att_1", "report.pdf")
	if err != nil {
		t.Fatalf("DownloadAgentRunAttachment: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "file-bytes" {
		t.Fatalf("expected file-bytes, got %q", string(body))
	}
}

func TestClient_DeleteExperiment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/models/playground/experiments/exp_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeleteExperiment(context.Background(), "exp_1"); err != nil {
		t.Fatalf("DeleteExperiment: %v", err)
	}
}

// ── New in this sync: identity, agent pause, email governance, email domains,
// generation tiers, docs search ─────────────────────────────────────────────

func TestClient_GetMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/me" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"account_id":"3f1a0d6e-0000-4000-8000-00000000000a","organizations":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if got.AccountId.String() == "" {
		t.Fatal("expected account id")
	}
}

func TestClient_DisableAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/a_1/disable" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a_1","name":"x","disabled":true}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.DisableAgent(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}
	if got == nil {
		t.Fatal("expected response")
	}
}

func TestClient_EnableAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/a_1/enable" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a_1","name":"x","disabled":false}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.EnableAgent(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("EnableAgent: %v", err)
	}
	if got == nil {
		t.Fatal("expected response")
	}
}

func TestClient_GetAgentCallers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/a_1/callers" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"3f1a0d6e-0000-4000-8000-000000000001","name":"Caller","disabled":false}]`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.GetAgentCallers(context.Background(), "a_1")
	if err != nil {
		t.Fatalf("GetAgentCallers: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Caller" {
		t.Fatalf("unexpected callers: %+v", got)
	}
}

func TestClient_SetEmailTriggerConfig(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/agents/a_1/triggers/t_1/email-config" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"trigger_id":"3f1a0d6e-0000-4000-8000-000000000002","agent_id":"3f1a0d6e-0000-4000-8000-000000000003","trigger_type":"EMAIL_RECEIVED","email_addresses":["support@agent.seclai.com"]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.SetEmailTriggerConfig(context.Background(), "a_1", "t_1", RoutersApiAgentsSetEmailTriggerConfigRequest{})
	if err != nil {
		t.Fatalf("SetEmailTriggerConfig: %v", err)
	}
	if got.EmailAddresses == nil || len(*got.EmailAddresses) != 1 {
		t.Fatalf("unexpected addresses: %+v", got.EmailAddresses)
	}
	// A zero-value request must be a no-op. The API treats an explicit null as
	// "clear this field", so sending nulls here would silently wipe the alias,
	// the sender allowlist and the inbound-handling flags.
	if len(body) != 0 {
		t.Fatalf("zero-value request must send an empty object, got %v", body)
	}
}

func TestClient_SetEmailTriggerConfig_SendsOnlySetFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/agents/a_1/triggers/t_1/email-config" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"trigger_id":"3f1a0d6e-0000-4000-8000-000000000002","agent_id":"3f1a0d6e-0000-4000-8000-000000000003","trigger_type":"EMAIL_RECEIVED"}`)
	}))
	t.Cleanup(srv.Close)

	alias := "support"
	requireAuth := false
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if _, err := c.SetEmailTriggerConfig(context.Background(), "a_1", "t_1",
		RoutersApiAgentsSetEmailTriggerConfigRequest{Alias: &alias, RequireSenderAuth: &requireAuth}); err != nil {
		t.Fatalf("SetEmailTriggerConfig: %v", err)
	}
	if len(body) != 2 || body["alias"] != "support" || body["require_sender_auth"] != false {
		t.Fatalf("expected only the two set fields, got %v", body)
	}
}

func TestClient_SetEmailTriggerConfig_ClearsWithZeroValue(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/agents/a_1/triggers/t_1/email-config" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"trigger_id":"3f1a0d6e-0000-4000-8000-000000000002","agent_id":"3f1a0d6e-0000-4000-8000-000000000003","trigger_type":"EMAIL_RECEIVED"}`)
	}))
	t.Cleanup(srv.Close)

	// Clearing is still possible: a pointer to the zero value is sent explicitly.
	empty := ""
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if _, err := c.SetEmailTriggerConfig(context.Background(), "a_1", "t_1",
		RoutersApiAgentsSetEmailTriggerConfigRequest{Alias: &empty}); err != nil {
		t.Fatalf("SetEmailTriggerConfig: %v", err)
	}
	if len(body) != 1 || body["alias"] != "" {
		t.Fatalf("expected alias cleared, got %v", body)
	}
}

func TestClient_ListAgentEmailOptOuts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/agent-email-optouts" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("agent_id") != "a_1" || r.URL.Query().Get("limit") != "25" || r.URL.Query().Get("offset") != "50" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[],"total":0}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ListAgentEmailOptOuts(context.Background(), AgentEmailOptOutOptions{AgentID: "a_1", Limit: 25, Offset: 50})
	if err != nil {
		t.Fatalf("ListAgentEmailOptOuts: %v", err)
	}
	if got.Total != 0 {
		t.Fatalf("unexpected total: %d", got.Total)
	}
}

func TestClient_RemoveAgentEmailOptOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/agents/agent-email-optouts/oo_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.RemoveAgentEmailOptOut(context.Background(), "oo_1"); err != nil {
		t.Fatalf("RemoveAgentEmailOptOut: %v", err)
	}
}

func TestClient_ListBlockedEmailSenders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/blocked-email-senders" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("limit") != "10" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[],"total":0,"auto_block_mode":"disabled"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ListBlockedEmailSenders(context.Background(), BlockedEmailSenderOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListBlockedEmailSenders: %v", err)
	}
	if got.AutoBlockMode != "disabled" {
		t.Fatalf("unexpected mode: %s", got.AutoBlockMode)
	}
}

func TestClient_BlockEmailSender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/blocked-email-senders" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"3f1a0d6e-0000-4000-8000-000000000004","created_at":"2026-07-01","sender_email":"spam@example.com","match_type":"address","source":"manual"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.BlockEmailSender(context.Background(), BlockEmailSenderRequest{SenderEmail: "spam@example.com"})
	if err != nil {
		t.Fatalf("BlockEmailSender: %v", err)
	}
	if got.SenderEmail != "spam@example.com" {
		t.Fatalf("unexpected sender: %s", got.SenderEmail)
	}
}

func TestClient_UnblockEmailSender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/agents/blocked-email-senders/b_1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.UnblockEmailSender(context.Background(), "b_1"); err != nil {
		t.Fatalf("UnblockEmailSender: %v", err)
	}
}

func TestClient_SetAutoBlockMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/agents/blocked-email-senders/mode" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[],"total":0,"auto_block_mode":"input_and_output"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.SetAutoBlockMode(context.Background(), SetAutoBlockModeRequest{Mode: "input_and_output"})
	if err != nil {
		t.Fatalf("SetAutoBlockMode: %v", err)
	}
	if got.AutoBlockMode != "input_and_output" {
		t.Fatalf("unexpected mode: %s", got.AutoBlockMode)
	}
}

func TestClient_ListInboundEmailRejections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/inbound-email-rejections" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("agent_id") != "a_1" || r.URL.Query().Get("limit") != "5" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"3f1a0d6e-0000-4000-8000-000000000005","created_at":"2026-07-01","recipient":"x@y","sender":"s@y","reason":"unauthorized_sender"}]`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ListInboundEmailRejections(context.Background(), InboundEmailRejectionOptions{AgentID: "a_1", Limit: 5})
	if err != nil {
		t.Fatalf("ListInboundEmailRejections: %v", err)
	}
	if len(got) != 1 || got[0].Reason != "unauthorized_sender" {
		t.Fatalf("unexpected rejections: %+v", got)
	}
}

func TestClient_GetInboundEmailStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/inbound-email-status" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"paused":true,"queued_backlog":42}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.GetInboundEmailStatus(context.Background())
	if err != nil {
		t.Fatalf("GetInboundEmailStatus: %v", err)
	}
	if !got.Paused || got.QueuedBacklog != 42 {
		t.Fatalf("unexpected status: %+v", got)
	}
}

func TestClient_CancelQueuedEmailRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/inbound-email-status/cancel-queued" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cancelled":7}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.CancelQueuedEmailRuns(context.Background())
	if err != nil {
		t.Fatalf("CancelQueuedEmailRuns: %v", err)
	}
	if got.Cancelled != 7 {
		t.Fatalf("unexpected count: %d", got.Cancelled)
	}
}

func TestClient_ResumeInboundEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/inbound-email-status/resume" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"resumed":true}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ResumeInboundEmail(context.Background())
	if err != nil {
		t.Fatalf("ResumeInboundEmail: %v", err)
	}
	if !got.Resumed {
		t.Fatal("expected resumed")
	}
}

func TestClient_ListEmailDomains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/email-domains" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"domains":[],"can_add_vanity":true}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ListEmailDomains(context.Background())
	if err != nil {
		t.Fatalf("ListEmailDomains: %v", err)
	}
	if got == nil {
		t.Fatal("expected response")
	}
}

func TestClient_AddEmailDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/email-domains" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"3f1a0d6e-0000-4000-8000-000000000006","domain":"acme.seclai.com","kind":"vanity","status":"pending","is_primary":false}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.AddEmailDomain(context.Background(), AddEmailDomainRequest{Kind: "vanity", Value: "acme"})
	if err != nil {
		t.Fatalf("AddEmailDomain: %v", err)
	}
	if got.Kind != "vanity" {
		t.Fatalf("unexpected kind: %s", got.Kind)
	}
}

func TestClient_RemoveEmailDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/email-domains/d_1" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"removed":true,"cleanup_note":"Delete the NS record"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.RemoveEmailDomain(context.Background(), "d_1")
	if err != nil {
		t.Fatalf("RemoveEmailDomain: %v", err)
	}
	if got.CleanupNote == nil || *got.CleanupNote == "" {
		t.Fatal("expected cleanup note")
	}
}

func TestClient_VerifyEmailDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/email-domains/d_1/verify" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"3f1a0d6e-0000-4000-8000-000000000007","domain":"a.b","kind":"custom","status":"verified","is_primary":false}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.VerifyEmailDomain(context.Background(), "d_1")
	if err != nil {
		t.Fatalf("VerifyEmailDomain: %v", err)
	}
	if got.Status != "verified" {
		t.Fatalf("unexpected status: %s", got.Status)
	}
}

func TestClient_SetPrimaryEmailDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/email-domains/d_1/primary" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"3f1a0d6e-0000-4000-8000-000000000008","domain":"a.b","kind":"custom","status":"verified","is_primary":true}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.SetPrimaryEmailDomain(context.Background(), "d_1")
	if err != nil {
		t.Fatalf("SetPrimaryEmailDomain: %v", err)
	}
	if !got.IsPrimary {
		t.Fatal("expected primary")
	}
}

func TestClient_UseSharedEmailDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/email-domains/use-shared-domain" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.UseSharedEmailDomain(context.Background()); err != nil {
		t.Fatalf("UseSharedEmailDomain: %v", err)
	}
}

func TestClient_SendEmailDomainTestEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/email-domains/d_1/test-email" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sent":true}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.SendEmailDomainTestEmail(context.Background(), "d_1")
	if err != nil {
		t.Fatalf("SendEmailDomainTestEmail: %v", err)
	}
	if got.Sent == nil || !*got.Sent {
		t.Fatal("expected sent")
	}
}

func TestClient_GetDmarcSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/email-domains/d_1/dmarc" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("days") != "7" || r.URL.Query().Get("top_sources") != "3" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"window_days":7,"report_count":2,"total_messages":100,"passed_messages":99,"failed_messages":1}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.GetDmarcSummary(context.Background(), "d_1", DmarcOptions{Days: 7, TopSources: 3})
	if err != nil {
		t.Fatalf("GetDmarcSummary: %v", err)
	}
	if got.WindowDays != 7 {
		t.Fatalf("unexpected window: %d", got.WindowDays)
	}
}

func TestClient_GetGenerationTiers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models/generation-tiers" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"image":{"fast":{}}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.GetGenerationTiers(context.Background())
	if err != nil {
		t.Fatalf("GetGenerationTiers: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected payload")
	}
}

func TestClient_SearchDocs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/docs-search" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("q") != "email triggers" || r.URL.Query().Get("mode") != "semantic" || r.URL.Query().Get("limit") != "3" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.SearchDocs(context.Background(), DocsSearchOptions{Query: "email triggers", Mode: "semantic", Limit: 3})
	if err != nil {
		t.Fatalf("SearchDocs: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected payload")
	}
}

func TestClient_Search_RequiresQuery(t *testing.T) {
	// `q` is required by the spec. Silently omitting it produced a 422 whose
	// message named the wire parameter rather than the field the caller set.
	c, _ := NewClient(Options{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if _, err := c.Search(context.Background(), SearchOptions{}); err == nil {
		t.Fatal("expected an error when Query is empty")
	}
	if _, err := c.SearchDocs(context.Background(), DocsSearchOptions{}); err == nil {
		t.Fatal("expected an error when Query is empty")
	}
}

func TestClient_ListEvaluationCriteria_ReturnsPaginatedEnvelope(t *testing.T) {
	// The endpoint returned a bare array until 2026-07. Nothing covered this
	// method, so the switch to a paginated envelope broke it silently: the
	// client kept decoding into []EvaluationCriteriaResponse and got nothing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/a_1/evaluation-criteria" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("limit") != "25" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"3f1a0d6e-0000-4000-8000-000000000009"}],`+
			`"pagination":{"page":2,"limit":25,"total":7,"pages":1,"has_next":false,"has_prev":true}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ListEvaluationCriteriaPage(context.Background(), "a_1", ListOptions{Page: 2, Limit: 25})
	if err != nil {
		t.Fatalf("ListEvaluationCriteriaPage: %v", err)
	}
	if len(got.Data) != 1 || got.Data[0].Id.String() != "3f1a0d6e-0000-4000-8000-000000000009" {
		t.Fatalf("unexpected data: %+v", got.Data)
	}
	if got.Pagination == nil {
		t.Fatal("expected the canonical envelope to carry pagination")
	}
	if got.Pagination.Total != 7 || got.Pagination.Page != 2 || got.Pagination.Limit != 25 {
		t.Fatalf("unexpected pagination: %+v", *got.Pagination)
	}
}

func TestClient_ListEvaluationCriteria_AcceptsABareArray(t *testing.T) {
	// The envelope is merged on main but not deployed, so both shapes are live
	// realities. Decoding only one breaks the client the day the other ships.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/a_1/evaluation-criteria" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"3f1a0d6e-0000-4000-8000-000000000009"}]`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ListEvaluationCriteria(context.Background(), "a_1", ListOptions{})
	if err != nil {
		t.Fatalf("ListEvaluationCriteria: %v", err)
	}
	if len(got) != 1 || got[0].Id.String() != "3f1a0d6e-0000-4000-8000-000000000009" {
		t.Fatalf("unexpected data: %+v", got)
	}
}

func TestClient_APIVersionHeader_OmittedUnlessOptedIn(t *testing.T) {
	// The point of the option: upgrading the SDK must not silently move an
	// account onto a newer API version and change response shapes.
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Seclai-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"total":0,"pages":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if _, err := c.ListAgents(context.Background(), ListOptions{}); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if seen != "" {
		t.Fatalf("expected no Seclai-Version header, got %q", seen)
	}
}

func TestClient_APIVersionHeader_SentWhenSet(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Seclai-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"total":0,"pages":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL, APIVersion: "2026-07-27"})
	if _, err := c.ListAgents(context.Background(), ListOptions{}); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if seen != "2026-07-27" {
		t.Fatalf("expected the version header, got %q", seen)
	}
}

func TestClient_GetAPIVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/version" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"pinned_version":null,"effective_version":"2026-01-01",`+
			`"default_version":"2026-01-01","latest_version":"2026-07-27",`+
			`"known_versions":["2026-01-01","2026-07-27"]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.GetAPIVersion(context.Background())
	if err != nil {
		t.Fatalf("GetAPIVersion: %v", err)
	}
	if got.LatestVersion != "2026-07-27" || len(got.KnownVersions) != 2 {
		t.Fatalf("unexpected version state: %+v", *got)
	}
}

func TestClient_UpdateAPIVersion_SendsExplicitNullToClearThePin(t *testing.T) {
	// null is the documented way to clear the pin, so it must reach the wire
	// rather than being omitted as an unset field.
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/version" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"pinned_version":null,"effective_version":"2026-01-01",`+
			`"default_version":"2026-01-01","latest_version":"2026-07-27","known_versions":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if _, err := c.UpdateAPIVersion(context.Background(), nil); err != nil {
		t.Fatalf("UpdateAPIVersion: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected exactly the version field, got %v", body)
	}
	if v, ok := body["version"]; !ok || v != nil {
		t.Fatalf("expected an explicit null version, got %v", body)
	}
}

func TestClient_ListAlerts_DoesNotSendSeverity(t *testing.T) {
	// GET /alerts declares no severity filter: it never filtered anything, and
	// sending it is a 422 once Options.APIVersion is 2026-07-27 or later.
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if _, err := c.ListAlerts(context.Background(), ListAlertsOptions{Severity: "high"}); err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if query.Has("severity") {
		t.Fatalf("severity must not reach the wire, got %v", query)
	}
}

func TestClient_ListModelAlerts_TranslatesPageToOffset(t *testing.T) {
	// The endpoint declares limit/offset, not page, so page 2 used to return
	// page 1.
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if _, err := c.ListModelAlerts(context.Background(), ListOptions{Page: 3, Limit: 25}); err != nil {
		t.Fatalf("ListModelAlerts: %v", err)
	}
	if query.Get("offset") != "50" || query.Get("limit") != "25" || query.Has("page") {
		t.Fatalf("expected offset=50&limit=25 with no page, got %v", query)
	}
}

func TestClient_GetAgentAiConversationHistoryWithOptions_SendsStepType(t *testing.T) {
	// step_type is required by the API and the original signature could not
	// supply it, so every call answered 422.
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"turns":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if _, err := c.GetAgentAiConversationHistoryWithOptions(context.Background(), "a_1",
		AiConversationHistoryOptions{StepType: "llm", Limit: 5}); err != nil {
		t.Fatalf("GetAgentAiConversationHistoryWithOptions: %v", err)
	}
	if query.Get("step_type") != "llm" || query.Get("limit") != "5" {
		t.Fatalf("expected step_type and limit on the wire, got %v", query)
	}
}

func TestClient_ListRunEvaluationResults_ReadsCanonicalPagination(t *testing.T) {
	// The run-level endpoint is version-gated to {data, pagination}. Reading
	// only the flat total/page/limit reported 0 for every opted-in caller.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":2,"limit":25,"total":7,`+
			`"pages":1,"has_next":false,"has_prev":true}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL, APIVersion: "2026-07-27"})
	got, err := c.ListRunEvaluationResults(context.Background(), "a_1", "r_1", ListOptions{Page: 2, Limit: 25})
	if err != nil {
		t.Fatalf("ListRunEvaluationResults: %v", err)
	}
	if got.Pagination == nil || got.Pagination.Total != 7 {
		t.Fatalf("expected canonical pagination, got %+v", got)
	}
}

func TestClient_ListAgentEvaluationResults_StillReadsTheFlatShape(t *testing.T) {
	// The agent-level endpoint is not version-gated and stays flat, so the
	// shared type must keep serving both.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"total":7,"page":2,"limit":25}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.ListAgentEvaluationResults(context.Background(), "a_1", ListOptions{Page: 2, Limit: 25})
	if err != nil {
		t.Fatalf("ListAgentEvaluationResults: %v", err)
	}
	if got.Total != 7 || got.Pagination != nil {
		t.Fatalf("expected the flat shape, got %+v", got)
	}
}

func TestClient_ListAlerts_DecodesTypedEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"3f1a0d6e-0000-4000-8000-00000000000a","title":"Disk full"}],`+
			`"pagination":{"page":1,"limit":20,"total":1,"pages":1,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.Typed().ListAlerts(context.Background(), ListAlertsOptions{})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(got.Data) != 1 || got.Data[0].Title != "Disk full" {
		t.Fatalf("unexpected alerts: %+v", got.Data)
	}
	if got.Pagination.Total != 1 {
		t.Fatalf("unexpected pagination: %+v", got.Pagination)
	}
}

func TestClient_ListAlertConfigs_ReadsEitherTopLevelKey(t *testing.T) {
	// "configs" by default, "data" once opted in. Items papers over the flip.
	for _, tc := range []struct{ name, body string }{
		{"legacy", `{"configs":[{"id":"3f1a0d6e-0000-4000-8000-00000000000b"}],"total":1}`},
		{"canonical", `{"data":[{"id":"3f1a0d6e-0000-4000-8000-00000000000b"}],` +
			`"pagination":{"page":1,"limit":20,"total":1,"pages":1,"has_next":false,"has_prev":false}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
			got, err := c.Typed().ListAlertConfigs(context.Background(), ListOptions{})
			if err != nil {
				t.Fatalf("ListAlertConfigs: %v", err)
			}
			if len(got.Items()) != 1 {
				t.Fatalf("expected one config from either key, got %+v", got)
			}
		})
	}
}

func TestClient_ListModelAlerts_ReadsEitherTopLevelKey(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"legacy", `{"alerts":[{"id":"3f1a0d6e-0000-4000-8000-00000000000c"}],"total":1}`},
		{"canonical", `{"data":[{"id":"3f1a0d6e-0000-4000-8000-00000000000c"}],` +
			`"pagination":{"page":1,"limit":20,"total":1,"pages":1,"has_next":false,"has_prev":false}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
			got, err := c.Typed().ListModelAlerts(context.Background(), ListOptions{})
			if err != nil {
				t.Fatalf("ListModelAlerts: %v", err)
			}
			if len(got.Items()) != 1 {
				t.Fatalf("expected one alert from either key, got %+v", got)
			}
		})
	}
}

func TestTypedClient_IssuesTheSameRequestAsTheRawMethod(t *testing.T) {
	// The façade delegates rather than rebuilding the request, so the two
	// surfaces cannot drift. This pins that.
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	opts := SearchOptions{Query: "hello", Limit: 5}
	if _, err := c.Search(context.Background(), opts); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := c.Typed().Search(context.Background(), opts); err != nil {
		t.Fatalf("Typed().Search: %v", err)
	}
	if len(seen) != 2 || seen[0] != seen[1] {
		t.Fatalf("expected identical requests, got %v", seen)
	}
}

func TestTypedClient_SearchDecodesTheEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"entity_type":"agent","name":"Support"}]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := c.Typed().Search(context.Background(), SearchOptions{Query: "support"})
	if err != nil {
		t.Fatalf("Typed().Search: %v", err)
	}
	if len(got.Results) != 1 || got.Results[0].Name != "Support" {
		t.Fatalf("unexpected results: %+v", got.Results)
	}
}

func TestTypedClient_RawSurfaceIsUnchanged(t *testing.T) {
	// The point of the façade: the existing methods still hand back raw JSON,
	// so no call site had to change.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	var raw json.RawMessage
	raw, err := c.Search(context.Background(), SearchOptions{Query: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected a raw body")
	}
}

func TestAPIVersionConstants_TrackTheSpec(t *testing.T) {
	raw, err := os.ReadFile("openapi/seclai.openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Versions struct {
			Default string   `json:"default"`
			Latest  string   `json:"latest"`
			Known   []string `json:"known"`
		} `json:"x-seclai-versions"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if APIVersionDefault != spec.Versions.Default {
		t.Fatalf("default: constant %q, spec %q", APIVersionDefault, spec.Versions.Default)
	}
	if APIVersionLatest != spec.Versions.Latest {
		t.Fatalf("latest: constant %q, spec %q", APIVersionLatest, spec.Versions.Latest)
	}
	known := []string{APIVersion20260701, APIVersion20260727}
	if len(known) != len(spec.Versions.Known) {
		t.Fatalf("known versions: constants %v, spec %v", known, spec.Versions.Known)
	}
	for i, v := range spec.Versions.Known {
		if known[i] != v {
			t.Fatalf("known[%d]: constant %q, spec %q", i, known[i], v)
		}
	}
}

func TestAPIVersion_UnknownIsRejected(t *testing.T) {
	// A newer server version can reshape responses, and this client would
	// mis-decode them silently rather than error. Fail closed at construction.
	_, err := NewClient(Options{APIKey: "k", APIVersion: "2099-01-01"})
	if err == nil {
		t.Fatal("expected an unknown APIVersion to be rejected")
	}
	if !strings.Contains(err.Error(), "2099-01-01") ||
		!strings.Contains(err.Error(), "AllowUnknownAPIVersion") {
		t.Fatalf("error should name the version and the escape hatch, got: %v", err)
	}
}

func TestAPIVersion_UnknownIsAllowedWhenAsked(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Seclai-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"total":0,"pages":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{
		APIKey: "k", BaseURL: srv.URL,
		APIVersion: "2099-01-01", AllowUnknownAPIVersion: true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ListAgents(context.Background(), ListOptions{}); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if seen != "2099-01-01" {
		t.Fatalf("expected the unknown version on the wire, got %q", seen)
	}
}

func TestAPIVersion_KnownNeedsNoEscapeHatch(t *testing.T) {
	if _, err := NewClient(Options{APIKey: "k", APIVersion: APIVersionLatest}); err != nil {
		t.Fatalf("a known version must be accepted: %v", err)
	}
}

func TestNewClient_VersionGuardCannotBeBypassedViaDefaultHeaders(t *testing.T) {
	// DefaultHeaders is applied last so it wins, which means it can carry a
	// Seclai-Version. Validating only Options.APIVersion left the guard one
	// header away from being bypassed.
	_, err := NewClient(Options{
		APIKey:         "k",
		DefaultHeaders: map[string]string{"Seclai-Version": "2099-01-01"},
	})
	if err == nil {
		t.Fatal("expected an unknown version in DefaultHeaders to be rejected")
	}
	if !strings.Contains(err.Error(), "2099-01-01") || !strings.Contains(err.Error(), "DefaultHeaders") {
		t.Fatalf("error should name the version and its source, got: %v", err)
	}

	// Case-insensitively, since header names are.
	if _, err := NewClient(Options{
		APIKey:         "k",
		DefaultHeaders: map[string]string{"seclai-version": "2099-01-01"},
	}); err == nil {
		t.Fatal("expected a lowercase header key to be caught too")
	}

	// The escape hatch still covers the header form.
	if _, err := NewClient(Options{
		APIKey:                 "k",
		DefaultHeaders:         map[string]string{"Seclai-Version": "2099-01-01"},
		AllowUnknownAPIVersion: true,
	}); err != nil {
		t.Fatalf("escape hatch should permit it: %v", err)
	}
}

func TestNewClient_DefaultHeaderOverridesRatherThanDuplicating(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Values("Seclai-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"total":0,"pages":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{
		APIKey: "k", BaseURL: srv.URL,
		APIVersion:     APIVersion20260701,
		DefaultHeaders: map[string]string{"seclai-version": APIVersion20260727},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ListAgents(context.Background(), ListOptions{}); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(seen) != 1 || seen[0] != APIVersion20260727 {
		t.Fatalf("expected exactly the caller's value on the wire, got %v", seen)
	}
}

func TestClient_ConversationHistory_RequiresStepType(t *testing.T) {
	// Omitting it only drops the parameter and defers to a 422 naming the wire
	// parameter — the failure this method exists to avoid.
	c, _ := NewClient(Options{APIKey: "k"})
	if _, err := c.GetAgentAiConversationHistoryWithOptions(
		context.Background(), "a_1", AiConversationHistoryOptions{}); err == nil {
		t.Fatal("expected a missing StepType to be rejected")
	} else if !strings.Contains(err.Error(), "StepType") {
		t.Fatalf("error should name the field, got: %v", err)
	}
}

func TestItems_EmptyCanonicalPageIsNotMistakenForTheLegacyKey(t *testing.T) {
	// `{"data": []}` is a valid empty page. Deciding by len() rather than
	// presence fell through to the legacy key, so an empty canonical page
	// reported whatever the legacy field held — and the existing tests never
	// caught it because they all used non-empty lists.
	var cfg AlertConfigListResponse
	if err := json.Unmarshal([]byte(`{"data":[],"pagination":{"page":1,"limit":20,"total":0,"pages":0,"has_next":false,"has_prev":false}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Items() == nil || len(cfg.Items()) != 0 {
		t.Fatalf("expected the empty canonical page, got %+v", cfg.Items())
	}
	if cfg.Pagination == nil || cfg.Pagination.Limit != 20 {
		t.Fatalf("pagination should survive an empty page, got %+v", cfg.Pagination)
	}

	var alerts ModelAlertListResponse
	if err := json.Unmarshal([]byte(`{"data":[],"pagination":{"page":1,"limit":20,"total":0,"pages":0,"has_next":false,"has_prev":false}}`), &alerts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if alerts.Items() == nil || len(alerts.Items()) != 0 {
		t.Fatalf("expected the empty canonical page, got %+v", alerts.Items())
	}
}

func TestItems_LegacyKeyStillWinsWhenCanonicalIsAbsent(t *testing.T) {
	// Guard the over-correction: with no `data` key at all the legacy list must
	// still be returned.
	var cfg AlertConfigListResponse
	if err := json.Unmarshal([]byte(`{"configs":[{}],"total":1}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Items()) != 1 {
		t.Fatalf("expected the legacy list, got %+v", cfg.Items())
	}

	var alerts ModelAlertListResponse
	if err := json.Unmarshal([]byte(`{"alerts":[{}],"total":1}`), &alerts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(alerts.Items()) != 1 {
		t.Fatalf("expected the legacy list, got %+v", alerts.Items())
	}
}

func TestNewClient_DuplicateVersionHeaderSpellings(t *testing.T) {
	// DefaultHeaders may carry two spellings of one header. Go's map order is
	// randomised, so validating "the first match" could approve one value and
	// send the other — nondeterministically. Run it repeatedly: a version-
	// dependent guard would pass only some of the time.
	for i := 0; i < 50; i++ {
		_, err := NewClient(Options{
			APIKey: "k",
			DefaultHeaders: map[string]string{
				"Seclai-Version": APIVersion20260727,
				"seclai-version": "2099-01-01",
			},
		})
		if err == nil {
			t.Fatalf("iteration %d: an unknown spelling slipped past the guard", i)
		}
	}
}

func TestNewClient_OnlyOneVersionHeaderReachesTheWire(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Values("Seclai-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"page":1,"limit":20,"total":0,"pages":0,"has_next":false,"has_prev":false}}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{
		APIKey: "k", BaseURL: srv.URL,
		DefaultHeaders: map[string]string{
			"Seclai-Version": APIVersion20260701,
			"seclai-version": APIVersion20260727,
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ListAgents(context.Background(), ListOptions{}); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("expected exactly one Seclai-Version on the wire, got %v", seen)
	}
}
