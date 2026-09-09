package reqlog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/core/blobstore"
	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// Store reads and writes a project's log bucket.
//
// A body past the blob store's threshold is kept in a FILE beside the
// database rather than in it, and this store is the only thing that
// knows: it writes the reference in place of the bytes and puts the
// bytes back on every read. See blobs.go.
type Store struct {
	db    *bolt.DB
	blobs *blobstore.Store
}

// NewStore builds a Store over db. blobs may be nil, and then every
// body stays in the record.
func NewStore(db *bolt.DB, blobs *blobstore.Store) *Store {
	return &Store{db: db, blobs: blobs}
}

func bucketPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketLogs}
}

func messagesPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketMessages}
}

func messageIndexPath(projectID string) [][]byte {
	return [][]byte{project.BucketData, []byte(projectID), BucketMessageIndex}
}

// messageKey orders messages within an entry.
func messageKey(entryID string, seq int) []byte {
	return fmt.Appendf(nil, "%s:%08d", entryID, seq)
}

func messagePrefix(entryID string) []byte {
	return []byte(entryID + ":")
}

// Put writes or rewrites an entry. A body past the threshold goes to a
// file first, so what reaches the database is a record.
func (s *Store) Put(ctx context.Context, e reqlog.Entry) error {
	stale, err := s.offload(&e)
	if err != nil {
		return err
	}

	data, err := database.Encode(e)
	if err != nil {
		return err
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(e.ProjectID)...)
		if err != nil {
			return err
		}

		return b.Put([]byte(e.ID), data)
	})
	if err != nil {
		return err
	}

	// Only once the record that stopped pointing at them is committed.
	s.blobs.Delete(stale...)

	return nil
}

// SetResponse attaches the response to an entry. The entry must exist:
// a response for a request that was never logged is ErrNotFound.
func (s *Store) SetResponse(ctx context.Context, projectID, id string, res reqlog.Response) error {
	return s.Update(ctx, projectID, id, func(e *reqlog.Entry) error {
		e.Response = &res

		return nil
	})
}

// Update rewrites one entry under change, in one transaction. A missing
// entry is ErrNotFound and change is not called.
func (s *Store) Update(ctx context.Context, projectID, id string, change func(*reqlog.Entry) error) error {
	var stale []string

	err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(projectID)...)
		if err != nil {
			return err
		}

		raw := b.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}

		var e reqlog.Entry
		if err := database.Decode(raw, &e); err != nil {
			return err
		}

		// change is handed a whole entry, bodies and all, the way every
		// caller above the store expects one.
		if err := s.hydrate(&e); err != nil {
			return err
		}

		if err := change(&e); err != nil {
			return err
		}

		stale, err = s.offload(&e)
		if err != nil {
			return err
		}

		data, err := database.Encode(e)
		if err != nil {
			return err
		}

		return b.Put([]byte(id), data)
	})
	if err != nil {
		return err
	}

	s.blobs.Delete(stale...)

	return nil
}

