package ids

import (
	"testing"
	"time"
)

func TestNewAt(t *testing.T) {
	want := time.Date(2025, 3, 4, 5, 6, 7, 8e6, time.UTC)

	seen := map[string]bool{}
	for range 100 {
		id := NewAt(want)
		if !Valid(id) {
			t.Fatalf("not a uuid: %s", id)
		}

		if got := MintedAt(id); !got.Equal(want) {
			t.Fatalf("MintedAt(%s) = %s, want %s", id, got, want)
		}

		if seen[id] {
			t.Fatalf("duplicate id in the same millisecond: %s", id)
		}

		seen[id] = true
	}
}
