package lane

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
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
}

// spread is one lane's prior variance for each filter.
type spread struct {
	ttft float64
	rate float64
}

// newLedger builds the live ledger. It is called from the registry and nowhere
// else.
func newLedger() *ledger {
	return &ledger{beliefs: map[ID]Belief{}, priors: map[ID]spread{}}
}

// keepIn attaches the store this ledger writes through. Yesterday's beliefs are
// not read here — that is done lazily, on first use, so that a process which
// merely imports this package never touches the disk.
func (l *ledger) keepIn(keeper Store) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keeper, l.loaded = keeper, false
}

// readFrom attaches the sheet this ledger dresses a newly seen lane from. It
// fetches nothing; see the field's own note.
func (l *ledger) readFrom(pages Sheet) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pages = pages
}

// restore reads yesterday's beliefs, once. It is called with the lock held.
func (l *ledger) restore() {
	if l.loaded {
		return
	}
	l.loaded = true
	l.foldInFile()
}

// reload folds the file into memory again, however many times it is called.
//
// IT IS THE OTHER HALF OF THE MULTI-PROCESS CONTRACT (store.go, "two processes,
// one file"). A save already comes back with the merged set, so a process that
// is writing stays current for nothing; this is for the process that is not.
// The beat calls it before it primes, which is the one moment a session is
// already doing a round of bookkeeping and the one moment a stale ledger would
// otherwise be about to overwrite a fresher file.
//
// NOTHING ON THE SEND PATH CALLS IT. A choice reads memory.
func (l *ledger) reload() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loaded = true
	l.foldInFile()
}

// foldInFile merges what is on disk into memory. It is called with the lock
// held.
//
// WHAT IS ON DISK NO LONGER LOSES OUTRIGHT TO WHAT IS IN MEMORY, and that is a
// correction rather than a preference. The old rule — memory always wins —
// was written for one process reading its own file at open, where it is
// exactly right: a sighting that landed while a slow read was happening must
// not be undone by it. With two processes the same rule silently discards
// everything the other one learned. [fresher] keeps both readings of the rule:
// a measurement this process has already made is newer than the file and still
// wins, and a model it has never heard of arrives intact.
func (l *ledger) foldInFile() {
	if l.keeper == nil {
		return
	}
	kept, err := l.keeper.Load()
	if err != nil {
		return
	}
	l.adopt(kept)
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

// save writes the whole set through the store, and takes back what the file
// then holds. It is called with the lock held, which is what keeps two updates
// from landing on disk in the wrong order; the file is tens of kilobytes and
// the write is one rename.
//
// THE MERGE IS THE SAVE. A store that can read-merge-write hands the merged set
// straight back ([store.saveMerging]), so the same lock that made the write
// safe also tells this process what the other one wrote — no second read, no
// second decode, and no window in which two ledgers are each sure they are the
// only one.
func (l *ledger) save() {
	if l.keeper == nil {
		return
	}
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
	belief := l.beliefs[row.ID]
	belief.ID = row.ID
	belief.Facts = row.Facts

	if row.Known() {
		ttft := fit(row.TTFTp50, row.TTFTp90)
		rate := fit(row.Ratep50, row.Ratep90)
		prior := l.priors[row.ID]
		l.priors[row.ID] = spread{ttft: ttft.P, rate: rate.P}
		// AGE FIRST, FOLD SECOND — the same order [Ledger.Note] keeps and for
		// the same reason. A reading taken now weighed against the confidence a
		// belief had ten minutes ago is a public number that cannot move a stale
		// opinion, which is exactly the lane this process has stopped sending
		// to. See [Row.At]; a row with no moment ages nothing, which is what
		// every caller that primes from a fixture wants.
		if !row.At.IsZero() && !belief.At.IsZero() {
			belief.TTFT = age(belief.TTFT, row.At.Sub(belief.At), prior.ttft)
			belief.Rate = age(belief.Rate, row.At.Sub(belief.At), prior.rate)
		}
		belief.TTFT = prime(belief.TTFT, ttft, k)
		belief.Rate = prime(belief.Rate, rate, k)
	}
	// The quality prior is set once and never re-asserted: a lane that has
	// spent the afternoon returning tool calls the decoder refused must not be
	// handed its optimism back every five minutes by the beat.
	if !belief.Quality.Known() {
		belief.Quality = qualityPrior(row.Facts.Quant)
	}
	l.beliefs[row.ID] = belief
	l.save()
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
		l.priors[belief.ID] = spread{ttft: ttft.P, rate: rate.P}
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
		belief.Quality = qualityPrior(row.Facts.Quant)
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
	belief := l.beliefs[s.ID]
	belief.ID = s.ID
	l.dress(&belief)
	prior := l.priors[s.ID]

	if !belief.At.IsZero() {
		belief.TTFT = age(belief.TTFT, s.At.Sub(belief.At), prior.ttft)
		belief.Rate = age(belief.Rate, s.At.Sub(belief.At), prior.rate)
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
		noise := variance(prior.ttft) * promptNoise(s.PromptTokens)
		if s.Probe {
			noise = variance(prior.ttft) * 0.5
		}
		belief.TTFT = belief.TTFT.Update(math.Log(msOf(s.TTFT)), noise)
	}
	// A SHORT ANSWER TEACHES THE FIRST TOKEN AND NEVER THE RATE. A probe is one
	// token sent on purpose and rates the handshake; a handful of tokens rates a
	// lane that has not found its stride. The floor is [ratedFloor] and it is
	// the same floor the transport applies to its own sightings, so that a lane
	// cannot be believed fast on the strength of an answer that never got going.
	if !s.Probe && s.Tokens >= ratedFloor && s.Gen > 0 {
		if rate := s.Rate(); rate > 0 {
			belief.Rate = belief.Rate.Update(math.Log(rate), variance(prior.rate))
		}
	}
	belief.At = s.At
	l.beliefs[s.ID] = belief
	l.save()
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
	belief := l.beliefs[o.ID]
	belief.ID = o.ID
	l.dress(&belief)
	prior := qualityPrior(belief.Facts.Quant)
	if !belief.Quality.Known() {
		belief.Quality = prior
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
	l.beliefs[o.ID] = belief
	l.save()
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
