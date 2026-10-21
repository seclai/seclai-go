package seclai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testPage = `"pagination":{"page":2,"limit":5,"pages":1,"total":1,"has_next":false,"has_prev":true}`

// requestRecorder captures the one request a test sends and answers with body.
type requestRecorder struct {
	method, path, rawQuery string
	body                   map[string]any
	rawBody                []byte
}

func newRecordingServer(t *testing.T, rec *requestRecorder, status int, contentType, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method, rec.path, rec.rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		rec.rawBody, _ = io.ReadAll(r.Body)
		if len(rec.rawBody) > 0 {
			_ = json.Unmarshal(rec.rawBody, &rec.body)
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func (rec *requestRecorder) expect(t *testing.T, method, path, rawQuery string) {
	t.Helper()
	if rec.method != method || rec.path != path || rec.rawQuery != rawQuery {
		t.Fatalf("got %s %s?%s, want %s %s?%s", rec.method, rec.path, rec.rawQuery, method, path, rawQuery)
	}
}

// ── Accepted attachments ────────────────────────────────────────────────────

func TestClient_SetAcceptedAttachments(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json",
		`{"agent_id":"3f1a0d6e-0000-4000-8000-000000000001","trigger_id":"3f1a0d6e-0000-4000-8000-000000000002",
		  "accepted_attachments":[{"name":"invoice","content_types":["application/pdf"],"min":1,"max":1,"label":null,"name_pattern":null}]}`)

	types := []string{"application/pdf"}
	one := 1
	slots := []AcceptedAttachmentSlot{{Name: "invoice", ContentTypes: &types, Min: &one, Max: &one}}
	got, err := c.SetAcceptedAttachments(context.Background(), "a_1", "t_1", SetAcceptedAttachmentsRequest{AcceptedAttachments: &slots})
	if err != nil {
		t.Fatalf("SetAcceptedAttachments: %v", err)
	}
	rec.expect(t, http.MethodPut, "/agents/a_1/triggers/t_1/accepted-attachments", "")
	sent, ok := rec.body["accepted_attachments"].([]any)
	if !ok || len(sent) != 1 {
		t.Fatalf("expected one slot in the body, got %s", rec.rawBody)
	}
	slot := sent[0].(map[string]any)
	if slot["name"] != "invoice" || slot["min"] != float64(1) || slot["max"] != float64(1) {
		t.Fatalf("unexpected slot %v", slot)
	}
	if len(got.AcceptedAttachments) != 1 || got.AcceptedAttachments[0].Name != "invoice" {
		t.Fatalf("unexpected response %+v", got)
	}
}

func TestClient_SetAcceptedAttachments_NilAcceptsAnyFiles(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json",
		`{"agent_id":"3f1a0d6e-0000-4000-8000-000000000001","trigger_id":"3f1a0d6e-0000-4000-8000-000000000002",
		  "accepted_attachments":[{"name":"files"}]}`)

	if _, err := c.SetAcceptedAttachments(context.Background(), "a_1", "t_1", SetAcceptedAttachmentsRequest{}); err != nil {
		t.Fatalf("SetAcceptedAttachments: %v", err)
	}
	// null is the documented "accept any files" declaration, so it must be sent, not omitted.
	if string(rec.rawBody) != `{"accepted_attachments":null}` {
		t.Fatalf("expected an explicit null, got %s", rec.rawBody)
	}
}

func TestClient_SetEmailTriggerConfig_SendsRunOnAttachmentMismatch(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json",
		`{"trigger_id":"3f1a0d6e-0000-4000-8000-000000000002","agent_id":"3f1a0d6e-0000-4000-8000-000000000003","trigger_type":"EMAIL_RECEIVED","email_run_on_attachment_mismatch":true}`)

	yes := true
	got, err := c.SetEmailTriggerConfig(context.Background(), "a_1", "t_1",
		RoutersApiAgentsSetEmailTriggerConfigRequest{RunOnAttachmentMismatch: &yes})
	if err != nil {
		t.Fatalf("SetEmailTriggerConfig: %v", err)
	}
	if len(rec.body) != 1 || rec.body["run_on_attachment_mismatch"] != true {
		t.Fatalf("expected only run_on_attachment_mismatch, got %v", rec.body)
	}
	if got.EmailRunOnAttachmentMismatch == nil || !*got.EmailRunOnAttachmentMismatch {
		t.Fatalf("expected email_run_on_attachment_mismatch decoded, got %+v", got)
	}
}

// ── Content attachments ─────────────────────────────────────────────────────

const testAttachment = `"id":"att_1","key":"k/report.pdf","content_type":"application/pdf","size_bytes":42,
	"indexed":true,"indexing_status":"completed","indexing_error":null,"metadata":{},
	"created_at":"2026-10-19T00:00:00Z","updated_at":"2026-10-19T00:00:00Z"`

