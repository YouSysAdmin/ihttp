package rules_test

import (
	"fmt"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

func setRules(t *testing.T, stack *testutil.Stack, rules ...project.Rule) {
	t.Helper()

	for i := range rules {
		rules[i].Enabled = true
	}

	_, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.Rules = rules

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, stack *testutil.Stack, url string, headers map[string]string) (*http.Response, string) {
	t.Helper()

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := stack.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)

	return res, string(body)
}

func TestRulesShapeTraffic(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "rules")

	hits := 0
	seen := http.Header{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		seen = r.Header.Clone()
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer target.Close()

	local := filepath.Join(t.TempDir(), "mock.json")
	if err := os.WriteFile(local, []byte(`{"local":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	setRules(t, stack,
		project.Rule{Name: "mock", URL: `/mocked$`, Action: project.RuleAction{Type: project.ActionMock, Status: 418, Body: `{"mock":true}`}},
		project.Rule{Name: "local", URL: `/local$`, Action: project.RuleAction{Type: project.ActionMapLocal, Path: local}},
		project.Rule{Name: "missing", URL: `/missing$`, Action: project.RuleAction{Type: project.ActionMapLocal, Path: filepath.Join(t.TempDir(), "nope")}},
		project.Rule{Name: "req header", URL: `/hdr`, Action: project.RuleAction{Type: project.ActionSetRequestHeader, Header: "X-Added", Value: "1"}},
		project.Rule{Name: "res header", URL: `/hdr`, Action: project.RuleAction{Type: project.ActionSetResponseHeader, Header: "X-Res", Value: "2"}},
		project.Rule{Name: "drop res header", URL: `/hdr`, Action: project.RuleAction{Type: project.ActionRemoveResponseHeader, Header: "X-Upstream"}},
		project.Rule{Name: "status", URL: `/status`, Action: project.RuleAction{Type: project.ActionSetStatus, Status: 503}},
		project.Rule{Name: "body", URL: `/body`, Action: project.RuleAction{Type: project.ActionReplaceBody, Pattern: `world`, Replace: `there`}},
		project.Rule{Name: "cors", URL: `/cors`, Action: project.RuleAction{Type: project.ActionAllowCORS}},
		project.Rule{Name: "only post", URL: `/onlypost`, Method: "POST", Action: project.RuleAction{Type: project.ActionSetStatus, Status: 201}},
	)

	res, body := get(t, stack, target.URL+"/mocked", nil)
	if res.StatusCode != 418 || body != `{"mock":true}` || res.Header.Get("Content-Type") != "application/json" || hits != 0 {
		t.Fatalf("mock: %d %q %s hits=%d", res.StatusCode, body, res.Header.Get("Content-Type"), hits)
	}

	res, body = get(t, stack, target.URL+"/local", nil)
	if res.StatusCode != 200 || body != `{"local":true}` || !strings.Contains(res.Header.Get("Content-Type"), "json") || hits != 0 {
		t.Fatalf("map local: %d %q %s", res.StatusCode, body, res.Header.Get("Content-Type"))
	}

	res, body = get(t, stack, target.URL+"/missing", nil)
	if res.StatusCode != 404 || !strings.Contains(body, "map local") {
		t.Fatalf("missing local file: %d %q", res.StatusCode, body)
	}

	res, _ = get(t, stack, target.URL+"/hdr", nil)
	if seen.Get("X-Added") != "1" || res.Header.Get("X-Res") != "2" || res.Header.Get("X-Upstream") != "" {
		t.Fatalf("headers: upstream saw %v, client got %v", seen, res.Header)
	}

	res, _ = get(t, stack, target.URL+"/status", nil)
	if res.StatusCode != 503 {
		t.Fatalf("status: %d", res.StatusCode)
	}

	_, body = get(t, stack, target.URL+"/body", nil)
	if body != "hello there" {
		t.Fatalf("replace body: %q", body)
	}

	res, _ = get(t, stack, target.URL+"/cors", map[string]string{"Origin": "http://app.local"})
	if res.Header.Get("Access-Control-Allow-Origin") != "http://app.local" || res.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("cors: %v", res.Header)
	}

	before := hits
	req, _ := http.NewRequest(http.MethodOptions, target.URL+"/cors", nil)
	req.Header.Set("Origin", "http://app.local")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "authorization")
	res, err := stack.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Headers") != "authorization" || hits != before {
		t.Fatalf("preflight: %d %v hits=%d", res.StatusCode, res.Header, hits)
	}

	res, _ = get(t, stack, target.URL+"/onlypost", nil)
	if res.StatusCode != 200 {
		t.Fatalf("method-narrowed rule applied to GET: %d", res.StatusCode)
	}

	// The mock was logged like any exchange.
	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Search: `req.url =~ "/mocked$"`, Limit: 5})

		return len(entries) == 1 && entries[0].StatusCode == 418
	}, "mock logged")
}

func TestRewriteURLAndDelay(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "rewrite")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("from " + r.Host + r.URL.Path))
	}))
	defer target.Close()

	setRules(t, stack,
		project.Rule{Name: "map remote", URL: `^http://api\.example\.test/`, Action: project.RuleAction{
			Type: project.ActionRewriteURL, Pattern: `^http://api\.example\.test`, Replace: target.URL,
		}},
		project.Rule{Name: "slow", URL: `/slow`, Action: project.RuleAction{Type: project.ActionDelay, DelayMS: 300}},
	)

	_, body := get(t, stack, "http://api.example.test/v1/users", nil)
	if !strings.HasPrefix(body, "from 127.0.0.1") || !strings.HasSuffix(body, "/v1/users") {
		t.Fatalf("rewrite: %q", body)
	}

	started := time.Now()
	get(t, stack, target.URL+"/slow", nil)
	if time.Since(started) < 300*time.Millisecond {
		t.Fatal("delay rule did not wait")
	}
}

