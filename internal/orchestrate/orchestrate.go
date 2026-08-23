// Package orchestrate is the adaptive runner: a planner model and a frontier
// scheduler, married. The planner never works and the scheduler never thinks.
// A node's needs are the only launch gate code consults; the planner amends
// the frontier on every completion, concurrently, so execution never blocks
// on thought. The graph is never designed — it crystallizes as the trace of
// what the planner did.
//
// This file is the CONTRACT: types the session, the room, the roster, and the
// rig all code against. Behavior lands behind these shapes.
package orchestrate

import (
	"context"
	"strings"
	"unicode"
)

// NameWords is how many words a node's [Node.Title] may run to.
//
// THREE IS THE LENGTH A PERSON READS AS A LABEL rather than as a sentence, and
// it is a cap and not a target — a two-word name is left at two. It is the
// figure the whole product names work by: internal/session's TaskNameWords is
// this constant, and the rail column that draws these rows cuts to the same
// number (internal/tui3's taskTitleWords). One figure, because a planner asked
// for more words than the column can show is a planner being billed for words
// thrown away on the way to the screen.
const NameWords = 3

// Node is one small unit of work the planner wants. Small is the law: one
// question, one artifact, a handful of turns; parallelism comes from node
// count, never node size.
type Node struct {
	ID string `json:"id"`
	// Title is the TWO OR THREE WORDS THIS NODE IS CALLED, and it is a
	// first-class field rather than something a surface derives.
	//
	// IT EXISTS BECAUSE A PROMPT IS NOT A NAME. Every surface that draws work
	// draws a short title — the rail's column, home's cards, a roster row — and
	// until this field existed there was nothing on a node to draw but Goal, so
	// each of them cut the first few words off a self-contained brief. Law 4
	// makes those briefs open in the second person, so a run divided nine ways
	// drew nine rows all reading "You are a": the column named every worker
	// after how its instructions cleared their throat, and a person tracking one
	// of them had to read every row.
	//
	// THE PLANNER FILLS IT, and a node that arrives without one is named from
	// its ID — which is already a slug the planner minted as a name — by
	// [NodeTitle]. Nothing anywhere derives a title by truncating prose.
	Title string   `json:"title"`
	Goal  string   `json:"goal"`            // self-contained: no "see above"
	Needs []string `json:"needs,omitempty"` // ids that must be Done before this may run
	Kind  string   `json:"kind,omitempty"`  // subharness node kind; empty is agent.loop
	// WriteScope is the set of repo paths this node may write (law 10). Two
	// nodes whose scopes intersect are not independent, and the scheduler
	// serializes them by force of an added Needs edge. Read-only nodes leave
	// this empty.
	WriteScope []string `json:"write_scope,omitempty"`
	// Worktree says the planner judged this node needs an isolated worktree
	// (law: hybrid collision policy — shared tree by default, worktree when
	// the deliverable is a branch or the planner says so). The path comes
	// from the session's WorktreePath seam, not from the planner.
	Worktree bool   `json:"worktree,omitempty"`
	Verify   string `json:"verify,omitempty"` // verify rung for this node's output, empty is none
}

// NodeTitle is the name one node is drawn under, and it is the ONLY place in
// this package that answers the question — so every surface asking it gets the
// same answer, and none of them has to invent one.
//
// TWO ANSWERS, AND NEITHER OF THEM READS THE GOAL. The planner's own title is
// taken as written, because a model that was asked to name the slice has done
// exactly the work a namer would be paid to do again. When there is none — an
// older planner, a reply that lost the key, a node this package minted itself
// ([Orchestrator.synthesize]) — the name is built from the ID, which the law
// already requires to be a slug: `token-bucket` is `token bucket`, and that is
// a derivation from a structured field rather than a sentence cut short.
//
// A GOAL IS NEVER A NAME. That is the whole point of the field, and a fallback
// that reached for the goal would put the defect back the moment a planner
// forgot the key.
func NodeTitle(n Node) string {
	if title := clipWords(n.Title, NameWords); title != "" {
		return title
	}
	return clipWords(slugWords(n.ID), NameWords)
}

// slugWords spells a slug out as words: hyphens, underscores and dots are the
// separators a slug is written with, so they are the spaces it is read with.
func slugWords(slug string) string {
	spelt := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' || r == '/' {
			return ' '
		}
		return r
	}, slug)
	return strings.Join(strings.Fields(spelt), " ")
}

// clipWords holds a name to its cap. It is a cap and not a target: a name that
// is already short comes back untouched, and there is no ellipsis — a title cut
// here is cut on a word boundary and reads as a shorter name rather than as a
// truncation.
func clipWords(text string, words int) string {
	fields := strings.FieldsFunc(strings.TrimSpace(text), unicode.IsSpace)
	if len(fields) > words {
		fields = fields[:words]
	}
	return strings.Join(fields, " ")
}

