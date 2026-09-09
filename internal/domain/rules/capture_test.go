package rules_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// The workflow that no other rule action can do: a token comes back
// from a sign-in and goes out on the requests that follow.
func TestCapturedTokenReachesLaterRequests(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "capture")

	var seen []string

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/signin" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"tok-abc123","expires":3600}`)

			return
		}

		// What the upstream actually received, which is the only thing
		// worth asserting.
		seen = append(seen, r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()

	setRules(t,
		stack,
		project.Rule{
			Name: "capture the token",
			URL:  "/signin$",
			Action: project.RuleAction{
				Type:    project.ActionCapture,
				From:    "res.body",
				Pattern: `"token"\s*:\s*"([^"]+)"`,
				Name:    "token",
			},
		},
		project.Rule{
			Name: "use it",
			URL:  "/api/",
			Action: project.RuleAction{
				Type:    project.ActionSetRequestHeader,
				Headers: []project.Header{{Name: "Authorization", Value: "Bearer ${token}"}},
			},
		},
	)

	client := stack.Client()

	// Before the sign-in there is nothing to put in, and the
	// placeholder is left as written rather than sent as an empty
	// header - a header that quietly became empty is the bug this
	// avoids.
	res, err := client.Get(target.URL + "/api/early")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if len(seen) != 1 || seen[0] != "Bearer ${token}" {
		t.Fatalf("before the capture the upstream saw %q", seen)
	}

	// Sign in: the rule reads the token out of the response.
	res, err = client.Post(target.URL+"/signin", "application/json", strings.NewReader(`{"user":"admin"}`))
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	// The client still gets the response whole: a capture reads, it
	// does not consume.
	if !strings.Contains(string(body), "tok-abc123") {
		t.Fatalf("the client got %q", body)
	}

	vars := stack.Rules.Variables()
	if len(vars) != 1 || vars[0].Name != "token" || vars[0].Value != "tok-abc123" {
		t.Fatalf("captured %+v", vars)
	}

	if vars[0].Rule != "capture the token" || vars[0].From != "res.body" {
		t.Errorf("the capture does not say where it came from: %+v", vars[0])
	}

	// And now the header carries it.
	res, err = client.Get(target.URL + "/api/late")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if len(seen) != 2 || seen[1] != "Bearer tok-abc123" {
		t.Fatalf("after the capture the upstream saw %q", seen)
	}

	// Closing the project forgets it: another project's token is not
	// this one's.
	stack.Projects.Close()

	if vars := stack.Rules.Variables(); len(vars) != 0 {
		t.Errorf("%d values survived closing the project", len(vars))
	}
}

// A capture reads any field the filter language names, and the built-in
// placeholders need no capture at all.
func TestCaptureFromAHeaderAndTheBuiltins(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "capture-header")

	var seen http.Header

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Set-Cookie", "session=zz-99; Path=/; HttpOnly")
			w.WriteHeader(http.StatusNoContent)

			return
		}

		seen = r.Header.Clone()
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()

	setRules(t,
		stack,
		project.Rule{
			Name: "session from the cookie",
			URL:  "/login$",
			Action: project.RuleAction{
				Type:    project.ActionCapture,
				From:    "res.header.set-cookie",
				Pattern: `session=([^;]+)`,
				Name:    "session",
			},
		},
		project.Rule{
			Name: "put it back with a nonce",
			URL:  "/work",
			Action: project.RuleAction{
				Type: project.ActionSetRequestHeader,
				Headers: []project.Header{
					{Name: "Cookie", Value: "session=${session}"},
					{Name: "X-Request-Id", Value: "${uuid}"},
					{Name: "X-Timestamp", Value: "${timestamp}"},
					{Name: "X-Nonce", Value: "${random}"},
					{Name: "X-Unknown", Value: "${nothing_captured_this}"},
				},
			},
		},
	)

	client := stack.Client()

	res, err := client.Get(target.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	res, err = client.Get(target.URL + "/work")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if got := seen.Get("Cookie"); got != "session=zz-99" {
		t.Errorf("cookie %q, want the captured session", got)
	}

	if got := seen.Get("X-Request-Id"); !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(got) {
		t.Errorf("uuid %q", got)
	}

	if got := seen.Get("X-Timestamp"); !regexp.MustCompile(`^\d{10}$`).MatchString(got) {
		t.Errorf("timestamp %q", got)
	}

	if got := seen.Get("X-Nonce"); !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(got) {
		t.Errorf("nonce %q", got)
	}

	// An unknown name is left as written, on purpose.
	if got := seen.Get("X-Unknown"); got != "${nothing_captured_this}" {
		t.Errorf("unknown placeholder became %q", got)
	}
}

// A mock body is text a rule writes, so it takes values too - which is
// what makes it a stub rather than one fixed answer.
func TestPlaceholdersInAMockAndAUrl(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "capture-mock")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Account", "acct-4242")
		_, _ = io.WriteString(w, "real")
	}))
	defer target.Close()

	setRules(t,
		stack,
		project.Rule{
			Name: "learn the account",
			URL:  "/who$",
			Action: project.RuleAction{
				Type: project.ActionCapture,
				From: "res.header.x-account",
				Name: "account",
			},
		},
		project.Rule{
			Name: "stub, built from what we learned",
			URL:  "/stub$",
			Action: project.RuleAction{
				Type:   project.ActionMock,
				Status: http.StatusOK,
				Body:   `{"account":"${account}","id":"${uuid}"}`,
			},
		},
	)

	client := stack.Client()

	res, err := client.Get(target.URL + "/who")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	res, err = client.Get(target.URL + "/stub")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	// No pattern on the capture: the whole field is the value.
	if !strings.Contains(string(body), `"account":"acct-4242"`) {
		t.Errorf("the mock body is %q", body)
	}

	if strings.Contains(string(body), "${") {
		t.Errorf("a placeholder survived into the mock: %q", body)
	}

	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q - the expanded body decides it", ct)
	}
}

// A capture that names a field the language does not have, or a name
// that could not be found in a body, is refused when the rule is saved.
func TestCaptureIsValidatedOnSave(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "capture-bad")

	for _, c := range []struct {
		name   string
		action project.RuleAction
	}{
		{name: "no field", action: project.RuleAction{Type: project.ActionCapture, Name: "x"}},
		{
			name:   "a field that is not one",
			action: project.RuleAction{Type: project.ActionCapture, From: "res.nonsense", Name: "x"},
		},
		{name: "no name", action: project.RuleAction{Type: project.ActionCapture, From: "res.body"}},
		{
			name:   "a name with a brace in it",
			action: project.RuleAction{Type: project.ActionCapture, From: "res.body", Name: "a}b"},
		},
		{
			name: "a pattern that is not one",
			action: project.RuleAction{
				Type: project.ActionCapture, From: "res.body", Name: "x", Pattern: "[a-",
			},
		},
	} {
		_, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
			s.Rules = []project.Rule{{Enabled: true, Name: c.name, Action: c.action}}

			return nil
		})
		if err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}
