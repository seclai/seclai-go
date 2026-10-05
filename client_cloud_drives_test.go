package seclai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// recordedRequest is what a stub server saw of the one request a test issues.
type recordedRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Body    string
	Version string
	URL     string
	Header  http.Header
}

// stubClient returns a client whose every request is answered with response,
// and a pointer to what the server received.
func stubClient(t *testing.T, apiVersion, response string) (*Client, *recordedRequest) {
	t.Helper()
	seen := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*seen = recordedRequest{
			Method:  r.Method,
			Path:    r.URL.EscapedPath(),
			Query:   r.URL.Query(),
			Body:    string(body),
			Version: r.Header.Get("Seclai-Version"),
			URL:     "http://" + r.Host + r.URL.RequestURI(),
			Header:  r.Header.Clone(),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL, APIVersion: apiVersion})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, seen
}

// assertRequest fails unless the recorded request has this verb, path, query and body.
func assertRequest(t *testing.T, seen *recordedRequest, method, path, query, body string) {
	t.Helper()
	if seen.Method != method || seen.Path != path {
		t.Fatalf("request: got %s %s, want %s %s", seen.Method, seen.Path, method, path)
	}
	if got := seen.Query.Encode(); got != query {
		t.Fatalf("query: got %q, want %q", got, query)
	}
	if seen.Body != body {
		t.Fatalf("body: got %q, want %q", seen.Body, body)
	}
}

const emptyPagination = `"pagination":{"page":1,"limit":50,"total":1,"pages":1,"has_next":false,"has_prev":false}`

// listShapes are the two bodies a version-gated list endpoint answers with for
// the same items, keyed by the API version that selects each.
func listShapes(items string) map[string]string {
	return map[string]string{
		"":                 items,
		APIVersion20260727: `{"data":` + items + `,` + emptyPagination + `}`,
	}
}

func TestClient_ListCloudDriveProviders_ReadsBothShapes(t *testing.T) {
	items := `[{"key":"google_drive","display_name":"Google Drive","access_levels":[{"key":"read"}],"scopes":[]}]`
	for version, body := range listShapes(items) {
		c, seen := stubClient(t, version, body)
		got, err := c.ListCloudDriveProviders(context.Background())
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		assertRequest(t, seen, http.MethodGet, "/cloud-drives/providers", "", "")
		if seen.Version != version {
			t.Fatalf("Seclai-Version: got %q, want %q", seen.Version, version)
		}
		if len(got) != 1 || got[0].Key != "google_drive" || got[0].DisplayName != "Google Drive" || len(got[0].AccessLevels) != 1 {
			t.Fatalf("version %q: unexpected providers: %+v", version, got)
		}
	}
}

func TestClient_ListCloudDrives_ReadsBothShapes(t *testing.T) {
	items := `[{"id":"cd_1","provider":"dropbox","status":"active","connected":true,"folder_path":"/contracts"}]`
	for version, body := range listShapes(items) {
		c, seen := stubClient(t, version, body)
		got, err := c.ListCloudDrives(context.Background())
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		assertRequest(t, seen, http.MethodGet, "/cloud-drives", "", "")
		if len(got) != 1 || got[0].Id != "cd_1" || got[0].Provider != "dropbox" || !got[0].Connected || got[0].FolderPath != "/contracts" {
			t.Fatalf("version %q: unexpected drives: %+v", version, got)
		}
	}
}

func TestClient_ListCloudDrives_EmptyOnEitherShape(t *testing.T) {
	for version, body := range listShapes(`[]`) {
		c, _ := stubClient(t, version, body)
		got, err := c.ListCloudDrives(context.Background())
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		if len(got) != 0 {
			t.Fatalf("version %q: expected no drives, got %+v", version, got)
		}
	}
}

func TestClient_GetCloudDrive(t *testing.T) {
	c, seen := stubClient(t, "", `{"id":"cd_1","provider":"dropbox","status":"active","name":"Contracts"}`)
	got, err := c.GetCloudDrive(context.Background(), "cd_1")
	if err != nil {
		t.Fatalf("GetCloudDrive: %v", err)
	}
	assertRequest(t, seen, http.MethodGet, "/cloud-drives/cd_1", "", "")
	if got.Id != "cd_1" || got.Name == nil || *got.Name != "Contracts" {
		t.Fatalf("unexpected drive: %+v", got)
	}
}

