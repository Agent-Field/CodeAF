package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// A job that grows while it runs used to leave no single trace of having grown.
// An overrun replan spliced a subtree, a revision sentinel spliced a node, and
// afterwards the two were indistinguishable without reading intents node by
// node — so "why did this job end up with 41 nodes, and who asked for the last
// eleven?" had no answer, and neither did "what did a governor refuse".
//
// So every execution-time growth decision — admitted or refused — is journaled
// against the job root it grew, the same key the job's own nodes are minted
// under. It is diagnosis for the refusals and accounting for the admissions:
// the round counter that bounds growth is derived from these events, so the
// journal is the counter rather than a description of one.
//
// EventJobGrowth is declared here rather than in store.go's block for the same
// reason EventScaleGate is declared beside its writer: a kind whose payload one
// file understands is easier to keep honest next to that file.
const EventJobGrowth EventKind = "job_growth"

// JobGrowth is one decision by the growth governor.
//
// Reason names the path that asked — an overrun replan, a delivery gap, a
// revision sentinel — because the whole point of the journal is that those were
// indistinguishable afterwards. Lineage is the namespace the rounds are counted
// under: a leaf's own split lineage for a replan, the job root for growth that
// belongs to the job as a whole. Two siblings that each split once are two
// lineages with one round each, not one lineage with two.
//
// Refused carries the governor's own words when it said no, so a reader of the
// journal sees the same sentence the work's record got.
type JobGrowth struct {
	Reason  string `json:"reason"`
	Lineage string `json:"lineage,omitempty"`
	Adding  int    `json:"adding,omitempty"`
	Round   int    `json:"round"`
	Allowed bool   `json:"allowed"`
	Refused string `json:"refused,omitempty"`
	// Cause is the machine-readable half of Refused: which governor spoke.
	Cause string `json:"cause,omitempty"`

	// Produced is how many files the work this round grows FROM left behind
	// THAT THIS JOB IS ABOUT — the workspace's own before-and-after reading,
	// narrowed to the change's own focus, and never the worker's account of
	// itself. It is journaled because it is the only evidence the next round
	// can be weighed against: a lineage whose last two bodies of work both
	// changed nothing relevant is not making slow progress, it is at a
	// standstill, and no round after that buys anything.
	//
	// IT COUNTS WHAT MATTERS AND NOT WHAT MOVED. It used to be every path the
	// tree gained, which is a count a stuck model raises for free: ink s10
	// spent its whole 5400-second wall over eight exhaustions of one lineage
	// while the only thing any round wrote was `debug-grid.ts`,
	// `debug-grid10.ts`, `debug-yoga.ts` and eleven more of the same beside a
	// change that never moved. Every one of those is a file, so every round
	// looked productive to a detector asking "did anything change" — and the
	// standstill rule, which was right, never got a chance to speak.
	Produced int `json:"produced,omitempty"`

	// Scratch is the rest of what the round wrote: files outside the focus,
	// which are not progress and are not nothing either.
	//
	// It is a separate count rather than a subtraction because the two facts
	// answer different questions. Produced answers "did this round move the
	// work"; this answers "then what WAS it doing", and a person reading a
	// refusal is owed the second — a round that wrote fourteen files and moved
	// none of them is a specific, recognisable failure and the count is what
	// makes it recognisable.
	Scratch int `json:"scratch,omitempty"`

	// Moved names the files inside the focus that the round did change, bounded
	// the same way. It is what the run's closing line points at when it says
	// what the last real change was — a person told "nothing has moved for
	// three rounds" is owed the name of the last thing that did.
	Moved []string `json:"moved,omitempty"`

	// Wrote names those files, bounded. The count says a round wrote nothing
	// that mattered; this says WHAT it wrote instead, which is the whole of
	// what an autopsy of a fruitless round has to work with — and it was, in
	// s10, only recoverable by reading resume payloads out of the transcript.
	Wrote []string `json:"wrote,omitempty"`

	// Unexercised, Red and Standing are the job's own shortfall as its record
	// stood when this round was weighed: how many stated behaviours no check
	// exercises, how many checks the newest reading found red, and a digest of
	// the review finding that still stands.
	//
	// They are journaled because MOVING A FILE IS NOT THE ONLY WAY TO MOVE THE
	// WORK. A round that closed a regression, answered a standing finding, or
	// brought a behaviour under a check has made progress even where the focus
	// gained nothing, and a rule that could not see it would refuse the round
	// after the one that finally started working. Each is compared against the
	// row before it and only ever for a FALL — a shortfall reworded is not a
	// shortfall closed, which is the same distinction Remainder draws below.
	Unexercised int `json:"unexercised,omitempty"`
	Red         int `json:"red,omitempty"`
	// Lost is the symbol-level half of the same shortfall: public names the
	// finished tree no longer spells. See SurfaceReading.
	Lost     int    `json:"lost,omitempty"`
	Standing string `json:"standing,omitempty"`

	// Measured says somebody actually looked, so a Produced of zero reads as
	// "nothing was written" rather than as "nobody counted". A row written
	// before this field existed decodes false, which is the honest reading of
	// it: nothing counted anything.
	Measured bool `json:"measured,omitempty"`

	// CoveredDespite names the standing evidence that stopped the coverage
	// question from refusing this round: the kinds the WORLD still says are
	// wrong with the job, when the plan-level reading said everything it is
	// judged on was already covered.
	//
	// It is journaled because the two readings DISAGREED and the disagreement is
	// the measurement. Coverage is a claim about the plan — acceptance points
	// mapped onto work that has landed — and it can be perfectly true of a job
	// whose checks are red and whose stated behaviours nothing exercises,
	// because none of those is a point on the plan. A row carrying this is a row
	// where a model said "nothing left" over a tree that was demonstrably not
	// finished, and an autopsy asking how often that happens has nothing else to
	// read. Empty is every round where the two agreed or the question was never
	// put.
	CoveredDespite []string `json:"covered_despite,omitempty"`

	// Finding is the REVIEW FINDING this round was bought for: what kind of
	// finding it is and which names it cites, as the structured record holds
	// them rather than as the review's paragraph spells them.
	//
	// It is journaled because A FINDING HAS ITS OWN FIXED POINT and nothing
	// else in this row can see it. Remainder below is a digest of the review's
	// PROSE, so a finding restated in fresh words reads as new work; Produced
	// is a fact about the tree, so a round that rewrote half a repository and
	// left the finding exactly where it found it reads as progress. happy-dom
	// v4-flash s13 is the measured case: four gate events carrying one
	// identical finding — the same four check names, word for word — bought
	// four rounds, every one of them journaled as productive, and the finding
	// they were bought FOR never moved at all (2026-08-29, bench/deepswe).
	//
	// Empty is every round nobody bought for a finding: an overrun, a
	// cooperative split, a resumption. Those keep exactly the governors they
	// had.
	Finding GrowthFinding `json:"finding,omitempty"`

	// BoughtFor is every NAME that finding stood on when this round was bought:
	// the behaviours no check exercises, the checks the run left red, the public
	// names the change deleted. A round bought for a set is a round bought for
	// every name in it.
	//
	// It is journaled as names and not as a count because THE FINDING IS EACH
	// NAME AND NOT THE SET. A gate cites whichever subset of the request it
	// happened to weigh, and the subset rotates: ofetch v4-flash s15 raised four
	// unexercised findings whose sets digested to four different values while
	// `Count a circuit failure for body-read/stream-consumption errors` sat in
	// every one of them, unclosed, for four rounds and the whole run. Nothing
	// that compares sets can see that; the names can.
	BoughtFor []string `json:"bought_for,omitempty"`

	// Spent is the names that had already had their two rounds when this
	// decision was taken — the ones that may not buy another. A refused round
	// carries the whole set here, which is what makes the refusal legible: the
	// person is told which things were worked on twice and still stand.
	Spent []string `json:"spent,omitempty"`

	// Remainder is a digest of the work this round was planned to finish. Two
	// consecutive rounds handed the same remainder are a fixed point: the round
	// that just ran was aimed at exactly this and did not move it. The digest
	// rather than the text because the text is unbounded and this is only ever
	// compared for equality.
	Remainder string `json:"remainder,omitempty"`
}

