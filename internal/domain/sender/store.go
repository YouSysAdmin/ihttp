package sender

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/sender"
)

// Store reads and writes a project's sender bucket.
type Store struct {
	db *bolt.DB
}

// NewStore builds a Store over db.
func NewStore(db *bolt.DB) *Store {
	return &Store{db: db}
}

func bucketPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketRequests}
}

// Put writes or rewrites a request.
func (s *Store) Put(ctx context.Context, r sender.Request) error {
	data, err := database.Encode(r)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(r.ProjectID)...)
		if err != nil {
			return err
		}

		return b.Put([]byte(r.ID), data)
	})
}

// Get returns one request, or (nil, nil) when the project has none by id.
func (s *Store) Get(ctx context.Context, projectID, id string) (*sender.Request, error) {
	var out *sender.Request

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		raw := b.Get([]byte(id))
		if raw == nil {
			return nil
		}

		var r sender.Request
		if err := database.Decode(raw, &r); err != nil {
			return err
		}

		out = &r

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sender: get %s: %w", id, err)
	}

	return out, nil
}

// List returns every request of a project, newest first. Whole, because
// the history is bounded by what a person typed.
func (s *Store) List(ctx context.Context, projectID string) ([]sender.Request, error) {
	out := []sender.Request{}

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		// Keys are UUIDv7, so walking backwards is newest first.
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var r sender.Request
			if err := database.Decode(v, &r); err != nil {
				return err
			}

			out = append(out, r)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sender: list: %w", err)
	}

	return out, nil
}

// Delete removes one request.
func (s *Store) Delete(ctx context.Context, projectID, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(projectID)...)
		if err != nil {
			return err
		}

		if b.Get([]byte(id)) == nil {
			return ErrNotFound
		}

		return b.Delete([]byte(id))
	})
}

// Clear drops every request of a project.
func (s *Store) Clear(ctx context.Context, projectID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		parent, err := database.Bucket(tx, project.BucketData, []byte(projectID))
		if err != nil {
			return err
		}

		if parent.Bucket(BucketRequests) == nil {
			return nil
		}

		return parent.DeleteBucket(BucketRequests)
	})
}
