package factory

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ── LOOSENING THE ASK RULE: NAMED EXCEPTIONS, AND AFI'S SUGGESTION ────────
//
// The way to fewer Asks is a looser rule, not a longer Inbox (People and
// rules, screen 16; owner decisions §13). An Ask an agent raises carries a
// KIND, a few words it chose for what sort of question it is ("docs
// wording"). When more than [SuggestWhenWaiting] Asks wait in one workstream,
// Afi suggests a named exception for the kind most of them share: "Let Afi
// decide docs wording here?", with the evidence of how the steward answered
// that kind before. Yes adds the exception to the rules; from then on an
// agent that asks that kind is not stopped: the server decides it with the
// answer the agent recommended and lists it under Assumed on the task, where
// Send back still overturns it. Not now rests the suggestion for
// [SuggestionRestFor].
//
// AN EXCEPTION ONLY LOOSENS ONE KIND, AND ONLY FOR AGENTS. A person's Ask is
// never decided for them, an Ask with no recommended answer is still asked
// (there is nothing to assume), and the questions the server itself puts (a
// can't-do, a changed file, a decision conflict, an invite that waits) are
// never a kind.

// The three settings of the ask rule (WorkstreamRules.AsksBefore).
const (
	// AsksBeforeOutward asks before anything is published, sent or paid
	// for; everything else is done and listed under Assumed. The default.
	AsksBeforeOutward = "outward"
	// AsksBeforeSharedFile also asks before anything that changes a shared
	// file.
	AsksBeforeSharedFile = "shared_file"
	// AsksBeforeEveryTask asks before every task starts.
	AsksBeforeEveryTask = "every_task"
)

// FallbackAfterChoices are the waits, in hours, a workstream may set before
// its fallback person answers for a decider: half a day, the default day, two
// days, three days. ONE SOURCE: the desk's picker reads them from here.
var FallbackAfterChoices = []int{12, 24, 48, 72}

const (
	// SuggestWhenWaiting is how many Asks may wait in one workstream before
	// Afi suggests an exception: MORE than this many.
	SuggestWhenWaiting = 10
	// SuggestKindFewest is how many waiting Asks must share a kind before it
	// is worth an exception.
	SuggestKindFewest = 3
	// SuggestionRestFor is how long Not now keeps a kind from being
	// suggested again.
	SuggestionRestFor = 7 * 24 * time.Hour
	// AskExceptionsMost is how many exceptions one workstream keeps.
	AskExceptionsMost = 20
	// AskKindLongest is the longest kind, in characters.
	AskKindLongest = 60
	// suggestEvidenceLast is how many past answers of a kind the evidence
	// reads, newest first.
	suggestEvidenceLast = 10
)

// AskException is one kind of question Afi decides without asking in a
// workstream, who added it and when.
type AskException struct {
	Kind    string     `json:"kind"`
	AddedBy *PersonRef `json:"added_by,omitempty"`
	AddedAt string     `json:"added_at,omitempty"`
}

// AskNotNow is a suggested exception the steward turned down, and when.
type AskNotNow struct {
	Kind string `json:"kind"`
	At   string `json:"at"`
}

// RuleSuggestion is Afi's suggestion to loosen the ask rule, in the words the
// side of People and rules draws: the question, its evidence, and the Yes.
// It is worked out when the workstream is read, never kept.
type RuleSuggestion struct {
	Kind string `json:"kind"`
	// Title is the question: "Let Afi decide docs wording here?"
	Title string `json:"title"`
	// Why is the evidence, one or two sentences.
	Why string `json:"why"`
	// Yes is the button that adds it: "Yes, stop asking about docs wording".
	Yes string `json:"yes"`
	// Waiting is how many Asks wait in the workstream, and OfKind how many
	// of them are this kind.
	Waiting int `json:"waiting"`
	OfKind  int `json:"of_kind"`
}

// AskKind is s as a kind is kept: trimmed, one space between words, at most
// [AskKindLongest] characters, with no control characters.
func AskKind(s string) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)), " ")
	if r := []rune(s); len(r) > AskKindLongest {
		s = strings.TrimSpace(string(r[:AskKindLongest]))
	}
	return s
}

// kindKey is how two kinds are compared: case and spacing do not count.
func kindKey(s string) string { return strings.ToLower(AskKind(s)) }

// ExceptionFor is the exception in r that names kind, and whether there is
// one.
func ExceptionFor(r WorkstreamRules, kind string) (AskException, bool) {
	k := kindKey(kind)
	if k == "" {
		return AskException{}, false
	}
	for _, e := range r.Exceptions {
		if kindKey(e.Kind) == k {
			return e, true
		}
	}
	return AskException{}, false
}

