package seclai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"sort"
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

	// DefaultHeaders are HTTP headers applied to every request. A Seclai-Version
	// here passes the same guard as APIVersion, and an empty one is rejected by
	// [NewClient] rather than replacing APIVersion with no version.
	DefaultHeaders map[string]string

	// AllowUnknownAPIVersion permits an APIVersion this release was not built
	// against.
	//
	// Off by default. A newer API version can change response shapes, and this
	// client would decode them incorrectly rather than reject them — a
	// request-side mistake answers 422, but a reshaped response just
	// mis-decodes, silently. Prefer upgrading the module.
	AllowUnknownAPIVersion bool

	// EscapedDoPaths makes [Client.Do] send apiPath as given, already escaped:
	// a %XX in it is kept as an escape. Off by default, which keeps the
	// released behaviour: apiPath is taken as unescaped and escaped as a whole,
	// so a % in it is sent as %25 and an ID escaped with url.PathEscape is
	// escaped twice. It will become the default in the next major version.
	// The client's own methods always send their IDs escaped once.
	EscapedDoPaths bool

	// APIVersion is a dated API version (YYYY-MM-DD) sent as the Seclai-Version
	// header, opting this client into backward-incompatible API changes released
	// on or before that date.
	//
	// Left empty the header is omitted and the account's pinned baseline applies,
	// so responses keep their current shapes and upgrading the SDK alone never
	// changes the wire contract. Pin the account instead with
	// [Client.UpdateAPIVersion], and read what a request resolves to with
	// [Client.GetAPIVersion].
	//
	// From 2026-07-27 the API rejects undeclared query parameters with a 422
	// rather than ignoring them, and list endpoints return the canonical
	// {data, pagination} envelope.
	APIVersion string

	// HTTPClient is used for requests. Defaults to a client with a 30s timeout.
	// Its Timeout does not apply to the streaming methods, which ctx bounds.
	HTTPClient *http.Client
}

// Client is the Seclai Go SDK client.
type Client struct {
	auth           *authState
	baseURL        *url.URL
	defaultHeaders map[string]string
	httpClient     *http.Client

	allowUnknownAPIVersion bool
	escapedDoPaths         bool

	generated *generated.ClientWithResponses
}

// checkAPIVersion is the guard every Seclai-Version passes before it is sent.
// An empty value is refused even when unknown versions are allowed: it is not
// a version, and sending it silently drops the opt-in.
func checkAPIVersion(version, source string, allowUnknown bool) error {
	if strings.TrimSpace(version) == "" {
		return &ConfigurationError{Message: fmt.Sprintf(
			"empty Seclai-Version (via %s): an empty header would replace the "+
				"configured API version with none. Omit the header instead.", source)}
	}
	if !allowUnknown && !isKnownAPIVersion(version) {
		return &ConfigurationError{Message: fmt.Sprintf(
			"unknown API version %q (via %s): this release was built against %s. A "+
				"newer API version can change response shapes, which this client would "+
				"decode incorrectly rather than reject. Upgrade the module, or set "+
				"Options.AllowUnknownAPIVersion to proceed anyway.",
			version, source, strings.Join(KnownAPIVersions, ", "))}
	}
	return nil
}

// guardedDoer applies the version guard to requests issued through
// [Client.Generated], after every request editor has run.
type guardedDoer struct {
	inner        *http.Client
	allowUnknown bool
}