// Cancel drops a pending node. Running nodes are never touched (the
// commitment law); completed nodes are facts.
type Cancel struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// DonePlan ends the run: the planner has judged the goal answered and names
// the synthesis brief. Synthesis grounds every claim in node ids.
type DonePlan struct {
	Brief string `json:"brief"`
}

// Amendment is the planner's whole vocabulary: add nodes, cancel pending
// ones, say what it is thinking, or finish. An empty Amendment is the cheap
// NOOP — the expected answer to most completions.
type Amendment struct {
	Add    []Node    `json:"add,omitempty"`
	Cancel []Cancel  `json:"cancel,omitempty"`
	Note   string    `json:"note,omitempty"` // one line, shown between completions
	Done   *DonePlan `json:"done,omitempty"`
}

// State is where one node is.
type State int

const (
	Queued State = iota // in the frontier, needs unmet
	Ready               // needs met, waiting for a slot
	Running
	Done
	Failed
	// Cancelled is a node a PERSON stopped: one that was in flight when the run
	// was cancelled and had its context cut under it, or one still pending that
	// will now never launch. It is deliberately not [Failed] — nothing about the
	// work went wrong and nobody made a finding about it — and a cancelled node
	// keeps no digest, because a half-answer handed on as a fact is worse than
	// no answer at all (run.go's [Orchestrator.Cancel]).
	Cancelled
)

// NodeStatus is a Node plus what the run knows about it so far. Digest is
// the condensed output — what dependents and the planner see, never the raw
// artifact.
type NodeStatus struct {
	Node
	State  State   `json:"state"`
	Digest string  `json:"digest,omitempty"`
	Err    string  `json:"err,omitempty"`
	Cost   float64 `json:"cost"` // dollars this node has burned
}

// Fuel is the run's one tank. Cap is dollars the person approved; Spent rolls
// up every model call anywhere in the run, planner calls included.
type Fuel struct {
	Cap   float64 `json:"cap"`
	Spent float64 `json:"spent"`
}

// Snapshot is the run rendered for a surface: the crystallized graph so far,
// the fuel gauge, the planner's notes, and whether the run is paused at the
// gate. The room draws this; the roster tree reads the parent/child shape of
// the run as a whole from the session.
type Snapshot struct {
	Goal   string       `json:"goal"`
	Nodes  []NodeStatus `json:"nodes"`
	Fuel   Fuel         `json:"fuel"`
	Notes  []string     `json:"notes,omitempty"` // planner notes, in order
	Steer  []string     `json:"steer,omitempty"` // user steering, in order
	Paused bool         `json:"paused"`          // out of fuel, awaiting the gate's answer
	Done   bool         `json:"done"`
	// Stopped says a PERSON ended this run early ([Orchestrator.Cancel]) rather
	// than the planner finishing it. Done is true beside it — the run is over
	// either way, and a surface waiting for one flag must not wait forever for
	// the other — and Answer is empty, because a run somebody stopped does not
	// go on to pay for a synthesis.
	Stopped bool   `json:"stopped,omitempty"`
	Answer  string `json:"answer,omitempty"` // the synthesis, once Done
	// Planner is the model the planner thinks with, carried from
	// [Options.Planner] and never read by this package. It is on the snapshot
	// because it is the one fact a surface cannot derive from the run: the
	// planner is an interface here, it is usually NOT the model the person is
	// talking to (the session resolves it through internal/roles), and it cuts
	// every node the tank pays for — so a gauge drawn without it says how much
	// is being spent and not whose judgement is spending it.
	Planner string `json:"planner,omitempty"`
}

// View is what the planner sees on each call: the goal, condensed results,
// the frontier as it stands, the fuel gauge, and any steering the person
// typed since the last call. Digests only — the planner is the one
// big-context call in the system, and it stays small on purpose.
type View struct {
	Goal     string
	Results  []NodeStatus // completed nodes, digests only
	Frontier []NodeStatus // queued/ready/running
	Fuel     Fuel
	Steer    []string
}

// Planner thinks. It is called once at start and once per node completion,
// and it answers with an Amendment. NOOP is a first-class answer.
type Planner interface {
	Plan(ctx context.Context, v View) (Amendment, error)
}

// Executor runs one node and returns its digest. The session supplies this:
// it is the agent loop with the node's whitelist and worktree resolved.
type Executor interface {
	Exec(ctx context.Context, n Node, deps []NodeStatus) (digest string, cost float64, err error)
}

// Orchestrator is the run: construct it with [New], call Run, and read
// [Orchestrator.Snapshot] as it goes. The scheduler is in run.go, the tank in
// fuel.go, and what the planner is allowed to say in amend.go.
