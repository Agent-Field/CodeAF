package factory

import (
	"context"
	"time"
)

// Source is the connector contract: where items come from and where results
// go back. A chat, GitHub, GitLab, Linear, a mail folder and a fixture are all
// the same shape, so the floor never learns a vendor. Every item carries the
// source's name in [Item.Origin], which is an open string for that reason.
//
// THE CHAT IS A SOURCE TOO, AND THE FIRST ONE BUILT. Work a conversation
// splits off is already a row in the plan store; the chat source reads those
// rows as items, so the floor is useful before any forge is connected and the
// forge connectors are a matter of filling this contract.
//
// Read is cheap and polled; the floor's law is no server and no port, so a
// webhook is a later lane over the relay. Write is consent-gated by the
// surface: nothing posts without a person, and a stranger's words never
// reach Write at all.
type Source interface {
	// Name is the origin written on every item it produces: "chat", "github".
	Name() string
	// Read returns what changed since the mark, and the next mark. A source
	// that cannot say "since" returns everything and the same mark.
	Read(ctx context.Context, since string) (items []Item, next string, err error)
	// Write performs one outward action on one item: a comment, a review, a
	// label, a branch and pull request, an issue opened from the terminal.
	// Sources that cannot write return ErrReadOnly.
	Write(ctx context.Context, action Action) (Receipt, error)
}

// Action is one outward move, named by verb so a source can refuse the ones
// it does not have.
type Action struct {
	Verb   string // comment · review · label · open-pr · open-issue · close
	Item   Item
	Body   string
	Labels []string
	Branch string
	// Draft asks the source to stage rather than publish where it can (a
	// draft PR, a pending review). The surface sets it for anything a person
	// has not signed off on.
	Draft bool
}

// Receipt is what came back: the thing's address on the source, so provenance
// runs both ways. "Why did I get this" always has a door, pointed outward too.
type Receipt struct {
	URL string
	ID  string
	At  time.Time
}

// ErrReadOnly is a source that only reads.
var ErrReadOnly = readOnly{}

type readOnly struct{}

func (readOnly) Error() string { return "this source only reads" }

// Sources is what the seam serves: the connected ones by name, so the surface
// can say "3 repos on github · chat" and offer `open on github` only where a
// writing source exists.
type SourceInfo struct {
	Name   string
	Writes bool
	Repos  []string
	Polled time.Time
	// Trouble is what went wrong with the source's last read, in the few
	// words the facts line says after its name (`not reachable`), or "" when
	// the last read was good. Polled stays the last GOOD read, so a source in
	// trouble still says how stale the floor is.
	Trouble string
	// Polling is true while a read of the source is in flight, so the floor
	// can say it is being read rather than leave a person guessing.
	Polling bool
	// Reading is the repository the read is on now, `owner/name`, and Read of
	// Of how many of the watched repositories this read has finished, so a
	// floor with nothing on it yet can say `reading Agent-Field/CodeAF · 1 of
	// 3` instead of quiet. All three are zero when no read is in flight.
	Reading string
	Read    int
	Of      int
	// Items is how many issues and pull requests the read has listed so far,
	// so a big repository's read says `200 items so far` rather than go
	// quiet; zero when no read is in flight.
	Items int
}
