package desktopbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/placeorg/anchors"
	"github.com/Agent-Field/codeaf/internal/placeorg/audit"
	"github.com/Agent-Field/codeaf/internal/placeorg/community"
	"github.com/Agent-Field/codeaf/internal/placeorg/correct"
	"github.com/Agent-Field/codeaf/internal/placeorg/dedup"
	"github.com/Agent-Field/codeaf/internal/placeorg/journal"
	"github.com/Agent-Field/codeaf/internal/placeorg/kind"
	"github.com/Agent-Field/codeaf/internal/placeorg/knn"
	"github.com/Agent-Field/codeaf/internal/placeorg/link"
	"github.com/Agent-Field/codeaf/internal/placeorg/localembed"
	"github.com/Agent-Field/codeaf/internal/placeorg/name"
	"github.com/Agent-Field/codeaf/internal/placeorg/nest"
	orgpark "github.com/Agent-Field/codeaf/internal/placeorg/park"
	"github.com/Agent-Field/codeaf/internal/placeorg/review"
	"github.com/Agent-Field/codeaf/internal/placeorg/steer"
	"github.com/Agent-Field/codeaf/internal/placeorg/stream"
	"github.com/Agent-Field/codeaf/internal/placeorg/vectors"
)

func init() {
	// Exact tails are matched before "{id}", so "organize" is never read as a place.
	registerPlacesRoute("POST /places/organize", postOrganize)
	registerPlacesRoute("GET /places/organize", getOrganize)
	registerPlacesRoute("POST /places/organize/cancel", postOrganizeCancel)
	registerPlacesRoute("GET /places/tree", getPlaceTree)
	registerPlacesRoute("POST /places/guess", postGuess)
	registerPlacesRoute("POST /places/steer", postSteer)
	registerPlacesRoute("GET /places/{id}/lenses", getLenses)
}

// Organizer is the desktop's hold on one placeorg run. The bridge is the only
// caller: it builds the corpus and the existing tree, and it is the only
// place that turns a Plan into a store commit. A nil Organizer leaves the
// routes absent (404), the same way a tool with nothing behind it is left
// off the belt.
type Organizer struct {
	Stages  placeorg.Stages
	State   *placeorg.State
	Journal placeorg.Journal
	Primary placeorg.Embedder
	Local   placeorg.Embedder
	// Run, when set, replaces the pipeline. Tests hand back a fixed plan so
	// the wire can be asserted without a library.
	Run func(context.Context, *placeorg.Corpus, placeorg.Options) (*placeorg.Plan, error)
	// Ask, when set, is the model door for one filing or one steer. Nil uses
	// the advice's own door, and a nil door is the stage's fallback.
	Ask func() placegraph.Asker

	dir        string
	statePath  string
	reviewPath string
	stream     *stream.Stream
	steer      *steer.Steerer
	correct    *correct.Corrector
	mem        *memVectors

	mu           sync.Mutex
	running      bool
	runID        string
	cancel       context.CancelFunc
	progress     placeorg.Progress
	haveProgress bool
	held         *heldPlan
	last         *placeorg.RunSummary
}

// heldPlan is the offer the person is reviewing. Rows are what GET returns
// and what skip indexes count; each row's store ops are what Accept applies,
// in order, as one commit.
type heldPlan struct {
	ID          string                     `json:"id"`
	Summary     string                     `json:"summary"`
	Provisional bool                       `json:"provisional"`
	Invariants  []placeorg.InvariantResult `json:"invariants,omitempty"`
	Rows        []heldRow                  `json:"rows"`
}

type heldRow struct {
	Wire   opWire              `json:"wire"`
	Store  []placegraph.PlanOp `json:"store,omitempty"`
	Memory *placeorg.Op        `json:"memory,omitempty"`
}

type planWire struct {
	ID          string                     `json:"id"`
	Summary     string                     `json:"summary"`
	Ops         []opWire                   `json:"ops"`
	Provisional bool                       `json:"provisional"`
	Invariants  []placeorg.InvariantResult `json:"invariants,omitempty"`
}

type opWire struct {
	Kind     string `json:"kind"`
	Why      string `json:"why"`
	Name     string `json:"name,omitempty"`
	Proposed bool   `json:"proposed"`
}

