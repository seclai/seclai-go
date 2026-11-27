package seclai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// rangeServer answers every request with status, headers and body, and records
// the Range header (present reports whether one was sent at all) and the query.
type rangeServer struct {
	requests     atomic.Int32
	rangeValue   string
	rangePresent bool
	rawQuery     string
}

func newRangeServer(t *testing.T, status int, headers map[string]string, body string) (*Client, *rangeServer) {
	t.Helper()
	rs := &rangeServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.requests.Add(1)
		_, rs.rangePresent = r.Header["Range"]
		rs.rangeValue = r.Header.Get("Range")
		rs.rawQuery = r.URL.RawQuery
		if r.Method != http.MethodGet || r.URL.Path != "/v2/agent-runs/run_1/attachments/att_1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, rs
}

func i64(v int64) *int64 { return &v }

func TestDownloadAgentRunAttachmentWithOptions_RangeHeader(t *testing.T) {
	cases := []struct {
		name string
		opts DownloadAgentRunAttachmentOptions
		want string
	}{
		{"start only", DownloadAgentRunAttachmentOptions{RangeStart: i64(100)}, "bytes=100-"},
		{"start zero", DownloadAgentRunAttachmentOptions{RangeStart: i64(0)}, "bytes=0-"},
		{"start and end", DownloadAgentRunAttachmentOptions{RangeStart: i64(10), RangeEnd: i64(19)}, "bytes=10-19"},
		{"start equals end", DownloadAgentRunAttachmentOptions{RangeStart: i64(0), RangeEnd: i64(0)}, "bytes=0-0"},
		{"suffix", DownloadAgentRunAttachmentOptions{RangeSuffix: i64(500)}, "bytes=-500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rs := newRangeServer(t, http.StatusOK, nil, "x")
			resp, err := c.DownloadAgentRunAttachmentWithOptions(context.Background(), "run_1", "att_1", tc.opts)
			if err != nil {
				t.Fatalf("DownloadAgentRunAttachmentWithOptions: %v", err)
			}
			resp.Body.Close()
			if !rs.rangePresent || rs.rangeValue != tc.want {
				t.Fatalf("Range = %q (present %v), want %q", rs.rangeValue, rs.rangePresent, tc.want)
			}
			if rs.rawQuery != "" {
				t.Fatalf("query = %q, want none", rs.rawQuery)
			}
		})
	}
}

func TestDownloadAgentRunAttachment_NoRangeHeaderWhenUnset(t *testing.T) {
	calls := map[string]func(c *Client) (*http.Response, error){
		"DownloadAgentRunAttachment": func(c *Client) (*http.Response, error) {
			return c.DownloadAgentRunAttachment(context.Background(), "run_1", "att_1", "report.pdf")
		},
		"DownloadAgentRunAttachmentWithOptions": func(c *Client) (*http.Response, error) {
			return c.DownloadAgentRunAttachmentWithOptions(context.Background(), "run_1", "att_1",
				DownloadAgentRunAttachmentOptions{DownloadName: "report.pdf"})
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			c, rs := newRangeServer(t, http.StatusOK, nil, "whole")
			resp, err := call(c)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			defer resp.Body.Close()
			if rs.rangePresent {
				t.Fatalf("Range header sent: %q", rs.rangeValue)
			}
			if rs.rawQuery != "download_name=report.pdf" {
				t.Fatalf("query = %q, want download_name=report.pdf", rs.rawQuery)
			}
			if body, _ := io.ReadAll(resp.Body); string(body) != "whole" {
				t.Fatalf("body = %q", body)
			}
		})
	}
}

func TestDownloadAgentRunAttachmentWithOptions_InvalidRangeSendsNothing(t *testing.T) {
	cases := map[string]DownloadAgentRunAttachmentOptions{
		"end without start":   {RangeEnd: i64(10)},
		"end before start":    {RangeStart: i64(10), RangeEnd: i64(9)},
		"negative start":      {RangeStart: i64(-1)},
		"negative end":        {RangeStart: i64(0), RangeEnd: i64(-1)},
		"negative end alone":  {RangeEnd: i64(-1)},
		"suffix with start":   {RangeStart: i64(0), RangeSuffix: i64(10)},
		"suffix with end":     {RangeEnd: i64(10), RangeSuffix: i64(10)},
		"suffix with both":    {RangeStart: i64(0), RangeEnd: i64(10), RangeSuffix: i64(10)},
		"zero suffix":         {RangeSuffix: i64(0)},
		"negative suffix":     {RangeSuffix: i64(-5)},
		"download name stays": {DownloadName: "a.pdf", RangeEnd: i64(3)},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			c, rs := newRangeServer(t, http.StatusOK, nil, "x")
			resp, err := c.DownloadAgentRunAttachmentWithOptions(context.Background(), "run_1", "att_1", opts)
			if resp != nil {
				resp.Body.Close()
				t.Fatalf("expected no response")
			}
			var cfgErr *ConfigurationError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("expected *ConfigurationError, got %T %v", err, err)
			}
			if n := rs.requests.Load(); n != 0 {
				t.Fatalf("%d request(s) sent, want 0", n)
			}
		})
	}
}

func TestDownloadAgentRunAttachmentWithOptions_PartialContent(t *testing.T) {
	c, rs := newRangeServer(t, http.StatusPartialContent, map[string]string{
		"Content-Range": "bytes 2-4/10",
		"Accept-Ranges": "bytes",
	}, "cde")
	resp, err := c.DownloadAgentRunAttachmentWithOptions(context.Background(), "run_1", "att_1",
		DownloadAgentRunAttachmentOptions{RangeStart: i64(2), RangeEnd: i64(4)})
	if err != nil {
		t.Fatalf("DownloadAgentRunAttachmentWithOptions: %v", err)
	}
	defer resp.Body.Close()
	if rs.rangeValue != "bytes=2-4" {
		t.Fatalf("Range = %q", rs.rangeValue)
	}
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes 2-4/10" {
		t.Fatalf("Content-Range = %q", got)
	}
	if body, _ := io.ReadAll(resp.Body); string(body) != "cde" {
		t.Fatalf("body = %q", body)
	}
}

func TestDownloadAgentRunAttachmentWithOptions_RangeNotSatisfiable(t *testing.T) {
	c, _ := newRangeServer(t, http.StatusRequestedRangeNotSatisfiable, map[string]string{
		"Content-Range": "bytes */10",
	}, `{"detail":"Requested range not satisfiable"}`)
	resp, err := c.DownloadAgentRunAttachmentWithOptions(context.Background(), "run_1", "att_1",
		DownloadAgentRunAttachmentOptions{RangeStart: i64(10)})
	if resp != nil {
		resp.Body.Close()
		t.Fatalf("expected no response on 416")
	}
	statusErr, ok := err.(*APIStatusError)
	if !ok || statusErr.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("expected a 416 *APIStatusError, got %T %v", err, err)
	}
}
