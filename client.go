package seclai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/seclai/seclai-go/generated"
)

// DefaultBaseURL is the default API base URL.
//
// Convenience methods use paths like "/sources/" under this base.
const DefaultBaseURL = "https://seclai.com"

// AccessTokenProvider is a function that returns a bearer token on each call.
type AccessTokenProvider = func(ctx context.Context) (string, error)

// Options configure a Client.
type Options struct {
	// APIKey is used for authentication. Defaults to the SECLAI_API_KEY environment variable.
	APIKey string

	// AccessToken is a static bearer token (mutually exclusive with APIKey).
	AccessToken string

	// AccessTokenProvider returns a bearer token per request (mutually exclusive with APIKey).
	AccessTokenProvider AccessTokenProvider

	// Profile selects an SSO profile from the config file.
	// Defaults to the SECLAI_PROFILE environment variable, then "default".
	Profile string

	// ConfigDir overrides the config directory path.
	// Defaults to the SECLAI_CONFIG_DIR environment variable, then ~/.seclai.
	ConfigDir string

	// AutoRefresh controls whether expired SSO tokens are automatically refreshed.
	// Defaults to true. Set to a pointer to false to disable.
	AutoRefresh *bool

	// AccountID is sent as the X-Account-Id header for multi‑org targeting.
	AccountID string

	// BaseURL is the API base URL. Defaults to SECLAI_API_URL if set, else DefaultBaseURL.
	BaseURL string

	// APIKeyHeader is the HTTP header name used for the API key. Defaults to "x-api-key".
	APIKeyHeader string

	// DefaultHeaders are HTTP headers applied to every request.
	DefaultHeaders map[string]string

	// HTTPClient is used for requests. Defaults to a client with a 30s timeout.
	HTTPClient *http.Client
}

// Client is the Seclai Go SDK client.
type Client struct {
	auth           *authState
	baseURL        *url.URL
	defaultHeaders map[string]string
	httpClient     *http.Client

	generated *generated.ClientWithResponses
}

// NewClient constructs a new Client.
//
// Returns ConfigurationError if credentials are missing or if the base URL is invalid.
func NewClient(opts Options) (*Client, error) {
	state, err := resolveCredentialChain(opts)
	if err != nil {
		return nil, err
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

	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	state.httpClient = hc

	defHeaders := make(map[string]string, len(opts.DefaultHeaders))
	for k, v := range opts.DefaultHeaders {
		defHeaders[k] = v
	}

	client := &Client{
		auth:           state,
		baseURL:        parsed,
		defaultHeaders: defHeaders,
		httpClient:     hc,
	}

	gen, err := generated.NewClientWithResponses(parsed.String(),
		generated.WithHTTPClient(hc),
		generated.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			for k, v := range defHeaders {
				req.Header.Set(k, v)
			}
			return client.applyAuth(ctx, req)
		}),
	)
	if err != nil {
		return nil, &ConfigurationError{Message: fmt.Sprintf("failed to construct generated client: %v", err)}
	}
	client.generated = gen

	return client, nil
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

