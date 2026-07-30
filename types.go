package seclai

import (
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/seclai/seclai-go/generated"
)

// Typed API models — aliases of the generated OpenAPI models.
//
// These aliases let consumers import just "github.com/seclai/seclai-go" for all types
// without reaching into the generated sub-package.

// ── Common ──────────────────────────────────────────────────────────────────

// HTTPValidationError is the standard validation error shape (typically HTTP 422).
type HTTPValidationError = generated.HTTPValidationError

// ValidationError is an individual validation error entry within an [HTTPValidationError].
type ValidationError = generated.ValidationError

// PaginationResponse contains pagination metadata included in list responses.
type PaginationResponse = generated.PaginationResponse

// JsonValue is an arbitrary JSON value (object, array, string, number, bool, or null).
type JsonValue = generated.JsonValue

// File is the upload file type used by the generated client.
type File = openapi_types.File

// InsufficientCreditsResponse is the 402 envelope returned when an account has
// exhausted its credits.
type InsufficientCreditsResponse = generated.InsufficientCreditsResponse

// InsufficientCreditsDetail is the detail body of an [InsufficientCreditsResponse]
// (error code, message, and account id).
type InsufficientCreditsDetail = generated.InsufficientCreditsDetail

// ── Agents ──────────────────────────────────────────────────────────────────

// AgentListResponse is a paginated list of agents.
type AgentListResponse = generated.RoutersApiAgentsAgentListResponse

// AgentSummaryResponse is a summary of an agent (returned on create/update/get).
type AgentSummaryResponse = generated.AgentSummaryResponse

// CreateAgentRequest is the request body for creating an agent.
type CreateAgentRequest = generated.RoutersApiAgentsCreateAgentRequest

// UpdateAgentRequest is the request body for updating an agent.
type UpdateAgentRequest = generated.RoutersApiAgentsUpdateAgentRequest

// ── Agent Definitions ───────────────────────────────────────────────────────

// AgentDefinitionResponse is the agent's step workflow definition.
type AgentDefinitionResponse = generated.AgentDefinitionResponse

// AgentExportResponse is a portable JSON snapshot of an agent definition.
type AgentExportResponse = generated.AgentExportResponse

// UpdateAgentDefinitionRequest is the request body for updating an agent definition.
type UpdateAgentDefinitionRequest = generated.UpdateAgentDefinitionRequest

// ── Agent Import ────────────────────────────────────────────────────────────

// AgentImportPreviewRequest is the request body for previewing an agent_definition import (dry-run validation).
type AgentImportPreviewRequest = generated.RoutersApiAgentsAgentImportPreviewRequest

// AgentImportPreviewResponse summarises a successfully validated agent_definition import payload.
type AgentImportPreviewResponse = generated.RoutersApiAgentsAgentImportPreviewResponse

// AgentDefinitionImportErrorResponse is the 422 body for invalid agent_definition payloads
// on create / update / preview-import. Errors carry 1-indexed line/column references into
// the canonical Source echo.
type AgentDefinitionImportErrorResponse = generated.AgentDefinitionImportErrorResponse

// ImportFieldErrorModel is a single validation error within an [AgentDefinitionImportErrorResponse]
// (with source position).
type ImportFieldErrorModel = generated.ImportFieldErrorModel

// ImportSkipResponse is one item dropped or substituted during an agent import (emitted in
// the ImportWarnings field on responses that accept an agent_definition).
type ImportSkipResponse = generated.ImportSkipResponse

// GovernancePolicyRefResponse is a reference to a governance policy (id and optional display name).
type GovernancePolicyRefResponse = generated.RoutersApiAgentsGovernancePolicyRefResponse

// ── Agent Runs ──────────────────────────────────────────────────────────────

// AgentRunRequest is the request body for starting an agent run.
type AgentRunRequest = generated.AgentRunRequest

// AgentRunStreamRequest is the request body for starting an agent run in streaming mode (SSE).
type AgentRunStreamRequest = generated.AgentRunStreamRequest

// AgentRunResponse describes an agent run.
type AgentRunResponse = generated.AgentRunResponse