type organizeWire struct {
	Running  bool                 `json:"running"`
	Progress *placeorg.Progress   `json:"progress,omitempty"`
	Last     *placeorg.RunSummary `json:"last,omitempty"`
	Plan     *planWire            `json:"plan,omitempty"`
}

// memVectors is the correction channel's view of vectors this process has
// already embedded. A chat without one is simply not a neighbour.
type memVectors struct {
	mu sync.Mutex
	v  map[string][]float32
}

func (m *memVectors) Vector(id string) ([]float32, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.v[id]
	return append([]float32(nil), v...), ok && len(v) > 0
}

func (m *memVectors) put(id string, v []float32) {
	if m == nil || id == "" || len(v) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.v == nil {
		m.v = map[string][]float32{}
	}
	m.v[id] = append([]float32(nil), v...)
}

// WireOrganizer attaches the runner to advice, reading and writing its files
// beside the place graph. A missing organiser file is a new organiser. The
// local embedder is the one compiled into the binary; when that table cannot
// be read, the hashed floor stands in so a guess can still answer from a folder.
func WireOrganizer(a *PlaceAdvice, dir string) error {
	if a == nil {
		return errors.New("places organize: no advice")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("places organize: %w", err)
	}
	state, _, err := placeorg.LoadState(filepath.Join(dir, "places-org.json"), time.Now)
	if err != nil {
		return fmt.Errorf("places organize: %w", err)
	}
	j, err := journal.Open(dir)
	if err != nil {
		return fmt.Errorf("places organize: %w", err)
	}
	var local placeorg.Embedder = localembed.NewHashed()
	if static, err := localembed.NewStatic(); err == nil {
		local = static
	}
	st := stream.New()
	mem := &memVectors{}
	space := state.Space
	if space == "" {
		space = placeorg.SpacePrimary
	}
	cr := correct.New(space, mem, state.Exemplars)
	sr := steer.New()
	org := &Organizer{
		Stages: placeorg.Stages{
			Dedup: dedup.New(placeorg.DefaultSeed), Kind: kind.New(), Anchors: anchors.Derived{},
			Vectors: vectors.New(dir), Neighbours: knn.New(knn.DefaultK, knn.DefaultFloor),
			Communities: community.New(), Nest: nest.New(), Audit: audit.Auditor{}, Park: orgpark.New(),
			Name: &name.Stage{}, Review: &review.Stage{}, Link: link.New(state), Stream: st,
			Steer: sr, Correct: cr,
		},
		State: state, Journal: j, Primary: a.organizerEmbed(), Local: local,
		dir: dir, statePath: filepath.Join(dir, "places-org.json"),
		reviewPath: filepath.Join(dir, "places-review.json"),
		stream:     st, steer: sr, correct: cr, mem: mem,
	}
	if state.LastRun != nil {
		org.last = state.LastRun
	}
	org.loadReview()
	a.Organizer = org
	return nil
}

func (o *Organizer) loadReview() {
	if o == nil || o.reviewPath == "" {
		return
	}
	raw, err := os.ReadFile(o.reviewPath)
	if err != nil {
		return
	}
	var held heldPlan
	if json.Unmarshal(raw, &held) != nil || held.ID == "" {
		return
	}
	o.held = &held
}

