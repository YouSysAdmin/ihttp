package filter

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Subject is what a query is matched against. The stored log entry, a
// live request in the proxy and a sender request each implement it, so
// the language is one and the subjects are many.
type Subject interface {
	// Field returns the value of a key such as "req.url", or ok=false
	// when the subject has no such field. A dynamic key such as
	// "req.header.cookie" is known whenever its prefix is, and answers
	// "" when the named item is absent.
	Field(key string) (value string, ok bool)

	// Exists reports whether a key is present on this subject: a header
	// that was sent, a query parameter that is in the URL. ok=false is an
	// unknown key.
	Exists(key string) (present bool, ok bool)

	// Headers returns the header set a key such as "req.headers" names,
	// or ok=false when the key is not a header set.
	Headers(key string) (h http.Header, ok bool)

	// Values returns the list a key such as "req.tag" names, or ok=false
	// when the key is not a list. A comparison against a list is true
	// when any value satisfies it, and its negation when none does.
	Values(key string) (values []string, ok bool)

	// Text returns every string a bare word may match against.
	Text() []string
}

// Match evaluates e against s. An unknown key is an error rather than a
// silent false, because a typo in a filter that matches nothing looks
// exactly like an empty log.
func Match(e Expr, s Subject) (bool, error) {
	switch v := e.(type) {
	case nil:
		return true, nil
	case Not:
		m, err := Match(v.X, s)

		return !m, err
	case Logical:
		l, err := Match(v.L, s)
		if err != nil {
			return false, err
		}

		if v.Op == OpAnd && !l {
			return false, nil
		}

		if v.Op == OpOr && l {
			return true, nil
		}

		return Match(v.R, s)
	case Text:
		needle := strings.ToLower(v.Value)
		for _, t := range s.Text() {
			if strings.Contains(strings.ToLower(t), needle) {
				return true, nil
			}
		}

		return false, nil
	case Compare:
		return matchCompare(v, s)
	default:
		return false, fmt.Errorf("%w: unsupported expression %T", ErrInvalid, e)
	}
}

// Known reports whether s answers key at all, through whichever
// accessor it answers. It asks the same four questions matchCompare
// asks, so a key Known accepts is a key a match can evaluate - the
// vocabulary is never written down twice.
func Known(s Subject, key string) bool {
	if _, ok := s.Exists(key); ok {
		return true
	}

	if _, ok := s.Headers(key); ok {
		return true
	}

	if _, ok := s.Values(key); ok {
		return true
	}

	_, ok := s.Field(key)

	return ok
}

// Validate reports the first field in e that s does not know, so a
// query with a typo is refused where it was typed rather than failing
// every time it is used. It walks the tree with Keys, so a field inside
// a branch a match would short-circuit past is checked too.
//
// A nil expression - an empty query - is valid: it matches everything.
func Validate(e Expr, s Subject) error {
	for _, key := range Keys(e) {
		if !Known(s, key) {
			return fmt.Errorf("%w: unknown field %q", ErrInvalid, key)
		}
	}

	return nil
}

func matchCompare(c Compare, s Subject) (bool, error) {
	if c.Op == OpExists {
		present, ok := s.Exists(c.Key)
		if !ok {
			return false, fmt.Errorf("%w: unknown field %q", ErrInvalid, c.Key)
		}

		return present, nil
	}

	if h, ok := s.Headers(c.Key); ok {
		return matchHeaders(c, h)
	}

	if vals, ok := s.Values(c.Key); ok {
		return matchValues(c, vals)
	}

	val, ok := s.Field(c.Key)
	if !ok {
		return false, fmt.Errorf("%w: unknown field %q", ErrInvalid, c.Key)
	}

	switch c.Op {
	case OpRe:
		return c.Re.MatchString(val), nil
	case OpNotRe:
		return !c.Re.MatchString(val), nil
	case OpEq:
		return equalKey(c.Key, val, c.Value), nil
	case OpNotEq:
		return !equalKey(c.Key, val, c.Value), nil
	case OpContains:
		return strings.Contains(strings.ToLower(val), strings.ToLower(c.Value)), nil
	case OpIn:
		for _, want := range c.Values {
			if equalKey(c.Key, val, want) {
				return true, nil
			}
		}

		return false, nil
	}

	// The ordering operators compare as numbers when both sides are
	// numbers, so `res.statusCode >= 400` means what it says instead of
	// comparing "404" and "400" as text.
	return compareOrdered(c.Op, val, c.Value)
}