func (d guardedDoer) Do(req *http.Request) (*http.Response, error) {
	for k, values := range req.Header {
		if !strings.EqualFold(k, "Seclai-Version") {
			continue
		}
		for _, v := range values {
			if err := checkAPIVersion(v, "a Generated() request header", d.allowUnknown); err != nil {
				return nil, err
			}
		}
	}
	return d.inner.Do(req)
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

	defHeaders := make(map[string]string, len(opts.DefaultHeaders)+1)
	// Set first so an explicit DefaultHeaders entry still wins. Omitted unless
	// the caller opts in: with no header the account's pinned baseline applies.
	if opts.APIVersion != "" {
		defHeaders["Seclai-Version"] = opts.APIVersion
	}
	// DefaultHeaders may carry several spellings of one header. Sort the keys so
	// the survivor does not depend on Go's randomised map order, and drop any
	// differently-cased duplicate so only one value reaches the wire.
	headerKeys := make([]string, 0, len(opts.DefaultHeaders))
	for k := range opts.DefaultHeaders {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	for _, k := range headerKeys {
		for existing := range defHeaders {
			if strings.EqualFold(existing, k) && existing != k {
				delete(defHeaders, existing)
			}
		}
		defHeaders[k] = opts.DefaultHeaders[k]
	}

	// Validate what the merge produced, not what the options said. Inspecting the
	// options would have to predict which spelling survives — and with a map that
	// is not even deterministic, so the guard could approve one value and the
	// client send another.
	for k, v := range defHeaders {
		if !strings.EqualFold(k, "Seclai-Version") {
			continue
		}
		versionSource := "Options.APIVersion"
		if _, fromDefaults := opts.DefaultHeaders[k]; fromDefaults {
			versionSource = fmt.Sprintf("Options.DefaultHeaders[%q]", k)
		}
		if err := checkAPIVersion(v, versionSource, opts.AllowUnknownAPIVersion); err != nil {
			return nil, err
		}
	}

	client := &Client{
		auth:                   state,
		baseURL:                parsed,
		defaultHeaders:         defHeaders,
		httpClient:             hc,
		allowUnknownAPIVersion: opts.AllowUnknownAPIVersion,
		escapedDoPaths:         opts.EscapedDoPaths,
	}

	gen, err := generated.NewClientWithResponses(parsed.String(),
		generated.WithHTTPClient(guardedDoer{inner: hc, allowUnknown: opts.AllowUnknownAPIVersion}),
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
// It is fully typed and exposes all endpoints directly. It is a raw escape
// hatch: a request editor passed to one of its calls runs after this client's
// own and can replace any header, which is the caller's responsibility. The
// one check that still applies is the version guard — a request whose final
// Seclai-Version is empty, or unknown without Options.AllowUnknownAPIVersion,
// fails with a *ConfigurationError before it is sent. Responses are decoded
// by the generated types, which model only the default shape of each endpoint.
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

// Do makes a low-level request to the Seclai API. apiPath is joined to the
// base URL's path.
//
// By default (the released behaviour) apiPath is taken as unescaped and
// escaped as a whole: pass IDs in it raw ("/agents/" + id). A "%" in it is
// sent as %25, so an ID escaped with url.PathEscape is escaped twice, and an
// ID containing "/" cannot be sent. With [Options.EscapedDoPaths],
// recommended and the default from the next major version, apiPath is sent as
// given: escape IDs with url.PathEscape ("/agents/" + url.PathEscape(id)). A
// %XX is then taken as an escape already made and sent unchanged, a "%" that
// does not start one is sent as %25, and characters that are not allowed in a
// path, such as a space, are escaped.
//
// "." and ".." segments are resolved before sending, and url.PathEscape leaves
// them as they are: "/agents/" + url.PathEscape("..") is sent as "/". Reject an
// ID of "." or "..", or with EscapedDoPaths write its dots as %2E, before
// building apiPath.
//
// Reject a blank ID as well, which Do cannot detect: an empty segment is
// collapsed ("/agents//runs" is sent as "/agents/runs", another endpoint),
// and a trailing one ("/agents/") is a path Do sends as it is.
//
// For JSON responses, out is decoded from JSON when non-nil.
// For non-2xx responses, an *APIStatusError or *APIValidationError is returned.
//
// A Seclai-Version in headers passes the same guard as [Options.APIVersion]:
// an empty value, or one this release does not know without
// Options.AllowUnknownAPIVersion, returns a *ConfigurationError and no request
// is sent.
func (c *Client) Do(ctx context.Context, method, apiPath string, query map[string]string, body any, headers map[string]string, out any) error {
	if !c.escapedDoPaths {
		// As the released Do did, take the base URL's path and apiPath as
		// unescaped and escape them: the base's own escapes are decoded too.
		released := *c
		base := *c.baseURL
		base.RawPath = ""
		released.baseURL = &base
		return released.do(ctx, method, (&url.URL{Path: apiPath}).EscapedPath(), query, body, headers, out)
	}
	return c.do(ctx, method, apiPath, query, body, headers, out)
}

// do is [Client.Do] with Options.EscapedDoPaths on, for the client's own
// methods, whose paths are built with pathSegment and already escaped.
func (c *Client) do(ctx context.Context, method, apiPath string, query map[string]string, body any, headers map[string]string, out any) error {
	return c.doValues(ctx, method, apiPath, queryValues(query), body, headers, out)
}

// doValues is [Client.Do] with a multi-valued query, for repeatable parameters.
func (c *Client) doValues(ctx context.Context, method, apiPath string, query url.Values, body any, headers map[string]string, out any) error {
	raw, _, err := c.doBytes(ctx, method, apiPath, query, body, headers)
	if err != nil || out == nil || len(raw) == 0 {
		return err
	}
	return json.Unmarshal(raw, out)
}

// doBytes issues a request and returns the body of a 2xx response undecoded.
func (c *Client) doBytes(ctx context.Context, method, apiPath string, query url.Values, body any, headers map[string]string) ([]byte, *url.URL, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Sorted so that, of two spellings of one header, the same one always wins
	// and the guard validates the value that is sent.
	headerKeys := make([]string, 0, len(headers))
	for k := range headers {
		if strings.TrimSpace(k) != "" {
			headerKeys = append(headerKeys, k)
		}
	}
	sort.Strings(headerKeys)
	for i := len(headerKeys) - 1; i >= 0; i-- {
		if strings.EqualFold(headerKeys[i], "Seclai-Version") {
			source := fmt.Sprintf("Do headers[%q]", headerKeys[i])
			if err := checkAPIVersion(headers[headerKeys[i]], source, c.allowUnknownAPIVersion); err != nil {
				return nil, nil, err
			}
			break
		}
	}

	reqURL, err := c.buildURLValues(apiPath, query)
	if err != nil {
		return nil, nil, err
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reqBody)
	if err != nil {
		return nil, nil, err
	}

	for k, v := range c.defaultHeaders {
		req.Header.Set(k, v)
	}
	if err := c.applyAuth(ctx, req); err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for _, k := range headerKeys {
		req.Header.Set(k, headers[k])
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, apiError(resp.StatusCode, method, reqURL.String(), raw)
	}
	return raw, reqURL, nil
}

// apiError builds the error for a non-2xx response from its status and body:
// an *APIValidationError for a 422, carrying the decoded validation detail
// when the body has one, and an *APIStatusError otherwise.
func apiError(statusCode int, method, reqURL string, raw []byte) error {
	statusErr := APIStatusError{StatusCode: statusCode, Method: method, URL: reqURL, ResponseText: strings.TrimSpace(string(raw))}
	if statusCode == http.StatusUnprocessableEntity {
		var ve HTTPValidationError
		if len(raw) > 0 && json.Unmarshal(raw, &ve) == nil && ve.Detail != nil {
			return &APIValidationError{APIStatusError: statusErr, ValidationError: &ve}
		}
		return &APIValidationError{APIStatusError: statusErr}
	}
	return &statusErr
}

// ── Identity ──────────────────────────────────────────────────────────────────

// GetMe returns the authenticated user's personal account ID and the
// organizations they belong to.
func (c *Client) GetMe(ctx context.Context) (*MeResponse, error) {
	var out MeResponse
	if err := c.do(ctx, http.MethodGet, "/me", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── API Version ─────────────────────────────────────────────────────────────

// GetAPIVersion reports the API version this request resolved to, and the
// versions available.
//
// Resolution order is the Seclai-Version header, then the account pin, then the
// default — so EffectiveVersion reflects [Options.APIVersion] when it is set.
func (c *Client) GetAPIVersion(ctx context.Context) (*ApiVersionResponse, error) {
	var out ApiVersionResponse
	if err := c.do(ctx, http.MethodGet, "/version", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAPIVersion pins the account to a dated API version, or clears the pin
// when version is nil. Owner/admin only.
//
// The pin applies to later header-less requests; a Seclai-Version header still
// overrides it, so EffectiveVersion in the response describes this request
// rather than the pin just written.
func (c *Client) UpdateAPIVersion(ctx context.Context, version *string) (*ApiVersionResponse, error) {
	var out ApiVersionResponse
	// A literal null clears the pin. The generated struct omits a nil Version,
	// so the null is written explicitly.
	var body any = UpdateApiVersionRequest{Version: version}
	if version == nil {
		body = map[string]any{"version": nil}
	}
	if err := c.do(ctx, http.MethodPut, "/version", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
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

// setQueryValues copies the non-empty values into q. Callers pass the keys as a
// map literal, so every query key an endpoint sends stays written out in its
// method, where sdksync's params audit reads it. A helper that took the values
// positionally would hide the keys from that audit.
func setQueryValues(q, values map[string]string) {
	for k, v := range values {
		if v != "" {
			q[k] = v
		}
	}
}

// queryBool renders a flag that is sent only when set.
func queryBool(b bool) string {
	if b {
		return "true"
	}
	return ""
}

// queryPositiveInt renders a count that is sent only when positive.
func queryPositiveInt(n int) string {
	if n > 0 {
		return fmt.Sprintf("%d", n)
	}
	return ""
}

// ListAgents lists agents.
func (c *Client) ListAgents(ctx context.Context, opts ListOptions) (*AgentListResponse, error) {
	var out AgentListResponse
	if err := c.do(ctx, http.MethodGet, "/agents", listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAgentExamplesOptions controls query parameters for [Client.ListAgentExamples].
type ListAgentExamplesOptions struct {
	ListOptions
	// Query keeps only entries containing every one of these words.
	Query string
	// UseCase keeps only entries for this use case.
	UseCase string
}

// ListAgentExamples lists the agent gallery: example agents to start from,
// searchable by text and filterable by use case. Pass an entry's "key" as
// AgentTemplate on [Client.CreateAgent], with ResourceBindings for the
// resources it needs.
func (c *Client) ListAgentExamples(ctx context.Context, opts ListAgentExamplesOptions) (*AgentExampleListResponse, error) {
	q := listQuery(opts.Page, opts.Limit)
	if opts.Query != "" {
		q["query"] = opts.Query
	}
	if opts.UseCase != "" {
		q["use_case"] = opts.UseCase
	}
	var out AgentExampleListResponse
	if err := c.do(ctx, http.MethodGet, "/agents/examples", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAgent creates a new agent.
func (c *Client) CreateAgent(ctx context.Context, body CreateAgentRequest) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.do(ctx, http.MethodPost, "/agents", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgent retrieves an agent by ID.
func (c *Client) GetAgent(ctx context.Context, agentID string) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s", pathSegment(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAgent updates an agent.
func (c *Client) UpdateAgent(ctx context.Context, agentID string, body UpdateAgentRequest) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s", pathSegment(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgent deletes an agent.
func (c *Client) DeleteAgent(ctx context.Context, agentID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/agents/%s", pathSegment(agentID)), nil, nil, nil, nil)
}

// DisableAgent pauses an agent so it stops firing from every trigger path.
//
// Returns an error wrapping HTTP 409 when other live agents still call this one
// via a call_agent step; use [Client.GetAgentCallers] to list them.
func (c *Client) DisableAgent(ctx context.Context, agentID string) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/disable", pathSegment(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EnableAgent resumes a paused agent, whether it was paused manually or by
// the inbound-email overload safeguard.
func (c *Client) EnableAgent(ctx context.Context, agentID string) (*AgentSummaryResponse, error) {
	var out AgentSummaryResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/enable", pathSegment(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentCallers lists the live agents that call this agent via a call_agent
// step. They must be disabled before this agent can be paused.
func (c *Client) GetAgentCallers(ctx context.Context, agentID string) ([]AgentCallerApiResponse, error) {
	var out []AgentCallerApiResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/callers", pathSegment(agentID)), nil, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Agent Export / Import ───────────────────────────────────────────────────

// ExportAgent exports an agent definition as a portable JSON snapshot.
func (c *Client) ExportAgent(ctx context.Context, agentID string, download bool) (*AgentExportResponse, error) {
	query := map[string]string{"download": fmt.Sprintf("%t", download)}
	var out AgentExportResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/export", pathSegment(agentID)), query, nil, nil, &out); err != nil {
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
	if err := c.do(ctx, http.MethodPost, "/agents/preview-import", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Definitions ───────────────────────────────────────────────────────

// GetAgentDefinition retrieves the definition (step configuration) for an agent.
func (c *Client) GetAgentDefinition(ctx context.Context, agentID string) (*AgentDefinitionResponse, error) {
	var out AgentDefinitionResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/definition", pathSegment(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAgentDefinition updates the definition for an agent.
func (c *Client) UpdateAgentDefinition(ctx context.Context, agentID string, body UpdateAgentDefinitionRequest) (*AgentDefinitionResponse, error) {
	var out AgentDefinitionResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s/definition", pathSegment(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Runs ──────────────────────────────────────────────────────────────

// RunAgent runs an agent.
func (c *Client) RunAgent(ctx context.Context, agentID string, body AgentRunRequest) (*AgentRunResponse, error) {
	var out AgentRunResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/runs", pathSegment(agentID)), nil, body, nil, &out); err != nil {
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
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/runs", pathSegment(agentID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchAgentRuns searches agent runs with filter criteria.
func (c *Client) SearchAgentRuns(ctx context.Context, body AgentTraceSearchRequest) (*AgentTraceSearchResponse, error) {
	var out AgentTraceSearchResponse
	if err := c.do(ctx, http.MethodPost, "/agents/runs/search", nil, body, nil, &out); err != nil {
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
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/runs/%s", pathSegment(runID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgentRun cancels a run.
//
// Deprecated: this never deleted anything — the endpoint it calls is documented
// as "Cancel an agent run", and the API has no delete-a-run operation. It also
// discards the returned run state. Use [Client.CancelAgentRun] instead.
func (c *Client) DeleteAgentRun(ctx context.Context, runID string) error {
	_, err := c.CancelAgentRun(ctx, runID)
	return err
}

// CancelAgentRun cancels an in-progress agent run and returns its updated state.
//
// Cancellation is DELETE on the run resource — the API exposes no
// POST .../cancel route, and no operation that deletes a run.
// Rejected when the run has already reached a terminal state.
func (c *Client) CancelAgentRun(ctx context.Context, runID string) (*AgentRunResponse, error) {
	var out AgentRunResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/agents/runs/%s", pathSegment(runID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Input Uploads ─────────────────────────────────────────────────────

// UploadAgentInput uploads an input file for a dynamic_input agent's run; any
// other agent rejects it with a 400. Pass the UploadId as the run's
// InputUploadId. Files are limited to 200 MB.
//
// From API version 2026-10-16 a file of any type is accepted and is ready at
// once. Earlier versions accept only the document, image, audio and video
// types the API lists, and write the file's text into the run's input, so poll
// [Client.GetAgentInputUploadStatus] until Status is "ready" before running.
func (c *Client) UploadAgentInput(ctx context.Context, agentID string, req UploadFileRequest) (*UploadAgentInputApiResponse, error) {
	resp, err := c.doUpload(ctx, fmt.Sprintf("/agents/%s/upload-input", pathSegment(agentID)), req)
	if err != nil {
		return nil, err
	}
	var out UploadAgentInputApiResponse
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentInputUploadStatus checks the status of an input upload. From API
// version 2026-10-16 an upload is ready at once and need not be polled.
func (c *Client) GetAgentInputUploadStatus(ctx context.Context, agentID, uploadID string) (*UploadAgentInputApiResponse, error) {
	var out UploadAgentInputApiResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/input-uploads/%s", pathSegment(agentID), pathSegment(uploadID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentAttachmentReferences returns the static attachment-reference contract for
// an agent — what files (if any) its definition expects on a run.
//
// Call this before staging uploads to learn whether the agent accepts files at all
// (RequiresUploads) and which specific filenames, indexes, or glob patterns its
// templates reference. A run-time upload batch that does not satisfy every declared
// selector is rejected with HTTP 400.
func (c *Client) GetAgentAttachmentReferences(ctx context.Context, agentID string) (*AgentAttachmentRefsApiResponse, error) {
	var out AgentAttachmentRefsApiResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/attachment-references", pathSegment(agentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetAcceptedAttachments replaces the input attachments an agent's trigger
// accepts. A run's files are each assigned to one declared attachment before
// any step runs, and steps reference one as {{agent.attachments.<name>}}.
//
// triggerID is the TriggerId that [Client.GetAgent] returns. A nil
// AcceptedAttachments is sent as null and accepts any files, as one attachment
// named "files"; only dynamic_input and email_received triggers take files.
// The API answers 400 when the agent references an attachment the new
// declaration leaves out.
func (c *Client) SetAcceptedAttachments(ctx context.Context, agentID, triggerID string, body SetAcceptedAttachmentsRequest) (*AcceptedAttachmentsResponse, error) {
	var out AcceptedAttachmentsResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s/triggers/%s/accepted-attachments", pathSegment(agentID), pathSegment(triggerID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMetadataKeys returns the run metadata keys an agent's trigger declares,
// which the agent's steps may reference as {{metadata.<key>}}, and the system
// and source keys the trigger's type can declare.
//
// triggerID is the TriggerId that [Client.GetAgent] returns.
func (c *Client) GetMetadataKeys(ctx context.Context, agentID, triggerID string) (*MetadataKeysResponse, error) {
	var out MetadataKeysResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/triggers/%s/metadata-keys", pathSegment(agentID), pathSegment(triggerID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetMetadataKeys replaces the run metadata keys an agent's trigger declares.
// body.MetadataKeys is the whole declaration: a key left out is no longer
// declared. Pass an empty, non-nil slice to clear it; a nil MetadataKeys is
// refused with a [*ConfigurationError] before anything is sent. A
// caller-started run that sends an undeclared custom key, or lacks a required
// one, is refused with HTTP 400.
//
// The API answers 400 when the agent references a key no trigger of its
// branch declares. Warnings on the response lists agents whose call_agent
// mapping no longer fits the new declaration.
func (c *Client) SetMetadataKeys(ctx context.Context, agentID, triggerID string, body SetMetadataKeysRequest) (*MetadataKeysResponse, error) {
	if body.MetadataKeys == nil {
		return nil, &ConfigurationError{Message: "SetMetadataKeys: MetadataKeys must not be nil; pass an empty slice to clear"}
	}
	var out MetadataKeysResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s/triggers/%s/metadata-keys", pathSegment(agentID), pathSegment(triggerID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadAgentRunAttachment downloads a file attachment emitted by a step in an
// agent run. Returns the raw HTTP response so the caller can stream the body. The
// caller must close the response body.
//
// attachmentID is the URL-safe-base64-encoded storage_key of the attachment (as
// surfaced in run output manifests and webhook/email payloads). downloadName is an
// optional filename hint for the download disposition; pass "" to omit it.
func (c *Client) DownloadAgentRunAttachment(ctx context.Context, runID, attachmentID, downloadName string) (*http.Response, error) {
	var query map[string]string
	if downloadName != "" {
		query = map[string]string{"download_name": downloadName}
	}
	return c.doRaw(ctx, http.MethodGet, fmt.Sprintf("/v2/agent-runs/%s/attachments/%s", pathSegment(runID), pathSegment(attachmentID)), query)
}

// doRaw issues a request without a body and returns the 2xx response
// unread, for the download methods; the caller must close its body. A non-2xx
// response is read, closed and returned as an *APIStatusError, a 422 included.
// Keep it that way: v1.7.x shipped that type for download 422s, and callers
// type-assert on it, so returning *APIValidationError here is a breaking change.
func (c *Client) doRaw(ctx context.Context, method, apiPath string, query map[string]string) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqURL, err := c.buildURL(apiPath, query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), nil)
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
		return nil, &APIStatusError{StatusCode: resp.StatusCode, Method: method, URL: reqURL.String(), ResponseText: strings.TrimSpace(string(raw))}
	}
	return resp, nil
}

// ── Agent AI Assistant ──────────────────────────────────────────────────────

// GenerateAgentSteps uses the AI assistant to generate step configurations for an agent.
func (c *Client) GenerateAgentSteps(ctx context.Context, agentID string, body GenerateAgentStepsRequest) (*GenerateAgentStepsResponse, error) {
	var out GenerateAgentStepsResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/ai-assistant/generate-steps", pathSegment(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateStepConfig uses the AI assistant to generate configuration for a single step.
func (c *Client) GenerateStepConfig(ctx context.Context, agentID string, body GenerateStepConfigRequest) (*GenerateStepConfigResponse, error) {
	var out GenerateStepConfigResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/ai-assistant/step-config", pathSegment(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentAiConversationHistory retrieves AI assistant conversation history for an agent.
// Deprecated: the API requires a step_type query parameter that this signature
// cannot supply, so every call answers 422. Use
// [Client.GetAgentAiConversationHistoryWithOptions].
func (c *Client) GetAgentAiConversationHistory(ctx context.Context, agentID string) (*AiConversationHistoryResponse, error) {
	return c.GetAgentAiConversationHistoryWithOptions(ctx, agentID, AiConversationHistoryOptions{})
}

// GetAgentAiConversationHistoryWithOptions returns the AI assistant conversation
// history for an agent.
//
// StepType is required by the API — the endpoint answers 422 without it.
func (c *Client) GetAgentAiConversationHistoryWithOptions(ctx context.Context, agentID string, opts AiConversationHistoryOptions) (*AiConversationHistoryResponse, error) {
	// Leaving StepType empty only means the parameter is omitted and the server
	// answers 422 naming the wire parameter, which is the failure this method
	// exists to avoid. Fail here instead, naming the field.
	if opts.StepType == "" {
		return nil, &ConfigurationError{
			Message: `AiConversationHistoryOptions.StepType is required by the API; set e.g. StepType: "llm"`,
		}
	}
	q := map[string]string{}
	if opts.StepType != "" {
		q["step_type"] = opts.StepType
	}
	if opts.StepID != "" {
		q["step_id"] = opts.StepID
	}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	if opts.Offset > 0 {
		q["offset"] = fmt.Sprintf("%d", opts.Offset)
	}
	var out AiConversationHistoryResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/ai-assistant/conversations", pathSegment(agentID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiConversationHistoryOptions filters the AI assistant conversation history.
type AiConversationHistoryOptions struct {
	// StepType is the step type to look up. Required by the API.
	StepType string
	// StepID filters to a single step.
	StepID string
	// Limit is the max turns to return (1-50, default 10).
	Limit int
	// Offset is the number of recent turns to skip.
	Offset int
}

// MarkAgentAiSuggestion marks an AI assistant suggestion as accepted/rejected.
func (c *Client) MarkAgentAiSuggestion(ctx context.Context, agentID, conversationID string, body MarkAiSuggestionRequest) error {
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/agents/%s/ai-assistant/%s", pathSegment(agentID), pathSegment(conversationID)), nil, body, nil, nil)
}

// ── Agent Evaluations ───────────────────────────────────────────────────────

// ListEvaluationCriteria lists evaluation criteria for an agent.
//
// Use [Client.ListEvaluationCriteriaPage] for the pagination metadata.
func (c *Client) ListEvaluationCriteria(ctx context.Context, agentID string, opts ListOptions) ([]EvaluationCriteriaResponse, error) {
	page, err := c.ListEvaluationCriteriaPage(ctx, agentID, opts)
	if err != nil {
		return nil, err
	}
	return page.Data, nil
}

// ListEvaluationCriteriaPage lists evaluation criteria for an agent with
// pagination metadata.
//
// Either wire shape is accepted: the endpoint answers with a bare array by
// default and the canonical {data, pagination} envelope once Options.APIVersion
// is 2026-07-27 or later, so a client that decodes only one breaks the day the
// other arrives. Pagination is nil on the bare-array shape.
func (c *Client) ListEvaluationCriteriaPage(ctx context.Context, agentID string, opts ListOptions) (*EvaluationCriteriaListResponse, error) {
	var out EvaluationCriteriaListResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/evaluation-criteria", pathSegment(agentID)), listQuery(opts.Page, opts.Limit), nil, "data", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEvaluationCriteria creates new evaluation criteria for an agent.
func (c *Client) CreateEvaluationCriteria(ctx context.Context, agentID string, body CreateEvaluationCriteriaRequest) (*EvaluationCriteriaResponse, error) {
	var out EvaluationCriteriaResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/evaluation-criteria", pathSegment(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetEvaluationCriteria retrieves evaluation criteria by ID.
func (c *Client) GetEvaluationCriteria(ctx context.Context, criteriaID string) (*EvaluationCriteriaResponse, error) {
	var out EvaluationCriteriaResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s", pathSegment(criteriaID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateEvaluationCriteria updates evaluation criteria.
func (c *Client) UpdateEvaluationCriteria(ctx context.Context, criteriaID string, body UpdateEvaluationCriteriaRequest) (*EvaluationCriteriaResponse, error) {
	var out EvaluationCriteriaResponse
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/agents/evaluation-criteria/%s", pathSegment(criteriaID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EvaluationCriteriaField names a setting of an evaluation criteria that
// [Client.ClearEvaluationCriteriaFields] can clear.
type EvaluationCriteriaField string

const (
	// EvaluationCriteriaDescription clears the criteria's description.
	EvaluationCriteriaDescription EvaluationCriteriaField = "description"
	// EvaluationCriteriaEvaluationPrompt clears the criteria's evaluation prompt.
	EvaluationCriteriaEvaluationPrompt EvaluationCriteriaField = "evaluation_prompt"
	// EvaluationCriteriaExpectationConfig clears the criteria's expectation config.
	EvaluationCriteriaExpectationConfig EvaluationCriteriaField = "expectation_config"
	// EvaluationCriteriaEvaluationTier removes the criteria's pinned tier, so it
	// falls back to the agent's default evaluation tier.
	EvaluationCriteriaEvaluationTier EvaluationCriteriaField = "evaluation_tier"
)

// ClearEvaluationCriteriaFields clears the named settings of an evaluation
// criteria and leaves every other setting unchanged. It sends the fields as
// explicit nulls, which [Client.UpdateEvaluationCriteria] cannot, since it
// leaves a nil field out of the body:
//
//   - [EvaluationCriteriaDescription] removes the description.
//   - [EvaluationCriteriaEvaluationPrompt] removes the evaluation prompt.
//   - [EvaluationCriteriaExpectationConfig] removes the expectation config.
//   - [EvaluationCriteriaEvaluationTier] removes a pinned tier, so the
//     criteria uses the agent's default evaluation tier again. This is the
//     only way to undo a tier set with UpdateEvaluationCriteria.
//
// It returns a [*ConfigurationError], without sending a request, when no
// field is given or a field is not one of these constants.
func (c *Client) ClearEvaluationCriteriaFields(ctx context.Context, criteriaID string, fields ...EvaluationCriteriaField) (*EvaluationCriteriaResponse, error) {
	if len(fields) == 0 {
		return nil, &ConfigurationError{Message: "ClearEvaluationCriteriaFields needs at least one field to clear"}
	}
	payload := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case EvaluationCriteriaDescription, EvaluationCriteriaEvaluationPrompt, EvaluationCriteriaExpectationConfig, EvaluationCriteriaEvaluationTier:
			payload[string(f)] = nil
		default:
			return nil, &ConfigurationError{Message: fmt.Sprintf("ClearEvaluationCriteriaFields cannot clear %q; use one of the EvaluationCriteriaField constants", string(f))}
		}
	}
	var out EvaluationCriteriaResponse
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/agents/evaluation-criteria/%s", pathSegment(criteriaID)), nil, payload, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteEvaluationCriteria deletes evaluation criteria.
func (c *Client) DeleteEvaluationCriteria(ctx context.Context, criteriaID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/agents/evaluation-criteria/%s", pathSegment(criteriaID)), nil, nil, nil, nil)
}

// GetEvaluationCriteriaSummary retrieves the summary for evaluation criteria.
func (c *Client) GetEvaluationCriteriaSummary(ctx context.Context, criteriaID string) (*EvaluationResultSummaryResponse, error) {
	var out EvaluationResultSummaryResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s/summary", pathSegment(criteriaID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEvaluationResults lists evaluation results for criteria.
//
// Use [Client.ListEvaluationResultsWithOptions] to filter them.
func (c *Client) ListEvaluationResults(ctx context.Context, criteriaID string, opts ListOptions) (*EvaluationResultListResponse, error) {
	return c.ListEvaluationResultsWithOptions(ctx, criteriaID, ListEvaluationResultsOptions{ListOptions: opts})
}

// ListEvaluationResultsOptions controls query parameters for
// [Client.ListEvaluationResultsWithOptions].
type ListEvaluationResultsOptions struct {
	ListOptions
	// Status keeps only results with this status (e.g. "passed", "failed", "error").
	Status string
	// FlaggedOnly keeps only flagged results.
	FlaggedOnly bool
	// TimeFrom keeps results from this time on (ISO 8601).
	TimeFrom string
	// TimeTo keeps results up to this time (ISO 8601).
	TimeTo string
}

// ListEvaluationResultsWithOptions lists evaluation results for criteria,
// filtered by status, flag and time range.
func (c *Client) ListEvaluationResultsWithOptions(ctx context.Context, criteriaID string, opts ListEvaluationResultsOptions) (*EvaluationResultListResponse, error) {
	q := listQuery(opts.Page, opts.Limit)
	setQueryValues(q, map[string]string{
		"status":       opts.Status,
		"flagged_only": queryBool(opts.FlaggedOnly),
		"time_from":    opts.TimeFrom,
		"time_to":      opts.TimeTo,
	})
	var out EvaluationResultListResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s/results", pathSegment(criteriaID)), q, nil, "data", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEvaluationResult creates a new evaluation result for criteria.
func (c *Client) CreateEvaluationResult(ctx context.Context, criteriaID string, body CreateEvaluationResultRequest) (*EvaluationResultResponse, error) {
	var out EvaluationResultResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/evaluation-criteria/%s/results", pathSegment(criteriaID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCompatibleRuns lists runs compatible with evaluation criteria.
//
// Use [Client.ListCompatibleRunsWithOptions] to filter them by start time.
func (c *Client) ListCompatibleRuns(ctx context.Context, criteriaID string, opts ListOptions) (*CompatibleRunListResponse, error) {
	return c.ListCompatibleRunsWithOptions(ctx, criteriaID, ListCompatibleRunsOptions{ListOptions: opts})
}

// ListCompatibleRunsOptions controls query parameters for
// [Client.ListCompatibleRunsWithOptions].
type ListCompatibleRunsOptions struct {
	ListOptions
	// StartedAfter keeps only runs started after this time (ISO 8601 date-time).
	StartedAfter string
}

// ListCompatibleRunsWithOptions lists runs compatible with evaluation criteria,
// optionally only those started after a given time.
func (c *Client) ListCompatibleRunsWithOptions(ctx context.Context, criteriaID string, opts ListCompatibleRunsOptions) (*CompatibleRunListResponse, error) {
	q := listQuery(opts.Page, opts.Limit)
	if opts.StartedAfter != "" {
		q["started_after"] = opts.StartedAfter
	}
	var out CompatibleRunListResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/agents/evaluation-criteria/%s/compatible-runs", pathSegment(criteriaID)), q, nil, "data", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TestDraftEvaluation tests a draft evaluation criteria without persisting.
func (c *Client) TestDraftEvaluation(ctx context.Context, agentID string, body TestDraftEvaluationRequest) (*TestDraftEvaluationResponse, error) {
	var out TestDraftEvaluationResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agents/%s/evaluation-criteria/test-draft", pathSegment(agentID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAgentEvaluationResults lists all evaluation results for an agent.
//
// Use [Client.ListAgentEvaluationResultsWithOptions] to filter them.
func (c *Client) ListAgentEvaluationResults(ctx context.Context, agentID string, opts ListOptions) (*EvaluationResultWithCriteriaListResponse, error) {
	return c.ListAgentEvaluationResultsWithOptions(ctx, agentID, ListAgentEvaluationResultsOptions{ListOptions: opts})
}

// ListAgentEvaluationResultsOptions controls query parameters for
// [Client.ListAgentEvaluationResultsWithOptions].
type ListAgentEvaluationResultsOptions struct {
	ListOptions
	// Status keeps only results with this status (e.g. "passed", "failed", "error").
	Status string
	// Step keeps only results for this agent step.
	Step string
	// FlaggedOnly keeps only flagged results.
	FlaggedOnly bool
	// TimeFrom keeps results from this time on (ISO 8601).
	TimeFrom string
	// TimeTo keeps results up to this time (ISO 8601).
	TimeTo string
}

// ListAgentEvaluationResultsWithOptions lists an agent's evaluation results,
// filtered by status, step, flag and time range.
func (c *Client) ListAgentEvaluationResultsWithOptions(ctx context.Context, agentID string, opts ListAgentEvaluationResultsOptions) (*EvaluationResultWithCriteriaListResponse, error) {
	q := listQuery(opts.Page, opts.Limit)
	setQueryValues(q, map[string]string{
		"status":       opts.Status,
		"step":         opts.Step,
		"flagged_only": queryBool(opts.FlaggedOnly),
		"time_from":    opts.TimeFrom,
		"time_to":      opts.TimeTo,
	})
	var out EvaluationResultWithCriteriaListResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/evaluation-results", pathSegment(agentID)), q, nil, "data", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRunEvaluationResults lists evaluation results for a specific run.
//
// Either wire shape is accepted: the endpoint returns a bare array by default
// and an envelope once the caller opts in with Options.APIVersion of 2026-07-27
// or later, so Total, Page and Limit are zero in the former case.
func (c *Client) ListRunEvaluationResults(ctx context.Context, agentID, runID string, opts ListOptions) (*EvaluationResultWithCriteriaListResponse, error) {
	var out EvaluationResultWithCriteriaListResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/runs/%s/evaluation-results", pathSegment(agentID), pathSegment(runID)), listQuery(opts.Page, opts.Limit), nil, "data", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEvaluationRuns lists evaluation run summaries for an agent.
//
// Use [Client.ListEvaluationRunsWithOptions] to filter them.
func (c *Client) ListEvaluationRuns(ctx context.Context, agentID string, opts ListOptions) (*EvaluationRunSummaryListResponse, error) {
	return c.ListEvaluationRunsWithOptions(ctx, agentID, ListEvaluationRunsOptions{ListOptions: opts})
}

// ListEvaluationRunsOptions controls query parameters for
// [Client.ListEvaluationRunsWithOptions].
type ListEvaluationRunsOptions struct {
	ListOptions
	// Status keeps only runs with this status.
	Status string
	// Step keeps only runs for this agent step.
	Step string
	// TimeFrom keeps runs from this time on (ISO 8601).
	TimeFrom string
	// TimeTo keeps runs up to this time (ISO 8601).
	TimeTo string
}

// ListEvaluationRunsWithOptions lists an agent's evaluation run summaries,
// filtered by status, step and time range.
func (c *Client) ListEvaluationRunsWithOptions(ctx context.Context, agentID string, opts ListEvaluationRunsOptions) (*EvaluationRunSummaryListResponse, error) {
	q := listQuery(opts.Page, opts.Limit)
	setQueryValues(q, map[string]string{
		"status":    opts.Status,
		"step":      opts.Step,
		"time_from": opts.TimeFrom,
		"time_to":   opts.TimeTo,
	})
	var out EvaluationRunSummaryListResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/agents/%s/evaluation-runs", pathSegment(agentID)), q, nil, "data", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetNonManualEvaluationSummary retrieves a summary of non-manual evaluation
// results, for one agent or account-wide when agentID is empty.
//
// Use [Client.GetNonManualEvaluationSummaryWithOptions] to set the period.
func (c *Client) GetNonManualEvaluationSummary(ctx context.Context, agentID string) (*NonManualEvaluationSummaryResponse, error) {
	return c.GetNonManualEvaluationSummaryWithOptions(ctx, NonManualEvaluationSummaryOptions{AgentID: agentID})
}

// NonManualEvaluationSummaryOptions controls query parameters for
// [Client.GetNonManualEvaluationSummaryWithOptions].
//
// The period is StartDate through EndDate, or StartDate through today when
// EndDate is empty; Days applies only when StartDate is empty. An EndDate
// without a StartDate is ignored.
type NonManualEvaluationSummaryOptions struct {
	// AgentID scopes the summary to one agent. Empty is account-wide.
	AgentID string
	// Days is the look-back period in days, ending today (server default 30).
	// Zero omits it. Ignored when StartDate is set.
	Days int
	// StartDate starts the period (YYYY-MM-DD). Overrides Days.
	StartDate string
	// EndDate ends the period (YYYY-MM-DD). Used only with StartDate.
	EndDate string
}

// GetNonManualEvaluationSummaryWithOptions retrieves a summary of non-manual
// evaluation results over a chosen period.
func (c *Client) GetNonManualEvaluationSummaryWithOptions(ctx context.Context, opts NonManualEvaluationSummaryOptions) (*NonManualEvaluationSummaryResponse, error) {
	q := map[string]string{}
	setQueryValues(q, map[string]string{
		"agent_id":   opts.AgentID,
		"days":       queryPositiveInt(opts.Days),
		"start_date": opts.StartDate,
		"end_date":   opts.EndDate,
	})
	var out NonManualEvaluationSummaryResponse
	if err := c.do(ctx, http.MethodGet, "/agents/evaluation-results/non-manual-summary", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RunStreamingAgentAndWaitOptions controls [Client.RunStreamingAgentAndWaitWithOptions].
type RunStreamingAgentAndWaitOptions struct {
	// ReturnOnResult returns the run from the `event: result` message, sent
	// once the run has reported its result, without waiting for `done`. Its
	// Output is the run's answer and does not change, but its Status may not be
	// terminal: later steps may still run and the run can still fail. Check
	// ResultReportedAt, not Status.
	ReturnOnResult bool
	// ErrorOnIncomplete returns a [*StreamingError] carrying the latest
	// snapshot in its Run, rather than the released behaviour, whenever the
	// wait ends without the run it was waiting for: on a timeout, or when the
	// stream ends or breaks first. It will become the default in the next
	// major version.
	ErrorOnIncomplete bool
}

// RunStreamingAgentAndWait runs an agent in priority mode and waits for completion.
// It is [Client.RunStreamingAgentAndWaitWithOptions] with nil options.
func (c *Client) RunStreamingAgentAndWait(ctx context.Context, agentID string, body AgentRunStreamRequest) (*AgentRunResponse, error) {
	return c.RunStreamingAgentAndWaitWithOptions(ctx, agentID, body, nil)
}

// RunStreamingAgentAndWaitWithOptions runs an agent in priority mode and waits
// for completion.
//
// This method calls POST /agents/{agent_id}/runs/stream and consumes Server-Sent Events (SSE).
// It returns when the stream emits an `event: done` message whose `data:` field
// contains the final run payload, or, with opts.ReturnOnResult, the
// `event: result` one (see [RunStreamingAgentAndWaitOptions]).
//
// The latest snapshot read is the `event: result` run if one arrived
// (ResultReportedAt is set and Output is the answer), else the `event: init`
// one, else nil. If the stream ends or breaks before the event it waits for,
// or the timeout passes:
//
//   - By default (the released behaviour), a stream that ends returns the
//     latest snapshot with a nil error, though the run may not have finished:
//     check its Status and ResultReportedAt. With no snapshot it returns a
//     [*StreamingError]. A timeout or a broken stream returns the bare error
//     (a timeout is context.DeadlineExceeded) with the latest snapshot beside it.
//   - With opts.ErrorOnIncomplete, recommended and the default from the next
//     major version, each of these returns the latest snapshot together with a
//     [*StreamingError] whose Run is that snapshot. On a timeout the error also
//     satisfies errors.Is(err, context.DeadlineExceeded).
//
// Timeout behavior is controlled by ctx (for example, use context.WithTimeout). If ctx has no deadline,
// a default 60s timeout is applied.
//
// body.IncludeReasoning is not sent: this method reads only the run snapshots,
// so reasoning events would be streamed only to be discarded while counting
// against the timeout. Use [Client.RunStreamingAgent] to receive them.
func (c *Client) RunStreamingAgentAndWaitWithOptions(ctx context.Context, agentID string, body AgentRunStreamRequest, opts *RunStreamingAgentAndWaitOptions) (*AgentRunResponse, error) {
	returnOnResult := opts != nil && opts.ReturnOnResult
	errorOnIncomplete := opts != nil && opts.ErrorOnIncomplete
	body.IncludeReasoning = nil
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}

	reqURL, err := c.buildURL(fmt.Sprintf("/agents/%s/runs/stream", pathSegment(agentID)), nil)
	if err != nil {
		return nil, err
	}
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

	// http.Client.Timeout would cut the stream while it is read; ctx bounds it.
	streamClient := *c.httpClient
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, apiError(resp.StatusCode, http.MethodPost, reqURL.String(), raw)
	}

	reader := bufio.NewReader(resp.Body)
	var currentEvent string
	var dataLines []string
	var lastSeen *AgentRunResponse
	var sawResult bool

	// dispatch reports the run to return when the event finishes the wait.
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

		if currentEvent == "init" || currentEvent == "result" || currentEvent == "done" {
			var parsed AgentRunResponse
			if err := json.Unmarshal([]byte(data), &parsed); err == nil {
				lastSeen = &parsed
				if currentEvent == "result" {
					sawResult = true
				}
				if currentEvent == "done" || (returnOnResult && currentEvent == "result") {
					return &parsed, true
				}
			}
		}
		return nil, false
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// Use an event whose lines were read but whose blank line was not.
			if done, ok := dispatch(); ok {
				return done, nil
			}
			if !errorOnIncomplete {
				// The released endings, kept until the next major version,
				// with the additions the CHANGELOG lists: the buffered event
				// above is dispatched on any read error, not only at EOF, the
				// latest snapshot may be the result run, and it is returned
				// beside a timeout or read error rather than nil.
				switch {
				case err != io.EOF:
					return lastSeen, err
				case lastSeen != nil:
					return lastSeen, nil
				}
				return nil, &StreamingError{Message: "stream ended before receiving done event"}
			}
			ctxErr := ctx.Err()
			if errors.Is(ctxErr, context.Canceled) {
				return lastSeen, ctxErr
			}
			streamErr := &StreamingError{Run: lastSeen}
			switch {
			case ctxErr != nil:
				streamErr.Message, streamErr.Err = "timed out before receiving done event", ctxErr
			case err == io.EOF:
				streamErr.Message = "stream ended before receiving done event"
			default:
				streamErr.Message, streamErr.Err = "stream broke before receiving done event", err
			}
			if sawResult {
				streamErr.Message += " (a result had been reported; see Run)"
			} else {
				streamErr.Message += " (no result had been reported)"
			}
			if lastSeen != nil {
				streamErr.RunID = lastSeen.RunId
			}
			return lastSeen, streamErr
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
	// Event is the SSE event type (e.g. "init", "update", "result", "done").
	// "result" carries the run once it has reported its result
	// (ResultReportedAt is set and Output is the answer); it comes before
	// "done", possibly while later steps still run.
	Event string
	// Data is the raw JSON data payload.
	Data string
	// Run is the parsed AgentRunResponse if the data could be decoded.
	Run *AgentRunResponse
}

// RunStreamingAgent runs an agent in priority mode and returns a channel that
// yields SSE events as they arrive. The channel is closed when the stream ends
// or when ctx is cancelled. Take the run's answer from the "result" event's
// Run; the "done" event's Run is the terminal snapshot, and its Status says
// whether the whole run succeeded, which can still fail after its result.
//
// When the server answers with plain JSON instead of an SSE stream, the
// channel yields only a "done" event, so done may be the only event: read the
// answer from its Run.
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

		reqURL, err := c.buildURL(fmt.Sprintf("/agents/%s/runs/stream", pathSegment(agentID)), nil)
		if err != nil {
			errCh <- err
			return
		}
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

		// http.Client.Timeout would cut the stream while it is read; ctx bounds it.
		streamClient := *c.httpClient
		streamClient.Timeout = 0
		resp, err := streamClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(resp.Body)
			errCh <- apiError(resp.StatusCode, http.MethodPost, reqURL.String(), raw)
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
	// ReturnOnResult returns as soon as a snapshot has ResultReportedAt set:
	// its Output is the run's answer and does not change, but its Status may
	// not be terminal: later steps may still run and the run can still fail.
	// Check ResultReportedAt, not Status. When false, polling continues until
	// the run is completed or failed.
	ReturnOnResult bool
}

// RunAgentAndPoll runs an agent and polls until it reaches a terminal status
// (completed or failed), or, with opts.ReturnOnResult, until it has reported
// its result. The initial RunAgent response is checked before the first wait;
// with opts.IncludeStepOutputs, which that response lacks, a response that
// would end the poll is fetched again at once with step outputs, and that
// snapshot is checked instead. The context controls the overall timeout. When
// it ends, or a GetAgentRun fails, the last run polled is returned with the
// error, so its RunId is kept.
func (c *Client) RunAgentAndPoll(ctx context.Context, agentID string, body AgentRunRequest, opts *RunAgentAndPollOptions) (*AgentRunResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	run, err := c.RunAgent(ctx, agentID, body)
	if err != nil {
		return nil, err
	}

	interval := 2 * time.Second
	var includeSteps, returnOnResult bool
	if opts != nil {
		if opts.PollInterval > 0 {
			interval = opts.PollInterval
		}
		includeSteps = opts.IncludeStepOutputs
		returnOnResult = opts.ReturnOnResult
	}

	var getOpts *GetAgentRunOptions
	if includeSteps {
		getOpts = &GetAgentRunOptions{IncludeStepOutputs: true}
	}
	// The RunAgent response carries no step outputs, so with includeSteps it
	// cannot end the poll: fetch the run with them first, without waiting.
	lacksSteps := includeSteps

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		finished := run.Status == "completed" || run.Status == "failed" ||
			(returnOnResult && run.ResultReportedAt != nil)
		if finished && !lacksSteps {
			return run, nil
		}
		if !finished {
			select {
			case <-ctx.Done():
				return run, ctx.Err()
			case <-ticker.C:
			}
		}

		next, err := c.GetAgentRun(ctx, run.RunId, getOpts)
		if err != nil {
			return run, err
		}
		run, lacksSteps = next, false
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

	reqURL, err := c.buildURL(apiPath, nil)
	if err != nil {
		return nil, err
	}

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
	var fw io.Writer
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(resp.StatusCode, http.MethodPost, reqURL.String(), raw)
	}
	return raw, nil
}

// UploadFileToSource uploads a file to a source connection.
func (c *Client) UploadFileToSource(ctx context.Context, sourceConnectionID string, req UploadFileRequest) (*FileUploadResponse, error) {
	raw, err := c.doUpload(ctx, fmt.Sprintf("/sources/%s/upload", pathSegment(sourceConnectionID)), req)
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
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s", pathSegment(sourceConnectionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadFileToContent replaces the file backing an existing content version.
func (c *Client) UploadFileToContent(ctx context.Context, contentVersionID string, req UploadFileRequest) (*ContentFileUploadResponse, error) {
	raw, err := c.doUpload(ctx, fmt.Sprintf("/contents/%s/upload", pathSegment(contentVersionID)), req)
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

// ── Agent Email Triggers ──────────────────────────────────────────────────────

// SetEmailTriggerConfig sets the alias, sender allowlist and inbound-handling
// flags on an agent's EMAIL_RECEIVED trigger and returns its computed address(es).
//
// A nil field is left unchanged. To clear a field deliberately, pass a pointer to
// its zero value: a pointer to "" clears the alias, a pointer to an empty slice
// clears the allowlist.
func (c *Client) SetEmailTriggerConfig(ctx context.Context, agentID string, triggerID string, body RoutersApiAgentsSetEmailTriggerConfigRequest) (*EmailTriggerConfigResponse, error) {
	var out EmailTriggerConfigResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s/triggers/%s/email-config", pathSegment(agentID), pathSegment(triggerID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Slack Triggers ──────────────────────────────────────────────────────

// SetSlackTriggerConfig sets the Slack connection, form, channels, visibility
// and conversation settings of an agent's SLACK_COMMAND trigger, which people
// run from Slack with /seclai or the message shortcut.
//
// A nil field is left unchanged. To clear Form or ChannelIds, pass a non-nil
// pointer to an empty slice; the API treats an empty list and null the same.
// A nil MaxTurns cannot be sent, so remove the turn limit with
// ClearSlackTriggerMaxTurns.
func (c *Client) SetSlackTriggerConfig(ctx context.Context, agentID string, triggerID string, body SetSlackTriggerConfigRequest) (*SlackTriggerConfigResponse, error) {
	var out SlackTriggerConfigResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s/triggers/%s/slack-config", pathSegment(agentID), pathSegment(triggerID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClearSlackTriggerMaxTurns removes the turn limit of an agent's SLACK_COMMAND
// trigger, so a conversation may continue for any number of turns. It sends an
// explicit null max_turns and leaves every other setting unchanged; set a limit
// with SetSlackTriggerConfig's MaxTurns.
func (c *Client) ClearSlackTriggerMaxTurns(ctx context.Context, agentID string, triggerID string) (*SlackTriggerConfigResponse, error) {
	var out SlackTriggerConfigResponse
	payload := map[string]any{"max_turns": nil}
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/agents/%s/triggers/%s/slack-config", pathSegment(agentID), pathSegment(triggerID)), nil, payload, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Agent Email Governance ────────────────────────────────────────────────────

// AgentEmailOptOutOptions controls query parameters for [Client.ListAgentEmailOptOuts].
type AgentEmailOptOutOptions struct {
	// AgentID filters to one agent. Account-wide opt-outs still apply.
	AgentID string
	// Limit is the page size (1-200). Zero omits the parameter.
	Limit int
	// Offset is the number of rows to skip. Zero omits the parameter.
	Offset int
}

// BlockedEmailSenderOptions controls query parameters for [Client.ListBlockedEmailSenders].
//
// This endpoint paginates by limit/offset rather than by page number.
type BlockedEmailSenderOptions struct {
	// Limit is the page size (1-200). Zero omits the parameter.
	Limit int
	// Offset is the number of rows to skip. Zero omits the parameter.
	Offset int
}

// InboundEmailRejectionOptions controls query parameters for [Client.ListInboundEmailRejections].
type InboundEmailRejectionOptions struct {
	// AgentID filters to a single agent's rejections.
	AgentID string
	// Limit is the maximum number of results (1-200). Zero omits the parameter.
	Limit int
}

// ListAgentEmailOptOuts lists recipients who have opted out of this account's
// agent emails. Account-wide opt-outs always apply.
func (c *Client) ListAgentEmailOptOuts(ctx context.Context, opts AgentEmailOptOutOptions) (*AgentEmailOptOutListResponse, error) {
	q := map[string]string{}
	if opts.AgentID != "" {
		q["agent_id"] = opts.AgentID
	}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	if opts.Offset > 0 {
		q["offset"] = fmt.Sprintf("%d", opts.Offset)
	}
	var out AgentEmailOptOutListResponse
	if err := c.doList(ctx, http.MethodGet, "/agents/agent-email-optouts", q, nil, "items", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveAgentEmailOptOut revokes an opt-out, opting the recipient back in.
func (c *Client) RemoveAgentEmailOptOut(ctx context.Context, optoutID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/agents/agent-email-optouts/%s", pathSegment(optoutID)), nil, nil, nil, nil)
}

// ListBlockedEmailSenders lists the account's blocked inbound email senders
// (newest first) plus the governance auto-block mode.
func (c *Client) ListBlockedEmailSenders(ctx context.Context, opts BlockedEmailSenderOptions) (*BlockedEmailSenderListResponse, error) {
	q := map[string]string{}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	if opts.Offset > 0 {
		q["offset"] = fmt.Sprintf("%d", opts.Offset)
	}
	var out BlockedEmailSenderListResponse
	if err := c.doList(ctx, http.MethodGet, "/agents/blocked-email-senders", q, nil, "items", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BlockEmailSender adds a sender address or a whole domain to the account
// blocklist. Idempotent. Requires an account owner or admin.
func (c *Client) BlockEmailSender(ctx context.Context, body BlockEmailSenderRequest) (*BlockedEmailSenderResponse, error) {
	var out BlockedEmailSenderResponse
	if err := c.do(ctx, http.MethodPost, "/agents/blocked-email-senders", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnblockEmailSender removes a blocked sender by ID. Requires an account
// owner or admin.
func (c *Client) UnblockEmailSender(ctx context.Context, blockedID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/agents/blocked-email-senders/%s", pathSegment(blockedID)), nil, nil, nil, nil)
}

// SetAutoBlockMode sets whether a governance BLOCK on an authenticated inbound
// sender auto-adds them to the blocklist. Mode is disabled, input, or
// input_and_output. Requires an account owner or admin.
func (c *Client) SetAutoBlockMode(ctx context.Context, body SetAutoBlockModeRequest) (*BlockedEmailSenderListResponse, error) {
	var out BlockedEmailSenderListResponse
	if err := c.doList(ctx, http.MethodPut, "/agents/blocked-email-senders/mode", nil, body, "items", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListInboundEmailRejections lists recent inbound emails that were discarded
// before running an agent (unauthorized sender, unknown alias, spam, flood-shed).
func (c *Client) ListInboundEmailRejections(ctx context.Context, opts InboundEmailRejectionOptions) ([]InboundEmailRejectionResponse, error) {
	q := map[string]string{}
	if opts.AgentID != "" {
		q["agent_id"] = opts.AgentID
	}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	var out []InboundEmailRejectionResponse
	if err := c.doList(ctx, http.MethodGet, "/agents/inbound-email-rejections", q, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetInboundEmailStatus reports whether the account-wide overload circuit
// breaker has paused new inbound email, plus the queued backlog size.
func (c *Client) GetInboundEmailStatus(ctx context.Context) (*InboundEmailStatusResponse, error) {
	var out InboundEmailStatusResponse
	if err := c.do(ctx, http.MethodGet, "/agents/inbound-email-status", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelQueuedEmailRuns fails all of the account's QUEUED (over-quota parked)
// inbound-email runs. Requires an account owner or admin.
func (c *Client) CancelQueuedEmailRuns(ctx context.Context) (*CancelQueuedRunsResponse, error) {
	var out CancelQueuedRunsResponse
	if err := c.do(ctx, http.MethodPost, "/agents/inbound-email-status/cancel-queued", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResumeInboundEmail manually lifts the account-wide inbound pause. One-shot:
// the breaker re-arms if the backlog is still above the ceiling.
func (c *Client) ResumeInboundEmail(ctx context.Context) (*ResumeInboundResponse, error) {
	var out ResumeInboundResponse
	if err := c.do(ctx, http.MethodPost, "/agents/inbound-email-status/resume", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Knowledge Bases ─────────────────────────────────────────────────────────

// ListKnowledgeBases lists knowledge bases.
func (c *Client) ListKnowledgeBases(ctx context.Context, opts SortableListOptions) (*KnowledgeBaseListResponse, error) {
	var out KnowledgeBaseListResponse
	if err := c.doList(ctx, http.MethodGet, "/knowledge_bases", sortableListQuery(opts), nil, "knowledge_bases", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateKnowledgeBase creates a new knowledge base.
func (c *Client) CreateKnowledgeBase(ctx context.Context, body CreateKnowledgeBaseBody) (*KnowledgeBaseResponse, error) {
	var out KnowledgeBaseResponse
	if err := c.do(ctx, http.MethodPost, "/knowledge_bases", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetKnowledgeBase retrieves a knowledge base by ID.
func (c *Client) GetKnowledgeBase(ctx context.Context, knowledgeBaseID string) (*KnowledgeBaseResponse, error) {
	var out KnowledgeBaseResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/knowledge_bases/%s", pathSegment(knowledgeBaseID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetKnowledgeBaseMetadataKeyStats returns the item-metadata keys seen in a
// knowledge base's content, with item counts and each key's most frequent
// values, for all time and the last 30 and 7 days.
func (c *Client) GetKnowledgeBaseMetadataKeyStats(ctx context.Context, knowledgeBaseID string) (*KnowledgeBaseMetadataKeyStatsResponse, error) {
	var out KnowledgeBaseMetadataKeyStatsResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/knowledge_bases/%s/metadata-key-stats", pathSegment(knowledgeBaseID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateKnowledgeBase updates a knowledge base.
func (c *Client) UpdateKnowledgeBase(ctx context.Context, knowledgeBaseID string, body UpdateKnowledgeBaseBody) (*KnowledgeBaseResponse, error) {
	var out KnowledgeBaseResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/knowledge_bases/%s", pathSegment(knowledgeBaseID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteKnowledgeBase deletes a knowledge base.
func (c *Client) DeleteKnowledgeBase(ctx context.Context, knowledgeBaseID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/knowledge_bases/%s", pathSegment(knowledgeBaseID)), nil, nil, nil, nil)
}

// ── Memory Banks ────────────────────────────────────────────────────────────

// ListMemoryBanks lists memory banks.
//
// Use [Client.ListMemoryBanksWithOptions] to filter them by type.
func (c *Client) ListMemoryBanks(ctx context.Context, opts SortableListOptions) (*MemoryBankListResponse, error) {
	return c.ListMemoryBanksWithOptions(ctx, ListMemoryBanksOptions{SortableListOptions: opts})
}

// ListMemoryBanksOptions controls query parameters for [Client.ListMemoryBanksWithOptions].
type ListMemoryBanksOptions struct {
	SortableListOptions
	// Type keeps only banks of this type: "conversation" or "general".
	Type string
}

// ListMemoryBanksWithOptions lists memory banks, optionally of one type.
func (c *Client) ListMemoryBanksWithOptions(ctx context.Context, opts ListMemoryBanksOptions) (*MemoryBankListResponse, error) {
	q := sortableListQuery(opts.SortableListOptions)
	if opts.Type != "" {
		q["type"] = opts.Type
	}
	var out MemoryBankListResponse
	if err := c.doList(ctx, http.MethodGet, "/memory_banks", q, nil, "memory_banks", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateMemoryBankOptions holds the settings of [Client.CreateMemoryBankWithOptions]
// that [CreateMemoryBankBody] cannot express.
type CreateMemoryBankOptions struct {
	// RetainIndefinitely keeps the new bank's entries indefinitely on every API
	// version. It sends an explicit null retention_days, which a nil
	// RetentionDays cannot, since that is left out of the request. It cannot be
	// combined with RetentionDays.
	RetainIndefinitely bool
}

// CreateMemoryBank creates a new memory bank. It is
// [Client.CreateMemoryBankWithOptions] with no options.
//
// A nil RetentionDays is left out of the request, so the API's default
// applies. A general bank keeps its entries indefinitely on every version.
// From API version 2026-08-03 a conversation bank keeps them for 90 days;
// earlier versions keep them for 30 days, or for MaxAgeDays when that is
// longer. Set RetentionDays to choose a window, or use
// CreateMemoryBankWithOptions with RetainIndefinitely to keep entries
// indefinitely.
func (c *Client) CreateMemoryBank(ctx context.Context, body CreateMemoryBankBody) (*MemoryBankResponse, error) {
	return c.CreateMemoryBankWithOptions(ctx, body, CreateMemoryBankOptions{})
}

// CreateMemoryBankWithOptions creates a new memory bank with the settings in
// opts. It returns a [*ConfigurationError], without sending a request, when
// opts.RetainIndefinitely is set together with body.RetentionDays.
func (c *Client) CreateMemoryBankWithOptions(ctx context.Context, body CreateMemoryBankBody, opts CreateMemoryBankOptions) (*MemoryBankResponse, error) {
	var payload any = body
	if opts.RetainIndefinitely {
		if body.RetentionDays != nil {
			return nil, &ConfigurationError{Message: "CreateMemoryBankWithOptions: set RetentionDays or RetainIndefinitely, not both"}
		}
		// The outer RetentionDays shadows the embedded one and, having no
		// omitempty and being nil, is always written as null.
		payload = struct {
			CreateMemoryBankBody
			RetentionDays *int `json:"retention_days"`
		}{CreateMemoryBankBody: body}
	}
	var out MemoryBankResponse
	if err := c.do(ctx, http.MethodPost, "/memory_banks", nil, payload, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMemoryBank retrieves a memory bank by ID.
func (c *Client) GetMemoryBank(ctx context.Context, memoryBankID string) (*MemoryBankResponse, error) {
	var out MemoryBankResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/memory_banks/%s", pathSegment(memoryBankID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateMemoryBank updates a memory bank. Only the fields set on body are
// sent; a nil field leaves that setting unchanged. To remove the retention
// window, use [Client.ClearMemoryBankRetention].
func (c *Client) UpdateMemoryBank(ctx context.Context, memoryBankID string, body UpdateMemoryBankBody) (*MemoryBankResponse, error) {
	var out MemoryBankResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/memory_banks/%s", pathSegment(memoryBankID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClearMemoryBankRetention removes a memory bank's retention window, so its
// entries are kept indefinitely. It sends a retention_days of 0, which is how
// the update endpoint spells "no window", and leaves every other setting
// unchanged.
func (c *Client) ClearMemoryBankRetention(ctx context.Context, memoryBankID string) (*MemoryBankResponse, error) {
	zero := 0
	return c.UpdateMemoryBank(ctx, memoryBankID, UpdateMemoryBankBody{RetentionDays: &zero})
}

// DeleteMemoryBank deletes a memory bank.
func (c *Client) DeleteMemoryBank(ctx context.Context, memoryBankID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/memory_banks/%s", pathSegment(memoryBankID)), nil, nil, nil, nil)
}

// GetAgentsUsingMemoryBank lists agents that use a memory bank.
func (c *Client) GetAgentsUsingMemoryBank(ctx context.Context, memoryBankID string) (json.RawMessage, error) {
	body, _, err := c.agentsUsingMemoryBankBody(ctx, memoryBankID)
	return rawJSON(body, err)
}

// agentsUsingMemoryBankBody is the request behind [Client.GetAgentsUsingMemoryBank] and its typed form.
func (c *Client) agentsUsingMemoryBankBody(ctx context.Context, memoryBankID string) ([]byte, *url.URL, error) {
	return c.doBytes(ctx, http.MethodGet, fmt.Sprintf("/memory_banks/%s/agents", pathSegment(memoryBankID)), nil, nil, nil)
}

// GetMemoryBankStats retrieves statistics for a memory bank.
//
// Use [Client.GetMemoryBankStatsWithOptions] to set the period.
func (c *Client) GetMemoryBankStats(ctx context.Context, memoryBankID string) (json.RawMessage, error) {
	return c.GetMemoryBankStatsWithOptions(ctx, memoryBankID, MemoryBankStatsOptions{})
}

// MemoryBankStatsOptions controls query parameters for
// [Client.GetMemoryBankStatsWithOptions].
//
// StartDate and EndDate override Days only when both are set; either one alone
// is ignored and Days applies.
type MemoryBankStatsOptions struct {
	// Days is the look-back period in days, ending today (server default 30,
	// 1-730). Zero omits it.
	Days int
	// StartDate starts the period (YYYY-MM-DD). Used only with EndDate.
	StartDate string
	// EndDate ends the period (YYYY-MM-DD). Used only with StartDate.
	EndDate string
}

// GetMemoryBankStatsWithOptions retrieves statistics for a memory bank over a
// chosen period.
func (c *Client) GetMemoryBankStatsWithOptions(ctx context.Context, memoryBankID string, opts MemoryBankStatsOptions) (json.RawMessage, error) {
	q := map[string]string{}
	setQueryValues(q, map[string]string{
		"days":       queryPositiveInt(opts.Days),
		"start_date": opts.StartDate,
		"end_date":   opts.EndDate,
	})
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/memory_banks/%s/stats", pathSegment(memoryBankID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CompactMemoryBank triggers compaction for a memory bank.
func (c *Client) CompactMemoryBank(ctx context.Context, memoryBankID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/memory_banks/%s/compact", pathSegment(memoryBankID)), nil, nil, nil, nil)
}

// DeleteMemoryBankSource deletes the source associated with a memory bank.
func (c *Client) DeleteMemoryBankSource(ctx context.Context, memoryBankID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/memory_banks/%s/source", pathSegment(memoryBankID)), nil, nil, nil, nil)
}

// TestMemoryBankCompaction tests compaction for a memory bank.
func (c *Client) TestMemoryBankCompaction(ctx context.Context, memoryBankID string, body TestCompactionRequest) (*CompactionTestResponse, error) {
	var out CompactionTestResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/memory_banks/%s/test-compaction", pathSegment(memoryBankID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TestCompactionPromptStandalone tests a compaction prompt without a memory bank.
func (c *Client) TestCompactionPromptStandalone(ctx context.Context, body StandaloneTestCompactionRequest) (*CompactionTestResponse, error) {
	var out CompactionTestResponse
	if err := c.do(ctx, http.MethodPost, "/memory_banks/test-compaction", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMemoryBankTemplates lists available memory bank templates.
func (c *Client) ListMemoryBankTemplates(ctx context.Context) (json.RawMessage, error) {
	body, _, err := c.memoryBankTemplatesBody(ctx)
	return rawJSON(body, err)
}

// memoryBankTemplatesBody is the request behind [Client.ListMemoryBankTemplates] and its typed form.
func (c *Client) memoryBankTemplatesBody(ctx context.Context) ([]byte, *url.URL, error) {
	return c.doBytes(ctx, http.MethodGet, "/memory_banks/templates", nil, nil, nil)
}

// ── Memory Bank AI Assistant ────────────────────────────────────────────────

// GenerateMemoryBankConfig uses the AI assistant to generate memory bank configuration.
func (c *Client) GenerateMemoryBankConfig(ctx context.Context, body MemoryBankAiAssistantRequest) (*MemoryBankAiAssistantResponse, error) {
	var out MemoryBankAiAssistantResponse
	if err := c.do(ctx, http.MethodPost, "/memory_banks/ai-assistant", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMemoryBankAiLastConversation retrieves the last AI assistant conversation for memory banks.
//
// Use [Client.GetMemoryBankAiLastConversationWithOptions] to page through its turns.
func (c *Client) GetMemoryBankAiLastConversation(ctx context.Context) (*MemoryBankLastConversationResponse, error) {
	return c.GetMemoryBankAiLastConversationWithOptions(ctx, LastConversationOptions{})
}

// LastConversationOptions pages through the turns of an AI assistant's last
// conversation.
type LastConversationOptions struct {
	// Limit is the maximum number of turns (server default 5). Zero omits it.
	Limit int
	// Offset is the number of most recent turns to skip. Zero omits it.
	Offset int
}

// GetMemoryBankAiLastConversationWithOptions retrieves a page of turns from the
// last memory bank AI assistant conversation.
func (c *Client) GetMemoryBankAiLastConversationWithOptions(ctx context.Context, opts LastConversationOptions) (*MemoryBankLastConversationResponse, error) {
	q := map[string]string{}
	setQueryValues(q, map[string]string{
		"limit":  queryPositiveInt(opts.Limit),
		"offset": queryPositiveInt(opts.Offset),
	})
	var out MemoryBankLastConversationResponse
	if err := c.do(ctx, http.MethodGet, "/memory_banks/ai-assistant/last-conversation", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptMemoryBankAiSuggestion accepts an AI-generated memory bank suggestion.
func (c *Client) AcceptMemoryBankAiSuggestion(ctx context.Context, conversationID string, body MemoryBankAcceptRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/memory_banks/ai-assistant/%s", pathSegment(conversationID)), nil, body, nil, &out); err != nil {
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
	if err := c.do(ctx, http.MethodGet, "/sources", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSource creates a new source.
func (c *Client) CreateSource(ctx context.Context, body CreateSourceBody) (*SourceResponse, error) {
	var out SourceResponse
	if err := c.do(ctx, http.MethodPost, "/sources", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSource retrieves a source by ID.
func (c *Client) GetSource(ctx context.Context, sourceID string) (*SourceResponse, error) {
	var out SourceResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s", pathSegment(sourceID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSource updates a source. Only the fields set on body are sent; a nil
// field leaves that setting unchanged. To clear the retention window or the
// spend limit, use [Client.ClearSourceFields].
func (c *Client) UpdateSource(ctx context.Context, sourceID string, body UpdateSourceBody) (*SourceResponse, error) {
	var out SourceResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/sources/%s", pathSegment(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SourceField names a setting of a source that [Client.ClearSourceFields] can
// clear.
type SourceField string

const (
	// SourceFieldRetentionDays removes the retention window, so the source's
	// content is kept indefinitely.
	SourceFieldRetentionDays SourceField = "retention_days"
	// SourceFieldMaxCreditsPerPeriod removes the spend limit, so the source may
	// commit any number of credits per billing period.
	SourceFieldMaxCreditsPerPeriod SourceField = "max_credits_per_period"
)

// ClearSourceFields clears the named settings of a source and leaves every
// other setting unchanged. It sends the fields as explicit nulls, which
// [Client.UpdateSource] cannot, since it leaves a nil field out of the body:
//
//   - [SourceFieldRetentionDays] removes the retention window, so content is
//     kept indefinitely.
//   - [SourceFieldMaxCreditsPerPeriod] removes the spend limit. Set a limit
//     with UpdateSource's MaxCreditsPerPeriod.
//
// It returns a [*ConfigurationError], without sending a request, when no
// field is given or a field is not one of these constants.
func (c *Client) ClearSourceFields(ctx context.Context, sourceID string, fields ...SourceField) (*SourceResponse, error) {
	if len(fields) == 0 {
		return nil, &ConfigurationError{Message: "ClearSourceFields needs at least one field to clear"}
	}
	payload := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case SourceFieldRetentionDays, SourceFieldMaxCreditsPerPeriod:
			payload[string(f)] = nil
		default:
			return nil, &ConfigurationError{Message: fmt.Sprintf("ClearSourceFields cannot clear %q; use one of the SourceField constants", string(f))}
		}
	}
	var out SourceResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/sources/%s", pathSegment(sourceID)), nil, payload, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClearSourceRetention removes a source's retention window, so its content is
// kept indefinitely. It is ClearSourceFields with [SourceFieldRetentionDays].
func (c *Client) ClearSourceRetention(ctx context.Context, sourceID string) (*SourceResponse, error) {
	return c.ClearSourceFields(ctx, sourceID, SourceFieldRetentionDays)
}

// DeleteSource deletes a source.
func (c *Client) DeleteSource(ctx context.Context, sourceID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/sources/%s", pathSegment(sourceID)), nil, nil, nil, nil)
}

// ── Source Exports ──────────────────────────────────────────────────────────

// ListSourceExports lists exports for a source.
func (c *Client) ListSourceExports(ctx context.Context, sourceID string, opts ListOptions) (*ExportListResponse, error) {
	var out ExportListResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/exports", pathSegment(sourceID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSourceExport creates a new export for a source.
func (c *Client) CreateSourceExport(ctx context.Context, sourceID string, body CreateExportRequest) (*ExportResponse, error) {
	var out ExportResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/exports", pathSegment(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSourceExport retrieves a source export by ID.
func (c *Client) GetSourceExport(ctx context.Context, sourceID, exportID string) (*ExportResponse, error) {
	var out ExportResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/exports/%s", pathSegment(sourceID), pathSegment(exportID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelSourceExport cancels a source export.
func (c *Client) CancelSourceExport(ctx context.Context, sourceID, exportID string) (*ExportResponse, error) {
	var out ExportResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/exports/%s/cancel", pathSegment(sourceID), pathSegment(exportID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSourceExport deletes a source export.
func (c *Client) DeleteSourceExport(ctx context.Context, sourceID, exportID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/sources/%s/exports/%s", pathSegment(sourceID), pathSegment(exportID)), nil, nil, nil, nil)
}

// DownloadSourceExport downloads a source export. Returns the raw HTTP response
// so the caller can stream the body. The caller must close the response body.
func (c *Client) DownloadSourceExport(ctx context.Context, sourceID, exportID string) (*http.Response, error) {
	return c.doRaw(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/exports/%s/download", pathSegment(sourceID), pathSegment(exportID)), nil)
}

// EstimateSourceExport estimates the cost/size of a source export.
func (c *Client) EstimateSourceExport(ctx context.Context, sourceID string, body EstimateExportRequest) (*EstimateExportResponse, error) {
	var out EstimateExportResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/exports/estimate", pathSegment(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Source Embedding Migrations ─────────────────────────────────────────────

// GetSourceEmbeddingMigration retrieves the embedding migration status for a source.
func (c *Client) GetSourceEmbeddingMigration(ctx context.Context, sourceID string) (*SourceEmbeddingMigrationResponse, error) {
	var out SourceEmbeddingMigrationResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/embedding-migration", pathSegment(sourceID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartSourceEmbeddingMigration starts an embedding migration for a source.
func (c *Client) StartSourceEmbeddingMigration(ctx context.Context, sourceID string, body StartSourceEmbeddingMigrationRequest) (*SourceEmbeddingMigrationResponse, error) {
	var out SourceEmbeddingMigrationResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/embedding-migration", pathSegment(sourceID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelSourceEmbeddingMigration cancels an in-progress embedding migration.
func (c *Client) CancelSourceEmbeddingMigration(ctx context.Context, sourceID string) (*SourceEmbeddingMigrationResponse, error) {
	var out SourceEmbeddingMigrationResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/sources/%s/embedding-migration/cancel", pathSegment(sourceID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Source Contents ─────────────────────────────────────────────────────────

// ListSourceContentsOptions controls query parameters for [Client.ListSourceContents].
type ListSourceContentsOptions struct {
	// Page is the 1-indexed page number. Zero omits the parameter.
	Page int
	// Limit is the page size (1-100, default 20). Zero omits the parameter.
	Limit int
	// Sort is the sort field: "created_at", "title" or "status".
	Sort string
	// Order is the sort direction: "asc" or "desc".
	Order string
	// Status keeps only one status: "pending", "fetching", "transcribing",
	// "scanning", "indexing", "completed" or "failed".
	Status string
	// ContentVersionIDs keeps only these items — the content_version_id values
	// the upload methods return — to poll a batch of uploads in one request.
	// Nil applies no filter; a non-nil slice with no non-empty id matches
	// nothing, so an empty page is returned without a request. Empty ids are
	// dropped. Keep it to about 100: the ids travel in the query string, and a
	// URL over 8,192 bytes is rejected with a 414. The API itself accepts at
	// most 500.
	ContentVersionIDs []string
}

// ListSourceContents lists a source's content items and their indexing status.
// The response is the {data, pagination} envelope on every API version.
func (c *Client) ListSourceContents(ctx context.Context, sourceID string, opts ListSourceContentsOptions) (*SourceContentStatusListResponse, error) {
	var ids []string
	for _, id := range opts.ContentVersionIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if opts.ContentVersionIDs != nil && len(ids) == 0 {
		// An empty filter encodes as no parameter, which the API reads as unfiltered.
		page := PaginationResponse{Page: 1, Limit: 20}
		if opts.Page > 0 {
			page.Page = opts.Page
		}
		if opts.Limit > 0 {
			page.Limit = opts.Limit
		}
		return &SourceContentStatusListResponse{Data: []SourceContentStatusResponse{}, Pagination: page}, nil
	}
	q := url.Values{}
	if opts.Page > 0 {
		q["page"] = []string{fmt.Sprintf("%d", opts.Page)}
	}
	if opts.Limit > 0 {
		q["limit"] = []string{fmt.Sprintf("%d", opts.Limit)}
	}
	if opts.Sort != "" {
		q["sort"] = []string{opts.Sort}
	}
	if opts.Order != "" {
		q["order"] = []string{opts.Order}
	}
	if opts.Status != "" {
		q["status"] = []string{opts.Status}
	}
	if len(ids) > 0 {
		q["content_version_id"] = ids
	}
	var out SourceContentStatusListResponse
	if err := c.doValues(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/contents", pathSegment(sourceID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSourceContentStatus retrieves one content item's indexing status, keyed by
// the content_version_id an upload returned.
func (c *Client) GetSourceContentStatus(ctx context.Context, sourceID, contentVersionID string) (*SourceContentStatusResponse, error) {
	var out SourceContentStatusResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/contents/%s", pathSegment(sourceID), pathSegment(contentVersionID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEmailSourceRejections lists mail an email content source refused, newest
// first. Each entry says who sent the message and why it was refused, never its
// subject or body. To accept a refused sender, add its FromAddress or that
// address's domain to the source's EmailAllowedSenders — which only helps when
// Authenticated is true.
//
// Requires an account owner or administrator (403 otherwise); the API answers
// 404 for a source that is not an email source. The response is the
// {data, pagination} envelope on every API version.
func (c *Client) ListEmailSourceRejections(ctx context.Context, sourceID string, opts ListOptions) (*EmailSourceRejectionListResponse, error) {
	var out EmailSourceRejectionListResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/sources/%s/email-rejections", pathSegment(sourceID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
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
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/contents/%s", pathSegment(contentVersionID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceContentWithInlineText replaces content with inline text.
func (c *Client) ReplaceContentWithInlineText(ctx context.Context, contentVersionID string, body InlineTextReplaceRequest) (*ContentFileUploadResponse, error) {
	var out ContentFileUploadResponse
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/contents/%s", pathSegment(contentVersionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteContent deletes a content version.
func (c *Client) DeleteContent(ctx context.Context, contentVersionID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/contents/%s", pathSegment(contentVersionID)), nil, nil, nil, nil)
}

// ListContentEmbeddings lists embeddings for a content version.
func (c *Client) ListContentEmbeddings(ctx context.Context, contentVersionID string, opts ListOptions) (*ContentEmbeddingsListResponse, error) {
	var out ContentEmbeddingsListResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/contents/%s/embeddings", pathSegment(contentVersionID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListContentAttachments lists the files attached to a content item: those an
// agent's write_content_attachment step attached, the files sent with an email,
// the feed files an RSS source keeps, a web page's media and the images inside
// documents. Each entry says whether the file is indexed and, when indexing
// failed, why. The response is the {data, pagination} envelope on every API
// version.
func (c *Client) ListContentAttachments(ctx context.Context, contentVersionID string, opts ListOptions) (*ContentAttachmentsListResponse, error) {
	var out ContentAttachmentsListResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/contents/%s/attachments", pathSegment(contentVersionID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ContentAttachmentOptions controls the text range [Client.GetContentAttachment]
// returns. A nil field omits the parameter and the API applies its default.
type ContentAttachmentOptions struct {
	// Start is the offset of the first character of text returned (default 0).
	Start *int
	// End is the offset after the last character returned (default 5000).
	// A pointer to 0 returns no text, for a fetch of the metadata alone.
	End *int
}

// GetContentAttachment fetches one file attached to a content item: its
// indexing status and reason, its metadata, and the text it is indexed by.
// attachmentID is the Id of an entry from [Client.ListContentAttachments].
// opts.Start and opts.End page through long text. A file that was not indexed
// has no text.
//
// Unlike [Client.GetContentDetail], whose int arguments omit a zero, a pointer
// to 0 is sent as is. The API answers 402 (an [*APIStatusError]) when the
// account has no credits left.
func (c *Client) GetContentAttachment(ctx context.Context, contentVersionID, attachmentID string, opts ContentAttachmentOptions) (*ContentAttachmentDetailResponse, error) {
	q := map[string]string{}
	if opts.Start != nil {
		q["start"] = fmt.Sprintf("%d", *opts.Start)
	}
	if opts.End != nil {
		q["end"] = fmt.Sprintf("%d", *opts.End)
	}
	var out ContentAttachmentDetailResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/contents/%s/attachments/%s", pathSegment(contentVersionID), pathSegment(attachmentID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListContentAttachmentEmbeddings lists the embeddings (chunk vectors) an
// attached file is indexed by. The list is empty for a file with no completed
// index. The API answers 402 (an [*APIStatusError]) when the account has no
// credits left.
func (c *Client) ListContentAttachmentEmbeddings(ctx context.Context, contentVersionID, attachmentID string, opts ListOptions) (*ContentEmbeddingsListResponse, error) {
	var out ContentEmbeddingsListResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/contents/%s/attachments/%s/embeddings", pathSegment(contentVersionID), pathSegment(attachmentID)), listQuery(opts.Page, opts.Limit), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadContentAttachment downloads an attached file's bytes. Returns the raw
// HTTP response so the caller can stream the body; the caller must close it.
//
// The API answers 403 (an [*APIStatusError]) when the file is withheld: a
// governance policy blocked or redacted it, or it arrived from outside and is
// still being indexed. The attachment's FileWithheld field says which. It
// answers 402 when the account has no credits left.
func (c *Client) DownloadContentAttachment(ctx context.Context, contentVersionID, attachmentID string) (*http.Response, error) {
	return c.doRaw(ctx, http.MethodGet, fmt.Sprintf("/contents/%s/attachments/%s/file", pathSegment(contentVersionID), pathSegment(attachmentID)), nil)
}

// ── Solutions ───────────────────────────────────────────────────────────────

// ListSolutions lists solutions.
//
// Use [Client.ListSolutionsWithOptions] to search them by name.
func (c *Client) ListSolutions(ctx context.Context, opts SortableListOptions) (*SolutionListResponse, error) {
	return c.ListSolutionsWithOptions(ctx, ListSolutionsOptions{SortableListOptions: opts})
}

// ListSolutionsOptions controls query parameters for [Client.ListSolutionsWithOptions].
type ListSolutionsOptions struct {
	SortableListOptions
	// Search keeps only solutions whose name contains this text, case-insensitively
	// (1-200 characters).
	Search string
}

// ListSolutionsWithOptions lists solutions, optionally searched by name.
func (c *Client) ListSolutionsWithOptions(ctx context.Context, opts ListSolutionsOptions) (*SolutionListResponse, error) {
	q := sortableListQuery(opts.SortableListOptions)
	if opts.Search != "" {
		q["search"] = opts.Search
	}
	var out SolutionListResponse
	if err := c.do(ctx, http.MethodGet, "/solutions", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSolution creates a new solution.
func (c *Client) CreateSolution(ctx context.Context, body CreateSolutionRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodPost, "/solutions", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSolution retrieves a solution by ID.
func (c *Client) GetSolution(ctx context.Context, solutionID string) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/solutions/%s", pathSegment(solutionID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSolution updates a solution.
func (c *Client) UpdateSolution(ctx context.Context, solutionID string, body UpdateSolutionRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/solutions/%s", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSolution deletes a solution.
func (c *Client) DeleteSolution(ctx context.Context, solutionID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s", pathSegment(solutionID)), nil, nil, nil, nil)
}

// LinkAgentsToSolution links agents to a solution.
func (c *Client) LinkAgentsToSolution(ctx context.Context, solutionID string, body LinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/agents", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnlinkAgentsFromSolution unlinks agents from a solution.
func (c *Client) UnlinkAgentsFromSolution(ctx context.Context, solutionID string, body UnlinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s/agents", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LinkKnowledgeBasesToSolution links knowledge bases to a solution.
func (c *Client) LinkKnowledgeBasesToSolution(ctx context.Context, solutionID string, body LinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/knowledge-bases", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnlinkKnowledgeBasesFromSolution unlinks knowledge bases from a solution.
func (c *Client) UnlinkKnowledgeBasesFromSolution(ctx context.Context, solutionID string, body UnlinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s/knowledge-bases", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LinkSourceConnectionsToSolution links source connections to a solution.
func (c *Client) LinkSourceConnectionsToSolution(ctx context.Context, solutionID string, body LinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/source-connections", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnlinkSourceConnectionsFromSolution unlinks source connections from a solution.
func (c *Client) UnlinkSourceConnectionsFromSolution(ctx context.Context, solutionID string, body UnlinkResourcesRequest) (*SolutionResponse, error) {
	var out SolutionResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/solutions/%s/source-connections", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Solution Conversations ──────────────────────────────────────────────────

// ListSolutionConversations lists conversations for a solution.
func (c *Client) ListSolutionConversations(ctx context.Context, solutionID string) ([]SolutionConversationResponse, error) {
	var out []SolutionConversationResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/solutions/%s/conversations", pathSegment(solutionID)), nil, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddSolutionConversationTurn adds a conversation turn to a solution.
func (c *Client) AddSolutionConversationTurn(ctx context.Context, solutionID string, body AddConversationTurnRequest) (*SolutionConversationResponse, error) {
	var out SolutionConversationResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/conversations", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkSolutionConversationTurn marks a conversation turn (e.g. as accepted/rejected).
func (c *Client) MarkSolutionConversationTurn(ctx context.Context, solutionID, conversationID string, body MarkConversationTurnRequest) error {
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/solutions/%s/conversations/%s", pathSegment(solutionID), pathSegment(conversationID)), nil, body, nil, nil)
}

// ── Solution AI Assistant ───────────────────────────────────────────────────

// GenerateSolutionAiPlan uses the AI assistant to generate a plan for a solution.
func (c *Client) GenerateSolutionAiPlan(ctx context.Context, solutionID string, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/generate", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateSolutionAiKnowledgeBase uses the AI assistant to generate a knowledge base plan for a solution.
func (c *Client) GenerateSolutionAiKnowledgeBase(ctx context.Context, solutionID string, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/knowledge-base", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateSolutionAiSource uses the AI assistant to generate a source plan for a solution.
func (c *Client) GenerateSolutionAiSource(ctx context.Context, solutionID string, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/source", pathSegment(solutionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptSolutionAiPlan accepts an AI-generated solution plan.
func (c *Client) AcceptSolutionAiPlan(ctx context.Context, solutionID, conversationID string, body AiAssistantAcceptRequest) (*AiAssistantAcceptResponse, error) {
	var out AiAssistantAcceptResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/%s/accept", pathSegment(solutionID), pathSegment(conversationID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeclineSolutionAiPlan declines an AI-generated solution plan.
func (c *Client) DeclineSolutionAiPlan(ctx context.Context, solutionID, conversationID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/solutions/%s/ai-assistant/%s/decline", pathSegment(solutionID), pathSegment(conversationID)), nil, nil, nil, nil)
}

// ── Governance AI Assistant ─────────────────────────────────────────────────

// GenerateGovernanceAiPlan uses the governance AI assistant to generate a plan.
func (c *Client) GenerateGovernanceAiPlan(ctx context.Context, body GovernanceAiAssistantRequest) (*GovernanceAiAssistantResponse, error) {
	var out GovernanceAiAssistantResponse
	if err := c.do(ctx, http.MethodPost, "/governance/ai-assistant", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListGovernanceAiConversations lists governance AI assistant conversations.
//
// Use [Client.ListGovernanceAiConversationsWithOptions] to set how many.
func (c *Client) ListGovernanceAiConversations(ctx context.Context) ([]GovernanceConversationResponse, error) {
	return c.ListGovernanceAiConversationsWithOptions(ctx, ListGovernanceAiConversationsOptions{})
}

// ListGovernanceAiConversationsOptions controls query parameters for
// [Client.ListGovernanceAiConversationsWithOptions].
type ListGovernanceAiConversationsOptions struct {
	// Limit is the number of conversations (server default 20). Zero omits it.
	Limit int
}

// ListGovernanceAiConversationsWithOptions lists governance AI assistant
// conversations, up to Limit.
func (c *Client) ListGovernanceAiConversationsWithOptions(ctx context.Context, opts ListGovernanceAiConversationsOptions) ([]GovernanceConversationResponse, error) {
	q := map[string]string{}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	var out []GovernanceConversationResponse
	if err := c.doList(ctx, http.MethodGet, "/governance/ai-assistant/conversations", q, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AcceptGovernanceAiPlan accepts a governance AI plan.
func (c *Client) AcceptGovernanceAiPlan(ctx context.Context, conversationID string) (*GovernanceAiAcceptResponse, error) {
	var out GovernanceAiAcceptResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/governance/ai-assistant/%s/accept", pathSegment(conversationID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeclineGovernanceAiPlan declines a governance AI plan.
func (c *Client) DeclineGovernanceAiPlan(ctx context.Context, conversationID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/governance/ai-assistant/%s/decline", pathSegment(conversationID)), nil, nil, nil, nil)
}

// ── Alerts ──────────────────────────────────────────────────────────────────

// ListAlertsOptions controls optional query parameters for ListAlerts.
type ListAlertsOptions struct {
	ListOptions
	// Status filters alerts by status (e.g. "active", "resolved").
	Status string
	// Severity filters alerts by severity.
	Severity string
	// AgentID keeps only alerts for this agent.
	AgentID string
	// SourceConnectionID keeps only alerts for this source connection.
	SourceConnectionID string
	// TimeFrom keeps alerts from this time on (ISO 8601 date-time).
	TimeFrom string
	// TimeTo keeps alerts up to this time (ISO 8601 date-time).
	TimeTo string
}

// ListAlerts lists alerts.
// Severity is accepted and dropped: GET /alerts declares no such filter, so it
// never filtered anything, and sending it is a 422 once Options.APIVersion is
// 2026-07-27 or later.
func (c *Client) ListAlerts(ctx context.Context, opts ListAlertsOptions) (json.RawMessage, error) {
	q := listQuery(opts.Page, opts.Limit)
	if opts.Status != "" {
		q["status"] = opts.Status
	}
	if opts.AgentID != "" {
		q["agent_id"] = opts.AgentID
	}
	if opts.SourceConnectionID != "" {
		q["source_connection_id"] = opts.SourceConnectionID
	}
	if opts.TimeFrom != "" {
		q["time_from"] = opts.TimeFrom
	}
	if opts.TimeTo != "" {
		q["time_to"] = opts.TimeTo
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/alerts", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAlert retrieves an alert by ID.
func (c *Client) GetAlert(ctx context.Context, alertID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/alerts/%s", pathSegment(alertID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ChangeAlertStatus changes the status of an alert.
func (c *Client) ChangeAlertStatus(ctx context.Context, alertID string, body ChangeStatusRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/status", pathSegment(alertID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddAlertComment adds a comment to an alert.
func (c *Client) AddAlertComment(ctx context.Context, alertID string, body AddCommentRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/comments", pathSegment(alertID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SubscribeToAlert subscribes to an alert.
func (c *Client) SubscribeToAlert(ctx context.Context, alertID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/subscribe", pathSegment(alertID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnsubscribeFromAlert unsubscribes from an alert.
func (c *Client) UnsubscribeFromAlert(ctx context.Context, alertID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/alerts/%s/unsubscribe", pathSegment(alertID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Alert Configs ───────────────────────────────────────────────────────────

// ListAlertConfigs lists alert configurations.
// The configurations arrive under "configs" alongside "total" by default. Once
// the caller opts in with Options.APIVersion of 2026-07-27 or later the endpoint
// returns the canonical {data, pagination} envelope instead, so the top-level
// key changes.
//
// Use [Client.ListAlertConfigsWithOptions] to filter them.
func (c *Client) ListAlertConfigs(ctx context.Context, opts ListOptions) (json.RawMessage, error) {
	return c.ListAlertConfigsWithOptions(ctx, ListAlertConfigsOptions{ListOptions: opts})
}

// ListAlertConfigsOptions controls query parameters for
// [Client.ListAlertConfigsWithOptions].
type ListAlertConfigsOptions struct {
	ListOptions
	// AgentID keeps only configurations for this agent.
	AgentID string
	// SourceConnectionID keeps only configurations for this source connection.
	SourceConnectionID string
	// Scope set to "source" lists the account-level source alert configurations.
	Scope string
}

// ListAlertConfigsWithOptions lists alert configurations, filtered by agent,
// source connection or scope. The response shape is as for [Client.ListAlertConfigs].
func (c *Client) ListAlertConfigsWithOptions(ctx context.Context, opts ListAlertConfigsOptions) (json.RawMessage, error) {
	body, _, err := c.alertConfigsBody(ctx, opts)
	return rawJSON(body, err)
}

// alertConfigsBody is the request behind [Client.ListAlertConfigs] and its typed form.
func (c *Client) alertConfigsBody(ctx context.Context, opts ListAlertConfigsOptions) ([]byte, *url.URL, error) {
	q := listQuery(opts.Page, opts.Limit)
	if opts.AgentID != "" {
		q["agent_id"] = opts.AgentID
	}
	if opts.SourceConnectionID != "" {
		q["source_connection_id"] = opts.SourceConnectionID
	}
	if opts.Scope != "" {
		q["scope"] = opts.Scope
	}
	return c.doBytes(ctx, http.MethodGet, "/alerts/configs", queryValues(q), nil, nil)
}

// CreateAlertConfig creates a new alert configuration.
func (c *Client) CreateAlertConfig(ctx context.Context, body CreateAlertConfigRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/alerts/configs", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAlertConfig retrieves an alert configuration by ID.
func (c *Client) GetAlertConfig(ctx context.Context, configID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/alerts/configs/%s", pathSegment(configID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAlertConfig updates an alert configuration.
func (c *Client) UpdateAlertConfig(ctx context.Context, configID string, body UpdateAlertConfigRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/alerts/configs/%s", pathSegment(configID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteAlertConfig deletes an alert configuration.
func (c *Client) DeleteAlertConfig(ctx context.Context, configID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/alerts/configs/%s", pathSegment(configID)), nil, nil, nil, nil)
}

// ── Alert Preferences ───────────────────────────────────────────────────────

// ListOrganizationAlertPreferences lists organization alert preferences.
//
// Use [Client.ListOrganizationAlertPreferencesWithOptions] to filter them.
func (c *Client) ListOrganizationAlertPreferences(ctx context.Context) (*OrganizationAlertPreferenceListResponse, error) {
	return c.ListOrganizationAlertPreferencesWithOptions(ctx, ListOrganizationAlertPreferencesOptions{})
}

// ListOrganizationAlertPreferencesOptions controls query parameters for
// [Client.ListOrganizationAlertPreferencesWithOptions].
type ListOrganizationAlertPreferencesOptions struct {
	// OrganizationID keeps only preferences for this organization.
	OrganizationID string
	// IncludeDefaults adds the default subscribed entries for every alert type.
	IncludeDefaults bool
}

// ListOrganizationAlertPreferencesWithOptions lists organization alert
// preferences, optionally for one organization and with the defaults included.
func (c *Client) ListOrganizationAlertPreferencesWithOptions(ctx context.Context, opts ListOrganizationAlertPreferencesOptions) (*OrganizationAlertPreferenceListResponse, error) {
	q := map[string]string{}
	if opts.OrganizationID != "" {
		q["organization_id"] = opts.OrganizationID
	}
	if opts.IncludeDefaults {
		q["include_defaults"] = "true"
	}
	var out OrganizationAlertPreferenceListResponse
	if err := c.doList(ctx, http.MethodGet, "/alerts/organization-preferences/list", q, nil, "preferences", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateOrganizationAlertPreference updates an organization alert preference.
func (c *Client) UpdateOrganizationAlertPreference(ctx context.Context, organizationID, alertType string, body UpdateOrganizationAlertPreferenceRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/alerts/organization-preferences/%s/%s", pathSegment(organizationID), pathSegment(alertType)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Models & Alerts ─────────────────────────────────────────────────────────

// ListModelAlerts lists model alerts.
// The endpoint declares limit/offset, not page, so opts.Page is translated —
// previously it was ignored and every page after the first returned page 1.
//
// Use [Client.ListModelAlertsWithOptions] to filter them.
func (c *Client) ListModelAlerts(ctx context.Context, opts ListOptions) (json.RawMessage, error) {
	return c.ListModelAlertsWithOptions(ctx, ListModelAlertsOptions{ListOptions: opts})
}

// ListModelAlertsOptions controls query parameters for
// [Client.ListModelAlertsWithOptions]. Page is translated to the endpoint's
// offset as for [Client.ListModelAlerts].
type ListModelAlertsOptions struct {
	ListOptions
	// AgentID keeps only alerts for this agent.
	AgentID string
	// UnreadOnly keeps only unread alerts.
	UnreadOnly bool
}

// ListModelAlertsWithOptions lists model alerts, optionally for one agent or
// unread only.
func (c *Client) ListModelAlertsWithOptions(ctx context.Context, opts ListModelAlertsOptions) (json.RawMessage, error) {
	body, _, err := c.modelAlertsBody(ctx, opts)
	return rawJSON(body, err)
}

// modelAlertsBody is the request behind [Client.ListModelAlerts] and its typed form.
func (c *Client) modelAlertsBody(ctx context.Context, opts ListModelAlertsOptions) ([]byte, *url.URL, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	q := map[string]string{}
	if opts.Page > 1 {
		q["offset"] = fmt.Sprintf("%d", (opts.Page-1)*limit)
	}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	if opts.AgentID != "" {
		q["agent_id"] = opts.AgentID
	}
	if opts.UnreadOnly {
		q["unread_only"] = "true"
	}
	return c.doBytes(ctx, http.MethodGet, "/models/alerts", queryValues(q), nil, nil)
}

// MarkAllModelAlertsRead marks all model alerts as read.
func (c *Client) MarkAllModelAlertsRead(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/models/alerts/mark-all-read", nil, nil, nil, nil)
}

// GetUnreadModelAlertCount retrieves the count of unread model alerts.
func (c *Client) GetUnreadModelAlertCount(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/models/alerts/unread-count", nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkModelAlertRead marks a single model alert as read.
func (c *Client) MarkModelAlertRead(ctx context.Context, alertID string) error {
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/models/alerts/%s/read", pathSegment(alertID)), nil, nil, nil, nil)
}

// GetModelRecommendations retrieves recommendations for a model.
//
// Use [Client.GetModelRecommendationsWithOptions] to require capabilities.
func (c *Client) GetModelRecommendations(ctx context.Context, modelID string) (json.RawMessage, error) {
	return c.GetModelRecommendationsWithOptions(ctx, modelID, ModelRecommendationsOptions{})
}

// ModelRecommendationsOptions controls query parameters for
// [Client.GetModelRecommendationsWithOptions]. A nil field is not sent.
type ModelRecommendationsOptions struct {
	// RequireToolUse recommends only models that support tool use.
	RequireToolUse *bool
	// RequireStructuredOutput recommends only models that support structured output.
	RequireStructuredOutput *bool
	// RequireThinking recommends only models that support thinking/reasoning.
	RequireThinking *bool
	// MinContextTokens is the minimum context window size in tokens.
	MinContextTokens *int
	// MinOutputTokens is the minimum output token limit.
	MinOutputTokens *int
}

// GetModelRecommendationsWithOptions retrieves recommendations for a model,
// limited to models with the required capabilities.
func (c *Client) GetModelRecommendationsWithOptions(ctx context.Context, modelID string, opts ModelRecommendationsOptions) (json.RawMessage, error) {
	q := map[string]string{}
	if opts.RequireToolUse != nil {
		q["require_tool_use"] = fmt.Sprintf("%t", *opts.RequireToolUse)
	}
	if opts.RequireStructuredOutput != nil {
		q["require_structured_output"] = fmt.Sprintf("%t", *opts.RequireStructuredOutput)
	}
	if opts.RequireThinking != nil {
		q["require_thinking"] = fmt.Sprintf("%t", *opts.RequireThinking)
	}
	if opts.MinContextTokens != nil {
		q["min_context_tokens"] = fmt.Sprintf("%d", *opts.MinContextTokens)
	}
	if opts.MinOutputTokens != nil {
		q["min_output_tokens"] = fmt.Sprintf("%d", *opts.MinOutputTokens)
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/models/%s/recommendations", pathSegment(modelID)), q, nil, nil, &out); err != nil {
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
	// SupportsInputMedia filters to models that accept this input modality: a
	// coarse kind ("image", "audio", "video", "pdf") or a full MIME type.
	SupportsInputMedia string
	// SupportsOutputMedia filters to models that produce this output modality: a
	// coarse kind ("image", "audio", "video") or a full MIME type.
	SupportsOutputMedia string
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
	if opts.SupportsInputMedia != "" {
		q["supports_input_media"] = opts.SupportsInputMedia
	}
	if opts.SupportsOutputMedia != "" {
		q["supports_output_media"] = opts.SupportsOutputMedia
	}
	var out []ProviderGroupResponse
	if err := c.doList(ctx, http.MethodGet, "/models", q, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetModel retrieves full details for a specific model.
func (c *Client) GetModel(ctx context.Context, modelID string) (*PromptModelResponse, error) {
	var out PromptModelResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/models/%s/details", pathSegment(modelID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetGenerationTiers lists the media-generation quality tiers and the model
// and cost each resolves to. Global routing and pricing; read-only.
func (c *Client) GetGenerationTiers(ctx context.Context) (json.RawMessage, error) {
	body, _, err := c.generationTiersBody(ctx)
	return rawJSON(body, err)
}

// generationTiersBody is the request behind [Client.GetGenerationTiers] and its typed form.
func (c *Client) generationTiersBody(ctx context.Context) ([]byte, *url.URL, error) {
	return c.doBytes(ctx, http.MethodGet, "/models/generation-tiers", nil, nil, nil)
}

// ListEmbeddingModelsOptions controls query parameters for [Client.ListEmbeddingModels].
type ListEmbeddingModelsOptions struct {
	// SupportsInputMedia keeps only embedders that can index this input
	// modality: a coarse kind (text, image, video, audio) or a full MIME type.
	SupportsInputMedia string
}

// ListEmbeddingModels lists the embedding models a source can index with, and
// their pricing.
//
// Either wire shape is accepted: read the models through
// [EmbeddingModelListResponse.Items].
func (c *Client) ListEmbeddingModels(ctx context.Context, opts ListEmbeddingModelsOptions) (*EmbeddingModelListResponse, error) {
	q := map[string]string{}
	if opts.SupportsInputMedia != "" {
		q["supports_input_media"] = opts.SupportsInputMedia
	}
	var out EmbeddingModelListResponse
	if err := c.doList(ctx, http.MethodGet, "/models/embedders", q, nil, "models", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRerankerModels lists the reranker models a knowledge base can use, and
// their pricing.
//
// Either wire shape is accepted: read the models through
// [RerankerModelListResponse.Items].
func (c *Client) ListRerankerModels(ctx context.Context) (*RerankerModelListResponse, error) {
	var out RerankerModelListResponse
	if err := c.doList(ctx, http.MethodGet, "/models/rerankers", nil, nil, "models", &out); err != nil {
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
	body, _, err := c.experimentsBody(ctx, opts)
	return rawJSON(body, err)
}

// experimentsBody is the request behind [Client.ListExperiments] and its typed form.
func (c *Client) experimentsBody(ctx context.Context, opts ListExperimentsOptions) ([]byte, *url.URL, error) {
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
	return c.doBytes(ctx, http.MethodGet, "/models/playground/experiments", queryValues(q), nil, nil)
}

// CreateExperiment creates a model playground experiment.
func (c *Client) CreateExperiment(ctx context.Context, body PlaygroundCreateRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/models/playground/experiments", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetExperiment retrieves a model playground experiment by ID.
func (c *Client) GetExperiment(ctx context.Context, experimentID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/models/playground/experiments/%s", pathSegment(experimentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CancelExperiment cancels a running model playground experiment.
func (c *Client) CancelExperiment(ctx context.Context, experimentID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/models/playground/experiments/%s/cancel", pathSegment(experimentID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteExperiment soft-deletes a model playground experiment, removing it from
// list/detail views while preserving audit history.
func (c *Client) DeleteExperiment(ctx context.Context, experimentID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/models/playground/experiments/%s", pathSegment(experimentID)), nil, nil, nil, nil)
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
	// `q` is required by the spec. Omitting it when empty only defers the
	// failure to a 422 whose message points at the wire name, not the field.
	if opts.Query == "" {
		return nil, fmt.Errorf("seclai: SearchOptions.Query is required")
	}
	q := map[string]string{"q": opts.Query}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	if opts.EntityType != "" {
		q["entity_type"] = opts.EntityType
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/search", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DocsSearchOptions controls query parameters for [Client.SearchDocs].
type DocsSearchOptions struct {
	// Query is the search query (1-200 characters).
	Query string
	// Mode is the search strategy: "keyword" (default) or "semantic".
	Mode string
	// Limit is the maximum number of results (1-20). Zero omits the parameter.
	Limit int
}

// SearchDocs searches the Seclai documentation by content. Mode is keyword
// (default; titles and summaries) or semantic (body content by meaning).
// Documentation is global, so results are not account-scoped.
func (c *Client) SearchDocs(ctx context.Context, opts DocsSearchOptions) (json.RawMessage, error) {
	// `q` is required by the spec — see [Client.Search].
	if opts.Query == "" {
		return nil, fmt.Errorf("seclai: DocsSearchOptions.Query is required")
	}
	q := map[string]string{"q": opts.Query}
	if opts.Mode != "" {
		q["mode"] = opts.Mode
	}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/docs-search", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Email Domains ─────────────────────────────────────────────────────────────

// DmarcOptions controls the reporting window for [Client.GetDmarcSummary].
type DmarcOptions struct {
	// Days is the window length in days. Zero omits the parameter (server default 30).
	Days int
	// TopSources is how many top failing sources to return. Zero omits the parameter.
	TopSources int
}

// ListEmailDomains lists the account's vanity and custom agent-email domains
// with verification status, required DNS records, and plan capabilities.
// Requires a user-bound credential; an account-only API key is refused with 403.
func (c *Client) ListEmailDomains(ctx context.Context) (*EmailDomainsListResponse, error) {
	var out EmailDomainsListResponse
	if err := c.doList(ctx, http.MethodGet, "/email-domains", nil, nil, "domains", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddEmailDomain adds and provisions a vanity subdomain or a custom domain,
// standing up the SES identity and DNS. Requires an account owner or admin.
func (c *Client) AddEmailDomain(ctx context.Context, body AddEmailDomainRequest) (*EmailDomainResponse, error) {
	var out EmailDomainResponse
	if err := c.do(ctx, http.MethodPost, "/email-domains", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveEmailDomain removes a domain and tears down its SES identity and DNS.
// Returns a cleanup note when the domain was Seclai-managed, reminding the caller
// to delete the registrar NS delegation record.
func (c *Client) RemoveEmailDomain(ctx context.Context, domainID string) (*RemoveEmailDomainResponse, error) {
	var out RemoveEmailDomainResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/email-domains/%s", pathSegment(domainID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyEmailDomain re-polls SES and DNS for this domain now instead of waiting
// for the background sweep, returning updated status and record check results.
func (c *Client) VerifyEmailDomain(ctx context.Context, domainID string) (*EmailDomainResponse, error) {
	var out EmailDomainResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/email-domains/%s/verify", pathSegment(domainID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetPrimaryEmailDomain promotes a verified domain to the account's primary
// domain, so agent email sends from and receives on it instead of the shared
// agent.seclai.com.
func (c *Client) SetPrimaryEmailDomain(ctx context.Context, domainID string) (*EmailDomainResponse, error) {
	var out EmailDomainResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/email-domains/%s/primary", pathSegment(domainID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UseSharedEmailDomain clears the account's primary domain so agent email
// reverts to the shared agent.seclai.com scheme, without removing the configured
// domains — they stay verified and can be promoted again later.
func (c *Client) UseSharedEmailDomain(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/email-domains/use-shared-domain", nil, nil, nil, nil)
}

// SendEmailDomainTestEmail sends a test message from a verified domain to the
// account owner's address, confirming end-to-end that the domain sends. It never
// sends anywhere else, so it cannot be used as an open relay.
func (c *Client) SendEmailDomainTestEmail(ctx context.Context, domainID string) (*SendTestEmailResponse, error) {
	var out SendTestEmailResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/email-domains/%s/test-email", pathSegment(domainID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDmarcSummary returns pass rate, disposition breakdown and top failing
// source IPs from the DMARC aggregate reports. A self-managed custom domain keeps
// its own DMARC reporting and returns an all-zero summary.
func (c *Client) GetDmarcSummary(ctx context.Context, domainID string, opts DmarcOptions) (*DmarcSummaryResponse, error) {
	q := map[string]string{}
	if opts.Days > 0 {
		q["days"] = fmt.Sprintf("%d", opts.Days)
	}
	if opts.TopSources > 0 {
		q["top_sources"] = fmt.Sprintf("%d", opts.TopSources)
	}
	var out DmarcSummaryResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/email-domains/%s/dmarc", pathSegment(domainID)), q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Top-Level AI Assistant ──────────────────────────────────────────────────

// SubmitAiFeedback submits feedback to the AI assistant.
func (c *Client) SubmitAiFeedback(ctx context.Context, body AiAssistantFeedbackRequest) (*AiAssistantFeedbackResponse, error) {
	var out AiAssistantFeedbackResponse
	if err := c.do(ctx, http.MethodPost, "/ai-assistant/feedback", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantKnowledgeBase generates a knowledge base plan via the top-level AI assistant.
func (c *Client) AiAssistantKnowledgeBase(ctx context.Context, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.do(ctx, http.MethodPost, "/ai-assistant/knowledge-base", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantSource generates a source plan via the top-level AI assistant.
func (c *Client) AiAssistantSource(ctx context.Context, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.do(ctx, http.MethodPost, "/ai-assistant/source", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantSolution generates a solution plan via the top-level AI assistant.
func (c *Client) AiAssistantSolution(ctx context.Context, body AiAssistantGenerateRequest) (*AiAssistantGenerateResponse, error) {
	var out AiAssistantGenerateResponse
	if err := c.do(ctx, http.MethodPost, "/ai-assistant/solution", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AiAssistantMemoryBank generates a memory bank plan via the top-level AI assistant.
func (c *Client) AiAssistantMemoryBank(ctx context.Context, body MemoryBankAiAssistantRequest) (*MemoryBankAiAssistantResponse, error) {
	var out MemoryBankAiAssistantResponse
	if err := c.do(ctx, http.MethodPost, "/ai-assistant/memory-bank", nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAiAssistantMemoryBankHistory retrieves the last AI assistant memory bank conversation.
//
// Use [Client.GetAiAssistantMemoryBankHistoryWithOptions] to page through its turns.
func (c *Client) GetAiAssistantMemoryBankHistory(ctx context.Context) (*MemoryBankLastConversationResponse, error) {
	return c.GetAiAssistantMemoryBankHistoryWithOptions(ctx, LastConversationOptions{})
}

// GetAiAssistantMemoryBankHistoryWithOptions retrieves a page of turns from the
// last AI assistant memory bank conversation.
func (c *Client) GetAiAssistantMemoryBankHistoryWithOptions(ctx context.Context, opts LastConversationOptions) (*MemoryBankLastConversationResponse, error) {
	q := map[string]string{}
	setQueryValues(q, map[string]string{
		"limit":  queryPositiveInt(opts.Limit),
		"offset": queryPositiveInt(opts.Offset),
	})
	var out MemoryBankLastConversationResponse
	if err := c.do(ctx, http.MethodGet, "/ai-assistant/memory-bank/last-conversation", q, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptAiAssistantPlan accepts a top-level AI assistant plan.
func (c *Client) AcceptAiAssistantPlan(ctx context.Context, conversationID string, body AiAssistantAcceptRequest) (*AiAssistantAcceptResponse, error) {
	var out AiAssistantAcceptResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/ai-assistant/%s/accept", pathSegment(conversationID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeclineAiAssistantPlan declines a top-level AI assistant plan.
func (c *Client) DeclineAiAssistantPlan(ctx context.Context, conversationID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/ai-assistant/%s/decline", pathSegment(conversationID)), nil, nil, nil, nil)
}

// AcceptAiMemoryBankSuggestion accepts a top-level AI memory bank suggestion.
func (c *Client) AcceptAiMemoryBankSuggestion(ctx context.Context, conversationID string, body MemoryBankAcceptRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/ai-assistant/memory-bank/%s", pathSegment(conversationID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Cloud Drives ────────────────────────────────────────────────────────────

// ListCloudDriveProviders lists the cloud-drive providers this deployment has
// configured. Either wire shape is accepted.
func (c *Client) ListCloudDriveProviders(ctx context.Context) ([]CloudDriveProviderResponse, error) {
	var out []CloudDriveProviderResponse
	if err := c.doList(ctx, http.MethodGet, "/cloud-drives/providers", nil, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCloudDrives lists the account's cloud-drive connections. Either wire
// shape is accepted.
func (c *Client) ListCloudDrives(ctx context.Context) ([]CloudDriveResponse, error) {
	var out []CloudDriveResponse
	if err := c.doList(ctx, http.MethodGet, "/cloud-drives", nil, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCloudDrive retrieves a cloud-drive connection.
func (c *Client) GetCloudDrive(ctx context.Context, connectionID string) (*CloudDriveResponse, error) {
	var out CloudDriveResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/cloud-drives/%s", pathSegment(connectionID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateCloudDrive changes a cloud-drive connection's name and/or folder path.
// A nil field is left unchanged.
func (c *Client) UpdateCloudDrive(ctx context.Context, connectionID string, body CloudDriveUpdateRequest) (*CloudDriveResponse, error) {
	var out CloudDriveResponse
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/cloud-drives/%s", pathSegment(connectionID)), nil, body, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DisconnectCloudDrive disconnects a cloud-drive connection, keeping the
// connection itself, and returns it in its disconnected state.
func (c *Client) DisconnectCloudDrive(ctx context.Context, connectionID string) (*CloudDriveResponse, error) {
	var out CloudDriveResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/cloud-drives/%s/disconnect", pathSegment(connectionID)), nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCloudDrive deletes a cloud-drive connection.
func (c *Client) DeleteCloudDrive(ctx context.Context, connectionID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/cloud-drives/%s", pathSegment(connectionID)), nil, nil, nil, nil)
}

// GetAgentsUsingCloudDrive lists the agents that use a cloud-drive connection.
// Either wire shape is accepted.
func (c *Client) GetAgentsUsingCloudDrive(ctx context.Context, connectionID string) ([]AgentUsingCloudDriveResponse, error) {
	var out []AgentUsingCloudDriveResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/cloud-drives/%s/agents", pathSegment(connectionID)), nil, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CloudDriveRejectionOptions controls query parameters for [Client.ListCloudDriveRejections].
type CloudDriveRejectionOptions struct {
	// Limit is the maximum number of results (1-200, default 50). Zero omits the parameter.
	Limit int
}

// ListCloudDriveRejections lists the files a cloud-drive connection skipped,
// newest first, with the reason for each. A skipped file fires no trigger, so
// this is where to look when an agent did not run for a file.
func (c *Client) ListCloudDriveRejections(ctx context.Context, connectionID string, opts CloudDriveRejectionOptions) ([]CloudDriveRejectionResponse, error) {
	q := map[string]string{}
	if opts.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	var out []CloudDriveRejectionResponse
	if err := c.doList(ctx, http.MethodGet, fmt.Sprintf("/cloud-drives/%s/rejections", pathSegment(connectionID)), q, nil, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Internal helpers ────────────────────────────────────────────────────────

// buildURL constructs a full request URL by joining apiPath to the base URL
// and appending query parameters.
func (c *Client) buildURL(apiPath string, query map[string]string) (*url.URL, error) {
	return c.buildURLValues(apiPath, queryValues(query))
}

// pathSegment escapes one path segment (an ID) for an apiPath. Beyond
// url.PathEscape it escapes the dots of a "." or ".." segment, so buildURLValues's
// path.Clean, and any server or proxy that resolves dot segments, cannot take
// the ID as a step up the path. A blank ID becomes blankPathSegment, which
// buildURLValues refuses, since path.Clean would collapse an empty segment
// into a request for another endpoint ("/agents//runs" is "/agents/runs").
//
// The refusal works only where the path reaches buildURLValues, as every URL
// the client builds does today (doBytes, doRaw, doUpload and the two streaming
// methods). TestClient_BlankPathIDIsRefused calls one method on each of those
// paths; a new way of building a request URL needs a case there too.
func pathSegment(s string) string {
	if strings.TrimSpace(s) == "" {
		return blankPathSegment
	}
	switch s {
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	}
	return url.PathEscape(s)
}

// blankPathSegment is what pathSegment returns for a blank ID. It holds NUL
// bytes, which pathSegment never emits for a real ID.
const blankPathSegment = "\x00blank-id\x00"

// escapePath makes p a valid escaped path, so that [url.URL.EscapedPath] sends
// it as it is: a %XX escape is kept, a % that does not start one is written
// %25 (a [Client.Do] path such as "/files/100%" is sent as "/files/100%25"),
// and any other byte that may not appear in a path, such as a space or a
// non-ASCII byte, is percent-encoded. Were one such byte left raw, Go would
// discard RawPath and re-escape the decoded path, turning each %2F the caller
// wrote into a separator.
func escapePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '%' && i+2 < len(p) && isHex(p[i+1]) && isHex(p[i+2]):
			b.WriteByte(c)
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			strings.IndexByte("-._~/!$&'()*+,;=:@", c) >= 0:
			// Unreserved, "/", a sub-delimiter, ":" or "@": valid in RawPath as is.
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// queryValues lifts a single-valued query map into url.Values.
func queryValues(query map[string]string) url.Values {
	out := make(url.Values, len(query))
	for k, v := range query {
		out[k] = []string{v}
	}
	return out
}

// buildURLValues is buildURL for a query whose keys may repeat. Blank keys and
// empty values are dropped, and a key replaces the same key on the base URL.
// It returns a *ConfigurationError when apiPath holds a blank ID.
func (c *Client) buildURLValues(apiPath string, query url.Values) (*url.URL, error) {
	if strings.Contains(apiPath, blankPathSegment) {
		return nil, &ConfigurationError{Message: "an ID in the request path must not be blank"}
	}
	u := *c.baseURL
	joined := apiPath
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	hadTrailingSlash := joined != "/" && strings.HasSuffix(joined, "/")
	// apiPath is already escaped (the methods build it with pathSegment), so join
	// and clean the escaped form: there %2F stays inside its segment and an ID of
	// ".." is %2E%2E, which path.Clean leaves alone. Then set both Path and
	// RawPath, so String() sends the escaped form as it is instead of escaping
	// it a second time.
	cleaned := path.Clean(strings.TrimSuffix(u.EscapedPath(), "/") + joined)
	if hadTrailingSlash && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	cleaned = escapePath(cleaned)
	decoded, _ := url.PathUnescape(cleaned) // cannot fail after escapePath
	u.Path, u.RawPath = decoded, cleaned
	q := u.Query()
	for k, values := range query {
		if strings.TrimSpace(k) == "" {
			continue
		}
		kept := make([]string, 0, len(values))
		for _, v := range values {
			if v != "" {
				kept = append(kept, v)
			}
		}
		if len(kept) > 0 {
			q[k] = kept
		}
	}
	u.RawQuery = q.Encode()
	return &u, nil
}
