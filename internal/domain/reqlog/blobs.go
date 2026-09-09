package reqlog

import (
	"context"
	"encoding/json/v2"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// The two halves of an exchange, as the names of their body files.
const (
	sideRequest  = "request"
	sideResponse = "response"
)

// offload moves any body past the threshold out of the entry and into a
// file, leaving a reference in its place. It returns the references of
// files that are no longer pointed at - a body that shrank below the
// threshold on a rewrite - for the caller to remove once the write has
// committed.
//
// Nothing happens when no blob store is configured, which is what
// --body-blob-mb=0 means.
func (s *Store) offload(e *reqlog.Entry) (stale []string, err error) {
	if s.blobs == nil {
		return nil, nil
	}

	dropped, err := s.offloadOne(e.ProjectID, e.ID, sideRequest, &e.Body, &e.BodyRef)
	if err != nil {
		return nil, err
	}

	stale = append(stale, dropped...)

	if e.Response != nil {
		dropped, err := s.offloadOne(e.ProjectID, e.ID, sideResponse, &e.Response.Body, &e.Response.BodyRef)
		if err != nil {
			return nil, err
		}

		stale = append(stale, dropped...)
	}

	return stale, nil
}

// offloadOne handles one body: out to a file when it is big, back into
// the record when it is not.
func (s *Store) offloadOne(projectID, id, side string, body *httpmsg.Body, ref *string) ([]string, error) {
	if !s.blobs.Offloads(len(*body)) {
		// Small now, so any file it had is nobody's.
		if *ref != "" {
			stale := []string{*ref}
			*ref = ""

			return stale, nil
		}

		return nil, nil
	}

	written, err := s.blobs.Put(projectID, id+"."+side, *body)
	if err != nil {
		return nil, err
	}

	// The reference replaces the bytes: the record must not carry both,
	// or the database keeps exactly what this is here to keep out of it.
	*ref = written
	*body = nil

	return nil, nil
}

// hydrate puts offloaded bodies back, so every caller above the store
// sees an ordinary entry. A reference whose file has gone is an error
// rather than an empty body: something is wrong, and quietly showing
// nothing would hide it.
func (s *Store) hydrate(e *reqlog.Entry) error {
	if e.BodyRef != "" {
		body, err := s.blobs.Get(e.BodyRef)
		if err != nil {
			return fmt.Errorf("reqlog: entry %s: %w", e.ID, err)
		}

		e.Body, e.BodyRef = body, ""
	}

	if e.Response != nil && e.Response.BodyRef != "" {
		body, err := s.blobs.Get(e.Response.BodyRef)
		if err != nil {
			return fmt.Errorf("reqlog: entry %s response: %w", e.ID, err)
		}

		e.Response.Body, e.Response.BodyRef = body, ""
	}

	return nil
}

// storedRefs are the body files a stored record points at. Decoded on
// its own rather than by decoding the whole entry: a delete needs the
// references and nothing else, and the bodies are exactly what should
// not be read to get them.
type storedRefs struct {
	BodyRef  string `json:"body_ref"`
	Response *struct {
		BodyRef string `json:"body_ref"`
	} `json:"response"`
}

// refsOf reads the references out of a raw record. A record that will
// not decode has no references worth chasing: the delete goes ahead and
// the sweep will find any file it left.
func refsOf(raw []byte) []string {
	var r storedRefs
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil
	}

	var out []string

	if r.BodyRef != "" {
		out = append(out, r.BodyRef)
	}

	if r.Response != nil && r.Response.BodyRef != "" {
		out = append(out, r.Response.BodyRef)
	}

	return out
}

// SweepBodies removes body files no entry points at, and reports how
// many went.
//
// Two things leave one behind: a crash between writing the file and
// committing the record that names it, and a project deleted while this
// process was not running. Neither is recoverable from the record side,
// so the files are reconciled against the database instead.
func (s *Store) SweepBodies(ctx context.Context) (int, error) {
	if s.blobs == nil {
		return 0, nil
	}

	live := make(map[string]struct{})

	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(project.BucketData)
		if data == nil {
			return nil
		}

		// Every project's log, not just the open one: the files of a
		// project nobody has opened are still somebody's.
		return data.ForEachBucket(func(projectID []byte) error {
			pb := data.Bucket(projectID)
			if pb == nil {
				return nil
			}

			logs := pb.Bucket(BucketLogs)
			if logs == nil {
				return nil
			}

			return logs.ForEach(func(_, v []byte) error {
				if err := ctx.Err(); err != nil {
					return err
				}

				for _, ref := range refsOf(v) {
					live[ref] = struct{}{}
				}

				return nil
			})
		})
	})
	if err != nil {
		return 0, fmt.Errorf("reqlog: sweep bodies: %w", err)
	}

	return s.blobs.Sweep(func(ref string) bool {
		_, ok := live[ref]

		return ok
	})
}

// DropProjectBodies removes every body file of one project, for a
// project that has been deleted. Called from the console's wiring,
// since a deleted project's buckets are gone before this store hears
// about it and there is then nothing left to reconcile against.
func (s *Store) DropProjectBodies(projectID string) {
	s.blobs.DeleteGroup(projectID)
}
