// Package certgen is the proxy's certificate authority: it loads or
// mints the root the operator trusts, and issues a leaf for every host
// a client asks the proxy to CONNECT to.
//
// The leaf key is minted once per Authority and shared by every leaf.
// The leaves themselves are cached by host, because a browser opens a
// dozen connections to one origin. The cache is BOUNDED: the set of
// hosts a proxy is asked about is not, and a leaf is not small.
package certgen

import (
	"container/list"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // The Subject Key Identifier is defined over SHA-1 (RFC 5280 4.2.1.2).
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Leaf lifetime and how early a cached leaf is reissued. A day is what
// browsers accept without complaint for a locally trusted CA, and the
// margin keeps a connection opened at the edge of the window from
// handshaking with a certificate that expires mid-session.
const (
	leafValidity = 24 * time.Hour
	leafMargin   = time.Hour
	caValidity   = 10 * 365 * 24 * time.Hour
)

// MaxCachedLeaves bounds the cache. A browsing session touches tens of
// hosts and a busy one hundred, so this is far above ordinary use and
// still a fixed ceiling. Past it the least recently USED host goes,
// which is the one least likely to be asked for next - a fresh mint
// costs one signature, so a wrong eviction is cheap and a leak is not.
const MaxCachedLeaves = 1000

// Authority issues leaf certificates under one root. Safe for
// concurrent use.
type Authority struct {
	ca      *x509.Certificate
	caKey   crypto.Signer
	leafKey *ecdsa.PrivateKey
	keyID   []byte

	// The cache and its use order under one lock. list holds the hosts,
	// most recently used at the front, and cache points at the element that
	// holds each one, so a hit is O(1) and so is moving it.
	mu    sync.Mutex
	cache map[string]*cacheEntry
	order *list.List
}

// cacheEntry is one leaf and its place in the use order.
type cacheEntry struct {
	cert *tls.Certificate
	at   *list.Element
}

// New builds an Authority over an existing root.
func New(ca *x509.Certificate, caKey crypto.Signer) (*Authority, error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("certgen: leaf key: %w", err)
	}

	keyID, err := subjectKeyID(leafKey.Public())
	if err != nil {
		return nil, err
	}

	return &Authority{
		ca:      ca,
		caKey:   caKey,
		leafKey: leafKey,
		keyID:   keyID,
		cache:   make(map[string]*cacheEntry, MaxCachedLeaves),
		order:   list.New(),
	}, nil
}

// CA returns the root certificate, for the download the console offers.
func (a *Authority) CA() *x509.Certificate {
	return a.ca
}

// CAPEM renders the root as a PEM block.
func (a *Authority) CAPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.ca.Raw})
}

// TLSConfig is the server config the proxy hands a hijacked CONNECT
// tunnel: the certificate is chosen from the SNI per handshake. HTTP/2 is
// offered first, and a proxy that does not want it takes it out.
func (a *Authority) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			host := hello.ServerName
			if host == "" {
				// No SNI: fall back to the address the client connected
				// to, which the CONNECT handler put on the connection.
				if addr, ok := hello.Conn.LocalAddr().(*namedAddr); ok {
					host = addr.host
				}
			}

			if host == "" {
				return nil, errors.New("certgen: client sent no server name")
			}

			return a.Leaf(host)
		},
	}
}

// Leaf returns a certificate for host, minting one when the cache has
// none or the cached one is near expiry. A port on host is ignored.
func (a *Authority) Leaf(host string) (*tls.Certificate, error) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	if c, ok := a.cached(host); ok {
		return c, nil
	}

	// Minted outside the lock, so a cold cache does not serialize every
	// handshake behind one signature. The first to store wins.
	c, err := a.mint(host)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Another handshake for the same host may have got there first.
	if have, ok := a.cache[host]; ok && time.Until(have.cert.Leaf.NotAfter) > leafMargin {
		a.order.MoveToFront(have.at)

		return have.cert, nil
	}

	a.store(host, c)

	return c, nil
}

// store puts a leaf in the cache, replacing any entry for the host and
// dropping the least recently used one when the cache is full. Called
// under the lock.
func (a *Authority) store(host string, c *tls.Certificate) {
	if have, ok := a.cache[host]; ok {
		have.cert = c
		a.order.MoveToFront(have.at)

		return
	}

	a.cache[host] = &cacheEntry{cert: c, at: a.order.PushFront(host)}

	for a.order.Len() > MaxCachedLeaves {
		oldest := a.order.Back()
		if oldest == nil {
			return
		}

		a.order.Remove(oldest)
		delete(a.cache, oldest.Value.(string))
	}
}

func (a *Authority) cached(host string) (*tls.Certificate, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	e, ok := a.cache[host]
	if !ok {
		return nil, false
	}

	// Near expiry is a miss, and the entry is left where it is: the
	// mint that follows replaces it.
	if time.Until(e.cert.Leaf.NotAfter) <= leafMargin {
		return nil, false
	}

	a.order.MoveToFront(e.at)

	return e.cert, true
}

