package placeorg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// ErrNotBuilt is returned by every body in this package that a build lane
// has not written yet. The stubs compile so the lanes can start together.
var ErrNotBuilt = errors.New("placeorg: not built yet")

// Mode is how an organising run treats what is already there.
type Mode string

const (
	// ModeAddOn keeps every existing place as it is. New and unplaced chats
	// are filed or parked into the existing tree, Now is checked for
	// births, and only renames and merges are offered. This is the default
	// and what the idle pass runs.
	ModeAddOn Mode = "add-on"
	// ModeRefresh rebuilds the tree in a shadow and offers the difference.
	// Places the person made or confirmed are kept as must-link groups and
	// never dissolved; places this package created may be dissolved,
	// renamed or re-nested. Nothing is applied until the person says yes.
	ModeRefresh Mode = "refresh"
)

// Stage names, in order, for progress and the journal. A reader of the
// progress view sees the person-facing word; the journal uses these.
const (
	StageDedup       = "dedup"
	StageKind        = "kind"
	StageAnchors     = "anchors"
	StageVectors     = "vectors"
	StageNeighbours  = "neighbours"
	StageCommunities = "communities"
	StageNest        = "nest"
	StageAudit       = "audit"
	StagePark        = "park"
	StageName        = "name"
	StageReview      = "review"
	StageLink        = "link"
	StageCheck       = "check"
)

// Progress is one event on the way through a run, published to the desktop
// over the world feed. Done and Total are the stage's own units (chats,
// places, names); Spent is the running cost.
type Progress struct {
	Run   string    `json:"run"`
	Stage string    `json:"stage"`
	Done  int       `json:"done"`
	Total int       `json:"total"`
	Spent Spent     `json:"spent"`
	At    time.Time `json:"at"`
	// Cancellable is true from the first stage to the last; cancel is the
	// context, and a cancelled run applies nothing.
	Cancellable bool `json:"cancellable"`
}

// Resolutions are the three Leiden resolutions, fine to coarse. They are
// the reference implementation's and are calibrated per space by the park
// lane, not edited by hand.
var Resolutions = [3]float64{2.5, 1.0, 0.4}

// Consensus is how many seeded partitions vote and how many must agree on a
// pair before the final cut sees the edge.
const (
	ConsensusRuns  = 8
	ConsensusAgree = 0.4
)

// DefaultSeed is the reference implementation's seed, used when a run is not
// given one so that two runs over the same library still agree.
const DefaultSeed uint64 = 42

// claimShare is how much of two groups' chats must be common for the new
// group to be the place the person already has.
const claimShare = 0.5

// mustLinkWeight is the weight of the edge that ties two chats of one place
// the person made. It is above any similarity the kNN stage can produce, so
// the partition keeps the group together unless the whole graph disagrees.
const mustLinkWeight float32 = 4

// progressEvery is how many chats pass between progress events inside a long
// loop, so a ten-thousand-chat library reports without flooding the feed.
const progressEvery = 250

// Options is everything one run needs besides the stages.
type Options struct {
	Mode   Mode
	Seed   uint64
	Budget Budget
	// Existing is the store's current snapshot as a Tree, built by the
	// bridge; add-on mode keeps it, refresh mode uses its person-made
	// places as must-link groups.
	Existing *Tree
	Progress func(Progress)
	Now      func() time.Time
}

// Runner drives the stages in order, applies every Op through the one
// journaling door, runs the self-check and returns a Plan. It is the only
// type that can change a Tree. One Runner runs one run at a time: a second
// Run while one is going is refused, because the bridge queues them.
type Runner struct {
	Stages  Stages
	Journal Journal
	// Judge, when set, is what the stages are handed as it is. Tests and
	// replays use it. When nil the Runner builds its own from Ask, wrapped
	// in the budget and the journal.
	Judge Judge
	// Ask is the engine's model door; nil means rules only, and every
	// judgment takes its fallback.
	Ask placegraph.Asker
	// Price is what one exchange cost in dollars. Nil leaves cost unknown.
	Price func(prompt, answer string) float64
	// Primary and Local are the two embedding doors the Vectors stage uses.
	Primary, Local Embedder
	// State, when set, receives what a finished run learned: the space, the
	// kinds, the representatives, the calibrated thresholds and the summary.
	// The caller saves it.
	State *State

	mu  sync.Mutex
	cur *recorder
}

