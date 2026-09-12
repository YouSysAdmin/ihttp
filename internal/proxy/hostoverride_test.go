package proxy_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	projectmodels "github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// overrideHost is a name that NEVER resolves - RFC 2606 reserves
// .invalid for exactly this. That is what makes these tests honest: a
// request to it can only be answered if the override was used, so a
// green test cannot be a DNS coincidence.
const overrideHost = "api.example.invalid"

// seeing is a target that reports what it was told it was, so a test
// can assert the name never moved with the connection.
func seeing(t *testing.T, tlsOn bool) *httptest.Server {
	t.Helper()

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sni := ""
		if r.TLS != nil {
			sni = r.TLS.ServerName
		}

		w.Header().Set("X-Seen-Host", r.Host)
		w.Header().Set("X-Seen-SNI", sni)
		_, _ = w.Write([]byte("reached"))
	})

	var srv *httptest.Server
	if tlsOn {
		srv = httptest.NewTLSServer(h)
	} else {
		srv = httptest.NewServer(h)
	}

	t.Cleanup(srv.Close)

	return srv
}

// override points host at where srv is actually listening.
func override(t *testing.T, stack *testutil.Stack, host string, srv *httptest.Server) {
	t.Helper()

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.HostOverrides = []projectmodels.HostOverride{
			{Host: host, Address: srv.Listener.Addr().String(), Enabled: true},
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestOverrideMovesTheConnectionAndNotTheName is the feature: the
// request reaches a machine DNS would never have named, and the target
// sees the host the client asked for.
func TestOverrideMovesTheConnectionAndNotTheName(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "hosts")

	target := seeing(t, false)
	override(t, stack, overrideHost, target)

	res, err := stack.Client().Get("http://" + overrideHost + "/thing")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)
	if string(body) != "reached" {
		t.Fatalf("body %q, so the override did not reach the target", body)
	}

	if got := res.Header.Get("X-Seen-Host"); got != overrideHost {
		t.Errorf("the target saw Host %q, want %q - the name moved with the connection", got, overrideHost)
	}
}

// TestOverrideKeepsTheSNI is what separates this from a rewrite_url
// rule: over TLS the certificate the target is asked for is still the
// one for the name the client typed.
func TestOverrideKeepsTheSNI(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "hosts")

	target := seeing(t, true)
	override(t, stack, overrideHost, target)

	res, err := stack.Client().Get("https://" + overrideHost + "/thing")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	if got := res.Header.Get("X-Seen-SNI"); got != overrideHost {
		t.Errorf("the target was asked for %q, want %q", got, overrideHost)
	}

	if got := res.Header.Get("X-Seen-Host"); got != overrideHost {
		t.Errorf("the target saw Host %q, want %q", got, overrideHost)
	}
}

// TestOverrideAppliesToAHostLeftAlone: an override that worked
// everywhere except on the hosts a project does not decrypt would be a
// trap, since those are the ones a person is least able to see into.
func TestOverrideAppliesToAHostLeftAlone(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "hosts")

	target := seeing(t, false)

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.NoDecrypt = []string{overrideHost}
		s.HostOverrides = []projectmodels.HostOverride{
			{Host: overrideHost, Address: target.Listener.Addr().String(), Enabled: true},
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Plain HTTP inside the tunnel: the bytes are relayed either way,
	// and this asserts the tunnel reached the overridden address.
	conn := stack.DialTunnel(t, overrideHost+":443", false)

	if _, err := io.WriteString(conn, "GET /thing HTTP/1.1\r\nHost: "+overrideHost+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}

	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	if got := res.Header.Get("X-Seen-Host"); got != overrideHost {
		t.Errorf("the target saw Host %q, want %q", got, overrideHost)
	}
}

