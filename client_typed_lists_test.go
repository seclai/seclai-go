package seclai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// rawAndTypedList is an endpoint with both a json.RawMessage method and a
// Typed() list form, called with every option set.
type rawAndTypedList struct {
	name      string
	specPath  string
	queryKeys []string // what the options below send
	raw       func(ctx context.Context, c *Client) (json.RawMessage, error)
	typed     func(ctx context.Context, c *Client) error
}

var rawAndTypedLists = []rawAndTypedList{
	{
		name: "ListAlertConfigs", specPath: "/alerts/configs", queryKeys: []string{"limit", "page"},
		raw: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListAlertConfigs(ctx, ListOptions{Page: 3, Limit: 10})
		},
		typed: func(ctx context.Context, c *Client) error {
			_, err := c.Typed().ListAlertConfigs(ctx, ListOptions{Page: 3, Limit: 10})
			return err
		},
	},
	{
		name: "ListModelAlerts", specPath: "/models/alerts", queryKeys: []string{"limit", "offset"},
		raw: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListModelAlerts(ctx, ListOptions{Page: 3, Limit: 10})
		},
		typed: func(ctx context.Context, c *Client) error {
			_, err := c.Typed().ListModelAlerts(ctx, ListOptions{Page: 3, Limit: 10})
			return err
		},
	},
	{
		name: "GetGenerationTiers", specPath: "/models/generation-tiers", queryKeys: []string{},
		raw: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.GetGenerationTiers(ctx)
		},
		typed: func(ctx context.Context, c *Client) error {
			_, err := c.Typed().GetGenerationTiers(ctx)
			return err
		},
	},
	{
		name: "ListExperiments", specPath: "/models/playground/experiments",
		queryKeys: []string{"days", "end_date", "limit", "offset", "start_date"},
		raw: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListExperiments(ctx, ListExperimentsOptions{Days: 7, StartDate: "2026-09-01", EndDate: "2026-10-01", Limit: 5, Offset: 10})
		},
		typed: func(ctx context.Context, c *Client) error {
			_, err := c.Typed().ListExperiments(ctx, ListExperimentsOptions{Days: 7, StartDate: "2026-09-01", EndDate: "2026-10-01", Limit: 5, Offset: 10})
			return err
		},
	},
	{
		name: "ListMemoryBankTemplates", specPath: "/memory_banks/templates", queryKeys: []string{},
		raw: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.ListMemoryBankTemplates(ctx)
		},
		typed: func(ctx context.Context, c *Client) error {
			_, err := c.Typed().ListMemoryBankTemplates(ctx)
			return err
		},
	},
	{
		name: "GetAgentsUsingMemoryBank", specPath: "/memory_banks/{memory_bank_id}/agents", queryKeys: []string{},
		raw: func(ctx context.Context, c *Client) (json.RawMessage, error) {
			return c.GetAgentsUsingMemoryBank(ctx, "mb_123")
		},
		typed: func(ctx context.Context, c *Client) error {
			_, err := c.Typed().GetAgentsUsingMemoryBank(ctx, "mb_123")
			return err
		},
	},
}

// countingServer answers every request with body and counts them.
func countingServer(t *testing.T, body string) (string, *recordedRequest, *int) {
	t.Helper()
	seen, count := &recordedRequest{}, new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*count++
		*seen = recordedRequest{Method: r.Method, URL: "http://" + r.Host + r.URL.RequestURI()}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, seen, count
}

func jsonSyntaxError(t *testing.T) error {
	t.Helper()
	var v any
	err := json.Unmarshal([]byte(`{"truncated":`), &v)
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("expected a *json.SyntaxError, got %v", err)
	}
	return err
}

