// Package scope decides whether a request belongs to the work at hand.
//
// A project's scope is a list of rules, each a set of regular
// expressions over the URL, a header and the body. A request is in scope
// when ANY rule matches, and a rule matches when EVERY expression it
// carries does - so one rule is "this host", another "requests carrying
// this cookie", and the scope is their union.
package scope

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"

	"github.com/yousysadmin/ihttp/internal/models/project"
)

// Scope is a compiled rule set. The zero value matches nothing, which
// is what an empty scope means: with no rules, nothing is in scope and
// the only-in-scope switches show nothing.
type Scope struct {
	rules []rule
}

type rule struct {
	url, headerKey, headerValue, body *regexp.Regexp
}

// Compile turns stored rules into a Scope. A rule that tests nothing is
// refused, because it would match every request and read as a bug in
// whichever list it silently filled.
func Compile(rules []project.ScopeRule) (*Scope, error) {
	s := &Scope{rules: make([]rule, 0, len(rules))}

	for i, r := range rules {
		if r.IsEmpty() {
			return nil, fmt.Errorf("scope: rule %d tests nothing", i+1)
		}

		c := rule{}
		var err error

		if c.url, err = compile(r.URL); err != nil {
			return nil, fmt.Errorf("scope: rule %d url: %w", i+1, err)
		}

		if c.headerKey, err = compileFold(r.HeaderKey); err != nil {
			return nil, fmt.Errorf("scope: rule %d header key: %w", i+1, err)
		}

		if c.headerValue, err = compile(r.HeaderValue); err != nil {
			return nil, fmt.Errorf("scope: rule %d header value: %w", i+1, err)
		}

		if c.body, err = compile(r.Body); err != nil {
			return nil, fmt.Errorf("scope: rule %d body: %w", i+1, err)
		}

		s.rules = append(s.rules, c)
	}

	return s, nil
}

func compile(src string) (*regexp.Regexp, error) {
	if src == "" {
		return nil, nil
	}

	return regexp.Compile(src)
}

// compileFold compiles a pattern that ignores case. Header NAMES are
// case-insensitive in HTTP, and a rule written as X-TESTING has to match
// the X-Testing Go canonicalizes to - a scope that depends on how the
// operator capitalized a name is a scope that silently misses.
func compileFold(src string) (*regexp.Regexp, error) {
	if src == "" {
		return nil, nil
	}

	return regexp.Compile("(?i)" + src)
}

// Empty reports a scope with no rules.
func (s *Scope) Empty() bool {
	return s == nil || len(s.rules) == 0
}

// Match reports whether a request described by url, headers and body is
// in scope. Every field is optional on the caller's side - a log entry
// with no body passes' nil.
func (s *Scope) Match(url string, headers http.Header, body []byte) bool {
	if s == nil {
		return false
	}

	for _, r := range s.rules {
		if r.match(url, headers, body) {
			return true
		}
	}

	return false
}

func (r rule) match(url string, headers http.Header, body []byte) bool {
	if r.url != nil && !r.url.MatchString(url) {
		return false
	}

	if r.body != nil && !r.body.Match(body) {
		return false
	}

	if r.headerKey != nil || r.headerValue != nil {
		return r.matchHeader(headers)
	}

	return true
}

// matchHeader wants one header line satisfying whatever of key and
// value the rule carries. Both set means both on the SAME header.
func (r rule) matchHeader(headers http.Header) bool {
	for name, values := range headers {
		if r.headerKey != nil && !r.headerKey.MatchString(name) {
			continue
		}

		if r.headerValue == nil {
			return true
		}

		if slices.ContainsFunc(values, r.headerValue.MatchString) {
			return true
		}
	}

	return false
}
