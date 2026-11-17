package factory

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// ── ASKS, VIEWS AND DECISIONS (Workstreams final spec, "Working with other people") ──
//
// People coordinate through three things only: a request, an Ask, and a
// decision. Agents do the work and carry the context; nobody has an agent
// that speaks for them. The rules this file keeps, in both stores (the desk's
// crates/workstreams keeps the same ones for a Personal workstream):
//
//   - EVERY ASK HAS EXACTLY ONE DECIDER. A task's own Ask (Task.Ask) is its
//     owner's; each of Task.Asks names its own. Approval by several people is
//     several Asks on one task, and the task waits on all of them.
//   - OTHERS ATTACH ONE VIEW EACH. A view is a short line for the decider to
//     read before answering. It is not a vote, and the decider never adds one
//     to their own Ask.
//   - THE DECISIONS LIST RECORDS ITSELF, from three places: every answered
//     Ask, every request that changed shared work, and every rule set in
//     People and rules. Nobody writes it by hand.
//   - ONLY THE PERSON WHO MADE A DECISION WITHDRAWS OR REPLACES IT. The old
//     one stays in the list, greyed, saying who replaced it and when.
//   - A CONFLICT ASKS WHOEVER MADE THE DECISION. When the decision check
//     finds work going against a decision, the work pauses on an Ask to that
//     person ([ConflictAsk]); it never quietly goes ahead, and it never asks
//     the wrong person.
//   - ASK BEFORE, OR DO AND TELL AFTER. Cheap, reversible work that goes
//     against no decision is done and listed under Assumed; Send back
//     overturns an assumption.

// The states a decision is in. WITHDRAWN AND SUPERSEDED STAY IN THE LIST,
// greyed: what a team once decided, and who changed it, remains readable.
const (
	DecisionKept       = "kept"
	DecisionProposed   = "proposed"
	DecisionSuperseded = "superseded"
	DecisionWithdrawn  = "withdrawn"
)

// Where a decision came from.
const (
	DecisionFromAsk     = "ask"
	DecisionFromRequest = "request"
	DecisionFromRule    = "rule"
)

