package seclai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// listResult is what a gated list method returned, reduced to what both
// response shapes must agree on.
type listResult struct {
	Marker string // a field of the first item
	Count  int    // items where the declared type puts them
	Total  int
	Page   int
	Limit  int
	Extra  string // a non-list field carried beside the items
}

const (
	testUUID = "3f1a0d6e-0000-4000-8000-000000000001"

	// pagedPagination is what a paginated or offset endpoint sends on the
	// canonical shape; completePagination what a returned-in-full one sends.
	pagedPagination    = `{"page":2,"limit":1,"total":3,"pages":3,"has_next":true,"has_prev":true}`
	completePagination = `{"page":1,"limit":1,"total":1,"pages":1,"has_next":false,"has_prev":false}`
)

// gatedListRow is one client method for a version-gated list route. The bodies
// follow the backend handler: legacy is its `legacy=` value with ITEMS standing
// for the item array, and the canonical body is {data, pagination, **extras}.
type gatedListRow struct {
	route      string // as listed in testdata/gated_routes.txt
	name       string
	item       string
	legacy     string
	pagination string
	extras     string // canonical `**extra`, with its leading comma
	call       func(ctx context.Context, c *Client) (listResult, error)
	rawCall    func(ctx context.Context, c *Client) (json.RawMessage, error) // set instead of call for a json.RawMessage method
	want       listResult
	// wantCanonical differs from want only where the default shape carries no
	// paging metadata and the canonical one does.
	wantCanonical *listResult
}

func (r gatedListRow) bodies() map[string]string {
	items := "[" + r.item + "]"
	return map[string]string{
		"":                 strings.ReplaceAll(r.legacy, "ITEMS", items),
		APIVersion20260727: `{"data":` + items + `,"pagination":` + r.pagination + r.extras + `}`,
	}
}

func first[T any](items []T, marker func(T) string) (string, int) {
	if len(items) == 0 {
		return "", 0
	}
	return marker(items[0]), len(items)
}

var emailDomainExtras = `,"can_add_vanity":true,"can_add_custom":false,"has_vanity":false,"has_custom":false,` +
	`"vanity_plan_names":["Pro"],"custom_plan_names":["Business"]`

