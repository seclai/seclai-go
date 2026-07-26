# Seclai Go SDK

The official Go SDK for the [Seclai](https://seclai.com) API. Provides typed wrappers for the Seclai API, file uploads, SSE streaming, polling helpers, and pagination support.

Requires Go 1.22+.

## Install

```bash
go get github.com/seclai/seclai-go@latest
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	seclai "github.com/seclai/seclai-go"
)

func main() {
	client, err := seclai.NewClient(seclai.Options{})
	if err != nil {
		log.Fatal(err)
	}

	// List all sources
	sources, err := client.ListSources(context.Background(), seclai.ListSourcesOptions{
		Page: 1, Limit: 20,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("sources:", len(sources.Data))

	// Run an agent and stream the result
	run, err := client.RunStreamingAgentAndWait(context.Background(), "agent_id", seclai.AgentRunStreamRequest{
		Input: "Summarize the latest uploads",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("run:", run.RunId, "status:", run.Status)
}
```

## Configuration

| Option | Environment variable | Default |
| --- | --- | --- |
| `APIKey` | `SECLAI_API_KEY` | — |
| `AccessToken` | — | — |
| `AccessTokenProvider` | — | — |
| `Profile` | `SECLAI_PROFILE` | `"default"` |
| `ConfigDir` | `SECLAI_CONFIG_DIR` | `~/.seclai` |
| `AutoRefresh` | — | `true` |
| `AccountID` | — | — |
| `BaseURL` | `SECLAI_API_URL` | `https://seclai.com` |
| `APIKeyHeader` | — | `x-api-key` |
| `DefaultHeaders` | — | `nil` |
| `HTTPClient` | — | `&http.Client{Timeout: 30s}` |

> At least one credential must be provided via the options above, the
> `SECLAI_API_KEY` environment variable, or an SSO profile
> (see [Authentication](#authentication) below).

### Authentication

Credentials are resolved via a chain (first match wins):

1. Explicit `APIKey` option
2. Explicit `AccessToken` option (static string)
3. Explicit `AccessTokenProvider` option (`func(ctx) (string, error)`, called per request)
4. `SECLAI_API_KEY` environment variable
5. SSO profile from `~/.seclai/config` with cached tokens in `~/.seclai/sso/cache/`

```go
// API key
client, _ := seclai.NewClient(seclai.Options{APIKey: "sk-..."})

// Static bearer token
client, _ := seclai.NewClient(seclai.Options{AccessToken: "eyJhbGciOi..."})

// Dynamic bearer token provider (called per request)
client, _ := seclai.NewClient(seclai.Options{
	AccessTokenProvider: func(ctx context.Context) (string, error) {
		return getTokenFromVault(ctx)
	},
})

// SSO profile (uses cached tokens, auto-refreshes)
client, _ := seclai.NewClient(seclai.Options{Profile: "my-profile"})

// Environment variable (no options needed)
// export SECLAI_API_KEY="sk-..."
client, _ := seclai.NewClient(seclai.Options{})
```

#### SSO authentication

SSO is the default fallback when no explicit credentials are provided. The SDK
includes built-in production SSO defaults, so a single `auth login` is enough:

```bash
npx @seclai/cli auth login    # authenticate via browser — works immediately
```

To customize SSO settings (e.g. for a staging environment), create a profile
with `seclai configure sso` or set environment variables:

| Variable | Description | Default |
|---|---|---|
| `SECLAI_SSO_DOMAIN` | Cognito domain | `auth.seclai.com` |
| `SECLAI_SSO_CLIENT_ID` | Cognito app client ID | `4bgf8v9qmc5puivbaqon9n5lmr` |
| `SECLAI_SSO_REGION` | AWS region | `us-west-2` |

Environment variables take precedence over config file values, which take
precedence over built-in defaults.

## API documentation

Online API documentation (latest):

https://seclai.github.io/seclai-go/latest/

## Resources

### Identity

```go
me, _ := client.GetMe(ctx)
for _, org := range me.Organizations {
    fmt.Println(org.Name, org.AccountId)
}
// Act as an organization: seclai.NewClient(seclai.Options{AccountID: org.AccountId.String()})
```

### Agents

```go
ctx := context.Background()

// CRUD
agents, _ := client.ListAgents(ctx, seclai.ListOptions{Page: 1, Limit: 20})
agent, _ := client.CreateAgent(ctx, seclai.CreateAgentRequest{Name: "My Agent"})

// Pause / resume — a disabled agent stops firing from every trigger path
callers, _ := client.GetAgentCallers(ctx, "agent_id") // live agents calling this one
_, _ = client.DisableAgent(ctx, "agent_id")           // 409 if any caller is still live
_, _ = client.EnableAgent(ctx, "agent_id")
fetched, _ := client.GetAgent(ctx, "agent_id")
updated, _ := client.UpdateAgent(ctx, "agent_id", seclai.UpdateAgentRequest{})
_ = client.DeleteAgent(ctx, "agent_id")

// Definition (step workflow)
def, _ := client.GetAgentDefinition(ctx, "agent_id")
_, _ = client.UpdateAgentDefinition(ctx, "agent_id", seclai.UpdateAgentDefinitionRequest{
	// Steps: ...,
	// ChangeId: def.ChangeId,
})

// Export / import an agent
exported, _ := client.ExportAgent(ctx, "agent_id", false)

// Round-trip the export through a generic map (the import endpoints accept
// the same shape, but as a `map[string]any`).
buf, _ := json.Marshal(exported)
var payload map[string]any
_ = json.Unmarshal(buf, &payload)

// Validate the payload first to surface unresolved entity refs in this account.
preview, _ := client.PreviewImportAgent(ctx, seclai.AgentImportPreviewRequest{AgentDefinition: payload})
entityRemap := map[string]string{}
if preview.UnresolvedRefs != nil {
	for _, ref := range *preview.UnresolvedRefs {
		if id, ok := ref["ref_id"].(string); ok {
			// Replace "<target-uuid>" with an id from ref["alternatives"]
			// before calling CreateAgent; empty values are rejected.
			entityRemap[id] = "<target-uuid>"
		}
	}
}

// Commit — EntityRemap substitutes workflow refs before save.
trigger := "dynamic_input"
imported, _ := client.CreateAgent(ctx, seclai.CreateAgentRequest{
	Name:            "Imported",
	TriggerType:     &trigger,
	AgentDefinition: &payload,
	EntityRemap:     &entityRemap,
})
_ = imported.ImportWarnings // items that couldn't be applied, if any
```

### Agent runs

```go
// Start a run
run, _ := client.RunAgent(ctx, "agent_id", seclai.AgentRunRequest{Input: "Hello"})

// List & search runs
runs, _ := client.ListAgentRuns(ctx, "agent_id", seclai.ListAgentRunsOptions{Status: "completed"})
search, _ := client.SearchAgentRuns(ctx, seclai.AgentTraceSearchRequest{})

// Fetch run details (optionally with step outputs)
detail, _ := client.GetAgentRun(ctx, "run_id", &seclai.GetAgentRunOptions{IncludeStepOutputs: true})

// Cancel or delete
_, _ = client.CancelAgentRun(ctx, "run_id")
_ = client.DeleteAgentRun(ctx, "run_id")
```

### Streaming

The SDK provides two streaming patterns over the SSE `/runs/stream` endpoint.

**Block until done** — returns the final `done` payload or returns an error on timeout:

```go
// If ctx has no deadline, the SDK applies a default timeout.
run, err := client.RunStreamingAgentAndWait(ctx, "agent_id", seclai.AgentRunStreamRequest{
	Input:    "Hello from streaming",
	Metadata: map[string]any{"app": "My App"},
})
```

**Channel-based** — yields every SSE event as `AgentRunEvent`:

```go
events, errCh := client.RunStreamingAgent(ctx, "agent_id", seclai.AgentRunStreamRequest{
	Input: "Hello",
})
for evt := range events {
	fmt.Println(evt.Event, evt.Run)
}
if err := <-errCh; err != nil {
	log.Fatal(err)
}
```

### Polling

For environments where SSE is not practical, poll for a completed run:

```go
result, err := client.RunAgentAndPoll(ctx, "agent_id", seclai.AgentRunRequest{
	Input: "Hello",
}, &seclai.RunAgentAndPollOptions{
	PollInterval: 2 * time.Second,
})
```

### Agent input uploads

```go
// Discover which files (if any) the agent expects before staging uploads.
refs, _ := client.GetAgentAttachmentReferences(ctx, "agent_id")
// refs.RequiresUploads reports whether the agent accepts files; refs.Agent lists
// the exact names / indexes / glob patterns a run-time upload batch must satisfy.

upload, _ := client.UploadAgentInput(ctx, "agent_id", seclai.UploadFileRequest{
	File: data, FileName: "input.pdf",
})
status, _ := client.GetAgentInputUploadStatus(ctx, "agent_id", upload.UploadId)
```

### Agent run attachments

```go
// Download a file emitted by a step in an agent run. The attachmentID is the
// URL-safe-base64 storage_key surfaced in run output manifests / webhooks.
// Pass "" for downloadName to omit the filename hint. The caller closes the body.
resp, _ := client.DownloadAgentRunAttachment(ctx, "run_id", "attachment_id", "")
defer resp.Body.Close() // raw *http.Response — stream or save the bytes
```

### Agent AI assistant

```go
steps, _ := client.GenerateAgentSteps(ctx, "agent_id", seclai.GenerateAgentStepsRequest{
	UserInput: "Build a RAG pipeline",
})
config, _ := client.GenerateStepConfig(ctx, "agent_id", seclai.GenerateStepConfigRequest{
	StepType: "llm", UserInput: "...",
})

// Conversation history
history, _ := client.GetAgentAiConversationHistory(ctx, "agent_id")
_ = client.MarkAgentAiSuggestion(ctx, "agent_id", "conversation_id", seclai.MarkAiSuggestionRequest{Accepted: true})
```

### Agent evaluations

```go
criteria, _ := client.ListEvaluationCriteria(ctx, "agent_id", seclai.ListOptions{})
created, _ := client.CreateEvaluationCriteria(ctx, "agent_id", seclai.CreateEvaluationCriteriaRequest{})
detail, _ := client.GetEvaluationCriteria(ctx, "criteria_id")
_, _ = client.UpdateEvaluationCriteria(ctx, "criteria_id", seclai.UpdateEvaluationCriteriaRequest{})
_ = client.DeleteEvaluationCriteria(ctx, "criteria_id")

// Test a draft
_, _ = client.TestDraftEvaluation(ctx, "agent_id", seclai.TestDraftEvaluationRequest{})

// Results & summaries
results, _ := client.ListEvaluationResults(ctx, "criteria_id", seclai.ListOptions{})
summary, _ := client.GetEvaluationCriteriaSummary(ctx, "criteria_id")
_, _ = client.CreateEvaluationResult(ctx, "criteria_id", seclai.CreateEvaluationResultRequest{})

// Results by run
runResults, _ := client.ListRunEvaluationResults(ctx, "agent_id", "run_id", seclai.ListOptions{})
nonManual, _ := client.GetNonManualEvaluationSummary(ctx, "agent_id")
compatible, _ := client.ListCompatibleRuns(ctx, "criteria_id", seclai.ListOptions{})
```

### Knowledge bases

```go
kbs, _ := client.ListKnowledgeBases(ctx, seclai.SortableListOptions{})
kb, _ := client.CreateKnowledgeBase(ctx, seclai.CreateKnowledgeBaseBody{})
fetched, _ := client.GetKnowledgeBase(ctx, "kb_id")
_, _ = client.UpdateKnowledgeBase(ctx, "kb_id", seclai.UpdateKnowledgeBaseBody{})
_ = client.DeleteKnowledgeBase(ctx, "kb_id")
```

### Memory banks

```go
banks, _ := client.ListMemoryBanks(ctx, seclai.SortableListOptions{})
bank, _ := client.CreateMemoryBank(ctx, seclai.CreateMemoryBankBody{})
fetched, _ := client.GetMemoryBank(ctx, "mb_id")
_, _ = client.UpdateMemoryBank(ctx, "mb_id", seclai.UpdateMemoryBankBody{})
_ = client.DeleteMemoryBank(ctx, "mb_id")

// Stats & compaction
stats, _ := client.GetMemoryBankStats(ctx, "mb_id")
_ = client.CompactMemoryBank(ctx, "mb_id")

// Test compaction
test, _ := client.TestMemoryBankCompaction(ctx, "mb_id", seclai.TestCompactionRequest{})
standalone, _ := client.TestCompactionPromptStandalone(ctx, seclai.StandaloneTestCompactionRequest{})

// Templates & agents
templates, _ := client.ListMemoryBankTemplates(ctx)
agents, _ := client.GetAgentsUsingMemoryBank(ctx, "mb_id")

// AI assistant
suggestion, _ := client.GenerateMemoryBankConfig(ctx, seclai.MemoryBankAiAssistantRequest{})
lastConv, _ := client.GetMemoryBankAiLastConversation(ctx)
_, _ = client.AcceptMemoryBankAiSuggestion(ctx, "conversation_id", seclai.MemoryBankAcceptRequest{})

// Source management
_ = client.DeleteMemoryBankSource(ctx, "mb_id")
```

### Sources

```go
sources, _ := client.ListSources(ctx, seclai.ListSourcesOptions{Page: 1, Limit: 20, Order: "asc"})
source, _ := client.CreateSource(ctx, seclai.CreateSourceBody{})
fetched, _ := client.GetSource(ctx, "source_id")
_, _ = client.UpdateSource(ctx, "source_id", seclai.UpdateSourceBody{})
_ = client.DeleteSource(ctx, "source_id")
```

### File uploads

Upload a file to a source (max 200 MiB):

```go
upload, _ := client.UploadFileToSource(ctx, "source_id", seclai.UploadFileRequest{
	File:     fileBytes,
	FileName: "document.pdf",
	MimeType: "application/pdf",
	Title:    "Q4 Report",
	Metadata: map[string]any{"department": "finance"},
})
```

Upload inline text:

```go
upload, _ := client.UploadInlineTextToSource(ctx, "source_id", seclai.InlineTextUploadRequest{
	Text:  "Hello, world!",
	Title: "Greeting",
})
```

Replace a content version with a new file:

```go
upload, _ := client.UploadFileToContent(ctx, "content_version_id", seclai.UploadFileRequest{
	File:     fileBytes,
	FileName: "updated.pdf",
	MimeType: "application/pdf",
})
```

### Source exports

```go
exports, _ := client.ListSourceExports(ctx, "source_id", seclai.ListOptions{})
exp, _ := client.CreateSourceExport(ctx, "source_id", seclai.CreateExportRequest{})
status, _ := client.GetSourceExport(ctx, "source_id", "export_id")
estimate, _ := client.EstimateSourceExport(ctx, "source_id", seclai.EstimateExportRequest{})
resp, _ := client.DownloadSourceExport(ctx, "source_id", "export_id") // raw *http.Response
_ = client.DeleteSourceExport(ctx, "source_id", "export_id")
_, _ = client.CancelSourceExport(ctx, "source_id", "export_id")
```

### Source embedding migrations

```go
migration, _ := client.GetSourceEmbeddingMigration(ctx, "source_id")
_, _ = client.StartSourceEmbeddingMigration(ctx, "source_id", seclai.StartSourceEmbeddingMigrationRequest{})
_, _ = client.CancelSourceEmbeddingMigration(ctx, "source_id")
```

### Content

```go
detail, _ := client.GetContentDetail(ctx, "content_id", 0, 1000)
embeddings, _ := client.ListContentEmbeddings(ctx, "content_id", seclai.ListOptions{})
_ = client.DeleteContent(ctx, "content_id")

// Replace content with inline text
_, _ = client.ReplaceContentWithInlineText(ctx, "content_id", seclai.InlineTextReplaceRequest{
	Text: "Updated text", Title: "Updated",
})
```

### Solutions

```go
solutions, _ := client.ListSolutions(ctx, seclai.SortableListOptions{})
sol, _ := client.CreateSolution(ctx, seclai.CreateSolutionRequest{})
fetched, _ := client.GetSolution(ctx, "solution_id")
_, _ = client.UpdateSolution(ctx, "solution_id", seclai.UpdateSolutionRequest{})
_ = client.DeleteSolution(ctx, "solution_id")

// Link / unlink resources
_, _ = client.LinkAgentsToSolution(ctx, "solution_id", seclai.LinkResourcesRequest{})
_, _ = client.UnlinkAgentsFromSolution(ctx, "solution_id", seclai.UnlinkResourcesRequest{})
_, _ = client.LinkKnowledgeBasesToSolution(ctx, "solution_id", seclai.LinkResourcesRequest{})
_, _ = client.UnlinkKnowledgeBasesFromSolution(ctx, "solution_id", seclai.UnlinkResourcesRequest{})
_, _ = client.LinkSourceConnectionsToSolution(ctx, "solution_id", seclai.LinkResourcesRequest{})
_, _ = client.UnlinkSourceConnectionsFromSolution(ctx, "solution_id", seclai.UnlinkResourcesRequest{})

// AI assistant
plan, _ := client.GenerateSolutionAiPlan(ctx, "solution_id", seclai.AiAssistantGenerateRequest{})
_, _ = client.AcceptSolutionAiPlan(ctx, "solution_id", "conversation_id", seclai.AiAssistantAcceptRequest{})
_ = client.DeclineSolutionAiPlan(ctx, "solution_id", "conversation_id")

// AI-generated resources within the solution
_, _ = client.GenerateSolutionAiKnowledgeBase(ctx, "solution_id", seclai.AiAssistantGenerateRequest{})
_, _ = client.GenerateSolutionAiSource(ctx, "solution_id", seclai.AiAssistantGenerateRequest{})

// Conversations
convs, _ := client.ListSolutionConversations(ctx, "solution_id")
_, _ = client.AddSolutionConversationTurn(ctx, "solution_id", seclai.AddConversationTurnRequest{})
_ = client.MarkSolutionConversationTurn(ctx, "solution_id", "conversation_id", seclai.MarkConversationTurnRequest{})
```

### Governance AI

```go
plan, _ := client.GenerateGovernanceAiPlan(ctx, seclai.GovernanceAiAssistantRequest{})
convs, _ := client.ListGovernanceAiConversations(ctx)
_, _ = client.AcceptGovernanceAiPlan(ctx, "conversation_id")
_ = client.DeclineGovernanceAiPlan(ctx, "conversation_id")
```

### Alerts

```go
alerts, _ := client.ListAlerts(ctx, seclai.ListAlertsOptions{Status: "active"})
alert, _ := client.GetAlert(ctx, "alert_id")
_, _ = client.ChangeAlertStatus(ctx, "alert_id", seclai.ChangeStatusRequest{})
_, _ = client.AddAlertComment(ctx, "alert_id", map[string]any{"text": "Investigating"})

// Subscriptions
_, _ = client.SubscribeToAlert(ctx, "alert_id")
_, _ = client.UnsubscribeFromAlert(ctx, "alert_id")

// Alert configs
configs, _ := client.ListAlertConfigs(ctx, seclai.ListOptions{})
_, _ = client.CreateAlertConfig(ctx, seclai.CreateAlertConfigRequest{})
_, _ = client.GetAlertConfig(ctx, "config_id")
_, _ = client.UpdateAlertConfig(ctx, "config_id", seclai.UpdateAlertConfigRequest{})
_ = client.DeleteAlertConfig(ctx, "config_id")

// Organization preferences
prefs, _ := client.ListOrganizationAlertPreferences(ctx)
_, _ = client.UpdateOrganizationAlertPreference(ctx, "org_id", "alert_type", map[string]any{})
```

### Agent email triggers

```go
cfg, _ := client.SetEmailTriggerConfig(ctx, "agent_id", "trigger_id",
    seclai.RoutersApiAgentsSetEmailTriggerConfigRequest{
        // Alias, AllowedSenders, IgnoreAutoGenerated, RequireSenderAuth, QueueOnQuota
        // Omitted fields are left unchanged.
    })
fmt.Println(cfg.EmailAddresses)
```

### Agent email governance

```go
// Recipients who opted out of this account's agent emails
optOuts, _ := client.ListAgentEmailOptOuts(ctx, seclai.AgentEmailOptOutOptions{AgentID: "agent_id", Limit: 50})
_ = client.RemoveAgentEmailOptOut(ctx, "optout_id") // opt them back in

// Blocked inbound senders (owner/admin only)
blocked, _ := client.ListBlockedEmailSenders(ctx, seclai.ListOptions{Limit: 50})
_, _ = client.BlockEmailSender(ctx, seclai.BlockEmailSenderRequest{SenderEmail: "spam.example.com", MatchType: "domain"})
_ = client.UnblockEmailSender(ctx, "blocked_id")

// Auto-block on a governance BLOCK: "disabled" | "input" | "input_and_output"
_, _ = client.SetAutoBlockMode(ctx, seclai.SetAutoBlockModeRequest{Mode: "input_and_output"})

// Inbound mail discarded before running an agent
rejections, _ := client.ListInboundEmailRejections(ctx, seclai.InboundEmailRejectionOptions{AgentID: "agent_id"})

// Account-wide overload circuit breaker
status, _ := client.GetInboundEmailStatus(ctx)
_, _ = client.CancelQueuedEmailRuns(ctx) // fail all QUEUED (over-quota parked) runs
_, _ = client.ResumeInboundEmail(ctx)    // one-shot; re-arms if still overloaded
```

### Email domains

Send and receive agent email on your own domain instead of the shared
`agent.seclai.com`. Requires a user-bound credential; mutations require an
account owner/admin.

```go
listing, _ := client.ListEmailDomains(ctx)

vanity, _ := client.AddEmailDomain(ctx, seclai.AddEmailDomainRequest{Kind: "vanity", Value: "acme"})
custom, _ := client.AddEmailDomain(ctx, seclai.AddEmailDomainRequest{
    Kind: "custom", Value: "agent.mycompany.com",
})

// Publish custom.DnsRecords, then check without waiting for the background sweep
_, _ = client.VerifyEmailDomain(ctx, custom.Id.String())

_, _ = client.SetPrimaryEmailDomain(ctx, custom.Id.String())
_ = client.UseSharedEmailDomain(ctx) // revert; domains stay configured & verified

_, _ = client.SendEmailDomainTestEmail(ctx, custom.Id.String()) // always to the account owner
dmarc, _ := client.GetDmarcSummary(ctx, custom.Id.String(), seclai.DmarcOptions{Days: 30, TopSources: 10})

removed, _ := client.RemoveEmailDomain(ctx, custom.Id.String())
// removed.CleanupNote is set when the domain was Seclai-managed
```

### Models

```go
// Media-generation quality tiers (fast/balanced/thorough) and what each resolves to
tiers, _ := client.GetGenerationTiers(ctx)

alerts, _ := client.ListModelAlerts(ctx, seclai.ListOptions{})
_ = client.MarkModelAlertRead(ctx, "alert_id")
_ = client.MarkAllModelAlertsRead(ctx)
unread, _ := client.GetUnreadModelAlertCount(ctx)
recs, _ := client.GetModelRecommendations(ctx, "model_id")

// Model playground experiments
exp, _ := client.CreateExperiment(ctx, seclai.PlaygroundCreateRequest{ /* ... */ })
experiments, _ := client.ListExperiments(ctx, seclai.ListExperimentsOptions{})
detail, _ := client.GetExperiment(ctx, "experiment_id")
_, _ = client.CancelExperiment(ctx, "experiment_id")
_ = client.DeleteExperiment(ctx, "experiment_id") // soft-delete, preserves audit history
```

### Search

```go
results, _ := client.Search(ctx, seclai.SearchOptions{Query: "quarterly report"})
filtered, _ := client.Search(ctx, seclai.SearchOptions{Query: "my agent", EntityType: "agent", Limit: 5})
```

### Documentation search

Results are global (not account-scoped); each carries a `doc_slug` plus an optional
`anchor` for building a `https://seclai.com/docs/<doc_slug>[#<anchor>]` link.

```go
hits, _ := client.SearchDocs(ctx, seclai.DocsSearchOptions{Query: "email triggers"})
deep, _ := client.SearchDocs(ctx, seclai.DocsSearchOptions{
    Query: "how do I stop auto-reply loops", Mode: "semantic", Limit: 5,
})
```

### Top-level AI assistant

```go
// Generate plans for different resource types
kb, _ := client.AiAssistantKnowledgeBase(ctx, seclai.AiAssistantGenerateRequest{})
src, _ := client.AiAssistantSource(ctx, seclai.AiAssistantGenerateRequest{})
sol, _ := client.AiAssistantSolution(ctx, seclai.AiAssistantGenerateRequest{})
mb, _ := client.AiAssistantMemoryBank(ctx, seclai.MemoryBankAiAssistantRequest{})

// Accept or decline
_, _ = client.AcceptAiAssistantPlan(ctx, "conversation_id", seclai.AiAssistantAcceptRequest{})
_ = client.DeclineAiAssistantPlan(ctx, "conversation_id")

// Memory bank conversation history
history, _ := client.GetAiAssistantMemoryBankHistory(ctx)
_, _ = client.AcceptAiMemoryBankSuggestion(ctx, "conversation_id", seclai.MemoryBankAcceptRequest{})

// Feedback
_, _ = client.SubmitAiFeedback(ctx, seclai.AiAssistantFeedbackRequest{})
```

## Error handling

All SDK errors implement the `error` interface. Use type assertions for specific handling:

```go
run, err := client.RunAgent(ctx, "agent_id", seclai.AgentRunRequest{Input: "Hello"})
if err != nil {
	var statusErr *seclai.APIStatusError
	var validErr *seclai.APIValidationError
	var streamErr *seclai.StreamingError
	var configErr *seclai.ConfigurationError

	switch {
	case errors.As(err, &validErr):
		fmt.Println("Validation error:", validErr.StatusCode, validErr.ValidationError)
	case errors.As(err, &statusErr):
		fmt.Println("API error:", statusErr.StatusCode, statusErr.ResponseText)
	case errors.As(err, &streamErr):
		fmt.Println("Streaming error:", streamErr.Message, "run:", streamErr.RunID)
	case errors.As(err, &configErr):
		fmt.Println("Config error:", configErr.Message)
	default:
		fmt.Println("Unexpected error:", err)
	}
}
```

| Error type | When |
| --- | --- |
| `*ConfigurationError` | Missing API key, invalid base URL |
| `*APIStatusError` | Non-2xx HTTP response |
| `*APIValidationError` | HTTP 422 (embeds `APIStatusError`) |
| `*StreamingError` | SSE stream ended unexpectedly |

## Low-level access

Use `client.Do()` for direct API requests:

```go
var result MyType
err := client.Do(ctx, "GET", "/custom/endpoint", nil, nil, nil, &result)
```

Use `client.Generated()` for the raw OpenAPI-generated client with full request/response types:

```go
resp, err := client.Generated().GetAgentWithResponse(ctx, "agent_id")
```

## Development

### OpenAPI spec & regenerating the client

Place the OpenAPI JSON file at `openapi/seclai.openapi.json`, then regenerate:

```bash
make generate
```

### Generate docs

```bash
make docs VERSION=0.0.0
```

### Test

```bash
make test
```