// applyAuth resolves auth headers and applies them to a request.
func (c *Client) applyAuth(ctx context.Context, req *http.Request) error {
	hdrs, err := resolveAuthHeaders(ctx, c.auth)
	if err != nil {
		return err
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	return nil
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

	for k, v := range c.defaultHeaders {
		req.Header.Set(k, v)
	}
	if err := c.applyAuth(ctx, req); err != nil {
		return err
	}
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
			if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil && ve.Detail != nil {
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

// ── Agents ──────────────────────────────────────────────────────────────────

// ListOptions contains common pagination parameters.
type ListOptions struct {
	// Page is the 1-based page number. Zero omits the parameter.
	Page int
	// Limit is the maximum number of items per page. Zero omits the parameter.
	Limit int
}

// SortableListOptions extends ListOptions with sort field and order.
type SortableListOptions struct {
	ListOptions
	// Sort is the field name to sort by (e.g. "created_at").
	Sort string
	// Order is the sort direction: "asc" or "desc".
	Order string
}

// listQuery builds pagination query parameters from page and limit values.
func listQuery(page, limit int) map[string]string {
	q := map[string]string{}
	if page > 0 {
		q["page"] = fmt.Sprintf("%d", page)
	}
	if limit > 0 {
		q["limit"] = fmt.Sprintf("%d", limit)
	}
	return q
}

// sortableListQuery builds pagination and sort query parameters.
func sortableListQuery(opts SortableListOptions) map[string]string {
	q := listQuery(opts.Page, opts.Limit)
	if opts.Sort != "" {
		q["sort"] = opts.Sort
	}
	if opts.Order != "" {
		q["order"] = opts.Order
	}
	return q
}

// ListAgents lists agents.
func (c *Client) ListAgents(ctx context.Context, opts ListOptions) (*AgentListResponse, error) {
	var out AgentListResponse
	if err := c.Do(ctx, http.MethodGet, "/agents", listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAgent creates a new agent.
func (c *Client) CreateAgent(ctx context.Context, body CreateAgentRequest) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.Do(ctx, http.MethodPost, "/agents", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgent retrieves an agent by ID.
func (c *Client) GetAgent(ctx context.Context, agentID string) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s", url.PathEscape(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAgent updates an agent.
func (c *Client) UpdateAgent(ctx context.Context, agentID string, body UpdateAgentRequest) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.Do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgent deletes an agent.
func (c *Client) DeleteAgent(ctx context.Context, agentID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/agents/%s", url.PathEscape(agentID)), nil, nil, nil, nil)
}

// ── Agent Export / Import ───────────────────────────────────────────────────

// ExportAgent exports an agent definition as a portable JSON snapshot.
func (c *Client) ExportAgent(ctx context.Context, agentID string, download bool) (*AgentExportResponse, error) {
	query := map[string]string{"download": fmt.Sprintf("%t", download)}
	var out AgentExportResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/export", url.PathEscape(agentID)), query, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PreviewImportAgent validates an agent_definition payload (same shape as ExportAgent's
// response) without creating or modifying any agent.
//
// Use this before CreateAgent or UpdateAgent with an AgentDefinition to surface
// UnresolvedRefs — workflow references to knowledge bases, memory banks, source
// connections, or sub-agents that don't exist in the target account. Pass the
// returned ids back in EntityRemap on the commit call to substitute them.
//
// On HTTP 422 the response body is an [AgentDefinitionImportErrorResponse] listing
// each field error with a 1-indexed line/column anchored to a canonical Source echo.
// It is returned as an [APIValidationError]; the raw body is on
// APIValidationError.ResponseText (decode with json.Unmarshal into
// [AgentDefinitionImportErrorResponse]).
func (c *Client) PreviewImportAgent(ctx context.Context, body AgentImportPreviewRequest) (*AgentImportPreviewResponse, error) {
	var out AgentImportPreviewResponse
	if err := c.Do(ctx, http.MethodPost, "/agents/preview-import", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Definitions ───────────────────────────────────────────────────────

// GetAgentDefinition retrieves the definition (step configuration) for an agent.
func (c *Client) GetAgentDefinition(ctx context.Context, agentID string) (*AgentDefinitionResponse, error) {
	var out AgentDefinitionResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/definition", url.PathEscape(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAgentDefinition updates the definition for an agent.
func (c *Client) UpdateAgentDefinition(ctx context.Context, agentID string, body UpdateAgentDefinitionRequest) (*AgentDefinitionResponse, error) {
	var out AgentDefinitionResponse
	if err := c.Do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s/definition", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Runs ──────────────────────────────────────────────────────────────

// RunAgent runs an agent.
func (c *Client) RunAgent(ctx context.Context, agentID string, body AgentRunRequest) (*AgentRunResponse, error) {
	var out AgentRunResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/runs", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAgentRunsOptions controls optional query parameters for ListAgentRuns.
type ListAgentRunsOptions struct {
	// Page is the 1-based page number.
	Page int
	// Limit is the maximum number of items per page.
	Limit int
	// Status filters runs by status (e.g. "completed", "failed", "running").
	Status string
}

// ListAgentRuns lists runs for an agent.
func (c *Client) ListAgentRuns(ctx context.Context, agentID string, opts ListAgentRunsOptions) (*AgentRunListResponse, error) {
	q := listQuery(opts.Page, opts.Limit)
	if opts.Status != "" {
		q["status"] = opts.Status
	}
	var out AgentRunListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/runs", url.PathEscape(agentID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchAgentRuns searches agent runs with filter criteria.
func (c *Client) SearchAgentRuns(ctx context.Context, body AgentTraceSearchRequest) (*AgentTraceSearchResponse, error) {
	var out AgentTraceSearchResponse
	if err := c.Do(ctx, http.MethodPost, "/agents/runs/search", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentRunOptions controls behavior for GetAgentRun.
type GetAgentRunOptions struct {
	// IncludeStepOutputs requests per-step details to be included in the response.
	IncludeStepOutputs bool
}

// GetAgentRun fetches a specific run by run ID.
func (c *Client) GetAgentRun(ctx context.Context, runID string, opts *GetAgentRunOptions) (*AgentRunResponse, error) {
	var q map[string]string
	if opts != nil && opts.IncludeStepOutputs {
		q = map[string]string{"include_step_outputs": "true"}
	}
	var out AgentRunResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/runs/%s", url.PathEscape(runID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgentRun cancels/deletes a specific run by run ID.
func (c *Client) DeleteAgentRun(ctx context.Context, runID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/agents/runs/%s", url.PathEscape(runID)), nil, nil, nil, nil)
}

// CancelAgentRun cancels an in-progress agent run.
func (c *Client) CancelAgentRun(ctx context.Context, runID string) (*AgentRunResponse, error) {
	var out AgentRunResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/agents/runs/%s/cancel", url.PathEscape(runID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Input Uploads ─────────────────────────────────────────────────────

// UploadAgentInput uploads an input file for an agent run.
func (c *Client) UploadAgentInput(ctx context.Context, agentID string, req UploadFileRequest) (*UploadAgentInputApiResponse, error) {
	resp, err := c.doUpload(ctx, fmt.Sprintf("/agents/%s/upload-input", url.PathEscape(agentID)), req)
	if err != nil {
		return nil, err
	}
	var out UploadAgentInputApiResponse
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentInputUploadStatus checks the status of an input upload.
func (c *Client) GetAgentInputUploadStatus(ctx context.Context, agentID, uploadID string) (*UploadAgentInputApiResponse, error) {
	var out UploadAgentInputApiResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/input-uploads/%s", url.PathEscape(agentID), url.PathEscape(uploadID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent AI Assistant ──────────────────────────────────────────────────────

// GenerateAgentSteps uses the AI assistant to generate step configurations for an agent.
func (c *Client) GenerateAgentSteps(ctx context.Context, agentID string, body GenerateAgentStepsRequest) (*GenerateAgentStepsResponse, error) {
	var out GenerateAgentStepsResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/ai-assistant/generate-steps", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateStepConfig uses the AI assistant to generate configuration for a single step.
func (c *Client) GenerateStepConfig(ctx context.Context, agentID string, body GenerateStepConfigRequest) (*GenerateStepConfigResponse, error) {
	var out GenerateStepConfigResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/ai-assistant/step-config", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentAiConversationHistory retrieves AI assistant conversation history for an agent.
func (c *Client) GetAgentAiConversationHistory(ctx context.Context, agentID string) (*AiConversationHistoryResponse, error) {
	var out AiConversationHistoryResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/ai-assistant/conversations", url.PathEscape(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkAgentAiSuggestion marks an AI assistant suggestion as accepted/rejected.
func (c *Client) MarkAgentAiSuggestion(ctx context.Context, agentID, conversationID string, body MarkAiSuggestionRequest) error {
	return c.Do(ctx, http.MethodPatch, fmt.Sprintf("/agents/%s/ai-assistant/%s", url.PathEscape(agentID), url.PathEscape(conversationID)), nil, body, nil, nil)
}

// ── Agent Evaluations ───────────────────────────────────────────────────────

// ListEvaluationCriteria lists evaluation criteria for an agent.
func (c *Client) ListEvaluationCriteria(ctx context.Context, agentID string, opts ListOptions) ([]EvaluationCriteriaResponse, error) {
	var out []EvaluationCriteriaResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/evaluation-criteria", url.PathEscape(agentID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateEvaluationCriteria creates new evaluation criteria for an agent.
func (c *Client) CreateEvaluationCriteria(ctx context.Context, agentID string, body CreateEvaluationCriteriaRequest) (*EvaluationCriteriaResponse, error) {
	var out EvaluationCriteriaResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/evaluation-criteria", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetEvaluationCriteria retrieves evaluation criteria by ID.
func (c *Client) GetEvaluationCriteria(ctx context.Context, criteriaID string) (*EvaluationCriteriaResponse, error) {
	var out EvaluationCriteriaResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s", url.PathEscape(criteriaID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateEvaluationCriteria updates evaluation criteria.
func (c *Client) UpdateEvaluationCriteria(ctx context.Context, criteriaID string, body UpdateEvaluationCriteriaRequest) (*EvaluationCriteriaResponse, error) {
	var out EvaluationCriteriaResponse
	if err := c.Do(ctx, http.MethodPatch, fmt.Sprintf("/agents/evaluation-criteria/%s", url.PathEscape(criteriaID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteEvaluationCriteria deletes evaluation criteria.
func (c *Client) DeleteEvaluationCriteria(ctx context.Context, criteriaID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/agents/evaluation-criteria/%s", url.PathEscape(criteriaID)), nil, nil, nil, nil)
}

// GetEvaluationCriteriaSummary retrieves the summary for evaluation criteria.
func (c *Client) GetEvaluationCriteriaSummary(ctx context.Context, criteriaID string) (*EvaluationResultSummaryResponse, error) {
	var out EvaluationResultSummaryResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s/summary", url.PathEscape(criteriaID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEvaluationResults lists evaluation results for criteria.
func (c *Client) ListEvaluationResults(ctx context.Context, criteriaID string, opts ListOptions) (*EvaluationResultListResponse, error) {
	var out EvaluationResultListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s/results", url.PathEscape(criteriaID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEvaluationResult creates a new evaluation result for criteria.
func (c *Client) CreateEvaluationResult(ctx context.Context, criteriaID string, body CreateEvaluationResultRequest) (*EvaluationResultResponse, error) {
	var out EvaluationResultResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/agents/evaluation-criteria/%s/results", url.PathEscape(criteriaID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCompatibleRuns lists runs compatible with evaluation criteria.
func (c *Client) ListCompatibleRuns(ctx context.Context, criteriaID string, opts ListOptions) (*CompatibleRunListResponse, error) {
	var out CompatibleRunListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s/compatible-runs", url.PathEscape(criteriaID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TestDraftEvaluation tests a draft evaluation criteria without persisting.
func (c *Client) TestDraftEvaluation(ctx context.Context, agentID string, body TestDraftEvaluationRequest) (*TestDraftEvaluationResponse, error) {
	var out TestDraftEvaluationResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/evaluation-criteria/test-draft", url.PathEscape(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAgentEvaluationResults lists all evaluation results for an agent.
func (c *Client) ListAgentEvaluationResults(ctx context.Context, agentID string, opts ListOptions) (*EvaluationResultWithCriteriaListResponse, error) {
	var out EvaluationResultWithCriteriaListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/evaluation-results", url.PathEscape(agentID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRunEvaluationResults lists evaluation results for a specific run.
func (c *Client) ListRunEvaluationResults(ctx context.Context, agentID, runID string, opts ListOptions) (*EvaluationResultWithCriteriaListResponse, error) {
	var out EvaluationResultWithCriteriaListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/runs/%s/evaluation-results", url.PathEscape(agentID), url.PathEscape(runID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEvaluationRuns lists evaluation run summaries for an agent.
func (c *Client) ListEvaluationRuns(ctx context.Context, agentID string, opts ListOptions) (*EvaluationRunSummaryListResponse, error) {
	var out EvaluationRunSummaryListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/evaluation-runs", url.PathEscape(agentID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetNonManualEvaluationSummary retrieves a summary of non-manual evaluation results.
func (c *Client) GetNonManualEvaluationSummary(ctx context.Context, agentID string) (*NonManualEvaluationSummaryResponse, error) {
	q := map[string]string{}
	if agentID != "" {
		q["agent_id"] = agentID
	}
	var out NonManualEvaluationSummaryResponse
	if err := c.Do(ctx, http.MethodGet, "/agents/evaluation-results/non-manual-summary", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RunStreamingAgentAndWait runs an agent in priority mode and waits for completion.
//
// This method calls POST /agents/{agent_id}/runs/stream and consumes Server-Sent Events (SSE).
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

	reqURL := c.buildURL(fmt.Sprintf("/agents/%s/runs/stream", url.PathEscape(agentID)), nil)
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	for k, v := range c.defaultHeaders {
		req.Header.Set(k, v)
	}
	if err := c.applyAuth(ctx, req); err != nil {
		return nil, err
	}
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
			if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil && ve.Detail != nil {
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
				return nil, &StreamingError{Message: "stream ended before receiving done event"}
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

// AgentRunEvent is a single event from an SSE agent run stream.
type AgentRunEvent struct {
	// Event is the SSE event type (e.g. "init", "update", "done").
	Event string
	// Data is the raw JSON data payload.
	Data string
	// Run is the parsed AgentRunResponse if the data could be decoded.
	Run *AgentRunResponse
}

// RunStreamingAgent runs an agent in priority mode and returns a channel that
// yields SSE events as they arrive. The channel is closed when the stream ends
// or when ctx is cancelled.
//
// The caller should range over the returned channel:
//
//	ch, errCh := client.RunStreamingAgent(ctx, agentID, body)
//	for event := range ch {
//	    fmt.Println(event.Event, event.Data)
//	}
//	if err := <-errCh; err != nil {
//	    // handle error
//	}
func (c *Client) RunStreamingAgent(ctx context.Context, agentID string, body AgentRunStreamRequest) (<-chan AgentRunEvent, <-chan error) {
	events := make(chan AgentRunEvent, 16)
	errCh := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errCh)

		if ctx == nil {
			ctx = context.Background()
		}

		reqURL := c.buildURL(fmt.Sprintf("/agents/%s/runs/stream", url.PathEscape(agentID)), nil)
		b, err := json.Marshal(body)
		if err != nil {
			errCh <- err
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(b))
		if err != nil {
			errCh <- err
			return
		}
		for k, v := range c.defaultHeaders {
			req.Header.Set(k, v)
		}
		if err := c.applyAuth(ctx, req); err != nil {
			errCh <- err
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(resp.Body)
			text := strings.TrimSpace(string(raw))
			statusErr := APIStatusError{StatusCode: resp.StatusCode, Method: http.MethodPost, URL: reqURL.String(), ResponseText: text}
			if resp.StatusCode == 422 {
				var ve HTTPValidationError
				if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil && ve.Detail != nil {
					errCh <- &APIValidationError{APIStatusError: statusErr, ValidationError: &ve}
					return
				}
				errCh <- &APIValidationError{APIStatusError: statusErr}
				return
			}
			errCh <- &statusErr
			return
		}

		// If the server returns JSON instead of SSE, emit a single done event.
		ct := resp.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "application/json") {
			raw, _ := io.ReadAll(resp.Body)
			var parsed AgentRunResponse
			evt := AgentRunEvent{Event: "done", Data: string(raw)}
			if json.Unmarshal(raw, &parsed) == nil {
				evt.Run = &parsed
			}
			events <- evt
			return
		}

		reader := bufio.NewReader(resp.Body)
		var currentEvent string
		var dataLines []string

		dispatch := func() {
			if currentEvent == "" && len(dataLines) == 0 {
				return
			}
			data := strings.Join(dataLines, "\n")
			data = strings.TrimSuffix(data, "\n")
			evt := AgentRunEvent{Event: currentEvent, Data: data}
			currentEvent = ""
			dataLines = nil

			if data == "" {
				return
			}

			var parsed AgentRunResponse
			if json.Unmarshal([]byte(data), &parsed) == nil {
				evt.Run = &parsed
			}
			select {
			case events <- evt:
			case <-ctx.Done():
			}
		}

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				dispatch()
				if err != io.EOF {
					errCh <- err
				}
				return
			}

			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				dispatch()
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
	}()

	return events, errCh
}

// RunAgentAndPollOptions controls polling behavior.
type RunAgentAndPollOptions struct {
	// PollInterval is how often to check for completion. Defaults to 2s.
	PollInterval time.Duration
	// IncludeStepOutputs requests step details in the final result.
	IncludeStepOutputs bool
}

// RunAgentAndPoll runs an agent and polls until it reaches a terminal status
// (completed or failed). The context controls the overall timeout.
func (c *Client) RunAgentAndPoll(ctx context.Context, agentID string, body AgentRunRequest, opts *RunAgentAndPollOptions) (*AgentRunResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	run, err := c.RunAgent(ctx, agentID, body)
	if err != nil {
		return nil, err
	}

	interval := 2 * time.Second
	var includeSteps bool
	if opts != nil {
		if opts.PollInterval > 0 {
			interval = opts.PollInterval
		}
		includeSteps = opts.IncludeStepOutputs
	}

	var getOpts *GetAgentRunOptions
	if includeSteps {
		getOpts = &GetAgentRunOptions{IncludeStepOutputs: true}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		switch run.Status {
		case "completed", "failed":
			return run, nil
		}

		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-ticker.C:
		}

		run, err = c.GetAgentRun(ctx, run.RunId, getOpts)
		if err != nil {
			return nil, err
		}
	}
}

// ── File Uploads ────────────────────────────────────────────────────────────

// UploadFileRequest describes a file upload.
type UploadFileRequest struct {
	// File is the raw file content.
	File []byte
	// FileName is the name of the file (used for Content-Disposition and MIME inference).
	FileName string
	// MimeType is optional. If omitted, the SDK tries to infer it from FileName.
	MimeType string
	// Title is an optional human-readable title for the upload.
	Title string
	// Metadata is optional key-value metadata to attach to this upload.
	Metadata map[string]any
}

// doUpload performs a multipart file upload to the given API path.
// Returns the raw JSON response body on success.
func (c *Client) doUpload(ctx context.Context, apiPath string, req UploadFileRequest) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(req.File) == 0 {
		return nil, &ConfigurationError{Message: "upload requires non-empty file bytes"}
	}
	if strings.TrimSpace(req.FileName) == "" {
		return nil, &ConfigurationError{Message: "upload requires FileName"}
	}
	mimeType := strings.TrimSpace(req.MimeType)
	if mimeType == "" {
		mimeType = strings.TrimSpace(mime.TypeByExtension(filepath.Ext(req.FileName)))
	}

	reqURL := c.buildURL(apiPath, nil)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if req.Title != "" {
		_ = w.WriteField("title", req.Title)
	}
	if len(req.Metadata) > 0 {
		b, err := json.Marshal(req.Metadata)
		if err != nil {
			_ = w.Close()
			return nil, err
		}
		_ = w.WriteField("metadata", string(b))
	}
	var (
		fw  io.Writer
		err error
	)
	if mimeType != "" {
		fileName := strings.ReplaceAll(req.FileName, "\"", "")
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName))
		h.Set("Content-Type", mimeType)
		fw, err = w.CreatePart(h)
	} else {
		fw, err = w.CreateFormFile("file", req.FileName)
	}
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
	for k, v := range c.defaultHeaders {
		httpReq.Header.Set(k, v)
	}
	if err := c.applyAuth(ctx, httpReq); err != nil {
		return nil, err
	}
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
			if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil && ve.Detail != nil {
				return nil, &APIValidationError{APIStatusError: statusErr, ValidationError: &ve}
			}
			return nil, &APIValidationError{APIStatusError: statusErr}
		}
		return nil, &statusErr
	}
	return raw, nil
}

// UploadFileToSource uploads a file to a source connection.
func (c *Client) UploadFileToSource(ctx context.Context, sourceConnectionID string, req UploadFileRequest) (*FileUploadResponse, error) {
	raw, err := c.doUpload(ctx, fmt.Sprintf("/sources/%s/upload", url.PathEscape(sourceConnectionID)), req)
	if err != nil {
		return nil, err
	}
	var out FileUploadResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// UploadInlineTextToSource submits inline text content to a source.
func (c *Client) UploadInlineTextToSource(ctx context.Context, sourceConnectionID string, body InlineTextUploadRequest) (*FileUploadResponse, error) {
	var out FileUploadResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s", url.PathEscape(sourceConnectionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadFileToContent replaces the file backing an existing content version.
func (c *Client) UploadFileToContent(ctx context.Context, contentVersionID string, req UploadFileRequest) (*ContentFileUploadResponse, error) {
	if strings.TrimSpace(contentVersionID) == "" {
		return nil, &ConfigurationError{Message: "contentVersionID must not be blank"}
	}
	raw, err := c.doUpload(ctx, fmt.Sprintf("/contents/%s/upload", url.PathEscape(contentVersionID)), req)
	if err != nil {
		return nil, err
	}
	var out ContentFileUploadResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// ── Knowledge Bases ─────────────────────────────────────────────────────────

// ListKnowledgeBases lists knowledge bases.
func (c *Client) ListKnowledgeBases(ctx context.Context, opts SortableListOptions) (*KnowledgeBaseListResponse, error) {
	var out KnowledgeBaseListResponse
	if err := c.Do(ctx, http.MethodGet, "/knowledge_bases", sortableListQuery(opts), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateKnowledgeBase creates a new knowledge base.
func (c *Client) CreateKnowledgeBase(ctx context.Context, body CreateKnowledgeBaseBody) (*KnowledgeBaseResponse, error) {
	var out KnowledgeBaseResponse
	if err := c.Do(ctx, http.MethodPost, "/knowledge_bases", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetKnowledgeBase retrieves a knowledge base by ID.
func (c *Client) GetKnowledgeBase(ctx context.Context, knowledgeBaseID string) (*KnowledgeBaseResponse, error) {
	var out KnowledgeBaseResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/knowledge_bases/%s", url.PathEscape(knowledgeBaseID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateKnowledgeBase updates a knowledge base.
func (c *Client) UpdateKnowledgeBase(ctx context.Context, knowledgeBaseID string, body UpdateKnowledgeBaseBody) (*KnowledgeBaseResponse, error) {
	var out KnowledgeBaseResponse
	if err := c.Do(ctx, http.MethodPut, fmt.Sprintf("/knowledge_bases/%s", url.PathEscape(knowledgeBaseID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteKnowledgeBase deletes a knowledge base.
func (c *Client) DeleteKnowledgeBase(ctx context.Context, knowledgeBaseID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/knowledge_bases/%s", url.PathEscape(knowledgeBaseID)), nil, nil, nil, nil)
}

// ── Memory Banks ────────────────────────────────────────────────────────────

// ListMemoryBanks lists memory banks.
func (c *Client) ListMemoryBanks(ctx context.Context, opts SortableListOptions) (*MemoryBankListResponse, error) {
	var out MemoryBankListResponse
	if err := c.Do(ctx, http.MethodGet, "/memory_banks", sortableListQuery(opts), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateMemoryBank creates a new memory bank.
func (c *Client) CreateMemoryBank(ctx context.Context, body CreateMemoryBankBody) (*MemoryBankResponse, error) {
	var out MemoryBankResponse
	if err := c.Do(ctx, http.MethodPost, "/memory_banks", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMemoryBank retrieves a memory bank by ID.
func (c *Client) GetMemoryBank(ctx context.Context, memoryBankID string) (*MemoryBankResponse, error) {
	var out MemoryBankResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/memory_banks/%s", url.PathEscape(memoryBankID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateMemoryBank updates a memory bank.
func (c *Client) UpdateMemoryBank(ctx context.Context, memoryBankID string, body UpdateMemoryBankBody) (*MemoryBankResponse, error) {
	var out MemoryBankResponse
	if err := c.Do(ctx, http.MethodPut, fmt.Sprintf("/memory_banks/%s", url.PathEscape(memoryBankID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteMemoryBank deletes a memory bank.
func (c *Client) DeleteMemoryBank(ctx context.Context, memoryBankID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/memory_banks/%s", url.PathEscape(memoryBankID)), nil, nil, nil, nil)
}

// GetAgentsUsingMemoryBank lists agents that use a memory bank.
func (c *Client) GetAgentsUsingMemoryBank(ctx context.Context, memoryBankID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/memory_banks/%s/agents", url.PathEscape(memoryBankID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMemoryBankStats retrieves statistics for a memory bank.
func (c *Client) GetMemoryBankStats(ctx context.Context, memoryBankID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/memory_banks/%s/stats", url.PathEscape(memoryBankID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CompactMemoryBank triggers compaction for a memory bank.
func (c *Client) CompactMemoryBank(ctx context.Context, memoryBankID string) error {
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/memory_banks/%s/compact", url.PathEscape(memoryBankID)), nil, nil, nil, nil)
}

// DeleteMemoryBankSource deletes the source associated with a memory bank.
func (c *Client) DeleteMemoryBankSource(ctx context.Context, memoryBankID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/memory_banks/%s/source", url.PathEscape(memoryBankID)), nil, nil, nil, nil)
}

// TestMemoryBankCompaction tests compaction for a memory bank.
func (c *Client) TestMemoryBankCompaction(ctx context.Context, memoryBankID string, body TestCompactionRequest) (*CompactionTestResponse, error) {
	var out CompactionTestResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/memory_banks/%s/test-compaction", url.PathEscape(memoryBankID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TestCompactionPromptStandalone tests a compaction prompt without a memory bank.
func (c *Client) TestCompactionPromptStandalone(ctx context.Context, body StandaloneTestCompactionRequest) (*CompactionTestResponse, error) {
	var out CompactionTestResponse
	if err := c.Do(ctx, http.MethodPost, "/memory_banks/test-compaction", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMemoryBankTemplates lists available memory bank templates.
func (c *Client) ListMemoryBankTemplates(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/memory_banks/templates", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Memory Bank AI Assistant ────────────────────────────────────────────────

// GenerateMemoryBankConfig uses the AI assistant to generate memory bank configuration.
func (c *Client) GenerateMemoryBankConfig(ctx context.Context, body MemoryBankAiAssistantRequest) (*MemoryBankAiAssistantResponse, error) {
	var out MemoryBankAiAssistantResponse
	if err := c.Do(ctx, http.MethodPost, "/memory_banks/ai-assistant", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMemoryBankAiLastConversation retrieves the last AI assistant conversation for memory banks.
func (c *Client) GetMemoryBankAiLastConversation(ctx context.Context) (*MemoryBankLastConversationResponse, error) {
	var out MemoryBankLastConversationResponse
	if err := c.Do(ctx, http.MethodGet, "/memory_banks/ai-assistant/last-conversation", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptMemoryBankAiSuggestion accepts an AI-generated memory bank suggestion.
func (c *Client) AcceptMemoryBankAiSuggestion(ctx context.Context, conversationID string, body MemoryBankAcceptRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPatch, fmt.Sprintf("/memory_banks/ai-assistant/%s", url.PathEscape(conversationID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Sources ─────────────────────────────────────────────────────────────────

// ListSourcesOptions extends SortableListOptions with source-specific filters.
type ListSourcesOptions struct {
	SortableListOptions
	// AccountID filters sources by account.
	AccountID string
}

// ListSources lists sources.
func (c *Client) ListSources(ctx context.Context, opts ListSourcesOptions) (*SourceListResponse, error) {
	q := sortableListQuery(opts.SortableListOptions)
	if opts.AccountID != "" {
		q["account_id"] = opts.AccountID
	}
	var out SourceListResponse
	if err := c.Do(ctx, http.MethodGet, "/sources/", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSource creates a new source.
func (c *Client) CreateSource(ctx context.Context, body CreateSourceBody) (*SourceResponse, error) {
	var out SourceResponse
	if err := c.Do(ctx, http.MethodPost, "/sources", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSource retrieves a source by ID.
func (c *Client) GetSource(ctx context.Context, sourceID string) (*SourceResponse, error) {
	var out SourceResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s", url.PathEscape(sourceID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSource updates a source.
func (c *Client) UpdateSource(ctx context.Context, sourceID string, body UpdateSourceBody) (*SourceResponse, error) {
	var out SourceResponse
	if err := c.Do(ctx, http.MethodPut, fmt.Sprintf("/sources/%s", url.PathEscape(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSource deletes a source.
func (c *Client) DeleteSource(ctx context.Context, sourceID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/sources/%s", url.PathEscape(sourceID)), nil, nil, nil, nil)
}

// ── Source Exports ──────────────────────────────────────────────────────────

// ListSourceExports lists exports for a source.
func (c *Client) ListSourceExports(ctx context.Context, sourceID string, opts ListOptions) (*ExportListResponse, error) {
	var out ExportListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/exports", url.PathEscape(sourceID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSourceExport creates a new export for a source.
func (c *Client) CreateSourceExport(ctx context.Context, sourceID string, body CreateExportRequest) (*ExportResponse, error) {
	var out ExportResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/exports", url.PathEscape(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSourceExport retrieves a source export by ID.
func (c *Client) GetSourceExport(ctx context.Context, sourceID, exportID string) (*ExportResponse, error) {
	var out ExportResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/exports/%s", url.PathEscape(sourceID), url.PathEscape(exportID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelSourceExport cancels a source export.
func (c *Client) CancelSourceExport(ctx context.Context, sourceID, exportID string) (*ExportResponse, error) {
	var out ExportResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/exports/%s/cancel", url.PathEscape(sourceID), url.PathEscape(exportID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSourceExport deletes a source export.
func (c *Client) DeleteSourceExport(ctx context.Context, sourceID, exportID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/sources/%s/exports/%s", url.PathEscape(sourceID), url.PathEscape(exportID)), nil, nil, nil, nil)
}

// DownloadSourceExport downloads a source export. Returns the raw HTTP response
// so the caller can stream the body. The caller must close the response body.
func (c *Client) DownloadSourceExport(ctx context.Context, sourceID, exportID string) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqURL := c.buildURL(fmt.Sprintf("/sources/%s/exports/%s/download", url.PathEscape(sourceID), url.PathEscape(exportID)), nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.defaultHeaders {
		req.Header.Set(k, v)
	}
	if err := c.applyAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		text := strings.TrimSpace(string(raw))
		return nil, &APIStatusError{StatusCode: resp.StatusCode, Method: http.MethodGet, URL: reqURL.String(), ResponseText: text}
	}
	return resp, nil
}

// EstimateSourceExport estimates the cost/size of a source export.
func (c *Client) EstimateSourceExport(ctx context.Context, sourceID string, body EstimateExportRequest) (*EstimateExportResponse, error) {
	var out EstimateExportResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/exports/estimate", url.PathEscape(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Source Embedding Migrations ─────────────────────────────────────────────

// GetSourceEmbeddingMigration retrieves the embedding migration status for a source.
func (c *Client) GetSourceEmbeddingMigration(ctx context.Context, sourceID string) (*SourceEmbeddingMigrationResponse, error) {
	var out SourceEmbeddingMigrationResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/embedding-migration", url.PathEscape(sourceID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartSourceEmbeddingMigration starts an embedding migration for a source.
func (c *Client) StartSourceEmbeddingMigration(ctx context.Context, sourceID string, body StartSourceEmbeddingMigrationRequest) (*SourceEmbeddingMigrationResponse, error) {
	var out SourceEmbeddingMigrationResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/embedding-migration", url.PathEscape(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelSourceEmbeddingMigration cancels an in-progress embedding migration.
func (c *Client) CancelSourceEmbeddingMigration(ctx context.Context, sourceID string) (*SourceEmbeddingMigrationResponse, error) {
	var out SourceEmbeddingMigrationResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/embedding-migration/cancel", url.PathEscape(sourceID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Content ─────────────────────────────────────────────────────────────────

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
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/contents/%s", url.PathEscape(contentVersionID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceContentWithInlineText replaces content with inline text.
func (c *Client) ReplaceContentWithInlineText(ctx context.Context, contentVersionID string, body InlineTextReplaceRequest) (*ContentFileUploadResponse, error) {
	var out ContentFileUploadResponse
	if err := c.Do(ctx, http.MethodPut, fmt.Sprintf("/contents/%s", url.PathEscape(contentVersionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteContent deletes a content version.
func (c *Client) DeleteContent(ctx context.Context, contentVersionID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/contents/%s", url.PathEscape(contentVersionID)), nil, nil, nil, nil)
}

// ListContentEmbeddings lists embeddings for a content version.
func (c *Client) ListContentEmbeddings(ctx context.Context, contentVersionID string, opts ListOptions) (*ContentEmbeddingsListResponse, error) {
	var out ContentEmbeddingsListResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/contents/%s/embeddings", url.PathEscape(contentVersionID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Solutions ───────────────────────────────────────────────────────────────

// ListSolutions lists solutions.
func (c *Client) ListSolutions(ctx context.Context, opts SortableListOptions) (*SolutionListResponse, error) {
	var out SolutionListResponse
	if err := c.Do(ctx, http.MethodGet, "/solutions", sortableListQuery(opts), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSolution creates a new solution.
func (c *Client) CreateSolution(ctx context.Context, body CreateSolutionRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodPost, "/solutions", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSolution retrieves a solution by ID.
func (c *Client) GetSolution(ctx context.Context, solutionID string) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/solutions/%s", url.PathEscape(solutionID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSolution updates a solution.
func (c *Client) UpdateSolution(ctx context.Context, solutionID string, body UpdateSolutionRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodPatch, fmt.Sprintf("/solutions/%s", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSolution deletes a solution.
func (c *Client) DeleteSolution(ctx context.Context, solutionID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s", url.PathEscape(solutionID)), nil, nil, nil, nil)
}

// LinkAgentsToSolution links agents to a solution.
func (c *Client) LinkAgentsToSolution(ctx context.Context, solutionID string, body LinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/agents", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnlinkAgentsFromSolution unlinks agents from a solution.
func (c *Client) UnlinkAgentsFromSolution(ctx context.Context, solutionID string, body UnlinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s/agents", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LinkKnowledgeBasesToSolution links knowledge bases to a solution.
func (c *Client) LinkKnowledgeBasesToSolution(ctx context.Context, solutionID string, body LinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/knowledge-bases", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnlinkKnowledgeBasesFromSolution unlinks knowledge bases from a solution.
func (c *Client) UnlinkKnowledgeBasesFromSolution(ctx context.Context, solutionID string, body UnlinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s/knowledge-bases", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LinkSourceConnectionsToSolution links source connections to a solution.
func (c *Client) LinkSourceConnectionsToSolution(ctx context.Context, solutionID string, body LinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/source-connections", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnlinkSourceConnectionsFromSolution unlinks source connections from a solution.
func (c *Client) UnlinkSourceConnectionsFromSolution(ctx context.Context, solutionID string, body UnlinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.Do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s/source-connections", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Solution Conversations ──────────────────────────────────────────────────

// ListSolutionConversations lists conversations for a solution.
func (c *Client) ListSolutionConversations(ctx context.Context, solutionID string) ([]SolutionConversationResponse, error) {
	var out []SolutionConversationResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/solutions/%s/conversations", url.PathEscape(solutionID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddSolutionConversationTurn adds a conversation turn to a solution.
func (c *Client) AddSolutionConversationTurn(ctx context.Context, solutionID string, body AddConversationTurnRequest) (*SolutionConversationResponse, error) {
	var out SolutionConversationResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/conversations", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkSolutionConversationTurn marks a conversation turn (e.g. as accepted/rejected).
func (c *Client) MarkSolutionConversationTurn(ctx context.Context, solutionID, conversationID string, body MarkConversationTurnRequest) error {
	return c.Do(ctx, http.MethodPatch, fmt.Sprintf("/solutions/%s/conversations/%s", url.PathEscape(solutionID), url.PathEscape(conversationID)), nil, body, nil, nil)
}

// ── Solution AI Assistant ───────────────────────────────────────────────────

// GenerateSolutionAiPlan uses the AI assistant to generate a plan for a solution.
func (c *Client) GenerateSolutionAiPlan(ctx context.Context, solutionID string, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/generate", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateSolutionAiKnowledgeBase uses the AI assistant to generate a knowledge base plan for a solution.
func (c *Client) GenerateSolutionAiKnowledgeBase(ctx context.Context, solutionID string, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/knowledge-base", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateSolutionAiSource uses the AI assistant to generate a source plan for a solution.
func (c *Client) GenerateSolutionAiSource(ctx context.Context, solutionID string, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/source", url.PathEscape(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptSolutionAiPlan accepts an AI-generated solution plan.
func (c *Client) AcceptSolutionAiPlan(ctx context.Context, solutionID, conversationID string, body AiAssistantAcceptRequest) (*AiAssistantAcceptResponse, error) {
	var out AiAssistantAcceptResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/%s/accept", url.PathEscape(solutionID), url.PathEscape(conversationID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeclineSolutionAiPlan declines an AI-generated solution plan.
func (c *Client) DeclineSolutionAiPlan(ctx context.Context, solutionID, conversationID string) error {
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/%s/decline", url.PathEscape(solutionID), url.PathEscape(conversationID)), nil, nil, nil, nil)
}

// ── Governance AI Assistant ─────────────────────────────────────────────────

// GenerateGovernanceAiPlan uses the governance AI assistant to generate a plan.
func (c *Client) GenerateGovernanceAiPlan(ctx context.Context, body GovernanceAiAssistantRequest) (*GovernanceAiAssistantResponse, error) {
	var out GovernanceAiAssistantResponse
	if err := c.Do(ctx, http.MethodPost, "/governance/ai-assistant", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListGovernanceAiConversations lists governance AI assistant conversations.
func (c *Client) ListGovernanceAiConversations(ctx context.Context) ([]GovernanceConversationResponse, error) {
	var out []GovernanceConversationResponse
	if err := c.Do(ctx, http.MethodGet, "/governance/ai-assistant/conversations", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AcceptGovernanceAiPlan accepts a governance AI plan.
func (c *Client) AcceptGovernanceAiPlan(ctx context.Context, conversationID string) (*GovernanceAiAcceptResponse, error) {
	var out GovernanceAiAcceptResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/governance/ai-assistant/%s/accept", url.PathEscape(conversationID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeclineGovernanceAiPlan declines a governance AI plan.
func (c *Client) DeclineGovernanceAiPlan(ctx context.Context, conversationID string) error {
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/governance/ai-assistant/%s/decline", url.PathEscape(conversationID)), nil, nil, nil, nil)
}

// ── Alerts ──────────────────────────────────────────────────────────────────

// ListAlertsOptions controls optional query parameters for ListAlerts.
type ListAlertsOptions struct {
	ListOptions
	// Status filters alerts by status (e.g. "active", "resolved").
	Status string
	// Severity filters alerts by severity.
	Severity string
}

// ListAlerts lists alerts.
func (c *Client) ListAlerts(ctx context.Context, opts ListAlertsOptions) (json.RawMessage, error) {
	q := listQuery(opts.Page, opts.Limit)
	if opts.Status != "" {
		q["status"] = opts.Status
	}
	if opts.Severity != "" {
		q["severity"] = opts.Severity
	}
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/alerts", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAlert retrieves an alert by ID.
func (c *Client) GetAlert(ctx context.Context, alertID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/alerts/%s", url.PathEscape(alertID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ChangeAlertStatus changes the status of an alert.
func (c *Client) ChangeAlertStatus(ctx context.Context, alertID string, body ChangeStatusRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/status", url.PathEscape(alertID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddAlertComment adds a comment to an alert.
func (c *Client) AddAlertComment(ctx context.Context, alertID string, body AddCommentRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/comments", url.PathEscape(alertID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SubscribeToAlert subscribes to an alert.
func (c *Client) SubscribeToAlert(ctx context.Context, alertID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/subscribe", url.PathEscape(alertID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnsubscribeFromAlert unsubscribes from an alert.
func (c *Client) UnsubscribeFromAlert(ctx context.Context, alertID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/unsubscribe", url.PathEscape(alertID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Alert Configs ───────────────────────────────────────────────────────────

// ListAlertConfigs lists alert configurations.
func (c *Client) ListAlertConfigs(ctx context.Context, opts ListOptions) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/alerts/configs", listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateAlertConfig creates a new alert configuration.
func (c *Client) CreateAlertConfig(ctx context.Context, body CreateAlertConfigRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPost, "/alerts/configs", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAlertConfig retrieves an alert configuration by ID.
func (c *Client) GetAlertConfig(ctx context.Context, configID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/alerts/configs/%s", url.PathEscape(configID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAlertConfig updates an alert configuration.
func (c *Client) UpdateAlertConfig(ctx context.Context, configID string, body UpdateAlertConfigRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPatch, fmt.Sprintf("/alerts/configs/%s", url.PathEscape(configID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteAlertConfig deletes an alert configuration.
func (c *Client) DeleteAlertConfig(ctx context.Context, configID string) error {
	return c.Do(ctx, http.MethodDelete, fmt.Sprintf("/alerts/configs/%s", url.PathEscape(configID)), nil, nil, nil, nil)
}

// ── Alert Preferences ───────────────────────────────────────────────────────

// ListOrganizationAlertPreferences lists organization alert preferences.
func (c *Client) ListOrganizationAlertPreferences(ctx context.Context) (*OrganizationAlertPreferenceListResponse, error) {
	var out OrganizationAlertPreferenceListResponse
	if err := c.Do(ctx, http.MethodGet, "/alerts/organization-preferences/list", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateOrganizationAlertPreference updates an organization alert preference.
func (c *Client) UpdateOrganizationAlertPreference(ctx context.Context, organizationID, alertType string, body UpdateOrganizationAlertPreferenceRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPatch, fmt.Sprintf("/alerts/organization-preferences/%s/%s", url.PathEscape(organizationID), url.PathEscape(alertType)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Models & Alerts ─────────────────────────────────────────────────────────

// ListModelAlerts lists model alerts.
func (c *Client) ListModelAlerts(ctx context.Context, opts ListOptions) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/models/alerts", listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkAllModelAlertsRead marks all model alerts as read.
func (c *Client) MarkAllModelAlertsRead(ctx context.Context) error {
	return c.Do(ctx, http.MethodPost, "/models/alerts/mark-all-read", nil, nil, nil, nil)
}

// GetUnreadModelAlertCount retrieves the count of unread model alerts.
func (c *Client) GetUnreadModelAlertCount(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/models/alerts/unread-count", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkModelAlertRead marks a single model alert as read.
func (c *Client) MarkModelAlertRead(ctx context.Context, alertID string) error {
	return c.Do(ctx, http.MethodPatch, fmt.Sprintf("/models/alerts/%s/read", url.PathEscape(alertID)), nil, nil, nil, nil)
}

// GetModelRecommendations retrieves recommendations for a model.
func (c *Client) GetModelRecommendations(ctx context.Context, modelID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/models/%s/recommendations", url.PathEscape(modelID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListModelsOptions controls query parameters for the ListModels endpoint.
type ListModelsOptions struct {
	// Provider filters by provider name (e.g. "anthropic", "openai").
	Provider string
	// SupportsToolUse filters to models with tool calling support.
	SupportsToolUse *bool
	// SupportsThinking filters to models with extended thinking support.
	SupportsThinking *bool
}

// ListModels lists all enabled LLM models grouped by provider.
func (c *Client) ListModels(ctx context.Context, opts ListModelsOptions) ([]ProviderGroupResponse, error) {
	q := map[string]string{}
	if opts.Provider != "" {
		q["provider"] = opts.Provider
	}
	if opts.SupportsToolUse != nil {
		q["supports_tool_use"] = fmt.Sprintf("%t", *opts.SupportsToolUse)
	}
	if opts.SupportsThinking != nil {
		q["supports_thinking"] = fmt.Sprintf("%t", *opts.SupportsThinking)
	}
	var out []ProviderGroupResponse
	if err := c.Do(ctx, http.MethodGet, "/models", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetModel retrieves full details for a specific model.
func (c *Client) GetModel(ctx context.Context, modelID string) (*PromptModelResponse, error) {
	var out PromptModelResponse
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/models/%s/details", url.PathEscape(modelID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Model Playground Experiments ────────────────────────────────────────────

// ListExperimentsOptions controls query parameters for the ListExperiments endpoint.
type ListExperimentsOptions struct {
	// Days is the look-back window in days (1-730, default 30).
	Days int
	// StartDate filters by start date (YYYY-MM-DD).
	StartDate string
	// EndDate filters by end date (YYYY-MM-DD).
	EndDate string
	// Limit is the maximum number of results.
	Limit int
	// Offset is the pagination offset.
	Offset int
}

// ListExperiments lists model playground experiments.
func (c *Client) ListExperiments(ctx context.Context, opts ListExperimentsOptions) (json.RawMessage, error) {
	q := map[string]string{}
	if opts.Days > 0 {
		q["days"] = fmt.Sprintf("%d", opts.Days)
	}
	if opts.StartDate != "" {
		q["start_date"] = opts.StartDate
	}
	if opts.EndDate != "" {
		q["end_date"] = opts.EndDate
	}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	if opts.Offset > 0 {
		q["offset"] = fmt.Sprintf("%d", opts.Offset)
	}
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/models/playground/experiments", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateExperiment creates a model playground experiment.
func (c *Client) CreateExperiment(ctx context.Context, body PlaygroundCreateRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPost, "/models/playground/experiments", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetExperiment retrieves a model playground experiment by ID.
func (c *Client) GetExperiment(ctx context.Context, experimentID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/models/playground/experiments/%s", url.PathEscape(experimentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CancelExperiment cancels a running model playground experiment.
func (c *Client) CancelExperiment(ctx context.Context, experimentID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/models/playground/experiments/%s/cancel", url.PathEscape(experimentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── General Search ──────────────────────────────────────────────────────────

// SearchOptions controls query parameters for the general Search endpoint.
type SearchOptions struct {
	// Query is the search query string.
	Query string
	// Limit is the maximum number of results to return.
	Limit int
	// EntityType filters results by type (e.g. "agent", "source", "knowledge_base").
	EntityType string
}

// Search performs a general search across resources.
func (c *Client) Search(ctx context.Context, opts SearchOptions) (json.RawMessage, error) {
	q := map[string]string{}
	if opts.Query != "" {
		q["query"] = opts.Query
	}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	if opts.EntityType != "" {
		q["entity_type"] = opts.EntityType
	}
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/search", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Top-Level AI Assistant ──────────────────────────────────────────────────

// SubmitAiFeedback submits feedback to the AI assistant.
func (c *Client) SubmitAiFeedback(ctx context.Context, body AiAssistantFeedbackRequest) (*AiAssistantFeedbackResponse, error) {
	var out AiAssistantFeedbackResponse
	if err := c.Do(ctx, http.MethodPost, "/ai-assistant/feedback", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantKnowledgeBase generates a knowledge base plan via the top-level AI assistant.
func (c *Client) AiAssistantKnowledgeBase(ctx context.Context, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.Do(ctx, http.MethodPost, "/ai-assistant/knowledge-base", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantSource generates a source plan via the top-level AI assistant.
func (c *Client) AiAssistantSource(ctx context.Context, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.Do(ctx, http.MethodPost, "/ai-assistant/source", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantSolution generates a solution plan via the top-level AI assistant.
func (c *Client) AiAssistantSolution(ctx context.Context, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.Do(ctx, http.MethodPost, "/ai-assistant/solution", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantMemoryBank generates a memory bank plan via the top-level AI assistant.
func (c *Client) AiAssistantMemoryBank(ctx context.Context, body MemoryBankAiAssistantRequest) (*MemoryBankAiAssistantResponse, error) {
	var out MemoryBankAiAssistantResponse
	if err := c.Do(ctx, http.MethodPost, "/ai-assistant/memory-bank", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAiAssistantMemoryBankHistory retrieves the last AI assistant memory bank conversation.
func (c *Client) GetAiAssistantMemoryBankHistory(ctx context.Context) (*MemoryBankLastConversationResponse, error) {
	var out MemoryBankLastConversationResponse
	if err := c.Do(ctx, http.MethodGet, "/ai-assistant/memory-bank/last-conversation", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptAiAssistantPlan accepts a top-level AI assistant plan.
func (c *Client) AcceptAiAssistantPlan(ctx context.Context, conversationID string, body AiAssistantAcceptRequest) (*AiAssistantAcceptResponse, error) {
	var out AiAssistantAcceptResponse
	if err := c.Do(ctx, http.MethodPost, fmt.Sprintf("/ai-assistant/%s/accept", url.PathEscape(conversationID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeclineAiAssistantPlan declines a top-level AI assistant plan.
func (c *Client) DeclineAiAssistantPlan(ctx context.Context, conversationID string) error {
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/ai-assistant/%s/decline", url.PathEscape(conversationID)), nil, nil, nil, nil)
}

// AcceptAiMemoryBankSuggestion accepts a top-level AI memory bank suggestion.
func (c *Client) AcceptAiMemoryBankSuggestion(ctx context.Context, conversationID string, body MemoryBankAcceptRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.Do(ctx, http.MethodPatch, fmt.Sprintf("/ai-assistant/memory-bank/%s", url.PathEscape(conversationID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Internal helpers ────────────────────────────────────────────────────────

// buildURL constructs a full request URL by joining apiPath to the base URL
// and appending query parameters.
func (c *Client) buildURL(apiPath string, query map[string]string) *url.URL {
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
