package httpmsg_test

import (
	"strings"
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

func request() httpmsg.SnippetInput {
	return httpmsg.SnippetInput{
		Method: "POST",
		URL:    "https://api.example.com/v2/users?page=2",
		Proto:  "HTTP/1.1",
		Headers: httpmsg.Headers{
			{Name: "Host", Value: "api.example.com"},
			{Name: "Content-Type", Value: "application/json"},
			{Name: "Authorization", Value: "Bearer it's-a-secret"},
			{Name: "Accept-Encoding", Value: "gzip"},
			{Name: "Content-Length", Value: "31"},
		},
		// A real newline in the body: what every language has to escape
		// and the shell must not.
		Body: httpmsg.Body("{\"name\":\"O'Brien\",\"note\":\"line1\nline2\"}"),
	}
}

// Every spelling has to carry the same request, and the awkward parts
// are the same in all of them: a quote inside a value, a newline
// inside a body, and the two headers the client sets for itself.
func TestEverySnippetCarriesTheRequest(t *testing.T) {
	t.Parallel()

	in := request()

	cases := map[string]struct {
		got string
		// Text that must be there, and text that must not.
		want   []string
		unwant []string
	}{
		"curl": {
			got:    httpmsg.Curl(in),
			want:   []string{"curl 'https://api.example.com/v2/users?page=2'", `Bearer it'\''s-a-secret`, "--compressed", "--data-raw"},
			unwant: []string{"Content-Length", "Accept-Encoding: gzip"},
		},
		"fetch": {
			got:    httpmsg.Fetch(in),
			want:   []string{"await fetch('https://api.example.com/v2/users?page=2'", "method: 'POST'", `\'Brien`, `line1\nline2`},
			unwant: []string{"Content-Length", "Accept-Encoding"},
		},
		"httpie": {
			got: httpmsg.HTTPie(in),
			want: []string{
				// --ignore-stdin or the command refuses --raw when it is
				// run from a script rather than a terminal.
				"http --ignore-stdin POST 'https://api.example.com/v2/users?page=2'",
				"'Content-Type:application/json'", "--raw",
			},
			unwant: []string{"Content-Length"},
		},
		"python": {
			got:    httpmsg.Python(in),
			want:   []string{"import requests", "response = requests.post(", `"Authorization": "Bearer it's-a-secret"`, `.encode()`, "print(response.status_code)"},
			unwant: []string{"Content-Length", "Accept-Encoding"},
		},
		"go": {
			got: httpmsg.Go(in),
			want: []string{
				"package main", `"net/http"`, `"strings"`,
				`http.NewRequest("POST", "https://api.example.com/v2/users?page=2", strings.NewReader(`,
				// A Host in the header map is ignored by net/http.
				`req.Host = "api.example.com"`,
				`req.Header.Add("Content-Type", "application/json")`,
				`line1\nline2`,
			},
			unwant: []string{`Add("Content-Length"`, `Add("Host"`},
		},
		"powershell": {
			got: httpmsg.PowerShell(in),
			want: []string{
				"Invoke-RestMethod -Method POST -Uri 'https://api.example.com/v2/users?page=2'",
				// Its own parameter, because PowerShell refuses it in -Headers.
				"-ContentType 'application/json'",
				"'Authorization' = 'Bearer it''s-a-secret'",
				"-Body '",
			},
			unwant: []string{"'Content-Type' =", "Content-Length"},
		},
	}

	for name, c := range cases {
		for _, want := range c.want {
			if !strings.Contains(c.got, want) {
				t.Errorf("%s does not carry %q:\n%s", name, want, c.got)
			}
		}

		for _, unwant := range c.unwant {
			if strings.Contains(c.got, unwant) {
				t.Errorf("%s carries %q, which the client sets itself:\n%s", name, unwant, c.got)
			}
		}
	}
}

// A binary body cannot be a literal in any of these languages, so each
// one rebuilds it from base64 rather than pasting bytes into a shell.
func TestBinaryBodiesAreRebuiltFromBase64(t *testing.T) {
	t.Parallel()

	in := httpmsg.SnippetInput{
		Method:  "PUT",
		URL:     "https://api.example.com/blob",
		Headers: httpmsg.Headers{{Name: "Content-Type", Value: "application/octet-stream"}},
		Body:    httpmsg.Body([]byte{0x00, 0x01, 0xff, 0xfe}),
	}

	const encoded = "AAH//g=="

	cases := map[string]struct{ got, want string }{
		"curl": {got: httpmsg.Curl(in), want: "base64 -d | curl"},
		// Piped in, so --ignore-stdin would throw the body away.
		"httpie":     {got: httpmsg.HTTPie(in), want: "base64 -d | http PUT"},
		"fetch":      {got: httpmsg.Fetch(in), want: "Uint8Array.from(atob("},
		"python":     {got: httpmsg.Python(in), want: "data=base64.b64decode("},
		"go":         {got: httpmsg.Go(in), want: "base64.StdEncoding.DecodeString("},
		"powershell": {got: httpmsg.PowerShell(in), want: "[Convert]::FromBase64String("},
	}

	for name, c := range cases {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%s does not rebuild the body (%q):\n%s", name, c.want, c.got)
		}

		if !strings.Contains(c.got, encoded) {
			t.Errorf("%s does not carry the bytes:\n%s", name, c.got)
		}
	}
}

// An unusual method still has to come out right: requests has no
// function for it and curl needs -X.
func TestUnusualMethod(t *testing.T) {
	t.Parallel()

	in := httpmsg.SnippetInput{Method: "PURGE", URL: "https://api.example.com/cache"}

	if got := httpmsg.Curl(in); !strings.Contains(got, "-X PURGE") {
		t.Errorf("curl: %s", got)
	}

	if got := httpmsg.Python(in); !strings.Contains(got, `requests.request("PURGE", `) {
		t.Errorf("python: %s", got)
	}

	if got := httpmsg.Go(in); !strings.Contains(got, `http.NewRequest("PURGE"`) {
		t.Errorf("go: %s", got)
	}
}
