package seclai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	streamInitEv   = "event: init\ndata: {\"attempts\":[],\"error_count\":0,\"priority\":true,\"run_id\":\"run_1\",\"status\":\"pending\"}\n\n"
	streamUpdateEv = "event: update\ndata: {\"attempts\":[],\"error_count\":0,\"priority\":true,\"run_id\":\"run_1\",\"status\":\"processing\"}\n\n"
	streamResultEv = "event: result\ndata: {\"attempts\":[],\"error_count\":0,\"priority\":true,\"run_id\":\"run_1\",\"status\":\"processing\",\"output\":\"answer\",\"result_reported_at\":\"2026-11-05T00:00:00Z\"}\n\n"
	streamDoneEv   = "event: done\ndata: {\"attempts\":[],\"error_count\":0,\"priority\":true,\"run_id\":\"run_1\",\"status\":\"completed\",\"output\":\"final\"}\n\n"
)

// streamServer writes body with contentType, then hangs until the client
// gives up when hang is set.
func streamServer(t *testing.T, contentType, body string, hang bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
		w.(http.Flusher).Flush()
		if hang {
			<-r.Context().Done()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collectEvents(t *testing.T, c *Client, ctx context.Context) ([]AgentRunEvent, error) {
	t.Helper()
	events, errCh := c.RunStreamingAgent(ctx, "agent_1", AgentRunStreamRequest{})
	var got []AgentRunEvent
	for evt := range events {
		got = append(got, evt)
	}
	return got, <-errCh
}

// Run is set for the run snapshot and status events, and for error and
// timeout events (RunId only, as released), not for stream_token, reasoning
// or commentary events, whose data is also a JSON object.
func TestClient_RunStreamingAgent_RunOnlyOnEventsThatCarryARun(t *testing.T) {
	stream := streamInitEv +
		"event: status\ndata: {\"run_id\":\"run_1\",\"status\":\"processing\"}\n\n" +
		"event: stream_token\ndata: {\"token\":\"a\"}\n\n" +
		"event: stream_reasoning\ndata: {\"phase\":\"reasoning\",\"text\":\"x\"}\n\n" +
		"event: stream_commentary\ndata: {\"phase\":\"commentary\",\"text\":\"y\"}\n\n" +
		streamUpdateEv + streamResultEv +
		"event: error\ndata: {\"run_id\":\"run_1\",\"error\":\"boom\"}\n\n" +
		"event: timeout\ndata: {\"run_id\":\"run_1\"}\n\n" +
		streamDoneEv
	srv := streamServer(t, "text/event-stream", stream, false)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	got, err := collectEvents(t, c, context.Background())
	if err != nil {
		t.Fatalf("RunStreamingAgent: %v", err)
	}
	var names []string
	for _, evt := range got {
		names = append(names, evt.Event)
		isRunEvent := evt.Event == "init" || evt.Event == "status" || evt.Event == "update" || evt.Event == "result" || evt.Event == "done"
		isEnd := evt.Event == "error" || evt.Event == "timeout"
		if (evt.Run != nil) != (isRunEvent || isEnd) {
			t.Errorf("%s: Run %+v", evt.Event, evt.Run)
		}
		if isEnd && evt.Run != nil && (evt.Run.RunId != "run_1" || evt.Run.Status != "") {
			t.Errorf("%s: RunId %q Status %q, want run_1 and empty", evt.Event, evt.Run.RunId, evt.Run.Status)
		}
		if isRunEvent && evt.Run != nil && (evt.Run.RunId != "run_1" || evt.Run.Status == "") {
			t.Errorf("%s: RunId %q Status %q", evt.Event, evt.Run.RunId, evt.Run.Status)
		}
	}
	if strings.Join(names, ",") != "init,status,stream_token,stream_reasoning,stream_commentary,update,result,error,timeout,done" {
		t.Fatalf("events %v", names)
	}
}

// An `event: error` or `event: timeout` ends the wait with a *StreamingError
// carrying the latest snapshot, in both modes, without waiting for the
// connection to close; an error's Message carries the server's text.
func TestClient_RunStreamingAgentAndWait_ServerErrorAndTimeoutEvents(t *testing.T) {
	const (
		errorEv   = "event: error\ndata: {\"run_id\":\"run_1\",\"error\":\"model provider unavailable\"}\n\n"
		timeoutEv = "event: timeout\ndata: {\"run_id\":\"run_1\"}\n\n"
	)
	for _, tc := range []struct {
		name, stream string
		wantText     string
		wantOutput   string
		wantStatus   string
	}{
		{"error after update", streamInitEv + streamUpdateEv + errorEv, "model provider unavailable", "", "pending"},
		{"error after result", streamInitEv + streamResultEv + streamUpdateEv + errorEv, "model provider unavailable", "answer", "processing"},
		{"timeout after init", streamInitEv + timeoutEv, "timed out", "", "pending"},
		{"timeout after result", streamInitEv + streamResultEv + timeoutEv, "timed out", "answer", "processing"},
	} {
		for _, errorOnIncomplete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ErrorOnIncomplete=%v", tc.name, errorOnIncomplete), func(t *testing.T) {
				srv := streamServer(t, "text/event-stream", tc.stream, true)
				c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				res, err := c.RunStreamingAgentAndWaitWithOptions(ctx, "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: errorOnIncomplete})
				var streamErr *StreamingError
				if !errors.As(err, &streamErr) {
					t.Fatalf("got %T %v, want *StreamingError", err, err)
				}
				if ctx.Err() != nil {
					t.Fatalf("waited until ctx ended: %v", err)
				}
				if !strings.Contains(streamErr.Message, tc.wantText) || streamErr.Err != nil {
					t.Fatalf("Message %q Err %v, want %q and no cause", streamErr.Message, streamErr.Err, tc.wantText)
				}
				if errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("a server event is not a ctx deadline: %v", err)
				}
				if res == nil || streamErr.Run != res || streamErr.RunID != "run_1" {
					t.Fatalf("Run %p RunID %q, want the returned run %p and run_1", streamErr.Run, streamErr.RunID, res)
				}
				gotOutput := ""
				if res.Output != nil {
					gotOutput = *res.Output
				}
				if gotOutput != tc.wantOutput || string(res.Status) != tc.wantStatus {
					t.Fatalf("got output %q status %q, want %q %q", gotOutput, res.Status, tc.wantOutput, tc.wantStatus)
				}
			})
		}
	}
}

