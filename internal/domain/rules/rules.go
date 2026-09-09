// Package rules is what the proxy does to traffic on the developer's
// behalf: answer with a mock instead of the real backend, serve a local
// file, point one host at another, add or drop a header, change a status,
// edit a body, slow a request down, allow CORS. Rules are project settings
// compiled when the project opens and applied by one proxy hook.
package rules

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/models/project"
)

// Compiled is a rule with its regular expressions and its filter built.
type Compiled struct {
	Rule    project.Rule
	url     *regexp.Regexp
	pattern *regexp.Regexp
	subs    []substitution
	expr    filter.Expr
}

// NeedsSubject says the rule cannot be matched from the request line
// alone, so the caller has to build a filter subject for it.
func (c Compiled) NeedsSubject() bool {
	return c.expr != nil
}

// expander puts captured values into the text a rule writes. The
// service's own method, passed in so this file stays about compiling.
type expander func(string) string

// varName is what a captured value may be called. Narrow on purpose:
// ${name} has to be unambiguous to find in a body, and a name with a
// brace or a space in it would not be.
var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// substitution is one compiled body replacement.
type substitution struct {
	re      *regexp.Regexp
	replace string
}

// Compile validates and compiles rules. Every problem names the rule by
// position and name, since that is what the person sees in the table.
func Compile(rules []project.Rule) ([]Compiled, error) {
	out := make([]Compiled, 0, len(rules))

	for i, r := range rules {
		c, err := compileOne(r)
		if err != nil {
			label := r.Name
			if label == "" {
				label = strconv.Itoa(i + 1)
			}

			return nil, fmt.Errorf("rule %s: %w", label, err)
		}

		out = append(out, c)
	}

	return out, nil
}

func compileOne(r project.Rule) (Compiled, error) {
	r.Action.Normalize()
	c := Compiled{Rule: r}

	if strings.TrimSpace(r.URL) != "" {
		re, err := regexp.Compile(r.URL)
		if err != nil {
			return c, fmt.Errorf("url: %w", err)
		}

		c.url = re
	}

	if strings.TrimSpace(r.Filter) != "" {
		expr, err := filter.Parse(r.Filter)
		if err != nil {
			return c, fmt.Errorf("filter: %w", err)
		}

		// Same check the ignore filter gets, for the same reason: a rule
		// runs before the response exists, so a res.* key could only
		// ever answer nothing.
		if err := filter.Validate(expr, filter.HTTPSubject{}); err != nil {
			return c, fmt.Errorf("filter: %w", err)
		}

		for _, key := range filter.Keys(expr) {
			if strings.HasPrefix(key, "res.") || strings.HasPrefix(key, "ws.") {
				return c, fmt.Errorf("filter: %s is not known when a rule runs - only req.* fields apply", key)
			}
		}

		c.expr = expr
	}

	a := r.Action

	switch a.Type {
	case project.ActionMock:
		if a.Status != 0 && (a.Status < 100 || a.Status > 599) {
			return c, fmt.Errorf("status %d is not an HTTP status", a.Status)
		}
	case project.ActionMapLocal:
		if strings.TrimSpace(a.Path) == "" {
			return c, fmt.Errorf("map local needs a file path")
		}
	case project.ActionRewriteURL:
		// The rule's URL match is the natural pattern: what selected the
		// request is what gets replaced. A pattern of its own is for
		// replacing a different part than the one matched.
		switch {
		case a.Pattern != "":
			re, err := regexp.Compile(a.Pattern)
			if err != nil {
				return c, fmt.Errorf("pattern: %w", err)
			}

			c.pattern = re
		case c.url != nil:
			c.pattern = c.url
		default:
			return c, fmt.Errorf("rewrite url needs a pattern or a URL match")
		}
	case project.ActionReplaceBody, project.ActionReplaceRequestBody:
		if len(a.Replacements) == 0 {
			return c, fmt.Errorf("%s needs at least one replacement", a.Type)
		}

		for _, rp := range a.Replacements {
			if rp.Pattern == "" {
				return c, fmt.Errorf("%s: a replacement needs a pattern", a.Type)
			}

			re, err := regexp.Compile(rp.Pattern)
			if err != nil {
				return c, fmt.Errorf("pattern %q: %w", rp.Pattern, err)
			}

			c.subs = append(c.subs, substitution{re: re, replace: rp.Replace})
		}
	case project.ActionSetRequestHeader, project.ActionSetResponseHeader,
		project.ActionRemoveRequestHeader, project.ActionRemoveResponseHeader:
		if len(a.Headers) == 0 {
			return c, fmt.Errorf("%s needs at least one header", a.Type)
		}

		for _, h := range a.Headers {
			if strings.TrimSpace(h.Name) == "" {
				return c, fmt.Errorf("%s: a header needs a name", a.Type)
			}
		}
	case project.ActionSetStatus:
		if a.Status < 100 || a.Status > 599 {
			return c, fmt.Errorf("status %d is not an HTTP status", a.Status)
		}
	case project.ActionDelay, project.ActionDelayResponse:
		if a.DelayMS <= 0 {
			return c, fmt.Errorf("%s needs a positive number of milliseconds", a.Type)
		}
	case project.ActionBlock:
		if a.Status != 0 && (a.Status < 100 || a.Status > 599) {
			return c, fmt.Errorf("status %d is not an HTTP status", a.Status)
		}
	case project.ActionThrottle:
		if a.RateBPS <= 0 {
			return c, fmt.Errorf("throttle needs a positive number of bytes a second")
		}
	case project.ActionCapture:
		if !varName.MatchString(a.Name) {
			return c, fmt.Errorf("capture needs a name of letters, digits and underscores, starting with a letter - got %q", a.Name)
		}

		if strings.TrimSpace(a.From) == "" {
			return c, fmt.Errorf("capture needs a field to read, e.g. res.header.set-cookie")
		}

		// The same vocabulary the filters use, so a typo is caught here
		// rather than capturing nothing for ever.
		if key := filter.Normalize(a.From); !filter.Known(filter.HTTPSubject{}, key) {
			return c, fmt.Errorf("capture: %s is not a field - see the filter help for the list", a.From)
		}

		if a.Pattern != "" {
			re, err := regexp.Compile(a.Pattern)
			if err != nil {
				return c, fmt.Errorf("pattern: %w", err)
			}

			c.pattern = re
		}
	case project.ActionAllowCORS:
	default:
		return c, fmt.Errorf("unknown action %q", a.Type)
	}

	return c, nil
}

// substitute runs the replacements in order, expanding ${name} in each
// replacement first - a captured value is as useful in a body as it is
// in a header.
func (c Compiled) substitute(body []byte, expand expander) []byte {
	for _, s := range c.subs {
		body = s.re.ReplaceAll(body, []byte(expand(s.replace)))
	}

	return body
}

// Matches reports whether the rule applies to a request. The cheap
// tests come first, so a rule with a filter costs nothing until its URL
// and method have already agreed.
//
// subject is only consulted by a rule that carries a filter, and may be
// nil - a nil subject makes such a rule match nothing rather than
// everything, since a rule that answers instead of the upstream must
// never fire on a guess.
func (c Compiled) Matches(req *http.Request, subject filter.Subject) bool {
	if !c.Rule.Enabled {
		return false
	}

	if c.Rule.Method != "" && !strings.EqualFold(c.Rule.Method, req.Method) {
		return false
	}

	if c.url != nil && !c.url.MatchString(req.URL.String()) {
		return false
	}

	if c.expr == nil {
		return true
	}

	if subject == nil {
		return false
	}

	ok, err := filter.Match(c.expr, subject)

	return err == nil && ok
}
