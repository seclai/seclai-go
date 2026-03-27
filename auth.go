package seclai

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── SSO profile ─────────────────────────────────────────────────────────────

// SsoProfile holds resolved SSO settings from the config file.
type SsoProfile struct {
	// SsoAccountID is the AWS Cognito account ID.
	SsoAccountID string
	// SsoRegion is the AWS region for the Cognito user pool.
	SsoRegion string
	// SsoClientID is the Cognito app client ID.
	SsoClientID string
	// SsoDomain is the Cognito domain (e.g. "auth.example.com").
	SsoDomain string
}

// SsoCacheEntry represents cached SSO tokens on disk.
type SsoCacheEntry struct {
	// AccessToken is the JWT access token.
	AccessToken string `json:"accessToken"`
	// RefreshToken is the refresh token for obtaining new access tokens.
	RefreshToken string `json:"refreshToken,omitempty"`
	// IDToken is the OIDC ID token (optional).
	IDToken string `json:"idToken,omitempty"`
	// ExpiresAt is the ISO-8601 expiry timestamp for the access token.
	ExpiresAt string `json:"expiresAt"`
	// ClientID is the Cognito app client ID.
	ClientID string `json:"clientId"`
	// Region is the AWS region.
	Region string `json:"region"`
	// CognitoDomain is the Cognito domain.
	CognitoDomain string `json:"cognitoDomain"`
}

// authMode describes the active authentication method.
type authMode int

const (
	authModeAPIKey authMode = iota
	authModeBearerStatic
	authModeBearerProvider
	authModeSSO
)

// authState holds resolved auth for the client lifetime.
type authState struct {
	mode          authMode
	apiKey        string
	apiKeyHeader  string
	accessToken   string
	tokenProvider func(ctx context.Context) (string, error)
	accountID     string
	ssoProfile    *SsoProfile
	configDir     string
	autoRefresh   bool
	httpClient    *http.Client
	refreshMu     sync.Mutex
}

const (
	defaultConfigDir    = ".seclai"
	ssoConfigFile       = "config"
	ssoCacheDir         = "sso/cache"
	expiryBuffer = 30 * time.Second
)

// DefaultSsoDomain is the production Cognito domain. Override with SECLAI_SSO_DOMAIN or config file.
const DefaultSsoDomain = "auth.seclai.com"

// DefaultSsoClientID is the production Cognito app client ID. Override with SECLAI_SSO_CLIENT_ID or config file.
const DefaultSsoClientID = "4bgf8v9qmc5puivbaqon9n5lmr"

// DefaultSsoRegion is the default AWS region. Override with SECLAI_SSO_REGION or config file.
const DefaultSsoRegion = "us-west-2"

// ── Helpers ─────────────────────────────────────────────────────────────────

// CacheFileName computes the SHA-1 hex of "domain|clientId".
func CacheFileName(domain, clientID string) string {
	h := sha1.Sum([]byte(domain + "|" + clientID))
	return fmt.Sprintf("%x", h)
}

// resolveConfigDir resolves the config directory from an explicit override,
// SECLAI_CONFIG_DIR env var, or ~/.seclai default.
func resolveConfigDir(override string) string {
	if override != "" {
		return override
	}
	if envDir := os.Getenv("SECLAI_CONFIG_DIR"); envDir != "" {
		return envDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, defaultConfigDir)
}

// ── INI parser ──────────────────────────────────────────────────────────────

// ParseIni parses an AWS-style INI config into sections.
// [default] stays as "default"; [profile X] becomes "X".
func ParseIni(r io.Reader) map[string]map[string]string {
	sections := map[string]map[string]string{}
	var current string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			raw := strings.TrimSpace(line[1 : len(line)-1])
			if strings.HasPrefix(raw, "profile ") {
				current = strings.TrimSpace(raw[len("profile "):])
			} else {
				current = raw
			}
			if _, ok := sections[current]; !ok {
				sections[current] = map[string]string{}
			}
			continue
		}

		if current != "" {
			if idx := strings.Index(line, "="); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				sections[current][key] = val
			}
		}
	}

	return sections
}

// ── Profile resolution ──────────────────────────────────────────────────────