func TestClient_ListContentAttachments(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{"data":[{`+testAttachment+`}],`+testPage+`}`)

	got, err := c.ListContentAttachments(context.Background(), "cv_1", ListOptions{Page: 2, Limit: 5})
	if err != nil {
		t.Fatalf("ListContentAttachments: %v", err)
	}
	rec.expect(t, http.MethodGet, "/contents/cv_1/attachments", "limit=5&page=2")
	if len(got.Data) != 1 || got.Data[0].Id != "att_1" || got.Data[0].SizeBytes != 42 || got.Pagination.Page != 2 {
		t.Fatalf("unexpected response %+v", got)
	}
}

func TestClient_GetContentAttachment(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json",
		`{`+testAttachment+`,"file_withheld":null,"text_content":"hello","text_content_start":10,"text_content_end":15,"text_content_total_length":15}`)

	got, err := c.GetContentAttachment(context.Background(), "cv_1", "att_1", 10, 20)
	if err != nil {
		t.Fatalf("GetContentAttachment: %v", err)
	}
	rec.expect(t, http.MethodGet, "/contents/cv_1/attachments/att_1", "end=20&start=10")
	if got.TextContent == nil || *got.TextContent != "hello" || got.TextContentStart != 10 {
		t.Fatalf("unexpected response %+v", got)
	}

	// Zero start and end leave the API's defaults in place.
	if _, err := c.GetContentAttachment(context.Background(), "cv_1", "att_1", 0, 0); err != nil {
		t.Fatalf("GetContentAttachment: %v", err)
	}
	rec.expect(t, http.MethodGet, "/contents/cv_1/attachments/att_1", "")
}

func TestClient_ListContentAttachmentEmbeddings(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{"data":[],`+testPage+`}`)

	got, err := c.ListContentAttachmentEmbeddings(context.Background(), "cv_1", "att_1", ListOptions{Page: 2, Limit: 5})
	if err != nil {
		t.Fatalf("ListContentAttachmentEmbeddings: %v", err)
	}
	rec.expect(t, http.MethodGet, "/contents/cv_1/attachments/att_1/embeddings", "limit=5&page=2")
	if got.Pagination.Page != 2 {
		t.Fatalf("unexpected response %+v", got)
	}
}

func TestClient_DownloadContentAttachment(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/pdf", "%PDF-bytes")

	resp, err := c.DownloadContentAttachment(context.Background(), "cv_1", "att_1")
	if err != nil {
		t.Fatalf("DownloadContentAttachment: %v", err)
	}
	defer resp.Body.Close()
	rec.expect(t, http.MethodGet, "/contents/cv_1/attachments/att_1/file", "")
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "%PDF-bytes" || resp.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("unexpected download %q (%s)", body, resp.Header.Get("Content-Type"))
	}
}

func TestClient_DownloadContentAttachment_WithheldIsAnError(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 403, "application/json", `{"detail":"pending_screening"}`)

	resp, err := c.DownloadContentAttachment(context.Background(), "cv_1", "att_1")
	if resp != nil {
		t.Fatal("expected no response on 403")
	}
	var apiErr *APIStatusError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 403 {
		t.Fatalf("expected a 403 APIStatusError, got %v", err)
	}
}

// ── Email source rejections ─────────────────────────────────────────────────

func TestClient_ListEmailSourceRejections(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json",
		`{"data":[{"id":"r_1","created_at":"2026-10-19T00:00:00Z","sender":"bounce@example.com","from_address":"a@example.com",
		  "recipient":"in@agent.seclai.com","reason":"sender_not_allowed","authenticated":true,"sender_ip":null}],`+testPage+`}`)

	got, err := c.ListEmailSourceRejections(context.Background(), "src_1", ListOptions{Page: 2, Limit: 5})
	if err != nil {
		t.Fatalf("ListEmailSourceRejections: %v", err)
	}
	rec.expect(t, http.MethodGet, "/sources/src_1/email-rejections", "limit=5&page=2")
	if len(got.Data) != 1 || got.Data[0].FromAddress == nil || *got.Data[0].FromAddress != "a@example.com" || !got.Data[0].Authenticated {
		t.Fatalf("unexpected response %+v", got)
	}
}

// ── New fields on existing models ───────────────────────────────────────────

func TestNewFields_RoundTripOnExistingModels(t *testing.T) {
	var src SourceResponse
	if err := json.Unmarshal([]byte(`{"email_address":"in@agent.seclai.com","email_allowed_senders":["example.com"],
		"document_images":true,"feed_attachments":["audio"],"cloud_drive_folder_path":"/reports"}`), &src); err != nil {
		t.Fatalf("SourceResponse: %v", err)
	}
	if src.EmailAddress == nil || *src.EmailAddress != "in@agent.seclai.com" || src.DocumentImages == nil || !*src.DocumentImages ||
		src.CloudDriveFolderPath == nil || *src.CloudDriveFolderPath != "/reports" {
		t.Fatalf("SourceResponse new fields not decoded: %+v", src)
	}

	var status SourceContentStatusResponse
	if err := json.Unmarshal([]byte(`{"held_for_review":true}`), &status); err != nil {
		t.Fatalf("SourceContentStatusResponse: %v", err)
	}
	if status.HeldForReview == nil || !*status.HeldForReview {
		t.Fatalf("held_for_review not decoded: %+v", status)
	}

	var detail ContentDetailResponse
	if err := json.Unmarshal([]byte(`{"skipped_attachments":[{"content_type":"image/png","filename":null,"reason":"displayed_in_body","size_bytes":10}]}`), &detail); err != nil {
		t.Fatalf("ContentDetailResponse: %v", err)
	}
	if detail.SkippedAttachments == nil || (*detail.SkippedAttachments)[0].Reason != SkippedAttachmentReason("displayed_in_body") {
		t.Fatalf("skipped_attachments not decoded: %+v", detail.SkippedAttachments)
	}

	yes := true
	raw, _ := json.Marshal(AgentRunStreamRequest{IncludeReasoning: &yes})
	var sent map[string]any
	_ = json.Unmarshal(raw, &sent)
	if sent["include_reasoning"] != true {
		t.Fatalf("include_reasoning not sent: %s", raw)
	}
}