// AgentRunAttemptResponse describes a single attempt within an agent run step.
type AgentRunAttemptResponse = generated.AgentRunAttemptResponse

// AgentRunStepResponse describes a single step within an agent run.
type AgentRunStepResponse = generated.AgentRunStepResponse

// AgentRunToolCallResponse is a single LLM tool call made during a prompt_call step
// (within [AgentRunStepResponse]'s ToolCalls).
type AgentRunToolCallResponse = generated.AgentRunToolCallResponse

// AgentRunListResponse is a paginated list of agent runs.
type AgentRunListResponse = generated.RoutersApiAgentsAgentRunListResponse

// AgentTraceSearchRequest is a search request for agent runs (traces).
type AgentTraceSearchRequest = generated.RoutersApiAgentsAgentTraceSearchRequest

// AgentTraceSearchResponse contains matching agent runs from a search.
type AgentTraceSearchResponse = generated.AgentTraceSearchResponse

// AgentTraceMatchResponse is an individual match within an agent trace search.
type AgentTraceMatchResponse = generated.AgentTraceMatchResponse

// ── Agent Input Uploads ─────────────────────────────────────────────────────

// UploadAgentInputApiResponse is the response from uploading a file input for an agent run.
type UploadAgentInputApiResponse = generated.UploadAgentInputApiResponse

// AgentAttachmentRefsApiResponse is the static attachment-reference contract for an
// agent — what files (if any) its templates expect on a run, so uploads can be
// staged before starting the run.
type AgentAttachmentRefsApiResponse = generated.AgentAttachmentRefsApiResponse

// AttachmentRefsSourceApiSummary is the per-source attachment-reference summary
// (exact names, indexes, glob patterns) within an [AgentAttachmentRefsApiResponse].
type AttachmentRefsSourceApiSummary = generated.AttachmentRefsSourceApiSummary

// ── Agent AI Assistant ──────────────────────────────────────────────────────

// GenerateAgentStepsRequest is the request body for generating agent workflow steps via AI.
type GenerateAgentStepsRequest = generated.GenerateAgentStepsRequest

// GenerateAgentStepsResponse contains AI-generated agent workflow steps.
type GenerateAgentStepsResponse = generated.GenerateAgentStepsResponse

// GenerateStepConfigRequest is the request body for generating a single step config via AI.
type GenerateStepConfigRequest = generated.GenerateStepConfigRequest

// GenerateStepConfigResponse contains an AI-generated step config.
type GenerateStepConfigResponse = generated.GenerateStepConfigResponse

// AiConversationHistoryResponse is the AI conversation history for an agent.
type AiConversationHistoryResponse = generated.AiConversationHistoryResponse

// AiConversationTurnResponse is an individual turn in an AI conversation.
type AiConversationTurnResponse = generated.AiConversationTurnResponse

// MarkAiSuggestionRequest is the request body for marking an AI suggestion as accepted/rejected.
type MarkAiSuggestionRequest = generated.MarkAiSuggestionRequest

// ExamplePrompt is an example prompt entry used in step config generation.
type ExamplePrompt = generated.ExamplePrompt

// ── Agent Evaluations ───────────────────────────────────────────────────────

// EvaluationCriteriaResponse is the evaluation criteria configuration for an agent.
type EvaluationCriteriaResponse = generated.EvaluationCriteriaResponse

// EvaluationCriteriaListResponse is a page of evaluation criteria.
//
// Not a generated type: the endpoint returns a bare array by default and only
// emits the canonical {data, pagination} envelope once the caller opts in with
// Options.APIVersion of 2026-07-27 or later, so Pagination is nil unless opted in.
type EvaluationCriteriaListResponse struct {
	Data       []EvaluationCriteriaResponse `json:"data"`
	Pagination *PaginationResponse          `json:"pagination,omitempty"`
}

// AlertResponse is a single alert.
type AlertResponse = generated.RoutersApiAlertsAlertResponse

// AlertListResponse is a page of alerts. Always the canonical {data, pagination} envelope.
type AlertListResponse = generated.RoutersApiAlertsAlertListResponse

// AlertConfigResponse is a single alert configuration.
type AlertConfigResponse = generated.AlertConfigResponse

