package instance

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/database"
	instancemodels "github.com/yousysadmin/ihttp/internal/models/instance"
)

// BucketInstance holds what this machine is set to. Top level, beside
// projects and upstreams: none of it belongs to a project.
var BucketInstance = []byte("instance")

var keySettings = []byte("settings")

// Store reads and writes the one settings document.
type Store struct {
	db *bolt.DB
}

// NewStore builds a Store over db.
func NewStore(db *bolt.DB) *Store {
	return &Store{db: db}
}

// Get reads the settings. A database that has never been written to
// answers the zero document rather than an error - no settings is a
// state, not a failure.
func (s *Store) Get(ctx context.Context) (instancemodels.Settings, error) {
	if err := ctx.Err(); err != nil {
		return instancemodels.Settings{}, err
	}

	var out instancemodels.Settings

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketInstance)
		if err != nil || b == nil {
			return err
		}

		data := b.Get(keySettings)
		if data == nil {
			return nil
		}

		return database.Decode(data, &out)
	})
	if err != nil {
		return instancemodels.Settings{}, fmt.Errorf("instance: read settings: %w", err)
	}

	return out, nil
}

// Put writes the settings whole.
func (s *Store) Put(ctx context.Context, set instancemodels.Settings) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := database.Encode(set)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketInstance)
		if err != nil {
			return err
		}

		return b.Put(keySettings, data)
	})
}
