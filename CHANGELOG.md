# Changelog

## [1.8.0] - 2026-10-21

### Changed

- Move `APIVersionLatest` to `2026-10-19`, which opts a client on it into five versions at once. A source item an enforcing governance policy holds at import is `completed` with `HeldForReview` set rather than `failed` (`2026-10-05`), so code that looks for held items among failed ones stops finding them. `CreateSource` and `UpdateSource` answer 400 to an unknown `media_types` kind or one on a source that is not a website or RSS feed (`2026-10-13`), and `CreateSource` answers 400 to `polling_max_items` on a website, custom index or cloud drive source (`2026-10-19`). An uploaded agent input file is ready at once and its text is no longer written into `input` (`2026-10-16`), and a source that follows no web pages, created on a text-only embedder without `document_images`, gets it turned on (`2026-10-18`)
- Sync the bundled OpenAPI spec, adding 6 paths and 10 schemas, and regenerate the `generated` package from it. The request types now tag every optional, nullable field `omitempty`, so a nil field is left out of the body rather than sent as null; see Fixed
- Leave a nil `RetentionDays` out of `CreateMemoryBank` instead of sending it as null. A null kept the new bank's entries indefinitely; an omitted field gets the API's default, which from API version `2026-08-03` is 90 days for a conversation bank. Earlier versions, which include an account with no pinned version on a client without `Options.APIVersion`, give a conversation bank 30 days. A general bank keeps its entries indefinitely on every version. Set `RetentionDays` to `RetentionIndefinite` to keep the old behaviour. Existing banks are unaffected

### Added

- Add `SetAcceptedAttachments` to declare the named input attachments an agent's trigger accepts, which steps read as `{{agent.attachments.<name>}}`; a nil declaration is sent as null and accepts any files. `CreateAgentRequest` and `UpdateAgentDefinitionRequest` gain `AcceptedAttachments` to declare them in the same call, `AgentAttachmentRefsApiResponse` reports them, and `AgentSummaryResponse` gains the `TriggerId` the method takes
- Add content-attachment methods: `ListContentAttachments`, `GetContentAttachment` for one file's indexing status and indexed text, paged by the `*int` fields of `ContentAttachmentOptions` so that an `End` of 0 can be sent, `ListContentAttachmentEmbeddings`, and `DownloadContentAttachment`, which returns the raw `*http.Response` and an `*APIStatusError` with status 403 for a withheld file. All but `ListContentAttachments` answer 402 when the account has no credits left
- Add `ListEmailSourceRejections` for the mail an email content source refused, with the sender, the reason and whether the message was authenticated
- Add `RunOnAttachmentMismatch` to `SetEmailTriggerConfig`, sent only when set, and `EmailRunOnAttachmentMismatch` to `EmailTriggerConfigResponse`
- Add the `APIVersion20261005`, `APIVersion20261013`, `APIVersion20261016`, `APIVersion20261018` and `APIVersion20261019` constants. None of the five changes a response shape this client decodes; the README lists what each changes
- Add the email-source and cloud-drive-source settings to `CreateSourceBody`, `UpdateSourceBody` and `SourceResponse` — `EmailAllowedSenders`, `DocumentImages`, `FeedAttachments` and the `CloudDrive*` fields — and `EmailAddress` to `SourceResponse`
- Add `HeldForReview` to `SourceContentStatusResponse`, `SkippedAttachments` to `ContentDetailResponse`, and `IncludeReasoning` to `AgentRunStreamRequest`
- Add 10 types for those endpoints and fields, including `AcceptedAttachmentSlot`, `ContentAttachmentResponse`, `ContentAttachmentDetailResponse`, `EmailSourceRejectionResponse` and `SkippedAttachmentResponse`
- Add `ClearSourceRetention` to remove a source's retention window, so its content is kept indefinitely. `UpdateSource` no longer clears it, see below
- Add `ClearEvaluationCriteriaFields` and the `EvaluationCriteriaField` constants to clear an evaluation criteria's description, evaluation prompt, expectation config or pinned tier, leaving its other settings unchanged. Clearing `EvaluationCriteriaEvaluationTier` returns the criteria to the agent's default evaluation tier, which `UpdateEvaluationCriteria` cannot do
- Add `RetentionIndefinite`, a `RetentionDays` value that keeps a memory bank's entries indefinitely. `CreateMemoryBank` sends it as null and `UpdateMemoryBank` as 0, which is how each endpoint spells "no window"