var gatedListRows = []gatedListRow{
	{
		route: "GET /agents/agent-email-optouts", name: "ListAgentEmailOptOuts",
		item:   `{"id":"` + testUUID + `","created_at":"2026-10-01T00:00:00Z","recipient_email":"a@example.com"}`,
		legacy: `{"items":ITEMS,"total":3}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListAgentEmailOptOuts(ctx, AgentEmailOptOutOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Items, func(i AgentEmailOptOutResponse) string { return i.RecipientEmail })
			return listResult{Marker: m, Count: n, Total: got.Total}, nil
		},
		want: listResult{Marker: "a@example.com", Count: 1, Total: 3},
	},
	{
		route: "GET /agents/blocked-email-senders", name: "ListBlockedEmailSenders",
		item:   `{"id":"` + testUUID + `","created_at":"2026-10-01T00:00:00Z","sender_email":"spam@example.com","match_type":"address","source":"manual"}`,
		legacy: `{"items":ITEMS,"total":3,"auto_block_mode":"input"}`, pagination: pagedPagination,
		extras: `,"auto_block_mode":"input"`,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListBlockedEmailSenders(ctx, BlockedEmailSenderOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Items, func(i BlockedEmailSenderResponse) string { return i.SenderEmail })
			return listResult{Marker: m, Count: n, Total: got.Total, Extra: got.AutoBlockMode}, nil
		},
		want: listResult{Marker: "spam@example.com", Count: 1, Total: 3, Extra: "input"},
	},
	{
		route: "PUT /agents/blocked-email-senders/mode", name: "SetAutoBlockMode",
		item:   `{"id":"` + testUUID + `","created_at":"2026-10-01T00:00:00Z","sender_email":"spam@example.com","match_type":"address","source":"manual"}`,
		legacy: `{"items":ITEMS,"total":1,"auto_block_mode":"input"}`, pagination: completePagination,
		extras: `,"auto_block_mode":"input"`,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.SetAutoBlockMode(ctx, SetAutoBlockModeRequest{Mode: "input"})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Items, func(i BlockedEmailSenderResponse) string { return i.SenderEmail })
			return listResult{Marker: m, Count: n, Total: got.Total, Extra: got.AutoBlockMode}, nil
		},
		want: listResult{Marker: "spam@example.com", Count: 1, Total: 1, Extra: "input"},
	},
	{
		route: "GET /agents/evaluation-criteria/{criteria_id}/compatible-runs", name: "ListCompatibleRuns",
		item:   `{"agent_run_id":"` + testUUID + `","agent_step_run_id":"` + testUUID + `"}`,
		legacy: `{"data":ITEMS,"total":3,"page":2,"limit":1}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListCompatibleRuns(ctx, "crit_1", ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Data, func(i CompatibleRunResponse) string { return i.AgentRunId.String() })
			return listResult{Marker: m, Count: n, Total: got.Total, Page: got.Page, Limit: got.Limit}, nil
		},
		want: listResult{Marker: testUUID, Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /agents/evaluation-criteria/{criteria_id}/results", name: "ListEvaluationResults",
		item:   `{"id":"` + testUUID + `","agent_run_id":"` + testUUID + `","criteria_id":"` + testUUID + `"}`,
		legacy: `{"data":ITEMS,"total":3,"page":2,"limit":1}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListEvaluationResults(ctx, "crit_1", ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Data, func(i EvaluationResultResponse) string { return i.Id.String() })
			return listResult{Marker: m, Count: n, Total: got.Total, Page: got.Page, Limit: got.Limit}, nil
		},
		want: listResult{Marker: testUUID, Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /agents/inbound-email-rejections", name: "ListInboundEmailRejections",
		item:   `{"id":"` + testUUID + `","created_at":"2026-10-01T00:00:00Z","recipient":"x@example.com","sender":"s@example.com","reason":"unauthorized_sender"}`,
		legacy: `ITEMS`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListInboundEmailRejections(ctx, InboundEmailRejectionOptions{})
			m, n := first(got, func(i InboundEmailRejectionResponse) string { return i.Reason })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "unauthorized_sender", Count: 1},
	},
	{
		route: "GET /agents/{agent_id}/callers", name: "GetAgentCallers",
		item:   `{"id":"` + testUUID + `","name":"Router","disabled":false}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.GetAgentCallers(ctx, "a_1")
			m, n := first(got, func(i AgentCallerApiResponse) string { return i.Name })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "Router", Count: 1},
	},
	{
		route: "GET /agents/{agent_id}/evaluation-criteria", name: "ListEvaluationCriteria",
		item:   `{"id":"` + testUUID + `","evaluation_mode":"sample_and_flag"}`,
		legacy: `ITEMS`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListEvaluationCriteria(ctx, "a_1", ListOptions{})
			m, n := first(got, func(i EvaluationCriteriaResponse) string { return i.EvaluationMode })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "sample_and_flag", Count: 1},
	},
	{
		route: "GET /agents/{agent_id}/evaluation-criteria", name: "ListEvaluationCriteriaPage",
		item:   `{"id":"` + testUUID + `","evaluation_mode":"sample_and_flag"}`,
		legacy: `ITEMS`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListEvaluationCriteriaPage(ctx, "a_1", ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Data, func(i EvaluationCriteriaResponse) string { return i.EvaluationMode })
			res := listResult{Marker: m, Count: n}
			if got.Pagination != nil {
				res.Total, res.Page, res.Limit = got.Pagination.Total, got.Pagination.Page, got.Pagination.Limit
			}
			return res, nil
		},
		want:          listResult{Marker: "sample_and_flag", Count: 1},
		wantCanonical: &listResult{Marker: "sample_and_flag", Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /agents/{agent_id}/evaluation-results", name: "ListAgentEvaluationResults",
		item:   `{"id":"` + testUUID + `","agent_run_id":"` + testUUID + `","criteria_id":"` + testUUID + `"}`,
		legacy: `{"data":ITEMS,"total":3,"page":2,"limit":1}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListAgentEvaluationResults(ctx, "a_1", ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Data, func(i EvaluationResultWithCriteriaResponse) string { return i.Id.String() })
			return listResult{Marker: m, Count: n, Total: got.Total, Page: got.Page, Limit: got.Limit}, nil
		},
		want: listResult{Marker: testUUID, Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /agents/{agent_id}/evaluation-runs", name: "ListEvaluationRuns",
		item:   `{"agent_run_id":"` + testUUID + `","passed_count":2}`,
		legacy: `{"data":ITEMS,"total":3,"page":2,"limit":1}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListEvaluationRuns(ctx, "a_1", ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Data, func(i EvaluationRunSummaryResponse) string { return i.AgentRunId.String() })
			return listResult{Marker: m, Count: n, Total: got.Total, Page: got.Page, Limit: got.Limit}, nil
		},
		want: listResult{Marker: testUUID, Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /agents/{agent_id}/runs/{run_id}/evaluation-results", name: "ListRunEvaluationResults",
		item:   `{"id":"` + testUUID + `","agent_run_id":"` + testUUID + `","criteria_id":"` + testUUID + `"}`,
		legacy: `ITEMS`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListRunEvaluationResults(ctx, "a_1", "run_1", ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Data, func(i EvaluationResultWithCriteriaResponse) string { return i.Id.String() })
			return listResult{Marker: m, Count: n, Total: got.Total, Page: got.Page, Limit: got.Limit}, nil
		},
		want:          listResult{Marker: testUUID, Count: 1},
		wantCanonical: &listResult{Marker: testUUID, Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /alerts/configs", name: "ListAlertConfigs",
		item:   `{"id":"cfg_1","alert_type":"run_failed"}`,
		legacy: `{"configs":ITEMS,"total":3}`, pagination: pagedPagination,
		rawCall: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListAlertConfigs(ctx, ListOptions{})
		},
	},
	{
		route: "GET /alerts/configs", name: "Typed().ListAlertConfigs",
		item:   `{"id":"cfg_1","alert_type":"run_failed"}`,
		legacy: `{"configs":ITEMS,"total":3}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.Typed().ListAlertConfigs(ctx, ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, _ := first(got.Items(), func(i AlertConfigResponse) string { return i.Id })
			return listResult{Marker: m, Count: len(got.Configs), Total: got.Total}, nil
		},
		want: listResult{Marker: "cfg_1", Count: 1, Total: 3},
	},
	{
		route: "GET /alerts/organization-preferences/list", name: "ListOrganizationAlertPreferences",
		item:   `{"organization_id":"org_1","alert_type":"run_failed"}`,
		legacy: `{"preferences":ITEMS,"total":1}`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListOrganizationAlertPreferences(ctx)
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Preferences, func(i OrganizationAlertPreferenceResponse) string { return i.AlertType })
			return listResult{Marker: m, Count: n, Total: got.Total}, nil
		},
		want: listResult{Marker: "run_failed", Count: 1, Total: 1},
	},
	{
		route: "GET /cloud-drives", name: "ListCloudDrives",
		item:   `{"id":"cd_1","provider":"dropbox","status":"active"}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListCloudDrives(ctx)
			m, n := first(got, func(i CloudDriveResponse) string { return i.Id })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "cd_1", Count: 1},
	},
	{
		route: "GET /cloud-drives/providers", name: "ListCloudDriveProviders",
		item:   `{"key":"google_drive","display_name":"Google Drive","access_levels":[],"scopes":[]}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListCloudDriveProviders(ctx)
			m, n := first(got, func(i CloudDriveProviderResponse) string { return i.Key })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "google_drive", Count: 1},
	},
	{
		route: "GET /cloud-drives/{connection_id}/agents", name: "GetAgentsUsingCloudDrive",
		item:   `{"agent_id":"a_1","agent_name":"Intake","trigger_types":["FILE_ADDED"],"via_step":true,"via_prompt_tool":false}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.GetAgentsUsingCloudDrive(ctx, "cd_1")
			m, n := first(got, func(i AgentUsingCloudDriveResponse) string { return i.AgentName })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "Intake", Count: 1},
	},
	{
		route: "GET /cloud-drives/{connection_id}/rejections", name: "ListCloudDriveRejections",
		item:   `{"id":"r_1","created_at":"2026-10-01T00:00:00Z","reason":"too_large"}`,
		legacy: `ITEMS`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListCloudDriveRejections(ctx, "cd_1", CloudDriveRejectionOptions{})
			m, n := first(got, func(i CloudDriveRejectionResponse) string { return i.Reason })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "too_large", Count: 1},
	},
	{
		route: "GET /email-domains", name: "ListEmailDomains",
		item:   `{"id":"` + testUUID + `","domain":"mail.example.com"}`,
		legacy: `{"domains":ITEMS` + emailDomainExtras + `}`, pagination: completePagination,
		extras: emailDomainExtras,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListEmailDomains(ctx)
			if err != nil {
				return listResult{}, err
			}
			res := listResult{}
			if got.Domains != nil {
				res.Marker, res.Count = first(*got.Domains, func(i EmailDomainResponse) string { return i.Domain })
			}
			if got.CanAddVanity != nil && *got.CanAddVanity && got.VanityPlanNames != nil {
				res.Extra = strings.Join(*got.VanityPlanNames, ",")
			}
			return res, nil
		},
		want: listResult{Marker: "mail.example.com", Count: 1, Extra: "Pro"},
	},
	{
		route: "GET /governance/ai-assistant/conversations", name: "ListGovernanceAiConversations",
		item:   `{"id":"conv_1","created_at":"2026-10-01T00:00:00Z","user_input":"tighten the PII policy"}`,
		legacy: `ITEMS`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListGovernanceAiConversations(ctx)
			m, n := first(got, func(i GovernanceConversationResponse) string { return i.UserInput })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "tighten the PII policy", Count: 1},
	},
	{
		route: "GET /knowledge_bases", name: "ListKnowledgeBases",
		item:   `{"id":"kb_1","name":"Contracts"}`,
		legacy: `{"knowledge_bases":ITEMS,"page":2,"limit":1,"total":3}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListKnowledgeBases(ctx, SortableListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.KnowledgeBases, func(i KnowledgeBaseResponse) string { return i.Name })
			return listResult{Marker: m, Count: n, Total: got.Total, Page: got.Page, Limit: got.Limit}, nil
		},
		want: listResult{Marker: "Contracts", Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /memory_banks", name: "ListMemoryBanks",
		item:   `{"id":"mb_1","name":"Customer notes"}`,
		legacy: `{"memory_banks":ITEMS,"page":2,"limit":1,"total":3}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListMemoryBanks(ctx, SortableListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.MemoryBanks, func(i MemoryBankResponse) string { return i.Name })
			return listResult{Marker: m, Count: n, Total: got.Total, Page: got.Page, Limit: got.Limit}, nil
		},
		want: listResult{Marker: "Customer notes", Count: 1, Total: 3, Page: 2, Limit: 1},
	},
	{
		route: "GET /memory_banks/templates", name: "ListMemoryBankTemplates",
		item:   `{"id":"working","name":"Working Memory","defaults":{"type":"conversation"}}`,
		legacy: `ITEMS`, pagination: completePagination,
		rawCall: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListMemoryBankTemplates(ctx)
		},
	},
	{
		route: "GET /memory_banks/{memory_bank_id}/agents", name: "GetAgentsUsingMemoryBank",
		item:   `{"agent_id":"a_1","agent_name":"Intake"}`,
		legacy: `ITEMS`, pagination: completePagination,
		rawCall: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.GetAgentsUsingMemoryBank(ctx, "mb_1")
		},
	},
	{
		route: "GET /memory_banks/templates", name: "Typed().ListMemoryBankTemplates",
		item:   `{"id":"working","name":"Working Memory","defaults":{"type":"conversation"}}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.Typed().ListMemoryBankTemplates(ctx)
			m, n := first(got, func(i map[string]JsonValue) string { return fmt.Sprint(i["id"]) })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "working", Count: 1},
	},
	{
		route: "GET /memory_banks/{memory_bank_id}/agents", name: "Typed().GetAgentsUsingMemoryBank",
		item:   `{"agent_id":"a_1","agent_name":"Intake"}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.Typed().GetAgentsUsingMemoryBank(ctx, "mb_1")
			m, n := first(got, func(i map[string]JsonValue) string { return fmt.Sprint(i["agent_name"]) })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "Intake", Count: 1},
	},
	{
		route: "GET /models", name: "ListModels",
		item:   `{"provider":"anthropic","models":[]}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListModels(ctx, ListModelsOptions{})
			m, n := first(got, func(i ProviderGroupResponse) string { return i.Provider })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "anthropic", Count: 1},
	},
	{
		route: "GET /models/alerts", name: "ListModelAlerts",
		item:   `{"id":"ma_1","alert_type":"deprecation","message":"retiring"}`,
		legacy: `{"alerts":ITEMS,"total":3}`, pagination: pagedPagination,
		rawCall: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListModelAlerts(ctx, ListOptions{})
		},
	},
	{
		route: "GET /models/alerts", name: "Typed().ListModelAlerts",
		item:   `{"id":"ma_1","alert_type":"deprecation","message":"retiring"}`,
		legacy: `{"alerts":ITEMS,"total":3}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.Typed().ListModelAlerts(ctx, ListOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, _ := first(got.Items(), func(i ModelAlertResponse) string { return i.Id })
			return listResult{Marker: m, Count: len(got.Alerts), Total: got.Total}, nil
		},
		want: listResult{Marker: "ma_1", Count: 1, Total: 3},
	},
	{
		route: "GET /models/embedders", name: "ListEmbeddingModels",
		item:   `{"model_id":"m_1","model_type":"openai.text-embedding-3-small","credits":1.5,"dimensions":[1536]}`,
		legacy: `{"models":ITEMS,"default_model_type":"openai.text-embedding-3-small","default_dimension":1536,"file_processing_credits_per_mb":2,"storage_credits":[]}`,
		extras: `,"default_model_type":"openai.text-embedding-3-small","default_dimension":1536,"file_processing_credits_per_mb":2,"storage_credits":[]`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListEmbeddingModels(ctx, ListEmbeddingModelsOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, _ := first(got.Items(), func(i EmbeddingModelResponse) string { return i.ModelId })
			res := listResult{Marker: m, Count: len(got.Models)}
			if got.DefaultModelType != nil {
				res.Extra = *got.DefaultModelType
			}
			return res, nil
		},
		want: listResult{Marker: "m_1", Count: 1, Extra: "openai.text-embedding-3-small"},
	},
	{
		route: "GET /models/generation-tiers", name: "GetGenerationTiers",
		item:   `{"modality":"image","tier":"fast","model_id":"m_1","model_name":"Fast image"}`,
		legacy: `{"tiers":ITEMS}`, pagination: completePagination,
		rawCall: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.GetGenerationTiers(ctx)
		},
	},
	{
		route: "GET /models/generation-tiers", name: "Typed().GetGenerationTiers",
		item:   `{"modality":"image","tier":"fast","model_id":"m_1","model_name":"Fast image"}`,
		legacy: `{"tiers":ITEMS}`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.Typed().GetGenerationTiers(ctx)
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Tiers, func(i GenerationTierResponse) string { return i.Tier })
			return listResult{Marker: m, Count: n}, nil
		},
		want: listResult{Marker: "fast", Count: 1},
	},
	{
		route: "GET /models/playground/experiments", name: "ListExperiments",
		item:   `{"id":"exp_1","status":"completed","created_at":"2026-10-01T00:00:00Z"}`,
		legacy: `{"experiments":ITEMS,"total":3}`, pagination: pagedPagination,
		rawCall: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListExperiments(ctx, ListExperimentsOptions{})
		},
	},
	{
		route: "GET /models/playground/experiments", name: "Typed().ListExperiments",
		item:   `{"id":"exp_1","status":"completed","created_at":"2026-10-01T00:00:00Z"}`,
		legacy: `{"experiments":ITEMS,"total":3}`, pagination: pagedPagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.Typed().ListExperiments(ctx, ListExperimentsOptions{})
			if err != nil {
				return listResult{}, err
			}
			m, n := first(got.Experiments, func(i ExperimentSummaryResponse) string { return i.Id })
			return listResult{Marker: m, Count: n, Total: got.Total}, nil
		},
		want: listResult{Marker: "exp_1", Count: 1, Total: 3},
	},
	{
		route: "GET /models/rerankers", name: "ListRerankerModels",
		item:   `{"model_type":"cohere.rerank-v3","name":"Rerank v3","credits_per_action":0.5,"is_default":true}`,
		legacy: `{"models":ITEMS,"default_model_type":"cohere.rerank-v3","search_processing_credits":0.25}`,
		extras: `,"default_model_type":"cohere.rerank-v3","search_processing_credits":0.25`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListRerankerModels(ctx)
			if err != nil {
				return listResult{}, err
			}
			m, _ := first(got.Items(), func(i RerankerModelResponse) string { return i.Name })
			return listResult{Marker: m, Count: len(got.Models), Extra: got.DefaultModelType}, nil
		},
		want: listResult{Marker: "Rerank v3", Count: 1, Extra: "cohere.rerank-v3"},
	},
	{
		route: "GET /solutions/{solution_id}/conversations", name: "ListSolutionConversations",
		item:   `{"id":"` + testUUID + `","created_at":"2026-10-01T00:00:00Z","user_input":"add a knowledge base"}`,
		legacy: `ITEMS`, pagination: completePagination,
		call: func(ctx context.Context, c *Client) (listResult, error) {
			got, err := c.ListSolutionConversations(ctx, "sol_1")
			m, n := first(got, func(i SolutionConversationResponse) string { return i.UserInput })
			return listResult{Marker: m, Count: n}, err
		},
		want: listResult{Marker: "add a knowledge base", Count: 1},
	},
}

