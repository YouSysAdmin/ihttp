package reqlog_test

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	domainproject "github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/models/project"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// storedRecord is the raw record as the database holds it, which is
// what this file is about: the body must NOT be in there.
type storedRecord struct {
	Body     string `json:"body"`
	BodyRef  string `json:"body_ref"`
	Response *struct {
		Body    string `json:"body"`
		BodyRef string `json:"body_ref"`
	} `json:"response"`
}

func rawRecord(t *testing.T, stack *testutil.Stack, projectID, id string) storedRecord {
	t.Helper()

	var out storedRecord

	err := stack.DB.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(domainproject.BucketData)
		if data == nil {
			t.Fatal("no data bucket")
		}

		pb := data.Bucket([]byte(projectID))
		if pb == nil {
			t.Fatal("no bucket for the project")
		}

		logs := pb.Bucket(reqlog.BucketLogs)
		if logs == nil {
			t.Fatal("no log bucket")
		}

		raw := logs.Get([]byte(id))
		if raw == nil {
			t.Fatalf("no record for %s", id)
		}

		return json.Unmarshal(raw, &out)
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// A body past the threshold is kept in a file, and everything above the
// store still sees a whole entry: the reader, the filter language and
// an export.
func TestLargeBodiesAreKeptBesideTheDatabase(t *testing.T) {
	// A threshold small enough to reach with a test body.
	stack := testutil.New(t, testutil.WithBodyBlobs(2048))
	stack.OpenProject(t, "blobs")

	big := strings.Repeat("needle-in-here ", 400) // ~6 KiB.

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(big))
	}))
	defer target.Close()

	res, err := stack.Client().Post(target.URL+"/big", "text/plain", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if string(body) != big {
		t.Fatal("the client did not get the body it was sent")
	}

	var id string

	testutil.Eventually(t, 4*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 5})
		if len(entries) != 1 || entries[0].StatusCode == 0 {
			return false
		}

		id = entries[0].ID

		return true
	}, "the exchange is logged with its response")

	projectID := stack.Projects.ActiveID()

	// The record holds references, not bytes. This is the whole point.
	raw := rawRecord(t, stack, projectID, id)

	if raw.Body != "" {
		t.Errorf("the request body is in the database: %d bytes", len(raw.Body))
	}

	if raw.BodyRef == "" {
		t.Error("the request body was not offloaded")
	}

	if raw.Response == nil || raw.Response.Body != "" {
		t.Errorf("the response body is in the database: %+v", raw.Response)
	}

	if raw.Response == nil || raw.Response.BodyRef == "" {
		t.Error("the response body was not offloaded")
	}

	// And yet a read gives back a whole entry, with no reference left
	// on it for anything above the store to trip over.
	full, err := stack.ReqLogs.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}

	if string(full.Body) != big || string(full.Response.Body) != big {
		t.Fatalf("read back %d and %d bytes, want %d each",
			len(full.Body), len(full.Response.Body), len(big))
	}

	if full.BodyRef != "" || full.Response.BodyRef != "" {
		t.Error("a reference reached a caller above the store")
	}

	// The list is summarised from the bodies, so the sizes must be
	// real rather than zero.
	entries, _, err := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}

	if entries[0].ResSize != len(big) || entries[0].ReqSize != len(big) {
		t.Errorf("row sizes %d and %d, want %d", entries[0].ReqSize, entries[0].ResSize, len(big))
	}

	// A filter that reads a body still matches one that is in a file.
	found, _, err := stack.ReqLogs.List(t.Context(), reqlog.ListParams{
		Limit:  5,
		Search: `res.body contains "needle-in-here"`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(found) != 1 {
		t.Errorf("%d entries match a body search, want 1", len(found))
	}

	// An export carries the bytes too.
	_, write, err := stack.ReqLogs.ExportHAR(t.Context(), reqlog.ListParams{})
	if err != nil {
		t.Fatal(err)
	}

	var har bytes.Buffer
	if err := write(&har); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(har.String(), "needle-in-here") {
		t.Error("the HAR export has no body in it")
	}

	// Deleting the entry takes its files with it.
	refs := []string{raw.BodyRef, raw.Response.BodyRef}

	if err := stack.ReqLogs.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}

	for _, ref := range refs {
		if _, err := os.Stat(blobPath(t, stack, ref)); !os.IsNotExist(err) {
			t.Errorf("%s survived the delete: %v", ref, err)
		}
	}
}

// blobPath is where a reference lands on disk. The store keeps the
// directory to itself, so a test that checks the file is really gone
// has to rebuild the path - which is also a check that the layout is
// what it says.
func blobPath(t *testing.T, stack *testutil.Stack, ref string) string {
	t.Helper()

	dir := stack.BodyDir
	if dir == "" {
		t.Fatal("the stack does not say where bodies are kept")
	}

	return filepath.Join(dir, filepath.FromSlash(ref))
}

// Retention drops the oldest entries, and their bodies have to go with
// them or the files are exactly what the cap failed to reclaim.
func TestTrimRemovesTheBodiesItDrops(t *testing.T) {
	stack := testutil.New(t, testutil.WithBodyBlobs(1024))
	stack.OpenProject(t, "trim-blobs")

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.RequestLog.MaxEntries = 2

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	big := strings.Repeat("x", 4096)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer target.Close()

	client := stack.Client()

	for i := range 5 {
		res, err := client.Get(target.URL + "/" + string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}

	testutil.Eventually(t, 4*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 20})

		return len(entries) == 5
	}, "all five are logged")

	if _, err := stack.ReqLogs.Trim(t.Context()); err != nil {
		t.Fatal(err)
	}

	var entries []reqlogmodels.Summary

	testutil.Eventually(t, 2*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 20})

		return len(entries) == 2
	}, "the trim leaves two")

	// One file per surviving response, and nothing else.
	files, err := os.ReadDir(filepath.Join(stack.BodyDir, stack.Projects.ActiveID()))
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != len(entries) {
		names := make([]string, 0, len(files))
		for _, f := range files {
			names = append(names, f.Name())
		}

		t.Errorf("%d body files for %d entries: %v", len(files), len(entries), names)
	}

	// And what is left still reads.
	full, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(full.Response.Body) != len(big) {
		t.Errorf("a surviving body reads back as %d bytes", len(full.Response.Body))
	}
}

// A body file whose record never committed is nobody's. Nothing else
// will remove it, so the sweep does.
func TestSweepRemovesOrphanedBodies(t *testing.T) {
	stack := testutil.New(t, testutil.WithBodyBlobs(1024))
	stack.OpenProject(t, "sweep-blobs")

	projectID := stack.Projects.ActiveID()

	dir := filepath.Join(stack.BodyDir, projectID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	orphan := filepath.Join(dir, "01a00000-0000-7000-8000-000000000000.response")
	if err := os.WriteFile(orphan, []byte("nobody points here"), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := stack.LogStore.SweepBodies(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if n != 1 {
		t.Errorf("swept %d files, want the one orphan", n)
	}

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the orphan is still there: %v", err)
	}
}
