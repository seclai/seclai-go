package seclai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/seclai/seclai-go/generated"
)

// DefaultBaseURL is the default API base URL.
//
// Convenience methods use paths like "/api/sources/" under this base.
const DefaultBaseURL = "https://seclai.com"

// Options configure a Client.
type Options struct {
	// APIKey is used for authentication. Defaults to the SECLAI_API_KEY environment variable.
	APIKey string

	// BaseURL is the API base URL. Defaults to SECLAI_API_URL if set, else DefaultBaseURL.
	BaseURL string

	// APIKeyHeader is the HTTP header name used for the API key. Defaults to "x-api-key".
	APIKeyHeader string

	// HTTPClient is used for requests. Defaults to a client with a 30s timeout.
	HTTPClient *http.Client
}

// Client is the Seclai Go SDK client.
type Client struct {
	apiKey       string
	baseURL      *url.URL
	apiKeyHeader string
	httpClient   *http.Client

	generated *generated.ClientWithResponses
}

// NewClient constructs a new Client.
//
// Returns ConfigurationError if the API key is missing or if the base URL is invalid.
func NewClient(opts Options) (*Client, error) {
	apiKey := strings.TrimSpace(opts.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("SECLAI_API_KEY"))
	}
	if apiKey == "" {
		return nil, &ConfigurationError{Message: "missing API key: provide Options.APIKey or set SECLAI_API_KEY"}
	}

	base := strings.TrimSpace(opts.BaseURL)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("SECLAI_API_URL"))
	}
	if base == "" {
		base = DefaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, &ConfigurationError{Message: fmt.Sprintf("invalid base URL: %v", err)}
	}

	header := strings.TrimSpace(opts.APIKeyHeader)
	if header == "" {
		header = "x-api-key"
	}

	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	gen, err := generated.NewClientWithResponses(parsed.String(),
		generated.WithHTTPClient(hc),
		generated.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set(header, apiKey)
			return nil
		}),
	)
	if err != nil {
		return nil, &ConfigurationError{Message: fmt.Sprintf("failed to construct generated client: %v", err)}
	}

	return &Client{
		apiKey:       apiKey,
		baseURL:      parsed,
		apiKeyHeader: header,
		httpClient:   hc,
		generated:    gen,
	}, nil
}

// Generated returns the underlying OpenAPI-generated client.
//
// It is fully typed and exposes all endpoints directly.
func (c *Client) Generated() *generated.ClientWithResponses {
	if c == nil {
		return nil
	}
	return c.generated
}

// Do makes a low-level request to the Seclai API.
//
// For JSON responses, out is decoded from JSON when non-nil.
// For non-2xx responses, an *APIStatusError or *APIValidationError is returned.
func (c *Client) Do(ctx context.Context, method, apiPath string, query map[string]string, body any, headers map[string]string, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}

	reqURL := c.buildURL(apiPath, query)

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reqBody)
	if err != nil {
		return err
	}

	req.Header.Set(c.apiKeyHeader, c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(raw))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := APIStatusError{StatusCode: resp.StatusCode, Method: method, URL: reqURL.String(), ResponseText: text}
		if resp.StatusCode == 422 {
			var ve HTTPValidationError
			if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil {
				return &APIValidationError{APIStatusError: statusErr, ValidationError: &ve}
			}
			return &APIValidationError{APIStatusError: statusErr}
		}
		return &statusErr
	}

	if out == nil {
		return nil
	}

	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ListSources lists sources.