// routePattern matches the request a route's method must issue.
func routePattern(route string) (string, *regexp.Regexp) {
	method, path, _ := strings.Cut(route, " ")
	quoted := regexp.QuoteMeta(path)
	quoted = regexp.MustCompile(`\\\{[^}]*\\\}`).ReplaceAllString(quoted, `[^/]+`)
	return method, regexp.MustCompile(`^` + quoted + `$`)
}

func TestGatedLists_EveryMethodReadsBothShapes(t *testing.T) {
	for _, row := range gatedListRows {
		method, pattern := routePattern(row.route)
		for version, body := range row.bodies() {
			t.Run(row.name+"/version="+version, func(t *testing.T) {
				c, seen := stubClient(t, version, body)
				if row.rawCall != nil {
					got, err := row.rawCall(context.Background(), c)
					if err != nil {
						t.Fatalf("%v", err)
					}
					if string(got) != body {
						t.Fatalf("raw body altered: got %s, want %s", got, body)
					}
				} else {
					got, err := row.call(context.Background(), c)
					if err != nil {
						t.Fatalf("%v (body %s)", err, body)
					}
					want := row.want
					if version != "" && row.wantCanonical != nil {
						want = *row.wantCanonical
					}
					if got != want {
						t.Fatalf("got %+v, want %+v (body %s)", got, want, body)
					}
				}
				if seen.Method != method || !pattern.MatchString(seen.Path) {
					t.Fatalf("issued %s %s, which is not %s", seen.Method, seen.Path, row.route)
				}
			})
		}
	}
}

