package rules

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/ids"
)

// Var is one captured value: what it is called, what was read, which
// rule read it and when.
//
// RUNTIME STATE, NEVER STORED. A captured value is a token, a session
// cookie or a nonce - the last thing that should end up in a settings
// document that an export carries to somebody else. Values live for as
// long as the project is open and go when it closes.
type Var struct {
	Name  string    `json:"name"`
	Value string    `json:"value"`
	Rule  string    `json:"rule,omitempty"`
	From  string    `json:"from,omitempty"`
	At    time.Time `json:"at"`
}

// vars is the captured values of the open project.
type vars struct {
	mu     sync.RWMutex
	byName map[string]Var
}

func (v *vars) set(name string, entry Var) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.byName == nil {
		v.byName = make(map[string]Var)
	}

	v.byName[name] = entry
}

func (v *vars) get(name string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	entry, ok := v.byName[name]

	return entry.Value, ok
}

func (v *vars) all() []Var {
	v.mu.RLock()
	defer v.mu.RUnlock()

	out := make([]Var, 0, len(v.byName))
	for _, entry := range v.byName {
		out = append(out, entry)
	}

	return out
}

func (v *vars) clear() {
	v.mu.Lock()
	defer v.mu.Unlock()

	clear(v.byName)
}

// Variables are the values captured so far, newest first.
func (s *Service) Variables() []Var {
	out := s.vars.all()

	// Newest first: what was just captured is what somebody is looking
	// for.
	slices.SortFunc(out, func(a, b Var) int {
		return cmp.Or(b.At.Compare(a.At), strings.Compare(a.Name, b.Name))
	})

	return out
}

// ClearVariables forgets every captured value. Called when the open
// project changes and from the console, for a value that has gone stale.
func (s *Service) ClearVariables() {
	s.vars.clear()
	s.log.Debug("rules: captured values cleared")
}

// capture reads a value out of a finished exchange and remembers it.
// The entry is returned so the caller can announce it.
func (s *Service) capture(c Compiled, subject filter.Subject) (Var, bool) {
	a := c.Rule.Action

	value, ok := subject.Field(filter.Normalize(a.From))
	if !ok || value == "" {
		s.log.Debug("rules: nothing to capture", "rule", c.Rule.Name, "from", a.From)

		return Var{}, false
	}

	if c.pattern != nil {
		m := c.pattern.FindStringSubmatch(value)
		if m == nil {
			s.log.Debug("rules: capture pattern did not match", "rule", c.Rule.Name, "from", a.From)

			return Var{}, false
		}

		// The first group when the pattern has one, since that is what
		// a group is for, and the whole match when it has none.
		value = m[0]
		if len(m) > 1 {
			value = m[1]
		}
	}

	entry := Var{Name: a.Name, Value: value, Rule: c.Rule.Name, From: a.From, At: time.Now()}
	s.vars.set(a.Name, entry)

	// The value itself is not logged: it is a token more often than not.
	s.log.Debug("rules: captured", "rule", c.Rule.Name, "name", a.Name,
		"from", a.From, "bytes", len(value))

	return entry, true
}

// placeholder is what expansion looks for. Deliberately the shell's
// shape: ${name} is what everyone reaches for, and it cannot be
// mistaken for anything in a URL or a header value.
var placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expand replaces ${name} with a captured value or a built-in.
//
// An unknown name is LEFT AS IT IS rather than replaced with nothing: a
// literal ${token} at the server is visible in the log, an empty header
// is not.
func (s *Service) expand(text string) string {
	if !strings.Contains(text, "${") {
		return text
	}

	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		name := match[2 : len(match)-1]

		if value, ok := builtin(name); ok {
			return value
		}

		if value, ok := s.vars.get(name); ok {
			return value
		}

		s.log.Debug("rules: no value for a placeholder, left as written", "name", name)

		return match
	})
}

// builtin answers the values that need no capture: a fresh one per
// request, which is what a nonce or a timestamp has to be.
func builtin(name string) (string, bool) {
	now := time.Now()

	switch name {
	case "uuid":
		return ids.New(), true
	case "timestamp":
		return strconv.FormatInt(now.Unix(), 10), true
	case "timestamp_ms":
		return strconv.FormatInt(now.UnixMilli(), 10), true
	case "isotime":
		return now.UTC().Format(time.RFC3339), true
	case "random":
		// Sixteen hex characters: enough for a nonce, short enough to
		// read in a log.
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", false
		}

		return hex.EncodeToString(b[:]), true
	}

	return "", false
}
