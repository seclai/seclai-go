// Package seclai is the official Seclai Go SDK.
//
// The SDK provides typed convenience methods for the full Seclai API, covering:
//
//   - Agents: CRUD, definitions, runs, streaming, polling, input uploads, AI assistant
//   - Agent Evaluations: criteria, results, summaries, compatible runs, draft tests
//   - Knowledge Bases: CRUD
//   - Memory Banks: CRUD, compaction, templates, AI assistant
//   - Sources: CRUD, file uploads, inline text, embedding migrations
//   - Source Exports: create, list, download, estimate, cancel
//   - Content: detail, embeddings, inline text replace, file uploads
//   - Solutions: CRUD, resource linking, conversations, AI assistant
//   - Governance: AI assistant for policy management
//   - Alerts: CRUD, configs, organization preferences
//   - Models: alerts, recommendations
//   - Search: global search across resources
//   - Top-Level AI Assistant: feedback, generation, acceptance/decline
//
// # Authentication
//
// Credentials are resolved via a chain (first match wins):
//
//  1. Explicit APIKey option
//  2. Explicit AccessToken option (static string)
//  3. Explicit AccessTokenProvider option (function called per request)
//  4. SECLAI_API_KEY environment variable
//  5. SSO profile from ~/.seclai/config + cached tokens in ~/.seclai/sso/cache/
//
// API key authentication:
//
//	client, _ := seclai.NewClient(seclai.Options{
//	    APIKey: "sk-...",
//	})
//
// Bearer token with a static access token:
//
//	client, _ := seclai.NewClient(seclai.Options{
//	    AccessToken: "eyJhbGciOi...",
//	})
//
// Bearer token with a provider function (called per request):
//
//	client, _ := seclai.NewClient(seclai.Options{
//	    AccessTokenProvider: func(ctx context.Context) (string, error) {
//	        return getTokenFromVault(ctx)
//	    },
//	})
//
// SSO profile (reads ~/.seclai/config, auto-refreshes cached tokens):
//
//	client, _ := seclai.NewClient(seclai.Options{
//	    Profile: "my-profile",
//	})
//
// Environment variables:
//
//   - SECLAI_API_KEY: API key for x-api-key header authentication
//   - SECLAI_API_URL: base URL (defaults to https://seclai.com)
//   - SECLAI_PROFILE: SSO profile name (defaults to "default")
//   - SECLAI_CONFIG_DIR: config directory (defaults to ~/.seclai)
//
// # Usage
//
//	client, err := seclai.NewClient(seclai.Options{})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// List agents
//	agents, err := client.ListAgents(ctx, seclai.ListOptions{Page: 1, Limit: 20})
//
//	// Run an agent and poll for completion
//	result, err := client.RunAgentAndPoll(ctx, agentID, seclai.AgentRunRequest{}, nil)
//
//	// Stream agent events via channel
//	events, errCh := client.RunStreamingAgent(ctx, agentID, seclai.AgentRunStreamRequest{})
//	for evt := range events {
//	    fmt.Println(evt.Event, evt.Run)
//	}
//	if err := <-errCh; err != nil { ... }
//
// # Error Handling
//
// The SDK returns typed errors for programmatic handling:
//
//   - [ConfigurationError]: invalid or missing client configuration
//   - [APIStatusError]: non-2xx HTTP responses
//   - [APIValidationError]: HTTP 422 validation errors (embeds APIStatusError)
//   - [StreamingError]: SSE stream failures (includes RunID when available)
//
// # Low-Level Access
//
// Use [Client.Do] for direct API requests or [Client.Generated] for the raw
// OpenAPI-generated client with full request/response types.
package seclai