// A final line without its newline is parsed by both methods.
func TestClient_RunStreaming_FinalLineWithoutNewline(t *testing.T) {
	stream := streamInitEv + strings.TrimSuffix(streamDoneEv, "\n\n")
	srv := streamServer(t, "text/event-stream", stream, false)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})

	got, err := collectEvents(t, c, context.Background())
	if err != nil || len(got) != 2 || got[1].Event != "done" || got[1].Run == nil || got[1].Run.Status != "completed" {
		t.Fatalf("RunStreamingAgent: got %+v %v, want init and the done run", got, err)
	}
	for _, errorOnIncomplete := range []bool{false, true} {
		res, err := c.RunStreamingAgentAndWaitWithOptions(context.Background(), "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: errorOnIncomplete})
		if err != nil || res == nil || res.Status != "completed" {
			t.Fatalf("ErrorOnIncomplete=%v: got %+v %v, want the done run", errorOnIncomplete, res, err)
		}
	}
}

// A plain JSON reply is one done event in the iterator and the returned run
// in the wait helper.
func TestClient_RunStreaming_JSONReply(t *testing.T) {
	const run = `{"attempts":[],"error_count":0,"priority":true,"run_id":"run_1","status":"completed","output":"final"}`
	srv := streamServer(t, "application/json", run, false)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})

	got, err := collectEvents(t, c, context.Background())
	if err != nil || len(got) != 1 || got[0].Event != "done" || got[0].Run == nil || got[0].Run.RunId != "run_1" {
		t.Fatalf("RunStreamingAgent: got %+v %v, want one done event with the run", got, err)
	}
	for _, errorOnIncomplete := range []bool{false, true} {
		res, err := c.RunStreamingAgentAndWaitWithOptions(context.Background(), "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: errorOnIncomplete})
		if err != nil || res == nil || res.Output == nil || *res.Output != "final" {
			t.Fatalf("ErrorOnIncomplete=%v: got %+v %v, want the run", errorOnIncomplete, res, err)
		}
	}
}

