package sender_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

func TestSaveSendAndList(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "s")
	ctx := t.Context()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Proto", r.Proto)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("got " + r.Method + " " + string(body) + " " + r.Header.Get("X-A")))
	}))
	defer target.Close()

	if _, err := stack.Sender.Save(ctx, sender.SaveRequest{URL: "nope"}); !errors.Is(err, sender.ErrInvalidURL) {
		t.Fatalf("bad url: %v", err)
	}

	r, err := stack.Sender.Save(ctx, sender.SaveRequest{
		Method:  "post",
		URL:     target.URL + "/x",
		Proto:   httpmsg.ProtoHTTP11,
		Headers: httpmsg.Headers{{Name: "X-A", Value: "1"}},
		Body:    new(httpmsg.Body("payload")),
	})
	if err != nil {
		t.Fatal(err)
	}

	if r.Method != http.MethodPost || r.ID == "" {
		t.Fatalf("saved %+v", r)
	}

	sent, err := stack.Sender.Send(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}

	if sent.Response == nil || sent.Response.StatusCode != 201 || string(sent.Response.Body) != "got POST payload 1" {
		t.Fatalf("response %+v", sent.Response)
	}

	if sent.Response.Headers.Get("X-Proto") != "HTTP/1.1" {
		t.Fatalf("proto %s", sent.Response.Headers.Get("X-Proto"))
	}

	list, err := stack.Sender.List(ctx, "res.statusCode = 201", false)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %+v %v", list, err)
	}

	list, err = stack.Sender.List(ctx, "res.statusCode = 404", false)
	if err != nil || len(list) != 0 {
		t.Fatalf("filtered list %+v %v", list, err)
	}

	// A rewrite keeps the id and drops the stale response. Body null
	// keeps the stored bytes.
	again, err := stack.Sender.Save(ctx, sender.SaveRequest{ID: r.ID, URL: target.URL + "/y"})
	if err != nil || again.ID != r.ID || again.Response != nil || again.Method != http.MethodGet {
		t.Fatalf("rewrite %+v %v", again, err)
	}

	if string(again.Body) != "payload" {
		t.Fatalf("rewrite dropped the body: %q", again.Body)
	}

	target.Close()

	if _, err := stack.Sender.Send(ctx, r.ID); !isSendError(err) {
		t.Fatalf("a dead upstream must be a SendError, got %v", err)
	}
}

func isSendError(err error) bool {
	_, ok := errors.AsType[*sender.SendError](err)

	return ok
}
