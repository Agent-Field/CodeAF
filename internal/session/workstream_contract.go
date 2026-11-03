package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/workstream"
)

// WorkstreamDoor is the workstream agent's one way into its own workstream.
type WorkstreamDoor interface {
	// Brief returns a non-blocking snapshot, or false when none is ready.
	Brief() (WorkstreamBrief, bool)
	Read(context.Context, WorkstreamReadAsk) (json.RawMessage, error)
	Propose(context.Context, WorkstreamProposal) (WorkstreamProposed, error)
	Plan(context.Context) (WorkstreamPlanState, error)
	SetPlan(context.Context, int, WorkstreamPlanState) (WorkstreamPlanState, error)
	WritePage(context.Context, WorkstreamPageWrite) (WorkstreamPageWritten, error)
}

var ErrWorkstreamMissing = errors.New("No workstream holds this conversation.")

var ErrWorkstreamDone = errors.New("This workstream is done; nothing was added or written.")

var ErrWorkstreamVersion = errors.New("the workstream version changed")

var ErrWorkstreamNotAgentPage = errors.New("That page is the person's. You write only your own pages; suggesting edits on theirs is not available yet.")

var ErrWorkstreamTooLarge = errors.New("That page is too large; the encoded document must be at most 1 MiB.")

const WorkstreamGoAheadProposalPrefix = "Proposed go-ahead (not granted): "

// WorkstreamWhereItStandsTitle is the agent page the app shows on the Overview as
// the agent's summary, so the agent keeps the page with exactly this title current.
const WorkstreamWhereItStandsTitle = "Where it stands"

type WorkstreamBrief struct {
	Name string
	Why  string
}

type WorkstreamReadAsk struct {
	What   string `json:"what"`
	PageID string `json:"page_id,omitempty"`
	Kind   string `json:"kind,omitempty"`
	State  string `json:"state,omitempty"`
}

type WorkstreamProposal struct {
	Kind    string
	Text    string
	Source  *workstream.Source
	GoAhead *WorkstreamGoAheadAsk
}

type WorkstreamProposed struct {
	ID string
}

type WorkstreamPlanState struct {
	Version    int
	Milestones []workstream.Milestone
	Waves      []workstream.Wave
}

type WorkstreamGoAheadAsk struct {
	Scope       string  `json:"scope"`
	Factory     string  `json:"factory,omitempty"`
	Job         int     `json:"job,omitempty"`
	Wave        int     `json:"wave,omitempty"`
	MilestoneID string  `json:"milestone_id,omitempty"`
	CapUSD      float64 `json:"cap_usd"`
	EndsOn      string  `json:"ends_on"`
	Why         string  `json:"why"`
}

type WorkstreamPageWrite struct {
	PageID      string             `json:"page_id,omitempty"`
	Title       string             `json:"title,omitempty"`
	ParentID    string             `json:"parent_id,omitempty"`
	Markdown    string             `json:"markdown"`
	UpdatedFrom string             `json:"updated_from"`
	Source      *workstream.Source `json:"source,omitempty"`
}

type WorkstreamPageWritten struct {
	ID      string
	Title   string
	Version int
}