func TestGatedLists_ANonListBodyIsAnErrorNotAnEmptyList(t *testing.T) {
	for _, row := range gatedListRows {
		if row.rawCall != nil {
			continue
		}
		bodies := map[string]string{
			"error object":        `{"detail":"Internal error"}`,
			"string":              `"temporarily unavailable"`,
			"null":                `null`,
			"number":              `7`,
			"text":                `upstream timed out`,
			"empty body":          ``,
			"data not a list":     `{"data":{"id":"x"},"pagination":` + pagedPagination + `}`,
			"legacy key not list": strings.ReplaceAll(row.legacy, "ITEMS", `{"id":"x"}`),
		}
		for label, body := range bodies {
			for _, version := range []string{"", APIVersion20260727} {
				t.Run(row.name+"/"+label+"/version="+version, func(t *testing.T) {
					c, _ := stubClient(t, version, body)
					got, err := row.call(context.Background(), c)
					var shapeErr *UnexpectedResponseError
					if !errors.As(err, &shapeErr) {
						t.Fatalf("got %+v with error %v, want an *UnexpectedResponseError", got, err)
					}
					if (shapeErr.ResponseText != body && shapeErr.ResponseText != "") || !strings.Contains(shapeErr.Error(), "seclai: unexpected response") {
						t.Fatalf("unexpected error contents: %#v", shapeErr)
					}
				})
			}
		}
	}
}