// AlertConfigListResponse is a page of alert configurations.
//
// Not a generated type: the top-level key is version-gated. By default the
// configurations arrive under Configs alongside Total; once Options.APIVersion
// is 2026-07-27 or later the endpoint returns the canonical {data, pagination}
// envelope instead. Items returns whichever arrived.
type AlertConfigListResponse struct {
	Configs    []AlertConfigResponse `json:"configs,omitempty"`
	Data       []AlertConfigResponse `json:"data,omitempty"`
	Pagination *PaginationResponse   `json:"pagination,omitempty"`
	Total      int                   `json:"total,omitempty"`
}

// Items returns the configurations from whichever key the response used.
func (r AlertConfigListResponse) Items() []AlertConfigResponse {
	// Presence, not length. An empty canonical page is `{"data": [], ...}`, which
	// unmarshals to a non-nil empty slice; testing len() would fall through to
	// the legacy key and report the legacy list for a perfectly valid empty page.
	if r.Data != nil {
		return r.Data
	}
	return r.Configs
}

// ModelAlertResponse is a single model lifecycle alert.
type ModelAlertResponse = generated.RoutersApiModelLifecycleModelAlertResponse

// ModelAlertListResponse is a page of model lifecycle alerts.
//
// Not a generated type: the top-level key is version-gated. By default the
// alerts arrive under Alerts alongside Total; once Options.APIVersion is
// 2026-07-27 or later the endpoint returns the canonical {data, pagination}
// envelope instead. Items returns whichever arrived.
type ModelAlertListResponse struct {
	Alerts     []ModelAlertResponse `json:"alerts,omitempty"`
	Data       []ModelAlertResponse `json:"data,omitempty"`
	Pagination *PaginationResponse  `json:"pagination,omitempty"`
	Total      int                  `json:"total,omitempty"`
}

// Items returns the alerts from whichever key the response used.
func (r ModelAlertListResponse) Items() []ModelAlertResponse {
	// Presence, not length — see AlertConfigListResponse.Items.
	if r.Data != nil {
		return r.Data
	}
	return r.Alerts
}

// ── Typed response models (see [TypedClient]) ───────────────────────────────

// AlertDetailResponse is a single alert with its comments, subscribers and history.
type AlertDetailResponse = generated.RoutersApiAlertsAlertDetailResponse

// AlertCommentResponse is a comment on an alert.
type AlertCommentResponse = generated.RoutersApiAlertsAlertCommentResponse

// AlertSubscriberResponse is a subscriber to an alert.
type AlertSubscriberResponse = generated.RoutersApiAlertsAlertSubscriberResponse

// UnreadCountResponse is a count of unread items.
type UnreadCountResponse = generated.UnreadCountResponse

// ModelRecommendationsResponse is the set of successor recommendations for a model.
type ModelRecommendationsResponse = generated.RoutersApiModelLifecycleModelRecommendationsResponse

// ModelRecommendationResponse is a single successor recommendation.
type ModelRecommendationResponse = generated.RoutersApiModelLifecycleModelRecommendationResponse

// GenerationTierListResponse maps each media-generation modality and tier to its model and cost.
type GenerationTierListResponse = generated.GenerationTierListResponse

// GenerationTierResponse is one media-generation tier.
type GenerationTierResponse = generated.GenerationTierResponse

// ExperimentListResponse is a page of model playground experiments.
type ExperimentListResponse = generated.ExperimentListResponse

// ExperimentSummaryResponse is a model playground experiment in a listing.
type ExperimentSummaryResponse = generated.ExperimentSummaryResponse

// ExperimentDetailResponse is a model playground experiment with its results.
type ExperimentDetailResponse = generated.ExperimentDetailResponse

// CreateExperimentResponse is the acknowledgement of a created experiment.
type CreateExperimentResponse = generated.CreateExperimentResponse

// CancelExperimentResponse is the acknowledgement of a cancelled experiment.
type CancelExperimentResponse = generated.CancelExperimentResponse

// SearchResponse is a ranked set of search results. Deliberately not paginated.
type SearchResponse = generated.RoutersApiSearchSearchResponse

// SearchResultResponse is a single search hit.
type SearchResultResponse = generated.RoutersApiSearchSearchResultResponse

