// Package ids mints the identifiers every stored row carries.
//
// One place, so a table can never hold two id shapes. UUIDv7 rather
// than v4: the first 48 bits are a millisecond timestamp, so an id
// sorts by creation time and a keyset cursor over the primary key is a
// cursor over time - which is what every list in the product wants.
package ids

import (
	"time"
	"uuid"
)

// New mints a fresh UUIDv7 in its canonical string form.
func New() string {
	return uuid.NewV7().String()
}

// NewAt mints a UUIDv7 whose timestamp is t rather than now, so a row
// imported from elsewhere sorts by when it actually happened. Without it
// an imported log would key in file order at "now" and the list, which
// orders by the key, would disagree with every created_at it shows.
//
// Only the 48 timestamp bits are replaced. The version and variant bits
// and the random tail are the minted ones, so two rows in the same
// millisecond still differ.
func NewAt(t time.Time) string {
	u := uuid.NewV7()

	ms := t.UnixMilli()
	if ms < 0 {
		ms = 0
	}

	for i := range 6 {
		u[i] = byte(ms >> (40 - 8*i))
	}

	return u.String()
}

// Valid reports whether s parses as a UUID of any version.
func Valid(s string) bool {
	_, err := uuid.Parse(s)

	return err == nil
}

// MintedAt recovers the millisecond timestamp a v7 id was minted with.
// The zero time is returned for anything that is not a v7 id, so a
// caller can fall back to a stored created_at.
func MintedAt(id string) time.Time {
	u, err := uuid.Parse(id)
	if err != nil || u[6]>>4 != 7 {
		return time.Time{}
	}

	ms := int64(u[0])<<40 | int64(u[1])<<32 | int64(u[2])<<24 |
		int64(u[3])<<16 | int64(u[4])<<8 | int64(u[5])

	return time.UnixMilli(ms).UTC()
}
