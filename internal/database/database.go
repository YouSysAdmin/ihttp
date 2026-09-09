// Package database opens the bbolt file every store reads and holds the
// two policies they share: how a record is encoded, and how a bucket is
// walked newest-first.
package database

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// ErrNotFound is a key no bucket holds.
var ErrNotFound = errors.New("database: not found")

// Open opens or creates the database at path, creating the directory
// when missing. The open is bounded: a second process holding the file
// makes bbolt wait for the lock forever otherwise, and the right answer
// to "ihttp is already running" is to say so.
func Open(ctx context.Context, path string) (*bolt.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("database: create %s: %w", filepath.Dir(path), err)
	}

	timeout := 3 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(dl))
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: timeout})
	if err != nil {
		if errors.Is(err, berrors.ErrTimeout) {
			return nil, fmt.Errorf("database: %s is locked - is another ihttp running?", path)
		}

		return nil, fmt.Errorf("database: open %s: %w", path, err)
	}

	return db, nil
}

// storedOptions is the record policy: deterministic so a rewrite of an
// unchanged record is byte-identical, tolerant of a byte off the network
// that is not UTF-8, and with message bodies kept as BYTES. On the wire a
// Body is a string for the console to edit, but a record must hold what
// actually passed through, so here the same type is base64.
var storedOptions = json.JoinOptions(
	json.Deterministic(true),
	jsontext.AllowInvalidUTF8(true),
	json.WithMarshalers(json.MarshalFunc(func(b httpmsg.Body) ([]byte, error) {
		return json.Marshal([]byte(b))
	})),
	json.WithUnmarshalers(json.UnmarshalFunc(func(data []byte, b *httpmsg.Body) error {
		var raw []byte
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}

		*b = httpmsg.Body(raw)

		return nil
	})),
)

// Encode renders a record for storage.
func Encode(v any) ([]byte, error) {
	out, err := json.Marshal(v, storedOptions)
	if err != nil {
		return nil, fmt.Errorf("database: encode %T: %w", v, err)
	}

	return out, nil
}

// Decode reads a stored record into v.
func Decode(data []byte, v any) error {
	if err := json.Unmarshal(data, v, storedOptions); err != nil {
		return fmt.Errorf("database: decode %T: %w", v, err)
	}

	return nil
}

// Bucket returns the nested bucket at path inside tx, creating each
// level when the transaction is writable and answering nil when a level
// is missing in a read-only one.
func Bucket(tx *bolt.Tx, path ...[]byte) (*bolt.Bucket, error) {
	var b *bolt.Bucket

	for i, name := range path {
		var err error

		switch {
		case b == nil && tx.Writable():
			b, err = tx.CreateBucketIfNotExists(name)
		case b == nil:
			b = tx.Bucket(name)
		case tx.Writable():
			b, err = b.CreateBucketIfNotExists(name)
		default:
			b = b.Bucket(name)
		}

		if err != nil {
			return nil, fmt.Errorf("database: bucket %q: %w", path[:i+1], err)
		}

		if b == nil {
			return nil, nil
		}
	}

	return b, nil
}

// Page walks b newest-first, starting strictly before key `before` (or
// from the end when before is empty), calling fn for up to limit
// records. fn returns false to stop early. The bool reports whether more
// records remain after the last one visited.
func Page(b *bolt.Bucket, before []byte, limit int, fn func(k, v []byte) (bool, error)) (bool, error) {
	if b == nil || limit <= 0 {
		return false, nil
	}

	c := b.Cursor()

	var k, v []byte
	if len(before) == 0 {
		k, v = c.Last()
	} else {
		k, _ = c.Seek(before)
		if k == nil {
			k, v = c.Last()
		} else {
			k, v = c.Prev()
		}
	}

	visited := 0

	for ; k != nil; k, v = c.Prev() {
		if v == nil {
			// A nested bucket, not a record.
			continue
		}

		if visited == limit {
			return true, nil
		}

		more, err := fn(k, v)
		if err != nil {
			return false, err
		}

		visited++

		if !more {
			return true, nil
		}
	}

	return false, nil
}