// DecisionFrom is where a decision came from, for its row: "answered Afi's
// Ask on Draft the March release notes", "from her request on Refresh the
// store screenshots", "People and rules".
type DecisionFrom struct {
	Kind      string `json:"kind" jsonschema:"enum=ask,enum=request,enum=rule"`
	Task      string `json:"task,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
	Ask       string `json:"ask,omitempty"`
	// AskedBy is who raised the Ask, in the words a row says: "Afi", "the
	// chat Week view", "the factory", or a person's name.
	AskedBy string `json:"asked_by,omitempty"`
	// Views are the names of the people whose views were attached when it
	// was answered.
	Views []string `json:"views,omitempty"`
	// Replaces is the decision this one replaced.
	Replaces string `json:"replaces,omitempty"`
}

// The most a list of views, or of assumptions, holds, and the longest line.
const (
	taskViewMost    = 50
	taskAssumedMost = 30
	taskLineMost    = 500
	// DecisionTextMost is the longest decision line.
	DecisionTextMost = 500
)

// The refusals a person reads.
var (
	ErrDecisionNotMaker   error = TaskRefused("only the person who made a decision can withdraw or replace it")
	ErrDecisionNotCurrent       = TaskInvalid("that decision was already withdrawn or replaced")
	ErrTaskOwnAsk         error = TaskRefused("you decide this Ask, so there is no view to add: answer it")
)

// OpenAsks are every Ask t waits on: its own first, then the others.
func OpenAsks(t WorkstreamTask) []TaskAsk {
	out := make([]TaskAsk, 0, 1+len(t.Asks))
	if t.Ask != nil {
		out = append(out, *t.Ask)
	}
	return append(out, t.Asks...)
}

// AskDecider is who answers ask on t: its decider, else the task's owner
// (zero for owner Everyone).
func AskDecider(t WorkstreamTask, ask TaskAsk) PersonRef {
	if ask.Decider != nil {
		return *ask.Decider
	}
	if t.Owner.Kind == TaskOwnerPerson {
		return PersonRef{ID: t.Owner.ID, Name: t.Owner.Name}
	}
	return PersonRef{}
}

// findAsk is the Ask id names on t (empty: the task's own), and whether it is
// one of Asks (with its index there).
func findAsk(t WorkstreamTask, id string) (TaskAsk, int, bool) {
	if t.Ask != nil && (id == "" || t.Ask.ID == id) {
		return *t.Ask, -1, true
	}
	for i, a := range t.Asks {
		if a.ID == id {
			return a, i, true
		}
	}
	return TaskAsk{}, 0, false
}

// FindAsk is the open Ask id names on t (empty: the task's own), its index
// among Asks (-1 for the task's own), and whether it is open.
func FindAsk(t WorkstreamTask, id string) (TaskAsk, int, bool) { return findAsk(t, id) }

// IsOwnAsk is whether id names t's own Ask (the owner's).
func IsOwnAsk(t WorkstreamTask, id string) (TaskAsk, bool) {
	a, i, ok := findAsk(t, id)
	return a, ok && i < 0
}

// MayAnswerAsk is whether a may answer the Ask id names on t: the task's own
// Ask by MayAnswer's rule; any other by its decider alone, or the fallback
// person once it is a day old.
func MayAnswerAsk(t WorkstreamTask, id string, a TaskActor, fallback string, now time.Time) bool {
	ask, i, ok := findAsk(t, id)
	if !ok || !a.Edit || t.State != TaskNeeds {
		return false
	}
	if i < 0 {
		return MayAnswer(t, a, fallback, now)
	}
	if ask.Decider != nil && a.Handle != "" && NormalizeHandle(ask.Decider.ID) == a.Handle {
		return true
	}
	at, err := time.Parse(time.RFC3339Nano, ask.AskedAt)
	return fallback != "" && NormalizeHandle(fallback) == a.Handle && err == nil && now.Sub(at) >= TaskFallbackAfter
}

// AddView puts who's view on the Ask id names, replacing their earlier one;
// empty words take it away. The decider has no view on their own Ask.
func AddView(t WorkstreamTask, id, words string, a TaskActor, who PersonRef, now time.Time) (WorkstreamTask, error) {
	if !a.Edit {
		return t, TaskRefused("a view needs edit on the workstream")
	}
	ask, i, ok := findAsk(t, id)
	if !ok || t.State != TaskNeeds {
		return t, ErrTaskNoAsk
	}
	if d := AskDecider(t, ask); (d.ID != "" && NormalizeHandle(d.ID) == a.Handle) || (d.ID == "" && t.Owner.Kind == TaskOwnerEveryone) {
		return t, ErrTaskOwnAsk
	}
	words = strings.Join(strings.Fields(words), " ")
	if len(words) > taskLineMost {
		return t, taskInvalidf("a view is one short line, at most %d characters", taskLineMost)
	}
	views := slices.DeleteFunc(slices.Clone(ask.Views), func(v TaskView) bool { return NormalizeHandle(v.By.ID) == a.Handle })
	if words != "" {
		if len(views) >= taskViewMost {
			return t, taskInvalidf("an Ask holds at most %d views", taskViewMost)
		}
		views = append(views, TaskView{By: PersonRef{ID: a.Handle, Name: who.Name}, Text: words, At: now.UTC().Format(time.RFC3339Nano)})
	}
	ask.Views = views
	if len(ask.Views) == 0 {
		ask.Views = nil
	}
	if i < 0 {
		t.Ask = &ask
	} else {
		t.Asks = slices.Clone(t.Asks)
		t.Asks[i] = ask
	}
	t.Rev, t.UpdatedAt = t.Rev+1, now.UTC().Format(time.RFC3339Nano)
	return t, nil
}

// askerWords is who raised ask, as a decision's row says it.
func askerWords(t WorkstreamTask, ask TaskAsk) string {
	by := ask.By
	if by == nil {
		switch t.DoneBy.Kind {
		case TaskByAfi:
			by = &TaskEventBy{Kind: TaskByAfi, Name: "Afi"}
		case TaskByChat:
			by = &TaskEventBy{Kind: TaskByChat, Name: t.DoneBy.Name}
		case TaskByFactory:
			by = &TaskEventBy{Kind: TaskByFactory}
		default:
			return ""
		}
	}
	switch by.Kind {
	case TaskByAfi:
		return "Afi"
	case TaskByChat:
		if by.Name == "" {
			return "a chat"
		}
		return "the chat " + by.Name
	case TaskByFactory:
		return "the factory"
	}
	return by.Name
}

// answerDecision is the decision answering ask with option (or words)
// records, or false when it records none: the question a changed file puts
// to a running task is about the run, not the work, and keeping a decision
// that a conflict already holds ("Keep the decision") would only repeat it.
func answerDecision(t WorkstreamTask, ask TaskAsk, opt *TaskAskOption, words string, who PersonRef, id string, now time.Time) (WorkstreamDecision, bool) {
	if strings.HasPrefix(ask.ID, FileChangedAskID) {
		return WorkstreamDecision{}, false
	}
	text := ""
	if opt != nil {
		text = strings.TrimSpace(opt.Decides)
	}
	if text == "" {
		if ask.Against != nil {
			return WorkstreamDecision{}, false
		}
		chose := ""
		if opt != nil {
			chose = opt.Label
		}
		text = strings.TrimSpace(ask.Title + " " + strings.Join(nonEmpty(chose, words), " · "))
	}
	from := &DecisionFrom{Kind: DecisionFromAsk, Task: t.ID, TaskTitle: t.Title, Ask: ask.ID, AskedBy: askerWords(t, ask)}
	for _, v := range ask.Views {
		from.Views = append(from.Views, v.By.Name)
	}
	return newDecision(id, text, who, from, now), true
}

func newDecision(id, text string, who PersonRef, from *DecisionFrom, now time.Time) WorkstreamDecision {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > DecisionTextMost {
		text = firstLines(text, DecisionTextMost)
	}
	return WorkstreamDecision{ID: id, Text: text, State: DecisionKept, At: now.UTC().Format(time.RFC3339Nano),
		By: NormalizeHandle(who.ID), ByName: who.Name, From: from}
}

// RequestDecision is the decision a request that changed shared work records:
// text is what is now settled, in one line ("Store screenshots are made in
// light and dark"), made by who, from task t.
func RequestDecision(t WorkstreamTask, text string, who PersonRef, id string, now time.Time) (WorkstreamDecision, bool) {
	if strings.TrimSpace(text) == "" || who.ID == "" {
		return WorkstreamDecision{}, false
	}
	return newDecision(id, text, who, &DecisionFrom{Kind: DecisionFromRequest, Task: t.ID, TaskTitle: t.Title}, now), true
}

// sameDecision is whether text, made by maker, says what d says: the same
// words from the same person. ONE ANSWER IS ONE DECISION, and one request
// written on six tasks settles one thing (live2 E1 F6, E2 #5): a list that
// repeats it six times is noise, and a check that reads six copies asks the
// maker six times.
func sameDecision(text, maker string, d WorkstreamDecision) bool {
	return NormalizeHandle(maker) == NormalizeHandle(d.By) && decisionWords(text) == decisionWords(d.Text)
}

func decisionWords(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// KeepNewDecisions are the decisions of fresh that list does not already keep:
// an answered Ask or a request whose words its maker has already settled, and
// that is still kept, records nothing new (nor does a second copy within
// fresh). A rule is recorded every time it is set, because setting it again
// is news.
func KeepNewDecisions(list, fresh []WorkstreamDecision) []WorkstreamDecision {
	var out []WorkstreamDecision
	for _, d := range fresh {
		if d.From == nil || d.From.Kind != DecisionFromRule {
			kept := func(k WorkstreamDecision) bool { return k.State == DecisionKept && sameDecision(k.Text, k.By, d) }
			if slices.ContainsFunc(list, kept) || slices.ContainsFunc(out, kept) {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

// RuleDecisions are the decisions a change of rules from before to after
// records, one per rule that changed, made by who. names gives a standing
// order's name by its id (an id it does not know stays unsaid).
func RuleDecisions(before, after WorkstreamRules, who PersonRef, names func(order string) string, newID func() string, now time.Time) []WorkstreamDecision {
	var lines []string
	if before.DefaultOwner != after.DefaultOwner {
		if after.DefaultOwner.Kind == RulesOwnerEveryone {
			lines = append(lines, "Everyone owns incoming work")
		} else {
			lines = append(lines, firstNonEmpty(after.DefaultOwner.Name, after.DefaultOwner.ID)+" owns incoming work")
		}
	}
	switch b, a := before.Fallback, after.Fallback; {
	case a != nil && (b == nil || b.ID != a.ID):
		lines = append(lines, firstNonEmpty(a.Name, a.ID)+" answers what waits unanswered for a day")
	case a == nil && b != nil:
		lines = append(lines, "Nobody answers for an Ask that waits a day")
	}
	if before.StartWithoutAskingUSD != after.StartWithoutAskingUSD {
		if after.StartWithoutAskingUSD <= 0 {
			lines = append(lines, "Nothing starts without asking")
		} else {
			lines = append(lines, fmt.Sprintf("Tasks under $%.2f start without asking", after.StartWithoutAskingUSD))
		}
	}
	if before.KeepsRunning.Factory != after.KeepsRunning.Factory {
		if after.KeepsRunning.Factory {
			lines = append(lines, "The factory keeps running here")
		} else {
			lines = append(lines, "The factory stops here")
		}
	}
	for _, id := range after.KeepsRunning.Orders {
		if n := names(id); n != "" && !slices.Contains(before.KeepsRunning.Orders, id) {
			lines = append(lines, n+" keeps running")
		}
	}
	for _, id := range before.KeepsRunning.Orders {
		if n := names(id); n != "" && !slices.Contains(after.KeepsRunning.Orders, id) {
			lines = append(lines, n+" stops")
		}
	}
	out := make([]WorkstreamDecision, 0, len(lines))
	for _, l := range lines {
		out = append(out, newDecision(newID(), l, who, &DecisionFrom{Kind: DecisionFromRule}, now))
	}
	return out
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

// WithdrawDecision withdraws decision id from list, by its maker alone. It
// stays in the list, greyed.
func WithdrawDecision(list []WorkstreamDecision, id string, who PersonRef, now time.Time) ([]WorkstreamDecision, error) {
	i, err := makersDecision(list, id, who)
	if err != nil {
		return list, err
	}
	out := slices.Clone(list)
	out[i].State, out[i].ChangedBy, out[i].ChangedAt = DecisionWithdrawn, &PersonRef{ID: NormalizeHandle(who.ID), Name: who.Name}, now.UTC().Format(time.RFC3339Nano)
	return out, nil
}

// ReplaceDecision replaces decision id with text, by its maker alone: the
// new one is kept, made by them, and the old one stays, greyed, naming it.
func ReplaceDecision(list []WorkstreamDecision, id, text, newID string, who PersonRef, now time.Time) ([]WorkstreamDecision, WorkstreamDecision, error) {
	i, err := makersDecision(list, id, who)
	if err != nil {
		return list, WorkstreamDecision{}, err
	}
	if strings.TrimSpace(text) == "" {
		return list, WorkstreamDecision{}, TaskInvalid("a decision needs its words")
	}
	from := &DecisionFrom{Kind: DecisionFromRule, Replaces: id}
	if f := list[i].From; f != nil {
		from = &DecisionFrom{Kind: f.Kind, Task: f.Task, TaskTitle: f.TaskTitle, Replaces: id}
	}
	fresh := newDecision(newID, text, who, from, now)
	out := slices.Clone(list)
	out[i].State, out[i].ReplacedBy = DecisionSuperseded, fresh.ID
	out[i].ChangedBy, out[i].ChangedAt = &PersonRef{ID: NormalizeHandle(who.ID), Name: who.Name}, fresh.At
	return append(out, fresh), fresh, nil
}

func makersDecision(list []WorkstreamDecision, id string, who PersonRef) (int, error) {
	i := slices.IndexFunc(list, func(d WorkstreamDecision) bool { return d.ID == id })
	switch {
	case i < 0:
		return 0, ErrTaskGoneDecision
	case list[i].State == DecisionWithdrawn || list[i].State == DecisionSuperseded:
		return 0, ErrDecisionNotCurrent
	case NormalizeHandle(list[i].By) == "" || NormalizeHandle(list[i].By) != NormalizeHandle(who.ID):
		return 0, ErrDecisionNotMaker
	}
	return i, nil
}

// ErrTaskGoneDecision is a decision the workstream does not keep.
var ErrTaskGoneDecision = TaskInvalid("that decision is not in this workstream")

// CurrentDecisions are the decisions work is checked against: kept, neither
// withdrawn nor replaced.
func CurrentDecisions(list []WorkstreamDecision) []WorkstreamDecision {
	out := []WorkstreamDecision{}
	for _, d := range list {
		if d.State == DecisionKept {
			out = append(out, d)
		}
	}
	return out
}

// The ids of a conflict Ask's two answers.
const (
	ConflictKeep  = "keep"
	ConflictAhead = "go_ahead"
	// ConflictAskID prefixes the Ask the decision check raises.
	ConflictAskID = "ask_decision_"
)

// ConflictAsk is the Ask the decision check raises when work goes against
// decision d: to whoever made it, never to anyone else. Keeping the decision
// is recommended (the work is redone within it, and nothing new is recorded);
// going ahead records an exception, made by them.
func ConflictAsk(t WorkstreamTask, d WorkstreamDecision, why string, by TaskEventBy, now time.Time) TaskAsk {
	maker := PersonRef{ID: d.By, Name: firstNonEmpty(d.ByName, d.By)}
	why = strings.Join(strings.Fields(why), " ")
	detail := "This goes against “" + d.Text + "”, which " + maker.Name + " decided."
	if why != "" {
		detail += " " + why
	}
	return TaskAsk{
		ID: ConflictAskID + d.ID + "_" + now.UTC().Format("20060102T150405.000"), Title: "Go ahead with " + t.Title + "?",
		Detail: detail, AskedAt: now.UTC().Format(time.RFC3339Nano), Decider: &maker, By: &by,
		Against: &TaskAgainst{Decision: d.ID, Text: d.Text, By: maker, Why: why},
		Options: []TaskAskOption{
			{ID: ConflictKeep, Label: "Keep the decision", Recommended: true},
			{ID: ConflictAhead, Label: "Go ahead anyway", Decides: "An exception to “" + d.Text + "”: " + t.Title},
		},
	}
}

// RaiseConflict pauses t on the conflict Ask for decision d: the task waits
// on whoever made it, and its thread says so. A done task is not reopened
// here (the check runs before work is called done); a task that already
// waits on the same decision is unchanged.
func RaiseConflict(t WorkstreamTask, d WorkstreamDecision, why string, by TaskEventBy, newID func() string, now time.Time) (TaskChange, error) {
	for _, a := range OpenAsks(t) {
		if a.Against != nil && (a.Against.Decision == d.ID || sameDecision(a.Against.Text, a.Against.By.ID, d)) {
			return TaskChange{Task: t}, nil
		}
	}
	ask := ConflictAsk(t, d, why, by, now)
	stamp := now.UTC().Format(time.RFC3339Nano)
	t.Asks = append(slices.Clone(t.Asks), ask)
	if t.State != TaskDone {
		t.State = TaskNeeds
	}
	t.Rev, t.UpdatedAt = t.Rev+1, stamp
	line := "Paused: this goes against “" + d.Text + "” (" + ask.Decider.Name + "'s decision), so " + ask.Decider.Name + " is asked"
	return TaskChange{Task: t, Events: []WorkstreamTaskEvent{{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: TaskEventState, Text: line}}}, nil
}
