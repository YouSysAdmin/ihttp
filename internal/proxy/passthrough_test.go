package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"testing"
	"time"

	projectmodels "github.com/yousysadmin/ihttp/internal/models/project"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// The whole point of passthrough, asserted the only way that means
// anything: the client sees the REAL server's certificate rather than
// one ihttp minted for the host, and no payload reaches the log.
func TestPassthroughDoesNotDecrypt(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "no-decrypt")

	target := upstream(t, true)

	// The target's own certificate, which a decrypted connection could
	// never present to the client.
	realCert := target.Certificate()

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.NoDecrypt = []string{"127.0.0.1"}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A client that trusts BOTH ihttp's CA and the target's own, so the
	// request succeeds either way and the test is about which
	// certificate arrived rather than about a handshake failing.
	pool := x509.NewCertPool()
	pool.AddCert(realCert)
	pool.AddCert(stack.CA.CA())

	var seen *x509.Certificate

	client := stack.ClientWithTLS(&tls.Config{ //nolint:gosec // Test only.
		RootCAs: pool,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) > 0 {
				seen = cs.PeerCertificates[0]
			}

			return nil
		},
	})

	res, err := client.Get(target.URL + "/secret")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d, want the upstream's", res.StatusCode)
	}

	if string(body) != "echo:" {
		t.Errorf("the client got %q, so the relay altered the payload", body)
	}

	if seen == nil {
		t.Fatal("no peer certificate was seen, so this test proves nothing")
	}

	if !seen.Equal(realCert) {
		t.Errorf("the certificate was issued by %q for %v - ihttp decrypted the connection",
			seen.Issuer.CommonName, seen.DNSNames)
	}

	// The connection is logged all the same, because passthrough must
	// not be silently blind - as a CONNECT with no body, since nobody in
	// the middle saw one. It is there WHILE the tunnel is open, which is
	// the half that matters for a connection held for minutes.
	var entries []reqlogmodels.Summary

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlogListSearch("req.tunnel = true"))

		return len(entries) == 1
	}, "the open tunnel is logged")

	pending, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if pending.Response != nil {
		t.Errorf("an open tunnel already has a response: %+v", pending.Response)
	}

	// And the byte counts arrive when the connection ends.
	client.CloseIdleConnections()

	var full reqlogmodels.Entry

	testutil.Eventually(t, 3*time.Second, func() bool {
		e, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
		if err != nil {
			return false
		}

		full = e

		return e.Response != nil
	}, "the closed tunnel is filled in")

	if full.Method != http.MethodConnect {
		t.Errorf("the tunnel was logged as %s, want CONNECT", full.Method)
	}

	if full.Tunnel == nil {
		t.Fatal("the entry carries no tunnel information")
	}

	if full.Tunnel.BytesOut == 0 || full.Tunnel.BytesIn == 0 {
		t.Errorf("bytes: out %d in %d, want both counted", full.Tunnel.BytesOut, full.Tunnel.BytesIn)
	}

	if len(full.Body) != 0 {
		t.Errorf("the tunnel entry has a request body: %q", full.Body)
	}

	if len(full.Response.Body) != 0 {
		t.Errorf("the tunnel entry has a response body: %q", full.Response.Body)
	}

	if full.Tunnel.ClosedAt.IsZero() {
		t.Error("the tunnel entry does not say when it closed")
	}

	// A tunnel that ended because the client was done is not a failed
	// one. Closing our own other half must not be reported as the
	// reason it ended.
	if full.Tunnel.Error != "" {
		t.Errorf("an ordinary close was recorded as an error: %s", full.Tunnel.Error)
	}

	// Nothing else was written: no decrypted exchange sits beside it.
	all, _, err := stack.ReqLogs.List(t.Context(), reqlogList())
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 1 {
		t.Errorf("the log holds %d entries, want the tunnel alone", len(all))
	}
}

// A host the list does not name is decrypted as before, so passthrough
// is a narrow exception rather than a switch that quietly turns the tool
// off.
func TestOnlyListedHostsArePassedThrough(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "narrow")

	target := upstream(t, true)

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.NoDecrypt = []string{"*.example.com"}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res, err := stack.Client().Get(target.URL + "/still-read")
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	var entries []reqlogmodels.Summary

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ = stack.ReqLogs.List(t.Context(), reqlogList())

		return len(entries) == 1 && entries[0].StatusCode != 0
	}, "the exchange is logged in full")

	full, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if full.Tunnel != nil {
		t.Fatal("an unlisted host was passed through")
	}

	if string(full.Response.Body) != "echo:" {
		t.Errorf("response body %q, so the exchange was not decrypted after all", full.Response.Body)
	}
}

// A do-not-decrypt entry that is not a host pattern is refused when the
// settings are saved, rather than at CONNECT time on somebody's real
// request.
func TestNoDecryptIsValidatedOnSave(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "bad-glob")

	if _, err := stack.Projects.UpdateSettings(t.Context(), func(s *projectmodels.Settings) error {
		s.NoDecrypt = []string{"[a-"}

		return nil
	}); err == nil {
		t.Fatal("a broken host pattern was accepted")
	}
}
