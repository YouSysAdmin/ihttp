package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	// Aliased: this file already has an upstream() helper that makes a
	// target server, which is a different thing entirely.
	upstreamproxy "github.com/yousysadmin/ihttp/internal/core/upstream"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	projectmodels "github.com/yousysadmin/ihttp/internal/models/project"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/proxy"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// upstream is a target that echoes what it saw.
func upstream(t *testing.T, tlsOn bool) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Seen-Method", r.Method)
		w.Header().Set("X-Seen-Header", r.Header.Get("X-Test"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("echo:" + string(body)))
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

func TestPlainAndTunneledRequestsAreProxiedAndLogged(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "t")
	client := stack.Client()

	for _, tlsOn := range []bool{false, true} {
		target := upstream(t, tlsOn)

		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, target.URL+"/path?x=1", strings.NewReader("hello"))
		req.Header.Set("X-Test", "yes")

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("tls=%v: %v", tlsOn, err)
		}

		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if res.StatusCode != http.StatusAccepted || string(body) != "echo:hello" || res.Header.Get("X-Seen-Header") != "yes" {
			t.Fatalf("tls=%v: status %d body %q headers %v", tlsOn, res.StatusCode, body, res.Header)
		}
	}

	// The log catches up asynchronously on the response half.
	deadline := time.Now().Add(3 * time.Second)
	for {
		entries, _, err := stack.ReqLogs.List(t.Context(), reqlogList())
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) == 2 && entries[0].StatusCode == 202 && entries[1].StatusCode == 202 {
			if entries[0].Method != http.MethodPost || !strings.Contains(entries[0].URL, "/path?x=1") {
				t.Fatalf("unexpected summary %+v", entries[0])
			}

			full, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
			if err != nil {
				t.Fatal(err)
			}

			if string(full.Body) != "hello" || string(full.Response.Body) != "echo:hello" || full.Response.Headers.Get("X-Seen-Method") != "POST" {
				t.Fatalf("stored entry incomplete: %+v", full)
			}

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("log did not fill: %+v", entries)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func TestNothingIsLoggedWithoutAProject(t *testing.T) {
	stack := testutil.New(t)
	target := upstream(t, false)

	res, err := stack.Client().Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	stack.OpenProject(t, "later")

	entries, _, err := stack.ReqLogs.List(t.Context(), reqlogList())
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected an empty log, got %v %v", entries, err)
	}
}

func TestLandingPageAndCertificate(t *testing.T) {
	stack := testutil.New(t)

	res, err := http.Get(stack.Proxy.URL + "/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	pem, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.HasPrefix(string(pem), "-----BEGIN CERTIFICATE-----") {
		t.Fatalf("status %d body %q", res.StatusCode, pem)
	}

	res, err = http.Get(stack.Proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	page, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(page), "ihttp proxy") {
		t.Fatalf("landing page missing: %q", page)
	}
}

