package proxy_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/clientcert"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// An upstream that demands a client certificate is a place ihttp simply
// could not be until it could present one. The test is end to end: a
// client through the MITM, and the upstream reports whose certificate it
// saw - so it proves ours was forwarded rather than that a handshake
// happened to succeed.
func TestClientCertificateReachesAnMTLSUpstream(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, leaf := mintClientCert(t, dir, "ihttp-client")

	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	target := mtlsUpstream(t, pool)

	// Without the certificate the exchange fails, which is what makes
	// the second half of this test worth anything. The proxy answers the
	// client with a 502 rather than hanging.
	plain := testutil.New(t)
	plain.OpenProject(t, "no-cert")

	res, err := plain.Client().Get(target.URL + "/who")
	if err == nil {
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if res.StatusCode != http.StatusBadGateway {
			t.Fatalf("without a client certificate: status %d body %q, want a 502", res.StatusCode, body)
		}

		t.Logf("without a client certificate the proxy answered %d, as it should", res.StatusCode)
	} else {
		t.Logf("without a client certificate the request failed: %v", err)
	}

	keeper, err := clientcert.Load([]clientcert.Entry{{Host: "127.0.0.1", Cert: certPath, Key: keyPath}})
	if err != nil {
		t.Fatal(err)
	}

	stack := testutil.New(t, testutil.WithClientCerts(keeper))
	stack.OpenProject(t, "mtls")

	res, err = stack.Client().Get(target.URL + "/who")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if string(body) != "ihttp-client" {
		t.Fatalf("the upstream saw %q, want ihttp-client", body)
	}

	// And a replay reaches it too, since a request that cannot be sent
	// again is only half captured.
	sent, err := stack.Sender.Send(t.Context(), mustSave(t, stack, target.URL+"/who"))
	if err != nil {
		t.Fatal(err)
	}

	if sent.Response == nil {
		t.Fatal("the sender got no response")
	}

	if string(sent.Response.Body) != "ihttp-client" {
		t.Fatalf("the sender's upstream saw %q, want ihttp-client", sent.Response.Body)
	}
}

// mustSave stores a GET the sender can then send.
func mustSave(t *testing.T, stack *testutil.Stack, url string) string {
	t.Helper()

	req, err := stack.Sender.Save(t.Context(), sender.SaveRequest{
		Method: http.MethodGet,
		URL:    url,
	})
	if err != nil {
		t.Fatal(err)
	}

	return req.ID
}

// mtlsUpstream answers with the common name of the certificate the
// client presented, so a test can tell whose it was.
func mtlsUpstream(t *testing.T, clientCAs *x509.CertPool) *httptest.Server {
	t.Helper()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := ""
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			name = r.TLS.PeerCertificates[0].Subject.CommonName
		}

		_, _ = io.WriteString(w, name)
	}))

	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs} //nolint:gosec // Test only.
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return srv
}

// mintClientCert writes the two PEM files an operator points
// --client-cert at.
func mintClientCert(t *testing.T, dir, commonName string) (certPath, keyPath string, leaf *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	if leaf, err = x509.ParseCertificate(der); err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	certPath = filepath.Join(dir, "client.pem")
	keyPath = filepath.Join(dir, "client-key.pem")

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	return certPath, keyPath, leaf
}
