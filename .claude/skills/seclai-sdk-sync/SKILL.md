---
name: seclai-sdk-sync
description: Sync a Seclai SDK (seclai-python, seclai-javascript, seclai-go, seclai-csharp, seclai-cli, seclai-mcp) to a new OpenAPI spec, or add endpoints to one. Use when a new openapi/seclai.openapi.json has been copied in, when asked to add missing endpoints or check endpoint coverage, or when auditing an SDK against the API spec.
---

# Syncing a Seclai SDK to a new spec

> **Vendored file — do not edit in place.** The canonical copy lives in the
> `seclai/sdk-tools` repository at `skills/seclai-sdk-sync/`, and is mirrored
> into each SDK repo with `git subtree`. Edits made here are reported as drift by
> `sdk-tools/sync.sh --check` and are overwritten on the next pull. To change it,
> change it upstream — or, if you can't reach that repo, open an issue on this
> one describing the fix and a maintainer will carry it across.

Run the analysis with `sdksync.py`, bundled next to this file, rather than
hand-rolling greps. Every ad-hoc parity regex written so far has missed methods
with multi-line signatures.

```bash
S=.claude/skills/seclai-sdk-sync/sdksync.py     # vendored into each SDK repo

python3 $S spec-diff HEAD                       # what the new spec changed
python3 $S parity .                             # spec paths with no request call
python3 $S api-delta 1.3.0                      # public methods added since a tag
```

`parity` exits non-zero when a path is unimplemented, so it works as a CI gate.
Run it over the **whole spec**, never just the diff — `GET /me` sat unimplemented
in both the Python and JavaScript SDKs for months because each sync only looked at
its own new paths.

## The repos are not uniform

| Repo | Client | Bundles spec | Notes |
| --- | --- | --- | --- |
| seclai-python | generated + hand-written wrappers | yes | `make generate`, then black |
| seclai-javascript | types generated, methods hand-written | yes | `npm run generate` |
| seclai-go | hand-written | yes | |
| seclai-csharp | hand-rolled from the start | **no** | no codegen library was suitable; covers a subset of the API by design |
| seclai-cli | wraps `@seclai/sdk` | no | coverage question is command-to-SDK-method |
| seclai-mcp | no client source | no | |

For repos without a bundled spec, point at one:
`--spec ../seclai-python/openapi/seclai.openapi.json`.

## Workflow

1. **Confirm the spec is identical** across the repos that bundle it. They must
   not diverge — a local edit is always wrong; fix the spec upstream in `seclai`.
2. **`spec-diff`** to see added/removed/changed paths and schema property changes.
3. **Regenerate**, per repo:
   - python: `make generate`, then **immediately** `poetry run black .` — the
     generator formats with ruff but the repo commits black, so raw output shows
     ~240 changed files that collapse to ~60 real ones.
   - javascript: `npm run generate` (no churn; types only).
   - go / csharp: nothing to regenerate.
4. **`parity`** to list what is missing. Implement every path, not just the ones
   that look interesting — binary/stream endpoints with no JSON schema are the
   ones that get skipped.
5. **Write the methods.** In Python they must land in **both** `Seclai` and
   `AsyncSeclai`. Generating both from a single table of method definitions is
   the reliable way to keep them identical; hand-writing 2×N methods drifts.
6. **Tests** — sync and async for each method, asserting verb, path, query params
   and body. Python uses `httpx.MockTransport`; JavaScript uses a `makeClient`
   fetch stub.
7. **README** — one section per new endpoint group.
8. **Changelog** — use the `seclai-changelog` skill.
9. **Gate**: `make lint && make test` (python) or
   `npm run typecheck && npm test && npm run build` (javascript). Re-run `parity`
   and confirm zero missing.

## Naming

A new method name sets precedent for all six SDKs — the first repo synced defines
it and the rest should follow. Check whether a sibling already named the same
endpoint before inventing one, and prefer the sibling's name transliterated to
local conventions (`searchDocs` / `search_docs` / `SearchDocs`).

## Typing conventions

- Return the shape the spec declares: a `$ref` becomes the aliased type, never an
  untyped map. See the `seclai-changelog` skill for the full table and the
  breaking-change rules.
- **JavaScript:** openapi-typescript emits any property carrying a `default` as
  **required**, even when the spec omits it from `required`. Wrap the generated
  request so server-defaulted fields stay optional:
  `Pick<Req,"a"|"b"> & Partial<Omit<Req,"a"|"b">>` — see `AddEmailDomainInput`.
- Query params are camelCase in the method signature, snake_case on the wire.

## Repo gotchas

**seclai-python**

- `poetry run black .` will reformat the **subtree-vendored** `.claude/skills/`
  files and silently drift them from canonical. `.claude` must stay excluded in
  black `extend-exclude`, ruff `exclude`, and mypy `exclude`.
- `make generate` always prints
  `Unable to parse schema … duplicate models with name "FileUploadResponse"`.
  Pre-existing and non-fatal — `routers__api__sources__` and
  `routers__api__contents__FileUploadResponse` share a title. Generation completes.
- `seclai/_generated/seclai_api_client/` is **not** regenerated and nothing
  imports it. It is stale and safe to ignore; do not treat it as a source of truth.
- mypy rejects assigning the result of a `-> None` method, so tests for
  204-returning endpoints must call without binding a variable.

**seclai-javascript**

- `npm run typecheck` does not cover README snippets. Paste any example into a
  scratch `.ts` importing from `../src/index` and compile it before claiming it works.

## Release

The version comes from the merge commit message, read by `seclai/github-tag-action`
with `DEFAULT_BUMP: patch`. A sync that adds endpoints needs `#minor` in the PR
title, or it ships as a patch and the changelog heading will not match the tag.
