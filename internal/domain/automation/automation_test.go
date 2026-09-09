package automation_test

import (
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/domain/automation"
	models "github.com/yousysadmin/ihttp/internal/models/automation"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

func TestRunAutomationJobInjectsEverywhere(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "automation")

	var hits atomic.Int64
	seenPaths := make(chan string, 100)
	seenHeaders := make(chan string, 100)
	seenBodies := make(chan string, 100)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		seenPaths <- r.URL.Path
		seenHeaders <- r.Header.Get("X-Automation")
		seenBodies <- string(body)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok " + r.URL.Path))
	}))
	defer target.Close()

	ctx := t.Context()

	job, err := stack.Automation.Save(ctx, automation.SaveJob{
		Name:        "everywhere",
		Method:      "POST",
		URL:         target.URL + "/user/AUTO",
		Headers:     modelHeaders("X-Automation", "AUTO"),
		Body:        new(httpmsg.Body(`{"q":"AUTO"}`)),
		Placeholder: "AUTO",
		Payload:     models.Payload{Kind: models.PayloadList, List: []string{"alpha", "beta", "gamma"}},
		Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	if job.Positions != 3 {
		t.Fatalf("positions: want 3, got %d", job.Positions)
	}

	if _, err := stack.Automation.Start(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	waitDone(t, stack, job.ID)

	if hits.Load() != 3 {
		t.Fatalf("hits: want 3, got %d", hits.Load())
	}

	results, err := stack.Automation.Results(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 3 {
		t.Fatalf("results: want 3, got %d", len(results))
	}

	for _, r := range results {
		if r.StatusCode != 200 || r.Error != "" {
			t.Fatalf("result %d: status %d err %q", r.Index, r.StatusCode, r.Error)
		}
	}

	// The payload reached the path, the header and the body.
	paths := drain(seenPaths, 3)
	headers := drain(seenHeaders, 3)
	bodies := drain(seenBodies, 3)

	if !slices.Contains(paths, "/user/beta") || !slices.Contains(headers, "gamma") || !slices.Contains(bodies, `{"q":"alpha"}`) {
		t.Fatalf("injection missed: paths %v headers %v bodies %v", paths, headers, bodies)
	}

	full, err := stack.Automation.Result(ctx, job.ID, results[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(full.Body) == 0 || full.Proto == "" {
		t.Fatalf("result response not captured: %+v", full)
	}

	// The request as sent is kept too, with the payload written in.
	if full.ReqURL == "" || full.ReqHeaders.Get("X-Automation") == "" || len(full.ReqBody) == 0 {
		t.Fatalf("result request not captured: %+v", full)
	}
}

func TestStopConditionDropsRequestsInFlight(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "stop")

	// The first payload trips the condition at once. The rest are slow,
	// so several are in flight when the run is cancelled.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/1" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("boom"))

			return
		}

		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	ctx := t.Context()

	job, err := stack.Automation.Save(ctx, automation.SaveJob{
		Name:        "stop on 500",
		URL:         target.URL + "/user/AUTO",
		Placeholder: "AUTO",
		Payload:     models.Payload{Kind: models.PayloadNumbers, From: 1, To: 20, Step: 1},
		Concurrency: 5,
		StopMatch:   "any",
		StopOn:      []models.StopCondition{{Field: "status", Op: "gte", Value: "500"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Automation.Start(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	waitDone(t, stack, job.ID)

	done, err := stack.Automation.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	if done.Status != models.StatusStopped {
		t.Fatalf("status: want stopped, got %s", done.Status)
	}

	results, err := stack.Automation.Results(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly one result is marked, none carries "context canceled", and
	// the run stopped far short of 20.
	matched := 0
	for _, r := range results {
		if r.Matched {
			matched++
		}

		if r.Error != "" {
			t.Fatalf("a cancelled request was recorded: %+v", r)
		}
	}

	if matched != 1 || len(results) >= 20 {
		t.Fatalf("stop condition: %d results, %d matched", len(results), matched)
	}

	if done.Completed != len(results) {
		t.Fatalf("completed %d but %d results", done.Completed, len(results))
	}
}

func TestDeleteWaitsForTheRun(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "delete")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer target.Close()

	ctx := t.Context()

	job, err := stack.Automation.Save(ctx, automation.SaveJob{
		URL:         target.URL + "/user/AUTO",
		Payload:     models.Payload{Kind: models.PayloadNumbers, From: 1, To: 10},
		Concurrency: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Automation.Start(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	if err := stack.Automation.Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	// Nothing lands after the delete: the workers were waited for.
	time.Sleep(500 * time.Millisecond)

	left, err := stack.Automation.Results(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(left) != 0 {
		t.Fatalf("%d results outlived their job", len(left))
	}

	if _, err := stack.Automation.Get(ctx, job.ID); err == nil {
		t.Fatal("the job came back")
	}
}

func TestRewriteKeepsTheBodyWhenNull(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "body")

	ctx := t.Context()

	job, err := stack.Automation.Save(ctx, automation.SaveJob{
		URL:     "http://x.test/AUTO",
		Body:    new(httpmsg.Body("\x00\x01\x02")),
		Payload: models.Payload{Kind: models.PayloadLibrary},
	})
	if err != nil {
		t.Fatal(err)
	}

	again, err := stack.Automation.Save(ctx, automation.SaveJob{
		ID:      job.ID,
		URL:     "http://x.test/AUTO",
		Payload: models.Payload{Kind: models.PayloadLibrary},
	})
	if err != nil {
		t.Fatal(err)
	}

	if string(again.Body) != "\x00\x01\x02" {
		t.Fatalf("body not kept: %q", again.Body)
	}

	cleared, err := stack.Automation.Save(ctx, automation.SaveJob{
		ID:      job.ID,
		URL:     "http://x.test/AUTO",
		Body:    new(httpmsg.Body("")),
		Payload: models.Payload{Kind: models.PayloadLibrary},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(cleared.Body) != 0 {
		t.Fatalf("body not cleared: %q", cleared.Body)
	}
}

func TestURLEncodeReachesTheServer(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "encode")

	seen := make(chan string, 10)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path + "?" + r.URL.RawQuery
		w.WriteHeader(200)
	}))
	defer target.Close()

	ctx := t.Context()

	job, err := stack.Automation.Save(ctx, automation.SaveJob{
		URL:         target.URL + "/user/AUTO?q=AUTO",
		URLEncode:   true,
		Payload:     models.Payload{Kind: models.PayloadList, List: []string{"a b\n"}},
		Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Automation.Start(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	waitDone(t, stack, job.ID)

	got := drain(seen, 1)
	if len(got) != 1 || got[0] != "/user/a b\n?q=a+b%0A" {
		t.Fatalf("encoded url: %q", got)
	}

	results, err := stack.Automation.Results(ctx, job.ID)
	if err != nil || len(results) != 1 || results[0].Error != "" {
		t.Fatalf("results: %+v %v", results, err)
	}
}

func TestColumnsReachTheirTokens(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "columns")

	seen := make(chan string, 10)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path + " " + r.Header.Get("FAV") + " " + r.URL.Query().Get("first")
		w.WriteHeader(200)
	}))
	defer target.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "pairs.csv")
	if err := os.WriteFile(file, []byte("my-favorite-books,hello-world\r\nsecond,two\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()

	// $1 in the path, $2 in a header, and the placeholder is $1 again.
	job, err := stack.Automation.Save(ctx, automation.SaveJob{
		URL:         target.URL + "/list/$1?first=AUTO",
		Headers:     modelHeaders("FAV", "$2"),
		Payload:     models.Payload{Kind: models.PayloadList, File: file, Separator: ","},
		Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if job.Positions != 3 || job.Columns != 2 {
		t.Fatalf("positions %d columns %d", job.Positions, job.Columns)
	}

	if _, err := stack.Automation.Start(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	waitDone(t, stack, job.ID)

	got := drain(seen, 2)
	if !slices.Contains(got, "/list/my-favorite-books hello-world my-favorite-books") || !slices.Contains(got, "/list/second two second") {
		t.Fatalf("columns: %q", got)
	}

	results, err := stack.Automation.Results(ctx, job.ID)
	if err != nil || len(results) != 2 || results[0].Payload != "my-favorite-books,hello-world" {
		t.Fatalf("results: %+v %v", results, err)
	}

	// A line short of the columns the template names is refused at start.
	short, err := stack.Automation.Save(ctx, automation.SaveJob{
		URL:     target.URL + "/$1/$3",
		Payload: models.Payload{Kind: models.PayloadList, List: []string{"a,b,c", "a,b"}, Separator: ","},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = stack.Automation.Start(ctx, short.ID)
	if _, ok := errors.AsType[*automation.SpecError](err); !ok {
		t.Fatalf("short row: %v", err)
	}

	// A file that is not there is refused when the job is saved.
	_, err = stack.Automation.Save(ctx, automation.SaveJob{
		URL:     target.URL + "/AUTO",
		Payload: models.Payload{Kind: models.PayloadList, File: filepath.Join(dir, "missing.txt")},
	})
	if _, ok := errors.AsType[*automation.SpecError](err); !ok {
		t.Fatalf("missing file: %v", err)
	}
}

func TestLibraryAndNumbersExpand(t *testing.T) {
	if n, err := automation.Count(models.Payload{Kind: models.PayloadNumbers, From: 1, To: 10, Step: 2}); err != nil || n != 5 {
		t.Fatalf("numbers count: %d %v", n, err)
	}

	vals, err := automation.Expand(models.Payload{Kind: models.PayloadNumbers, From: 5, To: 1, Step: -2})
	if err != nil {
		t.Fatal(err)
	}

	if len(vals) != 3 || vals[0].Raw != "5" || vals[2].Raw != "1" {
		t.Fatalf("descending range: %v", vals)
	}

	// A range ending at the edge of int terminates.
	edge, err := automation.Expand(models.Payload{Kind: models.PayloadNumbers, From: math.MaxInt - 2, To: math.MaxInt})
	if err != nil || len(edge) != 3 {
		t.Fatalf("edge range: %d %v", len(edge), err)
	}

	lib, err := automation.Expand(models.Payload{Kind: models.PayloadLibrary})
	if err != nil || len(lib) == 0 {
		t.Fatalf("library: %d %v", len(lib), err)
	}
}

func TestBadJobsAreRefused(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "bad")

	ctx := t.Context()

	if _, err := stack.Automation.Save(ctx, automation.SaveJob{URL: "not-a-url", Payload: models.Payload{Kind: models.PayloadLibrary}}); err == nil {
		t.Fatal("a bad url was accepted")
	}

	if _, err := stack.Automation.Save(ctx, automation.SaveJob{URL: "http://x.test/AUTO", Payload: models.Payload{Kind: models.PayloadList}}); err == nil {
		t.Fatal("an empty list was accepted")
	}

	if _, err := stack.Automation.Save(ctx, automation.SaveJob{
		URL:     "http://x.test/AUTO",
		Payload: models.Payload{Kind: models.PayloadLibrary},
		StopOn:  []models.StopCondition{{Field: "status", Op: "contains", Value: "500"}},
	}); err == nil {
		t.Fatal("a status condition with a body comparison was accepted")
	}

	job, err := stack.Automation.Save(ctx, automation.SaveJob{URL: "http://x.test/no-placeholder", Payload: models.Payload{Kind: models.PayloadLibrary}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Automation.Start(ctx, job.ID); err == nil {
		t.Fatal("a job with no placeholder was started")
	}
}

func modelHeaders(name, value string) httpmsg.Headers {
	return httpmsg.Headers{{Name: name, Value: value}}
}

func waitDone(t *testing.T, stack *testutil.Stack, id string) {
	t.Helper()

	testutil.Eventually(t, 5*time.Second, func() bool {
		j, err := stack.Automation.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}

		return j.Status == models.StatusDone || j.Status == models.StatusStopped || j.Status == models.StatusError
	}, "job finishes")
}

func drain(ch chan string, n int) []string {
	out := make([]string, 0, n)
	for range n {
		select {
		case v := <-ch:
			out = append(out, v)
		case <-time.After(time.Second):
			return out
		}
	}

	return out
}
