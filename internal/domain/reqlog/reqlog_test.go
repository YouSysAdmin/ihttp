package reqlog_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/models/project"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

func TestIgnoreFilterKeepsMatchingRequestsOutOfTheLog(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "ignore")

	_, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.RequestLog.IgnoreFilter = `req.ext in (png, css) OR req.header.x-noise exists`

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.RequestLog.IgnoreFilter = `res.statusCode = 200`

		return nil
	})
	if err == nil {
		t.Fatal("an ignore filter over res.* must be refused")
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	client := stack.Client()

	for _, path := range []string{"/logo.png", "/site.css", "/api/data"} {
		res, err := client.Get(target.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
	}

	req, _ := http.NewRequest(http.MethodGet, target.URL+"/noisy", nil)
	req.Header.Set("X-Noise", "1")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	var entries []reqlogmodels.Summary
	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})

		return len(entries) == 1 && entries[0].StatusCode == 200
	}, "exactly one logged entry")

	if entries[0].URL != target.URL+"/api/data" {
		t.Fatalf("logged %s", entries[0].URL)
	}
}

func TestHARExportFollowsTheFilter(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "har")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	client := stack.Client()

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, _ := http.NewRequest(method, target.URL+"/"+method, nil)

		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})

		return len(entries) == 2 && entries[0].StatusCode != 0 && entries[1].StatusCode != 0
	}, "log fills with two answered entries")

	res, err := http.Get(stack.Console.URL + "/api/request-logs/export.har?search=" + url.QueryEscape("req.method = POST"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Disposition"), `attachment; filename="har.har"`) {
		t.Fatalf("status %d headers %v", res.StatusCode, res.Header)
	}

	var doc struct {
		Log struct {
			Entries []struct {
				Request struct {
					Method string `json:"method"`
				} `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}

	if err := json.UnmarshalRead(res.Body, &doc); err != nil {
		t.Fatal(err)
	}

	if len(doc.Log.Entries) != 1 || doc.Log.Entries[0].Request.Method != http.MethodPost {
		t.Fatalf("filtered export: %+v", doc.Log.Entries)
	}

	bad, err := http.Get(stack.Console.URL + "/api/request-logs/export.har?search=" + url.QueryEscape("req.method ="))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bad.Body.Close() }()

	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad filter answered %d", bad.StatusCode)
	}
}

func TestMarksAreSavedAndSearchable(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "marks")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	for range 2 {
		res, err := stack.Client().Get(target.URL + "/x")
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	entries, _, err := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})
	if err != nil || len(entries) != 2 {
		t.Fatalf("list: %v %v", entries, err)
	}

	id := entries[0].ID

	// patch sends one PATCH and answers with the status alone.
	patch := func(body string) int {
		req, _ := http.NewRequest(http.MethodPatch, stack.Console.URL+"/api/request-logs/"+id, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	if status := patch(`{"tags": [" todo ", "TODO", "auth-bug", ""], "color": "red"}`); status != http.StatusOK {
		t.Fatalf("patch answered %d", status)
	}

	e, err := stack.ReqLogs.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}

	if len(e.Tags) != 2 || e.Tags[0] != "todo" || e.Tags[1] != "auth-bug" || e.Color != "red" {
		t.Fatalf("marks not normalized: %+v", e)
	}

	// A note alone leaves the tags as they are.
	if status := patch(`{"note": "look here"}`); status != http.StatusOK {
		t.Fatalf("note patch answered %d", status)
	}

	e, _ = stack.ReqLogs.Get(t.Context(), id)
	if e.Note != "look here" || len(e.Tags) != 2 {
		t.Fatalf("note patch touched the tags: %+v", e)
	}

	if status := patch(`{"color": "pink"}`); status != http.StatusBadRequest {
		t.Fatalf("a bad color answered %d", status)
	}

	if status := patch(`{"tag": "x"}`); status != http.StatusBadRequest {
		t.Fatalf("an unknown member answered %d", status)
	}

	found, _, err := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Search: "req.tag = todo AND req.note contains look", Limit: 10})
	if err != nil || len(found) != 1 || found[0].ID != id || found[0].Color != "red" {
		t.Fatalf("search by marks: %+v %v", found, err)
	}

	tags, err := stack.ReqLogs.Tags(t.Context())
	if err != nil || len(tags) != 2 || tags[0].Count != 1 {
		t.Fatalf("tags: %+v %v", tags, err)
	}
}

func TestDeleteOneAndMatching(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "delete")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	for _, path := range []string{"/keep", "/junk/1", "/junk/2"} {
		res, err := stack.Client().Get(target.URL + path)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	entries, _, err := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})
	if err != nil || len(entries) != 3 {
		t.Fatalf("list: %v %v", entries, err)
	}

	// One by id through the route.
	req, _ := http.NewRequest(http.MethodDelete, stack.Console.URL+"/api/request-logs/"+entries[0].ID, nil)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete answered %d", res.StatusCode)
	}

	if _, err := stack.ReqLogs.Get(t.Context(), entries[0].ID); err == nil {
		t.Fatal("the entry is still there")
	}

	// The rest of the junk by filter.
	n, err := stack.ReqLogs.DeleteMatching(t.Context(), reqlog.ListParams{Search: `req.path contains junk`})
	if err != nil || n != 1 {
		t.Fatalf("delete matching: %d %v", n, err)
	}

	left, _, _ := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})
	if len(left) != 1 || !strings.HasSuffix(left[0].URL, "/keep") {
		t.Fatalf("left: %+v", left)
	}

	// A filtered DELETE on the collection answers the count.
	req, _ = http.NewRequest(http.MethodDelete, stack.Console.URL+"/api/request-logs?search="+url.QueryEscape("req.path = /keep"), nil)

	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	var out struct {
		Deleted int `json:"deleted"`
	}

	if err := json.UnmarshalRead(res.Body, &out); err != nil || res.StatusCode != http.StatusOK || out.Deleted != 1 {
		t.Fatalf("filtered delete: %d %+v %v", res.StatusCode, out, err)
	}
}

func TestSavedEntriesSurviveClear(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "saved")
	ctx := t.Context()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	for _, path := range []string{"/keep", "/junk"} {
		res, err := stack.Client().Get(target.URL + path)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if len(entries) != 2 {
		t.Fatalf("list: %v", entries)
	}

	var keep string
	for _, e := range entries {
		if strings.HasSuffix(e.URL, "/keep") {
			keep = e.ID
		}
	}

	res, err := http.Post(stack.Console.URL+"/api/request-logs/"+keep+"/save", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("save answered %d", res.StatusCode)
	}

	saved, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10, Saved: true})
	if err != nil || len(saved) != 1 || saved[0].ID != keep || !saved[0].Saved {
		t.Fatalf("saved view: %+v %v", saved, err)
	}

	// The log view no longer lists it: an entry is in one view.
	log, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if len(log) != len(entries)-1 {
		t.Fatalf("log view after save: %+v", log)
	}

	for _, s := range log {
		if s.ID == keep {
			t.Fatalf("log view still lists the saved entry: %+v", log)
		}
	}

	// Clear takes the junk and leaves the saved entry.
	if err := stack.ReqLogs.Clear(ctx); err != nil {
		t.Fatal(err)
	}

	if left, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10}); len(left) != 0 {
		t.Fatalf("log view after clear: %+v", left)
	}

	left, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10, Saved: true})
	if len(left) != 1 || left[0].ID != keep {
		t.Fatalf("saved view after clear: %+v", left)
	}

	// Delete by filter from the log view leaves it too, from the saved
	// view takes it.
	if n, _ := stack.ReqLogs.DeleteMatching(ctx, reqlog.ListParams{Search: "req.path = /keep"}); n != 0 {
		t.Fatalf("log-view delete removed %d saved", n)
	}

	if n, _ := stack.ReqLogs.DeleteMatching(ctx, reqlog.ListParams{Search: "req.path = /keep", Saved: true}); n != 1 {
		t.Fatalf("saved-view delete removed %d", n)
	}

	// Unsaving an entry and clearing takes everything.
	res2, _ := stack.Client().Get(target.URL + "/again")
	_ = res2.Body.Close()

	entries, _, _ = stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if _, err := stack.ReqLogs.SetSaved(ctx, entries[0].ID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := stack.ReqLogs.SetSaved(ctx, entries[0].ID, false); err != nil {
		t.Fatal(err)
	}

	if err := stack.ReqLogs.Clear(ctx); err != nil {
		t.Fatal(err)
	}

	if left, _, _ = stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10}); len(left) != 0 {
		t.Fatalf("after full clear: %+v", left)
	}
}

// A paused log leaves the proxy alone: the client gets its response and
// nothing is written. Resuming logs again, and what was captured before
// the pause is still there.
func TestPausedRecording(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "paused")
	ctx := t.Context()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	get := func(path string) {
		t.Helper()

		res, err := stack.Client().Get(target.URL + path)
		if err != nil {
			t.Fatal(err)
		}

		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if err != nil || string(body) != "ok" {
			t.Fatalf("%s answered %q %v", path, body, err)
		}
	}

	pause := func(on bool) {
		t.Helper()

		_, err := stack.Projects.UpdateSettings(ctx, func(s *project.Settings) error {
			s.RequestLog.Paused = on

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	get("/before")
	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})

		return len(entries) == 1
	}, "the first exchange is logged")

	pause(true)
	get("/while-paused")
	get("/while-paused-again")

	pause(false)
	get("/after")

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})

		return len(entries) == 2
	}, "the exchange after the pause is logged")

	entries, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("logged %d exchanges, want 2", len(entries))
	}

	for _, e := range entries {
		if strings.Contains(e.URL, "while-paused") {
			t.Fatalf("a paused exchange was logged: %s", e.URL)
		}
	}
}

// A HAR of a real capture, imported into a fresh project, brings back
// the request, the response, the bodies - text and binary - and the
// timing breakdown. Ids are not preserved on purpose: the rows are new
// rows, and one from a file must never overwrite one already there.
func TestHARRoundTrip(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "source")
	ctx := t.Context()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bin" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x00, 0x01, 0xff, 0xfe})

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom", "kept")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(target.Close)

	post, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL+"/json?q=1", strings.NewReader(`{"in":1}`))
	if err != nil {
		t.Fatal(err)
	}

	post.Header.Set("Content-Type", "application/json")

	res, err := stack.Client().Do(post)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	res, err = stack.Client().Get(target.URL + "/bin")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})

		return len(entries) == 2 && entries[0].StatusCode != 0 && entries[1].StatusCode != 0
	}, "both exchanges are logged")

	before, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	// Read in full while the source project is still open: Get reads the
	// open project, and this test switches to another one.
	want := make([]reqlogmodels.Entry, 0, len(before))

	for _, row := range before {
		full, err := stack.ReqLogs.Get(ctx, row.ID)
		if err != nil {
			t.Fatal(err)
		}

		want = append(want, full)
	}

	_, write, err := stack.ReqLogs.ExportHAR(ctx, reqlog.ListParams{})
	if err != nil {
		t.Fatal(err)
	}

	var file bytes.Buffer
	if err := write(&file); err != nil {
		t.Fatal(err)
	}

	// A fresh project, so nothing is confused with what was captured.
	fresh, err := stack.Projects.Create(ctx, "imported")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Projects.Open(ctx, fresh.ID); err != nil {
		t.Fatal(err)
	}

	n, err := stack.ReqLogs.ImportHAR(ctx, bytes.NewReader(file.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	if n != len(before) {
		t.Fatalf("imported %d entries, want %d", n, len(before))
	}

	after, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(after) != len(before) {
		t.Fatalf("the imported log has %d entries, want %d", len(after), len(before))
	}

	// Newest first on both sides, so the rows line up - which is the
	// point of minting ids from each entry's own timestamp.
	for i := range want {
		want := want[i]

		got, err := stack.ReqLogs.Get(ctx, after[i].ID)
		if err != nil {
			t.Fatal(err)
		}

		if got.ID == want.ID {
			t.Errorf("the imported entry kept the original id %s", got.ID)
		}

		if got.Method != want.Method || got.URL != want.URL {
			t.Errorf("request %d: %s %s, want %s %s", i, got.Method, got.URL, want.Method, want.URL)
		}

		if !bytes.Equal(got.Body, want.Body) {
			t.Errorf("request body %d: %q, want %q", i, got.Body, want.Body)
		}

		if !got.CreatedAt.Truncate(time.Millisecond).Equal(want.CreatedAt.Truncate(time.Millisecond)) {
			t.Errorf("created_at %d: %s, want %s", i, got.CreatedAt, want.CreatedAt)
		}

		if !slices.Contains(got.Tags, reqlog.ImportedTag) {
			t.Errorf("entry %d is not tagged %q: %v", i, reqlog.ImportedTag, got.Tags)
		}

		if got.Response == nil || want.Response == nil {
			t.Fatalf("entry %d lost its response", i)
		}

		if got.Response.StatusCode != want.Response.StatusCode {
			t.Errorf("status %d: %d, want %d", i, got.Response.StatusCode, want.Response.StatusCode)
		}

		if !bytes.Equal(got.Response.Body, want.Response.Body) {
			t.Errorf("response body %d: %q, want %q", i, got.Response.Body, want.Response.Body)
		}

		if got.Response.Headers.Get("Content-Type") != want.Response.Headers.Get("Content-Type") {
			t.Errorf("content type %d: %q, want %q", i,
				got.Response.Headers.Get("Content-Type"), want.Response.Headers.Get("Content-Type"))
		}

		// The breakdown survives the format, which is the whole reason
		// the HAR writer fills the timings object.
		gt, wt := got.Response.Timings, want.Response.Timings
		if gt == nil || wt == nil {
			t.Fatalf("timings %d: got %v, want %v", i, gt, wt)
		}

		if gt.Wait != wt.Wait || gt.Reused != wt.Reused {
			t.Errorf("timings %d: wait %.3f reused %v, want %.3f %v", i, gt.Wait, gt.Reused, wt.Wait, wt.Reused)
		}
	}
}

// A file from another tool, with the members a browser writes and none
// of ours, is read. An unanswered request keeps its pending shape, and
// a reused connection is recognised from the phases marked as not
// applying.
func TestHARImportFromAnotherTool(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "foreign")
	ctx := t.Context()

	const file = `{
	  "log": {
	    "version": "1.2",
	    "creator": {"name": "WebInspector", "version": "537.36"},
	    "pages": [{"id": "page_1", "title": "https://example.com"}],
	    "entries": [
	      {
	        "startedDateTime": "2026-01-02T03:04:05.678Z",
	        "time": 123.4,
	        "request": {
	          "method": "POST",
	          "url": "https://api.example.com/v1/items?page=2",
	          "httpVersion": "http/2.0",
	          "headers": [{"name": "content-type", "value": "application/json"}],
	          "queryString": [{"name": "page", "value": "2"}],
	          "cookies": [],
	          "headersSize": -1,
	          "bodySize": 9,
	          "postData": {"mimeType": "application/json", "text": "{\"a\":1}"}
	        },
	        "response": {
	          "status": 201,
	          "statusText": "",
	          "httpVersion": "http/2.0",
	          "headers": [{"name": "content-type", "value": "application/json"}],
	          "cookies": [],
	          "content": {"size": 11, "mimeType": "application/json", "text": "{\"id\":7}"},
	          "redirectURL": "",
	          "headersSize": -1,
	          "bodySize": 11
	        },
	        "cache": {},
	        "timings": {"blocked": 1.5, "dns": -1, "connect": -1, "ssl": -1, "send": 0.4, "wait": 120, "receive": 1.5},
	        "serverIPAddress": "93.184.216.34"
	      },
	      {
	        "startedDateTime": "2026-01-02T03:04:06.000Z",
	        "time": 0,
	        "request": {
	          "method": "GET",
	          "url": "https://api.example.com/never",
	          "httpVersion": "http/2.0",
	          "headers": [],
	          "queryString": [],
	          "cookies": [],
	          "headersSize": -1,
	          "bodySize": 0
	        },
	        "response": {
	          "status": 0,
	          "statusText": "",
	          "httpVersion": "",
	          "headers": [],
	          "cookies": [],
	          "content": {"size": 0, "mimeType": ""},
	          "redirectURL": "",
	          "headersSize": -1,
	          "bodySize": -1
	        },
	        "cache": {},
	        "timings": {"blocked": -1, "dns": -1, "connect": -1, "ssl": -1, "send": -1, "wait": -1, "receive": -1}
	      }
	    ]
	  }
	}`

	n, err := stack.ReqLogs.ImportHAR(ctx, strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}

	if n != 2 {
		t.Fatalf("imported %d entries, want 2", n)
	}

	entries, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil || len(entries) != 2 {
		t.Fatalf("%d entries (%v)", len(entries), err)
	}

	// Newest first, so the unanswered one comes back first.
	pending, err := stack.ReqLogs.Get(ctx, entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if pending.Response != nil {
		t.Errorf("a status-0 entry was given a response: %+v", pending.Response)
	}

	if pending.URL != "https://api.example.com/never" {
		t.Errorf("pending url %q", pending.URL)
	}

	answered, err := stack.ReqLogs.Get(ctx, entries[1].ID)
	if err != nil {
		t.Fatal(err)
	}

	if answered.Method != "POST" || string(answered.Body) != `{"a":1}` {
		t.Errorf("request: %s %q", answered.Method, answered.Body)
	}

	if answered.Response.StatusCode != 201 || answered.Response.Status != "Created" {
		t.Errorf("response: %d %q - the reason should come from the code when the file has none",
			answered.Response.StatusCode, answered.Response.Status)
	}

	if string(answered.Response.Body) != `{"id":7}` {
		t.Errorf("response body %q", answered.Response.Body)
	}

	tm := answered.Response.Timings
	if tm == nil {
		t.Fatal("the timings were dropped")
	}

	if !tm.Reused {
		t.Error("dns, connect and ssl all marked as not applying should read as a reused connection")
	}

	if tm.Wait != 120 || tm.Blocked != 1.5 || tm.DNS != 0 {
		t.Errorf("timings: %+v", tm)
	}

	if tm.ServerAddr != "93.184.216.34" {
		t.Errorf("server address %q", tm.ServerAddr)
	}

	// The entry sorts by when it happened, not by when it was read.
	want := time.Date(2026, 1, 2, 3, 4, 5, 678e6, time.UTC)
	if !answered.CreatedAt.Equal(want) {
		t.Errorf("created_at %s, want %s", answered.CreatedAt, want)
	}
}

// A file that is not a HAR is refused with a message, not stored.
func TestHARImportRefusesRubbish(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "rubbish")
	ctx := t.Context()

	for _, in := range []string{`not json at all`, `[]`, `{"log":{"version":"1.2"}}`, `{"log":{"version":"1.2","entries":[]}}`} {
		if _, err := stack.ReqLogs.ImportHAR(ctx, strings.NewReader(in)); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}

	entries, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil || len(entries) != 0 {
		t.Fatalf("%d entries were stored (%v)", len(entries), err)
	}
}

// The cap drops the oldest entries and never a saved one, which is what
// saving an entry is for.
func TestRetentionDropsTheOldestAndKeepsSaved(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "retention")
	ctx := t.Context()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	t.Cleanup(target.Close)

	// Ten exchanges, oldest first.
	for i := range 10 {
		res, err := stack.Client().Get(fmt.Sprintf("%s/n%d", target.URL, i))
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 50})

		return len(entries) == 10
	}, "ten exchanges are logged")

	all, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	// Newest first, so the last two are the two oldest. Save the very
	// oldest: it must survive a cap that would otherwise take it first.
	oldest := all[len(all)-1]
	if _, err := stack.ReqLogs.SetSaved(ctx, oldest.ID, true); err != nil {
		t.Fatal(err)
	}

	// Nine unsaved rows plus one saved, capped at four.
	setCap(t, stack, 4)

	dropped, err := stack.ReqLogs.Trim(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if dropped != 6 {
		t.Fatalf("dropped %d, want 6 (10 rows down to a cap of 4)", dropped)
	}

	left, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	saved, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 50, Saved: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(saved) != 1 || saved[0].ID != oldest.ID {
		t.Fatalf("the saved entry did not survive: %+v", saved)
	}

	// Four rows in total, and the saved one is one of them.
	if len(left)+len(saved) != 4 {
		t.Fatalf("%d in the log and %d saved, want 4 in total", len(left), len(saved))
	}

	// What is left of the log must be the newest, not the oldest.
	for _, e := range left {
		if strings.HasSuffix(e.URL, "/n0") || strings.HasSuffix(e.URL, "/n1") {
			t.Errorf("an old entry survived the trim: %s", e.URL)
		}
	}

	// Trimming again is a no-op, and no cap never drops anything.
	if n, err := stack.ReqLogs.Trim(ctx); err != nil || n != 0 {
		t.Fatalf("a second trim dropped %d (%v)", n, err)
	}

	setCap(t, stack, 0)

	if n, err := stack.ReqLogs.Trim(ctx); err != nil || n != 0 {
		t.Fatalf("no cap dropped %d (%v)", n, err)
	}
}

// setCap puts an entry cap on the open project.
func setCap(t *testing.T, stack *testutil.Stack, max int) {
	t.Helper()

	_, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.RequestLog.MaxEntries = max

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Muting a host hides it from the LOG VIEW and from nothing else. That
// distinction is the whole reason the setting exists beside the ignore
// filter, which refuses to write at all.
func TestMutedHostsAreHiddenFromTheViewOnly(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "muted")

	noisy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer noisy.Close()

	quiet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer quiet.Close()

	client := stack.Client()

	for range 3 {
		res, err := client.Get(noisy.URL + "/telemetry")
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	res, err := client.Get(quiet.URL + "/api/data")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})
		if len(entries) != 4 {
			return false
		}

		// With their responses: a host count is about answers, and an
		// entry whose response has not landed yet has none.
		return !slices.ContainsFunc(entries, func(e reqlogmodels.Summary) bool {
			return e.StatusCode == 0
		})
	}, "all four exchanges are logged with their responses")

	// The host list is what a person reads to decide what to mute: the
	// count, and how much of it failed.
	hosts, err := stack.ReqLogs.Hosts(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(hosts) != 1 {
		// Both servers are on 127.0.0.1, so they are one host - which is
		// itself the honest answer, since a host is a host.
		t.Fatalf("%d hosts, want 1: %+v", len(hosts), hosts)
	}

	if hosts[0].Host != "127.0.0.1" || hosts[0].Count != 4 || hosts[0].Errors != 3 {
		t.Errorf("host count %+v, want 127.0.0.1 with 4 entries and 3 errors", hosts[0])
	}

	if hosts[0].Muted {
		t.Error("a host is muted before anything muted it")
	}

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.RequestLog.MutedHosts = []string{"127.0.0.1"}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Hidden from the view.
	entries, _, err := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10, HonourMutes: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("%d entries in the view, want none from a muted host", len(entries))
	}

	// Still there, which is the difference from the ignore filter.
	entries, _, err = stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 4 {
		t.Errorf("%d entries when the mute is not honoured, want 4", len(entries))
	}

	// And still exported, because an export is what you have rather
	// than what you are looking at.
	_, write, err := stack.ReqLogs.ExportHAR(t.Context(), reqlog.ListParams{})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		t.Fatal(err)
	}

	if n := strings.Count(buf.String(), `"startedDateTime"`); n != 4 {
		t.Errorf("%d entries in the HAR, want 4", n)
	}

	if hosts, err = stack.ReqLogs.Hosts(t.Context()); err != nil {
		t.Fatal(err)
	} else if !hosts[0].Muted {
		t.Error("the host list does not say the host is muted")
	}

	// A pattern that is not a host pattern is refused when it is saved.
	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.RequestLog.MutedHosts = []string{"[a-"}

		return nil
	}); err == nil {
		t.Fatal("a broken host pattern was accepted")
	}
}

// Ranking is what column sorting is actually for, and it has to be a
// ranking of the WHOLE log rather than of the page in front of you.
func TestTopRanksTheWholeLog(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "top")

	// A target whose delay and size come from the path, so each entry
	// is distinguishable by both rankings at once.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			time.Sleep(120 * time.Millisecond)
			_, _ = w.Write([]byte("small"))
		case "/big":
			_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer target.Close()

	client := stack.Client()

	for _, path := range []string{"/quick", "/slow", "/big"} {
		res, err := client.Get(target.URL + path)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}

	testutil.Eventually(t, 4*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlog.ListParams{Limit: 10})

		return len(entries) == 3 && !slices.ContainsFunc(entries, func(e reqlogmodels.Summary) bool {
			return e.StatusCode == 0
		})
	}, "all three exchanges are logged with their responses")

	slowest, err := stack.ReqLogs.Top(t.Context(), reqlog.ListParams{}, reqlog.TopDuration, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(slowest) != 2 {
		t.Fatalf("%d entries, want the limit of 2", len(slowest))
	}

	if !strings.HasSuffix(slowest[0].URL, "/slow") {
		t.Errorf("slowest is %s", slowest[0].URL)
	}

	if slowest[0].DurationMS < slowest[1].DurationMS {
		t.Errorf("not ordered: %d then %d", slowest[0].DurationMS, slowest[1].DurationMS)
	}

	largest, err := stack.ReqLogs.Top(t.Context(), reqlog.ListParams{}, reqlog.TopSize, 20)
	if err != nil {
		t.Fatal(err)
	}

	if len(largest) != 3 || !strings.HasSuffix(largest[0].URL, "/big") || largest[0].ResSize != 4096 {
		t.Fatalf("largest: %+v", largest)
	}

	// The filter narrows a ranking the way it narrows a list.
	only, err := stack.ReqLogs.Top(t.Context(), reqlog.ListParams{Search: "req.path = /quick"}, reqlog.TopSize, 20)
	if err != nil {
		t.Fatal(err)
	}

	if len(only) != 1 || !strings.HasSuffix(only[0].URL, "/quick") {
		t.Fatalf("filtered ranking: %+v", only)
	}

	// A muted host is muted in the LIST. A ranking is a question about
	// the whole log, so it still counts.
	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.RequestLog.MutedHosts = []string{"127.0.0.1"}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	muted, err := stack.ReqLogs.Top(t.Context(), reqlog.ListParams{}, reqlog.TopSize, 20)
	if err != nil {
		t.Fatal(err)
	}

	if len(muted) != 3 {
		t.Errorf("%d entries after muting the host, want all 3", len(muted))
	}

	if _, err := stack.ReqLogs.Top(t.Context(), reqlog.ListParams{}, "sideways", 20); err == nil {
		t.Error("an unknown ranking was accepted")
	}
}
