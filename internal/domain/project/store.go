package project

import (
	"context"
	"fmt"
	"slices"
	"strings"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/models/project"
)

// BucketProjects holds one record per project, keyed by id.
var BucketProjects = []byte("projects")

// BucketData is the parent of every per-project bucket: BucketData/<id>/
// holds everything the project owns, so deleting the project is deleting
// one bucket.
var BucketData = []byte("project_data")

// BucketMeta holds process-level facts worth keeping across restarts,
// today only which project was open.
var BucketMeta = []byte("meta")

var keyActiveProject = []byte("active_project")

// Store reads and writes the projects bucket.
type Store struct {
	db *bolt.DB
}

// NewStore builds a Store over db.
func NewStore(db *bolt.DB) *Store {
	return &Store{db: db}
}

// List returns every project, newest first.
func (s *Store) List(ctx context.Context) ([]project.Project, error) {
	out := []project.Project{}

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketProjects)
		if err != nil || b == nil {
			return err
		}

		return b.ForEach(func(_, v []byte) error {
			var p project.Project
			if err := database.Decode(v, &p); err != nil {
				return err
			}

			out = append(out, p)

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}

	slices.Reverse(out)

	return out, nil
}

// Get returns one project, or (nil, nil) when there is none with that id.
func (s *Store) Get(ctx context.Context, id string) (*project.Project, error) {
	var out *project.Project

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketProjects)
		if err != nil || b == nil {
			return err
		}

		v := b.Get([]byte(id))
		if v == nil {
			return nil
		}

		var p project.Project
		if err := database.Decode(v, &p); err != nil {
			return err
		}

		out = &p

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("project: get %s: %w", id, err)
	}

	return out, nil
}

// Insert writes a new project. A duplicate name, compared without regard
// to case, is ErrNameTaken.
func (s *Store) Insert(ctx context.Context, p project.Project) error {
	data, err := database.Encode(p)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketProjects)
		if err != nil {
			return err
		}

		taken := false
		_ = b.ForEach(func(_, v []byte) error {
			var other project.Project
			if database.Decode(v, &other) == nil && strings.EqualFold(other.Name, p.Name) {
				taken = true
			}

			return nil
		})

		if taken {
			return ErrNameTaken
		}

		return b.Put([]byte(p.ID), data)
	})
}

// UpdateSettings rewrites the project record. It is the only writer of
// a project after Insert.
func (s *Store) UpdateSettings(ctx context.Context, p project.Project) error {
	data, err := database.Encode(p)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketProjects)
		if err != nil {
			return err
		}

		if b.Get([]byte(p.ID)) == nil {
			return ErrNotFound
		}

		return b.Put([]byte(p.ID), data)
	})
}

// Delete removes a project and everything stored under it.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketProjects)
		if err != nil {
			return err
		}

		if b.Get([]byte(id)) == nil {
			return ErrNotFound
		}

		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("project: delete: %w", err)
		}

		data, err := database.Bucket(tx, BucketData)
		if err != nil {
			return err
		}

		if data.Bucket([]byte(id)) != nil {
			if err := data.DeleteBucket([]byte(id)); err != nil {
				return fmt.Errorf("project: delete data: %w", err)
			}
		}

		return nil
	})
}

// SetActiveID remembers which project is open, or forgets it for "".
func (s *Store) SetActiveID(ctx context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketMeta)
		if err != nil {
			return err
		}

		if id == "" {
			return b.Delete(keyActiveProject)
		}

		return b.Put(keyActiveProject, []byte(id))
	})
}

// ActiveID returns the remembered open project, or "".
func (s *Store) ActiveID(ctx context.Context) (string, error) {
	var id string

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketMeta)
		if err != nil || b == nil {
			return err
		}

		id = string(b.Get(keyActiveProject))

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("project: read active id: %w", err)
	}

	return id, nil
}