// DocsSearchResponse is a set of documentation search results.
type DocsSearchResponse = generated.RoutersApiDocsSearchDocsSearchResponse

// DocsSearchResultResponse is a single documentation search hit.
type DocsSearchResultResponse = generated.DocsSearchResultResponse

// OkResponse is a simple acknowledgement.
type OkResponse = generated.OkResponse

// ApiVersionResponse is the API version a request resolved to, and the versions available.
type ApiVersionResponse = generated.ApiVersionResponse

// UpdateApiVersionRequest sets or clears the account's sticky API version pin.
type UpdateApiVersionRequest = generated.UpdateApiVersionRequest

// CreateEvaluationCriteriaRequest is the request body for creating evaluation criteria.
type CreateEvaluationCriteriaRequest = generated.CreateEvaluationCriteriaRequest

// UpdateEvaluationCriteriaRequest is the request body for updating evaluation criteria.
type UpdateEvaluationCriteriaRequest = generated.UpdateEvaluationCriteriaRequest

// EvaluationResultResponse is an individual evaluation result.
type EvaluationResultResponse = generated.EvaluationResultResponse

// EvaluationResultListResponse is a paginated list of evaluation results.
type EvaluationResultListResponse = generated.EvaluationResultListResponse

// EvaluationResultSummaryResponse is a summary of evaluation results for a criteria.
type EvaluationResultSummaryResponse = generated.EvaluationResultSummaryResponse

// EvaluationResultWithCriteriaListResponse is a paginated list of evaluation results with criteria context.
// EvaluationResultWithCriteriaListResponse is a page of evaluation results with
// criteria context.
//
// Not a generated type: two endpoints share this shape and populate different
// halves of it.
//
//   - GET /agents/{id}/evaluation-results is always paginated and fills Total,
//     Page and Limit.
//   - GET /agents/{id}/runs/{runID}/evaluation-results is version-gated: a bare
//     array by default, and the canonical {data, pagination} envelope once
//     Options.APIVersion is 2026-07-27 or later — in which case the metadata is
//     on Pagination and the flat fields stay zero.
type EvaluationResultWithCriteriaListResponse struct {
	Data []EvaluationResultWithCriteriaResponse `json:"data"`
	// Pagination carries the canonical metadata; nil on the flat and legacy shapes.
	Pagination *PaginationResponse `json:"pagination,omitempty"`
	// Total, Page and Limit are the flat shape; zero when Pagination is set.
	Total int `json:"total,omitempty"`
	Page  int `json:"page,omitempty"`
	Limit int `json:"limit,omitempty"`
}

// CreateEvaluationResultRequest is the request body for creating a manual evaluation result.
type CreateEvaluationResultRequest = generated.CreateEvaluationResultRequest

// CompatibleRunListResponse is a paginated list of runs compatible with a specific evaluation criteria.
type CompatibleRunListResponse = generated.CompatibleRunListResponse

// TestDraftEvaluationRequest is the request for testing a draft evaluation.
type TestDraftEvaluationRequest = generated.TestDraftEvaluationRequest

// TestDraftEvaluationResponse is the response from testing a draft evaluation.
type TestDraftEvaluationResponse = generated.TestDraftEvaluationResponse

// EvaluationRunSummaryListResponse is a paginated list of evaluation run summaries.
type EvaluationRunSummaryListResponse = generated.EvaluationRunSummaryListResponse

// NonManualEvaluationSummaryResponse is a summary of non-manual (automated) evaluation results.
type NonManualEvaluationSummaryResponse = generated.SchemasV1AgentEvaluationsNonManualEvaluationSummaryResponse

// NonManualEvaluationModeStatResponse is per-mode stats within a non-manual evaluation summary.
type NonManualEvaluationModeStatResponse = generated.SchemasV1AgentEvaluationsNonManualEvaluationModeStatResponse

// EvaluationResultWithCriteriaResponse is an evaluation result including criteria context.
type EvaluationResultWithCriteriaResponse = generated.EvaluationResultWithCriteriaResponse

// EvaluationRunSummaryResponse is a per-run evaluation summary with pass/fail/error breakdown.
type EvaluationRunSummaryResponse = generated.EvaluationRunSummaryResponse