// A plain JSON reply whose run has not finished is the latest snapshot: by
// default it is returned, and with ErrorOnIncomplete it comes with a
// *StreamingError, unless ReturnOnResult is set and it reported its result.
func TestClient_RunStreamingAgentAndWait_JSONReplyNotFinished(t *testing.T) {
	for _, tc := range []struct {
		name, run         string
		returnOnResult    bool
		wantErrStrictMode bool
	}{
		{"processing", `{"attempts":[],"error_count":0,"priority":true,"run_id":"run_1","status":"processing"}`, false, true},
		{"result reported, waiting for done", `{"attempts":[],"error_count":0,"priority":true,"run_id":"run_1","status":"processing","output":"answer","result_reported_at":"2026-11-05T00:00:00Z"}`, false, true},
		{"result reported, ReturnOnResult", `{"attempts":[],"error_count":0,"priority":true,"run_id":"run_1","status":"processing","output":"answer","result_reported_at":"2026-11-05T00:00:00Z"}`, true, false},
		{"failed", `{"attempts":[],"error_count":1,"priority":true,"run_id":"run_1","status":"failed"}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := streamServer(t, "application/json", tc.run, false)
			c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
			for _, errorOnIncomplete := range []bool{false, true} {
				res, err := c.RunStreamingAgentAndWaitWithOptions(context.Background(), "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: errorOnIncomplete, ReturnOnResult: tc.returnOnResult})
				if res == nil || res.RunId != "run_1" {
					t.Fatalf("ErrorOnIncomplete=%v: got run %+v, want the JSON run", errorOnIncomplete, res)
				}
				var streamErr *StreamingError
				wantErr := errorOnIncomplete && tc.wantErrStrictMode
				if errors.As(err, &streamErr) != wantErr || (!wantErr && err != nil) {
					t.Fatalf("ErrorOnIncomplete=%v: got %T %v, want a *StreamingError %v", errorOnIncomplete, err, err, wantErr)
				}
				if wantErr && (streamErr.Run != res || streamErr.RunID != "run_1") {
					t.Fatalf("Run %p RunID %q, want the returned run", streamErr.Run, streamErr.RunID)
				}
			}
		})
	}
}

// A JSON reply cut off mid-body is the read error, not a done event.
func TestClient_RunStreaming_JSONReplyCut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, _ := w.(http.Hijacker).Hijack()
		defer conn.Close()
		_, _ = io.WriteString(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 200\r\n\r\n{\"run_id\":\"run_1\"")
		_ = buf.Flush()
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})

	got, err := collectEvents(t, c, context.Background())
	if len(got) != 0 || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("RunStreamingAgent: got %+v %v, want no event and io.ErrUnexpectedEOF", got, err)
	}
	res, err := c.RunStreamingAgentAndWait(context.Background(), "agent_1", AgentRunStreamRequest{})
	if res != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("RunStreamingAgentAndWait: got %+v %v, want io.ErrUnexpectedEOF", res, err)
	}
}

// An update event does not feed the latest snapshot, as at the last release:
// a stream that ends after init and update returns the init run.
func TestClient_RunStreamingAgentAndWait_UpdateIsNotTheLatestSnapshot(t *testing.T) {
	srv := streamServer(t, "text/event-stream", streamInitEv+streamUpdateEv, false)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	res, err := c.RunStreamingAgentAndWait(context.Background(), "agent_1", AgentRunStreamRequest{})
	if err != nil || res == nil || res.Status != "pending" {
		t.Fatalf("got %+v %v, want the init run", res, err)
	}
}

// A status event does not replace the latest snapshot either.
func TestClient_RunStreamingAgentAndWait_StatusIsNotTheLatestSnapshot(t *testing.T) {
	statusEv := "event: status\ndata: {\"run_id\":\"run_1\",\"status\":\"processing\"}\n\n"
	srv := streamServer(t, "text/event-stream", streamInitEv+statusEv, false)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	res, err := c.RunStreamingAgentAndWait(context.Background(), "agent_1", AgentRunStreamRequest{})
	if err != nil || res == nil || res.Status != "pending" {
		t.Fatalf("got %+v %v, want the init run", res, err)
	}
}

// scaleDefaultClientTimeouts stands in for the default client's 30s with d,
// so a test can outlast it.
func scaleDefaultClientTimeouts(t *testing.T, c *Client, d time.Duration) {
	t.Helper()
	if c.httpClient.Timeout != 30*time.Second || c.streamHeaderTimeout != 30*time.Second {
		t.Fatalf("not the default client: Timeout %v, header timeout %v", c.httpClient.Timeout, c.streamHeaderTimeout)
	}
	c.httpClient.Timeout = d
	c.streamHeaderTimeout = d
}

// The default client's Timeout does not cut a stream: both methods read past
// it, bounded by ctx.
func TestClient_RunStreaming_DefaultClientTimeoutDoesNotCutTheStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, streamInitEv)
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, streamDoneEv)
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	scaleDefaultClientTimeouts(t, c, 100*time.Millisecond)

	got, err := collectEvents(t, c, context.Background())
	if err != nil || len(got) != 2 || got[1].Event != "done" {
		t.Fatalf("RunStreamingAgent: got %+v %v, want init and done", got, err)
	}
	res, err := c.RunStreamingAgentAndWaitWithOptions(context.Background(), "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: true})
	if err != nil || res == nil || res.Status != "completed" {
		t.Fatalf("RunStreamingAgentAndWait: got %+v %v, want the done run", res, err)
	}
}

// With the default client, waiting for a stream's response headers stays
// bounded, even when ctx has no deadline.
func TestClient_RunStreaming_DefaultClientBoundsTheHeaderWait(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs before srv.Close
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	scaleDefaultClientTimeouts(t, c, 100*time.Millisecond)

	check := func(name string, err error) {
		t.Helper()
		var urlErr *url.Error
		var netErr net.Error
		if !errors.As(err, &urlErr) || !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("%s: got %T %v, want a *url.Error that is a timeout", name, err, err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: errors.Is(err, context.DeadlineExceeded) is false for %v", name, err)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := collectEvents(t, c, context.Background())
		if len(got) != 0 {
			t.Errorf("RunStreamingAgent: events %+v", got)
		}
		check("RunStreamingAgent", err)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunStreamingAgent did not give up waiting for the headers")
	}
	for _, errorOnIncomplete := range []bool{false, true} {
		res, err := c.RunStreamingAgentAndWaitWithOptions(context.Background(), "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: errorOnIncomplete})
		if res != nil {
			t.Fatalf("got run %+v", res)
		}
		check(fmt.Sprintf("RunStreamingAgentAndWait ErrorOnIncomplete=%v", errorOnIncomplete), err)
	}
}

// A caller-supplied HTTPClient is used as it is for streams: its Transport
// carries the request and its Timeout cuts the stream.
func TestClient_RunStreaming_SuppliedClientKeepsItsTimeout(t *testing.T) {
	srv := streamServer(t, "text/event-stream", streamInitEv, true)
	var viaTransport atomic.Int32
	hc := &http.Client{
		Timeout: 100 * time.Millisecond,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			viaTransport.Add(1)
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL, HTTPClient: hc})

	// ctx outlasts the client's Timeout, so only the Timeout ends the stream.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := collectEvents(t, c, ctx)
	var netErr net.Error
	if len(got) != 1 || !errors.As(err, &netErr) || !netErr.Timeout() || ctx.Err() != nil {
		t.Fatalf("RunStreamingAgent: got %+v %T %v, want init and the client's timeout", got, err, err)
	}
	res, err := c.RunStreamingAgentAndWaitWithOptions(ctx, "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: true})
	var streamErr *StreamingError
	if !errors.As(err, &streamErr) || !strings.Contains(streamErr.Message, "broke") || res == nil || res.RunId != "run_1" || ctx.Err() != nil {
		t.Fatalf("RunStreamingAgentAndWait: got %+v %T %v, want the init run and a broken stream", res, err, err)
	}
	if viaTransport.Load() != 2 {
		t.Fatalf("%d requests went through the supplied Transport, want 2", viaTransport.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A program may replace http.DefaultTransport with another type (otelhttp,
// httpmock). The default client must not assume its type, and both the
// default client's requests and its streams go through the replacement.
func TestClient_DefaultClientUsesAReplacedDefaultTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/runs/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, streamDoneEv)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"attempts":[],"error_count":0,"priority":false,"run_id":"run_1","status":"completed"}`)
	}))
	t.Cleanup(srv.Close)

	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	var seen []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Path)
		return original.RoundTrip(r)
	})

	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetAgentRun(context.Background(), "run_1", nil); err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if _, err := c.RunStreamingAgentAndWait(context.Background(), "agent_1", AgentRunStreamRequest{}); err != nil {
		t.Fatalf("RunStreamingAgentAndWait: %v", err)
	}
	if _, err := collectEvents(t, c, context.Background()); err != nil {
		t.Fatalf("RunStreamingAgent: %v", err)
	}
	if strings.Join(seen, ",") != "/agents/runs/run_1,/agents/agent_1/runs/stream,/agents/agent_1/runs/stream" {
		t.Fatalf("through the replaced DefaultTransport: %v", seen)
	}
}

