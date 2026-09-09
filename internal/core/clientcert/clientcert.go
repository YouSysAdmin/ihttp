// Package clientcert answers an upstream that asks the CLIENT for a
// certificate: mutual TLS. Without it ihttp cannot be in the path of
// such a service at all - the handshake fails before a request is ever
// written.
//
// The material stays on disk and is named by path. ihttp reads it, holds
// it for the life of the process and never stores it: it is not a key
// store, and a tool that captures traffic is the last place a private
// key should be kept.
package clientcert

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// Entry is one "these hosts, this certificate" pair as it was asked
// for. Key is empty when the certificate file holds the key too.
type Entry struct {
	// Host is a host glob, the same shape the do-not-decrypt list and
	// the upstream bypass list take, so there is one syntax for "these
	// hosts" in the product.
	Host string

	Cert string
	Key  string
}

// Parse reads one flag value: "<host glob>=<cert file>[,<key file>]".
// A single file is read as holding both, which is how a PEM bundle
// exported by most tools arrives.
func Parse(spec string) (Entry, error) {
	host, files, ok := strings.Cut(spec, "=")
	if !ok {
		return Entry{}, fmt.Errorf("client certificate %q: expected <host>=<cert file>[,<key file>]", spec)
	}

	e := Entry{Host: strings.ToLower(strings.TrimSpace(host))}
	if e.Host == "" {
		return Entry{}, fmt.Errorf("client certificate %q: no host", spec)
	}

	if _, err := path.Match(e.Host, "probe"); err != nil {
		return Entry{}, fmt.Errorf("client certificate %q: %q is not a host pattern: %w", spec, e.Host, err)
	}

	cert, key, _ := strings.Cut(files, ",")
	e.Cert = strings.TrimSpace(cert)
	e.Key = strings.TrimSpace(key)

	if e.Cert == "" {
		return Entry{}, fmt.Errorf("client certificate %q: no certificate file", spec)
	}

	return e, nil
}

// Summary is one loaded certificate as the console shows it. No key
// material and no key path: what is useful is which hosts it is for and
// whether it is still valid.
type Summary struct {
	Host     string    `json:"host"`
	Subject  string    `json:"subject"`
	Issuer   string    `json:"issuer"`
	NotAfter time.Time `json:"not_after"`
	Expired  bool      `json:"expired"`
	Path     string    `json:"path"`
}

type loaded struct {
	entry Entry
	cert  tls.Certificate
	leaf  *x509.Certificate
}

// Keeper holds the loaded certificates and answers which one a host
// needs.
type Keeper struct {
	entries []loaded

	mu         sync.Mutex
	transports map[string]*http.Transport
}

// Load reads and parses every file NOW, so a wrong path or a key that
// does not match its certificate is a startup error naming the file
// rather than a failed handshake in the middle of somebody's work.
//
// Nil and no error when there is nothing configured, which is the
// ordinary case: the caller then changes nothing.
func Load(entries []Entry) (*Keeper, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	k := &Keeper{transports: map[string]*http.Transport{}}

	for _, e := range entries {
		keyPath := e.Key
		if keyPath == "" {
			keyPath = e.Cert
		}

		cert, err := tls.LoadX509KeyPair(e.Cert, keyPath)
		if err != nil {
			return nil, fmt.Errorf("client certificate for %s: %w", e.Host, err)
		}

		// LoadX509KeyPair leaves Leaf nil, and every read of the
		// certificate's own facts needs it parsed.
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("client certificate for %s: %w", e.Host, err)
		}

		cert.Leaf = leaf
		k.entries = append(k.entries, loaded{entry: e, cert: cert, leaf: leaf})
	}

	return k, nil
}

// For returns the certificate to present to host, or nil when no
// pattern names it. The port is not part of the match, since a
// certificate belongs to a name rather than to a port.
func (k *Keeper) For(host string) *tls.Certificate {
	if k == nil {
		return nil
	}

	host = strings.ToLower(strings.Trim(host, "[]"))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	for i := range k.entries {
		pattern := k.entries[i].entry.Host
		if pattern == host {
			return &k.entries[i].cert
		}

		// path.Match's separator is /, which a host never has, so a *
		// spans the whole name.
		if ok, _ := path.Match(pattern, host); ok {
			return &k.entries[i].cert
		}
	}

	return nil
}

// Summaries is what the console shows, in the order they were
// configured.
func (k *Keeper) Summaries() []Summary {
	if k == nil {
		return nil
	}

	now := time.Now()
	out := make([]Summary, 0, len(k.entries))

	for _, e := range k.entries {
		out = append(out, Summary{
			Host:     e.entry.Host,
			Subject:  e.leaf.Subject.String(),
			Issuer:   e.leaf.Issuer.String(),
			NotAfter: e.leaf.NotAfter,
			Expired:  now.After(e.leaf.NotAfter),
			Path:     e.entry.Cert,
		})
	}

	return out
}

// Hosts are the configured patterns, for a log line at startup.
func (k *Keeper) Hosts() []string {
	if k == nil {
		return nil
	}

	out := make([]string, 0, len(k.entries))
	for _, e := range k.entries {
		out = append(out, e.entry.Host)
	}

	return out
}

// Wrap returns a RoundTripper that sends a request whose host needs a
// client certificate through a transport carrying it, and everything
// else through base unchanged.
//
// A transport per host rather than one tls.Config with a callback,
// because there is no client-side hook that is told WHICH host is being
// dialled: GetClientCertificate is given the server's acceptable CAs and
// nothing else. http.Transport.DialTLSContext does know the address,
// but the transport ignores it for a request that goes through a proxy -
// so a certificate would be presented on a direct connection and
// silently not presented through an upstream proxy, which is the worst
// of the three options.
func (k *Keeper) Wrap(base *http.Transport) http.RoundTripper {
	if k == nil {
		return base
	}

	return &switcher{keeper: k, base: base}
}

type switcher struct {
	keeper *Keeper
	base   *http.Transport
}

func (s *switcher) RoundTrip(req *http.Request) (*http.Response, error) {
	cert := s.keeper.For(req.URL.Hostname())
	if cert == nil {
		return s.base.RoundTrip(req)
	}

	return s.keeper.transportFor(s.base, req.URL.Hostname(), cert).RoundTrip(req)
}

// transportFor is base with one certificate added, built once per host
// and kept, so connections to it are pooled like any other.
func (k *Keeper) transportFor(base *http.Transport, host string, cert *tls.Certificate) *http.Transport {
	k.mu.Lock()
	defer k.mu.Unlock()

	// Keyed by the base too: the sender has two transports, one that
	// offers h2 and one that never does, and they must not be confused
	// for each other.
	key := fmt.Sprintf("%p|%s", base, host)
	if tr, ok := k.transports[key]; ok {
		return tr
	}

	tr := base.Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	tr.TLSClientConfig.Certificates = []tls.Certificate{*cert}
	k.transports[key] = tr

	return tr
}
