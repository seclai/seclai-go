# Changelog

## [1.6.0] - 2026-07-28

### Changed

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
