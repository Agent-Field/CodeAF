package lane

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
)

// ── THE LEDGER: WHAT THIS PROCESS HAS MEASURED ──────────────────────────────
//
// One [Belief] per lane per model — two scalar Kalman filters and a Beta — fed
// by four kinds of evidence and by nothing else: a sheet row, a finished
// stream, a probe, and an answer the caller could not use. The sheet primes it,
// the stream corrects it, the probe corrects it sharply, and the file under
// [StorePath] carries it to the next process.
//
// ── THE UNITS, STATED ONCE ──────────────────────────────────────────────────
//
// TTFT IS BELIEVED IN MILLISECONDS AND RATE IN TOKENS PER SECOND, in the log
// domain, which is what [Belief] and [Row] both already say and what the router
// publishes. Nothing here converts, so no seam has to remember that it should.
// The one place seconds appear is [PerceivedSeconds], whose caller divides by a
// thousand at the point it is used and nowhere else.
//
// ── THE LEDGER HAS NO CLOCK ─────────────────────────────────────────────────
//
// Every moment it knows arrives on a [Sighting] or an [Outcome], and
// [Ledger.Belief] answers with the belief AS IT WAS STORED — not aged to now,
// because "now" is not something this type is entitled to an opinion about.
// The caller has [Request.Now] and ages what it reads with
// [Posterior.Predict], which is the same arithmetic whether the belief got old
// in memory or old in a file: one implementation, asked at the one moment that
// matters.
//
// [Belief.At] is therefore THE MOMENT OF THE LAST TIMED SIGHTING, and it is
// zero for a lane only the sheet has ever spoken about. A zero At is "there is
// nothing to age", never "aged since 1970", and a caller that ages a belief
// must ask.
//
// ── WHY A SIGHTING WITH NO MOMENT IS DROPPED ────────────────────────────────
//
// A sighting whose At is zero cannot be placed in time, and folding it in would
// stamp the belief with a moment that never happened — after which the next
// real sighting would age it by fifty-six years and the filter's variance would
// go to infinity. So it is refused, for the same reason an anonymous sighting
// is: an observation this ledger cannot attribute is one it must not keep.

// HalfLife is how long a belief takes to lose half its information when nothing
// new is heard about the lane. It is stated here because the ledger ages a
// belief on the way in and the chooser ages it on the way out, and a half-life
// that appeared in two places would be two different half-lives by Christmas.
const HalfLife = 10 * time.Minute

// QualityHalfLife is how long a belief about ANSWERS takes to lose half its
// information. It is six times [HalfLife] because the two things this package
// believes about a lane change on two different scales, and forgetting them at
// one rate gets one of them wrong.
//
// How quick a lane is right now is a fact about load: it moves minute to
// minute, and a reading from an hour ago is worth almost nothing. Whether a
// lane returns a usable answer at all is a fact about the deployment behind it
// — its quantisation, its context window, its truncation — and that holds for
// hours. Forgetting the second at the speed of the first makes the quality gate
// inert rather than lenient: a lane whose requests take five minutes each can
// never accumulate evidence faster than a ten-minute half-life burns it, so its
// mass sits at the prior's, the credible bound sits at one, and a lane that
// refuses one answer in six is never dropped. The simulator found exactly that
// — see "Part III" in the design, C1 — and the fix is not a wider gate but a
// memory long enough to hold the evidence the gate is asking for.
const QualityHalfLife = 6 * HalfLife

// SheetWeight is the k a caller passes to [Ledger.Prime]: the sheet's
// pseudo-observation is worth a quarter of one of our own sightings, because it
// is a half-hour aggregate over everybody's prompts from everywhere and ours is
// about our prompt from here. Both are evidence; neither is truth.
const SheetWeight = 4.0

// ratedFloor is the shortest answer worth rating. Below it the generation
// window is mostly the handshake and the first token's own arrival, so its
// tokens-per-second is a measurement of a warm-up rather than of a lane.
const ratedFloor = 32

// defaultSpread is the σ of a log-normal used when the sheet's own percentiles
// cannot give one — a p90 that is not above the p50, or no sheet at all. It is
// about a 1.6× spread between the median and the ninetieth percentile, which is
// narrower than a bad lane and wider than a good one, so a belief built on it
// is honestly uncertain rather than confidently wrong either way.
const defaultSpread = 0.6

// z90 is the standard normal's ninetieth percentile, which is how a p50 and a
// p90 become a location and a scale.
const z90 = 1.2816

