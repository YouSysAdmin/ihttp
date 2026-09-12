package upstreams_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	upstreamcore "github.com/yousysadmin/ihttp/internal/core/upstream"
	"github.com/yousysadmin/ihttp/internal/domain/upstreams"
	projectmodels "github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// mockingProxy is an ihttp that answers every request itself, so a test
// can tell which way out was taken without touching a network. The body
// it answers with names it.
func mockingProxy(t *testing.T, name string) *testutil.Stack {
	t.Helper()

	s := testutil.New(t)
	s.OpenProject(t, name)

	_, err := s.Projects.UpdateSettings(t.Context(), func(set *projectmodels.Settings) error {
		set.Rules = []projectmodels.Rule{{
			Enabled: true,
			Name:    "answer everything",
			Action: projectmodels.RuleAction{
				Type:   projectmodels.ActionMock,
				Status: http.StatusOK,
				Body:   name,
			},
		}}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// The point of the whole feature: two projects, two ways out, and
// switching project switches the way out with no restart and no rebuilt
// transport.
func TestProjectChoosesTheWayOut(t *testing.T) {
	viaA := mockingProxy(t, "proxy-a")
	viaB := mockingProxy(t, "proxy-b")
	deflt := mockingProxy(t, "the-default")

	front := testutil.New(t, testutil.WithUpstreamDefault(upstreamcore.Config{URL: deflt.Proxy.URL}))
	ctx := t.Context()

	a, err := front.Upstreams.Save(ctx, upstreams.SaveRequest{Name: "Lab A", URL: viaA.Proxy.URL})
	if err != nil {
		t.Fatal(err)
	}

	b, err := front.Upstreams.Save(ctx, upstreams.SaveRequest{Name: "Lab B", URL: viaB.Proxy.URL})
	if err != nil {
		t.Fatal(err)
	}

	// Three projects on one instance, each with its own way out.
	create := func(name string) string {
		t.Helper()

		p, err := front.Projects.Create(ctx, name)
		if err != nil {
			t.Fatal(err)
		}

		return p.ID
	}

	projA := create("uses A")
	projB := create("uses B")
	projD := create("uses the default")

	setUpstream := func(projectID, choice string) {
		t.Helper()

		if _, err := front.Projects.Open(ctx, projectID); err != nil {
			t.Fatal(err)
		}

		_, err := front.Projects.UpdateSettings(ctx, func(s *projectmodels.Settings) error {
			s.Upstream = choice

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	setUpstream(projA, a.ID)
	setUpstream(projB, b.ID)
	setUpstream(projD, "")

	wentThrough := func() string {
		t.Helper()

		res, err := front.Client().Get("http://somewhere.invalid/x")
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = res.Body.Close() }()

		buf := make([]byte, 64)
		n, _ := res.Body.Read(buf)

		return strings.TrimSpace(string(buf[:n]))
	}

	// No restart between any of these: the same process, the same
	// transport, a different open project.
	for _, c := range []struct{ project, want string }{
		{projA, "proxy-a"},
		{projB, "proxy-b"},
		{projD, "the-default"},
		{projA, "proxy-a"},
	} {
		if _, err := front.Projects.Open(ctx, c.project); err != nil {
			t.Fatal(err)
		}

		if got := wentThrough(); got != c.want {
			t.Errorf("with %s open the request went through %q, want %q", c.want, got, c.want)
		}
	}

	// Direct means direct, whatever the instance default is: .invalid
	// does not resolve, so it cannot succeed - which is the proof it did
	// not go through the default that would have answered.
	setUpstream(projD, projectmodels.UpstreamDirect)

	res, err := front.Client().Get("http://somewhere.invalid/x")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("a direct project answered %d, want a 502 from failing to resolve", res.StatusCode)
	}
}

// A project pointing at a proxy this instance does not have - deleted
// here, or imported from a machine that had it - falls back to the
// default rather than failing every request.
func TestUnknownChoiceFallsBackToTheDefault(t *testing.T) {
	deflt := mockingProxy(t, "the-default")
	gone := mockingProxy(t, "gone")

	front := testutil.New(t, testutil.WithUpstreamDefault(upstreamcore.Config{URL: deflt.Proxy.URL}))
	ctx := t.Context()

	srv, err := front.Upstreams.Save(ctx, upstreams.SaveRequest{Name: "Doomed", URL: gone.Proxy.URL})
	if err != nil {
		t.Fatal(err)
	}

	front.OpenProject(t, "orphaned")

	if _, err := front.Projects.UpdateSettings(ctx, func(s *projectmodels.Settings) error {
		s.Upstream = srv.ID

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// While it exists, it is used.
	res, err := front.Client().Get("http://somewhere.invalid/x")
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 32)
	n, _ := res.Body.Read(buf)
	_ = res.Body.Close()

	if got := strings.TrimSpace(string(buf[:n])); got != "gone" {
		t.Fatalf("went through %q, want the chosen proxy", got)
	}

	if err := front.Upstreams.Delete(ctx, srv.ID); err != nil {
		t.Fatal(err)
	}

	// Gone: the default, not a failure.
	res, err = front.Client().Get("http://somewhere.invalid/y")
	if err != nil {
		t.Fatal(err)
	}

	n, _ = res.Body.Read(buf)
	_ = res.Body.Close()

	if got := strings.TrimSpace(string(buf[:n])); got != "the-default" {
		t.Errorf("after the proxy was deleted the request went through %q, want the default", got)
	}
}

// An edit takes effect without a restart, which the compiled cache has
// to notice.
func TestEditTakesEffect(t *testing.T) {
	first := mockingProxy(t, "first")
	second := mockingProxy(t, "second")

	front := testutil.New(t)
	ctx := t.Context()

	srv, err := front.Upstreams.Save(ctx, upstreams.SaveRequest{Name: "Lab", URL: first.Proxy.URL})
	if err != nil {
		t.Fatal(err)
	}

	front.OpenProject(t, "edited")

	if _, err := front.Projects.UpdateSettings(ctx, func(s *projectmodels.Settings) error {
		s.Upstream = srv.ID

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	through := func() string {
		t.Helper()

		res, err := front.Client().Get("http://somewhere.invalid/x")
		if err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 32)
		n, _ := res.Body.Read(buf)
		_ = res.Body.Close()

		return strings.TrimSpace(string(buf[:n]))
	}

	if got := through(); got != "first" {
		t.Fatalf("went through %q", got)
	}

	// UpdatedAt is what the cache keys on, so an edit within the same
	// millisecond still has to be noticed.
	time.Sleep(2 * time.Millisecond)

	if _, err := front.Upstreams.Save(ctx, upstreams.SaveRequest{
		ID: srv.ID, Name: "Lab", URL: second.Proxy.URL,
	}); err != nil {
		t.Fatal(err)
	}

	if got := through(); got != "second" {
		t.Errorf("after the edit the request went through %q, want the new proxy", got)
	}
}

// A list and a summary never carry a credential, which is the whole
// reason the list and the editor are different endpoints.
func TestListCarriesNoCredentials(t *testing.T) {
	front := testutil.New(t)
	ctx := t.Context()

	const raw = "http://ops:s3cret@proxy.corp:3128"

	srv, err := front.Upstreams.Save(ctx, upstreams.SaveRequest{Name: "Corp", URL: raw})
	if err != nil {
		t.Fatal(err)
	}

	list, err := front.Upstreams.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("%d in the list (%v)", len(list), err)
	}

	row := list[0]
	if row.Host != "http://proxy.corp:3128" {
		t.Errorf("host %q, want the URL without its userinfo", row.Host)
	}

	if !row.HasCredentials {
		t.Error("the row does not say a credential is stored")
	}

	// Not the password, and not the user name either.
	if strings.Contains(row.Host, "s3cret") || strings.Contains(row.Host, "ops") {
		t.Errorf("the summary leaked a credential: %q", row.Host)
	}

	// The editor, and only the editor, gets the real thing.
	full, err := front.Upstreams.Get(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}

	if full.URL != raw {
		t.Errorf("the editor got %q, want the stored URL", upstreamcore.Redact(full.URL))
	}
}

// What cannot work is refused before it is stored.
func TestSaveRefusesWhatCannotWork(t *testing.T) {
	front := testutil.New(t)
	ctx := t.Context()

	if _, err := front.Upstreams.Save(ctx, upstreams.SaveRequest{Name: "Good", URL: "http://proxy:3128"}); err != nil {
		t.Fatal(err)
	}

	bad := map[string]upstreams.SaveRequest{
		"no name":                      {URL: "http://proxy:3128"},
		"no url":                       {Name: "Empty"},
		"a url with no scheme":         {Name: "Bare", URL: "proxy:3128"},
		"a scheme that is not a proxy": {Name: "Ftp", URL: "ftp://proxy:3128"},
		"a broken bypass glob":         {Name: "Glob", URL: "http://proxy:3128", Bypass: []string{"[a-"}},
		"a name already taken":         {Name: "good", URL: "http://other:3128"},
	}

	for what, in := range bad {
		if _, err := front.Upstreams.Save(ctx, in); err == nil {
			t.Errorf("%s was accepted", what)
		}
	}
}

// A project's reference is checked for shape when it is saved, since
// whether the id exists is another domain's business.
func TestProjectUpstreamShapeIsChecked(t *testing.T) {
	front := testutil.New(t)
	front.OpenProject(t, "shape")

	for _, choice := range []string{"", projectmodels.UpstreamDirect} {
		if _, err := front.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
			s.Upstream = choice

			return nil
		}); err != nil {
			t.Errorf("%q was refused: %v", choice, err)
		}
	}

	if _, err := front.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.Upstream = "not-an-id"

		return nil
	}); err == nil {
		t.Error("a reference that is neither direct nor an id was accepted")
	}

	// A well-shaped id that is not in the list is accepted here and
	// resolved at request time, because an imported project may name one.
	if _, err := front.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.Upstream = "01a08000-0000-7000-8000-000000000000"

		return nil
	}); err != nil {
		t.Errorf("a well-shaped unknown id was refused: %v", err)
	}

}

