package session

// WHERE A TASK CAME FROM, AS A FACT THE ENGINE CAPTURED AND NOT A THING ANYONE
// INFERS.
//
// A person who has eighty tasks on screen is asking "which message made these?".
// That answer exists exactly once, at the instant the work is admitted: the
// message the person typed (or the `/task` they typed) is known then and is
// never known as cheaply again. So the capture happens at the one door every
// graph node comes through ([TaskGraph.admit]) and at the one seam every run
// row comes through ([TaskGraph.keepRunRowsBorn]), is written onto the record,
// and is read back from the record — never from a title, a request's prose, a
// timestamp or the order tasks were made in.
//
// THE LAWS.
//
//   - ONE IDENTITY PER MESSAGE. Two tasks made from the same typed message carry
//     the same [TaskSource.ID]; the same sentence typed twice is two messages
//     (the id is the event, as [personSourceID] is).
//   - A CHILD INHERITS ITS PARENT'S SOURCE and never the conversation's current
//     one. A node's sub-task, a division's part and a plan row dispatched into
//     the graph all belong to the block of the work that made them, however
//     many messages the person has typed since.
//   - UNKNOWN IS ABSENT. A turn nobody typed into (a woken turn, a landing) has
//     no source; a record written before this existed has none. Neither is ever
//     reconstructed from the clock or the context at read time.
//   - EVERY WORD IS BOUNDED. The text is the first [MaxTaskSourceBytes] bytes of
//     the message, cut on a rune boundary and marked, so a task carries its
//     block's name and not the conversation.

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The kinds of source.
const (
	// TaskSourceMessage is a task made from a message the person typed into the
	// conversation (a proposal the model made while answering it).
	TaskSourceMessage = "message"
	// TaskSourceDirective is a task the person made themselves with `/task`: the
	// command's own words are the source.
	TaskSourceDirective = "directive"
)

// MaxTaskSourceBytes bounds the words a task keeps of its source.
const MaxTaskSourceBytes = 4096

// TaskSource is the message or directive that started a task, as it was when
// the task was admitted. It is persisted as written here (the JSON tags are the
// checkpoint's spelling) and projected to both app wires.
type TaskSource struct {
	// ID is shared by every task made from the same message or directive.
	ID string `json:"id"`
	// Kind is [TaskSourceMessage] or [TaskSourceDirective].
	Kind string `json:"kind"`
	// Text is the person's words, bounded; Cut says it was shortened.
	Text string `json:"text,omitempty"`
	Cut  bool   `json:"cut,omitempty"`
	// At is when the session heard it; the zero time is unknown.
	At time.Time `json:"at,omitzero"`
}

// valid is the shape a source must have to be kept: a known kind and an id. A
// record whose source is not valid is read as no source at all.
func (s *TaskSource) valid() bool {
	return s != nil && s.ID != "" && (s.Kind == TaskSourceMessage || s.Kind == TaskSourceDirective)
}

// cloneSource copies a source so no two records share one.
func cloneSource(s *TaskSource) *TaskSource {
	if !s.valid() {
		return nil
	}
	c := *s
	return &c
}

// boundSourceText cuts text to [MaxTaskSourceBytes] on a rune boundary.
func boundSourceText(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if len(text) <= MaxTaskSourceBytes {
		return text, false
	}
	cut := MaxTaskSourceBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}

// messageSource is the person's message the session is answering now, or nil
// when this turn was not opened or steered by a person ([Agent.askingNow] is the
// same read the forwarding door makes, so the identity, the words and the time
// cannot be torn apart).
func (a *Agent) messageSource() *TaskSource {
	asking := a.askingNow()
	if !asking.id.live() || strings.TrimSpace(asking.words) == "" {
		return nil
	}
	text, cut := boundSourceText(asking.words)
	return &TaskSource{
		ID:   asking.id.scope + "#" + strconv.FormatUint(asking.id.seq, 10),
		Kind: TaskSourceMessage, Text: text, Cut: cut, At: asking.spoken,
	}
}

// directiveSource names a `/task` the person typed. The scope is the life's own
// ([Agent.mintPersonScopeLocked]) so a directive of another life can never be
// mistaken for this one's; the counter is per life and only ever grows.
func (a *Agent) directiveSource(words string) *TaskSource {
	text, cut := boundSourceText(words)
	if text == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.personScope == "" && !a.mintPersonScopeLocked() {
		return nil
	}
	a.directiveSeq++
	return &TaskSource{
		ID:   a.personScope + "#d" + strconv.FormatUint(a.directiveSeq, 10),
		Kind: TaskSourceDirective, Text: text, Cut: cut, At: time.Now(),
	}
}

