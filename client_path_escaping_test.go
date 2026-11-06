package seclai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pathServer answers every request with body and records the request-target
// exactly as it arrived on the wire (path only, no query).
func pathServer(t *testing.T, prefix, body string) (*Client, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, _, _ := strings.Cut(r.RequestURI, "?")
		seen = append(seen, target)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL + prefix})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, &seen
}

// pathIDCases are IDs that url.PathEscape changes, or that a path cleaner could
// take as a dot segment, with the request-target each must produce under
// /agents/{id}.
var pathIDCases = []struct{ name, id, want string }{
	{"space", "a 1", "/agents/a%201"},
	{"slash", "a/1", "/agents/a%2F1"},
	{"percent", "a%1", "/agents/a%251"},
	{"escaped looking", "a%2F1", "/agents/a%252F1"},
	{"unicode", "agent-é", "/agents/agent-%C3%A9"},
	{"dot dot", "..", "/agents/%2E%2E"},
	{"dot", ".", "/agents/%2E"},
	{"query chars", "a?b#c", "/agents/a%3Fb%23c"},
}

func TestClient_PathIDsAreEscapedOnce(t *testing.T) {
	for _, tc := range pathIDCases {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := pathServer(t, "", `{}`)
			_, _ = c.GetAgent(context.Background(), tc.id)
			if len(*seen) != 1 || (*seen)[0] != tc.want {
				t.Fatalf("GetAgent(%q) sent %v, want %s", tc.id, *seen, tc.want)
			}
		})
	}
}

func TestClient_PathIDsAreEscapedOnce_UnderABasePathPrefix(t *testing.T) {
	for _, prefix := range []string{"/api", "/api/", "/my%20api"} {
		t.Run(prefix, func(t *testing.T) {
			c, seen := pathServer(t, prefix, `{}`)
			_, _ = c.GetAgent(context.Background(), "a 1/..")
			want := strings.TrimSuffix(prefix, "/") + "/agents/a%201%2F.."
			if len(*seen) != 1 || (*seen)[0] != want {
				t.Fatalf("sent %v, want %s", *seen, want)
			}
		})
	}
}

// Every request route (JSON, list, raw download, upload, stream) goes through
// buildURLValues; check each one sends the ID escaped once.
func TestClient_PathIDsAreEscapedOnce_OnEveryRoute(t *testing.T) {
	c, seen := pathServer(t, "/api/", `{}`)
	ctx := context.Background()
	id := "x y/z"
	_, _ = c.GetAgent(ctx, id)
	_, _ = c.ListCloudDriveRejections(ctx, id, CloudDriveRejectionOptions{})
	if resp, err := c.DownloadSourceExport(ctx, id, ".."); err == nil {
		resp.Body.Close()
	}
	_, _ = c.UploadFileToSource(ctx, id, UploadFileRequest{File: []byte("x"), FileName: "f.txt"})
	_, _ = c.RunStreamingAgentAndWait(ctx, id, AgentRunStreamRequest{})
	events, errs := c.RunStreamingAgent(ctx, id, AgentRunStreamRequest{})
	for range events {
	}
	for range errs {
	}
	want := []string{
		"/api/agents/x%20y%2Fz",
		"/api/cloud-drives/x%20y%2Fz/rejections",
		"/api/sources/x%20y%2Fz/exports/%2E%2E/download",
		"/api/sources/x%20y%2Fz/upload",
		"/api/agents/x%20y%2Fz/runs/stream",
		"/api/agents/x%20y%2Fz/runs/stream",
	}
	if strings.Join(*seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sent\n%s\nwant\n%s", strings.Join(*seen, "\n"), strings.Join(want, "\n"))
	}
}