### Fixed

- Leave every unset optional field out of a request body instead of sending it as null, on every method that takes a request type and on the types behind `Generated()`. Where the API reads a null as "clear this", that cleared settings the caller never named. `UpdateSource` cleared the retention window on any update that did not set `RetentionDays`, such as a rename, so the source kept its content indefinitely; a caller who relied on that must now call `ClearSourceRetention`. `UpdateEvaluationCriteria` cleared the description, prompt, expectation config and tier the call did not set, and answered 400 unless it set `StepId`. To clear one of those, call `ClearEvaluationCriteriaFields`. Elsewhere the API treats a null like an omitted field, so other requests behave as before. `UpdateAPIVersion` still sends a nil version as null, which clears the pin

## [1.7.1] - 2026-10-05

### Changed

- Return an `*UnexpectedResponseError` from the list methods of version-gated endpoints when a successful response is not a list — an error-shaped object, text, a number, a string, `null`, an empty body, or a list key holding something other than an array — or is a list that cannot be decoded. Such a body used to produce an empty result, a nil slice, or a raw `json` error, depending on the method and the body; an empty result reads as "no results". The JSON error, where there is one, is still reachable through `errors.As`. An explicit `"data": null` is still an empty list, unless the per-resource key beside it holds the list ([#15](https://github.com/seclai/seclai-go/issues/15))
- Treat an empty or `null` body on a 200 or 204 from those methods as an error. The methods returning a slice answered it with a nil slice and a nil error ([#15](https://github.com/seclai/seclai-go/issues/15))
- Accept a bare array from the list methods whose default shape is an object with a per-resource key, such as `ListKnowledgeBases`, placing the items under that key ([#15](https://github.com/seclai/seclai-go/issues/15))
- Hand the generated client behind `Client.Generated()` an unexported wrapper as its HTTP doer, so that the version guard runs after every request editor. Code that type-asserts that field to `*http.Client` no longer gets one. The requests on the wire, the transport, the timeout, redirects and cancellation are unchanged ([#16](https://github.com/seclai/seclai-go/issues/16))
- Fill the per-resource field and the flat `Total` beside `Data` and `Pagination` once `Options.APIVersion` is `2026-07-27` or later: `Configs` and `Total` on `Typed().ListAlertConfigs`, `Alerts` and `Total` on `Typed().ListModelAlerts`, `Models` on `ListEmbeddingModels` and `ListRerankerModels`, and `Total`, `Page` and `Limit` on `ListRunEvaluationResults` and `ListAgentEvaluationResults`. They were empty or zero on that shape, leaving `Items()` and `Pagination` as the only way to read it ([#15](https://github.com/seclai/seclai-go/issues/15))

### Added

- Add the `UnexpectedResponseError` type, carrying the method, the URL actually requested (base URL, expanded path and query string, for the `Typed()` forms too) and the body of a successful response that could not be read as the list its method returns. `Unwrap` gives the underlying JSON error when the body was malformed or an item had the wrong type
- Add `Typed().ListMemoryBankTemplates` and `Typed().GetAgentsUsingMemoryBank`, returning the items as `[]map[string]JsonValue` on both response shapes. The API declares no schema for either, and the raw `Client` methods still hand back the body as sent, whose shape follows the API version ([#15](https://github.com/seclai/seclai-go/issues/15))

### Fixed

- Return the items from every list method once `Options.APIVersion` is `2026-07-27` or later, when the API answers list endpoints with `{data, pagination}`. `ListKnowledgeBases`, `ListMemoryBanks`, `ListAgentEmailOptOuts`, `ListBlockedEmailSenders`, `SetAutoBlockMode`, `ListOrganizationAlertPreferences`, `ListEmailDomains`, `Typed().GetGenerationTiers` and `Typed().ListExperiments` returned an empty list with a nil error; `GetAgentCallers`, `ListInboundEmailRejections`, `ListGovernanceAiConversations`, `ListModels` and `ListSolutionConversations` failed with `json: cannot unmarshal object` ([#15](https://github.com/seclai/seclai-go/issues/15))
- Fill `Total`, `Page` and `Limit` from `pagination` on `ListEvaluationResults`, `ListCompatibleRuns` and `ListEvaluationRuns` once `Options.APIVersion` is `2026-07-27` or later, and the `Total`, `Page` and `Limit` of the keyed listings above where their types have them. They were zero, so a loop on `Total` stopped after the first page ([#15](https://github.com/seclai/seclai-go/issues/15))
- Reject a `Seclai-Version` passed in the per-request `headers` of `Client.Do` when it is unknown to this release, in any letter case, with a `*ConfigurationError` and without sending the request. This is a new rejection: the header was sent unchecked, bypassing the guard on `Options.APIVersion`, and a caller who relied on that must now set `Options.AllowUnknownAPIVersion`. Of two spellings of the header in one call, the same one is now sent every time and it is the one validated ([#16](https://github.com/seclai/seclai-go/issues/16))
- Reject an empty `Seclai-Version` in `Options.DefaultHeaders` at construction, and in the per-request `headers` of `Client.Do`, with or without `Options.AllowUnknownAPIVersion`. It passed the guard and replaced the configured `APIVersion` with an empty header, silently dropping the opt-in ([#16](https://github.com/seclai/seclai-go/issues/16))
- Apply the version guard to requests issued through `Client.Generated()`. A request editor could set an unknown or empty `Seclai-Version` that was sent as-is; such a request now fails with a `*ConfigurationError` before it is sent, which is likewise a new rejection. `Generated()` is otherwise documented as a raw escape hatch whose request editors are the caller's responsibility ([#16](https://github.com/seclai/seclai-go/issues/16))
- Correct the README's API-versioning section, which listed 17 methods as unsafe after opting in and described only the shape change: opting in also turns paging on for `ListEvaluationCriteria`, `ListRunEvaluationResults` and `ListAlertConfigs`, which return every item by default, and makes `SetAutoBlockMode` report the number of rows returned as its `Total`

## [1.7.0] - 2026-10-04

### Changed

- Sync the bundled OpenAPI spec, adding 10 paths and 18 schemas, and regenerate the `generated` package from it. The spec now declares a 503 response on every operation, so each generated `*Response` wrapper gains a `JSON503` field
- Send `"cooldown_minutes": null` from `CreateAlertConfig` when `CreateAlertConfigRequest.CooldownMinutes` is nil, where it was previously omitted. The API now resolves an unset cooldown per alert type — 1440 minutes for credit alerts, 60 otherwise — rather than to a flat 60

### Added

- Add cloud-drive methods: `ListCloudDriveProviders`, `ListCloudDrives`, `GetCloudDrive`, `UpdateCloudDrive`, `DisconnectCloudDrive`, `DeleteCloudDrive`, `GetAgentsUsingCloudDrive`, and `ListCloudDriveRejections` for the files a connection skipped and why. The four listings read both the bare array and the `{data, pagination}` envelope the API returns once `Options.APIVersion` is `2026-07-27` or later, and `UpdateCloudDrive` sends only the fields that are set
- Add `ListSourceContents` and `GetSourceContentStatus` for the indexing status of a source's content items, with `ListSourceContentsOptions.ContentVersionIDs` to poll a batch of uploads in one request
- Add `ListEmbeddingModels` and `ListRerankerModels` for the embedder and reranker catalogs with their defaults and pricing. `Items()` on each response returns the models from whichever key the API version used
- Add the `APIVersion20260803`, `APIVersion20260821`, `APIVersion20260928`, `APIVersion20260930` and `APIVersion20261003` constants, and move `APIVersionLatest` to `2026-10-03`. None of the five changes a response shape this client decodes
- Add 17 types for those endpoints, including `CloudDriveResponse`, `CloudDriveRejectionResponse`, `SourceContentStatusResponse`, `EmbeddingModelListResponse`, `RerankerModelListResponse`, and the `AgentRunFileResponse` and `EffortOptionsResponse` that existing responses now reference
- Add `Attachments` to `AgentRunResponse` and `AgentRunStepResponse`, along with `TracePurgedAt` on a run and `Warnings` on a step
- Add `EffortOptions`, `ChatCapable`, `GenerationCreditsPerVariant` and `Input30mCacheWriteCreditsPer1000Tokens` to `PromptModelResponse`, `EffortOptions` to `ModelRecommendationResponse`, and `Effort` to `PlaygroundCreateRequest` and `ExperimentDetailResponse`
- Add `StripQuotedReplyChains` to `CreateMemoryBankBody`, `UpdateMemoryBankBody` and `MemoryBankResponse`
- Add extracted-media fields to `ContentDetailResponse`, media provenance (`MediaName`, `PageNumber`, `SourceMime`, `SourceUrl`) to `ContentEmbeddingResponse`, and `EmbedderWarning` to `ContentFileUploadResponse`
- Add `GovernanceConversationId` to `AiAssistantFeedbackRequest`

## [1.6.0] - 2026-07-28

### Changed

- Run the test suite under `-race`, in `make test` and both CI workflows. The tests capture request state in an httptest handler goroutine and assert on it from the test goroutine; that is safe by construction, but nothing was enforcing it
- Stop sending `Severity` from `ListAlerts`. `GET /alerts` declares no such filter, so it never filtered, and it becomes a 422 once `Options.APIVersion` is `2026-07-27` or later. The field is still accepted and ignored
- Deprecate `GetAgentAiConversationHistory`. The API requires a `step_type` query parameter its signature cannot supply, so every call answered 422. Use `GetAgentAiConversationHistoryWithOptions`
- Accept either wire shape from `ListEvaluationCriteria`. The endpoint returns a bare array by default and the canonical `{data, pagination}` envelope once opted in, so the client reads both and keeps returning `[]EvaluationCriteriaResponse`
- Return an error from `Search` and `SearchDocs` when `Query` is empty, rather than deferring to a 422 that names the wire parameter `q` instead of the field
- Sync the bundled OpenAPI spec: dated API versioning, `agent_id` on the non-manual evaluation summary, and `page`/`limit` on the evaluation and alert-config listings

### Added

- Add `APIVersion20260701` / `APIVersion20260727` constants, plus `APIVersionDefault`, `APIVersionLatest` and `KnownAPIVersions`. An `Options.APIVersion` this release was not built against makes `NewClient` fail, since a newer version can reshape responses this client would mis-decode; set `Options.AllowUnknownAPIVersion` to override
- Add `Client.Typed()`, an opt-in surface carrying typed forms of the 23 methods that return `json.RawMessage` — alerts, alert configs, model alerts and recommendations, playground experiments, search, docs search, generation tiers and the AI assistant acknowledgements. Each delegates to its raw counterpart and unmarshals, so both issue the same request
- Add 20 response type aliases covering those endpoints, including `AlertResponse`, `AlertDetailResponse`, `AlertConfigResponse`, `ModelAlertResponse`, `ExperimentDetailResponse` and `SearchResponse`
- Add an `Options.APIVersion` field, sent as the `Seclai-Version` header, opting into dated API changes released on or before that date. Omitted by default, so upgrading the SDK alone never changes response shapes
- Add `GetAPIVersion` and `UpdateAPIVersion` to read the version a request resolves to and to pin or clear the account's version
- Add `ListEvaluationCriteriaPage` for the canonical `{data, pagination}` envelope, which the endpoint emits once `Options.APIVersion` is `2026-07-27` or later
- Add `GetAgentAiConversationHistoryWithOptions` and `AiConversationHistoryOptions`, carrying the required `step_type` plus `step_id`, `limit` and `offset`
- Add the `ApiVersionResponse` and `UpdateApiVersionRequest` type aliases

### Fixed

- Validate the `Seclai-Version` that survives the header merge, and order the merge deterministically. Go randomises map iteration, so two spellings in `Options.DefaultHeaders` meant the guard approved one value and the client sent another, differently on each run
- Read the canonical key by presence rather than length in `AlertConfigListResponse.Items()` and `ModelAlertListResponse.Items()`. An empty canonical page (`{"data": []}`) fell through to the legacy key and reported the wrong list
- Validate a `Seclai-Version` supplied through `Options.DefaultHeaders`, not just `Options.APIVersion`. `DefaultHeaders` is applied last so it wins, which left the unknown-version guard one header away from being bypassed; a differently-cased key also emitted two wire headers
- Return an error from `GetAgentAiConversationHistoryWithOptions` when `StepType` is empty, rather than omitting the parameter and deferring to a 422 naming the wire parameter
- Decode either wire shape in `ListRunEvaluationResults`. The endpoint answers with a bare array, which the declared envelope type could not read, so the method returned nothing; it now also reads the canonical `{data, pagination}` envelope. `ListAgentEvaluationResults` is genuinely flat and is unaffected
- Paginate `ListModelAlerts` with the `offset` the endpoint declares instead of `page`, which it does not accept — every page after the first returned page 1
- Request `GET /sources` rather than `GET /sources/`. The trailing-slash form is no longer declared by the API

## [1.5.0] - 2026-07-26

### Changed

- Sync the bundled OpenAPI spec, adding 22 paths and 22 schemas

### Added

- Add `GetMe` returning the authenticated user's account ID and organization memberships
- Add `DisableAgent`, `EnableAgent`, and `GetAgentCallers` to pause and resume an agent across every trigger path
- Add `SetEmailTriggerConfig` to set the alias, sender allowlist, and inbound-handling flags on an `EMAIL_RECEIVED` trigger
- Add agent-email opt-out methods `ListAgentEmailOptOuts` and `RemoveAgentEmailOptOut`
- Add inbound sender blocklist methods `ListBlockedEmailSenders`, `BlockEmailSender`, `UnblockEmailSender`, and `SetAutoBlockMode`
- Add inbound-email observability methods `ListInboundEmailRejections`, `GetInboundEmailStatus`, `CancelQueuedEmailRuns`, and `ResumeInboundEmail`
- Add email domain management: `ListEmailDomains`, `AddEmailDomain`, `RemoveEmailDomain`, `VerifyEmailDomain`, `SetPrimaryEmailDomain`, `UseSharedEmailDomain`, `SendEmailDomainTestEmail`, and `GetDmarcSummary`
- Add `GetGenerationTiers` mapping each media-generation modality and tier to its model and cost
- Add `SearchDocs` for keyword or semantic search over the Seclai documentation

### Fixed

- Send the `q` query parameter from `Search` instead of `query`. The API requires `q`, so every `Search` call had been failing validation since 1.1.0

## [1.4.0] - 2026-06-05

### Added

- Add `GetAgentAttachmentReferences` to read an agent's static attachment-reference contract before staging uploads ([#9](https://github.com/seclai/seclai-go/pull/9))
- Add `DownloadAgentRunAttachment` for a file emitted by a run step ([#9](https://github.com/seclai/seclai-go/pull/9))
- Add `DeleteExperiment` to soft-delete a model playground experiment ([#9](https://github.com/seclai/seclai-go/pull/9))

## [1.3.0] - 2026-05-22

_Re-tag of 1.2.0 to correct the release version; the tree is byte-identical and there are no functional changes._

## [1.2.0] - 2026-05-22

### Added

- Add `PreviewImportAgent` to dry-run an agent definition import and surface unresolved entity refs ([#8](https://github.com/seclai/seclai-go/pull/8))

## [1.1.4] - 2026-04-24

### Added

- Add `ListModels` and `GetModel` for the model catalog ([#7](https://github.com/seclai/seclai-go/pull/7))
- Add model playground methods `ListExperiments`, `CreateExperiment`, `GetExperiment`, and `CancelExperiment` ([#7](https://github.com/seclai/seclai-go/pull/7))

## [1.1.3] - 2026-04-02

### Added

- Add `ExportAgent` returning a portable JSON snapshot of an agent definition ([#6](https://github.com/seclai/seclai-go/pull/6))

## [1.1.2] - 2026-03-27

### Changed

- Default the SSO domain, client ID, and region so a profile only needs `sso_account_id` ([#5](https://github.com/seclai/seclai-go/pull/5))

### Added

- Add `GET /me` to the bundled OpenAPI spec and a `MeResponse` type alias; the corresponding `GetMe` client method arrived in 1.5.0 ([#5](https://github.com/seclai/seclai-go/pull/5))

## [1.1.1] - 2026-03-26

### Added

- Add OAuth SSO authentication with `~/.seclai/config` profiles, an on-disk token cache, and automatic refresh ([#4](https://github.com/seclai/seclai-go/pull/4))
- Add an `AccountID` option, sent as the `X-Account-Id` header, to switch organization account context ([#4](https://github.com/seclai/seclai-go/pull/4))

## [1.1.0] - 2026-03-23

### Added

- Expand endpoint coverage to knowledge bases, memory banks, sources, source exports, embedding migrations, content, solutions, alerts, governance, evaluations, and the AI assistants ([#3](https://github.com/seclai/seclai-go/pull/3))
- Add `RunStreamingAgent`, a channel-based stream of every SSE event in a run ([#3](https://github.com/seclai/seclai-go/pull/3))
- Add `RunAgentAndPoll` for environments where SSE is impractical ([#3](https://github.com/seclai/seclai-go/pull/3))
- Add `Search` across all resource types in an account ([#3](https://github.com/seclai/seclai-go/pull/3))

## [1.0.1] - 2026-01-30

### Changed

- Accept a run ID alone via `GetAgentRunByID` and `DeleteAgentRunByID`; the agent ID is no longer required
- Document the upload size limit and supported MIME types on the upload methods

### Added

- Add `RunStreamingAgentAndWait` to block until a streaming run completes
- Add `UploadFileToContent` to replace existing content with a file upload
- Add `GetAgentRunWithOptions` and `GetAgentRunByIDWithOptions` for including step outputs
- Add a metadata argument to the upload methods

### Fixed

- Drop the `/api` prefix from request paths so they match the deployed API
- Correct the file upload endpoint

## [1.0.0] - 2026-01-12

_Stable release. No functional changes since 0.0.2._

## [0.0.2] - 2026-01-12

### Fixed

- Correct the git tag format so Go module resolution works (`v`-prefixed tags)

## [0.0.1] - 2026-01-12

_Initial release._

[1.8.0]: https://github.com/seclai/seclai-go/releases/tag/v1.8.0
[1.7.1]: https://github.com/seclai/seclai-go/releases/tag/v1.7.1
[1.7.0]: https://github.com/seclai/seclai-go/releases/tag/v1.7.0
[1.6.0]: https://github.com/seclai/seclai-go/releases/tag/v1.6.0
[1.5.0]: https://github.com/seclai/seclai-go/releases/tag/v1.5.0
[1.4.0]: https://github.com/seclai/seclai-go/releases/tag/v1.4.0
[1.3.0]: https://github.com/seclai/seclai-go/releases/tag/v1.3.0
[1.2.0]: https://github.com/seclai/seclai-go/releases/tag/v1.2.0
[1.1.4]: https://github.com/seclai/seclai-go/releases/tag/v1.1.4
[1.1.3]: https://github.com/seclai/seclai-go/releases/tag/v1.1.3
[1.1.2]: https://github.com/seclai/seclai-go/releases/tag/v1.1.2
[1.1.1]: https://github.com/seclai/seclai-go/releases/tag/v1.1.1
[1.1.0]: https://github.com/seclai/seclai-go/releases/tag/v1.1.0
[1.0.1]: https://github.com/seclai/seclai-go/releases/tag/v1.0.1
[1.0.0]: https://github.com/seclai/seclai-go/releases/tag/v1.0.0
[0.0.2]: https://github.com/seclai/seclai-go/releases/tag/v0.0.2
[0.0.1]: https://github.com/seclai/seclai-go/releases/tag/0.0.1
