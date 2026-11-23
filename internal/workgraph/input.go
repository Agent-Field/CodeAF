package workgraph

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// What a producer hands [Build]: the tasks it knows, the links it stored, the
// crews and members around them, and a way to look at a task outside them. The
// builder trusts none of it — a link to a task nobody listed is classified, not
// believed — but it invents none of it either.

// Wait is one stored link: the task it belongs to waits on On. Via is
// [ViaRecord] or [ViaExternal].
type Wait struct {
	On  TaskRef
	Via string
}

// Fact is one task as a producer states it. Node carries everything the
// producer knows; the builder fills what only the whole graph can say
// (ParentState, Crew, the in/out degrees, InCycle) and overwrites nothing else.
type Fact struct {
	Node  GraphNode
	Waits []Wait
}

// Standing is whether a target outside the scope exists.
type Standing int

// The three answers a resolver gives about a task outside the scope.
const (
	// StandingUnknown: the target's owner could not be read, so nothing is said
	// about the task — it is not called missing.
	StandingUnknown Standing = iota
	// StandingFound: the task exists; State is its state word.
	StandingFound
	// StandingAbsent: the owner was read and has no such task.
	StandingAbsent
)

// Resolution is a resolver's answer for one target.
type Resolution struct {
	Standing Standing
	State    string
}

// CrewInput is one crew a crew scope is made of; the first is the scope's own.
type CrewInput struct {
	CrewFact
}

// MemberInput is one membership: a conversation in a crew. Home marks the
// membership that names the manager the conversation reports to.
type MemberInput struct {
	MemberFact
	Home bool
}

// Input is everything one snapshot is built from.
type Input struct {
	Scope GraphScope
	About ScopeFacts
	// Root is the task a task scope is about; the snapshot keeps its sub-tasks.
	Root string
	// Facts are the tasks, in any order.
	Facts   []Fact
	Crews   []CrewInput
	Members []MemberInput
	// Resolve answers about a target the facts do not list. Nil means every such
	// target is unknown.
	Resolve func(TaskRef) Resolution
	// Revision names the source facts; Owners, Unread, Truncated and Excluded
	// are the producer's own account of how much it read.
	Revision  string
	AsOf      time.Time
	Owners    int
	Unread    []OwnerIssue
	Truncated bool
	Excluded  []string
	// Unavailable is [Coverage.Unavailable].
	Unavailable []string
}

// orderKey is where a task sits in creation order.
type orderKey struct {
	at    int64
	owner string
	seq   uint64
	// task breaks the last tie: plan rows carry no number, and two of them can
	// share an instant.
	task string
}

func keyOf(n *GraphNode) orderKey {
	k := orderKey{owner: n.Ref.Owner, seq: n.Seq, task: n.Ref.Task}
	if n.Clock.AdmittedAt != nil {
		k.at = n.Clock.AdmittedAt.UnixNano()
	}
	return k
}

func (a orderKey) less(b orderKey) bool {
	if a.at != b.at {
		return a.at < b.at
	}
	if a.owner != b.owner {
		return a.owner < b.owner
	}
	if a.seq != b.seq {
		return a.seq < b.seq
	}
	return a.task < b.task
}

func sortFacts(facts []Fact) {
	sort.SliceStable(facts, func(i, j int) bool {
		return keyOf(&facts[i].Node).less(keyOf(&facts[j].Node))
	})
}

// Cut shortens s to at most limit runes and says whether it did. It never
// splits a rune, and it is ALLOCATION-BOUNDED: it scans at most limit+1 rune
// boundaries (never the whole of an oversized input), copies only the bytes it
// keeps, and returns a string that owns them. A tiny result therefore never
// pins the huge string it was cut from. Bytes that are not valid UTF-8 are
// replaced by U+FFFD, the way the wire would spell them, so what is returned
// is always valid text.
func Cut(s string, limit int) (string, bool) {
	if limit <= 0 {
		return "", s != ""
	}
	count := 0
	end, cut := len(s), false
	for i := range s {
		if count == limit {
			end, cut = i, true
			break
		}
		count++
	}
	kept := strings.ToValidUTF8(s[:end], "\uFFFD")
	return strings.Clone(kept), cut
}

// CutBytes shortens s to at most limit BYTES on a rune boundary and says whether
// it did. Bytes that are not valid UTF-8 are replaced by U+FFFD before the cut, so
// what is returned is valid text and the bound is on what is returned.
func CutBytes(s string, limit int) (string, bool) {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if len(s) <= limit {
		return strings.Clone(s), false
	}
	end := limit
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return strings.Clone(s[:end]), true
}

// stamp is a recorded instant, or nil for the zero time.
func stamp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// Stamp is the pointer a [Clock] field holds for an instant the record carries,
// and nil for one it does not.
func Stamp(t time.Time) *time.Time { return stamp(t) }