// Get returns one entry, or (nil, nil) when the project has none by id.
func (s *Store) Get(ctx context.Context, projectID, id string) (*reqlog.Entry, error) {
	var out *reqlog.Entry

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		raw := b.Get([]byte(id))
		if raw == nil {
			return nil
		}

		var e reqlog.Entry
		if err := database.Decode(raw, &e); err != nil {
			return err
		}

		out = &e

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reqlog: get %s: %w", id, err)
	}

	if out != nil {
		if err := s.hydrate(out); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// Walk visits entries newest-first starting before the given id, at most
// limit of them, and reports whether more remain. keep returns false for
// an entry the caller does not want counted - a filtered-out one - and
// stop reports that the caller has enough.
func (s *Store) Walk(ctx context.Context, projectID, before string, limit int, fn func(reqlog.Entry) (keep, stop bool, err error)) (bool, error) {
	more := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, bucketPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		kept := 0
		c := b.Cursor()

		var k, v []byte
		if before == "" {
			k, v = c.Last()
		} else {
			k, _ = c.Seek([]byte(before))
			if k == nil {
				k, v = c.Last()
			} else {
				k, v = c.Prev()
			}
		}

		for ; k != nil; k, v = c.Prev() {
			if err := ctx.Err(); err != nil {
				return err
			}

			if kept == limit {
				more = true

				return nil
			}

			var e reqlog.Entry
			if err := database.Decode(v, &e); err != nil {
				return err
			}

			// A page of the log is matched against a filter that may
			// read a body, and summarised into sizes that come from
			// one, so the bytes have to be here.
			if err := s.hydrate(&e); err != nil {
				return err
			}

			keep, stop, err := fn(e)
			if err != nil {
				return err
			}

			if keep {
				kept++
			}

			if stop {
				more = true

				return nil
			}
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("reqlog: walk: %w", err)
	}

	return more, nil
}

// eachPage is how many entries one Each transaction reads. Short
// transactions keep a long export from holding the database's read lock
// while a slow client downloads.
const eachPage = 200

// Each visits every entry of a project oldest-first, the order an export
// wants, and stops at the first error fn returns. Entries are read a
// page at a time and fn runs between transactions.
//
// Bodies are whole: an offloaded one is read back before fn sees it.
func (s *Store) Each(ctx context.Context, projectID string, fn func(reqlog.Entry) error) error {
	return s.each(ctx, projectID, true, fn)
}

// EachBodyless is Each without reading offloaded bodies back, for a
// caller that provably needs none of them.
//
// Only three do - counting tags, counting hosts, and collecting the ids
// a Clear will delete - and each one reads the marks, the URL or the id
// and nothing else. USE Each UNLESS YOU HAVE CHECKED: an entry from
// here has an empty body where a big one was, and code that measures a
// body would report zero rather than fail.
func (s *Store) EachBodyless(ctx context.Context, projectID string, fn func(reqlog.Entry) error) error {
	return s.each(ctx, projectID, false, fn)
}

func (s *Store) each(ctx context.Context, projectID string, bodies bool, fn func(reqlog.Entry) error) error {
	var after []byte

	for {
		var page []reqlog.Entry

		err := s.db.View(func(tx *bolt.Tx) error {
			b, err := database.Bucket(tx, bucketPath(projectID)...)
			if err != nil || b == nil {
				return err
			}

			c := b.Cursor()

			var k, v []byte
			if after == nil {
				k, v = c.First()
			} else {
				k, v = c.Seek(after)
				if bytes.Equal(k, after) {
					k, v = c.Next()
				}
			}

			for ; k != nil && len(page) < eachPage; k, v = c.Next() {
				var e reqlog.Entry
				if err := database.Decode(v, &e); err != nil {
					return err
				}

				page = append(page, e)
				after = slices.Clone(k)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("reqlog: each: %w", err)
		}

		for _, e := range page {
			if err := ctx.Err(); err != nil {
				return err
			}

			if bodies {
				if err := s.hydrate(&e); err != nil {
					return err
				}
			}

			if err := fn(e); err != nil {
				return err
			}
		}

		if len(page) < eachPage {
			return nil
		}
	}
}

// Delete removes one entry and the WebSocket messages it owns. A missing
// entry is ErrNotFound.
func (s *Store) Delete(ctx context.Context, projectID, id string) error {
	var refs []string

	err := s.db.Update(func(tx *bolt.Tx) error {
		var err error
		refs, err = deleteEntryTx(tx, projectID, id)

		return err
	})
	if err != nil {
		return err
	}

	s.blobs.Delete(refs...)

	return nil
}

// DeleteMany removes the given entries and their messages in one
// transaction, skipping ids that are already gone.
func (s *Store) DeleteMany(ctx context.Context, projectID string, ids []string) error {
	var refs []string

	err := s.db.Update(func(tx *bolt.Tx) error {
		bs, err := entryBucketsOf(tx, projectID)
		if err != nil {
			return err
		}

		for _, id := range ids {
			gone, err := bs.deleteEntry(id)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}

			refs = append(refs, gone...)
		}

		return nil
	})
	if err != nil {
		return err
	}

	s.blobs.Delete(refs...)

	return nil
}

// entryBuckets are the three buckets an entry lives in, resolved once
// per transaction.
type entryBuckets struct {
	entries  *bolt.Bucket
	messages [2]*bolt.Bucket
}

func entryBucketsOf(tx *bolt.Tx, projectID string) (entryBuckets, error) {
	var bs entryBuckets

	b, err := database.Bucket(tx, bucketPath(projectID)...)
	if err != nil {
		return bs, err
	}

	bs.entries = b

	for i, path := range [][][]byte{messagesPath(projectID), messageIndexPath(projectID)} {
		mb, err := database.Bucket(tx, path...)
		if err != nil {
			return bs, err
		}

		bs.messages[i] = mb
	}

	return bs, nil
}

func deleteEntryTx(tx *bolt.Tx, projectID, id string) ([]string, error) {
	bs, err := entryBucketsOf(tx, projectID)
	if err != nil {
		return nil, err
	}

	return bs.deleteEntry(id)
}

// deleteEntry removes an entry and its messages, and returns the body
// files it stopped pointing at so the caller can remove them once the
// transaction has committed. Deleting the files first would lose them
// if the transaction then failed.
func (bs entryBuckets) deleteEntry(id string) ([]string, error) {
	raw := bs.entries.Get([]byte(id))
	if raw == nil {
		return nil, ErrNotFound
	}

	refs := refsOf(raw)

	if err := bs.entries.Delete([]byte(id)); err != nil {
		return nil, err
	}

	for _, mb := range bs.messages {
		c := mb.Cursor()
		prefix := messagePrefix(id)

		var keys [][]byte
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			keys = append(keys, slices.Clone(k))
		}

		for _, k := range keys {
			if err := mb.Delete(k); err != nil {
				return nil, err
			}
		}
	}

	return refs, nil
}

// PutMessages writes WebSocket messages and their index rows in one
// transaction.
func (s *Store) PutMessages(ctx context.Context, projectID string, msgs []reqlog.Message) error {
	type record struct {
		key, data, row []byte
	}

	records := make([]record, 0, len(msgs))
	for _, m := range msgs {
		data, err := database.Encode(m)
		if err != nil {
			return err
		}

		row, err := database.Encode(m.Summarize())
		if err != nil {
			return err
		}

		records = append(records, record{key: messageKey(m.EntryID, m.Seq), data: data, row: row})
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		full, err := database.Bucket(tx, messagesPath(projectID)...)
		if err != nil {
			return err
		}

		index, err := database.Bucket(tx, messageIndexPath(projectID)...)
		if err != nil {
			return err
		}

		for _, r := range records {
			if err := full.Put(r.key, r.data); err != nil {
				return err
			}

			if err := index.Put(r.key, r.row); err != nil {
				return err
			}
		}

		return nil
	})
}