// CompatibleRunResponse is a run that has a completed step matching a criteria's step_id.
type CompatibleRunResponse = generated.CompatibleRunResponse

// AgentEvaluationTier controls model selection for agent evaluation: "fast", "balanced", or "thorough".
type AgentEvaluationTier = generated.AgentEvaluationTier

// EvaluationStatus is the result status of a single evaluation: "pending", "passed", "failed", "skipped", or "error".
type EvaluationStatus = generated.EvaluationStatus

// ── Knowledge Bases ─────────────────────────────────────────────────────────

// KnowledgeBaseListResponse is a paginated list of knowledge bases.
type KnowledgeBaseListResponse = generated.KnowledgeBaseListResponseModel

// KnowledgeBaseResponse is the full knowledge base configuration and metadata.
type KnowledgeBaseResponse = generated.KnowledgeBaseResponseModel

// CreateKnowledgeBaseBody is the request body for creating a knowledge base.
type CreateKnowledgeBaseBody = generated.CreateKnowledgeBaseBody

// UpdateKnowledgeBaseBody is the request body for updating a knowledge base.
type UpdateKnowledgeBaseBody = generated.UpdateKnowledgeBaseBody

// ── Memory Banks ────────────────────────────────────────────────────────────

// MemoryBankListResponse is a paginated list of memory banks.
type MemoryBankListResponse = generated.MemoryBankListResponseModel

// MemoryBankResponse is the full memory bank configuration and metadata.
type MemoryBankResponse = generated.MemoryBankResponseModel

// CreateMemoryBankBody is the request body for creating a memory bank.
type CreateMemoryBankBody = generated.CreateMemoryBankBody

// UpdateMemoryBankBody is the request body for updating a memory bank.
type UpdateMemoryBankBody = generated.UpdateMemoryBankBody

// MemoryBankAiAssistantRequest is the request body for the memory bank AI assistant.
type MemoryBankAiAssistantRequest = generated.RoutersApiMemoryBanksMemoryBankAiAssistantRequest

// MemoryBankAiAssistantResponse is the response from the memory bank AI assistant.
type MemoryBankAiAssistantResponse = generated.MemoryBankAiAssistantResponse

// MemoryBankAcceptRequest is the request body for accepting a memory bank AI suggestion.
type MemoryBankAcceptRequest = generated.RoutersApiMemoryBanksMemoryBankAcceptRequest

// MemoryBankLastConversationResponse is the last AI assistant conversation for memory banks.
type MemoryBankLastConversationResponse = generated.RoutersApiMemoryBanksMemoryBankLastConversationResponse

// TestCompactionRequest is the request body for testing memory bank compaction.
type TestCompactionRequest = generated.TestCompactionRequest

// StandaloneTestCompactionRequest is the request body for testing compaction without a memory bank.
type StandaloneTestCompactionRequest = generated.StandaloneTestCompactionRequest

// CompactionTestResponse is the response from a compaction test.
type CompactionTestResponse = generated.CompactionTestResponseModel

// CompactionEvaluationModel is a structured LLM-as-judge evaluation result.
type CompactionEvaluationModel = generated.CompactionEvaluationModel

// MemoryBankConversationTurnResponse is a single turn of memory bank AI assistant conversation.
type MemoryBankConversationTurnResponse = generated.RoutersApiMemoryBanksMemoryBankConversationTurnResponse

// MemoryBankConfigResponse is the suggested memory bank configuration from the AI assistant.
type MemoryBankConfigResponse = generated.MemoryBankConfigResponse

// ── Sources ─────────────────────────────────────────────────────────────────

// SourceListResponse is a paginated list of sources.
type SourceListResponse = generated.RoutersApiSourcesSourceListResponse

// SourceResponse is the full source response with metadata and configuration.
type SourceResponse = generated.SourceResponse

// SourceConnectionResponse is a detailed source connection model.
type SourceConnectionResponse = generated.SourceConnectionResponseModel

// CreateSourceBody is the request body for creating a source.
type CreateSourceBody = generated.CreateSourceBody

