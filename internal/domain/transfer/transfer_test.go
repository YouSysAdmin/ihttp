package transfer_test

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/domain/automation"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"github.com/yousysadmin/ihttp/internal/domain/transfer"
	automationmodels "github.com/yousysadmin/ihttp/internal/models/automation"
	projectmodels "github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

func TestExportImportRoundTrip(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "source")
	ctx := t.Context()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bin" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x00, 0x01, 0x02, 0xff})

			return
		}

		_, _ = w.Write([]byte("text"))
	}))
	t.Cleanup(target.Close)

	for _, path := range []string{"/bin", "/text"} {
		res, err := stack.Client().Get(target.URL + path)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	if _, err := stack.Sender.Save(ctx, sender.SaveRequest{URL: target.URL + "/saved"}); err != nil {
		t.Fatal(err)
	}

	job, err := stack.Automation.Save(ctx, automation.SaveJob{
		URL:     target.URL + "/AUTO",
		Payload: automationmodels.Payload{Kind: automationmodels.PayloadList, List: []string{"a", "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Automation.Start(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	source := stack.Projects.Active().Project

	// Wait until both log responses and both results have landed.
	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
		results, _ := stack.Automation.Results(ctx, job.ID)
		j, _ := stack.Automation.Get(ctx, job.ID)

		return len(entries) >= 2 && entries[0].StatusCode != 0 && entries[1].StatusCode != 0 && len(results) == 2 && j.Status == automationmodels.StatusDone
	}, "two log entries and two results settle")

	before, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	name, write, err := stack.Transfer.Export(ctx, source.ID, false)
	if err != nil || name != "source" {
		t.Fatalf("export: %q %v", name, err)
	}

	var file bytes.Buffer
	if err := write(&file); err != nil {
		t.Fatal(err)
	}

	var env transfer.Envelope
	if err := json.Unmarshal(file.Bytes(), &env); err != nil {
		t.Fatalf("%v in %s", err, file.String()[:200])
	}

	if env.Format != transfer.FormatName || env.Version != transfer.Version || len(env.Buckets["request_logs"]) < 2 || len(env.Buckets["automation_results"]) != 2 {
		t.Fatalf("envelope: format %q version %d buckets %v", env.Format, env.Version, counts(env))
	}

	imported, err := stack.Transfer.Import(ctx, bytes.NewReader(file.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	if imported.ID == source.ID || imported.Name != "source (imported)" || imported.IsActive {
		t.Fatalf("imported %+v", imported)
	}

	if stack.Projects.ActiveID() != source.ID {
		t.Fatal("the open project changed")
	}

	// The imported project holds the same records under the new id.
	if _, err := stack.Projects.Open(ctx, imported.ID); err != nil {
		t.Fatal(err)
	}

	after, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil || len(after) != len(before) {
		t.Fatalf("imported log: %d entries, want %d (%v)", len(after), len(before), err)
	}

	for i := range before {
		if after[i].ID != before[i].ID {
			t.Fatalf("entry ids differ: %s vs %s", after[i].ID, before[i].ID)
		}

		full, err := stack.ReqLogs.Get(ctx, after[i].ID)
		if err != nil {
			t.Fatal(err)
		}

		if full.ProjectID != imported.ID {
			t.Fatalf("project id not rewritten: %s", full.ProjectID)
		}

		if strings.HasSuffix(full.URL, "/bin") && !bytes.Equal(full.Response.Body, []byte{0x00, 0x01, 0x02, 0xff}) {
			t.Fatalf("binary body changed: %x", full.Response.Body)
		}
	}

	requests, err := stack.Sender.List(ctx, "", false)
	if err != nil || len(requests) != 1 {
		t.Fatalf("sender requests: %v %v", requests, err)
	}

	jobs, err := stack.Automation.List(ctx, "")
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("jobs: %v %v", jobs, err)
	}

	results, err := stack.Automation.Results(ctx, job.ID)
	if err != nil || len(results) != 2 {
		t.Fatalf("results: %v %v", results, err)
	}

	// A second import of the same file counts the name up.
	again, err := stack.Transfer.Import(ctx, bytes.NewReader(file.Bytes()))
	if err != nil || again.Name != "source (imported 2)" {
		t.Fatalf("second import: %+v %v", again, err)
	}

	// The route serves the same file as a download.
	res, err := http.Get(stack.Console.URL + "/api/projects/" + source.ID + "/export")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Disposition") != `attachment; filename="source.ihttp.json"` {
		t.Fatalf("export route: %d %v", res.StatusCode, res.Header)
	}
}

func TestImportRefusals(t *testing.T) {
	stack := testutil.New(t)
	ctx := t.Context()

	cases := map[string]string{
		"not ours":     `{"format":"other","version":1,"project":{"name":"x"},"buckets":{}}`,
		"newer":        `{"format":"ihttp-project","version":2,"project":{"name":"x"},"buckets":{}}`,
		"no project":   `{"format":"ihttp-project","version":1,"buckets":{}}`,
		"late format":  `{"buckets":{},"format":"ihttp-project","version":1,"project":{"name":"x"}}`,
		"bad bucket":   `{"format":"ihttp-project","version":1,"project":{"name":"x"},"buckets":{"secrets":[]}}`,
		"bad settings": `{"format":"ihttp-project","version":1,"project":{"name":"x","settings":{"request_log":{"ignore_filter":"res.statusCode = 1"}}},"buckets":{}}`,
		"not json":     `hello`,
	}

	for name, body := range cases {
		if _, err := stack.Transfer.Import(ctx, strings.NewReader(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	list, err := stack.Projects.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("a refused import left a project: %v %v", list, err)
	}

	res, err := http.Post(stack.Console.URL+"/api/projects/import", "application/json", strings.NewReader(`{"format":"other"}`))
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad file answered %d", res.StatusCode)
	}

	if _, _, err := stack.Transfer.Export(ctx, "missing", false); err == nil {
		t.Fatal("exporting a missing project succeeded")
	}
}

// A settings-only export carries the project and no traffic, and is
// still an ordinary project file: it imports through the same path and
// arrives with the settings intact and an empty log.
func TestExportSettingsOnly(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "shared")
	ctx := t.Context()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("text"))
	}))
	t.Cleanup(target.Close)

	_, err := stack.Projects.UpdateSettings(ctx, func(s *projectmodels.Settings) error {
		s.RequestLog.IgnoreFilter = "req.ext in (png, css)"

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := stack.Client().Get(target.URL + "/logged")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	source := stack.Projects.Active().Project

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})

		return len(entries) == 1
	}, "the exchange is logged")

	_, write, err := stack.Transfer.Export(ctx, source.ID, true)
	if err != nil {
		t.Fatal(err)
	}

	var file bytes.Buffer
	if err := write(&file); err != nil {
		t.Fatal(err)
	}

	var env transfer.Envelope
	if err := json.Unmarshal(file.Bytes(), &env); err != nil {
		t.Fatal(err)
	}

	if env.Format != transfer.FormatName || env.Version != transfer.Version {
		t.Fatalf("not a project file: format %q version %d", env.Format, env.Version)
	}

	for name, records := range env.Buckets {
		if len(records) != 0 {
			t.Fatalf("bucket %s carries %d records, want none", name, len(records))
		}
	}

	if env.Project.Settings.RequestLog.IgnoreFilter != "req.ext in (png, css)" {
		t.Fatalf("settings did not travel: %+v", env.Project.Settings.RequestLog)
	}

	imported, err := stack.Transfer.Import(ctx, bytes.NewReader(file.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	if imported.Settings.RequestLog.IgnoreFilter != "req.ext in (png, css)" {
		t.Fatalf("imported settings: %+v", imported.Settings.RequestLog)
	}

	if _, err := stack.Projects.Open(ctx, imported.ID); err != nil {
		t.Fatal(err)
	}

	entries, _, err := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 10})
	if err != nil || len(entries) != 0 {
		t.Fatalf("imported log: %d entries, want none (%v)", len(entries), err)
	}
}

func counts(env transfer.Envelope) map[string]int {
	out := map[string]int{}
	for k, v := range env.Buckets {
		out[k] = len(v)
	}

	return out
}