// The phases are measured and stored, the server's own delay shows up
// as the wait, and a second request on the kept-alive connection reports
// itself reused with no connection phases at all - which is the
// difference the console has to show rather than drawing three instant
// bars.
func TestTimingsAreMeasuredAndStored(t *testing.T) {
	const delay = 60 * time.Millisecond

	stack := testutil.New(t)
	stack.OpenProject(t, "timing")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(delay)
		_, _ = w.Write([]byte("body"))
	}))
	t.Cleanup(target.Close)

	client := stack.Client()

	for range 2 {
		res, err := client.Get(target.URL + "/timed")
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.ReadAll(res.Body)
		_ = res.Body.Close()
	}

	var entries []reqlogmodels.Summary

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlogList())

		return len(entries) == 2 && entries[0].StatusCode != 0 && entries[1].StatusCode != 0
	}, "both exchanges are logged")

	// Newest first, so the reused one is entries[0].
	second, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := stack.ReqLogs.Get(t.Context(), entries[1].ID)
	if err != nil {
		t.Fatal(err)
	}

	ft, st := first.Response.Timings, second.Response.Timings
	if ft == nil || st == nil {
		t.Fatalf("timings were not stored: first %v second %v", ft, st)
	}

	if ft.Wait < float64(delay/time.Millisecond) {
		t.Errorf("wait = %.3f ms, want at least %d", ft.Wait, delay/time.Millisecond)
	}

	if ft.Reused {
		t.Error("the first request reported a reused connection")
	}

	if ft.Connect <= 0 {
		t.Errorf("connect = %.3f ms on a new connection, want more than zero", ft.Connect)
	}

	if ft.ServerAddr == "" {
		t.Error("the upstream address was not recorded")
	}

	// A plain HTTP upstream has no TLS phase and nothing to negotiate.
	if ft.TLS != 0 || ft.TLSVersion != "" {
		t.Errorf("a plain upstream reported TLS: %.3f ms %q", ft.TLS, ft.TLSVersion)
	}

	// TTFB is derived from the phases, so it can never disagree with
	// them, and the whole duration must not be shorter than it.
	if ft.TTFB() < ft.Wait || float64(first.Response.DurationMS)+1 < ft.TTFB() {
		t.Errorf("ttfb %.3f does not sit between wait %.3f and duration %d", ft.TTFB(), ft.Wait, first.Response.DurationMS)
	}

	if !st.Reused {
		t.Error("the second request did not report the connection as reused")
	}

	if st.DNS != 0 || st.Connect != 0 || st.TLS != 0 {
		t.Errorf("a reused connection reported connection phases: dns %.3f connect %.3f tls %.3f", st.DNS, st.Connect, st.TLS)
	}
}

// A TLS upstream records what was negotiated with it, which the log had
// nowhere to put before.
func TestTimingsRecordTheUpstreamTLS(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "tls")

	target := upstream(t, true)

	res, err := stack.Client().Get(target.URL + "/secure")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	var entries []reqlogmodels.Summary

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlogList())

		return len(entries) == 1 && entries[0].StatusCode != 0
	}, "the exchange is logged")

	full, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	tm := full.Response.Timings
	if tm == nil {
		t.Fatal("timings were not stored")
	}

	if tm.TLS <= 0 || !strings.HasPrefix(tm.TLSVersion, "TLS") {
		t.Errorf("tls phase %.3f ms, version %q", tm.TLS, tm.TLSVersion)
	}
}

// A response a rule answered locally never reaches the transport, so it
// is stored as not measured rather than as a row of zeros.
func TestMockedResponseIsStoredAsNotMeasured(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "mocked")

	_, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.Rules = []projectmodels.Rule{{
			Enabled: true,
			Name:    "mock",
			URL:     ".*",
			Action: projectmodels.RuleAction{
				Type:   projectmodels.ActionMock,
				Status: http.StatusTeapot,
				Body:   "mocked",
			},
		}}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := stack.Client().Get("http://mocked.invalid/anything")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusTeapot {
		t.Fatalf("status %d, want 418", res.StatusCode)
	}

	var entries []reqlogmodels.Summary

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlogList())

		return len(entries) == 1 && entries[0].StatusCode != 0
	}, "the mocked exchange is logged")

	full, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if full.Response.Timings != nil {
		t.Fatalf("a mocked response carries timings: %+v", full.Response.Timings)
	}
}

