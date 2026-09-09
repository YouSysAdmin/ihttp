package clientcert_test

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
)

func TestParse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		spec string
		want clientcert.Entry
		bad  bool
	}{
		{spec: "api.example.com=/c.pem,/k.pem", want: clientcert.Entry{Host: "api.example.com", Cert: "/c.pem", Key: "/k.pem"}},
		{spec: "*.example.com=/both.pem", want: clientcert.Entry{Host: "*.example.com", Cert: "/both.pem"}},
		{spec: " API.Example.com = /c.pem , /k.pem ", want: clientcert.Entry{Host: "api.example.com", Cert: "/c.pem", Key: "/k.pem"}},
		{spec: "/c.pem", bad: true},
		{spec: "=/c.pem", bad: true},
		{spec: "api.example.com=", bad: true},
		{spec: "[a-=/c.pem", bad: true},
	}

	for _, c := range cases {
		got, err := clientcert.Parse(c.spec)
		if c.bad {
			if err == nil {
				t.Errorf("Parse(%q) was accepted", c.spec)
			}

			continue
		}

		if err != nil {
			t.Errorf("Parse(%q): %v", c.spec, err)

			continue
		}

		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.spec, got, c.want)
		}
	}
}

// A path that cannot be read is a startup error, not a handshake that
// fails in the middle of somebody's work.
func TestLoadRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	if _, err := clientcert.Load([]clientcert.Entry{{Host: "x", Cert: filepath.Join(t.TempDir(), "gone.pem")}}); err == nil {
		t.Fatal("a missing file was accepted")
	}

	if k, err := clientcert.Load(nil); k != nil || err != nil {
		t.Fatalf("nothing configured gave (%v, %v), want (nil, nil)", k, err)
	}
}

func TestForMatchesHostsAndIgnoresThePort(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	one := writeCert(t, dir, "one", "client-one")
	two := writeCert(t, dir, "two", "client-two")

	k, err := clientcert.Load([]clientcert.Entry{
		{Host: "*.example.com", Cert: one.cert, Key: one.key},
		{Host: "api.other.test", Cert: two.cert, Key: two.key},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"a.example.com":       "client-one",
		"a.b.example.com":     "client-one",
		"a.example.com:8443":  "client-one",
		"API.Other.Test":      "client-two",
		"example.com":         "",
		"other.test":          "",
		"api.other.test.evil": "",
	}

	for host, want := range cases {
		got := k.For(host)

		switch {
		case want == "" && got != nil:
			t.Errorf("For(%q) matched %s, want no certificate", host, got.Leaf.Subject.CommonName)
		case want != "" && got == nil:
			t.Errorf("For(%q) matched nothing, want %s", host, want)
		case want != "" && got != nil && got.Leaf.Subject.CommonName != want:
			t.Errorf("For(%q) = %s, want %s", host, got.Leaf.Subject.CommonName, want)
		}
	}

	if n := len(k.Summaries()); n != 2 {
		t.Errorf("%d summaries, want 2", n)
	}

	if s := k.Summaries()[0]; s.Host != "*.example.com" || s.Expired {
		t.Errorf("summary %+v", s)
	}
}

// The whole point, against an upstream that really does demand a
// client certificate: without one the handshake fails, with one the
// request goes through, and a host the list does not name gets nothing.
func TestWrapPresentsTheCertificateToTheRightHost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	client := writeCert(t, dir, "client", "the-client")

	pool := x509.NewCertPool()
	pool.AddCert(client.leaf)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := ""
		if len(r.TLS.PeerCertificates) > 0 {
			name = r.TLS.PeerCertificates[0].Subject.CommonName
		}

		_, _ = io.WriteString(w, name)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool} //nolint:gosec // Test only.
	srv.StartTLS()

	t.Cleanup(srv.Close)

	base := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test only.
	}

	// Without a certificate the server refuses the handshake, which is
	// what makes the rest of this test mean anything.
	if bare, err := base.RoundTrip(get(t, srv.URL)); err == nil {
		_ = bare.Body.Close()

		t.Fatal("the upstream accepted a connection with no client certificate")
	}

	k, err := clientcert.Load([]clientcert.Entry{{Host: "127.0.0.1", Cert: client.cert, Key: client.key}})
	if err != nil {
		t.Fatal(err)
	}

	res, err := k.Wrap(base).RoundTrip(get(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if string(body) != "the-client" {
		t.Errorf("the upstream saw %q, want the-client", body)
	}

	// A host no pattern names is left alone: the wrapper must not
	// present a certificate to everything it happens to be asked about.
	elsewhere, err := clientcert.Load([]clientcert.Entry{{Host: "somewhere.invalid", Cert: client.cert, Key: client.key}})
	if err != nil {
		t.Fatal(err)
	}

	if wrong, err := elsewhere.Wrap(base).RoundTrip(get(t, srv.URL)); err == nil {
		_ = wrong.Body.Close()

		t.Error("a certificate was presented to a host the list does not name")
	}
}

func get(t *testing.T, url string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}

	return req
}

type pair struct {
	cert, key string
	leaf      *x509.Certificate
}

// writeCert mints a self-signed client certificate and writes the two
// PEM files, which is what an operator points --client-cert at.
func writeCert(t *testing.T, dir, name, commonName string) pair {
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

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	p := pair{
		cert: filepath.Join(dir, name+".pem"),
		key:  filepath.Join(dir, name+"-key.pem"),
		leaf: leaf,
	}

	write(t, p.cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	write(t, p.key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))

	return p
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