// cleanExceptions puts r's exceptions and not-nows in the shape they are kept
// in: kinds cleaned, no repeats, names dropped, bounded.
func cleanExceptions(r *WorkstreamRules) error {
	out := make([]AskException, 0, len(r.Exceptions))
	for _, e := range r.Exceptions {
		e.Kind = AskKind(e.Kind)
		if e.Kind == "" || slices.ContainsFunc(out, func(o AskException) bool { return kindKey(o.Kind) == kindKey(e.Kind) }) {
			continue
		}
		if e.AddedBy != nil {
			e.AddedBy = &PersonRef{ID: NormalizeHandle(e.AddedBy.ID)}
			if e.AddedBy.ID == "" {
				e.AddedBy = nil
			}
		}
		out = append(out, e)
	}
	if len(out) > AskExceptionsMost {
		return fmt.Errorf("a workstream keeps at most %d kinds Afi decides without asking", AskExceptionsMost)
	}
	r.Exceptions = out
	if len(r.Exceptions) == 0 {
		r.Exceptions = nil
	}
	nots := make([]AskNotNow, 0, len(r.NotNow))
	for _, n := range r.NotNow {
		n.Kind = AskKind(n.Kind)
		if n.Kind == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, n.At); err != nil {
			return errors.New("a not-now carries when it was said")
		}
		nots = slices.DeleteFunc(nots, func(o AskNotNow) bool { return kindKey(o.Kind) == kindKey(n.Kind) })
		nots = append(nots, n)
	}
	if len(nots) > AskExceptionsMost {
		nots = nots[len(nots)-AskExceptionsMost:]
	}
	r.NotNow = nots
	if len(r.NotNow) == 0 {
		r.NotNow = nil
	}
	return nil
}

// ExceptionDecisions are the decisions a change of exceptions records: one per
// kind added or taken away. RuleDecisions calls it.
func exceptionLines(before, after WorkstreamRules) []string {
	var lines []string
	for _, e := range after.Exceptions {
		if _, ok := ExceptionFor(before, e.Kind); !ok {
			lines = append(lines, "Afi decides "+e.Kind+" without asking")
		}
	}
	for _, e := range before.Exceptions {
		if _, ok := ExceptionFor(after, e.Kind); !ok {
			lines = append(lines, "Afi asks about "+e.Kind+" again")
		}
	}
	return lines
}

// suggestable is whether ask may count toward a suggestion: an agent's Ask
// with a kind, not one of the questions the server puts itself.
func suggestable(ask TaskAsk) bool {
	if ask.Kind == "" || ask.By == nil || ask.By.Kind == TaskByPerson || ask.Against != nil {
		return false
	}
	for _, p := range []string{CantDoAskID, FileChangedAskID, InviteChaseAskID} {
		if strings.HasPrefix(ask.ID, p) {
			return false
		}
	}
	return true
}

