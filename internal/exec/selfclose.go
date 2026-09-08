package exec

// A leaf closes its own photograph finding before it lands.
//
// THE DEFECT THIS ANSWERS. PhotographAfter is a measurement of the finished
// leaf, and everything it finds — a public name the work deleted, a name the
// work reads that nothing binds, the work's own checks red, a check the work
// turned red — used to reach nobody until three things had happened: the leaf
// landed, a gate weighed it, and the growth governor bought a repair round. That
// round is a COLD SPLICE. It is a new leaf, with a fresh brief, in a new
// context, holding none of the reasoning that produced the fault it is sent to
// fix — and igel's v4-flash s14 run is the whole argument. Its original leaf
// photographed clean twice (`surface task-2 lost=0`). Then a growth round bought
// `task-2-x1-n4`, which lost three public names off `igel/configs.py`
// — init_file_path, res_path, temp_post_req_data_path — and every cold leaf
// after it deleted what the last one had relied on. All twenty-four hidden tests
// failed on `cannot import name 'temp_post_req_data_path'`, a name that was
// readable off the tree the moment the leaf that removed it stopped.
//
// The leaf that caused the fault is the cheapest and best-informed reader of it,
// and at the moment the photograph is taken it is STILL STANDING: it holds its
// own transcript, its own reasoning, its own prefix cache, and the room it did
// not spend. So it is asked, once, before anything is bought.
//
//	A FINDING A LEAF RAISED AGAINST ITSELF IS PUT TO THAT LEAF BEFORE IT IS
//	PUT TO A GATE. The leaf is not landed; it is resumed on the transcript it
//	already has, with the finding as the note, inside what is left of its own
//	meter — the same shape a leaf that ran out of room is resumed in, and never
//	a new bound of its own.
//
// EVERY BOUND HERE IS DERIVED AND NONE IS NEW. The room a close may spend is
// exactly what the leaf has left of the turns, the billed tokens and the wall it
// was already granted; a leaf whose meter is spent lands with its finding, as it
// does today. The number of closes is bounded by the finding KINDS the leaf can
// raise against itself, one each, because a second reading that is still red has
// said the only thing it is going to and the gate is the floor under it.
//
// It is asked at the one exit where a leaf finishes UNDER ITS OWN POWER. A leaf
// landed by its clock, its turns, its budget or a straggler hand-back has no
// room by construction, and a leaf that was cancelled, paused, faulted, promoted
// or split is not being asked to deliver anything.

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The four findings a leaf can raise against ITSELF, spelled once. They are the
// four Sourced findings the delivery gate raises with no citation to weigh
// (revision.Regressions, OwnChecksFailing, the removed-public-name settlement,
// UnboundNames) — the ones that are measurements of the world rather than a
// judge's reading, which is exactly why a leaf can be handed one and told to
// fix it without any model being asked whether it is true.
const (
	SelfCloseLostNames = "lost public names"
	SelfCloseUnbound   = "unbound names"
	SelfCloseOwnChecks = "its own checks"
	SelfCloseRegressed = "checks it turned red"
)

// SelfCloseFinding is one fault a leaf's closing photograph raised against the
// leaf's own work: what kind it is, what it is about, and the sentence the leaf
// is handed.
type SelfCloseFinding struct {
	// Kind is one of the four constants above.
	Kind string
	// Names is what the finding is ABOUT, in the reading's own words. A kind
	// alone sends a worker off to run a suite; the name is the diagnosis.
	Names []string
	// Fact is the finding without an instruction addressed to a worker that may
	// already have stopped. A handover needs this clause on its own, while
	// Sentence composes it with the move offered to a leaf still standing.
	Fact string
	// Sentence is the finding put to the leaf in the person-facing register: a
	// fact, then the one move that settles it, then the honest alternative —
	// because a measurement can be right about the tree and wrong about the
	// request, and a leaf told to obey a measurement it disagrees with will
	// invent work rather than say so.
	Sentence string
}

