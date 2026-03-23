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
		if r.URL.Path != "/sources/" {
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
		if r.URL.Path != "/sources/" {
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
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.DeleteAgentRun(context.Background(), "run_1"); err != nil {
		t.Fatalf("DeleteAgentRun: %v", err)
	}
}

func TestClient_CancelAgentRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agents/runs/run_1/cancel" {
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
		if got := r.URL.Query().Get("query"); got != "hello" {
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
