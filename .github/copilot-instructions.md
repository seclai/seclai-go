# Copilot Instructions — seclai-go

## Build & Lint Pipeline

```sh
go vet ./...     # lint
go test ./...    # tests
go build ./...   # build
```

## Quality gates (must pass to report completion)

- **ALL tests must pass with ZERO failures. No exceptions.** CI/CD runs the full test suite on every PR. A test failure blocks the build.
- **`go vet ./...` must pass with ZERO warnings.** Vet catches real bugs; treat all warnings as errors.
- **`go build ./...` must succeed.** Compilation errors block the build.
- **Do not dismiss test or vet failures as pre-existing or unrelated.** The `main` branch CI/CD is green. Any failure on a feature branch was caused by changes on that branch.
- **CRITICAL — NEVER INVESTIGATE ERROR ORIGIN OR BLAME**: When a vet, build, or test error appears, **fix it immediately**. Do NOT run `git blame` or use git history to argue that an error is "pre-existing" or not your responsibility. Tools like `git diff`, `git log`, and `git show` may be used to understand and review changes, but never to avoid fixing an error. There is no scenario where knowing the origin of an error changes what you must do: **fix it**.
- **CRITICAL — NEVER PIPE TEST OR BUILD OUTPUT**: Do not append `| tail`, `| head`, `| grep`, or any pipe to `go test`, `go vet`, `go build`, or similar commands. Piping hides errors. Use `-count=1` or `-v` flags to control verbosity — never pipe.

## Key Rules

- Go exported field names use PascalCase with acronyms fully uppercased: `APIKey` (not `ApiKey`), `BaseURL` (not `BaseUrl`), `HTTPClient` (not `HttpClient`).
- `doc.go` examples must compile — field names in code samples must match the `Options` struct exactly.
- The OpenAPI spec at `openapi/seclai.openapi.json` is shared identically with `seclai-python`. Changes must be synced to both repos.
- OpenAPI specs are generated from the main `seclai` app repo. Description or endpoint changes made here must also be applied upstream, or they will be overwritten on the next generation.
- `.github/copilot-instructions.md` shares common sections (quality gates, git rules, editing rules, self-correction rules) across all SDK repos. When updating shared rules, apply the same change to all repos: `seclai-python`, `seclai-javascript`, `seclai-go`, `seclai-csharp`, `seclai-cli`, `seclai-mcp`.
- Generated client is in `generated/` — produced from the OpenAPI spec via oapi-codegen. Do not edit manually.
- `WriteSsoCache`: uses `os.Remove` before `os.Rename` for Windows compatibility (`os.Rename` fails if dest exists on Windows).
- `resolveSsoToken` distinguishes nil cache ("No cached SSO token found") vs expired ("SSO token is missing or has expired").
- `LoadSsoProfile` always returns a valid profile — missing config values fall back to `SECLAI_SSO_*` env vars, then built-in defaults (`DefaultSsoDomain`, `DefaultSsoClientID`, `DefaultSsoRegion`).
- Auth modes: `api_key`, bearer (static string / provider function), `sso` with auto-refresh. SSO is the default fallback when no explicit credentials are provided.
- Do not run ad-hoc Go snippets; add tests instead.

## Git rules

- **NEVER use `git stash`.** Use `git diff`, `git log`, or `git show` instead.
- Do not run `git checkout` to switch branches, `git reset`, or any other destructive git operation without explicit user approval.

## Editing rules

- Do not use CLI text tools (sed/awk). Use the editor-based patch tool.

## Self-correction rules

- **NEVER promise to "do better" without updating these instruction files.** If a recurring mistake is identified, edit this file with a concrete rule that prevents the mistake. Do that FIRST, then continue work.
