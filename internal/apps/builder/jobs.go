// Package builder keeps durable app editing jobs and gives their model only
// definition files, the kit, public type shapes, and check reports.
package builder

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

const (
	Planning  = "planning"
	Writing   = "writing"
	Checking  = "checking"
	Ready     = "ready"
	NeedsYou  = "needs_you"
	Failed    = "failed"
	maxRounds = 3
)

type Options struct {
	Compiler      Compiler
	ValidateTypes func([]byte) error

	CanLook func() bool
	Look    func(context.Context, string, string) ([]string, error)

	Home         string
	Store        *store.Store
	Model        func() (Completer, error)
	Catalog      func(app string) (Catalog, error)
	Check        func(context.Context, string, schema.Definition, schema.TypeSet) schema.Report
	CheckPackage func(context.Context, string, schema.Definition, schema.TypeSet) schema.Report
	SeedPreview  func(scope string, def schema.Definition, real bool) error
	ClosePreview func(scope string)
	Changed      func(Event)
	Notify       func(Summary)
	Proposed     func(Proposal)
}

// Proposal identifies a requested change after its work is durable.
type Proposal struct {
	App          string       `json:"app"`
	Draft        string       `json:"draft"`
	ID           string       `json:"proposal_id"`
	Screen       string       `json:"screen"`
	Kind         string       `json:"kind"`
	Source       string       `json:"source"`
	BaseVersion  int          `json:"base_version"`
	Checkpoint   string       `json:"checkpoint"`
	Target       *Target      `json:"target,omitempty"`
	SummaryLines []string     `json:"summary_lines"`
	Package      *PackageCard `json:"package,omitempty"`
}

type proposalIntent struct {
	ID     string  `json:"id"`
	Target *Target `json:"target,omitempty"`
}

// Catalog carries shape and counts only. The model adapter projects the
// fields again so validation metadata such as expressions and defaults stay local.
type Catalog struct {
	Types      schema.TypeSet
	Counts     map[string]int
	TypeLabels map[string]string
	AppNames   map[string]string
	Origins    map[string]string
}

type Jobs struct {
	author authoring

	opts      Options
	root      string
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	packageMu sync.Mutex
	workers   map[string]context.CancelFunc
	scopes    map[string]previewScope
	changes   chan struct{}
	wg        sync.WaitGroup
	lease     *os.File
}

type Summary struct {
	Draft    string    `json:"draft"`
	App      string    `json:"app,omitempty"`
	Name     string    `json:"name"`
	State    string    `json:"state"`
	Proposal *Proposal `json:"proposal,omitempty"`
}

type State struct {
	Marks    []Mark    `json:"marks"`
	Presence *Presence `json:"presence,omitempty"`

	Summary
	Published   bool            `json:"published,omitempty"`
	Revision    uint64          `json:"revision"`
	Plan        json.RawMessage `json:"plan,omitempty"`
	Needs       []string        `json:"needs"`
	Offers      []Offer         `json:"offers"`
	Using       []string        `json:"using"`
	Transcript  []Message       `json:"transcript"`
	Checkpoints []Checkpoint    `json:"checkpoints"`
	Screens     []string        `json:"screens"`
	Progress    []Event         `json:"progress"`
	Problem     string          `json:"problem,omitempty"`
}

type Event struct {
	Draft  string         `json:"draft"`
	Stage  string         `json:"stage"`
	Round  int            `json:"round"`
	Line   string         `json:"line"`
	OK     *bool          `json:"ok,omitempty"`
	Report *schema.Report `json:"report,omitempty"`
}

type Offer struct {
	Common string `json:"common,omitempty"`
	Line   string `json:"line,omitempty"`
	Type   string `json:"type"`
	Count  int    `json:"count"`
	App    string `json:"app"`
}

type Message struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	Text       string `json:"text"`
	Checkpoint string `json:"checkpoint,omitempty"`
}

type Checkpoint struct {
	ID      string   `json:"id"`
	Summary string   `json:"summary"`
	Screens []string `json:"screens"`
}

// AppBuildTarget is the address of an element in an app preview.
type AppBuildTarget struct {
	Node       string `json:"node,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`

	Screen string `json:"screen"`
	Path   []any  `json:"path"`
}

// Target is the app authoring address used by job callers.
type Target = AppBuildTarget

type PlanRequest struct {
	Ask     string `json:"ask,omitempty"`
	Draft   string `json:"draft,omitempty"`
	Request string `json:"request,omitempty"`
}

type PlanResult struct {
	Draft  string          `json:"draft"`
	Plan   json.RawMessage `json:"plan"`
	Needs  []string        `json:"needs"`
	Offers []Offer         `json:"offers"`
}