// bornSource is the source of work this agent is making NOW: a node's work
// belongs to the block its own node belongs to, and a conversation's belongs to
// the message the person is answering. Nil is unknown, never a guess.
func (a *Agent) bornSource() *TaskSource {
	if a == nil {
		return nil
	}
	if a.config.InTask {
		if node := a.graph().node(a.config.taskID); node != nil {
			return node.source()
		}
		return nil
	}
	return a.messageSource()
}

// source is the node's block, copied.
func (n *TaskNode) source() *TaskSource {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return cloneSource(n.spec.source)
}

// sourceOfID is the source of a graph node or run row by number, for a reader
// that has an id and no node (a plan row's root). Nil when it has none.
func (g *TaskGraph) sourceOfID(id uint64) *TaskSource {
	if g == nil || id == 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if node := g.nodes[id]; node != nil {
		return cloneSource(node.spec.source)
	}
	for _, rows := range g.runs {
		for _, row := range rows {
			if row.ID == id {
				return cloneSource(row.Source)
			}
		}
	}
	return nil
}

// ── the door a source travels through ───────────────────────────────────────

type taskSourceKey struct{}

// withTaskSource carries a captured source to the road that admits the work.
func withTaskSource(ctx context.Context, source *TaskSource) context.Context {
	return context.WithValue(ctx, taskSourceKey{}, cloneSource(source))
}

func taskSourceCaptured(ctx context.Context) bool {
	_, ok := ctx.Value(taskSourceKey{}).(*TaskSource)
	return ok
}

// taskSourceFrom is the source a door captured, or nil.
func taskSourceFrom(ctx context.Context) *TaskSource {
	source, _ := ctx.Value(taskSourceKey{}).(*TaskSource)
	return cloneSource(source)
}

// expectRunSource captures the source a run row is born with, including a
// truthful unknown; delayed publication must not adopt a later person message.
func (a *Agent) expectRunSource(id uint64, source *TaskSource) {
	if id != 0 {
		a.runSources.Store(id, cloneSource(source))
	}
}

// runBorn is the source of a run row that has never been kept: the one captured
// for its number, else the one this agent is making work under right now.
func (a *Agent) runBorn(id uint64) *TaskSource {
	if v, ok := a.runSources.LoadAndDelete(id); ok {
		return cloneSource(v.(*TaskSource))
	}
	return a.bornSource()
}

// sourcedRunRow gives a row its source. A row that names one keeps it; a row
// already kept carries the kept one (INCLUDING NONE — a legacy row is never
// stamped with the context of the day it is touched); a row never kept is born
// with [Agent.runBorn]'s.
func (a *Agent) sourcedRunRow(g *TaskGraph, row TaskNotice) TaskNotice {
	if row.Source.valid() {
		row.Source = cloneSource(row.Source)
		return row
	}
	for _, kept := range g.runRows(row.ID) {
		if kept.ID == row.ID {
			row.Source = cloneSource(kept.Source)
			return row
		}
	}
	row.Source = a.runBorn(row.ID)
	return row
}

// planRowSource is the source of one row of a run's plan store, by exact id.
//
// A row whose own id is a number IS a node or a run row of the conversation (the
// run road names each store task with its task's number), and that record's
// answer is final — present or absent — because climbing past it would hand a
// joined task the block of the run it joined. A row with a non-numeric id (a part
// a worker filed) belongs to the block of its nearest ancestor that has one. An
// ancestor chain that never reaches a number has no known source.
func planRowSource(id string, parentOf func(string) string, linked func(token string) (*TaskSource, bool)) *TaskSource {
	for depth := 0; id != "" && depth < 64; depth++ {
		if source, ok := linked(planStoreID(planTaskID(id))); ok {
			return source
		}
		id = parentOf(id)
	}
	return nil
}

// planLinks is every plan token a record of THIS conversation says it owns, with
// that record's source (nil for a legacy record, which is a final answer). A node
// owns the plan row its PlanID names and a run row owns the one its PlanTask
// names; both were written by the code that made the pair, so the join is the
// record's own and not a coincidence of numbers. A part a worker filed under a
// number that happens to equal a node's has no such link and is never joined to it.
func (g *TaskGraph) planLinks() map[string]*TaskSource {
	links := map[string]*TaskSource{}
	if g == nil {
		return links
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, node := range g.nodes {
		if node != nil && node.spec.planID != "" {
			links[planStoreID(planTaskID(node.spec.planID))] = cloneSource(node.spec.source)
		}
	}
	for _, row := range g.runRowsLocked() {
		if row.PlanTask != "" {
			links[planStoreID(planTaskID(row.PlanTask))] = cloneSource(row.Source)
		}
	}
	return links
}

// inheritedSource is the source a spec admitted without one is born with.
func (g *TaskGraph) inheritedSource(spec taskSpec, admitter *Agent) *TaskSource {
	if spec.parent != 0 {
		return g.sourceOfID(spec.parent)
	}
	return admitter.bornSource()
}
