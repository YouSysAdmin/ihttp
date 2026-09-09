package database

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

func TestBodyIsBytesInARecord(t *testing.T) {
	type rec struct {
		B httpmsg.Body `json:"b"`
	}

	raw := httpmsg.Body{0xff, 0x00, 'a'}

	data, err := Encode(rec{B: raw})
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != `{"b":"/wBh"}` {
		t.Fatalf("stored form must be base64, got %s", data)
	}

	var back rec
	if err := Decode(data, &back); err != nil {
		t.Fatal(err)
	}

	if string(back.B) != string(raw) {
		t.Fatalf("round trip lost bytes: %v", back.B)
	}
}

func TestPageWalksNewestFirst(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	err = db.Update(func(tx *bolt.Tx) error {
		b, err := Bucket(tx, []byte("p"), []byte("logs"))
		if err != nil {
			return err
		}

		for _, k := range []string{"a", "b", "c", "d", "e"} {
			if err := b.Put([]byte(k), []byte(k)); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *bolt.Tx) error {
		b, err := Bucket(tx, []byte("p"), []byte("logs"))
		if err != nil {
			return err
		}

		var got []string
		more, err := Page(b, nil, 2, func(k, _ []byte) (bool, error) {
			got = append(got, string(k))

			return true, nil
		})
		if err != nil || !more || len(got) != 2 || got[0] != "e" || got[1] != "d" {
			t.Fatalf("first page: %v more=%v err=%v", got, more, err)
		}

		got = nil
		more, err = Page(b, []byte("d"), 10, func(k, _ []byte) (bool, error) {
			got = append(got, string(k))

			return true, nil
		})
		if err != nil || more || len(got) != 3 || got[0] != "c" {
			t.Fatalf("second page: %v more=%v err=%v", got, more, err)
		}

		if missing, _ := Bucket(tx, []byte("nope")); missing != nil {
			t.Fatal("a missing bucket reads as nil")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