// A body cut off mid-read, or a ctx deadline passing before it arrives, is
// returned as an error that wraps the cause, not decoded as a (zero) run.
func TestClient_BodyReadErrorIsReturned(t *testing.T) {
	for _, tc := range []struct {
		name string
		then string // "cut": the body stops short; "hang": no body byte arrives
		want error
	}{
		{"cut", "cut", io.ErrUnexpectedEOF},
		{"deadline", "hang", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, buf, _ := w.(http.Hijacker).Hijack()
				defer conn.Close()
				_, _ = io.WriteString(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 200\r\n\r\n")
				if tc.then == "cut" {
					_, _ = io.WriteString(buf, `{"run_id":"run_1"`)
				}
				_ = buf.Flush()
				if tc.then == "hang" {
					_, _ = conn.Read(make([]byte, 1)) // until the client closes
				}
			}))
			t.Cleanup(srv.Close)
			c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
			// Each call gets its own ctx, so it fails reading the body, not
			// on a ctx that an earlier call let expire.
			call := func(f func(ctx context.Context) error) error {
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				defer cancel()
				return f(ctx)
			}

			var run *AgentRunResponse
			err := call(func(ctx context.Context) (err error) { run, err = c.GetAgentRun(ctx, "run_1", nil); return })
			if run != nil || !errors.Is(err, tc.want) {
				t.Fatalf("GetAgentRun: got %+v %v, want an error wrapping %v", run, err, tc.want)
			}
			if !strings.Contains(err.Error(), "GET ") {
				t.Fatalf("GetAgentRun error does not say what was read: %v", err)
			}
			var up *FileUploadResponse
			err = call(func(ctx context.Context) (err error) {
				up, err = c.UploadFileToSource(ctx, "src_1", UploadFileRequest{File: []byte("x"), FileName: "a.txt"})
				return
			})
			if up != nil || !errors.Is(err, tc.want) {
				t.Fatalf("UploadFileToSource: got %+v %v, want an error wrapping %v", up, err, tc.want)
			}
			// A method returning the body as json.RawMessage returns the error too.
			var raw json.RawMessage
			err = call(func(ctx context.Context) (err error) { raw, err = c.GetAgentsUsingMemoryBank(ctx, "mb_1"); return })
			if raw != nil || !errors.Is(err, tc.want) {
				t.Fatalf("GetAgentsUsingMemoryBank: got %s %v, want an error wrapping %v", raw, err, tc.want)
			}
			var out map[string]any
			if err := call(func(ctx context.Context) error { return c.Do(ctx, http.MethodGet, "/x", nil, nil, nil, &out) }); !errors.Is(err, tc.want) {
				t.Fatalf("Do with out: got %v, want an error wrapping %v", err, tc.want)
			}
		})
	}
}

