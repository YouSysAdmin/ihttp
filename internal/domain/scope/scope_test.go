package scope

import (
	"net/http"
	"testing"

	"github.com/yousysadmin/ihttp/internal/models/project"
)

func TestMatchIsAnyRuleAllFields(t *testing.T) {
	s, err := Compile([]project.ScopeRule{
		{URL: `^https://api\.example\.com/`},
		{HeaderKey: `(?i)^cookie$`, HeaderValue: `session=`},
		{URL: `/upload`, Body: `magic`},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		url  string
		h    http.Header
		body string
		want bool
	}{
		{"https://api.example.com/x", nil, "", true},
		{"https://other.com/", nil, "", false},
		{"https://other.com/", http.Header{"Cookie": {"session=1"}}, "", true},
		{"https://other.com/", http.Header{"Cookie": {"theme=dark"}}, "", false},
		{"https://other.com/upload", nil, "no", false},
		{"https://other.com/upload", nil, "has magic", true},
	}

	for _, c := range cases {
		if got := s.Match(c.url, c.h, []byte(c.body)); got != c.want {
			t.Errorf("Match(%s, %v, %q) = %v, want %v", c.url, c.h, c.body, got, c.want)
		}
	}
}

func TestHeaderNameIgnoresCase(t *testing.T) {
	s, err := Compile([]project.ScopeRule{{HeaderKey: `^X-TESTING$`, HeaderValue: `^yes$`}})
	if err != nil {
		t.Fatal(err)
	}

	if !s.Match("http://x/", http.Header{"X-Testing": {"yes"}}, nil) {
		t.Fatal("a header name must match whatever its case")
	}

	if s.Match("http://x/", http.Header{"X-Testing": {"YES"}}, nil) {
		t.Fatal("a header value keeps its case")
	}
}

func TestCompileRefusesEmptyAndBadRules(t *testing.T) {
	if _, err := Compile([]project.ScopeRule{{}}); err == nil {
		t.Fatal("an empty rule must be refused")
	}

	if _, err := Compile([]project.ScopeRule{{URL: "["}}); err == nil {
		t.Fatal("a bad regexp must be refused")
	}

	var empty *Scope
	if empty.Match("x", nil, nil) || !empty.Empty() {
		t.Fatal("a nil scope matches nothing")
	}
}