// TestOverrideNamesItsOwnPort, so the same service on another port is
// reachable without touching the request.
func TestOverrideNamesItsOwnPort(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "hosts")

	target := seeing(t, false)
	_, port, err := net.SplitHostPort(target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	// The request asks for 80 and the override sends it somewhere else
	// entirely, which is the case a bare IP cannot cover.
	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.HostOverrides = []projectmodels.HostOverride{
			{Host: "*.example.invalid", Address: "127.0.0.1:" + port, Enabled: true},
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res, err := stack.Client().Get("http://" + overrideHost + "/thing")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)
	if string(body) != "reached" {
		t.Fatalf("body %q, so the wildcard override did not reach the target", body)
	}
}

// TestDisabledOverrideIsNotApplied. Turning one off is how a target is
// compared against the real host, so it has to actually stop.
func TestDisabledOverrideIsNotApplied(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "hosts")

	target := seeing(t, false)

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.HostOverrides = []projectmodels.HostOverride{
			{Host: overrideHost, Address: target.Listener.Addr().String(), Enabled: false},
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Nothing sends it anywhere now, and the name does not resolve, so
	// the proxy answers the failure rather than the target.
	res, err := stack.Client().Get("http://" + overrideHost + "/thing")
	if err == nil {
		defer func() { _ = res.Body.Close() }()

		if res.StatusCode < 500 {
			t.Fatalf("a disabled override still reached something: %s", res.Status)
		}
	}
}

// TestAnEditTakesEffectOnTheNextRequest. The upstream connection pool
// is keyed by the host a request named and NOT by the address dialled
// for it, so without dropping the idle ones an edit would be ignored
// for as long as a socket to the old address lived.
func TestAnEditTakesEffectOnTheNextRequest(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "hosts")

	first := answering(t, "first")
	second := answering(t, "second")

	override(t, stack, overrideHost, first)

	if got := getBody(t, stack, "http://"+overrideHost+"/"); got != "first" {
		t.Fatalf("reached %q, want first", got)
	}

	override(t, stack, overrideHost, second)

	if got := getBody(t, stack, "http://"+overrideHost+"/"); got != "second" {
		t.Fatalf("reached %q after the edit, want second - a pooled connection to the old address was reused", got)
	}
}

// answering is a target that says which one it is.
func answering(t *testing.T, name string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, name)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func getBody(t *testing.T, stack *testutil.Stack, url string) string {
	t.Helper()

	res, err := stack.Client().Get(url)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)

	return strings.TrimSpace(string(body))
}

// TestAnOverrideCanReachAPlainServer is the case a hosts file cannot
// cover and the one most people came for: a production name whose
// target is a dev server on plain HTTP. The client goes on asking for
// https, the proxy answers with its own certificate, and the hop to the
// target is plain.
func TestAnOverrideCanReachAPlainServer(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "hosts")

	target := seeing(t, false)

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.HostOverrides = []projectmodels.HostOverride{
			{Host: overrideHost, Address: "http://" + target.Listener.Addr().String(), Enabled: true},
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res, err := stack.Client().Get("https://" + overrideHost + "/thing")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)
	if string(body) != "reached" {
		t.Fatalf("body %q, so the plain target was not reached over https", body)
	}

	// The target spoke plain HTTP, so it was never asked for a name.
	if got := res.Header.Get("X-Seen-SNI"); got != "" {
		t.Errorf("the target saw SNI %q, so the hop was still TLS", got)
	}

	if got := res.Header.Get("X-Seen-Host"); got != overrideHost {
		t.Errorf("the target saw Host %q, want %q", got, overrideHost)
	}

	// What the CLIENT asked for is what the log records. Sending the
	// downgraded URL to the log would make the entry a lie and would
	// not replay.
	testutil.Eventually(t, 2*time.Second, func() bool {
		entries, _, err := stack.ReqLogs.List(t.Context(), reqlogList())

		return err == nil && len(entries) == 1
	}, "the exchange was logged")

	entries, _, err := stack.ReqLogs.List(t.Context(), reqlogList())
	if err != nil {
		t.Fatal(err)
	}

	if got := entries[0].URL; got != "https://"+overrideHost+"/thing" {
		t.Errorf("the log recorded %q, want the https URL the client asked for", got)
	}
}
