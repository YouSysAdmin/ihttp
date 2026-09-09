package intercept_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/domain/intercept"
	"github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

func enable(t *testing.T, stack *testutil.Stack, req, res bool, reqFilter string) {
	t.Helper()

	_, err := stack.Projects.UpdateSettings(t.Context(), func(s *project.Settings) error {
		s.Intercept = project.InterceptSettings{RequestsEnabled: req, ResponsesEnabled: res, RequestFilter: reqFilter}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitForItem(t *testing.T, stack *testutil.Stack, kind intercept.Kind) intercept.Item {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, it := range stack.Intercept.Items() {
			if it.Kind == kind {
				return it
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("no %s item appeared", kind)

	return intercept.Item{}
}

func TestRequestIsHeldEditedAndForwarded(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "i")
	enable(t, stack, true, false, "")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte(r.Method + " " + r.URL.Path + " " + r.Header.Get("X-Edited") + " " + string(body)))
	}))
	defer target.Close()

	done := make(chan string, 1)
	go func() {
		res, err := stack.Client().Post(target.URL+"/orig", "text/plain", strings.NewReader("a"))
		if err != nil {
			done <- "err: " + err.Error()

			return
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		done <- string(b)
	}()

	item := waitForItem(t, stack, intercept.KindRequest)
	if item.Request.Method != http.MethodPost || string(item.Request.Body) != "a" {
		t.Fatalf("unexpected item %+v", item.Request)
	}

	err := stack.Intercept.ForwardRequest(item.ID, &intercept.ForwardRequest{
		Method:  http.MethodPut,
		URL:     target.URL + "/edited",
		Headers: httpmsg.Headers{{Name: "X-Edited", Value: "1"}},
		Body:    httpmsg.Body("b"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := <-done; got != "PUT /edited 1 b" {
		t.Fatalf("upstream saw %q", got)
	}

	if len(stack.Intercept.Items()) != 0 {
		t.Fatal("queue must be empty after forwarding")
	}
}

func TestDroppedRequestAnswers502(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "d")
	enable(t, stack, true, false, `req.url =~ "/secret"`)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	// Not matching the filter: passes straight through.
	res, err := stack.Client().Get(target.URL + "/public")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("public: %v %v", res, err)
	}
	_ = res.Body.Close()

	status := make(chan int, 1)
	go func() {
		res, err := stack.Client().Get(target.URL + "/secret")
		if err != nil {
			status <- -1

			return
		}
		_ = res.Body.Close()
		status <- res.StatusCode
	}()

	item := waitForItem(t, stack, intercept.KindRequest)
	if err := stack.Intercept.DropRequest(item.ID); err != nil {
		t.Fatal(err)
	}

	if got := <-status; got != http.StatusBadGateway {
		t.Fatalf("dropped request answered %d", got)
	}
}

func TestResponseIsHeldAndEdited(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "r")
	enable(t, stack, false, true, "")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("original"))
	}))
	defer target.Close()

	type answer struct {
		status int
		body   string
	}

	done := make(chan answer, 1)
	go func() {
		res, err := stack.Client().Get(target.URL)
		if err != nil {
			done <- answer{-1, err.Error()}

			return
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		done <- answer{res.StatusCode, string(b)}
	}()

	item := waitForItem(t, stack, intercept.KindResponse)
	if item.Response == nil || string(item.Response.Body) != "original" {
		t.Fatalf("unexpected item %+v", item)
	}

	err := stack.Intercept.ForwardResponse(item.ID, &intercept.ForwardResponse{
		StatusCode: http.StatusTeapot,
		Body:       httpmsg.Body("edited"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := <-done; got.status != http.StatusTeapot || got.body != "edited" {
		t.Fatalf("client saw %+v", got)
	}
}

func TestDisablingReleasesWaitingRequests(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "rel")
	enable(t, stack, true, false, "")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	status := make(chan int, 1)
	go func() {
		res, err := stack.Client().Get(target.URL)
		if err != nil {
			status <- -1

			return
		}
		_ = res.Body.Close()
		status <- res.StatusCode
	}()

	waitForItem(t, stack, intercept.KindRequest)
	enable(t, stack, false, false, "")

	if got := <-status; got != 200 {
		t.Fatalf("released request answered %d", got)
	}
}
