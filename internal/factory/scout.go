package factory

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Scout is what Look closer found: a model with read-only tools on the
// repository's checkout, under a hard budget, asked what the work touches.
// Every path in it was checked to exist at Head before it was kept; Dropped
// counts the ones that did not, and Cited every path the answer named.
type Scout struct {
	Files    []ScoutFile `json:",omitempty"`
	Plan     []string    `json:",omitempty"`
	Unknowns []string    `json:",omitempty"`
	Tests    []string    `json:",omitempty"`
	Rubric   *Rubric     `json:",omitempty"`
	Cited    int         `json:",omitempty"`
	Dropped  int         `json:",omitempty"`
	// Calls is how many tool calls it made; USD what its model calls cost;
	// Model the model they ran on; Took how long it ran.
	Calls int           `json:",omitempty"`
	USD   float64       `json:",omitempty"`
	Model string        `json:",omitempty"`
	Took  time.Duration `json:",omitempty"`
	// Body is [TextHash] of the item when it looked, and Head the commit it
	// read, so a changed issue or a moved branch reads as an older look.
	Body string
	Head string
	At   time.Time
}

// ScoutFile is one file or folder the scout expects the work to change.
type ScoutFile struct {
	Path string
	Why  string `json:",omitempty"`
}

// TextHash is the item's title and body as one hash: what a scout's look was
// of, so an edited issue is seen to have moved on.
func TextHash(it Item) string {
	sum := sha256.Sum256([]byte(it.Title + "\x00" + it.Body))
	return hex.EncodeToString(sum[:8])
}

// Stale says the issue changed since the scout looked.
func (s Scout) Stale(it Item) bool { return s.Body != TextHash(it) }
