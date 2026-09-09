package ids

import (
	"testing"
	"time"
)

func TestNewIsOrderedAndDecodable(t *testing.T) {
	a := New()
	time.Sleep(2 * time.Millisecond)
	b := New()

	if !Valid(a) || !Valid(b) {
		t.Fatalf("minted ids do not parse: %q %q", a, b)
	}

	if a >= b {
		t.Fatalf("v7 ids must sort by time: %q >= %q", a, b)
	}

	at := MintedAt(a)
	if at.IsZero() || time.Since(at) > time.Minute {
		t.Fatalf("MintedAt(%q) = %v", a, at)
	}

	if !MintedAt("not-an-id").IsZero() {
		t.Fatal("MintedAt of garbage must be the zero time")
	}
}