func TestGatedLists_ExplicitNullDataIsAnEmptyList(t *testing.T) {
	for _, row := range gatedListRows {
		if row.rawCall != nil {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			c, _ := stubClient(t, APIVersion20260727, `{"data":null,"pagination":`+completePagination+`}`)
			got, err := row.call(context.Background(), c)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if got.Count != 0 || got.Marker != "" {
				t.Fatalf("expected an empty list, got %+v", got)
			}
		})
	}
}

// The gated routes come from the backend, not from this table: a route added
// there without a row here fails, as does a row for a route no longer gated.
func TestGatedLists_EveryGatedRouteHasARow(t *testing.T) {
	raw, err := os.ReadFile("testdata/gated_routes.txt")
	if err != nil {
		t.Fatalf("read gated routes: %v", err)
	}
	gated := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			gated[line] = false
		}
	}
	if len(gated) == 0 {
		t.Fatal("testdata/gated_routes.txt lists no routes")
	}
	for _, row := range gatedListRows {
		if _, ok := gated[row.route]; !ok {
			t.Errorf("row %s is for %q, which is not a gated route", row.name, row.route)
		}
		gated[row.route] = true
	}
	for route, covered := range gated {
		if !covered {
			t.Errorf("gated route %q has no row in gatedListRows", route)
		}
	}
}

