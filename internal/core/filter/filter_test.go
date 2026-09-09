package filter

import (
	"net/http"
	"testing"
)

type fake struct {
	fields  map[string]string
	headers map[string]http.Header
}

func (f fake) Field(key string) (string, bool) {
	v, ok := f.fields[key]

	return v, ok
}

func (f fake) Values(string) ([]string, bool) {
	return nil, false
}

func (f fake) Exists(key string) (bool, bool) {
	v, ok := f.fields[key]

	return v != "", ok
}

func (f fake) Headers(key string) (http.Header, bool) {
	h, ok := f.headers[key]

	return h, ok
}

func (f fake) Text() []string {
	var out []string
	for _, v := range f.fields {
		out = append(out, v)
	}

	for _, h := range f.headers {
		out = append(out, HeaderLines(h)...)
	}

	return out
}

var subject = fake{
	fields: map[string]string{
		"req.method":     "POST",
		"req.url":        "https://example.com/api/v2/login",
		"res.statuscode": "404",
	},
	headers: map[string]http.Header{
		"req.headers": {"Content-Type": {"application/json"}, "Cookie": {"a=b"}},
	},
}

func TestParseShapes(t *testing.T) {
	cases := map[string]string{
		`login`:             `"login"`,
		`req.method = POST`: `(req.method = "POST")`,
		`a b`:               `("a" AND "b")`,
		`a OR b AND c`:      `("a" OR ("b" AND "c"))`,
		`(a OR b) AND c`:    `(("a" OR "b") AND "c")`,
		`NOT a b`:           `((NOT "a") AND "b")`,
		`req.url =~ "v[0-9]+" res.statusCode >= 400`: `((req.url =~ "v[0-9]+") AND (res.statuscode >= "400"))`,
		`"quoted value" = x`:                         `(quoted value = "x")`,

		// A filter typed over several lines is the same filter: the console
		// offers a multi-line box for a long rule, and a line break is
		// whitespace like any other.
		"req.method != GET\n  AND req.url =~ \"example\"\n  OR res.statusCode >= 500": `(((req.method != "GET") AND (req.url =~ "example")) OR (res.statuscode >= "500"))`,
	}

	for in, want := range cases {
		e, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}

		if e.String() != want {
			t.Errorf("Parse(%q) = %s, want %s", in, e.String(), want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{``, `(a`, `a =`, `= a`, `a =~ "["`, `a ! b`, `"unclosed`, `a in`, `a in ()`, `a in (b`, `a in b`} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := map[string]bool{
		`login`:              true,
		`LOGIN`:              true,
		`nope`:               false,
		`req.method = POST`:  true,
		`req.method != POST`: false,
		`req.METHOD = post`:  true,
		`req.url = HTTPS://EXAMPLE.COM/API/V2/LOGIN`:     false,
		`req.url contains LOGIN`:                         true,
		`req.method in (GET, POST)`:                      true,
		`req.method in (GET, PUT)`:                       false,
		`req.url exists`:                                 true,
		`res.statusCode > 0.4kb`:                         false,
		`res.statusCode < 1kb`:                           true,
		`res.statusCode >= 400`:                          true,
		`res.statusCode < 400`:                           false,
		`res.statusCode > 99`:                            true,
		`req.url =~ "^https://example\.com/api"`:         true,
		`req.url !~ "png$"`:                              true,
		`req.headers =~ "^Cookie: a=b$"`:                 true,
		`req.headers = "Content-Type: application/json"`: true,
		`req.headers != "X-Nope: 1"`:                     true,
		`NOT req.method = GET AND login`:                 true,
		`cookie`:                                         true,
	}

	for in, want := range cases {
		got, err := Match(mustParse(t, in), subject)
		if err != nil {
			t.Fatalf("Match(%q): %v", in, err)
		}

		if got != want {
			t.Errorf("Match(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMatchUnknownKeyIsAnError(t *testing.T) {
	if _, err := Match(mustParse(t, `req.nope = 1`), subject); err == nil {
		t.Fatal("an unknown key must be reported")
	}
}

func TestKeys(t *testing.T) {
	keys := Keys(mustParse(t, `req.url = a OR (req.url = b AND res.statusCode = 1)`))
	if len(keys) != 2 || keys[0] != "req.url" || keys[1] != "res.statuscode" {
		t.Fatalf("Keys = %v", keys)
	}
}
