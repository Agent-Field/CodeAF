package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DeliveryGate is the final judge's evidence about one job. Pass is the first
// delivery's result; Gap names what it missed; PolishClosed says whether the
// single permitted repair was subsequently judged complete.
//
// The last fields are the gap ledger, and they are fields on this event rather
// than a second event kind because every reader of a job's judgement already
// reads this one. Quotes are the spans of the user's verbatim request the gap
// was said to be a failure of, and Quote is those spans as the one line a
// person reads; Round is which round of repair it was weighed for; Extended
// says the job actually grew work to close it; Refused names, in the words the
// user would be told, why it did not. A citation that was extended on is spent
// — the same words may not buy a second round — so the ledger that bounds the
// loop is exactly what replays out of the journal.
//
// Quotes is a list because a gap may be a failure of several things at once:
// the mechanical half of the gate names one citation per file the plan promised
// and the disk does not hold. Quote stays, holding the same citations joined,
// because it is what every existing reader and every already-written journal
// row has — see Cited, which is how the ledger reads either.
//
// Mechanical distinguishes those two halves, and it is recorded rather than
// inferred because the exit code depends on it. A refused gap from a model
// judge is the gate being wrong; a refused gap from the mechanical half is a
// file that is still not on disk, and no refusal of a citation makes it appear.
type DeliveryGate struct {
	Pass         bool     `json:"pass"`
	Gap          string   `json:"gap,omitempty"`
	PolishClosed bool     `json:"polish_closed"`
	Quote        string   `json:"quote,omitempty"`
	Quotes       []string `json:"quotes,omitempty"`
	Round        int      `json:"round,omitempty"`
	Extended     bool     `json:"extended,omitempty"`
	Refused      string   `json:"refused,omitempty"`
	Mechanical   bool     `json:"mechanical,omitempty"`

	// Finding names WHICH MEASUREMENT raised this gap — `regression`,
	// `own-checks-failing`, `removed-public-name`, `removed-checks` — and
	// Quotes above holds the names it cited. Empty where a model judge read the
	// request rather than the world.
	//
	// The pair is what makes a finding comparable ACROSS ROUNDS, and until it
	// existed the only identity a finding had was its sentence — with a bounded
	// list of names glued into the middle, so one finding raised over two
	// different tails read as two. happy-dom's v4-flash s13 raised the identical
	// removed-checks finding on four consecutive rounds, each round bought a
	// repair, no repair could close it, and no reader of this event could say
	// the four were one thing (2026-08-29, bench/deepswe).
	Finding string `json:"finding,omitempty"`

	// Unclosed says the gap STANDS: the repair that would have closed it was
	// never bought, so nothing ran and nothing about the shortfall changed.
	//
	// It is the distinction the exit code turns on, and it is recorded rather
	// than read out of the refusal sentence because those are two categorically
	// different refusals wearing the same field. A gap refused as ungrounded, or
	// as one somebody already paid to close, is the GATE being wrong and caught
	// at it — the deliverable stands whole. A gap whose repair a governor would
	// not fund, or that nothing could plan, is the gate being RIGHT and
	// unaffordable: the thing it named is still missing, and a run that hands
	// that over is handing over less than it promised. One measured run shipped
	// "Deliverable is empty - contains no implementation" over exit 0 because
	// the two were one field (2026-08-28, meta/muse-spark-1.1).
	Unclosed bool `json:"unclosed,omitempty"`

	// Overturned says the refusal was CHECKED AGAINST THE WORLD and the finding
	// lost: the file the review says is missing is on disk under the name the
	// request used, or the things it says are absent are in the delivered text.
	//
	// It is the other half of the distinction Unclosed opened, and it is the one
	// the exit code should have been reading all along. Refused holds refusals
	// of two categorically different kinds. One looks at the filesystem or at
	// the deliverable and finds the review wrong — that acquits, and charging it
	// a non-zero code would teach a harness to distrust the gate's own
	// corrections. The other looks only at where the review's words came from
	// and declines to BUY a round; it settles nothing about whether the work
	// landed, because no ruling on a citation makes missing work appear.
	//
	// Recorded rather than inferred from the sentence, for the reason every
	// other field here is: the exit code turns on it, and a sentence is not a
	// field. Seven of eight measured runs exited 0 over a provenance refusal
	// while the review that named the missing work was right every time
	// (2026-08-28, bench/deepswe; see docs/design/gate/SETTLEMENT.md §2).
	//
	// THE WORLD IS THE DISK, A READING, OR THE RECORD — NEVER THE DELIVERABLE'S
	// OWN PROSE. The deliverable is the component the gate is checking, and a
	// refusal that reads it is FAILSAFE clause 2 broken in the strict sense the
	// clause states it. One measured run set this field because the words of the
	// request appeared in a summary the worker had written about work it had not
	// done, and shipped 1 of 20 hidden checks over exit 0 (2026-08-29,
	// bench/deepswe textual s5; SETTLEMENT.md §6). The delivered text may settle
	// a finding only where it IS the whole of what the run left behind — a
	// question answered in prose, whose message is its own artifact.
	Overturned bool `json:"overturned,omitempty"`

	// Unmoved says the repair round that was judged here LEFT THE TREE EXACTLY AS
	// THE FINDING FOUND IT: same files, same sizes, same modification times, stamped
	// on either side of the round.
	//
	// It is recorded because it is the fact that explains a PolishClosed which is
	// absent — and, on the runs that made this necessary, one which is present and
	// should not have been. A composed repair (revision.Compose) rewrites the
	// account of work that already landed and runs nothing, so it cannot change what
	// the world says; ink s5 and ofetch s5 both composed a better summary over a
	// finding about the substance of the work, were re-judged on the summary, and
	// settled whole at 7 of 25 and 44 of 47 hidden checks (2026-08-29,
	// bench/deepswe; docs/design/gate/SETTLEMENT.md §8).
	//
	// A round that moved nothing may still close a finding whose only ground was the
	// delivered text — that is what writing can honestly fix — so this is a fact
	// about the round and never a verdict on its own. The verdict is PolishClosed,
	// which the wiring sets only where the two agree.
	Unmoved bool `json:"unmoved,omitempty"`

	// Exercised is the acceptance mapping as the gate settled it: one row per
	// behaviour the request stated, naming the check that exercises it, or
	// naming nothing when no check does.
	//
	// It is recorded rather than reduced to the finding it produced, because the
	// mapping is the evidence and the finding is only its conclusion. A run that
	// passed with every point exercised and a run that passed because the
	// checklist was empty are the same event without it, and telling those two
	// apart is the whole of what an autopsy of this mechanism has to do.
	Exercises []ExercisedPoint `json:"exercises,omitempty"`

	// Unexercised is the behaviours the request stated that no check exercises,
	// one entry per line of the request they were read from.
	//
	// It is the FINDING; Exercised above is the evidence it is a conclusion of.
	// They are two fields because a mapping with three empty rows and a finding
	// naming three behaviours are the same fact only to a reader who already
	// knows this mechanism exists, and the person watching the run is not that
	// reader. igel s6 journaled the mapping and never the finding: the gate event
	// carried "exercises: 17 rows, 3 unmapped" and the coverage gap survived only
	// as a paragraph inside `gap`, where the stream's own line — firstLine(gap) —
	// could not reach it and no repair round was ever aimed at it.
	Unexercised []string `json:"unexercised,omitempty"`

	// Unasserted is the behaviours the request stated that a check NAMES and no
	// assertion WEIGHS, one entry per line of the request they were read from,
	// each naming the observables nothing asserted.
	//
	// It is a finding of its own beside Unexercised because the two are answered
	// by different evidence and a repair round is aimed at them differently.
	// Unexercised says write a check; this says the check you wrote runs the
	// behaviour and asserts nothing about it, and names which identifier to
	// assert on. textual s13's gate had said `no check exercises:
	// RichLog.write(expand=True) …` in round one; round two wrote a check that
	// called `write("short", expand=True)` and asserted `len(lines) > 0`, the
	// mapping paired the two, and the run passed at exit 0 with the hidden check
	// for that behaviour red.
	//
	// It stands exactly as Unexercised does — it leaves the delivery short in
	// Whole below, and it empties the one way a measurement empties: a later
	// reading finds an assertion that names the observable.
	Unasserted []string `json:"unasserted,omitempty"`

	// OwnFailing is the checks THIS WORK WROTE that are red: names the baseline
	// roster never held, so nothing that was working stopped.
	//
	// It is a field beside Unexercised rather than prose inside Gap because the
	// distinction it carries is the one happy-dom's nemotron n1 run lost. That
	// gate read `This work broke checks that were passing before it:
	// IntersectionObserver initial observation queuing …` over eighteen checks
	// the run had written that hour, on a tree the grader scored 9 of 9. A run
	// that broke the repository and a run that has not finished its own tests
	// are two different states, and an autopsy with one list could not tell them
	// apart.
	OwnFailing []string `json:"own_failing,omitempty"`

	// Unmeasured says the gate held a checklist and could settle none of it:
	// the project declares no verification this run could read and the change
	// produced no readable diff, so nothing could be matched to what the
	// request asked for.
	//
	// It is a field rather than a silence because NOBODY LOOKED IS NOT NOTHING
	// WRONG, and the two are the same event without it. A delivery that
	// satisfied every point and one that was measured against nothing both
	// journal a passing gate; only this tells them apart, and an autopsy of
	// this mechanism has nothing else to read.
	Unmeasured string `json:"unmeasured,omitempty"`

	// Unreadable says the project DECLARED a way of checking itself and this run
	// could not read it. It is the half of Unmeasured that leaves the delivery
	// short, and it is what Whole below spends.
	Unreadable bool `json:"unreadable,omitempty"`

	// Consumers is the finding the changed-definition reading produced: one line
	// per definition this run reshaped that the rest of the project still uses,
	// naming the shape its callers expect and how many of them there are.
	//
	// It is the FINDING; EventConsumers is the evidence it is a conclusion of,
	// and they are two records for the reason Unexercised and Exercises are two:
	// a finding that lives only as a paragraph inside `gap` is journaled by
	// nothing and reachable by nothing. igel s12 changed `configs` from a dict
	// to an instance of a class it wrote and the whole store held not one word
	// about it (2026-08-29, bench/deepswe).
	Consumers []string `json:"consumers,omitempty"`

	// Unbound is the finding the unbound-reference reading produced: one line
	// per name the run's own sources READ that nothing in the tree binds, each
	// naming the file and line it is read at.
	//
	// It is a field beside Consumers because it is the same kind of fact one
	// question further back. Consumers is a name that exists and no longer
	// answers to how it is used; this is a name that does not exist at all —
	// igel s14 imported `temp_post_req_data_path` from a module that had stopped
	// binding it, and the run's whole record of that was that its own checks
	// were red, never WHICH name was missing (2026-08-29, bench/deepswe).
	Unbound []string `json:"unbound,omitempty"`

	// Subject is WHAT THIS GATE JUDGED, in the words the gate keeps them:
	// "tree (6 files)" where the run changed the repository and the change was
	// the deliverable, "claim" where the run left nothing behind and the
	// worker's message was the whole of what it produced.
	//
	// It is a field because an autopsy has nothing else to read. Three gates on
	// one run refused a delivery for what the worker's final MESSAGE was — "the
	// fenced text contains only {"contract": …}" — while forty-two kilobytes of
	// changed Python sat in the worktree and the artifact record named every
	// file of it (2026-08-29, bench/deepswe textual-richlog-follow-state
	// nemotron n1). Afterwards those three events were indistinguishable from
	// three refusals over a real reading of the world, and the only way to tell
	// was to reconstruct the prompt from the transcript.
	//
	// Empty on every gate journaled before this existed, which reads as
	// unrecorded rather than as either answer.
	Subject string `json:"subject,omitempty"`

	// HeldPoint is WHICH BEHAVIOUR OF THE REQUEST this gate was held to: the
	// span a refusal was built on, the size of the list a pass was weighed
	// against, or "checklist: empty" where the request states none and the
	// requirement was off.
	//
	// Subject says what the gate read; this says what it was allowed to
	// convict on. textual v4-flash s13 journaled two gates with the subject
	// right and the quote a verbatim behaviour of the request, and nothing in
	// the event could tell whether the quote had passed the checklist or there
	// had been no checklist to pass — the mechanism working and the mechanism
	// switched off, wearing one event.
	//
	// Empty on a claim-subject gate, where there is no such list, and on every
	// gate journaled before this existed.
	HeldPoint string `json:"held_point,omitempty"`
}