func TestDecodeList_PrefersARealListOverAnExplicitNull(t *testing.T) {
	type keyed struct {
		Things []struct {
			ID int `json:"id"`
		} `json:"things"`
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	cases := []struct {
		name, body string
		wantIDs    []int
		notAList   bool
	}{
		{name: "data array wins over a legacy array", body: `{"things":[{"id":1}],"data":[{"id":2}]}`, wantIDs: []int{2}},
		{name: "legacy array beside null data", body: `{"things":[{"id":1}],"data":null}`, wantIDs: []int{1}},
		{name: "legacy array beside non-array data", body: `{"things":[{"id":1}],"data":{"id":2}}`, wantIDs: []int{1}},
		{name: "legacy array alone", body: `{"things":[{"id":1}]}`, wantIDs: []int{1}},
		{name: "bare array", body: `[{"id":1}]`, wantIDs: []int{1}},
		{name: "null data alone is empty", body: `{"data":null}`, wantIDs: []int{}},
		{name: "null data beside a non-array legacy key is empty", body: `{"things":{"id":1},"data":null}`, wantIDs: []int{}},
		{name: "null legacy key alone", body: `{"things":null}`, notAList: true},
		{name: "non-array data alone", body: `{"data":{"id":2}}`, notAList: true},
		{name: "non-array legacy key beside non-array data", body: `{"things":"x","data":7}`, notAList: true},
		{name: "neither key", body: `{"detail":"nope"}`, notAList: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out keyed
			err := decodeList([]byte(tc.body), "things", &out)
			if tc.notAList {
				if !errors.Is(err, errNotAList) {
					t.Fatalf("got %+v with error %v, want errNotAList", out, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%v", err)
			}
			ids := []int{}
			for _, thing := range out.Things {
				ids = append(ids, thing.ID)
			}
			if fmt.Sprint(ids) != fmt.Sprint(tc.wantIDs) {
				t.Fatalf("got ids %v, want %v", ids, tc.wantIDs)
			}
		})
	}
}

func TestUnexpectedResponseError_UnwrapsTheJSONError(t *testing.T) {
	ctx := context.Background()

	c, _ := stubClient(t, "", `[7]`)
	_, err := c.ListModels(ctx, ListModelsOptions{})
	var shapeErr *UnexpectedResponseError
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &shapeErr) || !errors.As(err, &typeErr) {
		t.Fatalf("a list element of the wrong type: got %v, want both error types", err)
	}

	c, _ = stubClient(t, "", `upstream timed out`)
	_, err = c.ListModels(ctx, ListModelsOptions{})
	var syntaxErr *json.SyntaxError
	shapeErr = nil
	if !errors.As(err, &shapeErr) || !errors.As(err, &syntaxErr) {
		t.Fatalf("malformed JSON: got %v, want both error types", err)
	}

	c, _ = stubClient(t, "", `upstream timed out`)
	_, err = c.Typed().ListMemoryBankTemplates(ctx)
	syntaxErr, shapeErr = nil, nil
	if !errors.As(err, &shapeErr) || !errors.As(err, &syntaxErr) {
		t.Fatalf("malformed JSON through Typed(): got %v, want both error types", err)
	}

	c, _ = stubClient(t, "", `{"detail":"nope"}`)
	_, err = c.ListModels(ctx, ListModelsOptions{})
	shapeErr = nil
	if !errors.As(err, &shapeErr) || shapeErr.Unwrap() != nil {
		t.Fatalf("valid JSON of the wrong shape: got %v, want an *UnexpectedResponseError with no cause", err)
	}
}