// ledger is the live belief set.
//
// It is the only mutable state in the package and one mutex covers all of it:
// the operations are microseconds of arithmetic over a map of a few hundred
// entries, and a lock per lane would buy nothing but a way to deadlock.
type ledger struct {
	mu      sync.Mutex
	beliefs map[ID]Belief
	// priors is the sheet's own log-domain variance per lane, kept because it
	// is two things at once: the observation noise a real sighting is folded in
	// with, and THE FLOOR AGEING MAY NOT WIDEN PAST. A belief that has been
	// left alone for a day should decay to the public sheet's certainty and
	// stop there — worthless, never worse than what anybody can look up.
	priors map[ID]spread
	// keeper is where beliefs sleep, nil when nothing is attached. loaded is
	// whether yesterday's have been read back yet: that read is one file, on
	// the first question or the first sighting, so that importing this package
	// costs a process nothing.
	keeper Store
	loaded bool
	// pages is the sheet already in memory, nil when nothing is attached.
	//
	// IT IS READ AND NEVER FETCHED. [Sheet.Rows] is a map lookup by contract
	// and this ledger calls nothing else on it, so a lane the sheet knows can
	// be dressed with its facts the moment it is first SEEN — which is what
	// keeps a belief that arrived as a sighting from sitting in the file with
	// every fact blank until the next beat happens to prime it. The registry
	// introduces the two, for the same reason it introduces the store: a ledger
	// that went looking for a sheet by itself would be the reach-around the
	// registry exists to prevent.
	pages Sheet
	// wait, rate and think are the three hierarchies this ledger answers
	// [Hierarchy] from, and judged is the quality evidence above a pair. They
	// are FED BY THE SAME OBSERVATIONS as the flat beliefs above, in the same
	// call, because two accounts of one lane updated separately would disagree
	// the first time one of them was fixed.
	wait   chains
	rate   chains
	think  chains
	judged tallies
	// shifted names the pairs whose leaf a change point has just reset. It is
	// read once and cleared, which is what makes it a piece of news rather than
	// a state somebody has to remember to acknowledge.
	shifted map[ID]bool
	// carried is what this ledger had measured BEFORE it was given anywhere to
	// write. Those observations are in no journal, so they are the one thing a
	// rebuild cannot recover by replaying, and they are re-adopted every time.
	carried []Belief
	// appended is how many observations have gone into the journal since the
	// last compaction, and settled whether this process has compacted at all.
	// skipped counts the journal lines that would not parse.
	appended int
	settled  bool
	skipped  int
	// owed is how a door tells the writer a compaction is worth doing. It holds
	// ONE slot and the send never waits for it: a full channel already says
	// "there is a compaction to do", and the writer reads the journal rather
	// than any queued delta, so a second signal would say nothing the first
	// does not (issue #264).
	owed chan struct{}
	// deferred counts the compactions that could not take the file's lock. A
	// write that was quietly dropped is the same freeze with the symptom
	// removed, so it is counted here and said once in the call log.
	deferred int
}

// spread is one lane's prior variance for each filter, as the sheet published
// it. It is written down with the beliefs because it is not decoration: it is
// the observation noise a real sighting is folded in with AND the floor ageing
// may not widen past, so a process that reloaded the beliefs without it would
// weigh its next measurement against a default it never measured.
type spread struct {
	ID   ID      `json:"id"`
	TTFT float64 `json:"ttft,omitempty"`
	Rate float64 `json:"rate,omitempty"`
}

// newLedger builds the live ledger. It is called from the registry and nowhere
// else.
func newLedger() *ledger {
	fresh := &ledger{
		beliefs: map[ID]Belief{},
		priors:  map[ID]spread{},
		shifted: map[ID]bool{},
		owed:    make(chan struct{}, 1),
	}
	fresh.pace()
	return fresh
}

// pace asserts where each chain's world level starts. The three figures are
// properties of the measured world rather than of anything this process learns,
// so they are re-asserted after every read rather than trusted to a file that
// an older build may have written.
func (l *ledger) pace() {
	l.wait.Pace, l.rate.Pace, l.think.Pace = waitPace, ratePace, thinkPace
}

// keepIn attaches the store this ledger writes through. Yesterday's beliefs are
// not read here — that is done lazily, on first use, so that a process which
// merely imports this package never touches the disk.
func (l *ledger) keepIn(keeper Store) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keeper, l.loaded = keeper, false
	l.carried = l.held()
}

// readFrom attaches the sheet this ledger dresses a newly seen lane from. It
// fetches nothing; see the field's own note.
func (l *ledger) readFrom(pages Sheet) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pages = pages
}

// restore reads what is written down, once. It is called with the lock held.
func (l *ledger) restore() {
	if l.loaded {
		return
	}
	l.loaded = true
	l.readBack()
}