// UpdateSourceBody is the request body for updating a source.
type UpdateSourceBody = generated.UpdateSourceBody

// FileUploadResponse is the upload response for file uploads to a source.
type FileUploadResponse = generated.RoutersApiSourcesFileUploadResponse

// InlineTextUploadRequest is the request body for uploading inline text to a source.
type InlineTextUploadRequest = generated.InlineTextUploadRequest

// ── Source Exports ──────────────────────────────────────────────────────────

// ExportListResponse is a paginated list of source exports.
type ExportListResponse = generated.ExportListResponse

// ExportResponse is a single source export.
type ExportResponse = generated.RoutersApiSourceExportsExportResponse

// CreateExportRequest is the request body for creating a source export.
type CreateExportRequest = generated.RoutersApiSourceExportsCreateExportRequest

// EstimateExportRequest is the request body for estimating a source export.
type EstimateExportRequest = generated.RoutersApiSourceExportsEstimateExportRequest

// EstimateExportResponse is the response for a source export estimate.
type EstimateExportResponse = generated.RoutersApiSourceExportsEstimateExportResponse

// ExportFormat represents supported export formats: "csv", "jsonl", "parquet", "zip".
type ExportFormat = generated.ExportFormat

// ── Source Embedding Migrations ─────────────────────────────────────────────

// SourceEmbeddingMigrationResponse is the status of a source embedding migration.
type SourceEmbeddingMigrationResponse = generated.SourceEmbeddingMigrationResponse

// StartSourceEmbeddingMigrationRequest is the request body for starting an embedding migration.
type StartSourceEmbeddingMigrationRequest = generated.StartSourceEmbeddingMigrationRequest

// ── Content ─────────────────────────────────────────────────────────────────

// ContentDetailResponse is the content detail for a specific content version.
type ContentDetailResponse = generated.RoutersApiContentsContentDetailResponse

// ContentEmbeddingsListResponse is a paginated list of content embeddings.
type ContentEmbeddingsListResponse = generated.RoutersApiContentsContentEmbeddingsListResponse

// ContentEmbeddingResponse is an individual content embedding.
type ContentEmbeddingResponse = generated.ContentEmbeddingResponse

// ContentFileUploadResponse is the upload response for content replacement uploads.
type ContentFileUploadResponse = generated.RoutersApiContentsFileUploadResponse

// InlineTextReplaceRequest is the request body for replacing content with inline text.
type InlineTextReplaceRequest = generated.InlineTextReplaceRequest

// ── Solutions ───────────────────────────────────────────────────────────────

// SolutionListResponse is a paginated list of solutions.
type SolutionListResponse = generated.RoutersApiSolutionsSolutionListResponse

// SolutionResponse is the full solution configuration and metadata.
type SolutionResponse = generated.RoutersApiSolutionsSolutionResponse

// SolutionSummaryResponse is a summary of a solution.
type SolutionSummaryResponse = generated.SolutionSummaryResponse

// CreateSolutionRequest is the request body for creating a solution.
type CreateSolutionRequest = generated.CreateSolutionRequest

// UpdateSolutionRequest is the request body for updating a solution.
type UpdateSolutionRequest = generated.UpdateSolutionRequest

// LinkResourcesRequest is the request body for linking resources (agents, KBs, sources) to a parent.
type LinkResourcesRequest = generated.LinkResourcesRequest

// UnlinkResourcesRequest is the request body for unlinking resources from a parent.
type UnlinkResourcesRequest = generated.UnlinkResourcesRequest

// SolutionConversationResponse is a conversation in a solution AI assistant.
type SolutionConversationResponse = generated.RoutersApiSolutionsSolutionConversationResponse

// AddConversationTurnRequest is the request body for adding a conversation turn.
type AddConversationTurnRequest = generated.AddConversationTurnRequest

// MarkConversationTurnRequest is the request body for marking a conversation turn as accepted/rejected.
type MarkConversationTurnRequest = generated.MarkConversationTurnRequest

// AiAssistantGenerateRequest is the request body for generating an AI assistant plan.
type AiAssistantGenerateRequest = generated.RoutersApiSolutionsAiAssistantGenerateRequest

