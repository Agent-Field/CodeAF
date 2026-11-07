// Package trigger is the factory's Trigger API: the message and status doors
// ([Message], [Status], [StatusOfJob]) that AgentField's `message` and
// `status` reasoners call in-process.
//
// THEY HAVE NO HTTP DOOR OF THEIR OWN. A WorkAF user never thinks about code:
// built-in connectors (GitHub, Jira) are set up in the app with clicks, and
// anything programmatic enters through AgentField, so the factory serves no
// /api/intake route and posts no signed webhook. Filing stays inbox.Add, THE
// ONE DOOR, and a job's moments reach AgentField and Jira over the inbox's
// event bus. docs/design/factory-general/DESIGN.md ("The line between WorkAF
// and AgentField") is the line this package keeps.
package trigger

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── THE DOORS ANY CALLER USES ──────────────────────────────────────────────
//
// AgentField's `message` and `status` reasoners call these directly; nothing
// reaches them over HTTP from outside the node. Neither door decides
// anything of its own: the message is inbox.Message, the comment path every
// GitHub comment takes; the status is the job wire's own facts.

// Said is the message door's question; see [inbox.Said].
type Said = inbox.Said

// Accepted is the message door's answer: the job the words reached, and what
// its manager made of them.
type Accepted struct {
	Job      int    `json:"job"`
	Accepted bool   `json:"accepted"`
	Outcome  string `json:"outcome,omitempty"`
	Line     string `json:"line,omitempty"`
}

// Message gives s's words to the job s.Source filed as s.ID, as a comment on
// it ([inbox.Message]). It refuses with [inbox.ErrNoJob] for a pair never
// filed and [inbox.Refused] for words it cannot take.
func Message(ctx context.Context, st *store.Store, s Said, now time.Time) (Accepted, error) {
	heard, err := inbox.Message(ctx, st, s, now)
	if err != nil {
		return Accepted{}, err
	}
	return Accepted{Job: heard.Job, Accepted: true, Outcome: heard.Outcome, Line: heard.Line}, nil
}

// Status is how far the job source filed as id is ([api.IntakeStatusOf]),
// or [inbox.ErrNoJob].
func Status(st *store.Store, source, id string) (api.IntakeStatus, error) {
	it, err := inbox.Find(st, source, id)
	if err != nil {
		return api.IntakeStatus{}, err
	}
	return api.IntakeStatusOf(it), nil
}

// StatusOfJob is [Status] by the job's own number, for a caller that holds
// it (AgentField's `status {job}`). A job the inbox did not file is
// [inbox.ErrNoJob] too: this door reads only outside work.
func StatusOfJob(st *store.Store, job int) (api.IntakeStatus, error) {
	it, err := st.GetSaved(job)
	if err != nil {
		return api.IntakeStatus{}, inbox.ErrNoJob
	}
	if _, _, ok := inbox.Split(it); !ok {
		return api.IntakeStatus{}, inbox.ErrNoJob
	}
	return api.IntakeStatusOf(it), nil
}
