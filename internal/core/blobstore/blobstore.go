// Package blobstore keeps large message bodies beside the database
// instead of inside it, so a log of big responses does not turn the
// bbolt file into a heap of megabytes that never shrinks.
//
// The rule this package exists to keep: A BODY IS EITHER IN THE RECORD
// OR IN A FILE, NEVER HALF WAY. A caller offloads before writing and
// hydrates immediately after reading, so nothing downstream - the
// console, the filter language, an export - ever sees a record with a
// body missing. That is why there is no lazy read here: a body that is
// sometimes absent would be a lie in the log, and a smaller database is
// not worth one.
package blobstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultThreshold is the body size past which a body goes to a file. A
// megabyte: below it a record is still a record, above it one exchange
// can weigh more than a thousand.
const DefaultThreshold = 1 << 20

// Store is a directory of body files.
type Store struct {
	dir       string
	threshold int
}

// New prepares dir. A threshold of zero or less turns offloading off,
// and every body stays in the record.
func New(dir string, threshold int) (*Store, error) {
	if threshold > 0 {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("blobstore: %w", err)
		}
	}

	return &Store{dir: filepath.Clean(dir), threshold: threshold}, nil
}

// Offloads says whether a body of n bytes belongs in a file.
func (s *Store) Offloads(n int) bool {
	return s != nil && s.threshold > 0 && n >= s.threshold
}

// Put writes a body and returns the reference to store in its place.
// group and name are the project and the body's own name - an entry id
// and which half it is - and they decide the path, so the same body
// rewritten replaces its file rather than leaving another behind.
func (s *Store) Put(group, name string, body []byte) (string, error) {
	if s == nil || s.threshold <= 0 {
		return "", errors.New("blobstore: not configured")
	}

	ref, err := s.ref(group, name)
	if err != nil {
		return "", err
	}

	path := s.path(ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("blobstore: %w", err)
	}

	// Written whole and moved into place, so a crash halfway through
	// leaves the old file or none - never half a body under a name a
	// record points at.
	tmp := path + ".part"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", fmt.Errorf("blobstore: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return "", fmt.Errorf("blobstore: %w", err)
	}

	return ref, nil
}

// Get reads a body back. A missing file is an error rather than an
// empty body: something has gone wrong, and answering with nothing
// would hide it.
func (s *Store) Get(ref string) ([]byte, error) {
	path, err := s.checked(ref)
	if err != nil {
		return nil, err
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("blobstore: read %s: %w", ref, err)
	}

	return body, nil
}

// Delete removes bodies. A reference with no file is not an error:
// deleting twice, or deleting a record whose file has already gone, is
// the ordinary way this is called.
func (s *Store) Delete(refs ...string) {
	if s == nil {
		return
	}

	for _, ref := range refs {
		path, err := s.checked(ref)
		if err != nil {
			continue
		}

		_ = os.Remove(path)
	}
}

// DeleteGroup removes every body of one group - a project that has
// been deleted.
func (s *Store) DeleteGroup(group string) {
	if s == nil || s.threshold <= 0 || group == "" || !safeSegment(group) {
		return
	}

	_ = os.RemoveAll(filepath.Join(s.dir, group))
}

// Sweep removes bodies that no record claims, and reports how many
// went. keep is asked for every reference found.
//
// This is the answer to a crash between writing a body and committing
// the record that points at it: the file is there, nothing names it,
// and without a sweep it stays for ever.
func (s *Store) Sweep(keep func(ref string) bool) (int, error) {
	if s == nil || s.threshold <= 0 {
		return 0, nil
	}

	groups, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}

		return 0, fmt.Errorf("blobstore: sweep: %w", err)
	}

	removed := 0

	for _, group := range groups {
		if !group.IsDir() {
			continue
		}

		files, err := os.ReadDir(filepath.Join(s.dir, group.Name()))
		if err != nil {
			return removed, fmt.Errorf("blobstore: sweep: %w", err)
		}

		for _, f := range files {
			if f.IsDir() {
				continue
			}

			ref := group.Name() + "/" + f.Name()

			// A half-written file from a crash has no record either.
			if !strings.HasSuffix(f.Name(), ".part") && keep(ref) {
				continue
			}

			if err := os.Remove(filepath.Join(s.dir, group.Name(), f.Name())); err == nil {
				removed++
			}
		}
	}

	return removed, nil
}

// ref builds the reference for a body, refusing anything that could
// point outside the directory.
func (s *Store) ref(group, name string) (string, error) {
	if !safeSegment(group) || !safeSegment(name) {
		return "", fmt.Errorf("blobstore: %q/%q is not a body name", group, name)
	}

	return group + "/" + name, nil
}

// checked turns a reference into a path, refusing one that escapes.
func (s *Store) checked(ref string) (string, error) {
	group, name, ok := strings.Cut(ref, "/")
	if !ok || !safeSegment(group) || !safeSegment(name) {
		return "", fmt.Errorf("blobstore: %q is not a body reference", ref)
	}

	return s.path(group + "/" + name), nil
}

func (s *Store) path(ref string) string {
	return filepath.Join(s.dir, filepath.FromSlash(ref))
}

// safeSegment allows what an id and a body name are made of, and
// nothing that walks the filesystem. Ids here are UUIDs and names are
// like "request" or "message-12", so this is not a limitation.
func safeSegment(seg string) bool {
	if seg == "" || seg == "." || seg == ".." {
		return false
	}

	for _, r := range seg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}

	return !strings.Contains(seg, "..")
}
