// Package instance is what this machine is set to, as against what a
// project is set to.
//
// One document, one bucket. What lives here holds whichever project is
// open and travels with no export: today the extra header names the
// console reads as credentials, which is a fact about how the person at
// this machine reads traffic, not about the target.
package instance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	instancemodels "github.com/yousysadmin/ihttp/internal/models/instance"
)

// The refusals a caller can act on.
var (
	// ErrInvalidHeader is a name that is not a header name. What comes
	// back wrapping it is an InvalidHeaderError, which names the
	// offender: a list is saved whole and the operator has to know
	// which line to fix.
	ErrInvalidHeader = errors.New("instance: not a header name")

	// ErrTooMany is a list past MaxAuthHeaders.
	ErrTooMany = errors.New("instance: too many header names")
)

// MaxAuthHeaders bounds the extra list. A shortlist that is not short is
// not a shortlist, and the Auth view is meant to be read at a glance.
const MaxAuthHeaders = 50

// maxAuthHeaderLen is the longest name accepted, which is longer than
// any header anyone sends and short enough to stay a name.
const maxAuthHeaderLen = 128

// InvalidHeaderError is one name that is not a header name.
type InvalidHeaderError struct {
	Name string
}

// Error names the offending entry.
func (e InvalidHeaderError) Error() string {
	return fmt.Sprintf("%q is not a header name - letters, digits and !#$%%&'*+-.^_`|~ only", e.Name)
}

// Is makes the typed error answer to ErrInvalidHeader.
func (e InvalidHeaderError) Is(target error) bool {
	return target == ErrInvalidHeader
}

// Service owns the document.
type Service struct {
	store *Store
	log   *slog.Logger
}

// NewService builds the service over store.
func NewService(store *Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}

	return &Service{store: store, log: log}
}

// Get returns the settings as stored.
func (s *Service) Get(ctx context.Context) (instancemodels.Settings, error) {
	return s.store.Get(ctx)
}

// SetAuthHeaders replaces the extra header names and answers the
// settings as they were stored. Names are cleaned before they are kept:
// trimmed, emptied lines dropped, and a repeat of one already in the
// list ignored whatever its case, since a header name is matched
// without case.
func (s *Service) SetAuthHeaders(ctx context.Context, names []string) (instancemodels.Settings, error) {
	clean, err := CleanAuthHeaders(names)
	if err != nil {
		return instancemodels.Settings{}, err
	}

	set, err := s.store.Get(ctx)
	if err != nil {
		return instancemodels.Settings{}, err
	}

	set.AuthHeaders = clean

	if err := s.store.Put(ctx, set); err != nil {
		return instancemodels.Settings{}, err
	}

	s.log.Debug("auth header names saved", "count", len(clean))

	return set, nil
}

// CleanAuthHeaders trims, drops blanks, refuses anything that is not a
// header name and removes case-insensitive repeats, keeping the
// spelling that was given first.
func CleanAuthHeaders(names []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}

	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}

		if !isHeaderName(name) {
			return nil, InvalidHeaderError{Name: name}
		}

		lower := strings.ToLower(name)
		if seen[lower] {
			continue
		}

		seen[lower] = true

		out = append(out, name)
	}

	if len(out) > MaxAuthHeaders {
		return nil, ErrTooMany
	}

	return out, nil
}

// isHeaderName says whether name is a header field name: the token
// characters of RFC 9110, nothing else. It is what the console will
// compare against a real header, so anything that cannot be one is a
// typo worth refusing at the form rather than silently never matching.
func isHeaderName(name string) bool {
	if name == "" || len(name) > maxAuthHeaderLen {
		return false
	}

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}

	return true
}