// ExercisedPoint is one row of that mapping: a behaviour the request stated and
// the check that exercises it. An empty Check is the finding — nothing in the
// project's own verification touches this.
type ExercisedPoint struct {
	Point string `json:"point"`
	Check string `json:"check,omitempty"`

	// Observables is what the behaviour was weighed against: the identifiers the
	// request spelled, bound, or named in words against the tree's own public
	// surface. It is journaled beside the finding because the RESOLUTION is the
	// half an autopsy cannot reconstruct — "vertical scrollbar position" became
	// `ScrollBar.position` by a reading of a tree that has since moved on, and
	// without the row there is no way to ask whether the door was asking about
	// the right thing at all.
	Observables []string `json:"observables,omitempty"`

	// Unasserted is the behaviour's own observables — the identifiers the
	// request spelled — that the mapped check's ASSERTIONS never name. A row
	// with a check and an unasserted list is a pairing the world admits and the
	// check does not earn: the check runs the behaviour and weighs nothing about
	// it.
	//
	// It is journaled beside the pairing rather than reduced to the finding it
	// produces, for the reason Exercises itself is. textual s13 shipped at exit
	// 0 with three such rows, and afterwards there was no way to ask which
	// observable had been skipped — `expand` and `min_width` were named in the
	// request, called in the check, and asserted nowhere, and the mapping said
	// only that a check existed. See revision.WeighAssertions.
	Unasserted []string `json:"unasserted,omitempty"`
}