// A second ihttp as the upstream proxy: what the front one forwards
// leaves through the back one, and both logs see it. A bypassed host
// goes out directly instead, which is visible because it then has to
// resolve and cannot.
//
// The back proxy mocks every request, so nothing in this test depends on
// reaching a real network or on what DNS says.
func TestUpstreamProxyChainsAndBypasses(t *testing.T) {
	back := testutil.New(t)
	back.OpenProject(t, "upstream")

	_, err := back.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.Rules = []projectmodels.Rule{{
			Enabled: true,
			Name:    "answer everything",
			Action: projectmodels.RuleAction{
				Type:   projectmodels.ActionMock,
				Status: http.StatusTeapot,
				Body:   "from the upstream proxy",
			},
		}}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := upstreamproxy.Config{URL: back.Proxy.URL, Bypass: []string{"direct.invalid"}}

	via, err := cfg.Compile()
	if err != nil {
		t.Fatal(err)
	}

	front := testutil.New(t,
		testutil.WithProxy(func(o *proxy.Options) { o.UpstreamProxy = via }),
		testutil.WithSender(func(o *sender.Options) { o.UpstreamProxy = via }),
	)
	front.OpenProject(t, "front")

	// Through: the front proxy hands it to the back one, which answers.
	res, err := front.Client().Get("http://through.invalid/x")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != http.StatusTeapot || string(body) != "from the upstream proxy" {
		t.Fatalf("through the chain: %d %q", res.StatusCode, body)
	}

	// Bypassed: the front proxy goes out on its own, and .invalid does
	// not resolve, so it cannot succeed - which is the proof it did not
	// use the proxy that would have mocked it.
	res, err = front.Client().Get("http://direct.invalid/x")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("a bypassed host answered %d, want a 502 from failing to resolve", res.StatusCode)
	}

	// Both logs saw the chained exchange, and only the front one saw the
	// bypassed attempt.
	testutil.Eventually(t, 3*time.Second, func() bool {
		frontEntries, _, _ := front.ReqLogs.List(t.Context(), reqlogList())
		backEntries, _, _ := back.ReqLogs.List(t.Context(), reqlogList())

		return len(frontEntries) == 2 && len(backEntries) == 1
	}, "the front log has both and the back log has the chained one")

	backEntries, _, err := back.ReqLogs.List(t.Context(), reqlogList())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(backEntries[0].URL, "through.invalid") {
		t.Errorf("the upstream proxy logged %q", backEntries[0].URL)
	}

	// The sender leaves the same way, which is the point of giving it
	// the same configuration: a replay behaves like the original.
	saved, err := front.Sender.Save(t.Context(), sender.SaveRequest{URL: "http://through.invalid/replayed"})
	if err != nil {
		t.Fatal(err)
	}

	sent, err := front.Sender.Send(t.Context(), saved.ID)
	if err != nil {
		t.Fatal(err)
	}

	if sent.Response == nil || sent.Response.StatusCode != http.StatusTeapot {
		t.Fatalf("the sender did not go through the upstream proxy: %+v", sent.Response)
	}
}

// The probe host is answered by the proxy itself and IS logged, which is
// what makes it worth having: it proves the whole path a person cares
// about - their client reached the proxy and the exchange turned up in
// the log - with no network and no real target.
func TestProbeIsAnsweredAndLogged(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "probe")

	const token = "tok-abc123"

	res, err := stack.Client().Get("http://" + proxy.DefaultProbeHost + "/v/" + token)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("the probe answered %d, want 200", res.StatusCode)
	}

	if res.Header.Get("X-Ihttp-Probe") != "1" {
		t.Error("the answer does not say it is a probe")
	}

	// The command's own output has to be readable by the person running
	// it, and has to say nothing left the machine.
	for _, want := range []string{"ihttp saw this request", token, "does not resolve"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the probe body does not mention %q: %q", want, body)
		}
	}

	// And it is in the log, findable by the token alone. The response
	// half is written off the request path, so wait for it rather than
	// for the row.
	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlogListSearch("req.url contains "+token))

		return len(entries) == 1 && entries[0].StatusCode != 0
	}, "the probe is in the log with its answer")

	entries, _, err := stack.ReqLogs.List(t.Context(), reqlogListSearch("req.url contains "+token))
	if err != nil {
		t.Fatal(err)
	}

	if entries[0].StatusCode != http.StatusOK {
		t.Errorf("the logged probe has status %d", entries[0].StatusCode)
	}

	// It never reached the network, so it carries no timings - the same
	// as a mocked response, and for the same reason.
	full, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if full.Response.Timings != nil {
		t.Errorf("the probe was measured, so it went somewhere: %+v", full.Response.Timings)
	}
}

// Nothing else is touched: a host that merely looks similar goes
// upstream as usual.
func TestOnlyTheProbeHostIsAnswered(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "probe-narrow")

	target := upstream(t, false)

	res, err := stack.Client().Get(target.URL + "/ihttp.probe/not-really")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.Header.Get("X-Ihttp-Probe") != "" || !strings.HasPrefix(string(body), "echo:") {
		t.Errorf("a path that mentions the probe host was answered locally: %d %q", res.StatusCode, body)
	}
}