func TestBadRulesAreRefused(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "bad")

	bad := []project.Rule{
		{Name: "regex", Enabled: true, URL: `[`, Action: project.RuleAction{Type: project.ActionSetStatus, Status: 200}},
		{Name: "status", Enabled: true, Action: project.RuleAction{Type: project.ActionSetStatus, Status: 7}},
		{Name: "path", Enabled: true, Action: project.RuleAction{Type: project.ActionMapLocal}},
		{Name: "noheaders", Enabled: true, Action: project.RuleAction{Type: project.ActionSetRequestHeader}},
		{Name: "nopattern", Enabled: true, Action: project.RuleAction{Type: project.ActionReplaceRequestBody}},
		{Name: "norewrite", Enabled: true, Action: project.RuleAction{Type: project.ActionRewriteURL, Replace: "x"}},
		{Name: "unknown", Enabled: true, Action: project.RuleAction{Type: "teleport"}},
	}

	for _, r := range bad {
		_, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
			s.Rules = []project.Rule{r}

			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "rule "+r.Name) {
			t.Fatalf("rule %s: expected a refusal naming it, got %v", r.Name, err)
		}
	}
}

// Every matched rule applies, whatever its position: a mock placed before
// a header rule does not end the chain.
func TestAnsweringRuleDoesNotCutTheChain(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "chain")

	setRules(t, stack,
		project.Rule{Name: "mock", URL: `/m$`, Action: project.RuleAction{Type: project.ActionMock, Status: 418, Body: `ok`}},
		project.Rule{Name: "req header", URL: `/m$`, Action: project.RuleAction{Type: project.ActionSetRequestHeader, Header: "X-Added", Value: "1"}},
		project.Rule{Name: "res header", URL: `/m$`, Action: project.RuleAction{Type: project.ActionSetResponseHeader, Header: "X-Res", Value: "2"}},
		project.Rule{Name: "second mock", URL: `/m$`, Action: project.RuleAction{Type: project.ActionMock, Status: 500, Body: `late`}},
	)

	res, body := get(t, stack, "http://example.test/m", nil)
	if res.StatusCode != 418 || body != "ok" || res.Header.Get("X-Res") != "2" {
		t.Fatalf("first mock with the header rule applied: %d %q %v", res.StatusCode, body, res.Header)
	}

	var entries []reqlogmodels.Summary
	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlog.ListParams{Search: `req.url =~ "/m$"`, Limit: 5})

		return len(entries) == 1 && entries[0].StatusCode != 0
	}, "exchange logged")

	e, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if e.Headers.Get("X-Added") != "1" || e.Response.Headers.Get("X-Res") != "2" {
		t.Fatalf("logged exchange: req %v res %v", e.Headers, e.Response.Headers)
	}
}