// LoadSsoProfile reads the config file and resolves a profile.
// Always returns a valid profile — missing config values fall back to
// environment variable overrides (SECLAI_SSO_DOMAIN, SECLAI_SSO_CLIENT_ID,
// SECLAI_SSO_REGION), then built-in production defaults.
func LoadSsoProfile(configDir, profileName string) (*SsoProfile, error) {
	configPath := filepath.Join(configDir, ssoConfigFile)
	f, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file — return profile with defaults
			return &SsoProfile{
				SsoDomain:  envOrDefault("SECLAI_SSO_DOMAIN", "", DefaultSsoDomain),
				SsoClientID: envOrDefault("SECLAI_SSO_CLIENT_ID", "", DefaultSsoClientID),
				SsoRegion:  envOrDefault("SECLAI_SSO_REGION", "", DefaultSsoRegion),
			}, nil
		}
		return nil, err
	}
	defer f.Close()

	sections := ParseIni(f)

	defaultSection := sections["default"]
	if defaultSection == nil {
		defaultSection = map[string]string{}
	}

	var section map[string]string
	if profileName == "default" {
		section = defaultSection
	} else {
		section = sections[profileName]
		if section == nil {
			log.Printf("seclai: SSO profile %q not found in config; using defaults", profileName)
			section = defaultSection
		} else {
			// Inherit from default
			merged := make(map[string]string)
			for k, v := range defaultSection {
				merged[k] = v
			}
			for k, v := range section {
				merged[k] = v
			}
			section = merged
		}
	}

	accountID := section["sso_account_id"]

	// Apply env var overrides, then built-in defaults
	domain := envOrDefault("SECLAI_SSO_DOMAIN", section["sso_domain"], DefaultSsoDomain)
	clientID := envOrDefault("SECLAI_SSO_CLIENT_ID", section["sso_client_id"], DefaultSsoClientID)
	region := envOrDefault("SECLAI_SSO_REGION", section["sso_region"], DefaultSsoRegion)

	return &SsoProfile{
		SsoAccountID: accountID,
		SsoRegion:    region,
		SsoClientID:  clientID,
		SsoDomain:    domain,
	}, nil
}

// envOrDefault returns the env var value if set, else configVal if non-empty, else fallback.
func envOrDefault(envKey, configVal, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if configVal != "" {
		return configVal
	}
	return fallback
}

// ── Cache I/O ───────────────────────────────────────────────────────────────

// ssoCachePath resolves the full path to a profile's SSO cache file.
func ssoCachePath(configDir string, profile *SsoProfile) string {
	hash := CacheFileName(profile.SsoDomain, profile.SsoClientID)
	return filepath.Join(configDir, ssoCacheDir, hash+".json")
}

// ReadSsoCache reads a cached token file.
func ReadSsoCache(configDir string, profile *SsoProfile) (*SsoCacheEntry, error) {
	cachePath := ssoCachePath(configDir, profile)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entry SsoCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("corrupt SSO cache file %s: %w", cachePath, err)
	}
	return &entry, nil
}

// WriteSsoCache atomically writes a cache entry.
func WriteSsoCache(configDir string, profile *SsoProfile, entry *SsoCacheEntry) error {
	cacheDir := filepath.Join(configDir, ssoCacheDir)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}
	cachePath := ssoCachePath(configDir, profile)

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := cachePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}
	// Remove destination first for Windows compatibility (os.Rename fails if dest exists).
	os.Remove(cachePath)
	if err := os.Rename(tmpFile, cachePath); err != nil {
		os.Remove(tmpFile) // clean up orphaned temp file
		return err
	}
	return nil
}

// DeleteSsoCache removes a cached token file.
func DeleteSsoCache(configDir string, profile *SsoProfile) error {
	cachePath := ssoCachePath(configDir, profile)
	err := os.Remove(cachePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ── Token validation ────────────────────────────────────────────────────────

// IsTokenValid checks if a cached token is still valid (with 30s buffer).
func IsTokenValid(entry *SsoCacheEntry) bool {
	t, err := time.Parse(time.RFC3339, entry.ExpiresAt)
	if err != nil {
		// Try RFC3339Nano
		t, err = time.Parse(time.RFC3339Nano, entry.ExpiresAt)
		if err != nil {
			return false
		}
	}
	return time.Now().Add(expiryBuffer).Before(t)
}

// ── Token refresh ───────────────────────────────────────────────────────────

// RefreshToken exchanges a refresh token for new credentials via Cognito.
func RefreshToken(ctx context.Context, profile *SsoProfile, refreshToken string, hc *http.Client) (*SsoCacheEntry, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth2/token", profile.SsoDomain)

	body := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {profile.SsoClientID},
		"refresh_token": {refreshToken},
	}

	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	rt := result.RefreshToken
	if rt == "" {
		rt = refreshToken
	}

	expiresAt := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)

	return &SsoCacheEntry{
		AccessToken:   result.AccessToken,
		RefreshToken:  rt,
		IDToken:       result.IDToken,
		ExpiresAt:     expiresAt,
		ClientID:      profile.SsoClientID,
		Region:        profile.SsoRegion,
		CognitoDomain: profile.SsoDomain,
	}, nil
}

// ── Credential chain ────────────────────────────────────────────────────────