// A method that decodes nothing succeeds on a 2xx whose body could not be
// read, as at v1.11.0: the action was accepted, and a retry could repeat it.
func TestClient_BodyReadErrorIgnoredWhenNothingIsDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, _ := w.(http.Hijacker).Hijack()
		defer conn.Close()
		_, _ = io.WriteString(buf, "HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: 200\r\n\r\n{\"status\"")
		_ = buf.Flush()
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	if err := c.CompactMemoryBank(context.Background(), "mb_1"); err != nil {
		t.Fatalf("CompactMemoryBank: %v", err)
	}
	if err := c.DeleteMemoryBankSource(context.Background(), "mb_1"); err != nil {
		t.Fatalf("DeleteMemoryBankSource: %v", err)
	}
	if err := c.Do(context.Background(), http.MethodPost, "/x", nil, nil, nil, nil); err != nil {
		t.Fatalf("Do with a nil out: %v", err)
	}
	if err := c.DeleteAgentRun(context.Background(), "run_1"); err != nil {
		t.Fatalf("DeleteAgentRun: %v", err)
	}
}

// classifyTimeout is the test ErrDefaultStreamTimeout documents.
func classifyTimeout(ctx context.Context, err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, ErrDefaultStreamTimeout):
		return "sdk default"
	case ctx.Err() != nil:
		return "ctx"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "http client"
	}
	return fmt.Sprintf("other: %T %v", err, err)
}

