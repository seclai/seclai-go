package seclai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
