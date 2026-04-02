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
type EvaluationResultWithCriteriaListResponse = generated.EvaluationResultWithCriteriaListResponse

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
type AiAssistantGenerateRequest = generated.AiAssistantGenerateRequest

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

// ── Identity ────────────────────────────────────────────────────────────────

// MeResponse is the response from the GET /me identity endpoint.
type MeResponse = generated.MeResponse

// OrganizationInfoResponse is an organization entry inside MeResponse.
type OrganizationInfoResponse = generated.OrganizationInfoResponse
