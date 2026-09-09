package protoschema

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/protoschema"
)

// Store reads and writes a project's schema bucket.
type Store struct {
	db *bolt.DB
}

// NewStore builds a Store over db.
func NewStore(db *bolt.DB) *Store {
	return &Store{db: db}
}

func bucketPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketSchemas}
}

// Put writes or rewrites a schema.
func (s *Store) Put(ctx context.Context, sc protoschema.Schema) error {
	data, err := database.Encode(sc)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(sc.ProjectID)...)
		if err != nil {
			return err
		}

		return b.Put([]byte(sc.ID), data)
	})
}

// Replace writes sc and removes the schemas named by drop in one
// transaction, so a re-upload never leaves the project without the file.
func (s *Store) Replace(ctx context.Context, drop []string, sc protoschema.Schema) error {
	data, err := database.Encode(sc)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(sc.ProjectID)...)
		if err != nil {
			return err
		}

		for _, id := range drop {
			if err := b.Delete([]byte(id)); err != nil {
				return err
			}
		}

		return b.Put([]byte(sc.ID), data)
	})
}

// List returns every schema of a project, oldest first.
func (s *Store) List(ctx context.Context, projectID string) ([]protoschema.Schema, error) {
	out := []protoschema.Schema{}

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		return b.ForEach(func(_, v []byte) error {
			var sc protoschema.Schema
			if err := database.Decode(v, &sc); err != nil {
				return err
			}

			out = append(out, sc)

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("protoschema: list: %w", err)
	}

	return out, nil
}

// Delete removes one schema. A missing one is ErrNotFound.
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
