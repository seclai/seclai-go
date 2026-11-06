package seclai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These tests pin the behaviour v1.9.0 released, which stays the default
// until the next major version. They use only v1.9.0's API, so they also run
// against that tag's code.

// By default Do takes apiPath as unescaped and escapes it as a whole.
func TestClient_Do_DefaultEscapesTheWholePath(t *testing.T) {
	for _, tc := range []struct{ apiPath, want string }{
		{"/agents/a%201", "/agents/a%25201"},
		{"/agents/a 1", "/agents/a%201"},
		{"/files/100%", "/files/100%25"},
		{"/agents/é", "/agents/%C3%A9"},
		{"agents", "/agents"},
		{"/agents/", "/agents/"},
		{"/agents//x/./y/../z", "/agents/x/z"},
	} {
		t.Run(tc.apiPath, func(t *testing.T) {
			var seen []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target, _, _ := strings.Cut(r.RequestURI, "?")
				seen = append(seen, target)
			}))
			t.Cleanup(srv.Close)
			c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if err := c.Do(context.Background(), http.MethodGet, tc.apiPath, nil, nil, nil, nil); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if len(seen) != 1 || seen[0] != tc.want {
				t.Fatalf("sent %v, want %s", seen, tc.want)
			}
		})
	}
}

// By default the wait helper returns the init run when the stream ends
// without done, and the bare error on a timeout or a broken stream.
func TestClient_RunStreamingAgentAndWait_ReleasedEndings(t *testing.T) {
	const initEv = "event: init\ndata: {\"attempts\":[],\"error_count\":0,\"priority\":true,\"run_id\":\"run_1\",\"status\":\"processing\"}\n\n"
	for _, tc := range []struct {
		name, stream, then string // then: "eof", "hang" or "cut", as in the Endings test
		check              func(*AgentRunResponse, error) error
	}{
		{"ends after init", initEv, "eof", func(res *AgentRunResponse, err error) error {
			if err != nil || res == nil || res.RunId != "run_1" {
				return fmt.Errorf("got %v %v, want the init run and no error", res, err)
			}
			return nil
		}},
		{"ends with nothing", "", "eof", func(_ *AgentRunResponse, err error) error {
			var streamErr *StreamingError
			if !errors.As(err, &streamErr) {
				return fmt.Errorf("got %T %v, want *StreamingError", err, err)
			}
			return nil
		}},
		{"times out", initEv, "hang", func(_ *AgentRunResponse, err error) error {
			if err != context.DeadlineExceeded {
				return fmt.Errorf("got %T %v, want the bare context.DeadlineExceeded", err, err)
			}
			return nil
		}},
		{"cut", initEv, "cut", func(_ *AgentRunResponse, err error) error {
			if err != io.ErrUnexpectedEOF {
				return fmt.Errorf("got %T %v, want the bare io.ErrUnexpectedEOF", err, err)
			}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.then == "cut" {
					conn, buf, _ := w.(http.Hijacker).Hijack()
					defer conn.Close()
					_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", len(tc.stream), tc.stream)
					_ = buf.Flush()
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.stream)
				w.(http.Flusher).Flush()
				if tc.then == "hang" {
					<-r.Context().Done()
				}
			}))
			t.Cleanup(srv.Close)
			c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := tc.check(c.RunStreamingAgentAndWait(ctx, "agent_1", AgentRunStreamRequest{})); err != nil {
				t.Fatal(err)
			}
		})
	}
}
