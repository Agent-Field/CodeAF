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
