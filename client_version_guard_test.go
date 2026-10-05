package seclai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seclai/seclai-go/generated"
)

// versionRecorder is a server that records the Seclai-Version values of every
// request it receives.
type versionRecorder struct {
	requests int
	versions []string
}

func guardClient(t *testing.T, opts Options) (*Client, *versionRecorder) {
	t.Helper()
	rec := &versionRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.requests++
		rec.versions = r.Header.Values("Seclai-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	opts.APIKey, opts.BaseURL = "k", srv.URL
	c, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, rec
}

func assertRefused(t *testing.T, err error, rec *versionRecorder, mention string) {
	t.Helper()
	var cfg *ConfigurationError
	if !errors.As(err, &cfg) {
		t.Fatalf("got error %v, want a *ConfigurationError", err)
	}
	if !strings.Contains(cfg.Message, mention) {
		t.Fatalf("error %q does not mention %q", cfg.Message, mention)
	}
	if rec != nil && rec.requests != 0 {
		t.Fatalf("a request reached the server carrying %v", rec.versions)
	}
}

func assertSent(t *testing.T, err error, rec *versionRecorder, version string) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.requests != 1 || len(rec.versions) != 1 || rec.versions[0] != version {
		t.Fatalf("server saw %d request(s) with Seclai-Version %v, want one carrying %q", rec.requests, rec.versions, version)
	}
}

func TestDo_RefusesAnUnknownVersionInRequestHeaders(t *testing.T) {
	for _, key := range []string{"Seclai-Version", "seclai-version", "SECLAI-VERSION"} {
		c, rec := guardClient(t, Options{APIVersion: APIVersion20260727})
		err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, map[string]string{key: "2099-01-01"}, nil)
		assertRefused(t, err, rec, `unknown API version "2099-01-01"`)
	}
}

func TestDo_RefusesAnEmptyVersionInRequestHeaders(t *testing.T) {
	for _, value := range []string{"", "  "} {
		// Even with unknown versions allowed: an empty header is not a version.
		c, rec := guardClient(t, Options{APIVersion: APIVersion20260727, AllowUnknownAPIVersion: true})
		err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, map[string]string{"Seclai-Version": value}, nil)
		assertRefused(t, err, rec, "empty Seclai-Version")
	}
}

func TestDo_SendsAKnownVersionFromRequestHeaders(t *testing.T) {
	c, rec := guardClient(t, Options{APIVersion: APIVersion20260701})
	err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, map[string]string{"seclai-version": APIVersion20260727}, nil)
	assertSent(t, err, rec, APIVersion20260727)
}

func TestDo_SendsAnUnknownVersionWhenAllowed(t *testing.T) {
	c, rec := guardClient(t, Options{AllowUnknownAPIVersion: true})
	err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, map[string]string{"Seclai-Version": "2099-01-01"}, nil)
	assertSent(t, err, rec, "2099-01-01")
}

func TestDo_ValidatesTheSpellingThatIsSent(t *testing.T) {
	// Of two spellings the later one in sorted order is sent, every time.
	for i := 0; i < 20; i++ {
		c, rec := guardClient(t, Options{})
		err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil,
			map[string]string{"Seclai-Version": APIVersion20260727, "seclai-version": "2099-01-01"}, nil)
		assertRefused(t, err, rec, `Do headers["seclai-version"]`)

		c, rec = guardClient(t, Options{})
		err = c.Do(context.Background(), http.MethodGet, "/x", nil, nil,
			map[string]string{"Seclai-Version": "2099-01-01", "seclai-version": APIVersion20260727}, nil)
		assertSent(t, err, rec, APIVersion20260727)
	}
}

func TestDo_WithoutAVersionHeaderSendsTheConfiguredOne(t *testing.T) {
	c, rec := guardClient(t, Options{APIVersion: APIVersion20260727})
	err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, map[string]string{"X-Trace": "1"}, nil)
	assertSent(t, err, rec, APIVersion20260727)
}

func TestNewClient_RefusesAnEmptyVersionInDefaultHeaders(t *testing.T) {
	for _, key := range []string{"Seclai-Version", "seclai-version"} {
		for _, value := range []string{"", " "} {
			_, err := NewClient(Options{
				APIKey: "k", APIVersion: APIVersion20260727, AllowUnknownAPIVersion: true,
				DefaultHeaders: map[string]string{key: value},
			})
			assertRefused(t, err, nil, "empty Seclai-Version")
		}
	}
}

func generatedCall(c *Client, version string) error {
	resp, err := c.Generated().GetMeApiMeGet(context.Background(), &generated.GetMeApiMeGetParams{},
		func(_ context.Context, req *http.Request) error {
			req.Header.Set("Seclai-Version", version)
			return nil
		})
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestGenerated_RequestEditorVersionPassesTheGuard(t *testing.T) {
	c, rec := guardClient(t, Options{APIVersion: APIVersion20260727})
	assertRefused(t, generatedCall(c, "2099-01-01"), rec, `unknown API version "2099-01-01"`)

	c, rec = guardClient(t, Options{APIVersion: APIVersion20260727})
	assertRefused(t, generatedCall(c, ""), rec, "empty Seclai-Version")

	c, rec = guardClient(t, Options{APIVersion: APIVersion20260727})
	assertSent(t, generatedCall(c, APIVersion20260701), rec, APIVersion20260701)

	c, rec = guardClient(t, Options{AllowUnknownAPIVersion: true})
	assertSent(t, generatedCall(c, "2099-01-01"), rec, "2099-01-01")
}

func TestGenerated_WithoutAnEditorSendsTheConfiguredVersion(t *testing.T) {
	c, rec := guardClient(t, Options{APIVersion: APIVersion20260727})
	resp, err := c.Generated().GetMeApiMeGet(context.Background(), &generated.GetMeApiMeGetParams{})
	if err == nil {
		_ = resp.Body.Close()
	}
	assertSent(t, err, rec, APIVersion20260727)
}