// With Options.EscapedDoPaths, Do sends apiPath as given. The released
// default is pinned in client_released_behaviour_test.go.
func TestClient_Do_PathHandling(t *testing.T) {
	cases := []struct{ prefix, apiPath, want string }{
		{"", "/agents/a%201", "/agents/a%201"},         // already escaped: sent as given
		{"", "/agents/a 1", "/agents/a%201"},           // raw space: escaped
		{"", "/files/100%", "/files/100%25"},           // stray percent: escaped
		{"", "agents", "/agents"},                      // leading slash added
		{"", "/agents/", "/agents/"},                   // trailing slash kept
		{"", "/agents//x/./y/../z", "/agents/x/z"},     // literal dot segments cleaned
		{"/api/", "/agents/", "/api/agents/"},          // base prefix with trailing slash
		{"/api", "/agents/a%2F1", "/api/agents/a%2F1"}, // %2F is not a separator
		// Escaped and raw characters mixed: the escapes the caller wrote are kept.
		{"", "/agents/a%2F1/x y", "/agents/a%2F1/x%20y"},
		{"", "/agents/é%2F1", "/agents/%C3%A9%2F1"},
		{"", "/agents/a%2F1?b", "/agents/a%2F1%3Fb"},
	}
	for _, tc := range cases {
		t.Run(tc.prefix+" "+tc.apiPath, func(t *testing.T) {
			c, seen := pathServer(t, tc.prefix, `{}`)
			c.escapedDoPaths = true
			if err := c.Do(context.Background(), http.MethodGet, tc.apiPath, nil, nil, nil, nil); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if len(*seen) != 1 || (*seen)[0] != tc.want {
				t.Fatalf("sent %v, want %s", *seen, tc.want)
			}
		})
	}
}

func TestPathSegment(t *testing.T) {
	for in, want := range map[string]string{".": "%2E", "..": "%2E%2E", "...": "...", "a b": "a%20b", "a/b": "a%2Fb"} {
		if got := pathSegment(in); got != want {
			t.Errorf("pathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

// A blank ID would leave an empty segment that path.Clean collapses into
// another endpoint ("/agents//runs" is "/agents/runs"), so every route refuses
// it with a *ConfigurationError before sending anything.
func TestClient_BlankPathIDIsRefused(t *testing.T) {
	c, seen := pathServer(t, "", `{}`)
	ctx := context.Background()
	calls := map[string]func(id string) error{
		"GetAgent": func(id string) error { _, err := c.GetAgent(ctx, id); return err },
		"ListAgentRuns": func(id string) error {
			_, err := c.ListAgentRuns(ctx, id, ListAgentRunsOptions{})
			return err
		},
		"GetEvaluationCriteria": func(id string) error { _, err := c.GetEvaluationCriteria(ctx, id); return err },
		"SetMetadataKeys trigger": func(id string) error {
			_, err := c.SetMetadataKeys(ctx, "a_1", id, SetMetadataKeysRequest{MetadataKeys: []MetadataKeyDeclaration{}})
			return err
		},
		"DownloadSourceExport": func(id string) error {
			resp, err := c.DownloadSourceExport(ctx, "s_1", id)
			if err == nil {
				resp.Body.Close()
			}
			return err
		},
		"UploadFileToContent": func(id string) error {
			_, err := c.UploadFileToContent(ctx, id, UploadFileRequest{File: []byte("x"), FileName: "f.txt"})
			return err
		},
		"RunStreamingAgentAndWait": func(id string) error {
			_, err := c.RunStreamingAgentAndWait(ctx, id, AgentRunStreamRequest{})
			return err
		},
		"RunStreamingAgent": func(id string) error {
			events, errs := c.RunStreamingAgent(ctx, id, AgentRunStreamRequest{})
			for range events {
			}
			return <-errs
		},
	}
	for name, call := range calls {
		for _, id := range []string{"", "  "} {
			t.Run(name+"/"+id, func(t *testing.T) {
				*seen = nil
				err := call(id)
				var cfg *ConfigurationError
				if !errors.As(err, &cfg) {
					t.Fatalf("err = %v, want *ConfigurationError", err)
				}
				if len(*seen) != 0 {
					t.Fatalf("sent %v, want no request", *seen)
				}
			})
		}
	}
}