// AiAssistantGenerateResponse is the AI assistant generated plan response.
type AiAssistantGenerateResponse = generated.AiAssistantGenerateResponse

// AiAssistantAcceptRequest is the request body for accepting an AI assistant plan.
type AiAssistantAcceptRequest = generated.RoutersApiSolutionsAiAssistantAcceptRequest

// AiAssistantAcceptResponse is the response from accepting an AI assistant plan.
type AiAssistantAcceptResponse = generated.AiAssistantAcceptResponse

// ProposedActionResponse is a proposed action in a solution AI plan.
type ProposedActionResponse = generated.ProposedActionResponse

// ExecutedActionResponse is an executed action result in a solution AI plan.
type ExecutedActionResponse = generated.ExecutedActionResponse

// ── Governance ──────────────────────────────────────────────────────────────

// GovernanceAiAssistantRequest is the request body for the governance AI assistant.
type GovernanceAiAssistantRequest = generated.RoutersApiGovernanceGovernanceAiAssistantRequest

// GovernanceAiAssistantResponse is the governance AI assistant response.
type GovernanceAiAssistantResponse = generated.GovernanceAiAssistantResponse

// GovernanceConversationResponse is a governance AI conversation turn.
type GovernanceConversationResponse = generated.RoutersApiGovernanceGovernanceConversationResponse

// GovernanceAiAcceptResponse is the response from accepting a governance AI plan.
type GovernanceAiAcceptResponse = generated.GovernanceAiAcceptResponse

// GovernanceProposedPolicyActionResponse is a single proposed governance policy action.
type GovernanceProposedPolicyActionResponse = generated.ProposedPolicyActionResponse

// GovernanceAppliedActionResponse is the result of a single executed governance action.
type GovernanceAppliedActionResponse = generated.AppliedActionResponse

// ── Alerts ──────────────────────────────────────────────────────────────────

// CreateAlertConfigRequest is the request body for creating an alert configuration.
type CreateAlertConfigRequest = generated.CreateAlertConfigRequest

// UpdateAlertConfigRequest is the request body for updating an alert configuration.
type UpdateAlertConfigRequest = generated.UpdateAlertConfigRequest

// ChangeStatusRequest is the request body for changing an alert's status.
type ChangeStatusRequest = generated.ChangeStatusRequest

// AddCommentRequest is the request body for adding a comment to an alert.
type AddCommentRequest = generated.RoutersApiAlertsAddCommentRequest

// OrganizationAlertPreferenceListResponse is a paginated list of organization alert preferences.
type OrganizationAlertPreferenceListResponse = generated.OrganizationAlertPreferenceListResponse

// OrganizationAlertPreferenceResponse is a single organization alert preference.
type OrganizationAlertPreferenceResponse = generated.RoutersApiAlertsOrganizationAlertPreferenceResponse

// UpdateOrganizationAlertPreferenceRequest is the request body for updating an organization alert preference.
type UpdateOrganizationAlertPreferenceRequest = generated.RoutersApiAlertsUpdateOrganizationAlertPreferenceRequest

// ── AI Assistant (Top-Level) ────────────────────────────────────────────────

// AiAssistantFeedbackRequest is the request body for submitting AI assistant feedback.
type AiAssistantFeedbackRequest = generated.RoutersApiAiAssistantAiAssistantFeedbackRequest

// AiAssistantFeedbackResponse is the response from submitting AI assistant feedback.
type AiAssistantFeedbackResponse = generated.AiAssistantFeedbackResponse

// ── Models ──────────────────────────────────────────────────────────────────

// ProviderGroupResponse is a group of models from a single provider.
type ProviderGroupResponse = generated.SchemasModelResponsesProviderGroupResponse

// PromptModelResponse is the full detail response for a single model.
type PromptModelResponse = generated.SchemasModelResponsesPromptModelResponse

// ModalityRateResponse is a per-modality rate for a model that prices
// image/audio/video distinctly from its default text rate.
type ModalityRateResponse = generated.ModalityRateResponse

// PlaygroundCreateRequest is the request body for creating a model playground experiment.
type PlaygroundCreateRequest = generated.PlaygroundCreateRequest

// ── Identity ────────────────────────────────────────────────────────────────

