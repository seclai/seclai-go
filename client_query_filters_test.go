package seclai

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"testing"
)

// queryFilterCase calls one method with every filter set and states the exact
// query it must send.
type queryFilterCase struct {
	name, method, path, specPath, response string
	call                                   func(ctx context.Context, c *Client) error
	want                                   url.Values
}

func ptr[T any](v T) *T { return &v }

var queryFilterCases = []queryFilterCase{
	{
		name: "ListAgentExamples", method: "GET", path: "/agents/examples", specPath: "/agents/examples",
		response: `{"data":[{"key":"triage"}],"pagination":{"page":2,"limit":5,"total":6,"pages":2,"has_next":false,"has_prev":true}}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListAgentExamples(ctx, ListAgentExamplesOptions{ListOptions: ListOptions{Page: 2, Limit: 5}, Query: "support ticket", UseCase: "support"})
			return err
		},
		want: url.Values{"page": {"2"}, "limit": {"5"}, "query": {"support ticket"}, "use_case": {"support"}},
	},
	{
		name: "ListEvaluationResultsWithOptions", method: "GET", path: "/agents/evaluation-criteria/c_1/results", specPath: "/agents/evaluation-criteria/{criteria_id}/results",
		response: `{"data":[],"total":0,"page":1,"limit":10}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListEvaluationResultsWithOptions(ctx, "c_1", ListEvaluationResultsOptions{ListOptions: ListOptions{Page: 1, Limit: 10}, Status: "failed", FlaggedOnly: true, TimeFrom: "2026-10-01T00:00:00Z", TimeTo: "2026-10-31T00:00:00Z"})
			return err
		},
		want: url.Values{"page": {"1"}, "limit": {"10"}, "status": {"failed"}, "flagged_only": {"true"}, "time_from": {"2026-10-01T00:00:00Z"}, "time_to": {"2026-10-31T00:00:00Z"}},
	},
	{
		name: "ListCompatibleRunsWithOptions", method: "GET", path: "/agents/evaluation-criteria/c_1/compatible-runs", specPath: "/agents/evaluation-criteria/{criteria_id}/compatible-runs",
		response: `{"data":[],"total":0,"page":1,"limit":10}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListCompatibleRunsWithOptions(ctx, "c_1", ListCompatibleRunsOptions{StartedAfter: "2026-10-01T00:00:00Z"})
			return err
		},
		want: url.Values{"started_after": {"2026-10-01T00:00:00Z"}},
	},
	{
		name: "ListAgentEvaluationResultsWithOptions", method: "GET", path: "/agents/a_1/evaluation-results", specPath: "/agents/{agent_id}/evaluation-results",
		response: `{"data":[],"total":0,"page":1,"limit":10}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListAgentEvaluationResultsWithOptions(ctx, "a_1", ListAgentEvaluationResultsOptions{Status: "passed", Step: "s_1", FlaggedOnly: true, TimeFrom: "2026-10-01", TimeTo: "2026-10-31"})
			return err
		},
		want: url.Values{"status": {"passed"}, "step": {"s_1"}, "flagged_only": {"true"}, "time_from": {"2026-10-01"}, "time_to": {"2026-10-31"}},
	},
	{
		name: "ListEvaluationRunsWithOptions", method: "GET", path: "/agents/a_1/evaluation-runs", specPath: "/agents/{agent_id}/evaluation-runs",
		response: `{"data":[],"total":0,"page":1,"limit":10}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListEvaluationRunsWithOptions(ctx, "a_1", ListEvaluationRunsOptions{ListOptions: ListOptions{Limit: 3}, Status: "completed", Step: "s_1", TimeFrom: "2026-10-01", TimeTo: "2026-10-31"})
			return err
		},
		want: url.Values{"limit": {"3"}, "status": {"completed"}, "step": {"s_1"}, "time_from": {"2026-10-01"}, "time_to": {"2026-10-31"}},
	},
	{
		name: "GetNonManualEvaluationSummaryWithOptions", method: "GET", path: "/agents/evaluation-results/non-manual-summary", specPath: "/agents/evaluation-results/non-manual-summary",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.GetNonManualEvaluationSummaryWithOptions(ctx, NonManualEvaluationSummaryOptions{AgentID: "a_1", Days: 7, StartDate: "2026-10-01", EndDate: "2026-10-08"})
			return err
		},
		want: url.Values{"agent_id": {"a_1"}, "days": {"7"}, "start_date": {"2026-10-01"}, "end_date": {"2026-10-08"}},
	},
	{
		name: "GetAiAssistantMemoryBankHistoryWithOptions", method: "GET", path: "/ai-assistant/memory-bank/last-conversation", specPath: "/ai-assistant/memory-bank/last-conversation",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.GetAiAssistantMemoryBankHistoryWithOptions(ctx, LastConversationOptions{Limit: 10, Offset: 5})
			return err
		},
		want: url.Values{"limit": {"10"}, "offset": {"5"}},
	},
	{
		name: "GetMemoryBankAiLastConversationWithOptions", method: "GET", path: "/memory_banks/ai-assistant/last-conversation", specPath: "/memory_banks/ai-assistant/last-conversation",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.GetMemoryBankAiLastConversationWithOptions(ctx, LastConversationOptions{Limit: 10, Offset: 5})
			return err
		},
		want: url.Values{"limit": {"10"}, "offset": {"5"}},
	},
	{
		name: "ListAlerts", method: "GET", path: "/alerts", specPath: "/alerts",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListAlerts(ctx, ListAlertsOptions{Status: "active", AgentID: "a_1", SourceConnectionID: "sc_1", TimeFrom: "2026-10-01T00:00:00Z", TimeTo: "2026-10-31T00:00:00Z"})
			return err
		},
		want: url.Values{"status": {"active"}, "agent_id": {"a_1"}, "source_connection_id": {"sc_1"}, "time_from": {"2026-10-01T00:00:00Z"}, "time_to": {"2026-10-31T00:00:00Z"}},
	},
	{
		name: "ListOrganizationAlertPreferencesWithOptions", method: "GET", path: "/alerts/organization-preferences/list", specPath: "/alerts/organization-preferences/list",
		response: `{"preferences":[]}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListOrganizationAlertPreferencesWithOptions(ctx, ListOrganizationAlertPreferencesOptions{OrganizationID: "3f1a0d6e-0000-4000-8000-000000000001", IncludeDefaults: true})
			return err
		},
		want: url.Values{"organization_id": {"3f1a0d6e-0000-4000-8000-000000000001"}, "include_defaults": {"true"}},
	},
	{
		name: "ListGovernanceAiConversationsWithOptions", method: "GET", path: "/governance/ai-assistant/conversations", specPath: "/governance/ai-assistant/conversations",
		response: `[]`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListGovernanceAiConversationsWithOptions(ctx, ListGovernanceAiConversationsOptions{Limit: 50})
			return err
		},
		want: url.Values{"limit": {"50"}},
	},
	{
		name: "ListMemoryBanksWithOptions", method: "GET", path: "/memory_banks", specPath: "/memory_banks",
		response: `{"memory_banks":[]}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListMemoryBanksWithOptions(ctx, ListMemoryBanksOptions{SortableListOptions: SortableListOptions{ListOptions: ListOptions{Page: 2, Limit: 5}, Sort: "name", Order: "asc"}, Type: "conversation"})
			return err
		},
		want: url.Values{"page": {"2"}, "limit": {"5"}, "sort": {"name"}, "order": {"asc"}, "type": {"conversation"}},
	},
	{
		name: "GetMemoryBankStatsWithOptions", method: "GET", path: "/memory_banks/mb_1/stats", specPath: "/memory_banks/{memory_bank_id}/stats",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.GetMemoryBankStatsWithOptions(ctx, "mb_1", MemoryBankStatsOptions{Days: 14, StartDate: "2026-10-01", EndDate: "2026-10-15"})
			return err
		},
		want: url.Values{"days": {"14"}, "start_date": {"2026-10-01"}, "end_date": {"2026-10-15"}},
	},
	{
		name: "ListModels", method: "GET", path: "/models", specPath: "/models",
		response: `[]`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListModels(ctx, ListModelsOptions{SupportsInputMedia: "image", SupportsOutputMedia: "audio/mpeg"})
			return err
		},
		want: url.Values{"supports_input_media": {"image"}, "supports_output_media": {"audio/mpeg"}},
	},
	{
		name: "GetModelRecommendationsWithOptions", method: "GET", path: "/models/m_1/recommendations", specPath: "/models/{model_id}/recommendations",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.GetModelRecommendationsWithOptions(ctx, "m_1", ModelRecommendationsOptions{
				RequireToolUse: ptr(true), RequireStructuredOutput: ptr(false), RequireThinking: ptr(true),
				MinContextTokens: ptr(200000), MinOutputTokens: ptr(8192),
			})
			return err
		},
		want: url.Values{"require_tool_use": {"true"}, "require_structured_output": {"false"}, "require_thinking": {"true"}, "min_context_tokens": {"200000"}, "min_output_tokens": {"8192"}},
	},
	{
		name: "TypedGetModelRecommendationsWithOptions", method: "GET", path: "/models/m_1/recommendations", specPath: "/models/{model_id}/recommendations",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.Typed().GetModelRecommendationsWithOptions(ctx, "m_1", ModelRecommendationsOptions{MinContextTokens: ptr(1000)})
			return err
		},
		want: url.Values{"min_context_tokens": {"1000"}},
	},
	{
		name: "ListSolutionsWithOptions", method: "GET", path: "/solutions", specPath: "/solutions",
		response: `{}`,
		call: func(ctx context.Context, c *Client) error {
			_, err := c.ListSolutionsWithOptions(ctx, ListSolutionsOptions{SortableListOptions: SortableListOptions{ListOptions: ListOptions{Page: 1, Limit: 20}}, Search: "billing"})
			return err
		},
		want: url.Values{"page": {"1"}, "limit": {"20"}, "search": {"billing"}},
	},
}

// Every filter reaches the wire under its spec name, and the spec declares it.
func TestQueryFilters_SendEveryDeclaredKey(t *testing.T) {
	declared := specQueryParams(t)
	for _, tc := range queryFilterCases {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := stubClient(t, "", tc.response)
			if err := tc.call(context.Background(), c); err != nil {
				t.Fatalf("call: %v", err)
			}
			if seen.Method != tc.method || seen.Path != tc.path {
				t.Fatalf("got %s %s, want %s %s", seen.Method, seen.Path, tc.method, tc.path)
			}
			if !reflect.DeepEqual(seen.Query, tc.want) {
				t.Fatalf("query %v, want %v", seen.Query, tc.want)
			}
			for key := range seen.Query {
				if !declared[tc.specPath][key] {
					t.Errorf("sends query key %q, which GET %s does not declare", key, tc.specPath)
				}
			}
		})
	}
}

// The original signatures still send exactly what they sent before the
// filtered forms were added: no filter leaks in from a zero-valued option.
func TestQueryFilters_OriginalSignaturesAreUnchanged(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, response string
		call           func(c *Client) error
		want           url.Values
	}{
		{"ListEvaluationResults", `{"data":[]}`, func(c *Client) error {
			_, err := c.ListEvaluationResults(ctx, "c_1", ListOptions{Page: 2, Limit: 5})
			return err
		}, url.Values{"page": {"2"}, "limit": {"5"}}},
		{"ListCompatibleRuns", `{"data":[]}`, func(c *Client) error {
			_, err := c.ListCompatibleRuns(ctx, "c_1", ListOptions{Page: 2})
			return err
		}, url.Values{"page": {"2"}}},
		{"ListAgentEvaluationResults", `{"data":[]}`, func(c *Client) error {
			_, err := c.ListAgentEvaluationResults(ctx, "a_1", ListOptions{Limit: 5})
			return err
		}, url.Values{"limit": {"5"}}},
		{"ListEvaluationRuns", `{"data":[]}`, func(c *Client) error {
			_, err := c.ListEvaluationRuns(ctx, "a_1", ListOptions{})
			return err
		}, url.Values{}},
		{"GetNonManualEvaluationSummary", `{}`, func(c *Client) error {
			_, err := c.GetNonManualEvaluationSummary(ctx, "a_1")
			return err
		}, url.Values{"agent_id": {"a_1"}}},
		{"GetAiAssistantMemoryBankHistory", `{}`, func(c *Client) error {
			_, err := c.GetAiAssistantMemoryBankHistory(ctx)
			return err
		}, url.Values{}},
		{"GetMemoryBankAiLastConversation", `{}`, func(c *Client) error {
			_, err := c.GetMemoryBankAiLastConversation(ctx)
			return err
		}, url.Values{}},
		{"ListOrganizationAlertPreferences", `{"preferences":[]}`, func(c *Client) error {
			_, err := c.ListOrganizationAlertPreferences(ctx)
			return err
		}, url.Values{}},
		{"ListGovernanceAiConversations", `[]`, func(c *Client) error {
			_, err := c.ListGovernanceAiConversations(ctx)
			return err
		}, url.Values{}},
		{"ListMemoryBanks", `{"memory_banks":[]}`, func(c *Client) error {
			_, err := c.ListMemoryBanks(ctx, SortableListOptions{Sort: "name"})
			return err
		}, url.Values{"sort": {"name"}}},
		{"GetMemoryBankStats", `{}`, func(c *Client) error {
			_, err := c.GetMemoryBankStats(ctx, "mb_1")
			return err
		}, url.Values{}},
		{"GetModelRecommendations", `{}`, func(c *Client) error {
			_, err := c.GetModelRecommendations(ctx, "m_1")
			return err
		}, url.Values{}},
		{"ListSolutions", `{}`, func(c *Client) error {
			_, err := c.ListSolutions(ctx, SortableListOptions{Order: "desc"})
			return err
		}, url.Values{"order": {"desc"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := stubClient(t, "", tc.response)
			if err := tc.call(c); err != nil {
				t.Fatalf("call: %v", err)
			}
			got := seen.Query
			if got == nil {
				got = url.Values{}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("query %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClient_ListAgentExamples_DecodesTheEnvelope(t *testing.T) {
	c, _ := stubClient(t, "", `{"data":[{"key":"triage","name":"Ticket triage"}],"pagination":{"page":1,"limit":20,"total":1,"pages":1,"has_next":false,"has_prev":false}}`)
	got, err := c.ListAgentExamples(context.Background(), ListAgentExamplesOptions{})
	if err != nil {
		t.Fatalf("ListAgentExamples: %v", err)
	}
	if len(got.Data) != 1 || got.Pagination.Total != 1 {
		t.Fatalf("unexpected %+v", got)
	}
	entry, ok := got.Data[0].(map[string]any)
	if !ok || entry["key"] != "triage" {
		t.Fatalf("entry %#v", got.Data[0])
	}
}

func TestClient_SetSlackTriggerConfig(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{
		"trigger_id":"3f1a0d6e-0000-4000-8000-000000000002","agent_id":"3f1a0d6e-0000-4000-8000-000000000001",
		"mcp_client_id":null,"slack_team_id":"T1","form":[{"key":"topic","label":"Topic","type":"short_text"}],
		"scope":"selected_channels","allow_dms":true,"activation":"channel_opt_in","visibility":"private",
		"start_without_form":false,"turns_enabled":true,"turns_anyone":false,"max_turns":5,
		"include_invoker_email":false,"notify_failure_after_answer":true,"channel_ids":["C1"],"missing_scopes":["users:read.email"]}`)

	activation, scope, visibility := SlackTriggerActivation("channel_opt_in"), SlackTriggerScope("selected_channels"), SlackReplyVisibility("private")
	channels := []string{"C1"}
	form := []map[string]any{{"key": "topic", "label": "Topic", "type": "short_text"}}
	got, err := c.SetSlackTriggerConfig(context.Background(), "a_1", "t_1", SetSlackTriggerConfigRequest{
		Activation: &activation, Scope: &scope, Visibility: &visibility,
		ChannelIds: &channels, Form: &form, AllowDms: ptr(true), MaxTurns: ptr(5),
	})
	if err != nil {
		t.Fatalf("SetSlackTriggerConfig: %v", err)
	}
	rec.expect(t, "PUT", "/agents/a_1/triggers/t_1/slack-config", "")
	want := map[string]any{
		"activation": "channel_opt_in", "scope": "selected_channels", "visibility": "private",
		"channel_ids": []any{"C1"}, "form": []any{map[string]any{"key": "topic", "label": "Topic", "type": "short_text"}},
		"allow_dms": true, "max_turns": float64(5),
	}
	if !reflect.DeepEqual(rec.body, want) {
		t.Fatalf("body %s", rec.rawBody)
	}
	if got.Visibility != "private" || got.MaxTurns == nil || *got.MaxTurns != 5 || len(got.MissingScopes) != 1 {
		t.Fatalf("unexpected response %+v", got)
	}
}