// An override says where a host is, so it is reached directly even when
// the project goes out through a proxy: asking a corporate proxy to
// route to an address only this machine can see is how an override
// would quietly stop working. Everything else still goes the long way.
func TestAnOverriddenHostGoesDirect(t *testing.T) {
	via := mockingProxy(t, "the-proxy")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "the lab box")
	}))
	t.Cleanup(target.Close)

	front := testutil.New(t, testutil.WithUpstreamDefault(upstreamcore.Config{URL: via.Proxy.URL}))
	front.OpenProject(t, "hosts")

	// api.example.invalid never resolves, so reaching the target at all
	// proves the override was used, and reaching it DIRECTLY is what
	// this test is about.
	if _, err := front.Projects.UpdateSettings(t.Context(), func(set *projectmodels.Settings) error {
		set.HostOverrides = []projectmodels.HostOverride{
			{Host: "api.example.invalid", Address: target.Listener.Addr().String(), Enabled: true},
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if got := body(t, front, "http://api.example.invalid/"); got != "the lab box" {
		t.Errorf("an overridden host answered %q, want the lab box - it went through the proxy", got)
	}

	// A host with no override is untouched and still goes out the
	// configured way.
	if got := body(t, front, "http://elsewhere.example.com/"); got != "the-proxy" {
		t.Errorf("a host with no override answered %q, want the-proxy", got)
	}
}

// body is one GET through the stack's proxy, read whole.
func body(t *testing.T, s *testutil.Stack, url string) string {
	t.Helper()

	res, err := s.Client().Get(url)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	return strings.TrimSpace(string(out))
}
