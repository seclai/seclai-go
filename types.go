package seclai

import (
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/seclai/seclai-go/generated"
)

// Typed API models (aliases of the generated OpenAPI models).
//
// These aliases let consumers import just "github.com/seclai/seclai-go" for types.

type HTTPValidationError = generated.HTTPValidationError
type ValidationError = generated.ValidationError
type PaginationResponse = generated.PaginationResponse

type AgentRunRequest = generated.AgentRunRequest
type AgentRunResponse = generated.AgentRunResponse
type AgentRunAttemptResponse = generated.AgentRunAttemptResponse

type AgentRunListResponse = generated.RoutersApiAgentsAgentRunListResponse
type SourceListResponse = generated.RoutersApiSourcesSourceListResponse
type ContentDetailResponse = generated.RoutersApiContentsContentDetailResponse
type ContentEmbeddingsListResponse = generated.RoutersApiContentsContentEmbeddingsListResponse

type FileUploadResponse = generated.FileUploadResponse

// File is the upload file type used by the generated client.
type File = openapi_types.File

// JsonValue is an arbitrary JSON value.
type JsonValue = generated.JsonValue
