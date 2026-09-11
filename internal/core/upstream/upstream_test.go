package upstream_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/upstream"
)

func proxyFor(t *testing.T, c upstream.Config, target string) string {
	t.Helper()

	fn, err := c.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}

	u, err := fn(req)
	if err != nil {
		t.Fatalf("proxy for %s: %v", target, err)
	}

	if u == nil {
		return ""
	}

	return u.String()
}

func TestBypassPatterns(t *testing.T) {
	c := upstream.Config{
		URL:    "http://proxy.corp:3128",
		Bypass: []string{"api.internal", "*.example.com", "?ost.local"},
	}

	cases := map[string]string{
		// Through the proxy.
		"https://github.com/x":    "http://proxy.corp:3128",
		"http://example.com/":     "http://proxy.corp:3128",
		"https://notexample.com/": "http://proxy.corp:3128",

		// Direct.
		"http://api.internal/v1":    "",
		"https://a.example.com/":    "",
		"https://a.b.example.com/":  "",
		"http://host.local/":        "",
		"http://localhost:3000/":    "",
		"http://127.0.0.1:6081/api": "",
		"http://[::1]:6081/api":     "",
		// The port is not part of the match.
		"https://api.internal:8443/x": "",
		// Case is not either.
		"https://API.Example.COM/": "",
	}

	for target, want := range cases {
		if got := proxyFor(t, c, target); got != want {
			t.Errorf("%s went through %q, want %q", target, got, want)
		}
	}
}

// An empty configuration must leave the environment in charge, so an
// instance that relied on HTTPS_PROXY keeps working.
func TestNoConfigFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://from-env:6080")
	t.Setenv("NO_PROXY", "")

	if got := proxyFor(t, upstream.Config{}, "https://example.com/"); got != "http://from-env:6080" {
		t.Errorf("proxy = %q, want the environment's", got)
	}
}

func TestParseRefusesWhatCannotWork(t *testing.T) {
	bad := []string{
		"proxy.corp:3128",          // no scheme
		"http://",                  // no host
		"ftp://proxy.corp:3128",    // not a proxy scheme
		"socks4://proxy.corp:1080", // nor that one
	}

	for _, raw := range bad {
		if _, err := upstream.Parse(raw); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}

	for _, raw := range []string{
		"http://proxy.corp:3128",
		"https://proxy.corp:3128",
		"socks5://proxy.corp:1080",
		"socks5h://proxy.corp:1080",
		"http://user:secret@proxy.corp:3128",
	} {
		if _, err := upstream.Parse(raw); err != nil {
			t.Errorf("%q was refused: %v", upstream.Redact(raw), err)
		}
	}

	// A bad bypass glob is reported rather than silently matching
	// nothing.
	if _, err := (upstream.Config{URL: "http://proxy:3128", Bypass: []string{"[a-"}}).Compile(); err == nil {
		t.Error("a broken bypass pattern was accepted")
	}
}

// A password must never reach a log line or the console.
func TestRedact(t *testing.T) {
	cases := map[string]string{
		"http://user:secret@proxy.corp:3128": "http://user:xxxxx@proxy.corp:3128",
		"http://user@proxy.corp:3128":        "http://user@proxy.corp:3128",
		"http://proxy.corp:3128":             "http://proxy.corp:3128",
	}

	for raw, want := range cases {
		if got := upstream.Redact(raw); got != want {
			t.Errorf("Redact(%q) = %q, want %q", raw, got, want)
		}

		if strings.Contains(upstream.Redact(raw), "secret") {
			t.Errorf("Redact(%q) leaked the password", raw)
		}
	}
}

// The test action reports what it found, including that a bypassed
// target proves nothing about the proxy.
func TestTestConnection(t *testing.T) {
	var sawProxyRequest bool

	// A stand-in upstream proxy: it answers an absolute-form request the
	// way a forward proxy does.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.IsAbs() {
			sawProxyRequest = true
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer fake.Close()

	c := upstream.Config{URL: fake.URL, Bypass: []string{"direct.example"}}

	res, err := c.Test(t.Context(), "http://through.example/")
	if err != nil {
		t.Fatalf("test: %v", err)
	}

	if res.Direct {
		t.Error("a target not on the bypass list was reported as direct")
	}

	if res.Status != http.StatusNoContent || !sawProxyRequest {
		t.Errorf("status %d, proxy saw an absolute request: %v", res.Status, sawProxyRequest)
	}

	if res.Target != "through.example" {
		t.Errorf("target %q", res.Target)
	}

	// A bypassed target does not touch the proxy, and the answer says so
	// rather than reporting a success the proxy had no part in.
	res, err = c.Test(t.Context(), "http://direct.example/")
	if err == nil && !res.Direct {
		t.Error("a bypassed target was not reported as direct")
	}

	if !res.Direct {
		t.Errorf("direct = false for a bypassed target: %+v", res)
	}

	// Nothing configured is a refusal, not a silent success.
	if _, err := (upstream.Config{}).Test(t.Context(), ""); err == nil {
		t.Error("testing with no proxy configured succeeded")
	}
}