// CachedLeaves is how many leaves are held, for a test and for a debug
// line. Not a metric anything depends on.
func (a *Authority) CachedLeaves() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return len(a.cache)
}

func (a *Authority) mint(host string) (*tls.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host, Organization: a.ca.Subject.Organization},
		SubjectKeyId:          a.keyID,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(leafValidity),
	}

	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}

	raw, err := x509.CreateCertificate(rand.Reader, tmpl, a.ca, a.leafKey.Public(), a.caKey)
	if err != nil {
		return nil, fmt.Errorf("certgen: sign leaf for %s: %w", host, err)
	}

	leaf, err := x509.ParseCertificate(raw)
	if err != nil {
		return nil, fmt.Errorf("certgen: parse leaf: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{raw, a.ca.Raw},
		PrivateKey:  a.leafKey,
		Leaf:        leaf,
	}, nil
}

// MintCA creates a fresh root. RSA rather than ECDSA because the trust
// store tooling on every platform accepts an RSA root without a second
// thought, and the root is signed once per leaf, not once per byte.
func MintCA(name, organization string) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("certgen: CA key: %w", err)
	}

	keyID, err := subjectKeyID(key.Public())
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name, Organization: []string{organization}},
		SubjectKeyId:          keyID,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
	}

	raw, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, nil, fmt.Errorf("certgen: self-sign CA: %w", err)
	}

	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("certgen: parse CA: %w", err)
	}

	return cert, key, nil
}

// LoadOrCreateCA reads the PEM pair at certPath and keyPath, minting and
// writing a new pair when neither exists. Exactly one of the two files
// existing is an error, since a root the operator trusts must keep its
// key. The bool reports whether a new root was minted.
func LoadOrCreateCA(certPath, keyPath, name, organization string) (*x509.Certificate, crypto.Signer, bool, error) {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)

	switch {
	case certErr == nil && keyErr == nil:
		cert, key, err := loadCA(certPath, keyPath)

		return cert, key, false, err
	case errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist):
	case certErr != nil && !errors.Is(certErr, os.ErrNotExist):
		return nil, nil, false, fmt.Errorf("certgen: stat %s: %w", certPath, certErr)
	case keyErr != nil && !errors.Is(keyErr, os.ErrNotExist):
		return nil, nil, false, fmt.Errorf("certgen: stat %s: %w", keyPath, keyErr)
	default:
		return nil, nil, false, fmt.Errorf("certgen: only one of %s and %s exists - remove it to mint a new pair", certPath, keyPath)
	}

	cert, key, err := MintCA(name, organization)
	if err != nil {
		return nil, nil, false, err
	}

	if err := writeCA(certPath, keyPath, cert, key); err != nil {
		return nil, nil, false, err
	}

	return cert, key, true, nil
}

func loadCA(certPath, keyPath string) (*x509.Certificate, crypto.Signer, error) {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("certgen: load CA pair: %w", err)
	}

	cert := pair.Leaf

	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, nil, errors.New("certgen: CA private key cannot sign")
	}

	if !cert.IsCA {
		return nil, nil, fmt.Errorf("certgen: %s is not a CA certificate", certPath)
	}

	return cert, signer, nil
}

func writeCA(certPath, keyPath string, cert *x509.Certificate, key *rsa.PrivateKey) error {
	for _, p := range []string{certPath, keyPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("certgen: create %s: %w", filepath.Dir(p), err)
		}
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil { //nolint:gosec // The certificate is public by design.
		return fmt.Errorf("certgen: write %s: %w", certPath, err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("certgen: encode CA key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("certgen: write %s: %w", keyPath, err)
	}

	return nil
}

func subjectKeyID(pub crypto.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("certgen: encode public key: %w", err)
	}

	sum := sha1.Sum(der) //nolint:gosec // See the import comment.

	return sum[:], nil
}

// maxSerial bounds a serial to the 20 bytes RFC 5280 allows.
var maxSerial = new(big.Int).Lsh(big.NewInt(1), 159)

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, maxSerial)
	if err != nil {
		return nil, fmt.Errorf("certgen: serial: %w", err)
	}

	return serial, nil
}

// namedAddr is a net.Addr that carries the host a CONNECT named, so a
// handshake with no SNI can still be answered with a certificate for the
// host the client asked for.
type namedAddr struct {
	net.Addr
	host string
}

// WithConnectHost wraps conn so its LocalAddr also carries host. The
// tunnel handler applies it before the TLS handshake.
func WithConnectHost(conn net.Conn, host string) net.Conn {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	return &namedConn{Conn: conn, addr: &namedAddr{Addr: conn.LocalAddr(), host: host}}
}

type namedConn struct {
	net.Conn
	addr net.Addr
}

func (c *namedConn) LocalAddr() net.Addr {
	return c.addr
}
