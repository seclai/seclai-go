# Copilot Instructions — seclai-go

## Build & Lint Pipeline

```sh
go vet ./...     # lint
go test ./...    # tests
go build ./...   # build
```

## Key Rules

- Go exported field names use PascalCase with acronyms fully uppercased: `APIKey` (not `ApiKey`), `BaseURL` (not `BaseUrl`), `HTTPClient` (not `HttpClient`).
- `doc.go` examples must compile — field names in code samples must match the `Options` struct exactly.
- The OpenAPI spec at `openapi/seclai.openapi.json` is shared identically with `seclai-python`. Changes must be synced to both repos.
- Generated client is in `generated/` — produced from the OpenAPI spec via oapi-codegen. Do not edit manually.
- `WriteSsoCache`: uses `os.Remove` before `os.Rename` for Windows compatibility (`os.Rename` fails if dest exists on Windows).
- `resolveSsoToken` distinguishes nil cache ("No cached SSO token found") vs expired ("SSO token is missing or has expired").
- `LoadSsoProfile` always returns a valid profile — missing config values fall back to `SECLAI_SSO_*` env vars, then built-in defaults (`DefaultSsoDomain`, `DefaultSsoClientID`, `DefaultSsoRegion`).
- Auth modes: `api_key`, bearer (static string / provider function), `sso` with auto-refresh. SSO is the default fallback when no explicit credentials are provided.