// reload reads the file and the journal again, however many times it is called,
// and compacts what it found.
//
// IT IS THE OTHER HALF OF THE MULTI-PROCESS CONTRACT (store.go, "state, and the
// observations since"). Another aforge may have been running the whole time
// this one was, learning about models this session has never mentioned; its
// observations are in the journal and this is where they are folded in. The
// beat calls it, which is the one moment a session is already doing a round of
// bookkeeping and is nowhere near a send.
//
// NOTHING ON THE SEND PATH CALLS IT. A choice reads memory.
func (l *ledger) reload() {
	l.mu.Lock()
	l.loaded = true
	deep, ok := l.keeper.(deepStore)
	if !ok {
		l.readBack()
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()
	// AND THE COMPACTION RUNS WITH THE MUTEX FREE. It takes the file's lock,
	// and a lock and this mutex held at once is the thirty minutes of issue
	// #264 — the beat is nowhere near a send, but the chooser that would have
	// queued behind it is on every one.
	l.compact(deep)
}

// readBack builds memory from what is written down. It is called with the lock
// held.
//
// A store that keeps only beliefs gets the old contract, unchanged: load the
// set and fold it in by [fresher], so a bench or a test that answers the plain
// [Store] seam behaves exactly as it always did. A store that keeps a whole
// hierarchy gets the new one, where memory is a pure function of the file and
// the journal beside it.
func (l *ledger) readBack() {
	if l.keeper == nil {
		return
	}
	deep, ok := l.keeper.(deepStore)
	if !ok {
		if kept, err := l.keeper.Load(); err == nil {
			l.adopt(kept)
		}
		return
	}
	held, records, skipped := deep.state()
	l.rebuild(held, records, skipped)
}

// rebuild makes memory a pure function of what is written down: the compacted
// state, every observation journalled since it was written, in time order, and
// whatever this process measured before it had anywhere to write.
//
// REPLACING RATHER THAN MERGING IS WHAT KEEPS AN OBSERVATION FROM BEING FOLDED
// TWICE. Everything this process has learned since the store was attached is
// already in the journal, so a replay restores it exactly; folding the journal
// on top of a memory that already held it would leave the filter more certain
// than the evidence, once per beat, for ever.
func (l *ledger) rebuild(held storeState, records []record, skipped int) {
	l.beliefs = map[ID]Belief{}
	l.priors = map[ID]spread{}
	for _, prior := range held.Priors {
		l.priors[prior.ID] = prior
	}
	l.wait, l.rate, l.think, l.judged = held.Wait, held.Rate, held.Think, held.Judged
	l.pace()
	l.skipped += skipped
	l.adopt(held.Beliefs)
	l.migrate(held)
	for _, entry := range records {
		l.replay(entry)
	}
	l.adopt(l.carried)
}

// migrate folds a version 1 file's beliefs into the hierarchy.
//
// A flat file says what a PAIR was believed to be and nothing about how much of
// that was the provider and how much the model, so each belief enters the chain
// the way any observation does — at exactly the certainty it was written with —
// and the four levels share it out among themselves. Nothing is thrown away and
// nothing is claimed that was not measured.
//
// Pasting the belief into the leaf instead would break the sum the moment a
// parent learned an offset: ln T is μ + a + b + e, and a leaf holding the whole
// of ln T is a leaf that would then be counted twice.
func (l *ledger) migrate(held storeState) {
	if held.Version >= stateVersion || len(held.Beliefs) == 0 {
		return
	}
	inOrder := append([]Belief(nil), held.Beliefs...)
	sort.SliceStable(inOrder, func(i, j int) bool { return inOrder[i].At.Before(inOrder[j].At) })
	for _, belief := range inOrder {
		if belief.At.IsZero() {
			continue
		}
		of := pairOf(belief.ID)
		if belief.TTFT.Known() {
			l.wait.fold(of, everyLevel, belief.TTFT.X, belief.TTFT.P, belief.At)
		}
		if belief.Rate.Known() {
			l.rate.fold(of, everyLevel, belief.Rate.X, belief.Rate.P, belief.At)
		}
	}
}

// replay folds one journalled observation back in, without journalling it
// again. It is the same arithmetic the live doors run and deliberately not a
// second copy of it.
func (l *ledger) replay(entry record) {
	switch {
	case entry.Sight != nil:
		l.see(*entry.Sight)
	case entry.Out != nil:
		l.weigh(*entry.Out)
	case entry.Row != nil:
		l.primeRow(*entry.Row, entry.Weight)
	case entry.Think != nil:
		l.deliberated(*entry.Think)
	}
}

// keep writes one observation down. It is called with the lock held.
//
// A journal line is one small append. The whole set is COMPACTED, and this
// function never does it: it says that one is owed and returns.
//
// ── WHY NOT HERE (issue #264) ───────────────────────────────────────────────
//
// This used to compact inline, on the first record of a process and on every
// [journalLimit]th after — with this ledger's mutex held, which the chooser
// reads on every encode, over an exclusive flock, which takes no deadline and
// which Go's scheduler cannot interrupt. On 2026-09-01 that arrangement sent
// nothing at all for 29m49s: another process had the file, and this one was
// inside the lock with the mutex every model call needs.
//
// So the compaction is somebody else's work now ([ledger.Persist]), and the
// only thing on this path is an O_APPEND write and a signal that cannot block.
// DEFERRING IS SAFE BY CONSTRUCTION: the observation is in the journal before
// the signal is sent, so a compaction that never happens costs a longer replay
// and not a belief.
func (l *ledger) keep(entry record) {
	if l.keeper == nil {
		return
	}
	deep, ok := l.keeper.(deepStore)
	if !ok {
		l.save()
		return
	}
	if err := deep.log(entry); err != nil {
		return
	}
	l.appended++
	if !l.settled || l.appended >= journalLimit {
		l.owe()
	}
}

// owe says a compaction is worth doing, without doing it and without waiting to
// say so. It is called with the lock held.
func (l *ledger) owe() {
	select {
	case l.owed <- struct{}{}:
	default:
	}
}

// compact replays the journal under the exclusive lock, writes the state it
// folded into, and empties the journal.
//
// IT IS CALLED WITHOUT THE LEDGER'S MUTEX AND TAKES IT ONLY AROUND THE
// ARITHMETIC. That order is the whole of issue #264: asking for the file's lock
// and holding this mutex at once is what turned one stuck lock into a silent
// process. Here the asking, the reading and the rename all happen with the
// mutex free, and the fold below holds it for the microseconds a replay costs.
func (l *ledger) compact(deep deepStore) error {
	err := deep.hold(func(held storeState, records []record, skipped int) (storeState, bool) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.rebuild(held, records, skipped)
		l.settled, l.appended = true, 0
		return l.snapshot(), true
	})
	if err != nil {
		l.deferWrite(err)
	}
	return err
}

// deferredNote is what a compaction that could not be done says. It is one
// sentence because it goes in the call log, which is the file somebody reading
// an incident already has open — and a run of thirty silent minutes with
// nothing written anywhere is the state this whole issue is about.
const deferredNote = "the belief file could not be compacted"

