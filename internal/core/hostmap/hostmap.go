// Package hostmap is where a host name is dialled: a list of host
// patterns and the addresses that stand in for whatever DNS would have
// answered.
//
// It is a hosts file for the proxy, and it moves the connection and
// nothing else. The Host header, the SNI and the certificate check are
// all taken from the request's URL, which is untouched, so the target
// sees exactly the request it would have seen - which is what makes
// this a replacement for dnsmasq rather than a rewrite.
package hostmap

import (
	"fmt"
	"net"
	"path"
	"strings"
)

// Entry is one host pattern and the address dialled in its place.
type Entry struct {
	// Host is a host glob, the same shape every other host list in the
	// product takes: api.example.com, *.example.com, ?ost.local.
	// Matching ignores case and ignores the port.
	Host string

	// Address is where that host is dialled: an IP, optionally with a
	// port. 10.0.0.5 keeps the port the request asked for, 10.0.0.5:8443
	// replaces that too. An IPv6 address with a port is bracketed,
	// [::1]:8443, as it is everywhere else.
	//
	// A scheme in front says how to SPEAK to it as well as where it is:
	// http://127.0.0.1:3000 reaches a plain dev server even though the
	// client asked for https, and https:// does the reverse. Without one
	// the client's own scheme is kept, which is the ordinary case. This
	// is the one thing a hosts file cannot do and the reason the right
	// side is not simply an IP.
	Address string
}

// Map is a compiled list of overrides. A nil *Map matches nothing, so a
// caller with no overrides needs no nil check.
type Map struct {
	entries []compiled
}

// compiled is one entry with its address already split.
type compiled struct {
	pattern string
	host    string
	port    string

	// scheme is "" when the entry named none, so the caller's own is
	// kept.
	scheme string
}

// Compile checks the entries and returns the map. The order is kept and
// THE FIRST MATCH WINS, unlike the other host lists here, which only
// ask whether anything matches: a specific entry placed above a
// wildcard is how one host in a domain goes somewhere else.
func Compile(entries []Entry) (*Map, error) {
	out := make([]compiled, 0, len(entries))

	for _, e := range entries {
		pattern := strings.ToLower(strings.TrimSpace(e.Host))
		if pattern == "" {
			continue
		}

		if err := CheckPattern(pattern); err != nil {
			return nil, err
		}

		scheme, host, port, err := parseAddress(e.Address)
		if err != nil {
			return nil, fmt.Errorf("host override for %q: %w", e.Host, err)
		}

		out = append(out, compiled{pattern: pattern, host: host, port: port, scheme: scheme})
	}

	if len(out) == 0 {
		return nil, nil
	}

	return &Map{entries: out}, nil
}

// Addr is what to dial instead of addr, which is host:port as a dialler
// takes it, or "" to dial it as given. The entry's port is used when it
// named one and the request's port is kept when it did not.
func (m *Map) Addr(addr string) string {
	if m == nil {
		return ""
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Not host:port after all. There is no port to keep, so only an
		// entry carrying its own can answer.
		host, port = addr, ""
	}

	for _, e := range m.entries {
		if !matches(e.pattern, host) {
			continue
		}

		switch {
		case e.port != "":
			return net.JoinHostPort(e.host, e.port)
		case port != "":
			return net.JoinHostPort(e.host, port)
		default:
			return e.host
		}
	}

	return ""
}

// Has says a host is overridden, for a caller that has a name and no
// port - the upstream proxy decision, which is made per host.
func (m *Map) Has(host string) bool {
	if m == nil {
		return false
	}

	for _, e := range m.entries {
		if matches(e.pattern, host) {
			return true
		}
	}

	return false
}

// Scheme is how to speak to host - "http" or "https" - when the
// override said, and "" to speak whatever the caller was going to. The
// port follows from it: a caller that flips the scheme before it dials
// picks that scheme's default port, and Addr then keeps it unless the
// entry named one of its own.
func (m *Map) Scheme(host string) string {
	if m == nil {
		return ""
	}

	for _, e := range m.entries {
		if matches(e.pattern, host) {
			return e.scheme
		}
	}

	return ""
}

// Len is how many overrides are in force, for a log line.
func (m *Map) Len() int {
	if m == nil {
		return 0
	}

	return len(m.entries)
}

// CheckPattern reports a host glob that cannot be matched against.
func CheckPattern(pattern string) error {
	if _, err := path.Match(pattern, "probe"); err != nil {
		return fmt.Errorf("%q is not a host pattern: %w", pattern, err)
	}

	return nil
}

// MatchHost reports whether a host glob covers a host. This is the one
// host-glob in the product: the do-not-decrypt list, the upstream
// bypass list and these overrides all mean the same thing by
// *.example.com, so a person learns it once.
func MatchHost(pattern, host string) bool {
	return matches(strings.ToLower(strings.TrimSpace(pattern)), host)
}

// matches is MatchHost with the pattern already cleaned, which is how
// it is kept in a compiled list.
func matches(pattern, host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}

	pattern = strings.Trim(pattern, "[]")
	if pattern == host {
		return true
	}

	// path.Match's separator is /, which a host never has, so a * spans
	// the whole name - *.example.com matches a.b.example.com as well as
	// a.example.com.
	ok, _ := path.Match(pattern, host)

	return ok
}

// parseAddress reads the right side of an override: an optional scheme,
// an IP, and an optional port. A NAME is refused rather than resolved,
// because an override is the answer DNS would have given and resolving
// it would only move the question - the scheme is the one part of a URL
// that is allowed here, and it says how to speak rather than where to
// go.
func parseAddress(raw string) (scheme, host, port string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", errNotAnAddress(raw)
	}

	for _, s := range []string{"http", "https"} {
		if rest, ok := strings.CutPrefix(strings.ToLower(raw), s+"://"); ok {
			scheme, raw = s, raw[len(raw)-len(rest):]

			break
		}
	}

	// Anything left that looks like a URL is a mistake worth naming, not
	// something to quietly strip.
	if strings.ContainsAny(raw, "/?#") {
		return "", "", "", errNotAnAddress(raw)
	}

	host, port = raw, ""

	if h, p, splitErr := net.SplitHostPort(raw); splitErr == nil {
		host, port = h, p
	}

	if net.ParseIP(strings.Trim(host, "[]")) == nil {
		return "", "", "", errNotAnAddress(raw)
	}

	if port != "" {
		if _, portErr := net.LookupPort("tcp", port); portErr != nil {
			return "", "", "", fmt.Errorf("%q is not a port", port)
		}
	}

	return scheme, strings.Trim(host, "[]"), port, nil
}

// errNotAnAddress says what the right side of an override is for, since
// the obvious wrong answer - another name - is the one worth naming.
func errNotAnAddress(raw string) error {
	return fmt.Errorf("%q is not an address - an override says where a name lives, so this is an IP such as 10.0.0.5, 10.0.0.5:8443 or http://127.0.0.1:3000, not another name", raw)
}
