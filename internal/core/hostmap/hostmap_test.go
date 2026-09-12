package hostmap_test

import (
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/hostmap"
)

// TestAddrMovesTheConnectionAndNothingElse is the whole point of the
// package: the address changes, and what the caller asked for does not.
func TestAddrMovesTheConnectionAndNothingElse(t *testing.T) {
	m, err := hostmap.Compile([]hostmap.Entry{
		{Host: "api.example.com", Address: "10.0.0.5"},
		{Host: "*.test.local", Address: "127.0.0.1:8443"},
		{Host: "v6.example.com", Address: "[::1]:8443"},
		{Host: "bare6.example.com", Address: "2001:db8::1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		// No port on the entry, so the request's port is kept.
		"api.example.com:443": "10.0.0.5:443",
		"api.example.com:80":  "10.0.0.5:80",

		// The entry named a port, so it replaces the request's.
		"a.test.local:443":   "127.0.0.1:8443",
		"a.b.test.local:443": "127.0.0.1:8443",

		// IPv6 is bracketed on the way out, whichever way it went in.
		"v6.example.com:443":    "[::1]:8443",
		"bare6.example.com:443": "[2001:db8::1]:443",

		// Nothing matches, so nothing is said.
		"other.example.com:443": "",
		"test.local.evil.com:1": "",
	}

	for addr, want := range cases {
		if got := m.Addr(addr); got != want {
			t.Errorf("Addr(%q) = %q, want %q", addr, got, want)
		}
	}
}

// TestFirstMatchWins is what lets one host in a domain go somewhere
// else: the specific entry is placed above the wildcard.
func TestFirstMatchWins(t *testing.T) {
	m, err := hostmap.Compile([]hostmap.Entry{
		{Host: "api.example.com", Address: "10.0.0.5"},
		{Host: "*.example.com", Address: "10.0.0.9"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := m.Addr("api.example.com:443"); got != "10.0.0.5:443" {
		t.Errorf("the specific entry did not win: %q", got)
	}

	if got := m.Addr("www.example.com:443"); got != "10.0.0.9:443" {
		t.Errorf("the wildcard did not answer: %q", got)
	}
}

// TestAnAddressWithoutAPortIsLeftAlone covers a dialler handing over
// something that is not host:port. Only an entry carrying its own port
// can answer, since there is none to keep.
func TestAnAddressWithoutAPortIsLeftAlone(t *testing.T) {
	m, err := hostmap.Compile([]hostmap.Entry{
		{Host: "noport.example.com", Address: "10.0.0.5"},
		{Host: "withport.example.com", Address: "10.0.0.6:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := m.Addr("noport.example.com"); got != "10.0.0.5" {
		t.Errorf("Addr without a port = %q, want 10.0.0.5", got)
	}

	if got := m.Addr("withport.example.com"); got != "10.0.0.6:8080" {
		t.Errorf("Addr without a port = %q, want 10.0.0.6:8080", got)
	}
}

// TestMatchingIgnoresCase because a host name does.
func TestMatchingIgnoresCase(t *testing.T) {
	m, err := hostmap.Compile([]hostmap.Entry{{Host: "API.Example.COM", Address: "10.0.0.5"}})
	if err != nil {
		t.Fatal(err)
	}

	if got := m.Addr("api.EXAMPLE.com:443"); got != "10.0.0.5:443" {
		t.Errorf("Addr = %q, want 10.0.0.5:443", got)
	}
}

// TestHasAnswersOnTheNameAlone is what the upstream proxy decision
// needs, which is made per host and has no port to hand.
func TestHasAnswersOnTheNameAlone(t *testing.T) {
	m, err := hostmap.Compile([]hostmap.Entry{{Host: "*.example.com", Address: "10.0.0.5"}})
	if err != nil {
		t.Fatal(err)
	}

	if !m.Has("api.example.com") {
		t.Error("Has said no to an overridden host")
	}

	if m.Has("example.org") {
		t.Error("Has said yes to a host with no override")
	}
}

// TestNilMapMatchesNothing, so a project with no overrides needs no nil
// check at the dial.
func TestNilMapMatchesNothing(t *testing.T) {
	var m *hostmap.Map

	if got := m.Addr("api.example.com:443"); got != "" {
		t.Errorf("a nil map answered %q", got)
	}

	if m.Has("api.example.com") {
		t.Error("a nil map said it had a host")
	}

	if m.Len() != 0 {
		t.Errorf("a nil map is %d long", m.Len())
	}
}

// TestEmptyAndBlankCompileToNothing: a list of blanks is a list with
// nothing in it, not an error and not a map that matches.
func TestEmptyAndBlankCompileToNothing(t *testing.T) {
	for _, entries := range [][]hostmap.Entry{
		nil,
		{},
		{{Host: "  ", Address: "10.0.0.5"}},
	} {
		m, err := hostmap.Compile(entries)
		if err != nil {
			t.Fatalf("%v: %v", entries, err)
		}

		if m != nil {
			t.Errorf("%v compiled to a map of %d", entries, m.Len())
		}
	}
}

// TestCompileRefusesWhatCannotWork. Each of these is a typo a person
// makes, and each message has to name what is wrong with it.
func TestCompileRefusesWhatCannotWork(t *testing.T) {
	bad := map[string]hostmap.Entry{
		"a name on the right":      {Host: "api.example.com", Address: "staging.internal"},
		"a name behind a scheme":   {Host: "api.example.com", Address: "http://staging.internal"},
		"a path":                   {Host: "api.example.com", Address: "http://10.0.0.5/api"},
		"a scheme we do not speak": {Host: "api.example.com", Address: "ftp://10.0.0.5"},
		"nothing on the right":     {Host: "api.example.com", Address: ""},
		"a port and no address":    {Host: "api.example.com", Address: ":8443"},
		"a port that is a word":    {Host: "api.example.com", Address: "10.0.0.5:not-a-port"},
		"a broken glob":            {Host: "api.[example.com", Address: "10.0.0.5"},
	}

	for name, entry := range bad {
		if _, err := hostmap.Compile([]hostmap.Entry{entry}); err == nil {
			t.Errorf("%s was accepted: %+v", name, entry)
		}
	}
}

// TestASchemeSaysHowToSpeakNotWhereToGo. This is the one thing a hosts
// file cannot do, and the reason a prod name can be pointed at a plain
// dev server: the client keeps asking for https and the hop to the
// target is plain HTTP.
func TestASchemeSaysHowToSpeakNotWhereToGo(t *testing.T) {
	m, err := hostmap.Compile([]hostmap.Entry{
		{Host: "wiki.example.com", Address: "http://127.0.0.1:3000"},
		{Host: "secure.example.com", Address: "https://127.0.0.1"},
		{Host: "plain.example.com", Address: "127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"wiki.example.com":    "http",
		"secure.example.com":  "https",
		"plain.example.com":   "",
		"nothing.example.com": "",
	}

	for host, want := range cases {
		if got := m.Scheme(host); got != want {
			t.Errorf("Scheme(%q) = %q, want %q", host, got, want)
		}
	}

	// The address still answers as it always did, so the two halves of
	// an entry are independent.
	if got := m.Addr("wiki.example.com:80"); got != "127.0.0.1:3000" {
		t.Errorf("Addr = %q, want 127.0.0.1:3000", got)
	}

	// A scheme with no port of its own leaves the port to the caller,
	// which by then has picked the one that scheme implies.
	if got := m.Addr("secure.example.com:443"); got != "127.0.0.1:443" {
		t.Errorf("Addr = %q, want 127.0.0.1:443", got)
	}
}

// TestMatchHostIsTheOneHostGlob, shared with the lists that ask only
// whether anything matches.
func TestMatchHostIsTheOneHostGlob(t *testing.T) {
	cases := []struct {
		pattern, host string
		want          bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "www.example.com", false},
		{"*.example.com", "a.b.example.com", true},
		{"*.example.com", "example.com", false},
		{"?ost.local", "host.local", true},
		{"[::1]", "::1", true},
		{"example.com", "", false},
	}

	for _, c := range cases {
		if got := hostmap.MatchHost(c.pattern, c.host); got != c.want {
			t.Errorf("MatchHost(%q, %q) = %v, want %v", c.pattern, c.host, got, c.want)
		}
	}
}