type turn struct {
	DeskReuse bool `json:",omitempty"`
	CheckOnly bool
	ID        string
	Stage     string
	Request   string
	Proposal  string
	Target    *Target
	Round     int
	Report    *schema.Report
	Reply     string
}

type snapshot struct {
	Files         map[string]string
	Passed        bool
	CheckComplete bool
	Look          string
}

type job struct {
	State
	PendingProposal *proposalIntent `json:",omitempty"`
	Package         *packageEdit    `json:",omitempty"`
	WritingFiles    map[string]string
	Working         snapshot
	Saved           map[string]snapshot
	Queue           []turn
	Paused          *turn `json:",omitempty"`
	Base            int
	Dirty           bool
	Editable        bool
	Publishing      bool
	KeptMessages    int
	KeptCheckpoints int
	KeptMarks       int
	LastRequest     string
	Accepted        []string
}

// OpenJobs resumes work waiting from an earlier engine process.
func OpenJobs(opts Options) (*Jobs, error) {
	if opts.Store == nil {
		opts.Store = store.NewAt(opts.Home, nil)
	}
	if opts.Home == "" || opts.Model == nil {
		return nil, store.ErrBadInput
	}
	if err := opts.Store.Prepare(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Jobs{
		opts: opts, author: authoring{compiler: opts.Compiler, validateTypes: opts.ValidateTypes}, root: filepath.Join(opts.Home, "apps", "_drafts"), ctx: ctx, cancel: cancel,
		workers: map[string]context.CancelFunc{}, scopes: map[string]previewScope{}, changes: make(chan struct{}),
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		cancel()
		return nil, err
	}
	lease, err := os.OpenFile(filepath.Join(s.root, ".builder.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := filelock.Lock(lease, true, true); err != nil {
		_ = lease.Close()
		cancel()
		return nil, err
	}
	s.lease = lease
	s.mu.Lock()
	s.prunePreviews()
	jobs, err := s.jobs()
	if err == nil {
		for _, j := range jobs {
			s.start(j)
			if j.Dirty && (j.State.State == Ready || j.State.State == NeedsYou) && opts.Notify != nil {
				opts.Notify(j.Summary)
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Close interrupts model calls and leaves their turns queued for the next open.
func (s *Jobs) Close() {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	for scope := range s.scopes {
		s.removeScope(scope)
	}
	if s.lease != nil {
		_ = filelock.Unlock(s.lease)
		_ = s.lease.Close()
		s.lease = nil
	}
}

func (s *Jobs) jobs() ([]*job, error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*job
	for _, entry := range entries {
		if !entry.IsDir() || !cell.ValidID(entry.Name()) {
			continue
		}
		j, err := s.load(entry.Name())
		if err == nil {
			out = append(out, j)
		}
	}
	return out, nil
}

// Each preview directory is scratch. A marker lets the next engine remove
// leftover scopes from a quit that could not finish closing them.
func (s *Jobs) prunePreviews() {
	entries, _ := os.ReadDir(s.root)
	for _, entry := range entries {
		id := entry.Name()
		if entry.IsDir() && cell.ValidID(id) {
			if _, err := os.Stat(filepath.Join(s.root, id, "preview")); err == nil {
				s.removeScope(id)
			}
		}
	}
}

func (s *Jobs) load(id string) (*job, error) {
	if !cell.ValidID(id) {
		return nil, store.ErrBadInput
	}
	raw, err := os.ReadFile(filepath.Join(s.root, id, "build.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var j job
	if err := json.Unmarshal(raw, &j); err != nil || j.Draft != id {
		return nil, store.ErrCorrupt
	}
	return &j, nil
}

// save commits the state and its checkpoint atomically before emitting the
// invalidation. A caller reopening on that event sees the new revision.
func (s *Jobs) save(j *job, event Event) error {
	j.Revision++
	j.Published = j.App != "" && !j.Dirty && len(j.Queue) == 0
	if event.Stage == "" {
		event.Stage = j.State.State
	}
	event.Draft = j.Draft
	if event.Line != "" {
		j.Progress = append(j.Progress, event)
	}
	if err := writeJob(filepath.Join(s.root, j.Draft), j); err != nil {
		return err
	}
	close(s.changes)
	s.changes = make(chan struct{})
	if s.opts.Changed != nil {
		s.opts.Changed(event)
	}
	return nil
}

func writeJob(dir string, j *job) error {
	f, err := os.CreateTemp(dir, ".build-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(j)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err := fsreplace.Rename(f.Name(), filepath.Join(dir, "build.json")); err != nil {
		return err
	}
	return fsreplace.SyncDir(dir)
}

func newJob(id, app, name string) *job {
	return &job{
		State: State{
			Summary: Summary{Draft: id, App: app, Name: name, State: Ready},
			Needs:   []string{}, Offers: []Offer{}, Using: []string{}, Transcript: []Message{},
			Checkpoints: []Checkpoint{}, Marks: []Mark{}, Screens: []string{}, Progress: []Event{},
		},
		Working: snapshot{Files: map[string]string{}}, Saved: map[string]snapshot{},
	}
}

func (s *Jobs) create(app string) (*job, error) {
	// The draft ID determines the publish ID for new apps. A change takes
	// its baseline from the store's committed, validated definition.
	draft, err := s.opts.Store.NewDraft("")
	if err != nil {
		return nil, err
	}
	j := newJob(draft.ID(), app, "New app")
	if app != "" {
		live, err := s.opts.Store.Get(app)
		if err != nil {
			_ = s.opts.Store.Discard(draft)
			return nil, err
		}
		j.Base, j.Name = live.Meta.Version, live.Meta.Name
		j.Working = snapshot{Files: live.Files(), Passed: true}
		j.acceptKeptUses()
		s.describeKept(j)
	}
	return j, nil
}

func (s *Jobs) List() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.jobs()
	result := []Summary{}
	for _, j := range jobs {
		if j.Dirty || len(j.Queue) != 0 {
			result = append(result, j.Summary)
		}
	}
	return result, err
}

func (s *Jobs) State(id string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(id)
	if err != nil {
		return State{}, err
	}
	return j.State, nil
}

// PlanFailure carries the persisted, person-facing explanation of a failed plan.
type PlanFailure struct{ Message string }

func (e *PlanFailure) Error() string { return e.Message }

func (s *Jobs) Plan(ctx context.Context, p PlanRequest) (PlanResult, error) {
	newApp := p.Draft == "" && strings.TrimSpace(p.Ask) != "" && p.Request == ""
	replan := p.Draft != "" && p.Ask == "" && strings.TrimSpace(p.Request) != ""
	if !newApp && !replan {
		return PlanResult{}, store.ErrBadInput
	}
	s.mu.Lock()
	var j *job
	var err error
	request := p.Request
	if p.Draft == "" {
		j, err = s.create("")
		request = p.Ask
	} else {
		j, err = s.load(p.Draft)
	}
	var id string
	if err == nil {
		id, err = s.enqueue(j, Planning, request, nil, "")
	}
	s.mu.Unlock()
	if err != nil {
		return PlanResult{}, err
	}
	state, err := s.wait(ctx, j.Draft, id)
	if err != nil {
		return PlanResult{}, err
	}
	if state.State == Failed || len(state.Plan) == 0 {
		message := state.Problem
		if message == "" {
			message = "I couldn't make the plan because the app outline is missing."
		}
		return PlanResult{}, &PlanFailure{Message: message}
	}
	return PlanResult{Draft: state.Draft, Plan: state.Plan, Needs: state.Needs, Offers: state.Offers}, nil
}

func (s *Jobs) wait(ctx context.Context, draft, turnID string) (State, error) {
	for {
		s.mu.Lock()
		j, err := s.load(draft)
		changed := s.changes
		s.mu.Unlock()
		if err != nil {
			return State{}, err
		}
		if !slices.ContainsFunc(j.Queue, func(t turn) bool { return t.ID == turnID }) {
			return j.State, nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return State{}, ctx.Err()
		case <-s.ctx.Done():
			return State{}, s.ctx.Err()
		}
	}
}

func (s *Jobs) Build(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(id)
	if err != nil {
		return err
	}
	if !j.Dirty || j.Working.CheckComplete || slices.ContainsFunc(j.Queue, func(t turn) bool { return t.Stage != Planning }) {
		return nil
	}
	_, err = s.enqueue(j, Writing, "", nil, "")
	return err
}

// Change opens the app's editing job. A request queues work and returns
// immediately so an assistant can announce where the proposal will land.
func (s *Jobs) Change(app, request string) (string, error) {
	return s.Propose(app, request, nil)
}

// Propose starts a fresh scoped turn from the request and optional node
// address. A caller has no way to supply a conversation or its tool history.
func (s *Jobs) Propose(app, request string, target *Target) (string, error) {
	return s.propose(app, request, target, "")
}

// ProposeAssistant carries the assistant's card ID with the persisted turn.
func (s *Jobs) ProposeAssistant(app, request, proposal string) (string, error) {
	return s.propose(app, request, nil, proposal)
}

func (s *Jobs) propose(app, request string, target *Target, proposal string) (string, error) {
	if !cell.ValidID(app) || !validTarget(target) {
		return "", store.ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.jobs()
	if err != nil {
		return "", err
	}
	var j *job
	for _, candidate := range jobs {
		if candidate.App == app {
			j = candidate
			break
		}
	}
	if j == nil {
		j, err = s.create(app)
		if err != nil {
			return "", err
		}
	} else if !j.Dirty && len(j.Queue) == 0 {
		live, err := s.opts.Store.Get(app)
		if err != nil {
			return "", err
		}
		if j.Base != live.Meta.Version {
			j.Base, j.Name = live.Meta.Version, live.Meta.Name
			j.Working = snapshot{Files: live.Files(), Passed: true}
			j.acceptKeptUses()
			s.describeKept(j)
		}
	}
	j.Editable = true
	if strings.TrimSpace(request) != "" {
		_, err = s.enqueue(j, Writing, request, target, proposal)
	} else {
		err = s.save(j, Event{})
	}
	return j.Draft, err
}

var redirect = regexp.MustCompile(`(?i)^\s*(actually\b|instead\b|scratch that\b|stop\b|forget that\b|no[, ])`)

func (s *Jobs) Say(id, text string, target *Target) error {
	if strings.TrimSpace(text) == "" || !validTarget(target) {
		return store.ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(id)
	if err != nil {
		return err
	}
	stage := Writing
	if !j.Editable && j.Paused == nil && len(j.Screens) == 0 && (len(j.Queue) == 0 || j.Queue[0].Stage == Planning) {
		stage = Planning
	}
	var active turn
	if len(j.Queue) > 0 {
		active = j.Queue[0]
	}
	interrupt := active.ID != "" && redirect.MatchString(text)
	if interrupt {
		stage = active.Stage
		j.Queue = j.Queue[1:]
	}
	newID, err := s.enqueue(j, stage, text, target, "")
	if err != nil {
		return err
	}
	if interrupt {
		return s.prioritize(j, newID)
	}
	if active.ID != "" {
		s.classify(j, active, newID, text)
	}
	return nil
}

// A node is an element address. Only the screen names a file.
var markNodeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validTarget(target *Target) bool {
	if target == nil {
		return true
	}
	if !definitionPath.MatchString("screens/"+target.Screen+".json") || len(target.Path) > 128 ||
		(target.Node != "" && !markNodeID.MatchString(target.Node)) {
		return false
	}
	for _, step := range target.Path {
		switch value := step.(type) {
		case string:
		case float64:
			if value < 0 || value >= 1<<31 || value != float64(int(value)) {
				return false
			}
		case int:
			if value < 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (s *Jobs) prioritize(j *job, id string) error {
	idx := slices.IndexFunc(j.Queue, func(t turn) bool { return t.ID == id })
	if idx < 0 {
		return nil
	}
	next := j.Queue[idx]
	j.Queue = append([]turn{next}, slices.Delete(j.Queue, idx, idx+1)...)
	j.State.State, j.Problem = next.Stage, ""
	j.Presence = nil
	j.WritingFiles = nil
	j.Screens = screens(j.Working.Files)
	j.Progress = []Event{}
	if err := s.save(j, Event{Line: "Changing direction."}); err != nil {
		return err
	}
	if cancel := s.workers[j.Draft]; cancel != nil {
		cancel()
	}
	return nil
}

func (s *Jobs) enqueue(j *job, stage, request string, target *Target, proposal string) (string, error) {
	if s.ctx.Err() != nil {
		return "", s.ctx.Err()
	}
	if j.Publishing || j.Proposal != nil || j.Package != nil {
		return "", store.ErrConflict
	}
	wantsProposal := proposal != "" || (j.App != "" && target != nil)
	if wantsProposal && j.PendingProposal != nil && len(j.Queue) != 0 {
		return "", store.ErrConflict
	}
	id, err := cell.NewID(time.Now())
	if err != nil {
		return "", err
	}
	if request != "" {
		if target != nil {
			if err := j.mark(request, *target); err != nil {
				return "", err
			}
		}
		j.applyReuseChoice(request)
		j.Transcript = append(j.Transcript, Message{ID: id, Role: "user", Text: request})
		j.LastRequest = request
	}
	if len(j.Queue) == 0 {
		j.Progress = []Event{}
		j.State.State = stage
		j.Problem = ""
	}
	if wantsProposal && proposal == "" {
		proposal = id
	}
	if proposal != "" {
		j.PendingProposal = &proposalIntent{ID: proposal, Target: target}
	}
	next := turn{ID: id, Stage: stage, Request: request, Target: target, Proposal: proposal}
	if stage == Writing && request == "" && j.Paused != nil {
		next = *j.Paused
		next.ID = id
	}
	j.Paused = nil
	j.Dirty = true
	j.Queue = append(j.Queue, next)
	if err := s.save(j, Event{}); err != nil {
		return "", err
	}
	s.start(j)
	return id, nil
}

func (s *Jobs) catalog(app string) (Catalog, error) {
	if s.opts.Catalog == nil {
		return Catalog{}, nil
	}
	return s.opts.Catalog(app)
}

// describe derives the card from app.json after every committed model turn.
func (j *job) describe(catalog Catalog) {
	j.describeDraft(catalog, false, false)
}

func (j *job) describeDraft(catalog Catalog, headless, planning bool) {
	files := j.Working.Files
	j.Plan = nil
	if raw := json.RawMessage(files[schema.PlanFile]); json.Valid(raw) {
		j.Plan = raw
	}
	j.Screens = screens(files)
	j.Needs, j.Offers, j.Using = []string{}, []Offer{}, []string{}
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil {
		return
	}
	if app.Name != "" {
		j.Name = app.Name
	}
	for _, name := range slices.Sorted(maps.Keys(app.Records)) {
		typ := app.Records[name]
		if typ.Uses && (headless || j.reuseAccepted(name, typ, catalog)) {
			j.Using = append(j.Using, name)
			source := name
			if typ.Common != "" {
				offer, _ := catalog.Types.OfferCommonType(name, typ)
				source = offer.Existing
			}
			if (headless || typ.Common != "") && catalog.reuseSource(source) != "" {
				j.Needs = append(j.Needs, "Uses "+catalog.reuseText(source))
			}
		}
		access := app.Capabilities.Records[name]
		if access == "r" || access == "rw" {
			label := "Read "
			if access == "rw" {
				label = "Read and edit "
			}
			j.Needs = append(j.Needs, label+name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(app.Capabilities.Connectors)) {
		connector := app.Capabilities.Connectors[name]
		line := name + " · " + strings.Join(connector.Can, ", ")
		if len(connector.Approve) > 0 {
			line += " · Ask before " + strings.Join(connector.Approve, ", ")
		}
		j.Needs = append(j.Needs, line)
	}
	for _, name := range app.Capabilities.Tools {
		j.Needs = append(j.Needs, "Tool · "+name)
	}
	for _, verb := range app.Capabilities.AI {
		j.Needs = append(j.Needs, "AI · "+verb)
	}
	if app.Budget != nil && app.Budget.Month > 0 {
		j.Needs = append(j.Needs, fmt.Sprintf("Monthly limit · $%.2f", app.Budget.Month))
	}
	j.Offers = reuseOffers(app, j.App, j.Accepted, catalog)
	if headless && planning {
		// Put reuse of actual records before empty types and library suggestions.
		slices.SortStableFunc(j.Offers, func(a, b Offer) int {
			return cmp.Compare(b.Count, a.Count)
		})
		for _, offer := range j.Offers {
			line := "Uses " + catalog.reuseText(offer.Type)
			if offer.App != "" && !slices.Contains(j.Needs, line) {
				j.Needs = append(j.Needs, line)
			}
		}
	}
}

func (j *job) acceptKeptUses() {
	app, _ := schema.ParseApp([]byte(j.Working.Files[schema.AppFile]))
	j.Accepted = []string{}
	for _, name := range slices.Sorted(maps.Keys(app.Records)) {
		if record := app.Records[name]; record.Uses {
			if record.Common != "" {
				name = "common:" + record.Common
			}
			j.Accepted = append(j.Accepted, name)
		}
	}
}

// The plan card sends the chosen type in the person's own words. Treat only
// a positive choice for an offered type as consent to share it.
func (j *job) applyReuseChoice(request string) {
	const prefix = "use my existing "
	text, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(request)), prefix)
	if !ok {
		return
	}
	field, _, _ := strings.Cut(text, " ")
	name := strings.Trim(field, ".,:;!?")
	for _, offer := range j.Offers {
		if offer.App != "" && name == offer.Type {
			j.acceptReuse(offer.Type)
			return
		}
	}
	for _, offer := range j.Offers {
		if offer.App != "" && strings.TrimSuffix(offer.Type, "s") == strings.TrimSuffix(name, "s") {
			j.acceptReuse(offer.Type)
			return
		}
	}
}

func screens(files map[string]string) []string {
	result := []string{}
	for _, path := range slices.Sorted(maps.Keys(files)) {
		if strings.HasPrefix(path, "screens/") && json.Valid([]byte(files[path])) {
			result = append(result, schema.FileID(path))
		}
	}
	return result
}