// SelfCloseFindings reads the leaf's own after-photograph for faults the LEAF
// caused.
//
// Every one of these fields is already scoped to the run's own record of what it
// changed (see photograph.go), so a finding here is a finding about this work
// and never about the repository it arrived in. A nil field is NO CLAIM —
// nobody looked — and reads here exactly as it reads at the gate: silence, never
// a clean bill.
func SelfCloseFindings(outcome *Outcome) []SelfCloseFinding {
	if outcome == nil {
		return nil
	}
	var found []SelfCloseFinding
	if names := trimmedNames(outcome.Removed); len(names) > 0 {
		fact := "this work removed public names that the tree spelled before it — " +
			describeChecks(names)
		found = append(found, SelfCloseFinding{
			Kind: SelfCloseLostNames, Names: names,
			Fact: fact,
			Sentence: fact + ". Restore them, or say in your delivery why the " +
				"request requires their removal.",
		})
	}
	if names := trimmedNames(outcome.Unbound); len(names) > 0 {
		fact := "this work reads names that nothing in the tree defines — " + describeChecks(names)
		found = append(found, SelfCloseFinding{
			Kind: SelfCloseUnbound, Names: names,
			Fact:     fact,
			Sentence: fact + ". Bind them where they are read from, or stop reading them.",
		})
	}
	if names := trimmedNames(outcome.OwnFailing); len(names) > 0 {
		fact := "the checks this work wrote are red — " + describeChecks(names)
		found = append(found, SelfCloseFinding{
			Kind: SelfCloseOwnChecks, Names: names,
			Fact: fact,
			Sentence: fact +
				". Get them passing, or say in your delivery which of them the request does not ask for.",
		})
	}
	if names := trimmedNames(outcome.Regressed); len(names) > 0 {
		fact := "this work turned checks red that passed before it — " + describeChecks(names)
		found = append(found, SelfCloseFinding{
			Kind: SelfCloseRegressed, Names: names,
			Fact: fact,
			Sentence: fact +
				". Get them green again — nobody asked for their repository to stop working.",
		})
	}
	return found
}