// caseMatters says which fields compare case-sensitively under = and in.
// A method, a header name, a media type or a scheme is written in any
// case and means the same thing, so `req.method = post` matches POST. A
// URL, a body or a header value keeps its case.
func caseMatters(key string) bool {
	switch key {
	case "req.method", "req.scheme", "req.host", "req.proto", "res.proto", "res.type", "res.mime", "req.ext", "req.color",
		"req.websocket", "res.streamed", "ws.subprotocol", "req.grpcservice", "req.grpcmethod":
		return false
	}

	return true
}

// matchValues matches a list the way a person reads "req.tag = todo":
// true when any value fits, and the negated operators when none does.
// Case is ignored, a tag is typed by hand. The ordering operators have
// no meaning over a list.
func matchValues(c Compare, values []string) (bool, error) {
	hit := false

	for _, v := range values {
		switch c.Op {
		case OpEq, OpNotEq:
			hit = hit || strings.EqualFold(v, c.Value)
		case OpRe, OpNotRe:
			hit = hit || c.Re.MatchString(v)
		case OpContains:
			hit = hit || strings.Contains(strings.ToLower(v), strings.ToLower(c.Value))
		case OpIn:
			hit = hit || slices.ContainsFunc(c.Values, func(want string) bool { return strings.EqualFold(v, want) })
		default:
			return false, fmt.Errorf("%w: operator %s is not defined for %s", ErrInvalid, c.Op, c.Key)
		}
	}

	if c.Op == OpNotEq || c.Op == OpNotRe {
		return !hit, nil
	}

	return hit, nil
}

func compareOrdered(op Op, a, b string) (bool, error) {
	var order int

	fa, okA := parseNumber(a)
	fb, okB := parseNumber(b)

	switch {
	case okA && okB:
		order = cmp.Compare(fa, fb)
	default:
		order = strings.Compare(a, b)
	}

	switch op {
	case OpGt:
		return order > 0, nil
	case OpLt:
		return order < 0, nil
	case OpGtEq:
		return order >= 0, nil
	case OpLtEq:
		return order <= 0, nil
	default:
		return false, fmt.Errorf("%w: unsupported operator %s", ErrInvalid, op)
	}
}

// equalKey is string equality under the key's case rule.
func equalKey(key, a, b string) bool {
	if caseMatters(key) {
		return a == b
	}

	return strings.EqualFold(a, b)
}

// matchHeaders matches every header as "Name: value". Equality is an
// exact match on one line, a regexp is tested on each, and the negated
// forms mean "no header matches".
func matchHeaders(c Compare, h http.Header) (bool, error) {
	hit := false

	for name, values := range h {
		for _, v := range values {
			line := name + ": " + v

			switch c.Op {
			case OpEq, OpNotEq:
				if line == c.Value || strings.EqualFold(line, c.Value) {
					hit = true
				}
			case OpRe, OpNotRe:
				if c.Re.MatchString(line) {
					hit = true
				}
			default:
				return false, fmt.Errorf("%w: operator %s is not defined for headers", ErrInvalid, c.Op)
			}
		}
	}

	if c.Op == OpNotEq || c.Op == OpNotRe {
		return !hit, nil
	}

	return hit, nil
}

// HeaderLines renders headers the way the language matches them, for
// the free-text search over a subject.
func HeaderLines(h http.Header) []string {
	out := make([]string, 0, len(h))

	for name, values := range h {
		for _, v := range values {
			out = append(out, name+": "+v)
		}
	}

	return out
}

// parseNumber reads a plain number or one with a unit: kb, mb, gb for
// sizes in bytes, ms, s, m for times in milliseconds. So `res.size > 1mb`
// and `res.duration > 2s` mean what they say.
var units = []struct {
	suffix string
	scale  float64
}{
	{"ms", 1}, {"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}, {"s", 1000}, {"m", 60_000},
}

func parseNumber(s string) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))

	for _, u := range units {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
			if err != nil {
				return 0, false
			}

			return f * u.scale, true
		}
	}

	f, err := strconv.ParseFloat(s, 64)

	return f, err == nil
}
