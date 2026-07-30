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
python3 $S params .                             # params the endpoint does not accept
python3 $S returns .                            # return types that disagree with the spec
python3 $S models . --since HEAD                # hand-written models left behind
python3 $S surface 1.3.0                        # is this release breaking?
python3 $S api-delta 1.3.0                      # public methods added since a tag
```

The three audits are three different directions, and each finds what the others
structurally cannot:

| | direction | catches |
| --- | --- | --- |
| `parity` | spec → client | endpoints nobody implemented |
| `params` | client → spec | query keys the endpoint ignores or requires |
| `returns` | client → spec | a response shape the client can no longer deserialize |

`parity`, `params`, `returns`, `models` and `surface` exit non-zero on a finding, so all work as CI gates.
Run the **canonical** copy from `sdk-tools`, or `./sync.sh --check` first. The
vendored copies go stale whenever a repo has uncommitted work, since `sync.sh`
skips dirty repos — a stale `spec-diff` reported 123 changed paths as
"description/params only" and hid 26 response changes.

Run them over the **whole spec**, never just the diff — `GET /me` sat unimplemented
in both the Python and JavaScript SDKs for months because each sync only looked at
its own new paths.

## `params` — the check that catches what tests cannot

`parity` walks spec→client. `params` walks client→spec, and that direction finds a
different and more dangerous class of defect, because the API silently ignores
query params it does not declare:

- **UNDECLARED** — the client sends a key the endpoint does not accept. The filter
  simply does nothing and the caller gets unfiltered results.
- **NOT IN SPEC** — the client calls a path the spec does not declare. `parity`
  cannot see this by construction.
- **UNPARSED** — a query-construction site the extractor could not read. Treated as
  an error, never as clean: a parser that gives up quietly is the failure this tool
  exists to prevent.
- **UNEXPOSED** (warning) — a declared param no method sends; a coverage gap.

Tests do not defend against this class. The SDK tests were written from the same
table as the code, so they assert the same wrong parameter names — one Go test
asserted the buggy `query` instead of the required `q`, locking the defect in. The
spec is the only independent oracle.

**Treat a finding you believe is wrong as a bug in the tool, not as noise to skip.**
Twice now that instinct was right: Go's `GetMe` was reported as sending
page/limit/sort/order it never sends (a method block ran on past its own function
and swallowed the helpers below it), and `Search` was reported as never sending its
required `q` (the extractor read `q["k"] =` but not the map literal that sets it).
Four of six Go findings were artefacts. Since both were fixed, all four SDKs report
the *same* three findings — independently-written clients agreeing is the signal
that the check is reading them correctly.

## `returns` — the axis that catches a changed response

Compares each method's **declared return type** against the spec's response
schema, and splits the result by how much it costs you:

- **SHAPE MISMATCH** (error) — the client commits to a list and the endpoint
  returns an object, or the reverse. This throws at deserialization; a shipped
  SDK is already broken. This is the 2026-07 envelope change.
- **NAME DIFFERS** (warning) — same shape, different type name. All four SDKs
  drop the spec's `Model` and `Api` affixes by convention, so this tier is mostly
  noise by design. Keep it out of CI with `--quiet-renamed`.
- **UNTYPED** (warning) — the spec names a schema and the client returns
  `JsonElement` / `json.RawMessage` / `unknown` / `JSONValue`. Not a defect, but
  it is the coverage backlog: seclai-python reports 206.

**A typed façade is invisible to this check.** seclai-csharp and seclai-go keep
their raw `JsonElement` / `json.RawMessage` methods and put the typed forms on
`client.Typed` / `client.Typed()`, so a raw-return signature is deliberate, not a
gap. Those façade methods delegate rather than issuing their own request, so
`block_call` finds no verb in them and they are never matched — the UNTYPED count
for those two SDKs will not drop as typing improves. Check the façade before
concluding an endpoint is untyped. seclai-javascript needs no façade (`unknown`
to a concrete type is source compatible) and does report zero.

**Dated versioning breaks the one-shape assumption.** Since `Seclai-Version`
(2026-07-28) an operation can have two legal response bodies — a bare array by
default, the canonical `{data, pagination}` envelope once the caller opts in —
and the spec documents only the default. A client that decodes both and declares
the envelope is therefore reported as a SHAPE MISMATCH and is *correct*. Two Go
methods sit in that state deliberately. Verify against the header before
"fixing" one; do not retype a permissive client to satisfy this check.

A wholly untyped return is never a shape error — it commits to nothing and
deserializes anything. But `list[dict[str, Any]]` **is** a commitment to a list,
and is graded as one.

## `surface` — is this release breaking?

Diffs the public API surface against the last released tag. Neither the changelog
nor the compiler can answer this: in 2026-07 a C# method gained an optional
parameter, every call site still compiled, the build was green — and it was
**binary breaking**, because adding a parameter changes the method's metadata
token and a consumer that does not recompile fails at runtime.

Run it before writing the changelog and before tagging:

```bash
python3 $S surface 1.3.0     # csharp/python: bare tag.  go: v-prefixed
```

Findings are graded by what actually breaks a caller, because a raw signature
diff called 21 JavaScript changes breaking when none were:

- **REMOVED / BREAKING** (error) — a released signature no longer works: method
  gone, parameter list changed, or a return type changed in a static language.
- **WARN** — compatible but worth checking: a *trailing* optional parameter
  (source compatible; flagged as binary breaking for C#), `unknown` narrowed to a
  concrete type in TypeScript (safe), or a Python return annotation (not enforced
  — but confirm the runtime value did not change too).

An optional parameter inserted *mid-list* is graded breaking, not a warning: in
C# a positional `M(id, token)` then tries to bind `token` to the new parameter.
Use an overload rather than growing a parameter list.

## Verifying a bulk edit

`as`/cast assertions are unchecked, so a green typecheck proves nothing about a
mass find-and-replace. A regex pass over `client.ts` in 2026-07 rewrote 129
return sites — 20 intended — and broke the parens twice.

Extract a per-method profile from the old and new sources and diff it, rather
than reading the patch: name, verb, path, query keys, request-call count. 162 of
167 methods came back byte-identical and the 5 differences were each an intended
edit, which is the assurance the diff itself cannot give. `surface` covers the
signature half of this; the request half is a few lines of `re.finditer` over
both revisions.

## `models` — hand-written models that fell behind

Only seclai-csharp: everywhere else regeneration keeps models in step. Use
`--since <rev>`, which limits the report to properties a schema **gained** since
that rev. Without it you get every long-standing coverage gap — 27 for
seclai-csharp, none of them actionable, which is how a gate gets muted.

It cannot see models whose schema is declared inline rather than as a named
component; it prints how many are unchecked for that reason. Two of the six
models left stale in 2026-07 were in that blind spot, so treat the count as real.

## `docexamples` — compile the README

```bash
D=.claude/skills/seclai-sdk-sync/docexamples.py
python3 $D list .        # every fence and whether it is checked
python3 $D check .       # compile the marked ones
```

Opt-in: mark a fence by putting `<!-- sdksync:check -->` on the line directly above
it. Invisible on GitHub, npm and pkg.go.dev. Most fences are deliberate fragments —
3 of 39 TypeScript fences carry an import — so compiling everything would mean
rewriting the READMEs first. `list` prints the marked fraction so coverage can be
raised over time. TypeScript and Go only; Python examples are plain dicts with
nothing to typecheck.

Mark any example you add or change. `npm run typecheck` and `go build` do **not**
cover README snippets, and two shipped PRs contained examples that could not compile.

## The repos are not uniform

| Repo | Client | Bundles spec | Notes |
| --- | --- | --- | --- |
| seclai-python | generated + hand-written wrappers | yes | `make generate`, then black |
| seclai-javascript | types generated, methods hand-written | yes | `npm run generate` |
| seclai-go | hand-written | yes | |
| seclai-csharp | hand-rolled from the start | **no** | no codegen library was suitable; still at near-full parity — sync it like the others |
| seclai-cli | wraps `@seclai/sdk` | no | coverage question is command-to-SDK-method |
| seclai-mcp | no client source | no | |

For repos without a bundled spec, point at one:
`--spec ../seclai-python/openapi/seclai.openapi.json`.

## Workflow

1. **Confirm the spec is identical** across the repos that bundle it. They must
   not diverge — a local edit is always wrong; fix the spec upstream in `seclai`.
2. **`spec-diff`** to see added/removed/changed paths and schema property changes.

   Under each changed path it names what a client can observe — `+query`,
   `~query … is now required`, `~request`, `~response` — and only says
   `(docs only)` when nothing but prose moved. A `~response` line is the one to
   stop on: it means a **shipped** SDK is already failing to deserialize, because
   the server changed under it. In the 2026-07 fast-follow two endpoints went from
   a bare array to a paginated envelope, breaking the method in all four SDKs.

   Do not stop at the new paths. The `SCHEMA PROPERTY CHANGES` section lists
   **existing** models that gained fields, and those are easy to miss because
   nothing about them looks new. Repos with generated models pick them up on
   regeneration; hand-written ones (seclai-csharp) do not. In the 2026-07 sync six
   C# models were left stale this way, including the `disabled` fields that agent
   pause/resume depends on — the methods shipped but the state they set was
   invisible in the response.
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
   the reliable way to keep them identical; hand-writing 2×N methods into an
   8,000-line file drifts, and no test asserts the two classes match. Working
   emitters from the last sync are in `emit-examples/` in the `sdk-tools` repo —
   copy and adapt one, they are reference material rather than a supported API.

   Generation removes transcription drift but propagates a wrong table uniformly
   to every call site. Two defects in the last sync were faithful emissions of a
   bad table. Audit the result with `params` before trusting it.
6. **Tests** — sync and async for each method, asserting verb, path, query params
   and body. Python uses `httpx.MockTransport`; JavaScript uses a `makeClient`
   fetch stub.
7. **README** — one section per new endpoint group.
8. **Version constants** — if `spec-diff` printed `API VERSIONS CHANGED`, update
   each SDK's version constants. They are pinned per release and an unknown
   version is rejected at construction, so a new API version is unusable until
   they are updated. Each SDK has a test asserting the constants match
   `x-seclai-versions`, so this fails loudly if skipped.
9. **`surface`** against the last released tag — confirm the release is additive,
   or decide deliberately to bump the major.
10. **Changelog** — use the `seclai-changelog` skill. Write entries against the
    last released tag, never against the branch's earlier state.
11. **Gate**: `make lint && make test` (python) or
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

**seclai-csharp**

- Adding a parameter — even an optional one — is **binary breaking** for a NuGet
  consumer that does not recompile, and inserting one before a trailing
  `CancellationToken` breaks positional callers at compile time too. Add an
  overload instead. `surface` catches both.

**seclai-javascript**

- `npm run typecheck` does not cover README snippets. Paste any example into a
  scratch `.ts` importing from `../src/index` and compile it before claiming it works.

## Release

The version comes from the merge commit message, read by `seclai/github-tag-action`
with `DEFAULT_BUMP: patch`. A sync that adds endpoints needs `#minor` in the PR
title, or it ships as a patch and the changelog heading will not match the tag.
