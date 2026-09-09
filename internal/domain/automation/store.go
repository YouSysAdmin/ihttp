package automation

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/automation"
)

// Store reads and writes a project's automation buckets: the jobs, the
// results, and an index of result summaries so the results table is
// answered without decoding a body per row.
type Store struct {
	db *bolt.DB
}

// NewStore builds a Store over db.
func NewStore(db *bolt.DB) *Store {
	return &Store{db: db}
}

func jobsPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketJobs}
}

func resultsPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketResults}
}

func indexPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketResultIndex}
}

// resultKey orders results within a job: the job id, then the index
// zero-padded so a prefix scan returns them in the order they ran. The
// same key names a result in both the results and the index bucket.
func resultKey(jobID string, index int) []byte {
	return fmt.Appendf(nil, "%s:%08d", jobID, index)
}

func resultPrefix(jobID string) []byte {
	return []byte(jobID + ":")
}

// PutJob writes or rewrites a job.
func (s *Store) PutJob(ctx context.Context, j automation.Job) error {
	data, err := database.Encode(j)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, jobsPath(j.ProjectID)...)
		if err != nil {
			return err
		}

		return b.Put([]byte(j.ID), data)
	})
}

// GetJob returns one job, or (nil, nil) when the project has none by id.
func (s *Store) GetJob(ctx context.Context, projectID, id string) (*automation.Job, error) {
	var out *automation.Job

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, jobsPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		raw := b.Get([]byte(id))
		if raw == nil {
			return nil
		}

		var j automation.Job
		if err := database.Decode(raw, &j); err != nil {
			return err
		}

		out = &j

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("automation: get job %s: %w", id, err)
	}

	return out, nil
}

// ListJobs returns every job of a project, newest first.
func (s *Store) ListJobs(ctx context.Context, projectID string) ([]automation.Job, error) {
	out := []automation.Job{}

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, jobsPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		// Keys are UUIDv7, so walking backwards is newest first.
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var j automation.Job
			if err := database.Decode(v, &j); err != nil {
				return err
			}

			out = append(out, j)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("automation: list jobs: %w", err)
	}

	return out, nil
}

// DeleteJob removes a job and every result it owns.
func (s *Store) DeleteJob(ctx context.Context, projectID, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		jobs, err := database.Bucket(tx, jobsPath(projectID)...)
		if err != nil {
			return err
		}

		if jobs.Get([]byte(id)) == nil {
			return ErrNotFound
		}

		if err := jobs.Delete([]byte(id)); err != nil {
			return err
		}

		return deleteResultsTx(tx, projectID, id)
	})
}

// PutResult writes one result and its row in the index, together.
func (s *Store) PutResult(ctx context.Context, projectID string, r automation.Result) error {
	data, err := database.Encode(r)
	if err != nil {
		return err
	}

	row, err := database.Encode(r.Summarize())
	if err != nil {
		return err
	}

	key := resultKey(r.JobID, r.Index)

	return s.db.Update(func(tx *bolt.Tx) error {
		results, err := database.Bucket(tx, resultsPath(projectID)...)
		if err != nil {
			return err
		}

		if err := results.Put(key, data); err != nil {
			return err
		}

		index, err := database.Bucket(tx, indexPath(projectID)...)
		if err != nil {
			return err
		}

		return index.Put(key, row)
	})
}

// GetResult returns one result of a job, or (nil, nil) when there is none
// by id. The index is scanned for the id, which is cheap, and the result
// itself is then read by key.
func (s *Store) GetResult(ctx context.Context, projectID, jobID, id string) (*automation.Result, error) {
	var out *automation.Result

	err := s.db.View(func(tx *bolt.Tx) error {
		index, err := database.Bucket(tx, indexPath(projectID)...)
		if err != nil || index == nil {
			return err
		}

		var key []byte

		prefix := resultPrefix(jobID)
		c := index.Cursor()

		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var row automation.ResultSummary
			if err := database.Decode(v, &row); err != nil {
				return err
			}

			if row.ID == id {
				key = k

				break
			}
		}

		if key == nil {
			return nil
		}

		results, err := database.Bucket(tx, resultsPath(projectID)...)
		if err != nil || results == nil {
			return err
		}

		raw := results.Get(key)
		if raw == nil {
			return nil
		}

		var r automation.Result
		if err := database.Decode(raw, &r); err != nil {
			return err
		}

		out = &r

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("automation: get result %s: %w", id, err)
	}

	return out, nil
}

// ListResults returns the summary of every result of a job, in the order
// they ran, from the index alone.
func (s *Store) ListResults(ctx context.Context, projectID, jobID string) ([]automation.ResultSummary, error) {
	out := []automation.ResultSummary{}

	err := s.db.View(func(tx *bolt.Tx) error {
		index, err := database.Bucket(tx, indexPath(projectID)...)
		if err != nil || index == nil {
			return err
		}

		prefix := resultPrefix(jobID)
		c := index.Cursor()

		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var row automation.ResultSummary
			if err := database.Decode(v, &row); err != nil {
				return err
			}

			out = append(out, row)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("automation: list results: %w", err)
	}

	return out, nil
}

// EachResult visits every full result of a job in the order they ran.
func (s *Store) EachResult(ctx context.Context, projectID, jobID string, fn func(automation.Result) error) error {
	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, resultsPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		prefix := resultPrefix(jobID)
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			var r automation.Result
			if err := database.Decode(v, &r); err != nil {
				return err
			}

			if err := fn(r); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("automation: each result: %w", err)
	}

	return nil
}

// DeleteResults drops every result of a job, before a re-run.
func (s *Store) DeleteResults(ctx context.Context, projectID, jobID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return deleteResultsTx(tx, projectID, jobID)
	})
}

// Clear drops every job and result of a project.
func (s *Store) Clear(ctx context.Context, projectID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		parent, err := database.Bucket(tx, project.BucketData, []byte(projectID))
		if err != nil {
			return err
		}

		for _, name := range [][]byte{BucketJobs, BucketResults, BucketResultIndex} {
			if parent.Bucket(name) == nil {
				continue
			}

			if err := parent.DeleteBucket(name); err != nil {
				return err
			}
		}

		return nil
	})
}

func deleteResultsTx(tx *bolt.Tx, projectID, jobID string) error {
	for _, path := range [][][]byte{resultsPath(projectID), indexPath(projectID)} {
		if err := deletePrefixTx(tx, path, resultPrefix(jobID)); err != nil {
			return err
		}
	}

	return nil
}

func deletePrefixTx(tx *bolt.Tx, path [][]byte, prefix []byte) error {
	b, err := database.Bucket(tx, path...)
	if err != nil {
		return err
	}

	c := b.Cursor()

	var keys [][]byte
	for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
		keys = append(keys, slices.Clone(k))
	}

	for _, k := range keys {
		if err := b.Delete(k); err != nil {
			return err
		}
	}

	return nil
}
