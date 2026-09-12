package instance_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"net/http"
	"testing"

	"github.com/yousysadmin/ihttp/internal/domain/instance"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// A name saved is a name read back, whatever project is open, and the
// document survives the service that wrote it.
func TestAuthHeadersRoundTrip(t *testing.T) {
	s := testutil.New(t)

	set, err := s.Instance.SetAuthHeaders(t.Context(), []string{" X-Acme-Token ", "X-Tenant-Key"})
	if err != nil {
		t.Fatal(err)
	}

	if len(set.AuthHeaders) != 2 || set.AuthHeaders[0] != "X-Acme-Token" {
		t.Fatalf("stored %q, want the trimmed pair", set.AuthHeaders)
	}

	again, err := instance.NewService(instance.NewStore(s.DB), nil).Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(again.AuthHeaders) != 2 || again.AuthHeaders[1] != "X-Tenant-Key" {
		t.Fatalf("read back %q, want what was written", again.AuthHeaders)
	}
}

// A blank line is not an entry and a repeat is not a second entry: the
// form is a textarea and both are what it produces.
func TestAuthHeadersCleaned(t *testing.T) {
	got, err := instance.CleanAuthHeaders([]string{"X-Token", "", "  ", "x-token", "X-Other"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0] != "X-Token" || got[1] != "X-Other" {
		t.Fatalf("cleaned to %q, want the first spelling of each", got)
	}
}

// A name that cannot be a header would never match anything, so it is
// refused at the form rather than stored to never fire.
func TestAuthHeadersRefuseNonHeader(t *testing.T) {
	for _, name := range []string{"X Token", "token:", "токен", "x\ttoken"} {
		if _, err := instance.CleanAuthHeaders([]string{name}); !errors.Is(err, instance.ErrInvalidHeader) {
			t.Fatalf("%q: got %v, want ErrInvalidHeader", name, err)
		}
	}
}

// The list is bounded like every other list here.
func TestAuthHeadersBounded(t *testing.T) {
	names := make([]string, 0, instance.MaxAuthHeaders+1)
	for i := range instance.MaxAuthHeaders + 1 {
		names = append(names, "X-Token-"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}

	if _, err := instance.CleanAuthHeaders(names); !errors.Is(err, instance.ErrTooMany) {
		t.Fatalf("got %v, want ErrTooMany", err)
	}
}

// The console reads the list over the API, and a bad name comes back as
// a 400 naming the offender rather than a 500.
func TestAuthHeadersOverAPI(t *testing.T) {
	s := testutil.New(t)

	var saved instance.SettingsResponse
	if code := putHeaders(t, s, `{"auth_headers":["X-Acme-Token"]}`, &saved); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}

	if len(saved.Settings.AuthHeaders) != 1 {
		t.Fatalf("saved %q, want one name", saved.Settings.AuthHeaders)
	}

	res, err := http.Get(s.Console.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	var read instance.SettingsResponse
	if err := json.UnmarshalRead(res.Body, &read); err != nil {
		t.Fatal(err)
	}

	if len(read.Settings.AuthHeaders) != 1 || read.Settings.AuthHeaders[0] != "X-Acme-Token" {
		t.Fatalf("read %q, want what was saved", read.Settings.AuthHeaders)
	}

	if code := putHeaders(t, s, `{"auth_headers":["X Token"]}`, nil); code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", code)
	}
}

// putHeaders writes the list over the API and reads the answer into out
// when there is one, returning the status.
func putHeaders(t *testing.T, s *testutil.Stack, body string, out *instance.SettingsResponse) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		s.Console.URL+"/api/settings/auth-headers", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if out != nil {
		if err := json.UnmarshalRead(res.Body, out); err != nil {
			t.Fatal(err)
		}
	}

	return res.StatusCode
}
