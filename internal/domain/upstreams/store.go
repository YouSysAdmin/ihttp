package upstreams

import (
	"context"
	"errors"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/models/upstream"
)

// BucketServers is the instance's list of upstream proxies. Top level,
// beside projects: which ways out this machine has is a fact about the
// machine, and a project only points at one of them.
var BucketServers = []byte("upstreams")

// ErrNotFound is a server that is not in the list.
var ErrNotFound = errors.New("upstreams: proxy not found")

// Store reads and writes the list.
type Store struct {
	db *bolt.DB
}

// NewStore builds a Store over db.
func NewStore(db *bolt.DB) *Store {
	return &Store{db: db}
}

// List returns every server, oldest first, which is the order they were
// added in.
func (s *Store) List(ctx context.Context) ([]upstream.Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var out []upstream.Server

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketServers)
		if b == nil {
			return nil
		}

		return b.ForEach(func(_, v []byte) error {
			if v == nil {
				return nil
			}

			var srv upstream.Server
			if err := database.Decode(v, &srv); err != nil {
				return err
			}

			out = append(out, srv)

			return nil
		})
	})

	return out, err
}

// Get reads one server.
func (s *Store) Get(ctx context.Context, id string) (upstream.Server, error) {
	if err := ctx.Err(); err != nil {
		return upstream.Server{}, err
	}

	var out upstream.Server

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketServers)
		if b == nil {
			return ErrNotFound
		}

		data := b.Get([]byte(id))
		if data == nil {
			return ErrNotFound
		}

		return database.Decode(data, &out)
	})

	return out, err
}

// Put writes or rewrites a server.
func (s *Store) Put(ctx context.Context, srv upstream.Server) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := database.Encode(srv)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, BucketServers)
		if err != nil {
			return err
		}

		return b.Put([]byte(srv.ID), data)
	})
}

// Delete removes a server. A missing one is ErrNotFound.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketServers)
		if b == nil || b.Get([]byte(id)) == nil {
			return ErrNotFound
		}

		return b.Delete([]byte(id))
	})
}