// deferWrite counts a compaction that did not happen and says so, once each.
func (l *ledger) deferWrite(reason error) {
	l.mu.Lock()
	l.deferred++
	count := l.deferred
	l.mu.Unlock()
	calllog.Append(calllog.Record{
		Tag:  "lanes",
		Note: fmt.Sprintf("%s: %v (%d deferred so far; every observation is still in the journal)", deferredNote, reason, count),
	})
}

// Deferred is how many compactions this ledger could not do. It is the counted
// half of what [deferWrite] says in words, and it is what a test asserts on:
// a deferral that were merely silent would be the original defect with the
// symptom removed rather than the cause.
func (l *ledger) Deferred() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.deferred
}

// ── THE ONE WRITER ──────────────────────────────────────────────────────────

// compactRetry is how long the writer waits before asking again for a lock it
// has just been refused, doubling to compactRetryMax. It backs off because a
// file held for half an hour would otherwise be a line a second in the log, and
// because a compaction gets no more urgent for being asked for twice.
const (
	compactRetry    = time.Second
	compactRetryMax = time.Minute
)

// Persist compacts the belief file whenever one is owed, and never anywhere
// else. It returns when ctx is done.
//
// IT STARTS NOTHING, for the reason [Beat] starts nothing: who is writing, and
// when, is a question with an answer in the session's own code rather than in a
// package nobody thought was running (sheet.go). A process that never runs it
// keeps every observation in the journal and pays a longer replay next time; it
// loses nothing.
func (l *ledger) Persist(ctx context.Context) {
	backoff := compactRetry
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.owed:
		}
		if l.write() == nil {
			backoff = compactRetry
			continue
		}
		// A LOCK THIS PROCESS COULD NOT TAKE IS A COMPACTION OWED, NOT ONE
		// LOST. It is asked for again after a pause; the journal it would have
		// emptied keeps growing meanwhile, and that longer replay is the entire
		// price of somebody else holding the file.
		l.owe()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > compactRetryMax {
			backoff = compactRetryMax
		}
	}
}

// Flush does the compaction that is owed, once, and returns. It is what a
// process on its way out calls so that a run which learned something leaves a
// state file behind it rather than a journal for the next one to replay.
//
// It takes no deadline because it cannot need one: the lock underneath is asked
// for and never waited on (store.go).
func (l *ledger) Flush() error {
	select {
	case <-l.owed:
	default:
		return nil
	}
	return l.write()
}

// write is one compaction, on whichever goroutine owns the writing.
func (l *ledger) write() error {
	l.mu.Lock()
	deep, ok := l.keeper.(deepStore)
	l.mu.Unlock()
	if !ok {
		return nil
	}
	return l.compact(deep)
}

// Persist runs the process's belief writer until ctx is done. The session
// starts it beside the beat, and for the same reason that one is started there.
func Persist(ctx context.Context) {
	if writer, ok := Default().Ledger().(interface{ Persist(context.Context) }); ok {
		writer.Persist(ctx)
	}
}

// Flush writes down what the writer has not reached yet. It is called on the
// way out of a process, beside the beat's own stop.
func Flush() {
	if writer, ok := Default().Ledger().(interface{ Flush() error }); ok {
		_ = writer.Flush()
	}
}

// snapshot is everything this ledger would have written down.
func (l *ledger) snapshot() storeState {
	return storeState{
		Beliefs: l.held(),
		Priors:  l.spreads(),
		Wait:    l.wait,
		Rate:    l.rate,
		Think:   l.think,
		Judged:  l.judged,
	}
}

// spreads is the sheet's own prior variances as a sorted slice, in the same
// spirit as [ledger.held]: two processes writing the same set write the same
// bytes.
func (l *ledger) spreads() []spread {
	all := make([]spread, 0, len(l.priors))
	for _, prior := range l.priors {
		all = append(all, prior)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID.String() < all[j].ID.String() })
	return all
}

// held is the belief set as a sorted slice, so that two processes writing the
// same set write the same bytes.
func (l *ledger) held() []Belief {
	all := make([]Belief, 0, len(l.beliefs))
	for _, belief := range l.beliefs {
		all = append(all, belief)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].ID.Model != all[j].ID.Model {
			return all[i].ID.Model < all[j].ID.Model
		}
		return all[i].ID.Lane < all[j].ID.Lane
	})
	return all
}

// adopt folds a set of beliefs into memory by [fresher]. It is called with the
// lock held.
func (l *ledger) adopt(beliefs []Belief) {
	for _, belief := range beliefs {
		if belief.ID.Zero() {
			continue
		}
		held, have := l.beliefs[belief.ID]
		if !have {
			l.beliefs[belief.ID] = belief
			continue
		}
		l.beliefs[belief.ID] = fresher(held, belief)
	}
}

// save writes the whole set through the store. It is the fallback for a store
// that keeps beliefs and nothing else, and it is called with the lock held.
//
// THE MERGE IS THE SAVE. A store that can read-merge-write hands the merged set
// straight back ([store.saveMerging]), so the same lock that made the write
// safe also tells this process what the other one wrote.
func (l *ledger) save() {
	if l.keeper == nil {
		return
	}
	all := l.held()
	if merging, ok := l.keeper.(interface {
		saveMerging([]Belief) ([]Belief, error)
	}); ok {
		merged, err := merging.saveMerging(all)
		if err == nil {
			l.adopt(merged)
		}
		return
	}
	_ = l.keeper.Save(all)
}

// ── THE PRIOR ───────────────────────────────────────────────────────────────

