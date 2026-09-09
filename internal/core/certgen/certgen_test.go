package certgen

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateCARoundTrip(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca_key.pem")

	cert, _, minted, err := LoadOrCreateCA(certPath, keyPath, "ihttp", "ihttp")
	if err != nil || !minted {
		t.Fatalf("first call: minted=%v err=%v", minted, err)
	}

	again, _, minted, err := LoadOrCreateCA(certPath, keyPath, "ihttp", "ihttp")
	if err != nil || minted {
		t.Fatalf("second call: minted=%v err=%v", minted, err)
	}

	if !cert.Equal(again) {
		t.Fatal("the loaded CA must be the one written")
	}
}

func TestLeafIsCachedAndVerifies(t *testing.T) {
	ca, key, err := MintCA("ihttp", "ihttp")
	if err != nil {
		t.Fatal(err)
	}

	a, err := New(ca, key)
	if err != nil {
		t.Fatal(err)
	}

	first, err := a.Leaf("example.com:443")
	if err != nil {
		t.Fatal(err)
	}

	second, err := a.Leaf("example.com")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatal("a leaf for the same host must come from the cache")
	}

	roots := x509.NewCertPool()
	roots.AddCert(ca)

	if _, err := first.Leaf.Verify(x509.VerifyOptions{DNSName: "example.com", Roots: roots}); err != nil {
		t.Fatalf("leaf does not verify: %v", err)
	}

	ipLeaf, err := a.Leaf("127.0.0.1")
	if err != nil || len(ipLeaf.Leaf.IPAddresses) != 1 {
		t.Fatalf("ip leaf: %v %v", ipLeaf, err)
	}

	cfg := a.TLSConfig()
	if _, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "a.test"}); err != nil {
		t.Fatal(err)
	}
}

// The cache is bounded, and what goes is the host that has not been
// asked for in the longest time.
func TestLeafCacheIsBoundedAndEvictsTheLeastRecentlyUsed(t *testing.T) {
	ca, key, err := MintCA("ihttp test CA", "ihttp")
	if err != nil {
		t.Fatal(err)
	}

	a, err := New(ca, key)
	if err != nil {
		t.Fatal(err)
	}

	// Fill it exactly.
	for i := range MaxCachedLeaves {
		if _, err := a.Leaf(fmt.Sprintf("host-%d.example.com", i)); err != nil {
			t.Fatal(err)
		}
	}

	if n := a.CachedLeaves(); n != MaxCachedLeaves {
		t.Fatalf("%d leaves cached, want the cap of %d", n, MaxCachedLeaves)
	}

	// Touch the oldest, so it is no longer the oldest.
	first, err := a.Leaf("host-0.example.com")
	if err != nil {
		t.Fatal(err)
	}

	// One more host evicts something, and the cache stays at the cap.
	if _, err := a.Leaf("newcomer.example.com"); err != nil {
		t.Fatal(err)
	}

	if n := a.CachedLeaves(); n != MaxCachedLeaves {
		t.Errorf("%d leaves after one over the cap, want %d", n, MaxCachedLeaves)
	}

	// host-0 was used most recently of the old ones, so it is still
	// there - the same pointer, not a fresh mint.
	again, err := a.Leaf("host-0.example.com")
	if err != nil {
		t.Fatal(err)
	}

	if again != first {
		t.Error("the most recently used host was evicted")
	}

	// host-1 is the one that had gone longest without use, so it went
	// and comes back as a new certificate.
	before, err := a.Leaf("host-1.example.com")
	if err != nil {
		t.Fatal(err)
	}

	after, err := a.Leaf("host-1.example.com")
	if err != nil {
		t.Fatal(err)
	}

	if before != after {
		t.Error("a cached leaf was not reused, so the cache is not working at all")
	}

	// And a leaf still verifies after all that eviction.
	pool := x509.NewCertPool()
	pool.AddCert(a.CA())

	if _, err := after.Leaf.Verify(x509.VerifyOptions{
		DNSName: "host-1.example.com",
		Roots:   pool,
	}); err != nil {
		t.Errorf("an evicted-and-reminted leaf does not verify: %v", err)
	}
}
