---
name: seclai-changelog
description: Write or update CHANGELOG.md in a Seclai SDK repo (seclai-python, seclai-javascript, seclai-go, seclai-csharp, seclai-cli, seclai-mcp) using the Common Changelog format. Use when adding a changelog entry, preparing a release, backfilling history from version tags, or finishing an OpenAPI spec sync in one of these repos.
---

# Seclai changelog entries

All six Seclai SDK repos keep a root `CHANGELOG.md` in [Common Changelog](https://common-changelog.org) format. This skill covers writing a new entry and backfilling from tags.

## Format rules

- File starts with `# Changelog`, then releases sorted latest-first.
- Release heading: `## [1.4.0] - 2026-07-25` — semver without a `v`, ISO date.
- **No `Unreleased` section.** Entries are written in the PR that ships them, under the version that PR will become (see "Determine the version").
- Change groups are third-level headings, only these four, always in this order:
  `### Changed`, `### Added`, `### Removed`, `### Fixed`.
- A group heading is followed by an unordered list and nothing else.
- Entry form: `- Change ([ref](url))`. Imperative present tense, self-describing — it must read correctly without its group heading. "Support CentOS", never "Support of CentOS" or "Added support".
- Breaking changes take a `**Breaking:**` prefix and sort first within their group. Otherwise sort by importance.
- A release may open with a one-sentence italic notice instead of, or before, its groups — used for first releases (`_Initial release._`) and no-op version bumps.
- Version links are reference-style at the bottom:
  `[1.4.0]: https://github.com/seclai/<repo>/releases/tag/1.4.0`. **Check the tag
  format per repo** — seclai-python and seclai-javascript tag without a prefix,
  seclai-go tags `v1.4.0` because Go module resolution requires it. The heading
  stays bare (`## [1.5.0]`); only the link target carries the `v`. Confirm with
  `git tag --sort=-v:refname | head -1` before writing the link block.
- Authors are omitted — these are effectively single-contributor repos.

## Determine the version

Do not guess the next version. Releases are cut by `seclai/github-tag-action` in `.github/workflows/main-build.yaml`, which reads the **merge commit message**:

- contains `#major` → major bump
- contains `#minor` → minor bump
- otherwise → `DEFAULT_BUMP: patch`

So a PR that adds endpoints must say `#minor` in its title/merge commit, or the heading you write will not match the tag that gets cut. Confirm the intended bump with the user when it isn't stated, and flag the mismatch risk if the PR title lacks the keyword.

Check `git tag --sort=-v:refname | head -1` for the current latest, then apply the bump.

## Derive entries from diffs, not from release notes

`gh release view` bodies are `--generate-notes` output — just "PR #N by @author". They are useless as entry text. Commit subjects like "2026 05 22 api sync" are equally useless. Always read the actual diff.

Extract the public API delta for a range. Per repo:

| Repo | Path | Pattern |
| --- | --- | --- |
| seclai-javascript | `src/client.ts` | `async ([a-zA-Z_][a-zA-Z0-9_]*)\(` |
| seclai-python | `seclai/seclai.py` | `^    (async )?def ([a-z][a-z0-9_]*)` |
| seclai-go | `*.go` | `^func \(c \*Client\) ([A-Z][A-Za-z0-9]*)\(` |
| seclai-csharp | `src/Seclai/SeclaiClient.cs` | `public (async )?[A-Za-z<>,? ]+ ([A-Z][A-Za-z0-9]*)\(` |
| seclai-cli | `src/commands/` | one file or subcommand per feature |
| seclai-mcp | `src/` | registered tool names |

```bash
# methods added between two tags
git diff PREV TAG -- src/ | grep "^+" | grep -oE '<pattern>' | sort -u
# and removed
git diff PREV TAG -- src/ | grep "^-" | grep -oE '<pattern>' | sort -u
```

A name in **both** lists was modified, not removed — check the signature diff before writing a `Removed` entry. Renames and reordering produce false positives constantly.

Also diff the type aliases (`src/types.ts`, `seclai/models`, etc.) and the bundled `openapi/seclai.openapi.json` — new schemas often mean new public types worth an entry even when no method changed.

## Classify

- New method, option, type export, or capability → **Added**
- Changed signature, default, accepted type, or behavior of something that already worked → **Changed**
- Deleted public surface → **Removed**
- It was broken and now works → **Fixed**

Judgment calls that have come up:

- A wrong default host or a wrong request path is **Fixed** — requests were failing — not Changed.
- Making a required parameter optional (via overload) is **Changed**, not Added.
- Exporting a type that should already have been exported is **Fixed**.
- Adding an endpoint to the bundled spec without a client method is worth its own entry; say so plainly rather than implying the method exists.
- A version bump with no code change gets a notice, not a group: `_Stable release. No functional changes since 0.0.1._`

## Write

Reference the PR when there is one (`([#9](https://github.com/seclai/<repo>/pull/9))`), otherwise the short commit SHA (`([`36bff73`](https://github.com/seclai/<repo>/commit/36bff73))`). For a PR not yet opened, omit the reference — never invent a number.

Group related additions into one entry when they ship as a unit (e.g. eight email-domain methods), and give standalone capabilities their own line. Aim for entries a user scanning for "what changed for me" can act on.

## Check the surface before you describe it

The changelog is the last gate before a release, so use the entries you are
writing as a prompt to spot-check the public surface they describe. Fix these, or
raise them, before they ship — each one is far cheaper now than after consumers
depend on it.

**Return the shape the spec declares.** Look up the endpoint's 2xx response schema
and mirror it:

| Spec response | Return type |
| --- | --- |
| `$ref` to a schema | that aliased type — never `unknown` |
| `type: object`, `additionalProperties: true` | `Record<string, unknown>` |
| `additionalProperties: {type: T}` | `Record<string, T>` |
| `type: array` | `T[]` / `unknown[]` — **not** `Record` |

A hand-written wrapper returning `unknown` where the generator had a real type
available is a bug, not a style choice.

**Widening `unknown` is breaking.** Changing a released method from `unknown` to
`Record<string, unknown>` breaks consumer code that does `result as SomeInterface`
— TypeScript rejects it with TS2352 and demands `as unknown as SomeInterface`.
That makes it a `#major`, never a patch or minor. New methods have no consumers
yet, so type them correctly from the start rather than inheriting a neighbour's
`unknown`.

**Server-defaulted request fields must be optional.** openapi-typescript emits any
property carrying a `default` as **required**, even when the spec leaves it out of
`required`. Callers then have to pass a value the server would have defaulted.
Wrap the generated request in an input type:

```ts
export type AddEmailDomainInput = Pick<AddEmailDomainRequest, "kind" | "value"> &
  Partial<Omit<AddEmailDomainRequest, "kind" | "value">>;
```

See `AddEmailDomainInput` and `CreateExperimentInput` in `src/types.ts`.

**Compile the doc examples.** `npm run typecheck` covers `src/` and `tests/`, not
README or changelog snippets. Paste any example you write into a scratch `.ts`
that imports from `../src/index` and compile it — that is how the
`delegated`-is-required bug above was found, after the snippet had already shipped
in a PR.

## Validate

Run the bundled checker from the repo root before declaring done:

```bash
python3 .claude/skills/seclai-changelog/validate.py CHANGELOG.md
```

That path holds in every SDK repo — this skill is vendored there from [`seclai/sdk-tools`](https://github.com/seclai/sdk-tools). When working inside `sdk-tools` itself, the canonical copy is `skills/seclai-changelog/validate.py`.

It verifies heading format, descending version order, group names and ordering, `Breaking:` sorting, absence of an `Unreleased` section, and that every release has exactly one matching link definition. It exits non-zero on error, so the same invocation works as a CI gate.

Also confirm `CHANGELOG.md` is in the published artifact list — `files` in `package.json` for the JS/CLI/MCP repos, the packaging config for the others.

## Backfilling from tags

1. `git tag --sort=-v:refname` and `gh release list` for versions and dates. Use the GitHub release published date (UTC) as the entry date.
2. Walk consecutive tag pairs oldest-first, extracting the API delta for each as above.
3. Map each range to its PR via `gh release view <tag> --json body`, which does at least carry the PR number reliably.
4. The earliest release gets `_Initial release._`.