// Prime folds one sheet row in as a pseudo-observation worth 1/k of a sighting.
//
// THE FACTS ARE TAKEN EVEN WHEN THE TIMING IS NOT. A row with no percentiles
// still says whether the lane honours a tool call and what it charges, and
// those are what the gate runs on: a lane the sheet published no timing for is
// a lane we cannot score, not a lane we cannot judge.
//
// ON A LANE NOBODY HAS MEASURED the prior is adopted outright — X at the log of
// the median, P at the sheet's own variance — rather than folded in at 1/k of
// its weight. k says how much less the public number weighs THAN OURS, and
// against no measurement at all there is nothing for it to weigh against.
func (l *ledger) Prime(row Row, k float64) {
	if row.ID.Zero() {
		return
	}
	// A TIER IS NOT A DEPLOYMENT — see [BareModel]. Every door of this ledger
	// files a belief under the bare id, so that `model:high` and `model` are
	// one set of machines rather than two ledgers, one of which is always empty.
	row.ID = row.ID.bare()
	if k < 1 {
		k = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	l.primeRow(row, k)
	l.keep(record{At: row.At, Row: &row, Weight: k})
}

// primeRow is [ledger.Prime]'s arithmetic without the door. It is called with
// the lock held, by the door and by a replay of the journal, so that a belief
// rebuilt from the file is the belief that was written.
func (l *ledger) primeRow(row Row, k float64) {
	belief := l.beliefs[row.ID]
	belief.ID = row.ID
	belief.Facts = row.Facts

	if row.Known() {
		ttft := fit(row.TTFTp50, row.TTFTp90)
		rate := fit(row.Ratep50, row.Ratep90)
		prior := l.priors[row.ID]
		l.priors[row.ID] = spread{ID: row.ID, TTFT: ttft.P, Rate: rate.P}
		// AGE FIRST, FOLD SECOND — the same order [Ledger.Note] keeps and for
		// the same reason. A reading taken now weighed against the confidence a
		// belief had ten minutes ago is a public number that cannot move a stale
		// opinion, which is exactly the lane this process has stopped sending
		// to. See [Row.At]; a row with no moment ages nothing, which is what
		// every caller that primes from a fixture wants.
		if !row.At.IsZero() && !belief.At.IsZero() {
			belief.TTFT = age(belief.TTFT, row.At.Sub(belief.At), prior.TTFT)
			belief.Rate = age(belief.Rate, row.At.Sub(belief.At), prior.Rate)
		}
		belief.TTFT = prime(belief.TTFT, ttft, k)
		belief.Rate = prime(belief.Rate, rate, k)
		// AND THE SAME ROW REACHES THE HIERARCHY, at the same weight and into
		// b[model] AND e[model, lane] ONLY. A sheet is published per model, so
		// folding it into μ or a[lane] would let one refresh move every belief
		// this process holds — including its opinion of providers the sheet was
		// not about. A row with no moment on it reaches neither: a component
		// stamped with a time that never happened is a component that can never
		// be aged. See [Row.At].
		if !row.At.IsZero() {
			l.wait.fold(modelOf(row.ID), publishedLevels, ttft.X, ttft.P*k, row.At)
			l.rate.fold(modelOf(row.ID), publishedLevels, rate.X, rate.P*k, row.At)
		}
	}
	// The quality prior is set once and never re-asserted: a lane that has
	// spent the afternoon returning tool calls the decoder refused must not be
	// handed its optimism back every five minutes by the beat.
	if !belief.Quality.Known() {
		belief.Quality = l.judged.start(row.ID, qualityPrior(row.Facts.Quant))
	}
	l.beliefs[row.ID] = belief
}

// dress fills in what the sheet already knows about a lane this process has
// merely SEEN. It is called with the lock held, and it does nothing to a belief
// that has been primed.
//
// ── WHY A SIGHTING IS NOT ENOUGH TO FILE A BELIEF ON ────────────────────────
//
// An answer says which machine wrote it and how quickly. It says nothing about
// whether that machine honours a tool call, how long an answer it will write,
// what it charges or what precision it serves at — and a belief with a sighting
// on it and no facts is not a lane with no facts, it is a lane nobody looked
// up. Filed that way it reaches the gate as a machine that refuses tool calls
// and cannot write a hundred tokens, which is a lane the router will never use
// again on evidence nobody produced.
//
// The sheet is usually sitting in memory when the sighting lands: it was
// fetched on the beat, it is a map lookup, and it has the row. So the row is
// taken HERE, at the moment the lane is first seen, rather than being waited
// for. A lane the sheet does not know is left alone — an honest blank, which
// [Facts.Known] and the gate both read as "nobody said" (frontier.go).
func (l *ledger) dress(belief *Belief) {
	if belief.Facts.Known() || l.pages == nil {
		return
	}
	row, found := l.rowFor(belief.ID)
	if !found {
		return
	}
	belief.Facts = row.Facts
	if row.Known() {
		ttft, rate := fit(row.TTFTp50, row.TTFTp90), fit(row.Ratep50, row.Ratep90)
		l.priors[belief.ID] = spread{ID: belief.ID, TTFT: ttft.P, Rate: rate.P}
		// THE POSTERIORS ARE ADOPTED AND NEVER FOLDED IN, for the reason
		// [Ledger.Prime] adopts them on a lane nobody has measured: k says how
		// much less the public number weighs THAN OURS, and there is nothing
		// here for it to weigh against. A filter this process has already moved
		// is left exactly where it is.
		if !belief.TTFT.Known() {
			belief.TTFT = ttft
		}
		if !belief.Rate.Known() {
			belief.Rate = rate
		}
	}
	if !belief.Quality.Known() {
		belief.Quality = l.judged.start(belief.ID, qualityPrior(row.Facts.Quant))
	}
}

// rowFor is the sheet's own line for one lane, false when the sheet has none.
//
// The lane is matched case-insensitively because the two spellings arrive from
// two places on the wire — a chunk's `provider` and a sheet row's
// `provider_name` — and a belief that missed its own row over a capital letter
// would be the bug this function exists to close, wearing a different hat.
func (l *ledger) rowFor(id ID) (Row, bool) {
	for _, row := range l.pages.Rows(id.Model) {
		if strings.EqualFold(row.ID.Lane, id.Lane) {
			return row, true
		}
	}
	return Row{}, false
}

// fit turns a median and a ninetieth percentile into a log-normal: the median
// is the location and the distance to the p90 is the scale. A p90 that is not
// above the p50 is a sheet saying nothing about spread, and [defaultSpread]
// stands in rather than a zero variance, which would be a claim of certainty
// nobody made.
func fit(p50, p90 float64) Posterior {
	if p50 <= 0 {
		return Posterior{}
	}
	sigma := defaultSpread
	if p90 > p50 {
		sigma = (math.Log(p90) - math.Log(p50)) / z90
	}
	return Posterior{X: math.Log(p50), P: sigma * sigma}
}

// prime folds a prior into a belief: adopted outright when there is no belief
// yet, and otherwise folded in at 1/k of a sighting's weight.
func prime(belief, prior Posterior, k float64) Posterior {
	if !prior.Known() {
		return belief
	}
	if !belief.Known() {
		return prior
	}
	return belief.Update(prior.X, prior.P*k)
}

// qualityPrior is what a lane is assumed to be worth before it has answered.
//
// Beta(8, 1) is "probably fine": eight usable answers to one bad one, which a
// handful of real refusals is enough to move. A lane serving FOUR-BIT WEIGHTS
// starts at Beta(2, 2) — an open question — because four-bit quantization is
// the one fact on the sheet that predicts a lane returning tool-call JSON the
// decoder refuses, and starting it optimistic means paying for that discovery
// on somebody's real turn.
// QualityPrior is [qualityPrior] for a caller outside this package: the surface
// that DRAWS a lane's standing has to age the belief exactly as the chooser
// does, and ageing needs the prior it decays toward. A surface that invented its
// own would be describing a different rule from the one doing the dropping.
func QualityPrior(quant string) Beta { return qualityPrior(quant) }

func qualityPrior(quant string) Beta {
	if fourBit(strings.ToLower(strings.TrimSpace(quant))) {
		return Beta{A: 2, B: 2}
	}
	return Beta{A: 8, B: 1}
}

// fourBit reports whether a quantization word names four-bit weights, in the
// spellings the router uses for them.
func fourBit(quant string) bool {
	for _, four := range []string{"fp4", "int4", "nf4", "q4"} {
		if strings.HasPrefix(quant, four) {
			return true
		}
	}
	return false
}

// ── WHAT WE MEASURED ────────────────────────────────────────────────────────

// Note folds one timed answer in.
//
// It predicts first and updates second, which is what makes the innovation
// mean anything: the belief is aged to the moment of the sighting, so "how
// surprising was that" is measured against what this lane was doing then rather
// than against what it was doing when we last looked.
func (l *ledger) Note(s Sighting) {
	if s.ID.Zero() || s.At.IsZero() {
		return
	}
	s.ID = s.ID.bare()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	l.see(s)
	l.keep(record{At: s.At, Sight: &s})
}

// see is [ledger.Note]'s arithmetic without the door. It is called with the
// lock held, by the door and by a replay of the journal.
func (l *ledger) see(s Sighting) {
	belief := l.beliefs[s.ID]
	belief.ID = s.ID
	l.dress(&belief)
	prior := l.priors[s.ID]

	if !belief.At.IsZero() {
		belief.TTFT = age(belief.TTFT, s.At.Sub(belief.At), prior.TTFT)
		belief.Rate = age(belief.Rate, s.At.Sub(belief.At), prior.Rate)
	}

	// AN ANSWER WITH NO TOKENS IN IT TEACHES QUALITY AND NOTHING ELSE.
	//
	// A first-token wait is the wait before A TOKEN, and an empty answer never
	// had one: whatever was timed is the wait until the stream gave up, which is
	// a fact about a failure and not about how quickly this lane starts writing.
	// Folding it in as a first-token measurement is how a lane that returned
	// nothing at all in eight hundred milliseconds gets believed to be the
	// fastest lane on the sheet — the belief improving BECAUSE the answer was
	// unusable. The usable half of an empty answer is its outcome, which
	// [Ledger.NoteOutcome] takes, and this file takes nothing else from it.
	if s.TTFT > 0 && s.Tokens > 0 {
		// A first-token wait behind a long prompt is mostly prefill, which no
		// endpoint could have avoided, so it is a noisier claim about the lane
		// the longer the conversation is. A PROBE is the opposite: one token,
		// sent on purpose, measuring exactly our path to this lane right now.
		noise := variance(prior.TTFT) * promptNoise(s.PromptTokens)
		if s.Probe {
			noise = variance(prior.TTFT) * 0.5
		}
		waited := math.Log(msOf(s.TTFT))
		belief.TTFT = belief.TTFT.Update(waited, noise)
		// AND THE SAME MEASUREMENT REACHES ALL FOUR LEVELS, which is what makes
		// the next pair nobody has measured predictable. The change-point test
		// rides on OUR OWN sightings and never on the sheet: a half-hour
		// aggregate re-published every beat would re-assert one surprise until
		// it tripped an alarm about a step that never happened.
		if l.wait.note(pairOf(s.ID), waited, noise, s.At) {
			l.stepped(s.ID)
		}
	}
	// A SHORT ANSWER TEACHES THE FIRST TOKEN AND NEVER THE RATE. A probe is one
	// token sent on purpose and rates the handshake; a handful of tokens rates a
	// lane that has not found its stride. The floor is [ratedFloor] and it is
	// the same floor the transport applies to its own sightings, so that a lane
	// cannot be believed fast on the strength of an answer that never got going.
	if !s.Probe && s.Tokens >= ratedFloor && s.Gen > 0 {
		if rate := s.Rate(); rate > 0 {
			written, noise := math.Log(rate), variance(prior.Rate)
			belief.Rate = belief.Rate.Update(written, noise)
			if l.rate.note(pairOf(s.ID), written, noise, s.At) {
				l.stepped(s.ID)
			}
		}
	}
	belief.At = s.At
	l.beliefs[s.ID] = belief
}

// stepped records that a change point has reset one pair's own component toward
// its parents. It is called with the lock held.
func (l *ledger) stepped(id ID) {
	if l.shifted == nil {
		l.shifted = map[ID]bool{}
	}
	l.shifted[id] = true
}

// NoteOutcome folds in whether an answer could be used.
//
// It does not touch [Belief.At]: quality is not a timing observation, and
// stamping the belief with this moment would quietly tell the next sighting
// that the timing filters had been updated when they had not.
func (l *ledger) NoteOutcome(o Outcome) {
	if o.ID.Zero() {
		return
	}
	o.ID = o.ID.bare()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	l.weigh(o)
	l.keep(record{At: o.At, Out: &o})
}

// weigh is [ledger.NoteOutcome]'s arithmetic without the door. It is called
// with the lock held, by the door and by a replay of the journal.
func (l *ledger) weigh(o Outcome) {
	belief := l.beliefs[o.ID]
	belief.ID = o.ID
	l.dress(&belief)
	prior := qualityPrior(belief.Facts.Quant)
	if !belief.Quality.Known() {
		belief.Quality = l.judged.start(o.ID, prior)
	}
	// Forget first and observe second, for the same reason [Ledger.Note]
	// predicts before it updates: three refusals this afternoon and three from
	// last week are not the same evidence, and folding the new one in on top of
	// the old without ageing would make them so.
	if !belief.QualityAt.IsZero() && !o.At.IsZero() {
		belief.Quality = belief.Quality.Toward(prior, o.At.Sub(belief.QualityAt), QualityHalfLife)
	}
	belief.Quality = belief.Quality.Observe(o.Accepted)
	if !o.At.IsZero() {
		belief.QualityAt = o.At
	}
	// And what the PROVIDER and the MODEL have shown moves too, so that the
	// next lane of this provider nobody has judged starts leaning the way its
	// provider has been shown to lean rather than at the flat prior.
	l.judged.observe(o.ID, o.Accepted, o.At, prior)
	l.beliefs[o.ID] = belief
}

// age widens a belief that has been sitting still, and stops where the public
// sheet stands.
//
// THE CLAMP IS THE WHOLE REASON THIS IS NOT [Posterior.Predict] CALLED DIRECTLY:
// left to itself the variance doubles every half-life forever, and a belief
// three days old would be less certain than the public sheet anybody can read.
// Ageing may make a belief worthless. It may not make it worse than free.
//
// AND "FREE" IS THE SHEET'S OWN WEIGHT, WHICH IS [SheetWeight] TIMES ITS
// SPREAD — not the spread itself. This is the correction, and it is one factor
// of k that made a stated law untrue. The sheet enters the filter as a
// pseudo-observation with R = k·σ² ([prime]), so a belief sitting at P = σ² is
// FOUR TIMES MORE CERTAIN than the public reading, not equally certain. Clamped
// there, a belief could never be outweighed by the sheet however old it got:
// the gain on every refresh was pinned at σ²/(σ² + kσ²) = one fifth, so a lane
// this process had stopped sending to crawled back toward the public number at
// twenty per cent a beat — twenty-five minutes to return from a bad minute, in
// a design whose whole claim is that there is no penalty box. At the honest
// clamp a fully forgotten belief and a fresh sheet weigh the same, which is
// what "worth about as much as anybody can look up" has to mean.
func age(p Posterior, elapsed time.Duration, floor float64) Posterior {
	p = p.Predict(elapsed, HalfLife)
	if ceiling := SheetWeight * floor; ceiling > 0 && p.P > ceiling {
		p.P = ceiling
	}
	return p
}

// variance is the observation noise for one measurement of a lane: the sheet's
// own spread when we have it, and an honestly wide default when we do not.
func variance(prior float64) float64 {
	if prior > 0 {
		return prior
	}
	return defaultSpread * defaultSpread
}

// promptNoise is how much less a first-token measurement says about the lane
// the longer the prompt was. A short prompt measures the lane; a long one
// measures a prefill that every lane would have had to do.
func promptNoise(prompt int) float64 {
	switch {
	case prompt <= 4_000:
		return 1
	case prompt <= 32_000:
		return 2
	default:
		return 4
	}
}

// msOf is a duration in milliseconds, which is the unit every first-token
// belief in this package is in.
func msOf(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// ── WHAT IS BELIEVED ────────────────────────────────────────────────────────

// Belief is what is believed about one lane, as it was last written.
//
// It is NOT aged to now — see the note at the top of this file. The caller
// holds the moment and ages what it reads.
func (l *ledger) Belief(id ID) (Belief, bool) {
	if id.Zero() {
		return Belief{}, false
	}
	id = id.bare()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	belief, ok := l.beliefs[id]
	return belief, ok
}

// Beliefs is every lane believed in for one model, ordered by lane name.
//
// The order is stable so that two readings of the same ledger are the same
// list: a picker that reshuffled its rows between two redraws would be a
// picker nobody could click.
func (l *ledger) Beliefs(model string) []Belief {
	model = BareModel(model)
	if model == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	var found []Belief
	for id, belief := range l.beliefs {
		if id.Model == model {
			found = append(found, belief)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].ID.Lane < found[j].ID.Lane })
	return found
}

// ── THE HIERARCHY'S DOOR ────────────────────────────────────────────────────
//
// The same ledger answers [Hierarchy]. It is a SECOND DOOR ONTO ONE OBJECT and
// not a second object: one sighting moves the chain and the flat belief in the
// same call, so the two can never be two accounts of one lane that disagree.
//
// Unlike [Ledger.Belief] these take the moment, because a chain is only ever
// wanted aged: what the controller waits against is what is believed NOW, and
// asking the caller to age four components itself would be four chances to age
// them by four different rules.

// Wait is the chain over ln first-token in MILLISECONDS for one pair.
func (l *ledger) Wait(id ID, now time.Time) Chain { return l.chainFor(&l.wait, id, now) }

// Rate is the chain over ln tokens-a-second for one pair.
func (l *ledger) Rate(id ID, now time.Time) Chain { return l.chainFor(&l.rate, id, now) }

// Draw is how much ONE ANSWER from this pair moves around what is believed
// about it: the sheet's own published dispersion, and [SpreadFloor] where
// nothing has been published.
//
// IT IS THE SAME NUMBER A SIGHTING IS WEIGHED AGAINST, said for a different
// purpose. [ledger.priors] holds the distance between a lane's published p50
// and its p90 because that is the observation noise one measurement of it
// carries; it is also, and for the same reason, how variable one answer from it
// is — and a wait is judged against exactly that. Nothing here ages: a machine
// does not become steadier because nobody has looked at it lately.
func (l *ledger) Draw(id ID) (first, gap float64) {
	if id.Zero() {
		return SpreadFloor, SpreadFloor
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	prior := l.priors[id.bare()]
	return drawSpread(prior.TTFT), drawSpread(prior.Rate)
}

// drawSpread is one published variance as a spread, and the prior where there
// is none.
func drawSpread(variance float64) float64 {
	if variance <= 0 {
		return SpreadFloor
	}
	return math.Sqrt(variance)
}

// chainFor is one timing chain, aged to now. A zero id is no chain at all
// rather than the world's own pace filed under nothing.
func (l *ledger) chainFor(of *chains, id ID, now time.Time) Chain {
	if id.Zero() {
		return Chain{}
	}
	id = id.bare()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	return of.look(pairOf(id), now)
}

// Think is the chain over ln SECONDS of a whole thinking phase for one model at
// one effort rung.
func (l *ledger) Think(model, rung string, now time.Time) Chain {
	if BareModel(model) == "" {
		return Chain{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	return l.think.look(thoughtOf(model, rung), now)
}

// Shifted reports whether a change point has just reset this pair's own
// component toward its parents, and clears the flag.
func (l *ledger) Shifted(id ID) bool {
	if id.Zero() {
		return false
	}
	id = id.bare()
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.shifted[id] {
		return false
	}
	delete(l.shifted, id)
	return true
}

// NoteThinking folds in how long one whole run of reasoning lasted.
//
// IT IS A THIRD KIND OF SIGHTING and it is separate from [Ledger.Note] because
// it is a fact about a MODEL rather than about a deployment: a lane cannot make
// a model think less, it can only make the same thought arrive faster, which
// the rate chain already says. Judging a long think against the lane's
// first-token belief is what made a legitimate minute of deliberation look like
// a stall.
func (l *ledger) NoteThinking(model, rung string, took time.Duration, at time.Time) {
	model = BareModel(model)
	if model == "" || took <= 0 || at.IsZero() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	seen := thought{Model: model, Rung: rung, Took: took, At: at}
	l.deliberated(seen)
	l.keep(record{At: at, Think: &seen})
}

// deliberated folds one thinking duration into the think chain. It is called
// with the lock held, by the door and by a replay of the journal.
//
// A thinking duration is believed in SECONDS — the unit a person would say it
// in and the unit the controller waits in — and it is folded at the leaf's own
// prior width, because one timed thought is worth about as much as the spread
// between two thoughts of one model at one rung.
func (l *ledger) deliberated(seen thought) {
	if seen.Took <= 0 || seen.At.IsZero() {
		return
	}
	l.think.fold(thoughtOf(seen.Model, seen.Rung), everyLevel, math.Log(seen.Took.Seconds()), levelVariance(LevelPair), seen.At)
}

// Skipped is how many journal lines this ledger could not read. A half-written
// record from a machine that lost power is one observation lost; the count is
// what keeps that from being a silent loss.
func (l *ledger) Skipped() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.skipped
}