func TestListsAndRequestBody(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "lists")

	seen := http.Header{}
	gotBody := ""
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("X-One", "1")
		w.Header().Set("X-Two", "2")
		_, _ = w.Write([]byte("alpha beta gamma"))
	}))
	defer target.Close()

	setRules(t, stack,
		project.Rule{Name: "set many", URL: `/many`, Action: project.RuleAction{Type: project.ActionSetRequestHeader, Headers: []project.Header{{Name: "X-A", Value: "a"}, {Name: "X-B", Value: "b"}}}},
		project.Rule{Name: "drop many", URL: `/many`, Action: project.RuleAction{Type: project.ActionRemoveResponseHeader, Headers: []project.Header{{Name: "X-One"}, {Name: "X-Two"}}}},
		project.Rule{Name: "res body", URL: `/many`, Action: project.RuleAction{Type: project.ActionReplaceBody, Replacements: []project.Replacement{{Pattern: `alpha`, Replace: `A`}, {Pattern: `(gam)ma`, Replace: `$1$1`}}}},
		project.Rule{Name: "req body", URL: `/many`, Action: project.RuleAction{Type: project.ActionReplaceRequestBody, Replacements: []project.Replacement{{Pattern: `"login":"user"`, Replace: `"login":"admin"`}}}},
		// The rule's URL match doubles as the rewrite pattern.
		project.Rule{Name: "rewrite by match", URL: `^http://legacy\.example\.test`, Action: project.RuleAction{Type: project.ActionRewriteURL, Replace: target.URL}},
	)

	req, _ := http.NewRequest(http.MethodPost, target.URL+"/many", strings.NewReader(`{"login":"user"}`))
	res, err := stack.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if seen.Get("X-A") != "a" || seen.Get("X-B") != "b" {
		t.Fatalf("set many: upstream saw %v", seen)
	}

	if gotBody != `{"login":"admin"}` || seen.Get("Content-Length") != "17" {
		t.Fatalf("request body: %q, Content-Length %q", gotBody, seen.Get("Content-Length"))
	}

	if res.Header.Get("X-One") != "" || res.Header.Get("X-Two") != "" {
		t.Fatalf("drop many: client got %v", res.Header)
	}

	if string(body) != "A beta gamgam" {
		t.Fatalf("response replacements: %q", body)
	}

	_, body2 := get(t, stack, "http://legacy.example.test/many", nil)
	if body2 != "A beta gamgam" {
		t.Fatalf("rewrite by match: %q", body2)
	}
}

func TestLegacySingleFieldsStillLoad(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "legacy")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Old", r.Header.Get("X-Legacy"))
		_, _ = w.Write([]byte("hello world"))
	}))
	defer target.Close()

	setRules(t, stack,
		project.Rule{Name: "old header", Action: project.RuleAction{Type: project.ActionSetRequestHeader, Header: "X-Legacy", Value: "yes"}},
		project.Rule{Name: "old body", Action: project.RuleAction{Type: project.ActionReplaceBody, Pattern: `world`, Replace: `there`}},
	)

	res, body := get(t, stack, target.URL+"/", nil)
	if res.Header.Get("X-Old") != "yes" || body != "hello there" {
		t.Fatalf("legacy rules: %v %q", res.Header, body)
	}

	// Stored in the current shape after the save.
	saved := stack.Projects.Active().Project.Settings.Rules
	if saved[0].Action.Header != "" || len(saved[0].Action.Headers) != 1 || saved[1].Action.Pattern != "" || len(saved[1].Action.Replacements) != 1 {
		t.Fatalf("not normalized: %+v", saved)
	}
}