// resolveCredentialChain resolves the credential chain from Options
// and returns an authState for the client lifetime. First match wins:
// explicit APIKey → AccessToken → AccessTokenProvider → SECLAI_API_KEY env → SSO profile.
func resolveCredentialChain(opts Options) (*authState, error) {
	header := strings.TrimSpace(opts.APIKeyHeader)
	if header == "" {
		header = "x-api-key"
	}

	// Mutual exclusion
	hasAPIKey := strings.TrimSpace(opts.APIKey) != ""
	hasBearerToken := strings.TrimSpace(opts.AccessToken) != ""
	hasBearerProvider := opts.AccessTokenProvider != nil

	authCount := 0
	if hasAPIKey {
		authCount++
	}
	if hasBearerToken {
		authCount++
	}
	if hasBearerProvider {
		authCount++
	}
	if authCount > 1 {
		return nil, &ConfigurationError{Message: "provide only one of APIKey, AccessToken, or AccessTokenProvider"}
	}

	// 1. Explicit API key
	if hasAPIKey {
		return &authState{
			mode:         authModeAPIKey,
			apiKey:       strings.TrimSpace(opts.APIKey),
			apiKeyHeader: header,
			accountID:    opts.AccountID,
		}, nil
	}

	// 2. Static access token
	if hasBearerToken {
		return &authState{
			mode:         authModeBearerStatic,
			accessToken:  strings.TrimSpace(opts.AccessToken),
			apiKeyHeader: header,
			accountID:    opts.AccountID,
		}, nil
	}

	// 3. Provider
	if hasBearerProvider {
		return &authState{
			mode:          authModeBearerProvider,
			tokenProvider: opts.AccessTokenProvider,
			apiKeyHeader:  header,
			accountID:     opts.AccountID,
		}, nil
	}

	// 4. SECLAI_API_KEY env var
	if envKey := strings.TrimSpace(os.Getenv("SECLAI_API_KEY")); envKey != "" {
		return &authState{
			mode:         authModeAPIKey,
			apiKey:       envKey,
			apiKeyHeader: header,
			accountID:    opts.AccountID,
		}, nil
	}

	// 5. SSO profile
	configDir := resolveConfigDir(opts.ConfigDir)
	if configDir != "" {
		profileName := opts.Profile
		if profileName == "" {
			profileName = os.Getenv("SECLAI_PROFILE")
		}
		if profileName == "" {
			profileName = "default"
		}
		ssoProfile, err := LoadSsoProfile(configDir, profileName)
		if err == nil && ssoProfile != nil {
			acctID := opts.AccountID
			if acctID == "" {
				acctID = ssoProfile.SsoAccountID
			}
			autoRefresh := true
			if opts.AutoRefresh != nil {
				autoRefresh = *opts.AutoRefresh
			}
			return &authState{
				mode:         authModeSSO,
				apiKeyHeader: header,
				accountID:    acctID,
				ssoProfile:   ssoProfile,
				configDir:    configDir,
				autoRefresh:  autoRefresh,
				httpClient:   opts.HTTPClient,
			}, nil
		}
	}

	// 6. Nothing
	return nil, &ConfigurationError{Message: "missing credentials: provide Options.APIKey, Options.AccessToken, set SECLAI_API_KEY, or run `seclai auth login`"}
}

// resolveAuthHeaders returns the auth headers for a given request.
func resolveAuthHeaders(ctx context.Context, state *authState) (map[string]string, error) {
	headers := make(map[string]string)

	switch state.mode {
	case authModeAPIKey:
		headers[state.apiKeyHeader] = state.apiKey
	case authModeBearerStatic:
		headers["Authorization"] = "Bearer " + state.accessToken
	case authModeBearerProvider:
		token, err := state.tokenProvider(ctx)
		if err != nil {
			return nil, fmt.Errorf("access token provider error: %w", err)
		}
		headers["Authorization"] = "Bearer " + token
	case authModeSSO:
		token, err := resolveSsoToken(ctx, state)
		if err != nil {
			return nil, err
		}
		headers["Authorization"] = "Bearer " + token
	}

	if state.accountID != "" {
		headers["X-Account-Id"] = state.accountID
	}

	return headers, nil
}

// resolveSsoToken resolves a valid SSO token, refreshing from cache if needed.
// Uses a mutex to prevent concurrent refresh attempts.
func resolveSsoToken(ctx context.Context, state *authState) (string, error) {
	cached, err := ReadSsoCache(state.configDir, state.ssoProfile)
	if err != nil {
		return "", err
	}
	if cached == nil {
		return "", &ConfigurationError{Message: "No cached SSO token found. Run `seclai auth login` to authenticate via SSO."}
	}
	if IsTokenValid(cached) {
		return cached.AccessToken, nil
	}
	if cached.RefreshToken != "" && state.autoRefresh {
		state.refreshMu.Lock()
		defer state.refreshMu.Unlock()
		// Re-check after acquiring lock — another goroutine may have refreshed
		cached, err = ReadSsoCache(state.configDir, state.ssoProfile)
		if err != nil {
			return "", err
		}
		if cached != nil && IsTokenValid(cached) {
			return cached.AccessToken, nil
		}
		if cached != nil && cached.RefreshToken != "" {
			hc := state.httpClient
			refreshed, err := RefreshToken(ctx, state.ssoProfile, cached.RefreshToken, hc)
			if err != nil {
				return "", fmt.Errorf("token refresh failed: %w", err)
			}
			if err := WriteSsoCache(state.configDir, state.ssoProfile, refreshed); err != nil {
				return "", err
			}
			return refreshed.AccessToken, nil
		}
	}
	return "", &ConfigurationError{Message: "SSO token is missing or has expired. Run `seclai auth login` to authenticate."}
}