// SuggestException is Afi's suggestion to loosen r, or nil: more than
// [SuggestWhenWaiting] Asks wait among tasks, at least [SuggestKindFewest] of
// them share a kind that has no exception and was not turned down within
// [SuggestionRestFor], and the steward has not mostly overruled Afi on that
// kind before (decisions). The kind most Asks share wins; a tie goes to the
// kind that sorts first, so the suggestion never flickers between reads.
func SuggestException(r WorkstreamRules, tasks []WorkstreamTask, decisions []WorkstreamDecision, now time.Time) *RuleSuggestion {
	waiting := 0
	count := map[string]int{}
	name := map[string]string{}
	for _, t := range tasks {
		if t.State != TaskNeeds {
			continue
		}
		for _, ask := range OpenAsks(t) {
			waiting++
			if !suggestable(ask) {
				continue
			}
			k := kindKey(ask.Kind)
			count[k]++
			if name[k] == "" {
				name[k] = AskKind(ask.Kind)
			}
		}
	}
	if waiting <= SuggestWhenWaiting {
		return nil
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if count[keys[i]] != count[keys[j]] {
			return count[keys[i]] > count[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		if count[k] < SuggestKindFewest {
			break
		}
		if _, ok := ExceptionFor(r, k); ok || restingNotNow(r, k, now) {
			continue
		}
		took, answered := kindEvidence(decisions, k)
		if answered >= SuggestKindFewest && took*2 < answered {
			// The steward mostly answers this kind differently from
			// Afi: deciding it without asking would be wrong more often
			// than right.
			continue
		}
		kind := name[k]
		why := fmt.Sprintf("%d Asks are waiting here, and %d of them are about %s.", waiting, count[k], kind)
		if answered > 0 {
			why = fmt.Sprintf("%s of your last %s answers about %s took Afi's recommendation unchanged. %s",
				capitalize(spellCount(took)), spellCount(answered), kind, why)
		}
		return &RuleSuggestion{
			Kind: kind, Title: "Let Afi decide " + kind + " here?", Why: why,
			Yes: "Yes, stop asking about " + kind, Waiting: waiting, OfKind: count[k],
		}
	}
	return nil
}

// restingNotNow is whether the steward said Not now to kind within the rest.
func restingNotNow(r WorkstreamRules, kind string, now time.Time) bool {
	for _, n := range r.NotNow {
		if kindKey(n.Kind) != kind {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, n.At)
		if err == nil && now.Sub(at) < SuggestionRestFor {
			return true
		}
	}
	return false
}

// kindEvidence reads the newest answers of kind among decisions: how many of
// the last [suggestEvidenceLast] took Afi's recommendation, of how many.
func kindEvidence(decisions []WorkstreamDecision, kind string) (took, answered int) {
	var of []WorkstreamDecision
	for _, d := range decisions {
		if d.From != nil && d.From.Kind == DecisionFromAsk && kindKey(d.From.AskKind) == kind {
			of = append(of, d)
		}
	}
	sort.SliceStable(of, func(i, j int) bool { return of[i].At > of[j].At })
	if len(of) > suggestEvidenceLast {
		of = of[:suggestEvidenceLast]
	}
	for _, d := range of {
		if d.From.Recommended {
			took++
		}
	}
	return took, len(of)
}

// ExceptionAssumed is the Assumed line an agent's Ask becomes when ex covers
// its kind: the question and the answer it recommended, and false when there
// is nothing to assume (no recommended answer, or not an agent's Ask).
func ExceptionAssumed(ask TaskAsk, ex AskException) (string, bool) {
	if !suggestable(ask) {
		return "", false
	}
	i := slices.IndexFunc(ask.Options, func(o TaskAskOption) bool { return o.Recommended })
	if i < 0 {
		return "", false
	}
	chose := ask.Options[i].Label
	if d := strings.TrimSpace(ask.Options[i].Decides); d != "" {
		chose = d
	}
	return cutWords(ask.Title+" · "+chose+" (Afi decides "+ex.Kind+" here without asking)", taskLineMost), true
}

// spellCount spells n as a word up to ten, as the suggestion reads aloud.
func spellCount(n int) string {
	if n == 10 {
		return "ten"
	}
	return countWord(n)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// ── INVITED PEOPLE WHO NEVER JOIN ─────────────────────────────────────────
//
// A task can be given to someone invited who has not joined yet; it waits for
// them. If they have not joined after [InviteChaseAfter], the task's owner
// gets one Ask, "Hand to someone else?" (owner decisions §13), rather than a
// task that sits silently forever. When the owner is the person who has not
// joined, the workstream's steward is asked. Keep waiting asks again after
// another [InviteChaseAfter].

const (
	// InviteChaseAskID prefixes the Ask an invite that waits puts on a task.
	InviteChaseAskID = "ask_invite_"
	// InviteChaseAfter is how long a task waits on someone invited before
	// its owner is asked to hand it on.
	InviteChaseAfter = 3 * 24 * time.Hour
)

// The answers an invite chase offers.
const (
	InviteChaseMe   = "me"
	InviteChaseAfi  = "afi"
	InviteChaseWait = "wait"
)

// IsInviteChaseAsk is whether ask is the Ask an invite that waits put on a
// task.
func IsInviteChaseAsk(ask TaskAsk) bool { return strings.HasPrefix(ask.ID, InviteChaseAskID) }

// InviteHeld is the invite t waits on, "invite:<id>", or "": the person who
// does it, else its owner, when that is someone who has not joined.
func InviteHeld(t WorkstreamTask) string {
	if t.DoneBy.Kind == TaskByPerson && InvitedPerson(t.DoneBy.ID) {
		return t.DoneBy.ID
	}
	if t.Owner.Kind == TaskOwnerPerson && InvitedPerson(t.Owner.ID) {
		return t.Owner.ID
	}
	return ""
}

// InviteChaseDue is whether t should ask now: it is open, it waits on an
// invite made at invitedAt, it has no chase Ask open, and the last chase (or
// the invite) is at least [InviteChaseAfter] old.
func InviteChaseDue(t WorkstreamTask, invitedAt, now time.Time) bool {
	if t.State == TaskDone || t.State == TaskStopped || InviteHeld(t) == "" {
		return false
	}
	if slices.ContainsFunc(OpenAsks(t), IsInviteChaseAsk) {
		return false
	}
	since := invitedAt
	if at, err := time.Parse(time.RFC3339Nano, t.InviteChasedAt); err == nil && at.After(since) {
		since = at
	}
	return now.Sub(since) >= InviteChaseAfter
}

// ChaseInvite puts the "Hand to someone else?" Ask on t, decided by decider
// (the owner, or the steward when the owner is the invitee), naming invitee by
// name. The task waits on it, and goes back to what it was when it is
// answered.
func ChaseInvite(t WorkstreamTask, invitee string, decider PersonRef, newID func() string, now time.Time) (WorkstreamTask, WorkstreamTaskEvent) {
	stamp := now.UTC().Format(time.RFC3339Nano)
	invitee = firstNonEmpty(invitee, "The person you invited")
	options := []TaskAskOption{{ID: InviteChaseMe, Label: "Hand it to me", Recommended: true}}
	if t.DoneBy.Kind == TaskByPerson && InvitedPerson(t.DoneBy.ID) {
		options = append(options, TaskAskOption{ID: InviteChaseAfi, Label: "Give it to Afi"})
	}
	options = append(options, TaskAskOption{ID: InviteChaseWait, Label: "Keep waiting for " + invitee})
	ask := TaskAsk{
		ID:    InviteChaseAskID + newID(),
		Title: "Hand to someone else?",
		Detail: invitee + " was invited " + daysWord(InviteChaseAfter) + " ago and has not joined, so “" + t.Title +
			"” has not started. Hand it to someone who can start it, or keep waiting.",
		Options: options, AskedAt: stamp, Decider: &decider,
		By:   &TaskEventBy{Kind: TaskByAfi, Name: "Afi"},
		Then: t.State,
	}
	if t.State == TaskNeeds {
		if i := slices.IndexFunc(OpenAsks(t), func(x TaskAsk) bool { return x.Then != "" }); i >= 0 {
			ask.Then = OpenAsks(t)[i].Then
		} else {
			ask.Then = ""
		}
	}
	t.Asks = append(slices.Clone(t.Asks), ask)
	t.State, t.InviteChasedAt = TaskNeeds, stamp
	t.Rev, t.UpdatedAt = t.Rev+1, stamp
	return t, WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: *ask.By, Kind: TaskEventState,
		Text: "Afi asks " + firstNonEmpty(decider.Name, decider.ID) + ": Hand to someone else?"}
}

// answerInviteChase applies option on an answered chase Ask to t: Hand it to
// me gives the invitee's part to who answered, Give it to Afi makes Afi do it,
// Keep waiting changes nothing (the next chase is another wait away).
func answerInviteChase(t WorkstreamTask, option string, who PersonRef) WorkstreamTask {
	held := InviteHeld(t)
	if held == "" {
		return t
	}
	switch option {
	case InviteChaseMe:
		if t.Owner.Kind == TaskOwnerPerson && t.Owner.ID == held {
			t.Owner = TaskOwner{Kind: TaskOwnerPerson, ID: who.ID, Name: who.Name}
		}
		if t.DoneBy.Kind == TaskByPerson && t.DoneBy.ID == held {
			t.DoneBy = TaskDoneBy{Kind: TaskByPerson, ID: who.ID, Name: who.Name}
		}
	case InviteChaseAfi:
		if t.DoneBy.Kind == TaskByPerson && t.DoneBy.ID == held {
			t.DoneBy = TaskDoneBy{Kind: TaskByAfi, Name: "Afi"}
		}
	}
	return t
}

// closeInviteChases takes every open chase Ask off t; when nothing else is
// asked, the task goes back to what it was before the first was raised.
func closeInviteChases(t WorkstreamTask) WorkstreamTask {
	if !slices.ContainsFunc(t.Asks, IsInviteChaseAsk) {
		return t
	}
	then := ""
	keep := make([]TaskAsk, 0, len(t.Asks))
	for _, a := range t.Asks {
		if IsInviteChaseAsk(a) {
			then = a.Then
			continue
		}
		keep = append(keep, a)
	}
	t.Asks = keep
	if len(t.Asks) == 0 {
		t.Asks = nil
	}
	if t.Ask == nil && len(t.Asks) == 0 && t.State == TaskNeeds {
		t.State = firstNonEmpty(then, TaskUpNext)
	}
	return t
}

func daysWord(d time.Duration) string {
	n := int(d / (24 * time.Hour))
	if n == 1 {
		return "a day"
	}
	return spellCount(n) + " days"
}
