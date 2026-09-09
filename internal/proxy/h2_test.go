package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/proxy"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// protoUpstream answers with the protocol it was spoken to in.
func protoUpstream(t *testing.T, h2 bool) *httptest.Server {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))
	srv.EnableHTTP2 = h2
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return srv
}

func TestTunnelSpeaksHTTP2ToTheClient(t *testing.T) {
	for _, upstreamH2 := range []bool{true, false} {
		stack := testutil.New(t)
		stack.OpenProject(t, "h2")
		target := protoUpstream(t, upstreamH2)

		res, err := stack.ClientH2().Get(target.URL + "/p")
		if err != nil {
			t.Fatal(err)
		}

		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if res.Proto != "HTTP/2.0" {
			t.Fatalf("upstream h2=%v: the client spoke %s", upstreamH2, res.Proto)
		}

		wantUpstream := "HTTP/1.1"
		if upstreamH2 {
			wantUpstream = "HTTP/2.0"
		}

		if string(body) != wantUpstream {
			t.Fatalf("upstream h2=%v: the upstream saw %q", upstreamH2, body)
		}

		// The log keeps both sides: what the client spoke, what the
		// upstream spoke.
		e := waitForResponse(t, stack)
		if e.Proto != "HTTP/2.0" || e.Response.Proto != wantUpstream {
			t.Fatalf("upstream h2=%v: logged %s -> %s", upstreamH2, e.Proto, e.Response.Proto)
		}
	}
}

func TestHTTP2CanBeDisabled(t *testing.T) {
	stack := testutil.New(t, testutil.WithProxy(func(o *proxy.Options) { o.DisableHTTP2 = true }))
	stack.OpenProject(t, "h1")
	target := protoUpstream(t, true)

	res, err := stack.ClientH2().Get(target.URL + "/p")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.Proto != "HTTP/1.1" {
		t.Fatalf("with h2 off the client spoke %s", res.Proto)
	}
}

func TestHTTP1ClientStillWorks(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "h1-client")
	target := protoUpstream(t, true)

	res, err := stack.Client().Get(target.URL + "/p")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.Proto != "HTTP/1.1" {
		t.Fatalf("an h1 client got %s", res.Proto)
	}

	if e := waitForResponse(t, stack); e.Proto != "HTTP/1.1" {
		t.Fatalf("logged %s", e.Proto)
	}
}

func TestManyH2StreamsAtOnce(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "streams")

	release := make(chan struct{})

	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	t.Cleanup(target.Close)

	client := stack.ClientH2()
	results := make(chan string, 3)

	for i := range 3 {
		go func() {
			res, err := client.Get(target.URL + "/" + string(rune('a'+i)))
			if err != nil {
				results <- err.Error()

				return
			}

			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			results <- res.Proto + " " + string(body)
		}()
	}

	// All three are in flight before any is answered, so nothing on the
	// proxy side serialized them.
	deadline := time.Now().Add(3 * time.Second)
	for {
		entries, _, err := stack.ReqLogs.List(t.Context(), reqlogList())
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) == 3 {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("only %d requests reached the log while the upstream held them", len(entries))
		}

		time.Sleep(10 * time.Millisecond)
	}

	close(release)

	seen := map[string]bool{}
	for range 3 {
		select {
		case r := <-results:
			seen[r] = true
		case <-time.After(3 * time.Second):
			t.Fatal("a request did not come back")
		}
	}

	for _, want := range []string{"HTTP/2.0 /a", "HTTP/2.0 /b", "HTTP/2.0 /c"} {
		if !seen[want] {
			t.Fatalf("missing %q in %v", want, seen)
		}
	}
}