// recorder is the journal door for the run in progress.
type recorder struct {
	run     string
	now     func() time.Time
	journal Journal
	judge   *meteredJudge
}

func (c *recorder) write(e Entry) error {
	e.At, e.Run = c.now(), c.run
	if c.judge != nil {
		e.Spent, _ = c.judge.snapshot()
	}
	if c.journal == nil {
		return nil
	}
	return c.journal.Write(e)
}

// Run performs one organising run. A cancelled context ends it with that
// error and no Plan. Nothing the caller handed in is changed: the run works
// on copies and returns the Plan that would change the store.
func (r *Runner) Run(ctx context.Context, corpus *Corpus, opts Options) (*Plan, error) {
	if !r.mu.TryLock() {
		return nil, errors.New("placeorg: a run is already going")
	}
	defer r.mu.Unlock()
	if opts.Mode == "" {
		opts.Mode = ModeAddOn
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if corpus == nil {
		corpus = &Corpus{}
	}
	if missing := r.missing(opts.Mode); len(missing) > 0 {
		return nil, fmt.Errorf("placeorg: no stage wired for %s", strings.Join(missing, ", "))
	}
	u := r.begin(corpus, opts)
	defer func() { r.cur = nil }()
	plan, err := u.execute(ctx)
	if err != nil {
		u.end(ctx, err)
		return nil, err
	}
	u.end(ctx, nil)
	return plan, nil
}

// apply journals an Op and applies it to the tree. Every change to a Tree
// anywhere in this package goes through this function. An op marked Proposed
// is journaled and left for the person: the tree does not take it.
func (r *Runner) apply(t *Tree, op Op) error {
	if r.cur != nil {
		if err := r.cur.write(Entry{Kind: EntryOp, Stage: op.Stage, Op: &op}); err != nil {
			return err
		}
	}
	if op.Proposed {
		return nil
	}
	return t.apply(op)
}

// missing names the stages this mode needs and the registry lacks, so a
// half-wired engine is a clear error before anything starts.
func (r *Runner) missing(mode Mode) []string {
	s := r.Stages
	need := map[string]bool{
		"dedup": s.Dedup == nil, "kind": s.Kind == nil, "anchors": s.Anchors == nil,
		"vectors": s.Vectors == nil, "link": s.Link == nil,
	}
	if mode == ModeRefresh {
		for name, absent := range map[string]bool{
			"neighbours": s.Neighbours == nil, "communities": s.Communities == nil, "nest": s.Nest == nil,
			"audit": s.Audit == nil, "park": s.Park == nil, "name": s.Name == nil, "review": s.Review == nil,
		} {
			need[name] = absent
		}
	} else {
		need["stream"], need["name"] = s.Stream == nil, s.Name == nil
	}
	var out []string
	for _, name := range []string{"dedup", "kind", "anchors", "vectors", "neighbours", "communities", "nest", "audit", "park", "stream", "name", "review", "link"} {
		if need[name] {
			out = append(out, name)
		}
	}
	return out
}

// run is one organising run's working state.
type run struct {
	r       *Runner
	id      string
	opts    Options
	corpus  *Corpus
	live    *Corpus
	judge   Judge
	metered *meteredJudge
	// door is the Runner's journaling door, (*Runner).apply, handed along as
	// a value so the one function allowed to change a Tree stays the only
	// one that calls the mutator.
	door    func(*Tree, Op) error
	started time.Time

	space   SpaceID
	fell    bool
	vectors [][]float32
	work    *Tree
	ops     []Op
	demoted []Op
}

func (r *Runner) begin(corpus *Corpus, opts Options) *run {
	started := opts.Now()
	u := &run{r: r, corpus: corpus, opts: opts, started: started, door: r.apply}
	u.id = fmt.Sprintf("plan-%d-%d", opts.Seed, started.UnixNano())
	rec := &recorder{run: u.id, now: opts.Now, journal: r.Journal}
	r.cur = rec
	switch {
	case r.Judge != nil:
		u.judge = r.Judge
	default:
		u.metered = &meteredJudge{ask: r.Ask, budget: opts.Budget, price: r.Price, note: rec.write, run: u.id, now: opts.Now}
		rec.judge = u.metered
		u.judge = u.metered
	}
	return u
}

// note writes a journal line, and publishes nothing.
func (u *run) note(e Entry) error { return u.r.cur.write(e) }

// stage records that a stage is under way and publishes progress. It is also
// the point where cancellation and a lost journal end the run, so no stage
// starts after either.
func (u *run) stage(ctx context.Context, name string, done, total int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if u.metered != nil {
		if err := u.metered.failure(); err != nil {
			return err
		}
	}
	note := fmt.Sprintf("%d of %d", done, total)
	if err := u.note(Entry{Kind: EntryStage, Stage: name, Note: note}); err != nil {
		return err
	}
	if u.opts.Progress != nil {
		var spent Spent
		if u.metered != nil {
			spent, _ = u.metered.snapshot()
		}
		u.opts.Progress(Progress{Run: u.id, Stage: name, Done: done, Total: total, Spent: spent, At: u.opts.Now(), Cancellable: true})
	}
	return nil
}

// end closes the journal's account of the run.
func (u *run) end(ctx context.Context, err error) {
	note := "finished"
	if err != nil {
		note = "failed: " + err.Error()
		if ctx.Err() != nil {
			note = "cancelled"
		}
	}
	_ = u.note(Entry{Kind: EntryRunEnded, Note: note})
}

func (u *run) execute(ctx context.Context) (*Plan, error) {
	if err := u.note(Entry{Kind: EntryRunStarted, Note: string(u.opts.Mode)}); err != nil {
		return nil, err
	}
	if err := u.declareFallbacks(); err != nil {
		return nil, err
	}
	if err := u.prepare(ctx); err != nil {
		return nil, err
	}
	var err error
	if u.opts.Mode == ModeRefresh {
		err = u.refresh(ctx)
	} else {
		err = u.addOn(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err := u.stage(ctx, StageLink, 0, 1); err != nil {
		return nil, err
	}
	if err := u.link(ctx); err != nil {
		return nil, err
	}
	return u.finish(ctx)
}

// declareFallbacks writes every judged stage's degraded behaviour into the
// journal, so the record of a run states what it would have done without a model.
func (u *run) declareFallbacks() error {
	s := u.r.Stages
	for _, stage := range []any{s.Audit, s.Name, s.Review, s.Stream, s.Steer} {
		judged, ok := stage.(Judged)
		if !ok {
			continue
		}
		for _, f := range judged.Fallbacks() {
			if err := u.note(Entry{Kind: EntryFallback, Note: f.When + ": " + f.Does}); err != nil {
				return err
			}
		}
	}
	return nil
}

// prepare is the part both modes share: fold copies, sort out probes, derive
// anchors, and embed the chats that count.
func (u *run) prepare(ctx context.Context) error {
	s, c := u.r.Stages, u.corpus
	if err := u.stage(ctx, StageDedup, 0, len(c.Chats)); err != nil {
		return err
	}
	rep, err := s.Dedup.Dedup(ctx, c.Chats)
	if err != nil {
		return fmt.Errorf("placeorg: dedup: %w", err)
	}
	c.Representative = rep
	if err := u.stage(ctx, StageKind, 0, len(c.Chats)); err != nil {
		return err
	}
	if c.Kinds, err = s.Kind.Kinds(ctx, c.Chats, rep); err != nil {
		return fmt.Errorf("placeorg: kind: %w", err)
	}
	if err := u.stage(ctx, StageAnchors, 0, len(c.Chats)); err != nil {
		return err
	}
	if c.Anchors, err = s.Anchors.Anchors(ctx, c.Chats, personNames(u.opts.Existing)); err != nil {
		return fmt.Errorf("placeorg: anchors: %w", err)
	}
	u.live = liveCorpus(c)
	if err := u.stage(ctx, StageVectors, 0, len(u.live.Chats)); err != nil {
		return err
	}
	if len(u.live.Chats) == 0 {
		return nil
	}
	space, vectors, fell, err := s.Vectors.Vectorize(ctx, u.live, u.r.Primary, u.r.Local)
	if err != nil {
		return fmt.Errorf("placeorg: vectors: %w", err)
	}
	u.space, u.vectors, u.fell = space, vectors, fell
	return u.stage(ctx, StageVectors, len(u.live.Chats), len(u.live.Chats))
}

// personNames are the names of the places the person made, which the anchor
// stage treats as subjects in the person's own terms.
func personNames(t *Tree) []string {
	if t == nil {
		return nil
	}
	var out []string
	for _, id := range sortedKeys(t.nodes) {
		if n := t.nodes[id]; n.Person && n.Name != "" {
			out = append(out, n.Name)
		}
	}
	return out
}

// liveCorpus is the chats that take part in the tree: real conversations, not
// probes and not copies. The kinds and anchors are shared, not copied.
func liveCorpus(c *Corpus) *Corpus {
	live := &Corpus{Kinds: c.Kinds, Representative: c.Representative, Anchors: c.Anchors}
	for _, chat := range c.Chats {
		if k := c.Kinds[chat.ID]; k == KindProbe || k == KindDuplicate {
			continue
		}
		live.Chats = append(live.Chats, chat)
	}
	return live
}

// refresh rebuilds the tree in a shadow: graph, three partitions, nesting,
// the audit's right-sizing, parking, names and the review.
func (u *run) refresh(ctx context.Context) error {
	u.work = NewTree()
	if len(u.live.Chats) > 0 {
		if err := u.build(ctx); err != nil {
			return err
		}
		if err := u.audit(ctx); err != nil {
			return err
		}
		if err := u.park(ctx); err != nil {
			return err
		}
		if err := u.settle(); err != nil {
			return err
		}
		if err := u.name(ctx); err != nil {
			return err
		}
		if err := u.review(ctx); err != nil {
			return err
		}
	}
	return nil
}

// build makes the kNN graph, cuts it at three resolutions and nests the cuts.
func (u *run) build(ctx context.Context) error {
	s := u.r.Stages
	ids := chatIDs(u.live.Chats)
	if err := u.stage(ctx, StageNeighbours, 0, len(ids)); err != nil {
		return err
	}
	g, err := s.Neighbours.KNN(ctx, u.vectors, u.live.Anchors, ids)
	if err != nil {
		return fmt.Errorf("placeorg: neighbours: %w", err)
	}
	g.Edges = append(g.Edges, mustLinks(u.opts.Existing, ids)...)
	seed := u.opts.Seed
	if seed == 0 {
		seed = DefaultSeed
	}
	var labels [3][]int
	for i, res := range Resolutions {
		if err := u.stage(ctx, StageCommunities, i, len(Resolutions)); err != nil {
			return err
		}
		if labels[i], err = s.Communities.Partition(ctx, g, res, seed); err != nil {
			return fmt.Errorf("placeorg: communities: %w", err)
		}
	}
	if err := u.stage(ctx, StageNest, 0, 1); err != nil {
		return err
	}
	nested, err := s.Nest.Nest(ctx, u.live, labels[0], labels[1], labels[2])
	if err != nil {
		return fmt.Errorf("placeorg: nest: %w", err)
	}
	// The nest stage spells its ids with a slash, which the id law refuses,
	// so the run's own copy spells them with a dot.
	u.work = nested.clone(func(id string) string { return strings.ReplaceAll(id, "/", ".") })
	u.claimPlaces()
	return nil
}

// mustLinks ties together the chats of every place the person made, so a
// rebuild does not pull them apart. Each group becomes a chain of edges.
func mustLinks(existing *Tree, ids []string) []Edge {
	if existing == nil {
		return nil
	}
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}
	var edges []Edge
	for _, id := range sortedKeys(existing.nodes) {
		if !existing.nodes[id].Person {
			continue
		}
		prev := -1
		for _, chat := range sortedKeys(existing.subtree(id)) {
			i, ok := index[chat]
			if !ok {
				continue
			}
			if prev >= 0 {
				edges = append(edges, Edge{I: min(prev, i), J: max(prev, i), W: mustLinkWeight})
			}
			prev = i
		}
	}
	return edges
}

// claimPlaces matches each place the person made to the new group that holds
// most of the same chats, and gives that group the old place's identity. A
// match needs the two to share at least half of what either holds.
func (u *run) claimPlaces() {
	existing := u.opts.Existing
	if existing == nil {
		return
	}
	taken := map[string]bool{}
	for _, id := range sortedKeys(existing.nodes) {
		old := existing.nodes[id]
		if !old.Person {
			continue
		}
		want := existing.subtree(id)
		best, bestScore := "", 0.0
		for _, cand := range sortedKeys(u.work.nodes) {
			if taken[cand] {
				continue
			}
			// A tie goes to the first, which is the broadest place, because
			// places are walked in id order and the roots sort before their
			// children.
			if score := overlap(want, u.work.subtree(cand)); score >= claimShare && score > bestScore {
				best, bestScore = cand, score
			}
		}
		if best != "" {
			taken[best] = true
			u.work.adopt(best, *old)
		}
	}
}

// overlap is the share of two chat sets that is common, by union.
func overlap(a, b map[string]bool) float64 {
	shared := 0
	for k := range a {
		if b[k] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// audit right-sizes the shadow. A change to a place the person made stays an
// offer; the others, interposes included, are how the shadow becomes the tree
// this run proposes, so they are applied to it. The Plan as a whole is still
// only an offer: nothing reaches the store until the person accepts it.
func (u *run) audit(ctx context.Context) error {
	if err := u.stage(ctx, StageAudit, 0, 1); err != nil {
		return err
	}
	ops, err := u.r.Stages.Audit.Audit(ctx, u.work, u.live, u.vectors, u.judge)
	if err != nil {
		return fmt.Errorf("placeorg: audit: %w", err)
	}
	for _, op := range ops {
		op.Proposed = u.touchesPerson(op)
		if err := u.take(op); err != nil {
			return fmt.Errorf("placeorg: audit: %w", err)
		}
		if op.Kind == OpDemote {
			u.demoted = append(u.demoted, op)
		}
	}
	return nil
}

func (u *run) touchesPerson(op Op) bool {
	for _, id := range []string{op.Place, op.Parent, op.Into} {
		if n, ok := u.work.nodes[id]; ok && n.Person {
			return true
		}
	}
	return false
}

// take applies one op through the journaling door and keeps it for the Plan.
func (u *run) take(op Op) error {
	if err := u.door(u.work, op); err != nil {
		return err
	}
	u.ops = append(u.ops, op)
	return nil
}

// park gives every chat of the shadow its home by the Chow rule.
func (u *run) park(ctx context.Context) error {
	if err := u.stage(ctx, StagePark, 0, len(u.live.Chats)); err != nil {
		return err
	}
	ops, err := u.r.Stages.Park.Park(ctx, u.work, u.live, u.vectors, u.space)
	if err != nil {
		return fmt.Errorf("placeorg: park: %w", err)
	}
	for i, op := range ops {
		op.Proposed = false
		if err := u.take(op); err != nil {
			return fmt.Errorf("placeorg: park: %w", err)
		}
		if (i+1)%progressEvery == 0 {
			if err := u.stage(ctx, StagePark, i+1, len(ops)); err != nil {
				return err
			}
		}
	}
	if t, ok := u.r.Stages.Park.(interface {
		Thresholds() map[SpaceID]Thresholds
	}); ok && u.r.State != nil {
		u.r.State.Thresholds = mergeThresholds(u.r.State.Thresholds, t.Thresholds())
	}
	return u.stage(ctx, StagePark, len(ops), len(ops))
}

// settle dissolves a place that parking left too small. The audit measured
// places by the chats of their communities, but a chat that parked at a broader
// place is no longer homed in the finer one, so a place whose chats all parked
// up holds nothing and one with a few holds too few. The place the person
// made is left alone: it is theirs to dissolve.
func (u *run) settle() error {
	for depth := u.work.Depth(); depth >= 1; depth-- {
		for _, id := range sortedKeys(u.work.nodes) {
			n := u.work.nodes[id]
			if n == nil || n.Depth != depth || n.Person {
				continue
			}
			if held := placeChatCount(u.work, id, u.live); held < Quorum {
				op := Op{
					Kind: OpDissolve, Stage: StagePark, Place: id, Parent: n.Parent, Members: u.work.subtreeList(id),
					Why: fmt.Sprintf("Only %d chats stayed here, which is not enough for a place.", held),
				}
				if err := u.take(op); err != nil {
					return fmt.Errorf("placeorg: settling: %w", err)
				}
			}
		}
	}
	return nil
}

func mergeThresholds(have, add map[SpaceID]Thresholds) map[SpaceID]Thresholds {
	if have == nil {
		have = map[SpaceID]Thresholds{}
	}
	for space, th := range add {
		have[space] = th
	}
	return have
}

// name names the places. A name for a place the person made is an offer.
func (u *run) name(ctx context.Context) error {
	if err := u.stage(ctx, StageName, 0, len(u.work.nodes)); err != nil {
		return err
	}
	ops, err := u.r.Stages.Name.Name(ctx, u.work, u.live, u.judge)
	if err != nil {
		return fmt.Errorf("placeorg: name: %w", err)
	}
	for _, op := range ops {
		if err := u.take(op); err != nil {
			return fmt.Errorf("placeorg: name: %w", err)
		}
	}
	return u.stage(ctx, StageName, len(ops), len(u.work.nodes))
}

// review reads the whole outline once. Renames apply unless they touch a
// place the person made; merges are only ever offers.
func (u *run) review(ctx context.Context) error {
	if err := u.stage(ctx, StageReview, 0, 1); err != nil {
		return err
	}
	ops, err := u.r.Stages.Review.Review(ctx, u.work, u.live, u.judge)
	if err != nil {
		return fmt.Errorf("placeorg: review: %w", err)
	}
	for _, op := range ops {
		op.Proposed = op.Proposed || op.Kind == OpMerge
		if err := u.take(op); err != nil {
			return fmt.Errorf("placeorg: review: %w", err)
		}
	}
	return nil
}

// addOn keeps every place and files what is not yet filed. Existing is the
// tree; without one the run starts from an empty tree and files nothing.
func (u *run) addOn(ctx context.Context) error {
	u.work = u.opts.Existing.clone(nil)
	if len(u.live.Chats) == 0 {
		return nil
	}
	if err := u.centroids(); err != nil {
		return err
	}
	if err := u.fileUnplaced(ctx); err != nil {
		return err
	}
	return u.births(ctx)
}

// centroids positions every existing place from the chats already in it, in
// the space of this run, so a new chat can be compared with them.
func (u *run) centroids() error {
	sums := map[string]*Centroid{}
	for i, chat := range u.live.Chats {
		home, ok := u.work.Home(chat.ID)
		if !ok || len(u.vectors[i]) == 0 {
			continue
		}
		for id := home; id != ""; id = u.work.nodes[id].Parent {
			c := sums[id]
			if c == nil {
				c = &Centroid{Sum: make([]float32, len(u.vectors[i]))}
				sums[id] = c
			}
			for d, v := range u.vectors[i] {
				c.Sum[d] += v
			}
			c.Count++
		}
	}
	for _, id := range sortedKeys(sums) {
		if err := SetCentroid(u.work, id, u.space, *sums[id]); err != nil {
			return err
		}
	}
	return nil
}

// fileUnplaced files each chat that has no home yet, one at a time, by the
// stream path's rule.
func (u *run) fileUnplaced(ctx context.Context) error {
	if err := u.stage(ctx, StagePark, 0, len(u.live.Chats)); err != nil {
		return err
	}
	for i, chat := range u.live.Chats {
		if _, placed := u.work.Home(chat.ID); placed {
			continue
		}
		filing, err := u.r.Stages.Stream.File(ctx, u.work, Arrival{Chat: chat, Vector: u.vectors[i]}, u.judge)
		if err != nil {
			return fmt.Errorf("placeorg: filing %s: %w", chat.ID, err)
		}
		filing.Op.Proposed = false
		if err := u.take(filing.Op); err != nil {
			return fmt.Errorf("placeorg: filing %s: %w", chat.ID, err)
		}
		if (i+1)%progressEvery == 0 {
			if err := u.stage(ctx, StagePark, i+1, len(u.live.Chats)); err != nil {
				return err
			}
		}
	}
	return u.stage(ctx, StagePark, len(u.live.Chats), len(u.live.Chats))
}

// births offers a place for each group Now has gathered, and names it.
func (u *run) births(ctx context.Context) error {
	var arrivals []Arrival
	for i, chat := range u.live.Chats {
		if u.work.inbox[chat.ID] {
			arrivals = append(arrivals, Arrival{Chat: chat, Vector: u.vectors[i]})
		}
	}
	ops, err := u.r.Stages.Stream.Births(ctx, u.work, arrivals)
	if err != nil || len(ops) == 0 {
		return err
	}
	for i := range ops {
		ops[i].Place = fmt.Sprintf("birth.%d", i+1)
		ops[i].Proposed = true
	}
	if err := u.nameBirths(ctx, ops); err != nil {
		return err
	}
	for _, op := range ops {
		if err := u.take(op); err != nil {
			return fmt.Errorf("placeorg: births: %w", err)
		}
	}
	return nil
}

// nameBirths names the offered places. They are not in the tree, so the name
// stage is shown a tree of just them; a name that an existing place already
// has is left off, and the person names that one.
func (u *run) nameBirths(ctx context.Context, ops []Op) error {
	if err := u.stage(ctx, StageName, 0, len(ops)); err != nil {
		return err
	}
	nodes := make([]Node, len(ops))
	for i, op := range ops {
		nodes[i] = Node{ID: op.Place, Depth: 1, Members: op.Members}
	}
	named, err := u.r.Stages.Name.Name(ctx, TreeFromNodes(nodes), u.live, u.judge)
	if err != nil {
		return fmt.Errorf("placeorg: name: %w", err)
	}
	used := map[string]bool{}
	for _, n := range u.work.nodes {
		used[strings.ToLower(n.Name)] = true
	}
	for _, nm := range named {
		for i := range ops {
			if ops[i].Place == nm.Place && nm.Kind == OpName && !used[strings.ToLower(nm.Name)] {
				ops[i].Name = nm.Name
			}
		}
	}
	return u.stage(ctx, StageName, len(ops), len(ops))
}

// link adds shortcuts and views, then files the copies beside the chat they
// copy. Copies come last so they follow wherever their original ended up.
func (u *run) link(ctx context.Context) error {
	if len(u.live.Chats) > 0 {
		if t, ok := u.r.Stages.Link.(interface{ SetDemoted([]Op) }); ok {
			t.SetDemoted(u.demoted)
		}
		ops, err := u.r.Stages.Link.Link(ctx, u.work, u.live, u.vectors)
		if err != nil {
			return fmt.Errorf("placeorg: link: %w", err)
		}
		for _, op := range ops {
			op.Proposed = false
			if op.Kind == OpLink && contains(u.work.Links(op.Chat), op.Place) {
				continue
			}
			if err := u.take(op); err != nil {
				return fmt.Errorf("placeorg: link: %w", err)
			}
		}
	}
	return u.fileCopies()
}

// fileCopies gives each duplicate the home of its representative.
func (u *run) fileCopies() error {
	for _, chat := range u.corpus.Chats {
		if u.corpus.Kinds[chat.ID] != KindDuplicate {
			continue
		}
		rep := u.corpus.Representative[chat.ID]
		op := Op{Stage: StageDedup, Chat: chat.ID, Why: "A copy of another chat, kept beside it."}
		if home, ok := u.work.Home(rep); ok {
			if cur, _ := u.work.Home(chat.ID); cur == home {
				continue
			}
			op.Kind, op.Place = OpFile, home
		} else if u.work.inbox[rep] {
			op.Kind = OpInbox
		} else {
			continue
		}
		if err := u.take(op); err != nil {
			return fmt.Errorf("placeorg: copies: %w", err)
		}
	}
	return nil
}

// finish runs the self-check and, only if every law holds, hands back the Plan.
func (u *run) finish(ctx context.Context) (*Plan, error) {
	if err := u.stage(ctx, StageCheck, 0, len(Invariants)); err != nil {
		return nil, err
	}
	plan := &Plan{
		ID: u.id, Mode: u.opts.Mode, Space: u.space, Seed: u.opts.Seed,
		Started: u.started, Ended: u.opts.Now(), Ops: u.ops,
	}
	if u.metered != nil {
		var over bool
		plan.Spent, over = u.metered.snapshot()
		plan.Provisional = over
		if err := u.metered.failure(); err != nil {
			return nil, err
		}
	}
	plan.Provisional = plan.Provisional || u.fell || u.space == SpaceHashed
	results, ok := Check(u.work, plan, u.corpus)
	plan.Invariants = results
	for _, res := range results {
		note := res.Name
		if !res.OK {
			note += ": " + res.Why
		}
		if err := u.note(Entry{Kind: EntryInvariant, Stage: StageCheck, Note: note}); err != nil {
			return nil, err
		}
	}
	if !ok {
		return nil, fmt.Errorf("placeorg: the plan broke %s", brokenLaws(results))
	}
	plan.Summary = summarize(plan, u.opts.Existing, u.work, u.roughness())
	u.learn(plan)
	return plan, nil
}

// roughness says why a plan is provisional, in the person's words.
func (u *run) roughness() []string {
	var out []string
	if u.fell || u.space == SpaceHashed {
		out = append(out, "a local model embedded the chats")
	}
	if u.metered != nil {
		if _, over := u.metered.snapshot(); over {
			out = append(out, "the budget ran out before every name was asked for")
		}
	}
	return out
}

func brokenLaws(results []InvariantResult) string {
	var out []string
	for _, r := range results {
		if !r.OK {
			out = append(out, fmt.Sprintf("%q (%s)", r.Name, r.Why))
		}
	}
	return strings.Join(out, "; ")
}

// learn records what the run found in the organiser's state, once the Plan
// is whole. A run that fails or is cancelled never reaches it.
func (u *run) learn(plan *Plan) {
	st := u.r.State
	if st == nil {
		return
	}
	st.Space = plan.Space
	st.Kinds, st.Representative = u.corpus.Kinds, u.corpus.Representative
	st.LastRun = &RunSummary{
		Plan: plan.ID, Mode: plan.Mode, Space: plan.Space, Started: plan.Started, Ended: plan.Ended,
		Chats: len(u.corpus.Chats), Places: len(u.work.nodes), Spent: plan.Spent,
		Summary: plan.Summary, Outcome: "offered",
	}
}

// summarize writes the person's one line from the ops. Only what happened is
// said: a count of zero is left out.
func summarize(plan *Plan, existing, work *Tree, rough []string) string {
	counts := map[OpKind]int{}
	for _, op := range plan.Ops {
		counts[op.Kind]++
	}
	var parts []string
	add := func(n int, one, many string) {
		switch {
		case n == 1:
			parts = append(parts, "1 "+one)
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	if plan.Mode == ModeRefresh {
		gone, moved := refreshDiff(existing, work)
		add(len(work.nodes), "place", "places")
		add(gone, "place would go", "places would go")
		add(moved, "chat would move", "chats would move")
	} else {
		add(counts[OpFile]+counts[OpPark], "chat filed", "chats filed")
		add(counts[OpBirth], "new place offered", "new places offered")
	}
	add(counts[OpRename], "rename offered", "renames offered")
	add(counts[OpMerge], "merge offered", "merges offered")
	line := strings.Join(parts, ", ")
	if len(rough) > 0 {
		line += suffixIf(line != "", ". ") + "This plan is provisional: " + strings.Join(rough, " and ") + "."
	}
	return line
}

func suffixIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

// refreshDiff counts what a rebuild would change against what the person has:
// existing places whose name the new tree no longer has, and chats whose place
// carries a different name than before.
func refreshDiff(existing, work *Tree) (gone, moved int) {
	if existing == nil {
		return 0, 0
	}
	names := map[string]bool{}
	for _, n := range work.nodes {
		names[strings.ToLower(n.Name)] = true
	}
	for _, n := range existing.nodes {
		if !names[strings.ToLower(n.Name)] {
			gone++
		}
	}
	for chat, old := range existing.home {
		now, ok := work.home[chat]
		if !ok {
			continue
		}
		if !strings.EqualFold(existing.nodes[old].Name, work.nodes[now].Name) {
			moved++
		}
	}
	return gone, moved
}

func chatIDs(chats []Chat) []string {
	ids := make([]string, len(chats))
	for i, c := range chats {
		ids[i] = c.ID
	}
	return ids
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