// ListMessages returns the summaries of an entry's messages after seq
// afterSeq, at most limit of them, and whether more remain.
func (s *Store) ListMessages(ctx context.Context, projectID, entryID string, afterSeq, limit int) ([]reqlog.MessageSummary, bool, error) {
	out := []reqlog.MessageSummary{}
	more := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, messageIndexPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		prefix := messagePrefix(entryID)
		c := b.Cursor()

		for k, v := c.Seek(messageKey(entryID, afterSeq+1)); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			if len(out) == limit {
				more = true

				return nil
			}

			var row reqlog.MessageSummary
			if err := database.Decode(v, &row); err != nil {
				return err
			}

			out = append(out, row)
		}

		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("reqlog: list messages: %w", err)
	}

	return out, more, nil
}

// ListMessagesBefore returns the summaries of an entry's messages newest
// first, starting below seq beforeSeq, or from the newest when it is 0,
// at most limit of them, and whether older ones remain.
func (s *Store) ListMessagesBefore(ctx context.Context, projectID, entryID string, beforeSeq, limit int) ([]reqlog.MessageSummary, bool, error) {
	out := []reqlog.MessageSummary{}
	more := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, messageIndexPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		prefix := messagePrefix(entryID)
		c := b.Cursor()

		// Land on the first key past the range, then step back onto the
		// newest message wanted.
		var k, v []byte
		if beforeSeq > 0 {
			c.Seek(messageKey(entryID, beforeSeq))
			k, v = c.Prev()
		} else {
			if k, _ = c.Seek(append(bytes.Clone(prefix), 0xff)); k == nil {
				k, v = c.Last()
			} else {
				k, v = c.Prev()
			}
		}

		for ; k != nil && bytes.HasPrefix(k, prefix); k, v = c.Prev() {
			if len(out) == limit {
				more = true

				return nil
			}

			var row reqlog.MessageSummary
			if err := database.Decode(v, &row); err != nil {
				return err
			}

			out = append(out, row)
		}

		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("reqlog: list messages: %w", err)
	}

	return out, more, nil
}