// Whole is THE reading of what this gate settled, and it is a method because it
// had been two readings.
//
// Three fields say the delivery stands: the first judgement passed; the one
// permitted repair was re-judged and passed (PolishClosed); or the finding was
// weighed against the world and lost (Overturned). Everything else leaves the
// finding STANDING — a fail nothing repaired, a refusal about where a review got
// its words, a gap nothing could fund, a promised file the disk does not hold.
//
// It lives here, on the event, because two readers spent this differently and
// disagreed out loud. deliveredWhole in cmd/aforge/do.go combined all three
// fields to decide the exit code; gateWords, in the same file, built the line a
// person watching reads from Pass and Refused alone — so ink s5 and ofetch s5
// printed "gate: fail — The deliverable is a listing of files, not the answer
// itself" as the last thing anybody saw and left with exit 0
// (2026-08-29, bench/deepswe; docs/design/gate/SETTLEMENT.md §7). A verdict a
// person reads and a verdict an exit code carries are one fact, and one fact is
// one reading.
func (g DeliveryGate) Whole() bool {
	// A PASS OVER A SUITE NOBODY COULD READ IS NOT A PASS OVER A CHECKED
	// DELIVERY. The judge answered on the deliverable's own words, and the one
	// thing that could have contradicted them — the project's own verification —
	// was declared, attempted, and unreadable. ink s7 journaled `npx ava --tap`
	// killed at its ceiling, passed the next gate over an empty roster, and left
	// with exit 0 at 13 of 25 hidden checks. FAILSAFE clause 5: a fail-safe has
	// a FLOOR that cannot deliver nothing as done.
	//
	// A project that declares no verification at all is deliberately not here.
	// There was nothing to read, the coverage question is unanswerable rather
	// than unanswered, and failing every such delivery would fail every piece of
	// prose this program writes. Which of the two it was is Unreadable, and it
	// is journaled either way.
	if g.Unreadable {
		return false
	}
	// AN ACQUITTAL IS OF ONE FINDING, AND THE COVERAGE SET IS NOT THAT FINDING.
	//
	// Overturned says a refusal was weighed against the world and lost: the file
	// the review called missing is on disk under the name the request used. That
	// is true of ONE thing, and it says nothing whatever about behaviours the
	// request states that no check exercises — which is a measurement of the
	// repository taken by a different mechanism, on different evidence, and
	// closed only by a check existing.
	//
	// textual s10 settled exactly that way: one gate, `unexercised` naming two
	// groups of behaviours, the verdict refused as `what it asked for is already
	// on disk under the name the request used`, Overturned true — and Whole()
	// read the acquittal as covering everything on the event. Exit 0 at 5 of 20
	// hidden checks, with the run's own record naming thirteen behaviours it had
	// measured as exercised by nothing.
	//
	// The set is the finding, and a finding that STANDS is what this method is
	// for. It empties the only way a finding here ever empties: a later mapping
	// measures a check that covers it. See revision.Unexercised, which is
	// Sourced for the same reason a regression is — no citation is weighed for
	// it, because a person does not have to ask for the behaviour they asked for
	// to be checked.
	if len(g.Unexercised) > 0 {
		return false
	}
	// And the same is true of a behaviour a check merely visits. A pairing the
	// assertion door emptied is a measurement of the repository too — the check
	// is on disk, its assertions are on disk, and neither of them names the
	// identifier the request spelled — so an acquittal of one refusal settles
	// nothing about it either.
	if len(g.Unasserted) > 0 {
		return false
	}
	// And a name nothing binds is the same kind of standing measurement, and the
	// hardest of the three to argue with: the reference is in one file, the
	// definition is in none, and no acquittal of a review's sentence puts a
	// binding in the tree. It empties the one way a measurement empties — a
	// later reading of the finished tree finds the name bound.
	if len(g.Unbound) > 0 {
		return false
	}
	return g.Pass || g.PolishClosed || g.Overturned
}

