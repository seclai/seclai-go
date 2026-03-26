package seclai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClient_UsesEnvAPIKey(t *testing.T) {
	t.Setenv("SECLAI_API_KEY", "k")
	c, err := NewClient(Options{})
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if c == nil {
		t.Fatalf("expected client")
	}
}

func TestNewClient_MissingCredentials(t *testing.T) {
	t.Setenv("SECLAI_API_KEY", "")
	t.Setenv("SECLAI_CONFIG_DIR", "/nonexistent-seclai-dir")
	_, err := NewClient(Options{})
	if err == nil {
		t.Fatalf("expected error for missing credentials")
	}
	var ce *ConfigurationError
	if !isConfigError(err, &ce) {
		t.Fatalf("expected ConfigurationError, got %T: %v", err, err)
	}
}

func TestNewClient_MutualExclusion(t *testing.T) {
	_, err := NewClient(Options{APIKey: "k", AccessToken: "tok"})
	if err == nil {
		t.Fatalf("expected error for both APIKey and AccessToken")
	}
}

func isConfigError(err error, target **ConfigurationError) bool {
	ce, ok := err.(*ConfigurationError)
	if ok && target != nil {
		*target = ce
	}
	return ok
}

func TestDo_SetsAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "k" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var out map[string]any
	if err := c.Do(context.Background(), http.MethodGet, "/sources/", nil, nil, nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestDo_ValidationError422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"detail":[{"msg":"bad"}]}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = c.Do(context.Background(), http.MethodGet, "/sources/", nil, nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if _, ok := err.(*APIValidationError); !ok {
		t.Fatalf("expected APIValidationError, got %T", err)
	}
}

func TestDo_BearerStaticToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-jwt" {
			t.Fatalf("expected Bearer my-jwt, got %q", auth)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Fatalf("expected no x-api-key header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{AccessToken: "my-jwt", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var out map[string]any
	if err := c.Do(context.Background(), http.MethodGet, "/test", nil, nil, nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestDo_BearerProvider(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer tok-") {
			t.Fatalf("expected Bearer tok-*, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{
		AccessTokenProvider: func(ctx context.Context) (string, error) {
			callCount.Add(1)
			return "tok-" + time.Now().Format("150405"), nil
		},
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	for i := 0; i < 3; i++ {
		var out map[string]any
		if err := c.Do(context.Background(), http.MethodGet, "/test", nil, nil, nil, &out); err != nil {
			t.Fatalf("Do call %d: %v", i, err)
		}
	}
	if n := callCount.Load(); n != 3 {
		t.Fatalf("expected provider called 3 times, got %d", n)
	}
}

func TestDo_AccountIDHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Account-Id"); got != "acct-123" {
			t.Fatalf("expected X-Account-Id acct-123, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{AccessToken: "tok", AccountID: "acct-123", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var out map[string]any
	if err := c.Do(context.Background(), http.MethodGet, "/test", nil, nil, nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestParseIni_Sections(t *testing.T) {
	input := strings.NewReader(`
[default]
sso_region = us-east-1
sso_domain = auth.example.com

# comment
[profile dev]
sso_account_id = 123
sso_client_id = abc
`)
	sections := ParseIni(input)
	if sections["default"]["sso_region"] != "us-east-1" {
		t.Fatalf("expected sso_region us-east-1, got %q", sections["default"]["sso_region"])
	}
	if sections["dev"]["sso_account_id"] != "123" {
		t.Fatalf("expected sso_account_id 123, got %q", sections["dev"]["sso_account_id"])
	}
}

func TestIsTokenValid(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	nearExpiry := time.Now().Add(20 * time.Second).UTC().Format(time.RFC3339)

	if !IsTokenValid(&SsoCacheEntry{ExpiresAt: future}) {
		t.Error("token with future expiry should be valid")
	}
	if IsTokenValid(&SsoCacheEntry{ExpiresAt: past}) {
		t.Error("token with past expiry should be invalid")
	}
	if IsTokenValid(&SsoCacheEntry{ExpiresAt: nearExpiry}) {
		t.Error("token expiring within 30s buffer should be invalid")
	}
}