// GetMessage returns one message by entry and seq, or (nil, nil).
func (s *Store) GetMessage(ctx context.Context, projectID, entryID string, seq int) (*reqlog.Message, error) {
	var out *reqlog.Message

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, messagesPath(projectID)...)
		if err != nil || b == nil {
			return err
		}

		raw := b.Get(messageKey(entryID, seq))
		if raw == nil {
			return nil
		}

		var m reqlog.Message
		if err := database.Decode(raw, &m); err != nil {
			return err
		}

		out = &m

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reqlog: get message: %w", err)
	}

	return out, nil
}

// Clear drops every entry of a project, and the messages with them.
func (s *Store) Clear(ctx context.Context, projectID string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		parent, err := database.Bucket(tx, project.BucketData, []byte(projectID))
		if err != nil {
			return err
		}

		for _, name := range [][]byte{BucketLogs, BucketMessages, BucketMessageIndex} {
			if parent.Bucket(name) == nil {
				continue
			}

			if err := parent.DeleteBucket(name); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Every entry of the project has gone, so every body file of it is
	// nobody's. The whole group goes rather than one file at a time.
	s.blobs.DeleteGroup(projectID)

	return nil
}

// savedFlag reads one member out of a stored entry. Decoding the whole
// entry to learn a bool would carry every body with it, and a trim only
// needs to know which rows it may not touch.
type savedFlag struct {
	Saved bool `json:"saved"`
}

// Trim drops the oldest entries once the log holds more than max, and
// returns how many it dropped. Zero max is no cap.
//
// Saved entries count towards the cap but are never dropped - that is
// what saving one is for - so a cap below the number of saved entries
// simply stops evicting rather than deleting what was kept on purpose.
//
// The count is a key-only walk, which reads no bodies. Only the rows
// actually being considered for eviction are decoded, and only far
// enough to read the saved flag, so the cost is the number dropped
// rather than the size of the log.
//
// Rows go through the same deleteEntry as Delete and DeleteMany, so an
// evicted entry's WebSocket messages leave both message buckets with
// it rather than being orphaned there.
func (s *Store) Trim(ctx context.Context, projectID string, max int) (int, error) {
	if max <= 0 {
		return 0, ctx.Err()
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	dropped := 0

	var refs []string

	err := s.db.Update(func(tx *bolt.Tx) error {
		bs, err := entryBucketsOf(tx, projectID)
		if err != nil {
			return err
		}

		total := 0

		c := bs.entries.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			// A nested bucket has no value and is not an entry.
			if v == nil {
				continue
			}

			total++
		}

		excess := total - max
		if excess <= 0 {
			return nil
		}

		// Oldest first: the key is a UUIDv7, so key order is time order.
		var victims []string

		c = bs.entries.Cursor()
		for k, v := c.First(); k != nil && len(victims) < excess; k, v = c.Next() {
			if v == nil {
				continue
			}

			var flag savedFlag
			if err := database.Decode(v, &flag); err != nil {
				// An entry that cannot be read is exactly the kind of
				// row a trim should carry away.
				victims = append(victims, string(slices.Clone(k)))

				continue
			}

			if flag.Saved {
				continue
			}

			victims = append(victims, string(slices.Clone(k)))
		}

		for _, id := range victims {
			gone, err := bs.deleteEntry(id)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}

			refs = append(refs, gone...)
			dropped++
		}

		return nil
	})
	if err != nil {
		return dropped, err
	}

	// Retention that left the bodies behind would keep exactly what it
	// is there to reclaim.
	s.blobs.Delete(refs...)

	return dropped, nil
}
