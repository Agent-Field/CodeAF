package placeorg

import "strconv"

// The self-check. These are the laws a Plan must satisfy before it may leave
// this package. They are code, not prose: each is a function over the Tree
// and the Plan, and the Runner runs all of them, journals each result, and
// refuses a Plan on any failure. The caps are named here once; the policy
// page interpolates them.

const (
	// MaxDepth is the deepest an emergent split may go.
	MaxDepth = 6
	// MaxFanout is the most children a node may have before an interpose is
	// considered.
	MaxFanout = 9
	// Quorum is the fewest chats that make a place, after duplicates fold.
	Quorum = 4
	// MaxLinks is the most shortcuts a chat may have.
	MaxLinks = 2
	// MaxNameWords is the longest a name may be.
	MaxNameWords = 2
)

// Invariant is one law.
type Invariant struct {
	// Name is the short spelling the journal and the progress view use.
	Name string
	// Check returns nil when the law holds, or an error naming the first
	// node or chat that breaks it.
	Check func(t *Tree, p *Plan, c *Corpus) error
}

// InvariantResult is one line of the self-check a person can read.
type InvariantResult struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Why  string `json:"why,omitempty"`
}

// Invariants is the full list, in the order they are checked. The bodies
// are built by the audit lane; the names are the contract.
var Invariants = []Invariant{
	{Name: "depth within " + strconv.Itoa(MaxDepth)},
	{Name: "fan-out within " + strconv.Itoa(MaxFanout)},
	{Name: "every place has at least " + strconv.Itoa(Quorum) + " chats"},
	{Name: "names unique tree-wide, one or two words"},
	{Name: "at most " + strconv.Itoa(MaxLinks) + " shortcuts per chat"},
	{Name: "forest: one parent per place"},
	{Name: "every op in the plan is in the journal and replays to this tree"},
	{Name: "a home for every chat, duplicates beside their representative"},
	{Name: "no probe is a place or counts toward one"},
	{Name: "the person's own places are intact in add-on mode"},
	{Name: "no model text became an id, a path or a permission"},
}

// Check runs every invariant and returns the results; ok is true only when
// all hold. Not built yet.
func Check(t *Tree, p *Plan, c *Corpus) (results []InvariantResult, ok bool) {
	return nil, false
}