// Cited is the gate's citations however they were written down. A row recorded
// before the list existed carries only the joined line, and reading it as one
// citation is the honest reading of it: that is exactly what it was when it was
// written, and a ledger that treated it as nothing would hand an old job a
// fresh allowance on replay.
func (g DeliveryGate) Cited() []string {
	cited := make([]string, 0, len(g.Quotes))
	for _, quote := range g.Quotes {
		if quote = strings.TrimSpace(quote); quote != "" {
			cited = append(cited, quote)
		}
	}
	if len(cited) > 0 {
		return cited
	}
	if quote := strings.TrimSpace(g.Quote); quote != "" {
		return []string{quote}
	}
	return nil
}

// RecordDeliveryGate appends one gate result. It has no materialized view: the
// event is sparse, read by node id, and remains the source of truth on rebuild.
func (s *Store) RecordDeliveryGate(nodeID string, gate DeliveryGate) error {
	nodeID = strings.TrimSpace(nodeID)
	gate.Gap = strings.TrimSpace(gate.Gap)
	gate.Refused = strings.TrimSpace(gate.Refused)
	if nodeID == "" {
		return fmt.Errorf("record delivery gate: %w: empty node id", ErrInvalid)
	}
	// A GATE THAT DID NOT PASS MUST SAY WHY, AND THERE ARE TWO WAYS TO SAY IT.
	// One is the gap a judgement found. The other is the refusal that stood in
	// for the judgement — a gate that was never asked at all — and that row says
	// so in Refused with Unclosed set, which is the shape every reader here and
	// the exit code already spend.
	//
	// Demanding a gap of the second rejected it on every real run. The harness
	// stops spending on a job once nothing is changing, journals the unasked
	// gate as `{Refused: …, Unclosed: true}` (cmd/aforge/chat.go), and got back
	// `record delivery gate: invalid graph mutation: a failed gate must name the
	// gap` — so the row a battery reads to tell an unjudged delivery from a
	// checked one never landed, and the only trace was a note in the log.
	//
	// A row that says neither is still refused: a gate that recorded nothing at
	// all is a gate no autopsy can read.
	if !gate.Pass && gate.Gap == "" && !(gate.Refused != "" && gate.Unclosed) {
		return fmt.Errorf("record delivery gate: %w: a gate that did not pass must name the gap "+
			"it found, or the refusal that stood in for the judgement", ErrInvalid)
	}
	gate.Gap = bounded(gate.Gap, MaxDigestBytes)
	gate.Quote = bounded(strings.TrimSpace(gate.Quote), MaxDigestBytes)
	gate.Refused = bounded(gate.Refused, MaxDigestBytes)
	// Per citation, not on the list as a whole. The bound exists so one event
	// cannot carry an unbounded string, and a citation clipped to a share of a
	// budget it does not know the size of would be clipped mid-word — which is
	// a citation that no longer matches the words it was taken from.
	quotes := make([]string, 0, len(gate.Quotes))
	for _, quote := range gate.Quotes {
		if quote = bounded(strings.TrimSpace(quote), MaxDigestBytes); quote != "" {
			quotes = append(quotes, quote)
		}
	}
	gate.Quotes = quotes
	// The finding is bounded the same way, per entry and for the same reason: a
	// behaviour clipped to a share of a budget it does not know the size of is a
	// behaviour clipped mid-word.
	unexercised := make([]string, 0, len(gate.Unexercised))
	for _, point := range gate.Unexercised {
		if point = bounded(strings.TrimSpace(point), MaxDigestBytes); point != "" {
			unexercised = append(unexercised, point)
		}
	}
	gate.Unexercised = unexercised
	if len(gate.Unexercised) == 0 {
		gate.Unexercised = nil
	}
	unasserted := make([]string, 0, len(gate.Unasserted))
	for _, point := range gate.Unasserted {
		if point = bounded(strings.TrimSpace(point), MaxDigestBytes); point != "" {
			unasserted = append(unasserted, point)
		}
	}
	gate.Unasserted = unasserted
	if len(gate.Unasserted) == 0 {
		gate.Unasserted = nil
	}
	// And the same, per line, for the consumers finding: one entry is one
	// definition, and a line clipped to a share of a budget it does not know the
	// size of would be clipped mid-path.
	consumers := make([]string, 0, len(gate.Consumers))
	for _, line := range gate.Consumers {
		if line = bounded(strings.TrimSpace(line), MaxDigestBytes); line != "" {
			consumers = append(consumers, line)
		}
	}
	gate.Consumers = consumers
	if len(gate.Consumers) == 0 {
		gate.Consumers = nil
	}
	// And the same, per line, for the unbound-reference finding: one entry is
	// one name and the site it is read at, and a line clipped to a share of a
	// budget it does not know the size of would be clipped mid-path.
	unbound := make([]string, 0, len(gate.Unbound))
	for _, line := range gate.Unbound {
		if line = bounded(strings.TrimSpace(line), MaxDigestBytes); line != "" {
			unbound = append(unbound, line)
		}
	}
	gate.Unbound = unbound
	if len(gate.Unbound) == 0 {
		gate.Unbound = nil
	}
	if len(gate.Quotes) == 0 {
		// An empty list and a nil one are the same fact, and only one of them
		// round-trips through the journal as the value it was given.
		gate.Quotes = nil
	}

	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventDeliveryGate, gate); err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	return nil
}