// The documented test tells the SDK's default deadline, the caller's ctx
// deadline and the HTTP client's Timeout apart, for each stream method and
// mode, before and after the response headers.
func TestClient_RunStreaming_TellingTimeoutsApart(t *testing.T) {
	const short = 100 * time.Millisecond
	type method int
	const (
		iterator method = iota
		waitDefault
		waitErrorOnIncomplete
	)
	for _, tc := range []struct {
		kind      string // "sdk default", "ctx" or "http client"
		client    string // "default", or "timeout": a supplied client with Timeout; "plain": one without
		headers   bool   // whether the server sends the headers and init before hanging
		methods   []method
		wantNoRun bool
	}{
		{"sdk default", "default", true, []method{waitDefault, waitErrorOnIncomplete}, false},
		{"sdk default", "plain", false, []method{waitDefault, waitErrorOnIncomplete}, true},
		{"ctx", "default", true, []method{iterator, waitDefault, waitErrorOnIncomplete}, false},
		{"ctx", "plain", false, []method{iterator, waitDefault, waitErrorOnIncomplete}, true},
		{"http client", "timeout", true, []method{iterator, waitDefault, waitErrorOnIncomplete}, false},
		{"http client", "timeout", false, []method{iterator, waitDefault, waitErrorOnIncomplete}, true},
		{"http client", "default", false, []method{iterator, waitDefault, waitErrorOnIncomplete}, true}, // the 30s header bound
	} {
		for _, m := range tc.methods {
			t.Run(fmt.Sprintf("%s/%s client/headers=%v/method=%d", tc.kind, tc.client, tc.headers, m), func(t *testing.T) {
				release := make(chan struct{})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.headers {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, streamInitEv)
						w.(http.Flusher).Flush()
					}
					<-release
				}))
				t.Cleanup(srv.Close)
				t.Cleanup(func() { close(release) })

				opts := Options{APIKey: "k", BaseURL: srv.URL}
				switch tc.client {
				case "timeout":
					opts.HTTPClient = &http.Client{Timeout: short}
				case "plain":
					opts.HTTPClient = &http.Client{}
				}
				c, _ := NewClient(opts)
				c.streamWaitDefault = 10 * short
				if tc.client == "default" && tc.kind == "http client" {
					scaleDefaultClientTimeouts(t, c, short)
				}
				if tc.kind == "sdk default" {
					c.streamWaitDefault = short
				}

				ctx := context.Background()
				if tc.kind == "ctx" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, short)
					defer cancel()
				}

				var err error
				var res *AgentRunResponse
				switch m {
				case iterator:
					var got []AgentRunEvent
					got, err = collectEvents(t, c, ctx)
					if tc.wantNoRun != (len(got) == 0) {
						t.Fatalf("events %+v", got)
					}
				case waitDefault, waitErrorOnIncomplete:
					res, err = c.RunStreamingAgentAndWaitWithOptions(ctx, "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: m == waitErrorOnIncomplete})
					if tc.wantNoRun != (res == nil) {
						t.Fatalf("run %+v", res)
					}
					var streamErr *StreamingError
					if isStreamErr := errors.As(err, &streamErr); isStreamErr != (m == waitErrorOnIncomplete && tc.headers) {
						t.Fatalf("got %T %v", err, err)
					}
				}
				if got := classifyTimeout(ctx, err); got != tc.kind {
					t.Fatalf("classified %q, want %q: %T %v", got, tc.kind, err, err)
				}
				// Every kind is a net.Error timeout, as at v1.11.0.
				var netErr net.Error
				if !errors.As(err, &netErr) || !netErr.Timeout() {
					t.Fatalf("not a net.Error timeout: %T %v", err, err)
				}
				if tc.kind != "http client" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("errors.Is(err, context.DeadlineExceeded) is false for %v", err)
				}
			})
		}
	}
}

