package shared

import "github.com/oklog/ulid/v2"

// NewID returns a fresh ULID. Every id in Hookary comes from here, so that ids
// stay time-sortable and match the `ulid` domain the database enforces.
//
// ulid.Make is safe for concurrent use.
func NewID() string {
	return ulid.Make().String()
}

// ValidID reports whether s is a well-formed ULID. Use it to reject a bad path
// parameter with a 404 before it reaches the database and trips the domain
// check as a 500.
func ValidID(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}