func (c *Client) ListSources(ctx context.Context, page, limit int, sort, order, accountID string) (*SourceListResponse, error) {
	q := map[string]string{}
	if page > 0 {
		q["page"] = fmt.Sprintf("%d", page)
	}
	if limit > 0 {
		q["limit"] = fmt.Sprintf("%d", limit)
	}
	if sort != "" {
		q["sort"] = sort
	}
	if order != "" {
		q["order"] = order
	}
	if accountID != "" {
		q["account_id"] = accountID
	}

	var out SourceListResponse
	if err := c.Do(ctx, http.MethodGet, "/api/sources/", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RunAgent runs an agent.
//
// body is marshaled as JSON.
func (c *Client) RunAgent(ctx context.Context, agentID string, body AgentRunRequest) (*AgentRunResponse, error) {
	var out AgentRunResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/api/agents/%s/runs", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RunStreamingAgentAndWait runs an agent in priority mode and waits for completion.
//
// This method calls POST /api/agents/{agent_id}/runs/stream and consumes Server-Sent Events (SSE).
// It returns when the stream emits an `event: done` message whose `data:` field contains the final run payload.
//
// Timeout behavior is controlled by ctx (for example, use context.WithTimeout). If ctx has no deadline,
// a default 60s timeout is applied.
func (c *Client) RunStreamingAgentAndWait(ctx context.Context, agentID string, body AgentRunStreamRequest) (*AgentRunResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}

	reqURL := c.buildURL(fmt.Sprintf("/api/agents/%s/runs/stream", url.PathEscape(agentID)), nil)
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set(c.apiKeyHeader, c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		text := strings.TrimSpace(string(raw))
		statusErr := APIStatusError{StatusCode: resp.StatusCode, Method: http.MethodPost, URL: reqURL.String(), ResponseText: text}
		if resp.StatusCode == 422 {
			var ve HTTPValidationError
			if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil {
				return nil, &APIValidationError{APIStatusError: statusErr, ValidationError: &ve}
			}
			return nil, &APIValidationError{APIStatusError: statusErr}
		}
		return nil, &statusErr
	}

	reader := bufio.NewReader(resp.Body)
	var currentEvent string
	var dataLines []string
	var lastSeen *AgentRunResponse

	dispatch := func() (*AgentRunResponse, bool) {
		if currentEvent == "" && len(dataLines) == 0 {
			return nil, false
		}
		data := strings.Join(dataLines, "\n")
		data = strings.TrimSuffix(data, "\n")
		defer func() {
			currentEvent = ""
			dataLines = nil
		}()

		if data == "" {
			return nil, false
		}

		if currentEvent == "init" || currentEvent == "done" {
			var parsed AgentRunResponse
			if err := json.Unmarshal([]byte(data), &parsed); err == nil {
				lastSeen = &parsed
				if currentEvent == "done" {
					return &parsed, true
				}
			}
		}
		return nil, false
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if done, ok := dispatch(); ok {
					return done, nil
				}
				if lastSeen != nil {
					return lastSeen, nil
				}
				return nil, fmt.Errorf("seclai: stream ended before receiving done event")
			}
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if done, ok := dispatch(); ok {
				return done, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field := line
		value := ""
		if i := strings.IndexByte(line, ':'); i >= 0 {
			field = line[:i]
			value = line[i+1:]
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
		}

		switch field {
		case "event":
			currentEvent = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
}

// ListAgentRuns lists runs for an agent.
func (c *Client) ListAgentRuns(ctx context.Context, agentID string, page, limit int) (*AgentRunListResponse, error) {
	q := map[string]string{}
	if page > 0 {
		q["page"] = fmt.Sprintf("%d", page)
	}
	if limit > 0 {
		q["limit"] = fmt.Sprintf("%d", limit)
	}

	var out AgentRunListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/agents/%s/runs", url.PathEscape(agentID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentRun fetches a specific run.
func (c *Client) GetAgentRun(ctx context.Context, agentID, runID string) (*AgentRunResponse, error) {
	var out AgentRunResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/agents/%s/runs/%s", url.PathEscape(agentID), url.PathEscape(runID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgentRun cancels/deletes a specific run.
func (c *Client) DeleteAgentRun(ctx context.Context, agentID, runID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/api/agents/%s/runs/%s", url.PathEscape(agentID), url.PathEscape(runID)), nil, nil, nil, nil)
}

// GetContentDetail fetches content detail.
func (c *Client) GetContentDetail(ctx context.Context, contentVersionID string, start, end int) (*ContentDetailResponse, error) {
	q := map[string]string{}
	if start > 0 {
		q["start"] = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		q["end"] = fmt.Sprintf("%d", end)
	}

	var out ContentDetailResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/contents/%s", url.PathEscape(contentVersionID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteContent deletes a content version.
func (c *Client) DeleteContent(ctx context.Context, contentVersionID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/api/contents/%s", url.PathEscape(contentVersionID)), nil, nil, nil, nil)
}

// ListContentEmbeddings lists embeddings for a content version.
func (c *Client) ListContentEmbeddings(ctx context.Context, contentVersionID string, page, limit int) (*ContentEmbeddingsListResponse, error) {
	q := map[string]string{}
	if page > 0 {
		q["page"] = fmt.Sprintf("%d", page)
	}
	if limit > 0 {
		q["limit"] = fmt.Sprintf("%d", limit)
	}

	var out ContentEmbeddingsListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/contents/%s/embeddings", url.PathEscape(contentVersionID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadFileRequest describes an upload.
type UploadFileRequest struct {
	File     []byte
	FileName string
	Title    string
}

// UploadFileToSource uploads a file to a source connection.
func (c *Client) UploadFileToSource(ctx context.Context, sourceConnectionID string, req UploadFileRequest) (*FileUploadResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(req.File) == 0 {
		return nil, &ConfigurationError{Message: "upload requires non-empty file bytes"}
	}
	if strings.TrimSpace(req.FileName) == "" {
		return nil, &ConfigurationError{Message: "upload requires FileName"}
	}

	reqURL := c.buildURL(fmt.Sprintf("/api/sources/%s/upload", url.PathEscape(sourceConnectionID)), nil)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if req.Title != "" {
		_ = w.WriteField("title", req.Title)
	}
	fw, err := w.CreateFormFile("file", req.FileName)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	if _, err := io.Copy(fw, bytes.NewReader(req.File)); err != nil {
		_ = w.Close()
		return nil, err
	}
	_ = w.Close()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set(c.apiKeyHeader, c.apiKey)
	httpReq.Header.Set("Content-Type", w.FormDataContentType())
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(raw))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := APIStatusError{StatusCode: resp.StatusCode, Method: http.MethodPost, URL: reqURL.String(), ResponseText: text}
		if resp.StatusCode == 422 {
			var ve HTTPValidationError
			if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil {
				return nil, &APIValidationError{APIStatusError: statusErr, ValidationError: &ve}
			}
			return nil, &APIValidationError{APIStatusError: statusErr}
		}
		return nil, &statusErr
	}

	var out FileUploadResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

func (c *Client) buildURL(apiPath string, query map[string]string) *url.URL {
	// Ensure path join doesn't drop base path.
	u := *c.baseURL
	joined := apiPath
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	hadTrailingSlash := joined != "/" && strings.HasSuffix(joined, "/")
	cleaned := path.Clean(strings.TrimSuffix(u.Path, "/") + joined)
	if hadTrailingSlash && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	u.Path = cleaned
	q := u.Query()
	for k, v := range query {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if v == "" {
			continue
		}
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return &u
}
