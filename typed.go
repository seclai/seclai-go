package seclai

import (
	"context"
	"encoding/json"
	"net/http"
)

// TypedClient exposes typed forms of the [Client] methods that return
// json.RawMessage.
//
// Opt-in, reached through [Client.Typed]. The methods on Client itself keep
// returning json.RawMessage so existing call sites are unaffected; the same
// endpoints are available here decoded into structs. Each method either
// delegates to its counterpart or, for the list forms, calls the one unexported
// request function its counterpart calls, so the two surfaces issue identical
// requests and cannot drift apart.
//
//	raw, _ := client.Search(ctx, opts)         // json.RawMessage
//	typed, _ := client.Typed().Search(ctx, opts) // *SearchResponse
type TypedClient struct {
	c *Client
}

// Typed returns the typed view of this client.
func (c *Client) Typed() *TypedClient {
	return &TypedClient{c: c}
}

// ListAlerts is the typed form of [Client.ListAlerts].
func (t *TypedClient) ListAlerts(ctx context.Context, opts ListAlertsOptions) (*AlertListResponse, error) {
	raw, err := t.c.ListAlerts(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out AlertListResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAlert is the typed form of [Client.GetAlert].
func (t *TypedClient) GetAlert(ctx context.Context, alertID string) (*AlertDetailResponse, error) {
	raw, err := t.c.GetAlert(ctx, alertID)
	if err != nil {
		return nil, err
	}
	var out AlertDetailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChangeAlertStatus is the typed form of [Client.ChangeAlertStatus].
func (t *TypedClient) ChangeAlertStatus(ctx context.Context, alertID string, body ChangeStatusRequest) (*AlertDetailResponse, error) {
	raw, err := t.c.ChangeAlertStatus(ctx, alertID, body)
	if err != nil {
		return nil, err
	}
	var out AlertDetailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddAlertComment is the typed form of [Client.AddAlertComment].
func (t *TypedClient) AddAlertComment(ctx context.Context, alertID string, body AddCommentRequest) (*AlertDetailResponse, error) {
	raw, err := t.c.AddAlertComment(ctx, alertID, body)
	if err != nil {
		return nil, err
	}
	var out AlertDetailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubscribeToAlert is the typed form of [Client.SubscribeToAlert].
func (t *TypedClient) SubscribeToAlert(ctx context.Context, alertID string) (*AlertDetailResponse, error) {
	raw, err := t.c.SubscribeToAlert(ctx, alertID)
	if err != nil {
		return nil, err
	}
	var out AlertDetailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnsubscribeFromAlert is the typed form of [Client.UnsubscribeFromAlert].
func (t *TypedClient) UnsubscribeFromAlert(ctx context.Context, alertID string) (*AlertDetailResponse, error) {
	raw, err := t.c.UnsubscribeFromAlert(ctx, alertID)
	if err != nil {
		return nil, err
	}
	var out AlertDetailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAlertConfigs is the typed form of [Client.ListAlertConfigs].
func (t *TypedClient) ListAlertConfigs(ctx context.Context, opts ListOptions) (*AlertConfigListResponse, error) {
	body, reqURL, err := t.c.alertConfigsBody(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out AlertConfigListResponse
	if err := decodeListBody(http.MethodGet, reqURL, body, "configs", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAlertConfig is the typed form of [Client.CreateAlertConfig].
func (t *TypedClient) CreateAlertConfig(ctx context.Context, body CreateAlertConfigRequest) (*AlertConfigResponse, error) {
	raw, err := t.c.CreateAlertConfig(ctx, body)
	if err != nil {
		return nil, err
	}
	var out AlertConfigResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAlertConfig is the typed form of [Client.GetAlertConfig].
func (t *TypedClient) GetAlertConfig(ctx context.Context, configID string) (*AlertConfigResponse, error) {
	raw, err := t.c.GetAlertConfig(ctx, configID)
	if err != nil {
		return nil, err
	}
	var out AlertConfigResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAlertConfig is the typed form of [Client.UpdateAlertConfig].
func (t *TypedClient) UpdateAlertConfig(ctx context.Context, configID string, body UpdateAlertConfigRequest) (*AlertConfigResponse, error) {
	raw, err := t.c.UpdateAlertConfig(ctx, configID, body)
	if err != nil {
		return nil, err
	}
	var out AlertConfigResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateOrganizationAlertPreference is the typed form of [Client.UpdateOrganizationAlertPreference].
func (t *TypedClient) UpdateOrganizationAlertPreference(ctx context.Context, organizationID, alertType string, body UpdateOrganizationAlertPreferenceRequest) (*OrganizationAlertPreferenceResponse, error) {
	raw, err := t.c.UpdateOrganizationAlertPreference(ctx, organizationID, alertType, body)
	if err != nil {
		return nil, err
	}
	var out OrganizationAlertPreferenceResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListModelAlerts is the typed form of [Client.ListModelAlerts].
func (t *TypedClient) ListModelAlerts(ctx context.Context, opts ListOptions) (*ModelAlertListResponse, error) {
	body, reqURL, err := t.c.modelAlertsBody(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out ModelAlertListResponse
	if err := decodeListBody(http.MethodGet, reqURL, body, "alerts", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetUnreadModelAlertCount is the typed form of [Client.GetUnreadModelAlertCount].
func (t *TypedClient) GetUnreadModelAlertCount(ctx context.Context) (*UnreadCountResponse, error) {
	raw, err := t.c.GetUnreadModelAlertCount(ctx)
	if err != nil {
		return nil, err
	}
	var out UnreadCountResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetModelRecommendations is the typed form of [Client.GetModelRecommendations].
func (t *TypedClient) GetModelRecommendations(ctx context.Context, modelID string) (*ModelRecommendationsResponse, error) {
	raw, err := t.c.GetModelRecommendations(ctx, modelID)
	if err != nil {
		return nil, err
	}
	var out ModelRecommendationsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetGenerationTiers is the typed form of [Client.GetGenerationTiers].
func (t *TypedClient) GetGenerationTiers(ctx context.Context) (*GenerationTierListResponse, error) {
	body, reqURL, err := t.c.generationTiersBody(ctx)
	if err != nil {
		return nil, err
	}
	var out GenerationTierListResponse
	if err := decodeListBody(http.MethodGet, reqURL, body, "tiers", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListExperiments is the typed form of [Client.ListExperiments].
func (t *TypedClient) ListExperiments(ctx context.Context, opts ListExperimentsOptions) (*ExperimentListResponse, error) {
	body, reqURL, err := t.c.experimentsBody(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out ExperimentListResponse
	if err := decodeListBody(http.MethodGet, reqURL, body, "experiments", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateExperiment is the typed form of [Client.CreateExperiment].
func (t *TypedClient) CreateExperiment(ctx context.Context, body PlaygroundCreateRequest) (*CreateExperimentResponse, error) {
	raw, err := t.c.CreateExperiment(ctx, body)
	if err != nil {
		return nil, err
	}
	var out CreateExperimentResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetExperiment is the typed form of [Client.GetExperiment].
func (t *TypedClient) GetExperiment(ctx context.Context, experimentID string) (*ExperimentDetailResponse, error) {
	raw, err := t.c.GetExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	var out ExperimentDetailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelExperiment is the typed form of [Client.CancelExperiment].
func (t *TypedClient) CancelExperiment(ctx context.Context, experimentID string) (*CancelExperimentResponse, error) {
	raw, err := t.c.CancelExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	var out CancelExperimentResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Search is the typed form of [Client.Search].
func (t *TypedClient) Search(ctx context.Context, opts SearchOptions) (*SearchResponse, error) {
	raw, err := t.c.Search(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out SearchResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchDocs is the typed form of [Client.SearchDocs].
func (t *TypedClient) SearchDocs(ctx context.Context, opts DocsSearchOptions) (*DocsSearchResponse, error) {
	raw, err := t.c.SearchDocs(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out DocsSearchResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptMemoryBankAiSuggestion is the typed form of [Client.AcceptMemoryBankAiSuggestion].
func (t *TypedClient) AcceptMemoryBankAiSuggestion(ctx context.Context, conversationID string, body MemoryBankAcceptRequest) (*OkResponse, error) {
	raw, err := t.c.AcceptMemoryBankAiSuggestion(ctx, conversationID, body)
	if err != nil {
		return nil, err
	}
	var out OkResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptAiMemoryBankSuggestion is the typed form of [Client.AcceptAiMemoryBankSuggestion].
func (t *TypedClient) AcceptAiMemoryBankSuggestion(ctx context.Context, conversationID string, body MemoryBankAcceptRequest) (*OkResponse, error) {
	raw, err := t.c.AcceptAiMemoryBankSuggestion(ctx, conversationID, body)
	if err != nil {
		return nil, err
	}
	var out OkResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMemoryBankTemplates is the typed form of [Client.ListMemoryBankTemplates].
// The API declares no schema for a template, so each is a JSON object.
func (t *TypedClient) ListMemoryBankTemplates(ctx context.Context) ([]map[string]JsonValue, error) {
	body, reqURL, err := t.c.memoryBankTemplatesBody(ctx)
	if err != nil {
		return nil, err
	}
	var out []map[string]JsonValue
	if err := decodeListBody(http.MethodGet, reqURL, body, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAgentsUsingMemoryBank is the typed form of [Client.GetAgentsUsingMemoryBank].
// The API declares no schema for an entry; each is an object carrying agent_id
// and agent_name.
func (t *TypedClient) GetAgentsUsingMemoryBank(ctx context.Context, memoryBankID string) ([]map[string]JsonValue, error) {
	body, reqURL, err := t.c.agentsUsingMemoryBankBody(ctx, memoryBankID)
	if err != nil {
		return nil, err
	}
	var out []map[string]JsonValue
	if err := decodeListBody(http.MethodGet, reqURL, body, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}