// When the default deadline cuts the reading of a non-2xx response's body,
// the error is still the *APIStatusError, not marked as the SDK's default.
func TestClient_RunStreamingAgentAndWait_DefaultDeadlineLeavesAPIErrorsAlone(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "200")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"detail":`)
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	c.streamWaitDefault = 100 * time.Millisecond

	for _, errorOnIncomplete := range []bool{false, true} {
		_, err := c.RunStreamingAgentAndWaitWithOptions(context.Background(), "agent_1", AgentRunStreamRequest{}, &RunStreamingAgentAndWaitOptions{ErrorOnIncomplete: errorOnIncomplete})
		if errors.Is(err, ErrDefaultStreamTimeout) {
			t.Fatalf("ErrorOnIncomplete=%v: %v matches ErrDefaultStreamTimeout", errorOnIncomplete, err)
		}
		if apiErr, ok := err.(*APIStatusError); !ok || apiErr.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ErrorOnIncomplete=%v: got %T %v, want the *APIStatusError", errorOnIncomplete, err, err)
		}
	}
}

// Downloads are not cut by the default client's Timeout, waiting for their
// headers stays bounded, and a supplied client's Timeout still cuts them.
func TestClient_Download_Timeouts(t *testing.T) {
	const short = 100 * time.Millisecond
	slow := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "part1-")
		w.(http.Flusher).Flush()
		time.Sleep(3 * short)
		_, _ = io.WriteString(w, "part2")
	}
	read := func(c *Client) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*short)
		defer cancel()
		resp, err := c.DownloadSourceExport(ctx, "src_1", "exp_1")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}

	t.Run("default client reads past its Timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(slow))
		t.Cleanup(srv.Close)
		c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
		scaleDefaultClientTimeouts(t, c, short)
		if got, err := read(c); err != nil || got != "part1-part2" {
			t.Fatalf("got %q %v, want the whole file", got, err)
		}
	})
	t.Run("default client bounds the header wait", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })
		c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
		scaleDefaultClientTimeouts(t, c, short)
		start := time.Now()
		_, err := read(c)
		if time.Since(start) > 10*short {
			t.Fatalf("waited %v for the headers", time.Since(start))
		}
		var urlErr *url.Error
		var netErr net.Error
		if !errors.As(err, &urlErr) || urlErr.Op != "Get" || !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("got %T %v, want a GET *url.Error that is a timeout", err, err)
		}
	})
	t.Run("supplied client keeps its Timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(slow))
		t.Cleanup(srv.Close)
		c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL, HTTPClient: &http.Client{Timeout: short}})
		got, err := read(c)
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() || got == "part1-part2" {
			t.Fatalf("got %q %T %v, want the client's timeout", got, err, err)
		}
	})
}

// With the default client, a download's non-2xx error body that stalls is
// read for a bounded time, even when ctx has no deadline, and the error is
// the *APIStatusError carrying what was read.
func TestClient_Download_ErrorBodyReadIsBounded(t *testing.T) {
	const short = 100 * time.Millisecond
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "200")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "bad gate")
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	scaleDefaultClientTimeouts(t, c, short)

	done := make(chan error, 1)
	go func() {
		_, err := c.DownloadSourceExport(context.Background(), "src_1", "exp_1")
		done <- err
	}()
	select {
	case err := <-done:
		var apiErr *APIStatusError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway || apiErr.ResponseText != "bad gate" {
			t.Fatalf("got %T %v, want the 502 with the body read so far", err, err)
		}
	case <-time.After(30 * short):
		t.Fatal("reading the stalled error body did not end")
	}
}

// With the default client, a stream request's non-2xx error body that
// stalls is read for a bounded time, even when ctx has no deadline.
func TestClient_RunStreamingAgent_ErrorBodyReadIsBounded(t *testing.T) {
	const short = 100 * time.Millisecond
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "200")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "bad gate")
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	scaleDefaultClientTimeouts(t, c, short)

	done := make(chan error, 1)
	go func() {
		_, err := collectEvents(t, c, context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		var apiErr *APIStatusError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway || apiErr.ResponseText != "bad gate" {
			t.Fatalf("got %T %v, want the 502 with the body read so far", err, err)
		}
	case <-time.After(30 * short):
		t.Fatal("reading the stalled error body did not end")
	}
}
