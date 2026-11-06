// Package inbox is the factory's generic intake: work that arrives from any
// outside system — a Jira ticket, a Sentry alert, a form, a script, a person's
// `codeaf factory add` — becomes one job, the same way a GitHub issue does.
// A connector (internal/factory/jira) and the trigger doors (the wire, the
// command line, the factory's HTTP endpoint) all come through [Add], so
// deduplication and routing are written once.
//
// docs/design/factory-general/DESIGN.md is the contract this package fills.
package inbox

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Entry is one piece of outside work, in the source's own words.
type Entry struct {
	// Source names the system: "jira", or "inbox:<name>" for a trigger.
	Source string `json:"source"`
	// ExternalID is the item's key on that system ("ENG-142"). Source and
	// ExternalID together are the identity: a second Add of the same pair
	// updates the job it already made and never makes a second one.
	ExternalID string   `json:"external_id"`
	URL        string   `json:"url,omitempty"`
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Author     string   `json:"author,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Priority   string   `json:"priority,omitempty"` // p0..p3; the source maps its own scale
	// Repo is the repository the sender already knows the work is on. Empty
	// leaves it to the routing rules; with neither, the job waits in Backlog
	// asking which repository.
	Repo string `json:"repo,omitempty"`
}

// Result says what Add did.
type Result struct {
	ID      int  `json:"id"`
	Created bool `json:"created"`
}

// ErrNotBuilt is the stub's answer until the inbox lane lands.
var ErrNotBuilt = errors.New("the inbox is not built yet")

// Add files e on the floor, or updates the job it already made. The inbox
// lane fills this in.
func Add(ctx context.Context, st *store.Store, e Entry, now time.Time) (Result, error) {
	_, _, _, _ = ctx, st, e, now
	return Result{}, ErrNotBuilt
}

// Feed is a connector that pulls outside work: Jira, Linear, a mail folder.
// A feed only translates; it never decides policy (that is internal/factory/route)
// and never writes the store (that is [Add]). [Poll] drives every feed the
// same way, so a new connector is one small type and no new loop.
type Feed interface {
	// Name is the source every entry it returns carries, such as "jira".
	Name() string
	// Fetch returns what changed since cursor and the next cursor. A feed that
	// cannot say "since" returns everything and the same cursor; [Add] makes
	// the repeat harmless.
	Fetch(ctx context.Context, cursor string) (entries []Entry, next string, err error)
}

// Replier is a feed that can answer back on its own system: a comment with
// the pull request's link, a note that the work merged. It is optional; a feed
// that only reads is simply not a Replier.
type Replier interface {
	Reply(ctx context.Context, externalID string, ev Event) error
}

// EventKind is a moment in a job's life worth telling the source about.
type EventKind string

const (
	EventPROpened EventKind = "pr_opened"
	EventMerged   EventKind = "merged"
	EventStuck    EventKind = "stuck" // the job stopped and needs a person
)

// Event is one such moment, already in a person's words.
type Event struct {
	Kind  EventKind `json:"kind"`
	JobID int       `json:"job_id"`
	URL   string    `json:"url,omitempty"` // the pull request
	Text  string    `json:"text"`
}

// Poll runs f every interval until ctx ends, filing each entry through [Add]
// and calling after (intake) once a pass. The inbox lane fills this in.
func Poll(ctx context.Context, f Feed, st *store.Store, every time.Duration, after func(context.Context) error) {
	_, _, _, _, _ = ctx, f, st, every, after
}

// Notify tells the job's source about ev when that source registered a
// [Replier]. The inbox lane fills this in and calls it where a job opens its
// pull request and where it merges; it never fails the job.
func Notify(ctx context.Context, st *store.Store, jobID int, ev Event) {
	_, _, _, _ = ctx, st, jobID, ev
}

// RegisterReplier makes r the answerer for every job whose source is name.
func RegisterReplier(name string, r Replier) { _, _ = name, r }
