package blobstore_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/blobstore"
)

func TestPutGetDelete(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "blobs")

	s, err := blobstore.New(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}

	body := bytes.Repeat([]byte("x"), 4096)

	ref, err := s.Put("project-1", "entry-1.response", body)
	if err != nil {
		t.Fatal(err)
	}

	if ref != "project-1/entry-1.response" {
		t.Fatalf("ref %q", ref)
	}

	back, err := s.Get(ref)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(back, body) {
		t.Fatalf("read back %d bytes, wrote %d", len(back), len(body))
	}

	// Rewriting replaces the file rather than leaving another behind.
	if _, err := s.Put("project-1", "entry-1.response", []byte("smaller")); err != nil {
		t.Fatal(err)
	}

	files, _ := os.ReadDir(filepath.Join(dir, "project-1"))
	if len(files) != 1 {
		t.Errorf("%d files after a rewrite, want 1", len(files))
	}

	s.Delete(ref)

	if _, err := s.Get(ref); err == nil {
		t.Error("a deleted body still reads")
	}

	// Deleting what is already gone is how this is normally called.
	s.Delete(ref, "project-1/never-existed")
}

func TestThresholdOffAndOn(t *testing.T) {
	t.Parallel()

	off, err := blobstore.New(filepath.Join(t.TempDir(), "blobs"), 0)
	if err != nil {
		t.Fatal(err)
	}

	if off.Offloads(1 << 30) {
		t.Error("offloading is on with a threshold of zero")
	}

	if _, err := off.Put("p", "e", []byte("x")); err == nil {
		t.Error("a store with offloading off accepted a body")
	}

	on, err := blobstore.New(filepath.Join(t.TempDir(), "blobs"), 1024)
	if err != nil {
		t.Fatal(err)
	}

	if on.Offloads(1023) {
		t.Error("a body under the threshold was offloaded")
	}

	if !on.Offloads(1024) {
		t.Error("a body at the threshold was not offloaded")
	}

	// A nil store is the "not configured" case callers hold, and every
	// method has to survive it.
	var nilStore *blobstore.Store

	if nilStore.Offloads(1 << 30) {
		t.Error("a nil store offloads")
	}

	nilStore.Delete("p/e")
	nilStore.DeleteGroup("p")

	if n, err := nilStore.Sweep(func(string) bool { return true }); n != 0 || err != nil {
		t.Errorf("nil sweep: %d %v", n, err)
	}
}

// A reference that walks out of the directory is refused, in both
// directions: writing one and reading one.
func TestRefusesPathsThatEscape(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "blobs")

	s, err := blobstore.New(dir, 1)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ group, name string }{
		{group: "..", name: "e"},
		{group: "p", name: ".."},
		{group: "../..", name: "e"},
		{group: "p/q", name: "e"},
		{group: "p", name: "a/b"},
		{group: "", name: "e"},
	} {
		if _, err := s.Put(c.group, c.name, []byte("x")); err == nil {
			t.Errorf("Put(%q, %q) was accepted", c.group, c.name)
		}
	}

	for _, ref := range []string{"../outside", "p/../../outside", "nogroup", "p/a/b"} {
		if _, err := s.Get(ref); err == nil {
			t.Errorf("Get(%q) was accepted", ref)
		}
	}
}

// The sweep is what saves a database whose write crashed after the body
// was written: the file has no record, and nothing else will ever
// remove it.
func TestSweepRemovesWhatNoRecordClaims(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "blobs")

	s, err := blobstore.New(dir, 1)
	if err != nil {
		t.Fatal(err)
	}

	kept, err := s.Put("p1", "kept", []byte("keep me"))
	if err != nil {
		t.Fatal(err)
	}

	orphan, err := s.Put("p1", "orphan", []byte("nobody points here"))
	if err != nil {
		t.Fatal(err)
	}

	// A half-written file, as a crash would leave it.
	if err := os.WriteFile(filepath.Join(dir, "p1", "entry.part"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := s.Sweep(func(ref string) bool { return ref == kept })
	if err != nil {
		t.Fatal(err)
	}

	if removed != 2 {
		t.Errorf("swept %d, want the orphan and the half-written file", removed)
	}

	if _, err := s.Get(kept); err != nil {
		t.Errorf("the claimed body went: %v", err)
	}

	if _, err := s.Get(orphan); err == nil {
		t.Error("the orphan is still there")
	}

	// A whole project's worth goes at once when the project does.
	s.DeleteGroup("p1")

	if _, err := s.Get(kept); err == nil {
		t.Error("DeleteGroup left the project's bodies")
	}
}
