package chat

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The settled deliverable card: what a finished job looks like in the room that
// commissioned it.
//
// THREAD-UX's presentation law splits the transcript in two — stream =
// conversation, card = work — and 4.3 says where the second half sits: "settled
// deliverable cards stay inline at birth position". What landed there instead
// was the job's whole account of itself, pasted into the conversation under its
// own node id as a heading:
//
//	task-16
//	  rivers.txt is written with three original haiku, each in the 5-7-5 …
//	  <every line of it>
//	  Files:
//	  /…/workspace/task-16/rivers.txt
//	  $—
//
// Three laws at once. 5.14's never-shown tier names node ids explicitly, and
// that heading is one. 5.9's progressive disclosure says a card is collapsed
// until it is asked to open, and that row had no fold at all. And the artifact
// law's rendering half (12.5) says a deliverable is REFERENCED by its path — the
// path was there, but as the eleventh line of a prose dump rather than as the
// reference row a reader can find.
//
// What this file adds is the reading that makes the card possible: a delivery
// message carries a node id and nothing else a renderer can dress, so the name,
// the money and the lifecycle are asked of the same board the rail draws from.
// The card and the rail card for the same job therefore agree by construction —
// they are the same numbers, read once per journal move, in [scopeSource].
//
// Nothing here rewrites the record. The collapsed form SHOWS less; the journal
// row is whole underneath it, one keystroke away (`ctrl+r`), and the full detail
// also stays where the doc puts it — in the task's own room, which renders the
// same node-anchored trail.

// deliveryCap is how many artifact rows one card draws before the rest stay
// folded with the prose. A job that wrote twenty files has a room to show them
// in; a card that listed twenty is no longer a card.
const deliveryCap = 3

// jobSource is what a delivery card asks the board for. It is an interface and
// not *scopeSource so the block builder stays testable without a store, and so
// a surface that has no board — the same surface that gets an honest empty rail
// — draws an honest card with no name and no money rather than an invented one.
type jobSource interface {
	jobFacts(nodeID string) (jobFacts, bool)
}

// jobFacts is the board's answer about one job: the three things 5.9 puts on a
// card that a message cannot carry.
type jobFacts struct {
	// Name is the human word for the job — never its id (5.14).
	Name string
	// Life is the lifecycle the board recorded. It decides the glyph and the
	// hue, so a failure can never be drawn green.
	Life rail.Lifecycle
	// Cost is what the job spent; HasCost says whether the journal had a run to
	// show for it. Missing money renders as the missing glyph, never as zero
	// (8.2.20).
	Cost    float64
	HasCost bool
}

// jobFacts implements [jobSource] over the board the rail already built.
//
// It reads the scope cache and never the store: the walk happened once, at the
// last journal move, and a second one here would put a snapshot query behind
// every message that arrives. A job with no scope of its own — a worker deep in
// a subtree, a node that has aged off the board — answers with whatever label
// the last walk knew, and no money, which is the honest half-answer.
func (s *scopeSource) jobFacts(nodeID string) (jobFacts, bool) {
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || !s.ready || nodeID == "" {
		return jobFacts{}, false
	}
	if scope, ok := s.tasks[rowTaskPrefix+nodeID]; ok && len(scope.Rows) > 0 {
		card := scope.Rows[0]
		return jobFacts{
			Name:    card.Name,
			Life:    card.Life,
			Cost:    card.Meta.Cost,
			HasCost: card.Meta.HasCost,
		}, card.Name != ""
	}
	name := strings.TrimSpace(s.label[nodeID])
	if name == "" {
		return jobFacts{}, false
	}
	return jobFacts{Name: name}, true
}

// isDelivery says whether one journal row is a job reporting its own ending.
//
// It reads COLUMNS and never prose (13.3.1). The signature is the reconciler's
// own: a system row anchored to a node and belonging to no command is the
// deliverable or the failure that node's lifecycle produced. Everything else
// anchored to a node — a narrator's progress line, a charter notice (both agent
// rows), an applied command's receipt (a command row) — keeps the dressing it
// already had.
func isDelivery(message store.Message) bool {
	return message.Role == store.RoleSystem &&
		strings.TrimSpace(message.NodeID) != "" &&
		message.CommandSeq == 0 &&
		strings.TrimSpace(message.Body) != ""
}

// deliveryFiles names the artifacts a delivery points at, in the order a reader
// should meet them.
//
// The typed part is the truth when it is there: [store.PartArtifact] is the
// artifact law's carrier and a producer that attaches one has said exactly which
// files it means. Until every producer does, the fallback is the SAME reading
// the head already performs on the same field — internal/head/depth.go's
// collectResultFiles, whose own comment is the specification: a job's artifacts
// live "only where the worker wrote them down, which is its own summary — inline
// or on a line of its own".
//
// This is not the banned prose scan. 13.3.1 forbids re-deriving structure that
// the producer already has a typed column for; a delivery message has no
// artifact column filled, and the choice is between naming the file and hiding
// it eleven lines into a fold. cas:// pointers are dropped: they are content
// addresses, and a reader cannot open one.
func deliveryFiles(message store.Message) []string {
	files := make([]string, 0, deliveryCap)
	seen := make(map[string]bool, deliveryCap)
	add := func(path string) {
		path = strings.Trim(strings.TrimSpace(path), `"'(),;:.`)
		if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~/") {
			return
		}
		if len(path) < 2 || seen[path] || len(files) == deliveryCap {
			return
		}
		seen[path] = true
		files = append(files, path)
	}
	for _, part := range message.Parts {
		if part.Kind == store.PartArtifact && part.Artifact != nil {
			add(part.Artifact.Path)
		}
	}
	if len(files) > 0 {
		return files
	}
	for _, field := range strings.Fields(message.Body) {
		add(field)
	}
	return files
}