// A rule may carry a filter query, which is what makes it as targetable
// as an intercept filter without a second matching language. It narrows
// further: the URL, the method and the filter all have to agree.
func TestRuleMatchesByFilter(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "rule-filter")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("upstream"))
	}))
	t.Cleanup(target.Close)

	setRules(t, stack, project.Rule{
		Name:   "mock the authorised writes",
		Filter: `req.method = POST AND req.header.authorization exists`,
		Action: project.RuleAction{
			Type:   project.ActionMock,
			Status: http.StatusTeapot,
			Body:   "mocked",
		},
	})

	send := func(method string, withAuth bool, body string) (int, string) {
		t.Helper()

		req, err := http.NewRequestWithContext(t.Context(), method, target.URL+"/x", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}

		if withAuth {
			req.Header.Set("Authorization", "Bearer x")
		}

		res, err := stack.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}

		out, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		return res.StatusCode, string(out)
	}

	if code, body := send(http.MethodPost, true, "hello"); code != http.StatusTeapot || body != "mocked" {
		t.Errorf("an authorised POST got %d %q, want the mock", code, body)
	}

	// Every clause has to agree, so each of these reaches the upstream.
	if code, body := send(http.MethodPost, false, "hello"); code != http.StatusOK || body != "upstream" {
		t.Errorf("a POST without the header got %d %q, want the upstream", code, body)
	}

	if code, body := send(http.MethodGet, true, ""); code != http.StatusOK || body != "upstream" {
		t.Errorf("an authorised GET got %d %q, want the upstream", code, body)
	}
}

// A filter over the request body works too, which the URL regex could
// never do.
func TestRuleFilterReadsTheRequestBody(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "rule-body-filter")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("upstream"))
	}))
	t.Cleanup(target.Close)

	setRules(t, stack, project.Rule{
		Name:   "block the dangerous payload",
		Filter: `req.body contains "DROP TABLE"`,
		Action: project.RuleAction{Type: project.ActionBlock, Status: http.StatusForbidden},
	})

	post := func(body string) (int, string) {
		t.Helper()

		res, err := stack.Client().Post(target.URL+"/q", "text/plain", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}

		out, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		return res.StatusCode, string(out)
	}

	code, body := post("select 1; drop table users")
	if code != http.StatusForbidden {
		t.Fatalf("the payload was not blocked: %d %q", code, body)
	}

	if !strings.Contains(body, "blocked by ihttp rule") {
		t.Errorf("the block says nothing about which rule did it: %q", body)
	}

	// The body must still reach the upstream when the filter does not
	// match, which is the proof that reading it for the filter put it
	// back.
	if code, body := post("select 1"); code != http.StatusOK || body != "upstream" {
		t.Errorf("an innocent payload got %d %q, want the upstream", code, body)
	}
}

// A filter that names a response field is refused when the rule is
// saved: a rule runs before the response exists, so res.* could only
// ever answer nothing.
func TestRuleFilterRefusesResponseFields(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "rule-filter-bad")

	bad := map[string]string{
		"a response field":            `res.statusCode >= 500`,
		"a websocket field":           `ws.messages > 1`,
		"a field that does not exist": `req.nothing = 1`,
		"a query that does not parse": `req.method =`,
	}

	for what, query := range bad {
		_, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
			s.Rules = []project.Rule{{
				Enabled: true,
				Name:    "bad",
				Filter:  query,
				Action:  project.RuleAction{Type: project.ActionBlock},
			}}

			return nil
		})
		if err == nil {
			t.Errorf("%s was accepted: %s", what, query)
		}
	}
}