// RecordJobGrowth journals one growth decision against a job root. Losing it
// costs the round counter its memory and diagnosis its record, so callers treat
// a failure as a note — but they do treat it: an unrecorded admission is a
// round nobody spent.
func (s *Store) RecordJobGrowth(jobRoot string, growth JobGrowth) error {
	jobRoot = strings.TrimSpace(jobRoot)
	if jobRoot == "" {
		return fmt.Errorf("record job growth: %w: job root is required", ErrInvalid)
	}
	if strings.TrimSpace(growth.Reason) == "" {
		return fmt.Errorf("record job growth: %w: reason is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record job growth: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, jobRoot, EventJobGrowth, growth); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record job growth: %w", err)
	}
	return nil
}

// JobGrowthRound is one journaled decision and when it was journaled.
//
// The time is the journal's own and not a field anybody wrote, and it is
// exposed because A JOB'S PACE IS READ FROM ITS OWN ROUNDS. How long a round of
// this job takes is the gap between two of these, measured on the job that is
// running rather than assumed from a constant, and it is what answers "can the
// wall still hold another one".
type JobGrowthRound struct {
	JobGrowth
	At time.Time
}

// JobGrowths returns every growth decision journaled for a job root, oldest
// first.
func (s *Store) JobGrowths(jobRoot string) ([]JobGrowth, error) {
	rounds, err := s.JobGrowthRounds(jobRoot)
	if err != nil {
		return nil, err
	}
	growths := make([]JobGrowth, 0, len(rounds))
	for _, round := range rounds {
		growths = append(growths, round.JobGrowth)
	}
	return growths, nil
}