func TestClient_SetSlackTriggerConfig_Alias(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{"alias":"triage","visibility":"private"}`)
	got, err := c.SetSlackTriggerConfig(context.Background(), "a_1", "t_1", SetSlackTriggerConfigRequest{Alias: ptr("triage")})
	if err != nil {
		t.Fatalf("SetSlackTriggerConfig: %v", err)
	}
	if string(rec.rawBody) != `{"alias":"triage"}` {
		t.Fatalf("body %s", rec.rawBody)
	}
	if got.Alias == nil || *got.Alias != "triage" {
		t.Fatalf("unexpected alias %v", got.Alias)
	}
}

// Input was removed from the spec after v1.9.0 shipped it. The field stays so
// existing callers compile; it is sent only when set, and the API ignores it.
func TestClient_SetSlackTriggerConfig_DeprecatedInputStillCompiles(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	//nolint:staticcheck // deprecated field kept for compatibility; still covered
	if _, err := c.SetSlackTriggerConfig(context.Background(), "a_1", "t_1", SetSlackTriggerConfigRequest{Input: ptr("{{metadata.topic}}")}); err != nil {
		t.Fatalf("SetSlackTriggerConfig: %v", err)
	}
	if string(rec.rawBody) != `{"input":"{{metadata.topic}}"}` {
		t.Fatalf("body %s", rec.rawBody)
	}
}

func TestClient_SetSlackTriggerConfig_ZeroValueIsEmptyObject(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	if _, err := c.SetSlackTriggerConfig(context.Background(), "a_1", "t_1", SetSlackTriggerConfigRequest{}); err != nil {
		t.Fatalf("SetSlackTriggerConfig: %v", err)
	}
	if string(rec.rawBody) != "{}" {
		t.Fatalf("expected {}, got %s", rec.rawBody)
	}
}

func TestClient_SetSlackTriggerConfig_EmptySlicesClear(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	form := []map[string]interface{}{}
	channels := []string{}
	if _, err := c.SetSlackTriggerConfig(context.Background(), "a_1", "t_1", SetSlackTriggerConfigRequest{Form: &form, ChannelIds: &channels}); err != nil {
		t.Fatalf("SetSlackTriggerConfig: %v", err)
	}
	if string(rec.rawBody) != `{"channel_ids":[],"form":[]}` {
		t.Fatalf("expected empty lists to be sent, got %s", rec.rawBody)
	}
}

func TestClient_ClearSlackTriggerMaxTurns(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{"max_turns":null,"visibility":"private"}`)
	got, err := c.ClearSlackTriggerMaxTurns(context.Background(), "a_1", "t_1")
	if err != nil {
		t.Fatalf("ClearSlackTriggerMaxTurns: %v", err)
	}
	rec.expect(t, "PUT", "/agents/a_1/triggers/t_1/slack-config", "")
	if string(rec.rawBody) != `{"max_turns":null}` {
		t.Fatalf("expected only an explicit null max_turns, got %s", rec.rawBody)
	}
	if got.MaxTurns != nil {
		t.Fatalf("expected nil MaxTurns, got %v", *got.MaxTurns)
	}
}

// specQueryParams maps each GET path in the bundled spec to its query params.
func specQueryParams(t *testing.T) map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("openapi/seclai.openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	out := map[string]map[string]bool{}
	for path, ops := range spec.Paths {
		get, ok := ops["get"]
		if !ok {
			continue
		}
		out[path] = map[string]bool{}
		for _, p := range get.Parameters {
			if p.In == "query" {
				out[path][p.Name] = true
			}
		}
	}
	return out
}