func TestClient_UpdateCloudDrive_SendsOnlyTheFieldsSet(t *testing.T) {
	name, folder := "Contracts", "/legal"
	cases := []struct {
		body CloudDriveUpdateRequest
		want string
	}{
		{CloudDriveUpdateRequest{}, `{}`},
		{CloudDriveUpdateRequest{Name: &name}, `{"name":"Contracts"}`},
		{CloudDriveUpdateRequest{FolderPath: &folder}, `{"folder_path":"/legal"}`},
		{CloudDriveUpdateRequest{Name: &name, FolderPath: &folder}, `{"folder_path":"/legal","name":"Contracts"}`},
	}
	for _, tc := range cases {
		c, seen := stubClient(t, "", `{"id":"cd_1","name":"Contracts","folder_path":"/legal"}`)
		got, err := c.UpdateCloudDrive(context.Background(), "cd_1", tc.body)
		if err != nil {
			t.Fatalf("UpdateCloudDrive: %v", err)
		}
		assertRequest(t, seen, http.MethodPatch, "/cloud-drives/cd_1", "", tc.want)
		if got.FolderPath != "/legal" {
			t.Fatalf("unexpected drive: %+v", got)
		}
	}
}

func TestClient_DisconnectCloudDrive(t *testing.T) {
	c, seen := stubClient(t, "", `{"id":"cd_1","connected":false,"status":"disconnected"}`)
	got, err := c.DisconnectCloudDrive(context.Background(), "cd_1")
	if err != nil {
		t.Fatalf("DisconnectCloudDrive: %v", err)
	}
	assertRequest(t, seen, http.MethodPost, "/cloud-drives/cd_1/disconnect", "", "")
	if got.Id != "cd_1" || got.Connected || got.Status != "disconnected" {
		t.Fatalf("unexpected drive: %+v", got)
	}
}

func TestClient_DeleteCloudDrive(t *testing.T) {
	c, seen := stubClient(t, "", `{"ok":true}`)
	if err := c.DeleteCloudDrive(context.Background(), "cd_1"); err != nil {
		t.Fatalf("DeleteCloudDrive: %v", err)
	}
	assertRequest(t, seen, http.MethodDelete, "/cloud-drives/cd_1", "", "")
}

func TestClient_GetAgentsUsingCloudDrive_ReadsBothShapes(t *testing.T) {
	items := `[{"agent_id":"a_1","agent_name":"Intake","trigger_types":["FILE_ADDED"],"via_step":true,"via_prompt_tool":false}]`
	for version, body := range listShapes(items) {
		c, seen := stubClient(t, version, body)
		got, err := c.GetAgentsUsingCloudDrive(context.Background(), "cd_1")
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		assertRequest(t, seen, http.MethodGet, "/cloud-drives/cd_1/agents", "", "")
		if len(got) != 1 || got[0].AgentId != "a_1" || !got[0].ViaStep || got[0].TriggerTypes[0] != "FILE_ADDED" {
			t.Fatalf("version %q: unexpected agents: %+v", version, got)
		}
	}
}

func TestClient_ListCloudDriveRejections_ReadsBothShapes(t *testing.T) {
	items := `[{"id":"r_1","created_at":"2026-10-01T00:00:00Z","reason":"too_large","file_path":"/big.mov"}]`
	for version, body := range listShapes(items) {
		c, seen := stubClient(t, version, body)
		got, err := c.ListCloudDriveRejections(context.Background(), "cd_1", CloudDriveRejectionOptions{Limit: 20})
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		assertRequest(t, seen, http.MethodGet, "/cloud-drives/cd_1/rejections", "limit=20", "")
		if len(got) != 1 || got[0].Reason != "too_large" || got[0].FilePath == nil || *got[0].FilePath != "/big.mov" {
			t.Fatalf("version %q: unexpected rejections: %+v", version, got)
		}
	}
}

func TestClient_ListCloudDriveRejections_OmitsAnUnsetLimit(t *testing.T) {
	c, seen := stubClient(t, "", `[]`)
	if _, err := c.ListCloudDriveRejections(context.Background(), "cd_1", CloudDriveRejectionOptions{}); err != nil {
		t.Fatalf("ListCloudDriveRejections: %v", err)
	}
	assertRequest(t, seen, http.MethodGet, "/cloud-drives/cd_1/rejections", "", "")
}