// JobGrowthRounds is the same journal with each decision's timestamp kept,
// oldest first. It reads the events directly, as ScaleGateFor does: the payload
// is sparse, looked up by one id, and has no query anyone would run across it.
func (s *Store) JobGrowthRounds(jobRoot string) ([]JobGrowthRound, error) {
	rows, err := s.db.Query(`
		SELECT payload, ts FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq`, strings.TrimSpace(jobRoot), EventJobGrowth)
	if err != nil {
		return nil, fmt.Errorf("read job growth: %w", err)
	}
	defer rows.Close()
	rounds := make([]JobGrowthRound, 0)
	for rows.Next() {
		var payload, timestamp string
		if err := rows.Scan(&payload, &timestamp); err != nil {
			return nil, fmt.Errorf("read job growth: %w", err)
		}
		var growth JobGrowth
		if err := json.Unmarshal([]byte(payload), &growth); err != nil {
			return nil, fmt.Errorf("read job growth: %w", err)
		}
		// A timestamp that will not parse leaves the zero time rather than
		// failing the read: the pace this feeds is an optimisation on top of a
		// journal whose whole point is the counts, and a clock nobody can read
		// is a pace nobody derives, never a growth decision nobody can make.
		at, _ := parseTime(timestamp)
		rounds = append(rounds, JobGrowthRound{JobGrowth: growth, At: at})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read job growth: %w", err)
	}
	return rounds, nil
}

// GrowthFinding is one review finding as the two things that decide whether a
// later round is being bought for the SAME finding: what kind of finding it is,
// and which names it cites.
//
// IT IS THE STRUCTURED FINDING AND NEVER THE PROSE. A gate records its
// conclusions as lists — the behaviours no check exercises, the checks the run
// wrote and left red, the definitions it reshaped, the spans it convicted on —
// and those lists are stable across a rewording of the paragraph that carries
// them. Comparing paragraphs answers "did the reviewer type the same sentence
// twice", which is a question about a model's phrasing; comparing these answers
// "is the same thing still missing", which is a question about the work.
//
// Names is a digest rather than the list because it is only ever compared for
// equality and a list of names is unbounded. The names themselves are on the
// row, in BoughtFor, because THE ROUND IS BOUGHT NAME BY NAME: the digest says
// two gates raised the same set, and the set is exactly what a rotating citation
// never repeats.
type GrowthFinding struct {
	Kind  string `json:"kind,omitempty"`
	Names string `json:"names,omitempty"`
}

// Empty reports that this round was not bought for any finding the record can
// name — which is not the same as a finding that cites nothing.
func (f GrowthFinding) Empty() bool { return strings.TrimSpace(f.Kind) == "" }

// Same reports that two rounds were bought for the same finding: same kind,
// same names. An empty finding is never the same as anything, INCLUDING ANOTHER
// EMPTY ONE — "nobody recorded what this round was for" is not evidence that
// two rounds were for one thing, and reading it as such would refuse a path
// that never named a finding at all.
func (f GrowthFinding) Same(other GrowthFinding) bool {
	return !f.Empty() && !other.Empty() && f.Kind == other.Kind && f.Names == other.Names
}