// Block answers instead of the upstream, and the exchange is logged like
// any other so the operator can see what they arranged.
func TestBlockRule(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "block")
	ctx := t.Context()

	var hits int

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("upstream"))
	}))
	t.Cleanup(target.Close)

	setRules(t, stack, project.Rule{
		Name:   "no analytics",
		URL:    `/analytics`,
		Action: project.RuleAction{Type: project.ActionBlock},
	})

	res, err := stack.Client().Get(target.URL + "/analytics/collect")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("status %d, want the default 502", res.StatusCode)
	}

	if hits != 0 {
		t.Errorf("the upstream was reached %d times", hits)
	}

	// Anything else still goes through.
	res, err = stack.Client().Get(target.URL + "/ok")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK || hits != 1 {
		t.Errorf("an unmatched request got %d after %d upstream hits", res.StatusCode, hits)
	}

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})

		return len(entries) == 2
	}, "both exchanges are logged, the blocked one too")
}

// Throttle paces the body rather than waiting before it, so the client
// takes about as long as the rate says.
func TestThrottleRule(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "throttle")

	const size = 8 << 10

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, size))
	}))
	t.Cleanup(target.Close)

	setRules(t, stack, project.Rule{
		Name:   "slow link",
		Action: project.RuleAction{Type: project.ActionThrottle, RateBPS: size / 2},
	})

	start := time.Now()

	res, err := stack.Client().Get(target.URL + "/big")
	if err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()

	took := time.Since(start)

	if err != nil || len(body) != size {
		t.Fatalf("read %d bytes of %d (%v)", len(body), size, err)
	}

	// Two seconds' worth at the rate. A generous floor, since the point
	// is that it was paced at all rather than the exact number.
	if took < 1200*time.Millisecond {
		t.Errorf("took %s for %d bytes at %d B/s, want at least about 2s", took, size, size/2)
	}
}

// delay_response holds the answer back, where delay holds the request.
func TestDelayResponseRule(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "delay-response")

	var reached time.Time

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = time.Now()
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	setRules(t, stack, project.Rule{
		Name:   "slow answer",
		Action: project.RuleAction{Type: project.ActionDelayResponse, DelayMS: 400},
	})

	start := time.Now()

	res, err := stack.Client().Get(target.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	// The upstream was reached promptly - that is the difference from
	// delay - and the client waited.
	if reached.Sub(start) > 300*time.Millisecond {
		t.Errorf("the upstream was reached after %s, so the request was held back too", reached.Sub(start))
	}

	if took := time.Since(start); took < 400*time.Millisecond {
		t.Errorf("the client waited %s, want at least 400ms", took)
	}
}

// Throttle is the one action that reaches a STREAMED response. Such a
// body never arrives at a hook and the live stream is put back after
// them, so the pacing goes through the exchange's wrapper instead - and
// this is the test that the claim is true rather than plausible.
func TestThrottleReachesAStreamedResponse(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "throttle-stream")

	const events = 8

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// An event stream: the proxy relays it as it arrives and never
		// captures the body.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		for i := range events {
			_, _ = fmt.Fprintf(w, "data: event-%d-padding-padding-padding\n\n", i)

			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(target.Close)

	// Small enough that the pacing is unmistakable against the time an
	// unthrottled stream takes, which is milliseconds.
	setRules(t, stack, project.Rule{
		Name:   "trickle",
		Action: project.RuleAction{Type: project.ActionThrottle, RateBPS: 128},
	})

	start := time.Now()

	res, err := stack.Client().Get(target.URL + "/feed")
	if err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()

	took := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), "data: event-0") || !strings.Contains(string(body), "data: event-7") {
		t.Fatalf("the stream did not arrive whole: %q", body)
	}

	// The log has to agree that this was a stream, or the test proves
	// nothing about the streamed path.
	entries, _, err := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 5})

		return len(entries) == 1 && entries[0].StatusCode != 0
	}, "the exchange is logged")

	if !entries[0].Streamed {
		t.Fatal("the response was not streamed, so this test is about the buffered path")
	}

	// At 128 B/s the payload takes about two seconds. A generous floor.
	want := time.Duration(float64(len(body)) / 128 * float64(time.Second))
	if took < want/2 {
		t.Errorf("%d streamed bytes at 128 B/s took %s, want at least about %s", len(body), took, want)
	}
}