func (o *Organizer) saveReview() {
	if o == nil || o.reviewPath == "" {
		return
	}
	if o.held == nil {
		_ = os.Remove(o.reviewPath)
		return
	}
	raw, err := json.MarshalIndent(o.held, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(o.reviewPath, raw, 0o600)
}

func (o *Organizer) runningNow() (bool, string) {
	if o == nil {
		return false, ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running, o.runID
}

// start begins one run. False means one is already going; the caller answers
// 409 on the organize route and leaves the idle pass queued as it was.
func (o *Organizer) start(a *PlaceAdvice, parent context.Context, mode placeorg.Mode) (bool, string) {
	if o == nil || a == nil {
		return false, ""
	}
	if mode == "" {
		mode = placeorg.ModeAddOn
	}
	o.mu.Lock()
	if o.running {
		id := o.runID
		o.mu.Unlock()
		return false, id
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	id := newID("run-")
	o.running = true
	o.runID = id
	o.cancel = cancel
	o.haveProgress = true
	o.progress = placeorg.Progress{Run: id, Cancellable: true, At: time.Now().UTC()}
	o.mu.Unlock()
	o.publish(a, o.progress)
	go o.execute(ctx, a, mode, id)
	return true, id
}

// stop cancels the run that is going. A run that is not going reports false
// and changes nothing: cancel is idempotent.
func (o *Organizer) stop() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	cancel, running := o.cancel, o.running
	o.mu.Unlock()
	if !running || cancel == nil {
		return false
	}
	cancel()
	return true
}

func (o *Organizer) publish(a *PlaceAdvice, p placeorg.Progress) {
	if a == nil || a.b == nil {
		return
	}
	a.b.worldFeed().Publish("placesOrganizing", p)
}

func (o *Organizer) execute(ctx context.Context, a *PlaceAdvice, mode placeorg.Mode, id string) {
	defer func() {
		o.mu.Lock()
		o.running = false
		o.cancel = nil
		o.mu.Unlock()
	}()
	corpus := a.corpus()
	existing, err := a.existingTree()
	var plan *placeorg.Plan
	if err == nil {
		opts := placeorg.Options{
			Mode: mode, Seed: placeorg.DefaultSeed, Existing: existing,
			Budget: placeorg.Budget{
				Calls: a.policy().OrganizeCallsPerRun,
				USD:   float64(a.policy().OrganizeUSDPerRun) / 100,
			},
			Progress: func(p placeorg.Progress) {
				if p.Run == "" {
					p.Run = id
				}
				p.Cancellable = true
				o.mu.Lock()
				o.progress, o.haveProgress = p, true
				o.mu.Unlock()
				o.publish(a, p)
			},
			Now: a.now,
		}
		if o.Run != nil {
			plan, err = o.Run(ctx, corpus, opts)
		} else {
			runner := &placeorg.Runner{
				Stages: o.Stages, Journal: o.Journal, State: o.State,
				Ask: a.organizerAsk(), Primary: o.Primary, Local: o.Local,
			}
			plan, err = runner.Run(ctx, corpus, opts)
		}
	}
	if err != nil || ctx.Err() != nil {
		o.finishCancelled(a, id)
		return
	}
	o.finishPlan(a, plan)
}

func (o *Organizer) finishCancelled(a *PlaceAdvice, id string) {
	summary := &placeorg.RunSummary{Plan: id, Outcome: "cancelled", Ended: time.Now().UTC()}
	o.mu.Lock()
	if o.last != nil && o.last.Summary != "" {
		summary.Summary = o.last.Summary
	}
	o.last = summary
	if o.State != nil {
		o.State.LastRun = summary
	}
	o.haveProgress = false
	o.mu.Unlock()
	o.persist(a)
	if o.Journal != nil {
		_ = o.Journal.Write(placeorg.Entry{At: time.Now().UTC(), Run: id, Kind: placeorg.EntryRunEnded, Note: "cancelled"})
	}
}

func (o *Organizer) finishPlan(a *PlaceAdvice, plan *placeorg.Plan) {
	if plan == nil {
		o.finishCancelled(a, "")
		return
	}
	snap, _ := a.snapshot()
	pol := a.policy()
	var rows []heldRow
	var immediate []placegraph.PlanOp
	for _, op := range plan.Ops {
		switch op.Kind {
		case placeorg.OpLens, placeorg.OpExemplar:
			o.remember(op)
			continue
		case placeorg.OpAxis:
			if !op.Proposed {
				o.remember(op)
				continue
			}
		case placeorg.OpInbox:
			continue
		case placeorg.OpFile, placeorg.OpPark:
			if !op.Proposed && pol.AutoFile {
				if row, ok := expandOp(op, snap); ok {
					immediate = append(immediate, row.Store...)
				}
				continue
			}
		}
		if row, ok := expandOp(op, snap); ok {
			rows = append(rows, row)
		}
	}
	if len(immediate) > 0 {
		if _, err := a.store().ApplyPlan(immediate); err != nil && len(rows) == 0 {
			// A filing that could not land is still something the person can accept.
			rows = append(rows, heldRow{Wire: opWire{Kind: "file", Why: "File a chat", Proposed: true}, Store: immediate})
		}
	}
	summary := &placeorg.RunSummary{
		Plan: plan.ID, Mode: plan.Mode, Space: plan.Space, Started: plan.Started, Ended: time.Now().UTC(),
		Summary: plan.Summary, Spent: plan.Spent,
	}
	if summary.Plan == "" {
		summary.Plan = o.runID
	}
	o.mu.Lock()
	if len(rows) > 0 {
		summary.Outcome = "offered"
		o.held = &heldPlan{
			ID: newPlanID(), Summary: plan.Summary, Provisional: plan.Provisional,
			Invariants: plan.Invariants, Rows: rows,
		}
	} else {
		summary.Outcome = "applied"
		o.held = nil
	}
	o.last = summary
	if o.State != nil {
		o.State.LastRun = summary
		if plan.Space != "" {
			o.State.Space = plan.Space
		}
	}
	o.haveProgress = false
	o.mu.Unlock()
	o.syncStream()
	o.persist(a)
}

func (o *Organizer) persist(a *PlaceAdvice) {
	if o.State != nil && o.statePath != "" {
		_ = o.State.Save(o.statePath)
	}
	o.mu.Lock()
	o.saveReview()
	o.mu.Unlock()
	_ = a
}

func (o *Organizer) syncStream() {
	if o == nil || o.stream == nil || o.State == nil {
		return
	}
	if o.State.Space != "" {
		o.stream.Space = o.State.Space
	}
	if th, ok := o.State.Thresholds[o.stream.Space]; ok {
		o.stream.Thresholds = th
	}
	o.stream.Kinds = o.State.Kinds
	o.stream.Representative = o.State.Representative
	if o.Local != nil && o.Local.Space() == placeorg.SpaceLocal {
		o.stream.Embed = o.Local
	}
}

func (o *Organizer) snapshotWire() organizeWire {
	out := organizeWire{}
	if o == nil {
		return out
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	out.Running = o.running
	if o.haveProgress && o.progress.Run != "" {
		p := o.progress
		out.Progress = &p
	}
	if o.last != nil && (o.last.Summary != "" || o.last.Outcome != "") {
		last := *o.last
		out.Last = &last
	}
	if !o.running && o.held != nil {
		out.Plan = o.held.wire()
	}
	return out
}

func (h *heldPlan) wire() *planWire {
	if h == nil {
		return nil
	}
	ops := make([]opWire, len(h.Rows))
	for i, row := range h.Rows {
		ops[i] = row.Wire
	}
	return &planWire{ID: h.ID, Summary: h.Summary, Ops: ops, Provisional: h.Provisional, Invariants: h.Invariants}
}

func (p *Places) organizer() *Organizer {
	if p == nil || p.advice == nil {
		return nil
	}
	return p.advice.Organizer
}

func postOrganize(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
	if !needPost(w, r) {
		return
	}
	org := p.organizer()
	if org == nil {
		fail(w, 404, "unknown engine action")
		return
	}
	var ask struct {
		Mode string `json:"mode"`
	}
	if !readBody(w, r, &ask) {
		return
	}
	mode := placeorg.Mode(ask.Mode)
	if mode == "" {
		mode = placeorg.ModeAddOn
	}
	if mode != placeorg.ModeAddOn && mode != placeorg.ModeRefresh {
		failPlaces(w, 400, "invalid", "Organize is add-on or refresh.")
		return
	}
	parent := p.advice.ctx
	ok, id := org.start(p.advice, parent, mode)
	if !ok {
		writeStatus(w, 409, map[string]any{
			"error": "Places are already being organized.",
			"code":  "running",
			"run":   id,
		})
		return
	}
	write(w, map[string]any{"run": id, "started": true})
}

func getOrganize(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
	if !needGet(w, r) {
		return
	}
	org := p.organizer()
	if org == nil {
		fail(w, 404, "unknown engine action")
		return
	}
	write(w, org.snapshotWire())
}

func postOrganizeCancel(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
	if !needPost(w, r) {
		return
	}
	org := p.organizer()
	if org == nil {
		fail(w, 404, "unknown engine action")
		return
	}
	if !readBody(w, r, &struct{}{}) {
		return
	}
	write(w, map[string]bool{"cancelled": org.stop()})
}

// takeOrganizerPlan accepts a plan this bridge is holding. handled is false
// when id is an ordinary filing offer, so the caller falls through to the ledger.
func (a *PlaceAdvice) takeOrganizerPlan(id string, skip []int) (placegraph.Receipt, bool, error) {
	o := a.Organizer
	if o == nil {
		return placegraph.Receipt{}, false, nil
	}
	o.mu.Lock()
	held := o.held
	running := o.running
	o.mu.Unlock()
	if held == nil || held.ID != id {
		return placegraph.Receipt{}, false, nil
	}
	if running {
		return placegraph.Receipt{}, true, errors.New("places are still being organized")
	}
	dropped := map[int]bool{}
	for _, i := range skip {
		if i < 0 || i >= len(held.Rows) {
			return placegraph.Receipt{}, true, fmt.Errorf("%w: that skip is not a change in the plan", placegraph.ErrInvalid)
		}
		dropped[i] = true
	}
	var ops []placegraph.PlanOp
	var memory []placeorg.Op
	for i, row := range held.Rows {
		if dropped[i] {
			continue
		}
		ops = append(ops, row.Store...)
		if row.Memory != nil {
			memory = append(memory, *row.Memory)
		}
	}
	if len(ops) == 0 {
		return placegraph.Receipt{}, true, fmt.Errorf("%w: nothing left in that plan changes a place", placegraph.ErrInvalid)
	}
	receipt, err := a.store().ApplyPlan(ops)
	if err != nil {
		return placegraph.Receipt{}, true, err
	}
	if receipt.ID == "" {
		return placegraph.Receipt{}, true, fmt.Errorf("%w: nothing in that plan was still waiting", placegraph.ErrProposalGone)
	}
	for _, op := range memory {
		o.remember(op)
	}
	o.mu.Lock()
	o.held = nil
	if o.last != nil {
		o.last.Outcome = "applied"
		if o.State != nil {
			o.State.LastRun = o.last
		}
	}
	o.saveReview()
	o.mu.Unlock()
	if o.State != nil && o.statePath != "" {
		_ = o.State.Save(o.statePath)
	}
	return receipt, true, nil
}

// dropOrganizerPlan declines a held plan. False means id is not one of ours.
func (a *PlaceAdvice) dropOrganizerPlan(id string) bool {
	o := a.Organizer
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.held == nil || o.held.ID != id {
		return false
	}
	o.held = nil
	if o.last != nil {
		o.last.Outcome = "declined"
		if o.State != nil {
			o.State.LastRun = o.last
		}
	}
	o.saveReview()
	if o.State != nil && o.statePath != "" {
		_ = o.State.Save(o.statePath)
	}
	return true
}

func (o *Organizer) remember(op placeorg.Op) {
	if o == nil || o.State == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.State.Lenses == nil {
		o.State.Lenses = map[string][]placeorg.Lens{}
	}
	if o.State.Axes == nil {
		o.State.Axes = map[string]placeorg.Axis{}
	}
	switch op.Kind {
	case placeorg.OpLens:
		axis := op.Into
		if axis == "" && op.Name == "by month" {
			axis = "month"
		}
		o.State.Lenses[op.Place] = append(o.State.Lenses[op.Place], placeorg.Lens{
			Name: op.Name, Axis: axis, Value: op.Name, Members: append([]string(nil), op.Members...),
		})
	case placeorg.OpAxis:
		if op.Name == "" {
			delete(o.State.Axes, op.Place)
			return
		}
		o.State.Axes[op.Place] = placeorg.Axis{Kind: placeorg.AxisKind(op.Name), Values: append([]string(nil), op.Children...)}
	case placeorg.OpExemplar:
		o.State.Exemplars = append(o.State.Exemplars, placeorg.Exemplar{Chat: op.Chat, To: op.Place, At: time.Now().UTC()})
	case placeorg.OpConstraint:
		o.State.Constraints = append(o.State.Constraints, placeorg.Constraint{Anchor: op.Name, Place: op.Place, At: time.Now().UTC()})
	}
}

func (a *PlaceAdvice) addExemplar(chat, from, to string) {
	o := a.Organizer
	if o == nil || o.State == nil || chat == "" || to == "" {
		return
	}
	o.mu.Lock()
	o.State.Exemplars = append(o.State.Exemplars, placeorg.Exemplar{Chat: chat, From: from, To: to, At: a.now().UTC()})
	o.mu.Unlock()
	if o.statePath != "" {
		_ = o.State.Save(o.statePath)
	}
}

// noteHandMove teaches the organiser from a move the person made. A removal
// has nowhere to be an exemplar, so it is not one. A failure here never
// fails the move the person already made.
func (a *PlaceAdvice) noteHandMove(chat, from, to string) {
	o := a.Organizer
	if o == nil || o.correct == nil || to == "" || chat == "" {
		return
	}
	tree, err := a.existingTree()
	if err != nil {
		return
	}
	ops, err := o.correct.Correct(context.Background(), tree, a.corpus(), placeorg.Correction{
		Chat: chat, From: from, To: to, At: a.now(),
	})
	if err != nil {
		return
	}
	a.addExemplar(chat, from, to)
	for _, op := range ops {
		noted := op
		if o.Journal != nil {
			_ = o.Journal.Write(placeorg.Entry{At: a.now().UTC(), Kind: placeorg.EntryCorrection, Op: &noted, Note: op.Why})
		}
		if op.Kind != placeorg.OpConstraint {
			continue
		}
		o.mu.Lock()
		running := o.running
		o.mu.Unlock()
		if running {
			continue
		}
		row := heldRow{Wire: opWire{Kind: string(op.Kind), Why: op.Why, Name: op.Name, Proposed: true}, Memory: &noted}
		o.mu.Lock()
		if o.held == nil {
			o.held = &heldPlan{ID: newPlanID(), Summary: op.Why, Rows: []heldRow{row}}
		} else {
			o.held.Rows = append(o.held.Rows, row)
		}
		o.saveReview()
		o.mu.Unlock()
	}
}

func (a *PlaceAdvice) store() *placegraph.Store {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	return a.b.places.Store
}

func (a *PlaceAdvice) snapshot() (*placegraph.Snapshot, error) {
	return a.store().Snapshot()
}

func (a *PlaceAdvice) corpus() *placeorg.Corpus {
	lib := a.library()
	chats := make([]placeorg.Chat, 0, len(lib))
	for _, e := range lib {
		c := placeorg.FromEvidence(e, "", e.UpdatedAt)
		c.Text = organizerText(e)
		chats = append(chats, c)
	}
	return &placeorg.Corpus{Chats: chats}
}

func organizerText(e placegraph.ChatEvidence) string {
	title := strings.TrimSpace(e.Title)
	msg := clipRunes(strings.TrimSpace(e.FirstMessage), placeorg.TextRunes)
	switch {
	case title == "" && msg == "":
		return ""
	case title == "":
		return msg
	case msg == "":
		return clipRunes(title, placeorg.TextRunes)
	default:
		return clipRunes(title+"\n"+msg, placeorg.TextRunes)
	}
}

// existingTree is the store as the runner's add-on input. One chat has one
// home: the deepest place it is filed in, not a shortcut. A tie keeps the
// membership that was filed first. Places the ledger records as made by the
// organiser are not person-made, so a refresh may dissolve them.
func (a *PlaceAdvice) existingTree() (*placeorg.Tree, error) {
	snap, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	ai := map[string]bool{}
	if a.Ledger != nil {
		if ids, err := a.Ledger.AIPlaceIDs(); err == nil {
			for _, id := range ids {
				ai[id] = true
			}
		}
	}
	active := map[string]placegraph.Place{}
	for _, p := range snap.Places {
		if !p.Archived {
			active[p.ID] = p
		}
	}
	parentOf := map[string]string{}
	for id, p := range active {
		parent := ""
		if len(p.Parents) > 0 {
			if _, ok := active[p.Parents[0]]; ok && p.Parents[0] != placegraph.RootID && p.Parents[0] != placegraph.NowID {
				parent = p.Parents[0]
			}
		}
		parentOf[id] = parent
	}
	depth := map[string]int{}
	var depthOf func(string, map[string]bool) int
	depthOf = func(id string, seen map[string]bool) int {
		if d, ok := depth[id]; ok {
			return d
		}
		if seen[id] {
			return 1
		}
		seen[id] = true
		if parentOf[id] == "" {
			depth[id] = 1
			return 1
		}
		depth[id] = depthOf(parentOf[id], seen) + 1
		return depth[id]
	}
	for id := range active {
		depthOf(id, map[string]bool{})
	}
	homes := map[string]string{}
	homeDepth := map[string]int{}
	for _, m := range snap.Memberships {
		if m.Link {
			continue
		}
		d, ok := depth[m.PlaceID]
		if !ok {
			continue
		}
		if _, seen := homes[m.ChatID]; !seen || d > homeDepth[m.ChatID] {
			homes[m.ChatID] = m.PlaceID
			homeDepth[m.ChatID] = d
		}
	}
	members := map[string][]string{}
	for chat, place := range homes {
		members[place] = append(members[place], chat)
	}
	nodes := make([]placeorg.BuiltNode, 0, len(active))
	for id, p := range active {
		list := members[id]
		sort.Strings(list)
		nodes = append(nodes, placeorg.BuiltNode{
			ID: id, Parent: parentOf[id], Name: p.Name, Depth: depth[id],
			Person: !ai[id], StoreID: id, Members: list,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return placeorg.Assemble(nodes, homes, nil)
}

func expandOp(op placeorg.Op, snap *placegraph.Snapshot) (heldRow, bool) {
	wire := opWire{Kind: string(op.Kind), Why: op.Why, Name: op.Name, Proposed: op.Proposed || op.Kind != placeorg.OpFile}
	if op.Kind == placeorg.OpFile || op.Kind == placeorg.OpPark {
		wire.Proposed = op.Proposed
	}
	switch op.Kind {
	case placeorg.OpBirth:
		id := op.Place
		if id == "" {
			id = splitID("birth", op.Name)
		}
		ops := []placegraph.PlanOp{{Kind: placegraph.PlanCreate, PlaceID: id, Name: op.Name, Parents: planParents(op.Parent)}}
		for _, chat := range op.Members {
			ops = append(ops, placegraph.PlanOp{Kind: placegraph.PlanFile, PlaceID: id, ChatID: chat})
		}
		return heldRow{Wire: wire, Store: ops}, op.Name != ""
	case placeorg.OpName, placeorg.OpRename:
		wire.Kind = string(op.Kind)
		return heldRow{Wire: wire, Store: []placegraph.PlanOp{{Kind: placegraph.PlanRename, PlaceID: op.Place, Name: op.Name}}}, op.Place != "" && op.Name != ""
	case placeorg.OpFile, placeorg.OpPark:
		chats := chatsOf(op)
		var ops []placegraph.PlanOp
		for _, chat := range chats {
			ops = append(ops, placegraph.PlanOp{Kind: placegraph.PlanFile, PlaceID: op.Place, ChatID: chat})
		}
		return heldRow{Wire: wire, Store: ops}, len(ops) > 0 && op.Place != ""
	case placeorg.OpLink:
		return heldRow{Wire: wire, Store: []placegraph.PlanOp{{Kind: placegraph.PlanLink, PlaceID: op.Place, ChatID: op.Chat}}}, op.Place != "" && op.Chat != ""
	case placeorg.OpMerge:
		return heldRow{Wire: wire, Store: []placegraph.PlanOp{{Kind: placegraph.PlanMerge, FromPlaceID: op.Place, PlaceID: op.Into}}}, op.Place != "" && op.Into != ""
	case placeorg.OpDissolve, placeorg.OpCollapse, placeorg.OpDemote:
		return heldRow{Wire: wire, Store: dissolveOps(op, snap)}, op.Place != ""
	case placeorg.OpSplit:
		id := splitID(op.Place, op.Name)
		ops := []placegraph.PlanOp{{Kind: placegraph.PlanCreate, PlaceID: id, Name: op.Name, Parents: planParents(op.Place)}}
		for _, chat := range op.Members {
			ops = append(ops, placegraph.PlanOp{Kind: placegraph.PlanMove, PlaceID: id, FromPlaceID: op.Place, ChatID: chat})
		}
		return heldRow{Wire: wire, Store: ops}, op.Name != "" && op.Place != ""
	case placeorg.OpInterpose:
		ops := []placegraph.PlanOp{{Kind: placegraph.PlanCreate, PlaceID: op.Place, Name: op.Name, Parents: planParents(op.Parent)}}
		for _, child := range op.Children {
			ops = append(ops, placegraph.PlanOp{Kind: placegraph.PlanReparent, PlaceID: child, Parents: planParents(op.Place)})
		}
		return heldRow{Wire: wire, Store: ops}, op.Place != "" && op.Name != ""
	case placeorg.OpAxis, placeorg.OpConstraint:
		noted := op
		return heldRow{Wire: wire, Memory: &noted}, op.Why != "" || op.Name != ""
	default:
		if op.Why == "" {
			return heldRow{}, false
		}
		return heldRow{Wire: wire}, true
	}
}

func chatsOf(op placeorg.Op) []string {
	if op.Chat != "" {
		return []string{op.Chat}
	}
	return append([]string(nil), op.Members...)
}

func dissolveOps(op placeorg.Op, snap *placegraph.Snapshot) []placegraph.PlanOp {
	parent := op.Parent
	var members []string
	if snap != nil {
		if p, ok := snap.Place(op.Place); ok && parent == "" && len(p.Parents) > 0 {
			parent = p.Parents[0]
		}
		for _, id := range snap.ChatsIn(op.Place, false) {
			home := false
			for _, m := range snap.PlacesOf(id) {
				if m.PlaceID == op.Place && !m.Link {
					home = true
				}
			}
			if home {
				members = append(members, id)
			}
		}
	}
	dest := parent
	if dest == "" || dest == placegraph.RootID {
		dest = placegraph.NowID
	}
	var ops []placegraph.PlanOp
	for _, chat := range members {
		ops = append(ops, placegraph.PlanOp{Kind: placegraph.PlanMove, PlaceID: dest, FromPlaceID: op.Place, ChatID: chat})
	}
	ops = append(ops, placegraph.PlanOp{Kind: placegraph.PlanDelete, PlaceID: op.Place})
	return ops
}

func planParents(id string) []string {
	if id == "" || id == placegraph.NowID || id == placegraph.RootID {
		return nil
	}
	return []string{id}
}

func splitID(parent, name string) string {
	sum := sha256.Sum256([]byte(parent + "\n" + name))
	return "split-" + hex.EncodeToString(sum[:8])
}

func newPlanID() string {
	return "prop_" + randomHex(8)
}

func newID(prefix string) string {
	return prefix + randomHex(8)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// doorJudge is the one-off Judge for a filing or a steer. It cannot fail:
// no model, a transport error, or an answer that is not one of the options
// is the fallback the stage already named.
type doorJudge struct{ ask placegraph.Asker }

func (j doorJudge) Choose(ctx context.Context, q placeorg.Question, fallback placeorg.Option) placeorg.Pick {
	if j.ask == nil || ctx.Err() != nil {
		return placeorg.Pick{Key: fallback.Key, FellBack: true, Why: "no model"}
	}
	raw, err := j.ask(ctx, placegraph.ModelRequest{Role: q.Role, System: q.Purpose, User: q.Context})
	if err != nil {
		return placeorg.Pick{Key: fallback.Key, FellBack: true, Why: "no answer"}
	}
	key := strings.Trim(strings.TrimSpace(raw), `"'`)
	for _, o := range q.Options {
		if strings.EqualFold(key, o.Key) {
			return placeorg.Pick{Key: o.Key}
		}
	}
	return placeorg.Pick{Key: fallback.Key, FellBack: true, Why: "answer outside the contract"}
}

func (j doorJudge) Write(ctx context.Context, q placeorg.Question, valid placeorg.Validator, fallback string) placeorg.Pick {
	accept := func(raw string, fell bool, why string) placeorg.Pick {
		text, ok := valid(raw)
		if !ok {
			text, _ = valid(fallback)
			fell, why = true, "answer outside the contract"
		}
		return placeorg.Pick{Written: text, FellBack: fell, Why: why}
	}
	if j.ask == nil || ctx.Err() != nil {
		return accept(fallback, true, "no model")
	}
	raw, err := j.ask(ctx, placegraph.ModelRequest{Role: q.Role, System: q.Purpose, User: q.Context})
	if err != nil {
		return accept(fallback, true, "no answer")
	}
	return accept(raw, false, "")
}

func (a *PlaceAdvice) judge() placeorg.Judge {
	var ask placegraph.Asker
	if a != nil && a.Organizer != nil && a.Organizer.Ask != nil {
		ask = a.Organizer.Ask()
	} else if a != nil {
		ask = a.organizerAsk()
	}
	return doorJudge{ask: ask}
}

func (a *PlaceAdvice) organizerAsk() placegraph.Asker {
	if ask := a.askVia(a.b.askingConversation()); ask != nil {
		return ask
	}
	return a.askThrough(a.detachedAsker(), "saved")
}

func (a *PlaceAdvice) organizerEmbed() placeorg.Embedder {
	return a.embedThrough(func(ctx context.Context) (placeAskDoor, error) {
		if s := a.b.askingConversation(); s != nil {
			if door, ok := conversationAsks(s); ok {
				return door, nil
			}
		}
		find := a.detachedAsker()
		if find == nil {
			return nil, errors.New("no engine for places embeddings")
		}
		return find(ctx)
	})
}