// assertPassedThrough checks that typedErr is the failure rawErr reports, not
// a statement about a response.
func assertPassedThrough(t *testing.T, rawErr, typedErr error) {
	t.Helper()
	if rawErr == nil || typedErr == nil {
		t.Fatalf("expected both to fail: raw %v, typed %v", rawErr, typedErr)
	}
	var shapeErr *UnexpectedResponseError
	if errors.As(typedErr, &shapeErr) {
		t.Fatalf("an error raised before any response was converted to %#v", shapeErr)
	}
	if reflect.TypeOf(typedErr) != reflect.TypeOf(rawErr) || typedErr.Error() != rawErr.Error() {
		t.Fatalf("typed returned %T %q, raw returned %T %q", typedErr, typedErr, rawErr, rawErr)
	}
}

func TestTypedLists_AnAuthErrorWrappingAJSONSyntaxErrorIsNotAResponse(t *testing.T) {
	providerErr := fmt.Errorf("vault returned garbage: %w", jsonSyntaxError(t))
	base, _, requests := countingServer(t, `[]`)
	c, err := NewClient(Options{BaseURL: base, AccessTokenProvider: func(context.Context) (string, error) {
		return "", providerErr
	}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for _, list := range rawAndTypedLists {
		t.Run(list.name, func(t *testing.T) {
			_, rawErr := list.raw(context.Background(), c)
			typedErr := list.typed(context.Background(), c)
			assertPassedThrough(t, rawErr, typedErr)
			if !errors.Is(typedErr, providerErr) {
				t.Fatalf("typed returned %v, which is not the provider's error", typedErr)
			}
			if *requests != 0 {
				t.Fatalf("%d request(s) were sent", *requests)
			}
		})
	}
}

func TestTypedLists_ACorruptSSOCacheIsNotAResponse(t *testing.T) {
	for _, name := range []string{"SECLAI_API_KEY", "SECLAI_PROFILE", "SECLAI_CONFIG_DIR"} {
		t.Setenv(name, "")
	}
	configDir := t.TempDir()
	profile, err := LoadSsoProfile(configDir, "default")
	if err != nil {
		t.Fatalf("LoadSsoProfile: %v", err)
	}
	cachePath := ssoCachePath(configDir, profile)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(`{"accessToken":`), 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	base, _, requests := countingServer(t, `[]`)
	c, err := NewClient(Options{BaseURL: base, ConfigDir: configDir})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for _, list := range rawAndTypedLists {
		t.Run(list.name, func(t *testing.T) {
			_, rawErr := list.raw(context.Background(), c)
			typedErr := list.typed(context.Background(), c)
			assertPassedThrough(t, rawErr, typedErr)
			var syntax *json.SyntaxError
			if !errors.As(typedErr, &syntax) || !strings.Contains(typedErr.Error(), "corrupt SSO cache file") {
				t.Fatalf("typed returned %v, want the corrupt-cache error", typedErr)
			}
			if *requests != 0 {
				t.Fatalf("%d request(s) were sent", *requests)
			}
		})
	}
}

// A token provider may itself call a seclai client with the context it is
// given. Nothing from that nested call may appear in the outer call's error.
func TestTypedLists_ANestedCallInTheTokenProviderLeavesNoTrace(t *testing.T) {
	const outerBody = `{"detail":"outer failure"}`
	for _, list := range rawAndTypedLists {
		t.Run(list.name+"/provider succeeds", func(t *testing.T) {
			innerBase, _, innerRequests := countingServer(t, `[{"id":"nested"}]`)
			inner, _ := NewClient(Options{APIKey: "k", BaseURL: innerBase})
			outerBase, outerSeen, outerRequests := countingServer(t, outerBody)
			outer, err := NewClient(Options{BaseURL: outerBase, AccessTokenProvider: func(ctx context.Context) (string, error) {
				if _, err := inner.Typed().ListMemoryBankTemplates(ctx); err != nil {
					return "", err
				}
				return "token", nil
			}})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			var shapeErr *UnexpectedResponseError
			if err := list.typed(context.Background(), outer); !errors.As(err, &shapeErr) {
				t.Fatalf("got %v, want an *UnexpectedResponseError", err)
			}
			if *innerRequests != 1 || *outerRequests != 1 {
				t.Fatalf("inner server saw %d request(s), outer %d; want one each", *innerRequests, *outerRequests)
			}
			if shapeErr.URL != outerSeen.URL || !strings.HasPrefix(shapeErr.URL, outerBase) || shapeErr.ResponseText != outerBody {
				t.Fatalf("error names %q with body %q; the outer request was %q with body %q",
					shapeErr.URL, shapeErr.ResponseText, outerSeen.URL, outerBody)
			}
		})

		t.Run(list.name+"/provider fails with a JSON syntax error", func(t *testing.T) {
			providerErr := fmt.Errorf("vault returned garbage: %w", jsonSyntaxError(t))
			innerBase, _, innerRequests := countingServer(t, `[{"id":"nested"}]`)
			inner, _ := NewClient(Options{APIKey: "k", BaseURL: innerBase})
			outerBase, _, outerRequests := countingServer(t, outerBody)
			outer, err := NewClient(Options{BaseURL: outerBase, AccessTokenProvider: func(ctx context.Context) (string, error) {
				if _, err := inner.Typed().ListMemoryBankTemplates(ctx); err != nil {
					return "", err
				}
				return "", providerErr
			}})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			_, rawErr := list.raw(context.Background(), outer)
			typedErr := list.typed(context.Background(), outer)
			assertPassedThrough(t, rawErr, typedErr)
			if !errors.Is(typedErr, providerErr) || strings.Contains(typedErr.Error(), innerBase) {
				t.Fatalf("typed returned %v, want the provider's error and nothing of the nested call", typedErr)
			}
			if *innerRequests != 2 || *outerRequests != 0 {
				t.Fatalf("inner server saw %d request(s), outer %d; want two nested and none outer", *innerRequests, *outerRequests)
			}
		})
	}
}

func TestTypedLists_RawAndTypedSendTheIdenticalRequest(t *testing.T) {
	for _, list := range rawAndTypedLists {
		t.Run(list.name, func(t *testing.T) {
			c, seen := stubClient(t, APIVersion20260727, `[]`)
			if _, err := list.raw(context.Background(), c); err != nil {
				t.Fatalf("raw: %v", err)
			}
			raw := *seen
			*seen = recordedRequest{}
			if err := list.typed(context.Background(), c); err != nil {
				t.Fatalf("typed: %v", err)
			}
			if raw.Method == "" || !reflect.DeepEqual(raw, *seen) {
				t.Fatalf("raw sent   %+v\ntyped sent %+v", raw, *seen)
			}
		})
	}
}

// The spec is the oracle for query keys: sdksync's params audit reads requests
// from exported Client methods only, and these six now build theirs in an
// unexported function it does not follow.
func TestTypedLists_QueryKeysAreDeclaredByTheSpec(t *testing.T) {
	specBytes, err := os.ReadFile("openapi/seclai.openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	for _, list := range rawAndTypedLists {
		t.Run(list.name, func(t *testing.T) {
			operation, ok := spec.Paths[list.specPath]["get"]
			if !ok {
				t.Fatalf("the spec declares no GET %s", list.specPath)
			}
			declared := map[string]bool{}
			for _, p := range operation.Parameters {
				if p.In == "query" {
					declared[p.Name] = true
				}
			}

			c, seen := stubClient(t, "", `[]`)
			if _, err := list.raw(context.Background(), c); err != nil {
				t.Fatalf("raw: %v", err)
			}
			sent := []string{}
			for key := range seen.Query {
				sent = append(sent, key)
				if !declared[key] {
					t.Errorf("sends query key %q, which GET %s does not declare", key, list.specPath)
				}
			}
			sort.Strings(sent)
			if !reflect.DeepEqual(sent, list.queryKeys) {
				t.Fatalf("sent query keys %v, expected the call to exercise %v", sent, list.queryKeys)
			}
		})
	}
}