func TestClient_ListEmbeddingModels_ReadsBothShapes(t *testing.T) {
	models := `[{"model_id":"m_1","model_type":"openai.text-embedding-3-small","credits":1.5,"dimensions":[512,1536]}]`
	extras := `"default_model_type":"openai.text-embedding-3-small","default_dimension":1536,` +
		`"file_processing_credits_per_mb":2,"storage_credits":[{"dimensions":1536,"credits":3}]`
	bodies := map[string]string{
		"":                 `{"models":` + models + `,` + extras + `}`,
		APIVersion20260727: `{"data":` + models + `,` + emptyPagination + `,` + extras + `}`,
	}
	for version, body := range bodies {
		c, seen := stubClient(t, version, body)
		got, err := c.ListEmbeddingModels(context.Background(), ListEmbeddingModelsOptions{SupportsInputMedia: "image"})
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		assertRequest(t, seen, http.MethodGet, "/models/embedders", "supports_input_media=image", "")
		items := got.Items()
		if len(items) != 1 || items[0].ModelType != "openai.text-embedding-3-small" || len(items[0].Dimensions) != 2 {
			t.Fatalf("version %q: unexpected models: %+v", version, items)
		}
		if got.DefaultModelType == nil || *got.DefaultModelType != "openai.text-embedding-3-small" ||
			got.DefaultDimension == nil || *got.DefaultDimension != 1536 ||
			got.FileProcessingCreditsPerMb != 2 || len(got.StorageCredits) != 1 || got.StorageCredits[0].Credits != 3 {
			t.Fatalf("version %q: extras lost: %+v", version, got)
		}
		if (got.Pagination != nil) != (version != "") {
			t.Fatalf("version %q: unexpected pagination: %+v", version, got.Pagination)
		}
	}
}

func TestClient_ListEmbeddingModels_OmitsAnUnsetFilter(t *testing.T) {
	c, seen := stubClient(t, "", `{"models":[]}`)
	if _, err := c.ListEmbeddingModels(context.Background(), ListEmbeddingModelsOptions{}); err != nil {
		t.Fatalf("ListEmbeddingModels: %v", err)
	}
	assertRequest(t, seen, http.MethodGet, "/models/embedders", "", "")
}

func TestClient_ListRerankerModels_ReadsBothShapes(t *testing.T) {
	models := `[{"model_type":"cohere.rerank-v3","name":"Rerank v3","credits_per_action":0.5,"is_default":true}]`
	extras := `"default_model_type":"cohere.rerank-v3","search_processing_credits":0.25`
	bodies := map[string]string{
		"":                 `{"models":` + models + `,` + extras + `}`,
		APIVersion20260727: `{"data":` + models + `,` + emptyPagination + `,` + extras + `}`,
	}
	for version, body := range bodies {
		c, seen := stubClient(t, version, body)
		got, err := c.ListRerankerModels(context.Background())
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		assertRequest(t, seen, http.MethodGet, "/models/rerankers", "", "")
		items := got.Items()
		if len(items) != 1 || items[0].ModelType != "cohere.rerank-v3" || !items[0].IsDefault {
			t.Fatalf("version %q: unexpected models: %+v", version, items)
		}
		if got.DefaultModelType != "cohere.rerank-v3" || got.SearchProcessingCredits != 0.25 {
			t.Fatalf("version %q: extras lost: %+v", version, got)
		}
		if (got.Pagination != nil) != (version != "") {
			t.Fatalf("version %q: unexpected pagination: %+v", version, got.Pagination)
		}
	}
}

func TestModelListResponses_ItemsPrefersAnEmptyCanonicalPage(t *testing.T) {
	embedders := EmbeddingModelListResponse{
		Data:   []EmbeddingModelResponse{},
		Models: []EmbeddingModelResponse{{ModelId: "legacy"}},
	}
	if got := embedders.Items(); got == nil || len(got) != 0 {
		t.Fatalf("embedders: expected the empty canonical page, got %+v", got)
	}
	rerankers := RerankerModelListResponse{
		Data:   []RerankerModelResponse{},
		Models: []RerankerModelResponse{{Name: "legacy"}},
	}
	if got := rerankers.Items(); got == nil || len(got) != 0 {
		t.Fatalf("rerankers: expected the empty canonical page, got %+v", got)
	}
}

