// Package factory is the vocabulary of the factory floor: what the surface
// draws and what a person may do to it, with NO engine behind it. The engine
// arrives through [Seam]; a mock seam (internal/factory/mock) stands in until
// the plan store, the forge and the crew are wired.
//
// THE TWO LAYERS. A [Recipe] is a fixed, ordered list of stages written once
// per repo or team. A run is that recipe compiled into tasks; the inside of a
// stage is dynamic (split, parallel, fix tasks, a parked question) and nothing
// inside a stage can add a stage. Loops are a condition on a stage ([Stage.Until]
// with [Stage.Max]), never an edge. There is no graph for a person to draw.
//
// NO MODEL ON A STAGE. A stage runs as an ordinary task with the ordinary crew:
// worker, planner and checker picked for the class of work (internal/crewroute).
// The one word a person may add is effort ([Stage.Effort]); pins and `redo
// stronger` remain the room's own doors, during or after a run.
//
// ONE POOL. Money is the item's cap and the day's rail, never a knob per stage.
package factory

import "time"

// Kind is what a floor item is. It is a string and open: an issue, a pull
// request, a red CI run, a chore, a standing order's firing, work a chat split
// off. A new kind needs no edit here.
type Kind string

const (
	KindIssue Kind = "issue"
	KindPR    Kind = "pr"
	KindCI    Kind = "ci"
	KindChore Kind = "chore"
)

// Tier is how far the item's author is trusted. A stranger's words may be
// drafted on, never shipped on, without a person.
type Tier string

const (
	TierOwner    Tier = "owner"
	TierCollab   Tier = "collaborator"
	TierStranger Tier = "stranger"
)

// Origin is where the item came from.
type Origin string

const (
	OriginForge    Origin = "github"
	OriginTerminal Origin = "terminal"
	OriginChat     Origin = "chat"
)

// State is the item's place on the floor. The floor groups by it.
type State string

const (
	StateNew       State = "new"
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateNeedsYou  State = "needs you"
	StateLanded    State = "landed"
	StateShipped   State = "shipped"
	StateDismissed State = "dismissed"
)

// Gate is where a person sits in a run.
type Gate string

const (
	GatePlan Gate = "plan" // comes back with the plan before any code
	GateShip Gate = "ship" // runs to a result; the person signs off
	GateNone Gate = "none" // a banked habit; green proof ships itself
)

// Stage is one sentence the factory runs at its place in the recipe. The six
// fields after Ask are the whole of what is structured about it.
type Stage struct {
	Name   string   // plan · write · test · review · neaten · security · proof
	Ask    string   // the sentence as typed; the brief is compiled from it
	Effort string   // "" (the knee) · cheap · strong — the crew's one word
	Fanout string   // one · per-file · per-finding · per-claim
	Until  string   // done · clean · green · proven
	Max    int      // rounds before it stops and asks; 0 means one
	Gate   Gate     // none · plan (ask before going on) · ship (sign-off)
	Proof  []string // what this stage must show
	On     bool     // an item may switch a banked stage off
}

// Recipe is what a repo or team banked: stages in order and the policy every
// proof must show.
type Recipe struct {
	Stages []Stage
	Policy []string
}

// Triage is the cheap read made on arrival.
type Triage struct {
	Type      string // bug · feat · chore · question · review
	Size      string // S · M · L
	Area      string
	Readiness int // 0..100; under 55 the item is thin and wants questions
	Est       float64
	DupOf     int
	Risk      string // low · mid · high
	Read      string // one sentence
	Questions []string
}

// Repo is a connected repository and the team that owns it.
type Repo struct {
	Name   string
	Team   string
	Areas  []string
	Habits []string // banked sentences
	Recipe Recipe
	CIRed  bool
	Hue    int
}

// PhaseState is a stage's state inside a running stream.
type PhaseState string

const (
	PhasePending PhaseState = "pending"
	PhaseRunning PhaseState = "running"
	PhaseDone    PhaseState = "done"
	PhaseFailed  PhaseState = "failed"
	PhaseWaiting PhaseState = "waiting"
)

// Phase is one stage as it runs: its state, what round it is on, how many
// tasks it fanned out into.
type Phase struct {
	Name  string
	State PhaseState
	Note  string
	Round int
	Tasks int
	Left  time.Duration
}

// LogLine is one line of a stream's grain: a thought, a shell call, a test,
// a write, something said, a question, a failure, a success.
type LogLine struct {
	At    time.Time
	Glyph string
	Tone  string // thought · shell · test · write · said · ask · fail · ok
	Text  string
}

// Stream is an item on a bench.
type Stream struct {
	Phases   []Phase
	Cur      int
	Spent    float64
	Started  time.Time
	Ended    time.Time
	Activity []int // 0..7, oldest first
	Log      []LogLine
	Paused   bool
	Findings int
	Bench    int
	Room     string // the conversation id the stream lives in, when known
}

// Claim is one row of a proof sheet: what was claimed, whether it was shown,
// and the evidence in the claim's own medium.
type Claim struct {
	Text     string
	OK       bool
	Evidence string
	Medium   string // test · screenshot · transcript · benchmark · policy
}

// Item is one row on the floor.
type Item struct {
	ID       int
	Repo     string
	Num      int
	Kind     Kind
	Title    string
	Body     string
	Author   string
	Tier     Tier
	Origin   Origin
	Synced   bool
	Created  time.Time
	Changed  time.Time
	State    State
	Triage   Triage
	Stages   []Stage // the item's copy of the recipe, toggled
	Cap      float64
	Gate     Gate
	Stream   *Stream
	Question string
	QKind    string // plan · cap · scope
	Proof    []Claim
	Policy   []Claim
	Marked   bool
	Labels   []string
	Checks   string
	Diff     string
}

// Ref is the item's short name: #123, or ci.
func (it Item) Ref() string {
	if it.Kind == KindCI {
		return "ci"
	}
	return "#" + itoa(it.Num)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Shift is the handover: what happened since the person last looked.
type Shift struct {
	Since    time.Time
	Shipped  int
	Arrived  int
	Asked    int
	Handled  int
	Spent    float64
	Hours    [24]int
	Shipping []string
}

// Snapshot is everything a frame needs, copied off the loop. A frame reads
// memory only (framedisk law), so the seam hands over values, never a store.
type Snapshot struct {
	Now     time.Time
	Repos   []Repo
	Items   []Item
	Benches int
	Daily   float64
	Rail    float64
	Shift   Shift
	Speed   time.Duration // the mock's clock; zero for a real engine
	Sources []SourceInfo  // what is connected; the chat is always one
}

// RepoNamed finds a repo in the snapshot.
func (s Snapshot) RepoNamed(name string) (Repo, bool) {
	for _, r := range s.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

// Count is how many items stand in a state.
func (s Snapshot) Count(st State) int {
	n := 0
	for _, it := range s.Items {
		if it.State == st {
			n++
		}
	}
	return n
}