// DeliveryGateFor returns the latest gate event for a node. More than one is
// legal because corrections in an append-only journal are later events.
func (s *Store) DeliveryGateFor(nodeID string) (DeliveryGate, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, nodeID, EventDeliveryGate).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryGate{}, false, nil
	}
	if err != nil {
		return DeliveryGate{}, false, fmt.Errorf("read delivery gate: %w", err)
	}
	var gate DeliveryGate
	if err := json.Unmarshal([]byte(payload), &gate); err != nil {
		return DeliveryGate{}, false, fmt.Errorf("read delivery gate: %w", err)
	}
	return gate, true, nil
}

// DeliveryGateLineage returns every gate recorded for a node and everything
// spliced beneath its id, oldest first — one job's whole run of judgements,
// including the repair rounds that continue it under "<id>-x<n>".
//
// It is an id-range read rather than a graph walk for the same reason the round
// counter is: the lineage IS an id namespace, and a reader that rebuilt it from
// parents and edges would own a second copy of the "-x" law.
func (s *Store) DeliveryGateLineage(baseID string) ([]DeliveryGate, error) {
	baseID = strings.TrimSpace(baseID)
	if baseID == "" {
		return nil, nil
	}
	// The lineage is the node itself plus its own split namespace, and nothing
	// else: a bare prefix range would also swallow "jobless" for "job", which
	// would let one job's ledger bound another's.
	namespace := baseID + SplitNamespace
	ceiling, ok := idPrefixCeiling(namespace)
	if !ok {
		return nil, fmt.Errorf("read delivery gate lineage %q: %w: prefix has no ordered ceiling", baseID, ErrInvalid)
	}
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE kind = ? AND (node_id = ? OR (node_id >= ? AND node_id < ?))
		ORDER BY seq`, EventDeliveryGate, baseID, namespace, ceiling)
	if err != nil {
		return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
	}
	defer rows.Close()
	gates := make([]DeliveryGate, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
		}
		var gate DeliveryGate
		if err := json.Unmarshal([]byte(payload), &gate); err != nil {
			return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
		}
		gates = append(gates, gate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
	}
	return gates, nil
}

// EventAcceptance is the acceptance checklist journaled against the piece of
// work it will be used to judge: the behaviours the person's own request states,
// read from the request before any work began.
//
// It is a first-class event beside the plan blob for the reason EventNodeBrief
// is: the checklist lives on plan.Spec, inside a document held in memory for the
// life of a run, and "what was this work actually asked for" is exactly the
// question an autopsy of a finished run needs answered from the run's own
// journal. It is also what the headless stream reads to say the checklist exists
// at all — a fail-safe that does not reach the person watching is decoration
// (docs/design/failsafe/FAILSAFE.md clause 3).
const EventAcceptance EventKind = "acceptance"

// AcceptancePoint is one behaviour the request states, and the words of the
// request it is a reading of.
//
// The store learns no more about a point than that, and deliberately: Quote is
// what the grounding rule weighs and Behaviour is what a person reads, and the
// rules that weigh them live where the gate lives. This is the journal's copy.
type AcceptancePoint struct {
	Behaviour string `json:"behaviour"`
	Quote     string `json:"quote"`
}

// Acceptance is the whole checklist for one piece of work.
type Acceptance struct {
	Points []AcceptancePoint `json:"points"`
}

// RecordAcceptance journals the checklist against the node whose delivery it
// governs. An empty checklist writes nothing: a request that states no checkable
// behaviour has no checklist, and an event saying so would be a row every reader
// has to learn to ignore.
func (s *Store) RecordAcceptance(nodeID string, acceptance Acceptance) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record acceptance: %w: empty node id", ErrInvalid)
	}
	points := make([]AcceptancePoint, 0, len(acceptance.Points))
	for _, point := range acceptance.Points {
		point.Behaviour = bounded(strings.TrimSpace(point.Behaviour), MaxDigestBytes)
		point.Quote = bounded(strings.TrimSpace(point.Quote), MaxDigestBytes)
		// Bounded per point rather than over the list, for the reason the gate's
		// citations are: a quotation clipped to a share of a budget it does not
		// know the size of is clipped mid-word, and a citation that no longer
		// matches the words it was taken from grounds against nothing.
		if point.Behaviour == "" || point.Quote == "" {
			continue
		}
		points = append(points, point)
	}
	if len(points) == 0 {
		return nil
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventAcceptance, Acceptance{Points: points}); err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	return nil
}

// AcceptanceFor returns the newest checklist journaled for a node. It reads the
// event directly, exactly as DeliveryGateFor does and for the same reason: the
// payload is sparse, looked up by id, and has no query anyone would run across
// it.
func (s *Store) AcceptanceFor(nodeID string) (Acceptance, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, nodeID, EventAcceptance).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Acceptance{}, false, nil
	}
	if err != nil {
		return Acceptance{}, false, fmt.Errorf("read acceptance: %w", err)
	}
	var acceptance Acceptance
	if err := json.Unmarshal([]byte(payload), &acceptance); err != nil {
		return Acceptance{}, false, fmt.Errorf("read acceptance: %w", err)
	}
	return acceptance, true, nil
}