func TestClient_ListSourceContents(t *testing.T) {
	body := `{"data":[{"content_version_id":"cv_1","content_status":"failed","content_type":"document",` +
		`"pulled_at":"2026-10-01T00:00:00Z","error":"unreadable"}],` + emptyPagination + `}`
	c, seen := stubClient(t, "", body)
	got, err := c.ListSourceContents(context.Background(), "src_1", ListSourceContentsOptions{
		Page: 2, Limit: 50, Sort: "title", Order: "asc", Status: "failed",
		ContentVersionIDs: []string{"cv_1", "cv_2"},
	})
	if err != nil {
		t.Fatalf("ListSourceContents: %v", err)
	}
	assertRequest(t, seen, http.MethodGet, "/sources/src_1/contents",
		"content_version_id=cv_1&content_version_id=cv_2&limit=50&order=asc&page=2&sort=title&status=failed", "")
	if len(got.Data) != 1 || got.Data[0].ContentVersionId != "cv_1" || got.Data[0].ContentStatus != "failed" ||
		got.Data[0].Error == nil || *got.Data[0].Error != "unreadable" {
		t.Fatalf("unexpected data: %+v", got.Data)
	}
	if got.Pagination.Total != 1 {
		t.Fatalf("unexpected pagination: %+v", got.Pagination)
	}
}

func TestClient_ListSourceContents_OmitsUnsetOptions(t *testing.T) {
	c, seen := stubClient(t, "", `{"data":[],`+emptyPagination+`}`)
	if _, err := c.ListSourceContents(context.Background(), "src_1", ListSourceContentsOptions{}); err != nil {
		t.Fatalf("ListSourceContents: %v", err)
	}
	assertRequest(t, seen, http.MethodGet, "/sources/src_1/contents", "", "")
}

func TestClient_ListSourceContents_EmptyFilterMatchesNothingWithoutARequest(t *testing.T) {
	cases := []struct {
		opts ListSourceContentsOptions
		want PaginationResponse
	}{
		{ListSourceContentsOptions{ContentVersionIDs: []string{}}, PaginationResponse{Page: 1, Limit: 20}},
		{ListSourceContentsOptions{ContentVersionIDs: []string{}, Page: 3, Limit: 50, Status: "failed"}, PaginationResponse{Page: 3, Limit: 50}},
		{ListSourceContentsOptions{ContentVersionIDs: []string{"", ""}}, PaginationResponse{Page: 1, Limit: 20}},
	}
	for _, tc := range cases {
		c, seen := stubClient(t, "", `{"data":[{"content_version_id":"cv_1"}],`+emptyPagination+`}`)
		got, err := c.ListSourceContents(context.Background(), "src_1", tc.opts)
		if err != nil {
			t.Fatalf("ListSourceContents: %v", err)
		}
		if seen.Method != "" {
			t.Fatalf("expected no request, got %s %s", seen.Method, seen.Path)
		}
		if got.Data == nil || len(got.Data) != 0 {
			t.Fatalf("expected an empty non-nil page, got %#v", got.Data)
		}
		if got.Pagination != tc.want {
			t.Fatalf("pagination: got %+v, want %+v", got.Pagination, tc.want)
		}
	}
}

func TestClient_ListSourceContents_DropsBlankIDsFromAMixedFilter(t *testing.T) {
	c, seen := stubClient(t, "", `{"data":[],`+emptyPagination+`}`)
	opts := ListSourceContentsOptions{ContentVersionIDs: []string{"", "cv_1", "", "cv_2"}}
	if _, err := c.ListSourceContents(context.Background(), "src_1", opts); err != nil {
		t.Fatalf("ListSourceContents: %v", err)
	}
	assertRequest(t, seen, http.MethodGet, "/sources/src_1/contents", "content_version_id=cv_1&content_version_id=cv_2", "")
}

func TestClient_GetSourceContentStatus(t *testing.T) {
	c, seen := stubClient(t, "", `{"content_version_id":"cv_1","content_status":"completed","content_type":"text",`+
		`"pulled_at":"2026-10-01T00:00:00Z","source_connection_content_version_id":"sccv_1"}`)
	got, err := c.GetSourceContentStatus(context.Background(), "src_1", "cv_1")
	if err != nil {
		t.Fatalf("GetSourceContentStatus: %v", err)
	}
	assertRequest(t, seen, http.MethodGet, "/sources/src_1/contents/cv_1", "", "")
	if got.ContentStatus != "completed" || got.SourceConnectionContentVersionId == nil ||
		*got.SourceConnectionContentVersionId != "sccv_1" {
		t.Fatalf("unexpected status: %+v", got)
	}
}

func TestClient_Do_KeepsItsSingleValuedQuerySemantics(t *testing.T) {
	seenQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.Query().Encode()
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL + "?tenant=base&keep=1"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	query := map[string]string{"tenant": "override", "empty": "", " ": "blank-key", "page": "2"}
	if err := c.Do(context.Background(), http.MethodGet, "/x", query, nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if want := "keep=1&page=2&tenant=override"; seenQuery != want {
		t.Fatalf("query: got %q, want %q", seenQuery, want)
	}
}