// MeResponse is the response from the GET /me identity endpoint.
type MeResponse = generated.MeResponse

// OrganizationInfoResponse is an organization entry inside MeResponse.
type OrganizationInfoResponse = generated.OrganizationInfoResponse

// ── Agent Email Triggers ────────────────────────────────────────────────────

// RoutersApiAgentsSetEmailTriggerConfigRequest is the request body for configuring
// an EMAIL_RECEIVED trigger. A field left at its zero value is unchanged.
type RoutersApiAgentsSetEmailTriggerConfigRequest = generated.RoutersApiAgentsSetEmailTriggerConfigRequest

// EmailTriggerConfigResponse is an EMAIL_RECEIVED trigger's resolved email
// address(es) and inbound-handling configuration.
type EmailTriggerConfigResponse = generated.EmailTriggerConfigResponse

// ── Agent Email Governance ──────────────────────────────────────────────────

// AgentEmailOptOutResponse is a recipient's opt-out from an account's agent
// emails, for one agent or account-wide.
type AgentEmailOptOutResponse = generated.AgentEmailOptOutResponse

// AgentEmailOptOutListResponse is a page of agent-email opt-outs plus the total.
type AgentEmailOptOutListResponse = generated.AgentEmailOptOutListResponse

// BlockEmailSenderRequest is the request body for blocking an inbound sender or domain.
type BlockEmailSenderRequest = generated.BlockEmailSenderRequest

// BlockedEmailSenderResponse is a single blocked inbound email sender.
type BlockedEmailSenderResponse = generated.BlockedEmailSenderResponse

// BlockedEmailSenderListResponse is a page of blocked senders plus the account's
// governance auto-block mode.
type BlockedEmailSenderListResponse = generated.BlockedEmailSenderListResponse

// SetAutoBlockModeRequest is the request body for setting the auto-block mode.
type SetAutoBlockModeRequest = generated.SetAutoBlockModeRequest

// InboundEmailRejectionResponse is an inbound email discarded before running an agent.
type InboundEmailRejectionResponse = generated.InboundEmailRejectionResponse

// InboundEmailStatusResponse is the account-wide inbound-email overload state.
type InboundEmailStatusResponse = generated.InboundEmailStatusResponse

// CancelQueuedRunsResponse reports how many queued inbound-email runs were cancelled.
type CancelQueuedRunsResponse = generated.CancelQueuedRunsResponse

// ResumeInboundResponse reports the result of lifting the inbound-email pause.
type ResumeInboundResponse = generated.ResumeInboundResponse

// AgentCallerApiResponse is a live agent that calls another agent via a
// call_agent step, blocking the callee from being disabled.
type AgentCallerApiResponse = generated.AgentCallerApiResponse

// ── Email Domains ───────────────────────────────────────────────────────────

// AddEmailDomainRequest is the request body for adding an agent-email domain.
type AddEmailDomainRequest = generated.AddEmailDomainRequest

// EmailDomainResponse is an agent-email domain with its verification status and
// the DNS records the customer must publish.
type EmailDomainResponse = generated.EmailDomainResponse

// EmailDomainsListResponse is the account's email domains plus plan capabilities.
type EmailDomainsListResponse = generated.EmailDomainsListResponse

// RemoveEmailDomainResponse is the result of removing an email domain, including
// an optional registrar cleanup note.
type RemoveEmailDomainResponse = generated.RemoveEmailDomainResponse

// SendTestEmailResponse is the result of sending a test email from a domain.
type SendTestEmailResponse = generated.SendTestEmailResponse

// DnsRecordResponse is a DNS record the customer must publish for a domain.
type DnsRecordResponse = generated.DnsRecordResponse

// DnsProviderResponse is the detected DNS provider for a domain.
type DnsProviderResponse = generated.DnsProviderResponse

// DmarcSummaryResponse is a DMARC aggregate-report summary for a domain.
type DmarcSummaryResponse = generated.DmarcSummaryResponse

// DmarcFailingSourceResponse is a top DMARC-failing source IP within a
// [DmarcSummaryResponse].
type DmarcFailingSourceResponse = generated.DmarcFailingSourceResponse