// trimmedNames drops the empty entries a reading can carry without changing what
// it means. An all-empty list is no finding: a finding about nothing is the one
// thing that would make a leaf spend its remaining room on a void.
func trimmedNames(names []string) []string {
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			kept = append(kept, name)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// SelfCloseRoom is what the leaf has left of ITS OWN meter, which is the whole
// of what a close is allowed to spend.
//
// There is no constant here and there must never be one. A close that had its
// own grant would be a second budget nobody voted for, riding on top of the one
// the scheduler leased this leaf; what the leaf did not spend is already its,
// and spending it on making its own work correct is the cheapest thing it can be
// spent on.
// NoWall is what a belt with no clock at all passes for the wall. It is not a
// duration and it is never subtracted from: it says the question does not apply
// here, which is a different answer from "none left".
const NoWall = time.Duration(-1)

type SelfCloseRoom struct {
	// Turns, Tokens and Wall are what is left on each meter that BOUNDS this
	// belt. A belt that does not meter one of them leaves it at zero and says
	// so through the flag below rather than reading as exhausted — a belt with
	// no turn cap and no token ceiling reads zero on both, and reading that as
	// "out of turns" would silently switch the mechanism off for a whole belt.
	Turns  int
	Tokens int
	Wall   time.Duration
	// Left says every meter that bounds this leaf still has something on it.
	Left bool
	// Why is the meter that said no, for the record. Empty when Left.
	Why string
}

// RoomLeft subtracts what the leaf has spent from what it was granted.
//
// A NON-POSITIVE GRANT MEANS THIS BELT DOES NOT BOUND THAT, which is a different
// fact from a grant that has run out, and the two must not be spelled the same
// way: a belt with no turn cap and no token ceiling reads zero on both, and
// reading those zeros as "spent" would switch this mechanism off for a whole
// belt.
//
// THE CLOCK IS THE OTHER WAY ROUND, because a clock is the one meter every belt
// has and a spent one is the dangerous reading to get wrong. wall is what is
// LEFT on it, so zero and below mean spent; a belt with no clock at all passes
// [NoWall].
//
// The reserve subtracted from the wall is the leaf's own landing reserve, which
// is the room already set aside for a leaf to finish safely — precisely the room
// a close needs, and not one second that was not already the leaf's.
func RoomLeft(outcome *Outcome, maxTurns, maxTokens int, wall, reserve time.Duration) SelfCloseRoom {
	room := SelfCloseRoom{Left: true}
	if maxTurns > 0 {
		room.Turns = maxTurns - outcome.Turns
		if room.Turns <= 0 {
			room.Left, room.Why = false, "its turns were spent"
		}
	}
	if maxTokens > 0 {
		room.Tokens = maxTokens - spent(outcome)
		if room.Tokens <= 0 && room.Left {
			room.Left, room.Why = false, "its budget was spent"
		}
	}
	if wall != NoWall {
		room.Wall = wall - reserve
		if room.Wall <= 0 && room.Left {
			room.Left, room.Why = false, "its clock was spent"
		}
	}
	// AND THE LEAF'S OWN ACCOUNT OF ITSELF OVERRULES THE ARITHMETIC. A leaf can
	// be inside every figure above and still have been told to land — by a
	// deadline reserve it entered and then finished early inside. Exhausted is
	// the field that carries exactly that, and a leaf
	// that was told to stop must not be handed more work by a mechanism that
	// only read the numbers.
	if room.Left && outcome.Exhausted != "" {
		room.Left, room.Why = false, "it had already been told to land"
	}
	return room
}

// SelfCloser is one leaf's own closing, held across the leaf's landings so that
// a kind is put to it once and once only.
//
// It is a value on the belt's stack rather than anything durable, because it is
// a fact about ONE run of one leaf: a later attempt on the same node is a
// different worker holding a different transcript, and it is entitled to its own
// reading of its own work.
type SelfCloser struct {
	history *store.Store
	task    Task
	// closed is the kinds this leaf has already been asked about. A second
	// reading of the same kind that is still red is the FLOOR: it lands, and
	// the gate weighs it exactly as it did before this mechanism existed.
	closed map[string]bool
}

// NewSelfCloser arms the closing for one leaf. A nil journal is ordinary — a
// leaf run outside a graph has nothing to write into — and the decision is
// taken identically with or without one.
func NewSelfCloser(history *store.Store, task Task) *SelfCloser {
	return &SelfCloser{history: history, task: task, closed: map[string]bool{}}
}

// Close is the whole decision, and it is asked with the after-photograph already
// on the outcome.
//
// It answers with the note the leaf resumes on and the findings that note is
// about, or nothing at all when the leaf is to land on the reading it is
// holding. The findings come back beside the note rather than being re-derived
// by the caller, because the set that is DUE is not the set the outcome carries
// — a kind already closed once is standing in both. Both answers are journaled,
// because "the leaf fixed its own work" and "nobody ever looked" were the same
// silence in every store this was built from (FAILSAFE.md clause 4).
//
// It never fails a leaf and it never returns an error. A journal that refuses
// the row, a store that is not there, a task with no node to file against —
// each changes what is written down and nothing about what the leaf does.
func (c *SelfCloser) Close(outcome *Outcome, room SelfCloseRoom) (string, []SelfCloseFinding) {
	if c == nil || outcome == nil {
		return "", nil
	}
	found := SelfCloseFindings(outcome)
	if len(found) == 0 {
		return "", nil
	}
	var due []SelfCloseFinding
	var already []string
	for _, finding := range found {
		if c.closed[finding.Kind] {
			already = append(already, finding.Kind)
			continue
		}
		due = append(due, finding)
	}
	switch {
	case len(due) == 0:
		// The floor. Every kind standing here has already been put to this leaf
		// once and is still red, so it lands and the gate has it.
		c.journal(found, outcome.Turns, false,
			"it had already closed "+strings.Join(already, " and ")+" once")
		return "", nil
	case !room.Left:
		c.journal(due, outcome.Turns, false, room.Why)
		return "", nil
	}
	for _, finding := range due {
		c.closed[finding.Kind] = true
	}
	c.journal(due, outcome.Turns, true, "")
	return selfCloseNote(due), due
}

// Kinds names what a set of findings is, for a caller that has to say it out
// loud — the trace line inside the leaf, and the stream line outside it.
func SelfCloseKinds(found []SelfCloseFinding) []string {
	kinds := make([]string, 0, len(found))
	for _, finding := range found {
		kinds = append(kinds, finding.Kind)
	}
	return kinds
}

// selfCloseNote is the finding put to the leaf, in the register a person would
// use with a colleague who is one statement away from handing something over.
//
// It leads with WHEN, because the leaf believes it is finished and the only
// thing that makes the note make sense is that it is not landed yet. It states
// the fact and the move, and it offers the honest alternative — a measurement
// can be right about the tree and wrong about the request, and a worker with no
// way to say so will invent work rather than contradict the machine. And it
// says what the room is, because a leaf that does not know it is nearly out will
// start something instead of finishing.
func selfCloseNote(found []SelfCloseFinding) string {
	var note strings.Builder
	note.WriteString("Before this lands, your own reading of the finished tree found ")
	if len(found) == 1 {
		note.WriteString("this:\n\n")
	} else {
		note.WriteString(strconv.Itoa(len(found)) + " things:\n\n")
	}
	for _, finding := range found {
		note.WriteString("- " + finding.Sentence + "\n")
	}
	note.WriteString("\nThis is your own work and it is still open, so settle it now rather than " +
		"handing it on. Use what is left of your room on this and start nothing new; then say " +
		"plainly what you changed, or why what you found has to stand.")
	return note.String()
}

// journal writes down one closing decision, taken or declined.
//
// The names ride along BOUNDED and the kinds do not, because the kinds are four
// short words and the names are what a reader actually needs: igel s14's stores
// say `lost: 3` in three places and never once say which three, and the word
// `temp_post_req_data_path` is the entire diagnosis of that run.
func (c *SelfCloser) journal(found []SelfCloseFinding, turns int, closed bool, why string) {
	if c.history == nil || strings.TrimSpace(c.task.StoreNodeID) == "" || len(found) == 0 {
		return
	}
	record := store.LeafSelfClose{
		Kinds: SelfCloseKinds(found), Turns: turns, Closed: closed, Why: why,
	}
	seen := map[string]bool{}
	for _, finding := range found {
		for _, name := range finding.Names {
			if !seen[name] {
				seen[name], record.Names = true, append(record.Names, name)
			}
		}
	}
	sort.Strings(record.Names)
	if len(record.Names) > store.VerificationSample {
		record.Names = record.Names[:store.VerificationSample]
	}
	_ = c.history.RecordLeafSelfClose(c.task.StoreNodeID, record)
}
