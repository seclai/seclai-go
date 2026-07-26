# Changelog

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
