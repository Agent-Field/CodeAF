package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

var _ appwire.Workstreamer = (*appBackend)(nil)

const (
	workstreamFactoryTimeout  = 3 * time.Second
	workstreamFactoryFresh    = 10 * time.Second
	workstreamFactoryCacheMax = 32
	workstreamMissingJobsMax  = 20
	workstreamFactoryParallel = 8
)

// appWorkstreams belongs to one backend, so cache entries and create retries
// cannot cross profiles. The function seams replace external owners in tests.
type appWorkstreams struct {
	createMu     sync.Mutex
	createAgent  func(appwire.CreateAsk) (appwire.Agent, error)
	closeAgent   func(string) error
	archiveChats func([]string) error
	queue        func(context.Context, string) (api.Queue, error)
	job          func(context.Context, string, int) (api.Job, error)
	spend        func(string) *float64
	cacheMu      sync.Mutex
	cache        map[string]workstreamQueueRead
	flights      map[string]chan struct{}
	briefMu      sync.Mutex
	briefs       map[string]session.WorkstreamBrief
}

type workstreamQueueRead struct {
	at   time.Time
	jobs map[int]api.Job
	err  error
}

func (b *appBackend) ServesWorkstreams() bool { return true }

func (b *appBackend) workstreamStore(create bool) (*workstream.Store, error) {
	s, err := b.collectionStore(create)
	if err != nil || s == nil {
		return nil, err
	}
	return workstream.New(s, workstream.Options{Now: b.now}), nil
}

// workstreamDo emits committed receipts, including a read's first observation.
// Unchanged and refused writes have empty receipts and do not notify watchers.
func workstreamDo[P interface{ Check() error }, R any](b *appBackend, p P, call func(*workstream.Store) (R, workstream.Change, error)) (R, error) {
	var zero R
	if err := p.Check(); err != nil {
		return zero, err
	}
	s, err := b.workstreamStore(false)
	if err != nil {
		return zero, err
	}
	if s == nil {
		return zero, appwire.Errorf(appwire.CodeNotFound, "workstream target not found")
	}
	out, change, err := call(s)
	// A refresh can fail after a write commits; its receipt still belongs to watchers.
	b.workstreamChanged(change)
	if err != nil {
		return zero, appWorkstreamError(err)
	}
	return out, nil
}

func appWorkstreamError(err error) error {
	var stale *workstream.VersionConflict
	switch {
	case errors.As(err, &stale):
		return &appwire.Error{Code: appwire.CodeVersionConflict, Message: stale.Error(), Detail: appwire.WorkstreamVersionConflict{ID: stale.ID, Version: stale.Version, UpdatedAt: stale.UpdatedAt, UpdatedBy: stale.UpdatedBy}}
	case errors.Is(err, workstream.ErrNotFound):
		return appwire.Errorf(appwire.CodeNotFound, "%s", err)
	case errors.Is(err, workstream.ErrConflict):
		return appwire.Errorf(appwire.CodeConflict, "%s", err)
	case errors.Is(err, workstream.ErrCycle):
		return appwire.Errorf(appwire.CodeCycle, "%s", err)
	case errors.Is(err, workstream.ErrTooLarge), errors.Is(err, workstream.ErrLimit):
		return appwire.Errorf(appwire.CodeTooLarge, "%s", err)
	case errors.Is(err, workstream.ErrNotAgentPage):
		return appwire.Errorf(appwire.CodeBadParams, "%s", err)
	}
	return appCollectionError(err)
}

func (b *appBackend) workstreamChanged(change workstream.Change) {
	if change.ID == "" || len(change.Parts) == 0 {
		return
	}
	parts := make([]string, 0, len(change.Parts))
	for _, p := range change.Parts {
		parts = append(parts, string(p))
	}
	b.emit.Emit(appwire.EventWorkstreamChanged, appwire.WorkstreamChanged{ID: change.ID, Parts: parts})
}

// workstreamCollectionChanged is used after collection writes that share the
// workstream's identity. Only workstream collections gain this second event.
func (b *appBackend) workstreamCollectionChanged(id string, part workstream.ChangedPart) {
	s, err := b.existingStore()
	if err != nil {
		return
	}
	c, err := s.Describe(context.Background(), id)
	if err == nil && c.Workstream {
		parts := []workstream.ChangedPart{workstream.PartWorkstream}
		if part != workstream.PartWorkstream {
			parts = append(parts, part)
		}
		b.workstreamChanged(workstream.Change{ID: id, Parts: parts})
	}
}

func workstreamValue[T any](p *T) T {
	if p != nil {
		return *p
	}
	var zero T
	return zero
}
func workstreamOptional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func workstreamPositive(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func appWorkstream(w workstream.Workstream, facts map[workstream.Job]workstream.JobFacts, spend *float64) appwire.Workstream {
	jobs := w.Jobs()
	states := make([]string, 0, w.Total)
	for _, j := range jobs {
		states = append(states, facts[j].State)
	}
	numbers := workstream.CountNumbers(jobs, facts, spend, w.Proposed)
	return appwire.Workstream{ID: w.ID, Name: w.Name, Why: w.Why, Stage: workstream.Stage(w.DoneAt, w.StartedAt, states), AgentID: w.AgentID, OriginChatID: workstreamOptional(w.OriginChatID), Autonomy: w.Autonomy, Wake: appwire.WorkstreamWake{Morning: w.Wake.Morning, RelatedIssues: w.Wake.RelatedIssues}, BudgetUSD: w.BudgetUSD, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, StartedAt: w.StartedAt, DoneAt: w.DoneAt, HowItWent: workstreamOptional(w.HowItWent), Total: numbers.Total, Merged: numbers.Merged, NeedsYou: numbers.NeedsYou, SpendUSD: numbers.SpendUSD}
}

func (b *appBackend) WorkstreamList(p appwire.WorkstreamListParams) (appwire.WorkstreamListResult, error) {
	if err := p.Check(); err != nil {
		return appwire.WorkstreamListResult{}, err
	}
	s, err := b.workstreamStore(false)
	out := appwire.WorkstreamListResult{Workstreams: []appwire.WorkstreamRow{}}
	if err != nil || s == nil {
		return out, err
	}
	rows, err := s.List(context.Background())
	if err != nil {
		return out, appWorkstreamError(err)
	}
	jobs := []workstream.Job{}
	for _, w := range rows {
		b.workstreamRememberBrief(w)
		jobs = append(jobs, w.Jobs()...)
	}
	facts := b.workstreamFacts(jobs)
	for _, w := range rows {
		var c workstream.Change
		w, c, err = b.workstreamObserved(s, w, facts, workstream.Change{})
		b.workstreamChanged(c)
		if err != nil {
			return appwire.WorkstreamListResult{}, err
		}
		b.workstreamRememberBrief(w)
		v := appWorkstream(w, facts, b.workstreamSpend(w.AgentID))
		out.Workstreams = append(out.Workstreams, appwire.WorkstreamRow{ID: v.ID, Name: v.Name, Stage: v.Stage, Total: v.Total, Merged: v.Merged, NeedsYou: v.NeedsYou, SpendUSD: v.SpendUSD, BudgetUSD: v.BudgetUSD, UpdatedAt: v.UpdatedAt, AgentID: v.AgentID, OriginChatID: v.OriginChatID})
	}
	return out, nil
}

func (b *appBackend) WorkstreamGet(p appwire.WorkstreamGetParams) (appwire.WorkstreamGetResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamGetResult, workstream.Change, error) {
		w, err := s.Get(context.Background(), p.ID)
		if err != nil {
			return appwire.WorkstreamGetResult{}, workstream.Change{}, err
		}
		b.workstreamRememberBrief(w)
		facts := b.workstreamFacts(w.Jobs())
		w, c, err := b.workstreamObserved(s, w, facts, workstream.Change{})
		return appwire.WorkstreamGetResult{Workstream: appWorkstream(w, facts, b.workstreamSpend(w.AgentID))}, c, err
	})
}

// workstreamAgentAsk is the single agent-creation hook for workstreams. Empty
// text is a bare open in prepareCreate and cannot start a model turn.
func workstreamAgentAsk(p appwire.WorkstreamCreateParams) appwire.CreateAsk {
	// Engine assembly attaches the prompt and tools through appWorkstreamAgentDoor.
	return appwire.CreateAsk{Model: workstreamValue(p.Model), CreatedBy: workstreamCreatedBy}
}

// workstreamPreflight checks store-owned refusals without writing or migrating.
// The final Create transaction repeats ownership and naming checks for peers.
func workstreamPreflight(s *workspace.Store, p appwire.WorkstreamCreateParams) error {
	ctx := context.Background()
	return s.Read(ctx, func(tx *sql.Tx, version int) error {
		if version == 0 {
			for _, r := range p.Refs {
				if r.Kind == appwire.RefCollection {
					return workspace.ErrNotFound
				}
			}
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT name FROM collections")
		if err != nil {
			return err
		}
		taken := false
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			taken = taken || strings.EqualFold(name, strings.TrimSpace(p.Name))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if taken {
			return workspace.ErrNameTaken
		}
		for _, r := range p.Refs {
			if r.Kind == appwire.RefCollection {
				var exists bool
				if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM collections WHERE id=?)", r.ID).Scan(&exists); err != nil {
					return err
				}
				if !exists {
					return workspace.ErrNotFound
				}
			}
			if version >= 3 && r.Kind == appwire.RefFactoryJob {
				var held workspace.WorkstreamTaken
				held.Factory = workstreamValue(r.Factory)
				held.Job, _ = strconv.Atoi(r.ID)
				err := tx.QueryRowContext(ctx, `SELECT j.workstream_id,c.name FROM workstream_jobs j JOIN collections c ON c.id=j.workstream_id WHERE j.factory=? AND j.job=?`, held.Factory, held.Job).Scan(&held.WorkstreamID, &held.WorkstreamName)
				if err == nil {
					return &held
				}
				if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
		}
		return nil
	})
}

func (b *appBackend) WorkstreamCreate(p appwire.WorkstreamCreateParams) (appwire.WorkstreamCreateResult, error) {
	var zero appwire.WorkstreamCreateResult
	if err := p.Check(); err != nil {
		return zero, err
	}
	b.workstreams.createMu.Lock()
	defer b.workstreams.createMu.Unlock()
	// A retry is recognized before origin lookup or agent creation. The domain
	// checks client_id again inside its transaction to settle competing peers.
	existing, err := b.workstreamStore(false)
	if err != nil {
		return zero, err
	}
	if existing != nil && workstreamValue(p.ClientID) != "" {
		w, err := existing.ByClientID(context.Background(), *p.ClientID)
		if err == nil {
			b.workstreamRememberBrief(w)
			return appwire.WorkstreamCreateResult{Workstream: appWorkstream(w, nil, b.workstreamSpend(w.AgentID))}, nil
		}
		if !errors.Is(err, workstream.ErrNotFound) {
			return zero, appWorkstreamError(err)
		}
	}
	if err := b.workstreamCreatePreflight(p); err != nil {
		return zero, err
	}
	create := b.workstreams.createAgent
	if create == nil {
		create = b.CreateAgent
	}
	agent, err := create(workstreamAgentAsk(p))
	if err != nil {
		return zero, err
	}
	w, change, err := b.workstreamCreateWithAgent(p, agent)
	if err != nil {
		return zero, appWorkstreamError(err)
	}
	b.workstreamRememberBrief(w)
	b.workstreamChanged(change)
	if change.ID != "" {
		b.collectionChanged(w.ID, false)
	}
	return appwire.WorkstreamCreateResult{Workstream: appWorkstream(w, nil, b.workstreamSpend(w.AgentID))}, nil
}

func (b *appBackend) workstreamCreatePreflight(p appwire.WorkstreamCreateParams) error {
	if p.OriginChatID != nil {
		if _, err := b.find(*p.OriginChatID); err != nil {
			return err
		}
	}
	store, err := b.collectionStore(false)
	if err != nil {
		return err
	}
	if store != nil {
		return appWorkstreamError(workstreamPreflight(store, p))
	}
	for _, ref := range p.Refs {
		if ref.Kind == appwire.RefCollection {
			return appCollectionError(workspace.ErrNotFound)
		}
	}
	return nil
}

func (b *appBackend) workstreamCreateWithAgent(p appwire.WorkstreamCreateParams, agent appwire.Agent) (workstream.Workstream, workstream.Change, error) {
	refs := make([]workspace.Ref, 0, len(p.Refs))
	for _, r := range p.Refs {
		refs = append(refs, workspace.Ref{Kind: workspace.Kind(r.Kind), ID: r.ID, SessionID: workstreamValue(r.SessionID), Factory: workstreamValue(r.Factory)})
	}
	var w workstream.Workstream
	var change workstream.Change
	store, err := b.collectionStore(true)
	if err == nil {
		s := workstream.New(store, workstream.Options{Now: b.now})
		w, change, err = s.Create(context.Background(), workstream.Create{Name: strings.TrimSpace(p.Name), Why: workstreamValue(p.Why), OriginChatID: workstreamValue(p.OriginChatID), Refs: refs, AgentID: agent.ID, ClientID: workstreamValue(p.ClientID)})
	}
	if err != nil || w.AgentID != agent.ID {
		if cleanup := b.workstreamCloseUnusedAgent(agent.ID, err); cleanup != nil {
			return workstream.Workstream{}, workstream.Change{}, cleanup
		}
	}
	return w, change, err
}

func (b *appBackend) workstreamCloseUnusedAgent(id string, cause error) error {
	closeAgent := b.workstreams.closeAgent
	if closeAgent == nil {
		closeAgent = func(id string) error { return b.MoveToClosed([]string{id}) }
	}
	if cleanup := closeAgent(id); cleanup != nil {
		var wire *appwire.Error
		if cause != nil && errors.As(appWorkstreamError(cause), &wire) {
			copy := *wire
			copy.Message += "; the unused workstream agent could not be closed"
			return &copy
		}
		return appwire.Errorf(appwire.CodeEngine, "the unused workstream agent could not be closed: %s", appUnaskedReason("the conversation", cleanup))
	}
	return nil
}

func (b *appBackend) WorkstreamUpdate(p appwire.WorkstreamUpdateParams) (appwire.WorkstreamUpdateResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamUpdateResult, workstream.Change, error) {
		patch := workstream.Patch{Name: p.Patch.Name, Why: p.Patch.Why, Autonomy: p.Patch.Autonomy, BudgetUSD: p.Patch.BudgetUSD}
		if p.Patch.Wake != nil {
			patch.Wake = &workstream.Wake{Morning: p.Patch.Wake.Morning, RelatedIssues: p.Patch.Wake.RelatedIssues}
		}
		w, c, err := s.Update(context.Background(), p.ID, patch)
		if err != nil {
			return appwire.WorkstreamUpdateResult{}, c, err
		}
		b.workstreamRememberBrief(w)
		if c.ID != "" && p.Patch.Name != nil {
			b.collectionChanged(w.ID, false)
		}
		facts := b.workstreamFacts(w.Jobs())
		w, c, err = b.workstreamObserved(s, w, facts, c)
		return appwire.WorkstreamUpdateResult{Workstream: appWorkstream(w, facts, b.workstreamSpend(w.AgentID))}, c, err
	})
}

func (b *appBackend) WorkstreamMarkDone(p appwire.WorkstreamMarkDoneParams) (appwire.WorkstreamMarkDoneResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamMarkDoneResult, workstream.Change, error) {
		w, c, err := s.MarkDone(context.Background(), p.ID, workstreamValue(p.HowItWent))
		if err != nil {
			return appwire.WorkstreamMarkDoneResult{}, c, err
		}
		b.workstreamRememberBrief(w)
		facts := b.workstreamFacts(w.Jobs())
		w, c, err = b.workstreamObserved(s, w, facts, c)
		// The workstream is already durable even if archiving a chat fails. Publish
		// that receipt first; retrying mark_done can finish the archive without a
		// second workstream mutation or notification.
		b.workstreamChanged(c)
		if err != nil {
			return appwire.WorkstreamMarkDoneResult{}, workstream.Change{}, err
		}
		if workstreamValue(p.ArchiveChats) {
			ids := []string{}
			for _, r := range w.Members {
				if r.Kind == workspace.ConversationKind && r.ID != w.AgentID {
					ids = append(ids, r.ID)
				}
			}
			archive := b.workstreams.archiveChats
			if archive == nil {
				archive = b.Archive
			}
			if len(ids) > 0 {
				err = archive(ids)
			}
		}
		if err != nil {
			return appwire.WorkstreamMarkDoneResult{}, workstream.Change{}, err
		}
		return appwire.WorkstreamMarkDoneResult{Workstream: appWorkstream(w, facts, b.workstreamSpend(w.AgentID))}, workstream.Change{}, nil
	})
}

func (b *appBackend) WorkstreamReopen(p appwire.WorkstreamReopenParams) (appwire.WorkstreamReopenResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamReopenResult, workstream.Change, error) {
		w, c, err := s.Reopen(context.Background(), p.ID)
		if err != nil {
			return appwire.WorkstreamReopenResult{}, c, err
		}
		b.workstreamRememberBrief(w)
		facts := b.workstreamFacts(w.Jobs())
		w, c, err = b.workstreamObserved(s, w, facts, c)
		return appwire.WorkstreamReopenResult{Workstream: appWorkstream(w, facts, b.workstreamSpend(w.AgentID))}, c, err
	})
}

func appWorkstreamPlan(p workstream.Plan) appwire.WorkstreamPlan {
	out := appwire.WorkstreamPlan{WorkstreamID: p.WorkstreamID, Version: p.Version, UpdatedAt: p.UpdatedAt, Milestones: []appwire.WorkstreamMilestone{}, Waves: []appwire.WorkstreamWave{}}
	for _, m := range p.Milestones {
		out.Milestones = append(out.Milestones, appwire.WorkstreamMilestone{ID: m.ID, Title: m.Title})
	}
	for _, w := range p.Waves {
		wave := appwire.WorkstreamWave{N: w.N, Title: workstreamOptional(w.Title), MilestoneID: workstreamOptional(w.MilestoneID), AutoStart: w.AutoStart, Items: []appwire.WorkstreamPlanItem{}}
		for _, i := range w.Items {
			wave.Items = append(wave.Items, appwire.WorkstreamPlanItem{Kind: i.Kind, Factory: workstreamOptional(i.Factory), Job: workstreamPositive(i.Job), DraftID: workstreamOptional(i.DraftID), Title: workstreamOptional(i.Title), Repo: workstreamOptional(i.Repo), Text: workstreamOptional(i.Text), Size: workstreamOptional(i.Size), BudgetUSD: workstreamDraftBudget(i.BudgetUSD), Source: appWorkstreamSource(i.Source)})
		}
		out.Waves = append(out.Waves, wave)
	}
	return out
}
func (b *appBackend) WorkstreamPlanGet(p appwire.WorkstreamPlanGetParams) (appwire.WorkstreamPlanGetResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPlanGetResult, workstream.Change, error) {
		v, err := s.Plan(context.Background(), p.ID)
		return appwire.WorkstreamPlanGetResult{Plan: appWorkstreamPlan(v)}, workstream.Change{}, err
	})
}
func (b *appBackend) WorkstreamPlanSet(p appwire.WorkstreamPlanSetParams) (appwire.WorkstreamPlanSetResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPlanSetResult, workstream.Change, error) {
		milestones := make([]workstream.Milestone, 0, len(p.Milestones))
		waves := make([]workstream.Wave, 0, len(p.Waves))
		for _, m := range p.Milestones {
			milestones = append(milestones, workstream.Milestone{ID: m.ID, Title: m.Title})
		}
		for _, w := range p.Waves {
			wave := workstream.Wave{N: w.N, Title: workstreamValue(w.Title), MilestoneID: workstreamValue(w.MilestoneID), AutoStart: w.AutoStart, Items: []workstream.PlanItem{}}
			for _, i := range w.Items {
				wave.Items = append(wave.Items, workstream.PlanItem{Kind: i.Kind, Factory: workstreamValue(i.Factory), Job: workstreamValue(i.Job), DraftID: workstreamValue(i.DraftID), Title: workstreamValue(i.Title), Repo: workstreamValue(i.Repo), Text: workstreamValue(i.Text), Size: workstreamValue(i.Size), BudgetUSD: workstreamValue(i.BudgetUSD), Source: workstreamSource(i.Source)})
			}
			waves = append(waves, wave)
		}
		v, c, err := s.SetPlan(context.Background(), p.ID, p.BaseVersion, milestones, waves, workstream.Attribution{})
		return appwire.WorkstreamPlanSetResult{Plan: appWorkstreamPlan(v)}, c, err
	})
}

func workstreamDraftBudget(budget float64) *float64 {
	if budget == 0 {
		return nil
	}
	return &budget
}

func appWorkstreamSource(p *workstream.Source) *appwire.WorkstreamSource {
	if p == nil {
		return nil
	}
	return &appwire.WorkstreamSource{Kind: p.Kind, ID: p.ID, ChatID: workstreamOptional(p.ChatID), Factory: workstreamOptional(p.Factory), Label: workstreamOptional(p.Label)}
}
func workstreamSource(p *appwire.WorkstreamSource) *workstream.Source {
	if p == nil {
		return nil
	}
	return &workstream.Source{Kind: p.Kind, ID: p.ID, ChatID: workstreamValue(p.ChatID), Factory: workstreamValue(p.Factory), Label: workstreamValue(p.Label)}
}
func appWorkstreamPageNode(p workstream.PageNode) appwire.WorkstreamPageNode {
	return appwire.WorkstreamPageNode{ID: p.ID, WorkstreamID: p.WorkstreamID, ParentID: workstreamOptional(p.ParentID), Title: p.Title, Owner: p.Owner, ReadByJobs: p.ReadByJobs, Position: p.Position, Version: p.Version, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy, UpdatedFrom: appWorkstreamSource(p.UpdatedFrom), DeletedAt: p.DeletedAt}
}
func appWorkstreamPage(p workstream.Page) appwire.WorkstreamPage {
	n := appWorkstreamPageNode(p.PageNode)
	return appwire.WorkstreamPage{ID: n.ID, WorkstreamID: n.WorkstreamID, ParentID: n.ParentID, Title: n.Title, Owner: n.Owner, ReadByJobs: n.ReadByJobs, Position: n.Position, Version: n.Version, UpdatedAt: n.UpdatedAt, UpdatedBy: n.UpdatedBy, UpdatedFrom: n.UpdatedFrom, DeletedAt: n.DeletedAt, Doc: p.Doc, Markdown: p.Markdown}
}
func (b *appBackend) WorkstreamPageList(p appwire.WorkstreamPageListParams) (appwire.WorkstreamPageListResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageListResult, workstream.Change, error) {
		pages, err := s.Pages(context.Background(), p.WorkstreamID)
		out := appwire.WorkstreamPageListResult{Pages: []appwire.WorkstreamPageNode{}}
		for _, page := range pages {
			out.Pages = append(out.Pages, appWorkstreamPageNode(page))
		}
		if err == nil && workstreamValue(p.IncludeDeleted) {
			var deleted []workstream.PageNode
			deleted, err = s.DeletedPages(context.Background(), p.WorkstreamID)
			out.Deleted = []appwire.WorkstreamPageNode{}
			for _, page := range deleted {
				out.Deleted = append(out.Deleted, appWorkstreamPageNode(page))
			}
		}
		return out, workstream.Change{}, err
	})
}
func (b *appBackend) WorkstreamPageGet(p appwire.WorkstreamPageGetParams) (appwire.WorkstreamPageGetResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageGetResult, workstream.Change, error) {
		var v workstream.Page
		var err error
		if p.Version != nil {
			v, err = s.PageAtVersion(context.Background(), p.ID, *p.Version, workstreamValue(p.Markdown))
		} else {
			v, err = s.Page(context.Background(), p.ID, workstreamValue(p.Markdown))
		}
		return appwire.WorkstreamPageGetResult{Page: appWorkstreamPage(v)}, workstream.Change{}, err
	})
}
func (b *appBackend) WorkstreamPageSave(p appwire.WorkstreamPageSaveParams) (appwire.WorkstreamPageSaveResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageSaveResult, workstream.Change, error) {
		v, c, err := s.SavePage(context.Background(), p.ID, p.BaseVersion, p.Doc, workstream.Attribution{From: workstreamSource(p.Source)})
		return appwire.WorkstreamPageSaveResult{Page: appWorkstreamPage(v)}, c, err
	})
}
func (b *appBackend) WorkstreamPageCreate(p appwire.WorkstreamPageCreateParams) (appwire.WorkstreamPageCreateResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageCreateResult, workstream.Change, error) {
		read := workstream.ReadByJobsDefault(p.Title)
		if p.ReadByJobs != nil {
			read = *p.ReadByJobs
		}
		v, c, err := s.CreatePage(context.Background(), workstream.PageCreate{WorkstreamID: p.WorkstreamID, ParentID: workstreamValue(p.ParentID), AfterID: workstreamValue(p.AfterID), Title: p.Title, Owner: workstreamValue(p.Owner), ReadByJobs: read, Doc: p.Doc, Attribution: workstream.Attribution{From: workstreamSource(p.Source)}})
		return appwire.WorkstreamPageCreateResult{Page: appWorkstreamPage(v)}, c, err
	})
}
func (b *appBackend) WorkstreamPageUpdate(p appwire.WorkstreamPageUpdateParams) (appwire.WorkstreamPageUpdateResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageUpdateResult, workstream.Change, error) {
		v, c, err := s.UpdatePage(context.Background(), p.ID, workstream.PagePatch{Title: p.Title, Owner: p.Owner, ReadByJobs: p.ReadByJobs})
		return appwire.WorkstreamPageUpdateResult{Page: appWorkstreamPageNode(v)}, c, err
	})
}
func (b *appBackend) WorkstreamPageMove(p appwire.WorkstreamPageMoveParams) (appwire.WorkstreamPageMoveResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageMoveResult, workstream.Change, error) {
		v, c, err := s.MovePage(context.Background(), p.ID, workstreamValue(p.ParentID), workstreamValue(p.AfterID))
		return appwire.WorkstreamPageMoveResult{Page: appWorkstreamPageNode(v)}, c, err
	})
}
func (b *appBackend) WorkstreamPageDelete(p appwire.WorkstreamPageDeleteParams) (appwire.WorkstreamPageDeleteResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageDeleteResult, workstream.Change, error) {
		v, c, err := s.DeletePage(context.Background(), p.ID)
		return appwire.WorkstreamPageDeleteResult{Deleted: v}, c, err
	})
}
func (b *appBackend) WorkstreamPageHistory(p appwire.WorkstreamPageHistoryParams) (appwire.WorkstreamPageHistoryResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageHistoryResult, workstream.Change, error) {
		limit := 50
		if p.Limit != nil {
			limit = *p.Limit
		}
		versions, err := s.PageHistory(context.Background(), p.ID, limit)
		out := appwire.WorkstreamPageHistoryResult{Versions: []appwire.WorkstreamPageVersion{}}
		for _, v := range versions {
			out.Versions = append(out.Versions, appwire.WorkstreamPageVersion{Version: v.Version, SavedAt: v.SavedAt, SavedBy: v.SavedBy, From: appWorkstreamSource(v.From), Title: v.Title, Bytes: v.Bytes})
		}
		return out, workstream.Change{}, err
	})
}
func (b *appBackend) WorkstreamPageRestore(p appwire.WorkstreamPageRestoreParams) (appwire.WorkstreamPageRestoreResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamPageRestoreResult, workstream.Change, error) {
		var v workstream.Page
		var c workstream.Change
		var err error
		if p.Version == nil {
			v, c, err = s.UndeletePage(context.Background(), p.ID)
		} else {
			v, c, err = s.RestorePage(context.Background(), p.ID, *p.Version, *p.BaseVersion, workstream.Attribution{})
		}
		return appwire.WorkstreamPageRestoreResult{Page: appWorkstreamPage(v)}, c, err
	})
}

func appWorkstreamRecord(r workstream.Record) appwire.WorkstreamRecord {
	return appwire.WorkstreamRecord{ID: r.ID, WorkstreamID: r.WorkstreamID, Kind: r.Kind, Text: r.Text, State: r.State, By: r.By, Source: appWorkstreamSource(r.Source), CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt, Supersedes: workstreamOptional(r.Supersedes), SupersededBy: workstreamOptional(r.SupersededBy), GoAhead: appWorkstreamGoAheadProposal(r.GoAhead), GoAheadID: workstreamOptional(r.GoAheadID)}
}

func appWorkstreamGoAheadProposal(p *workstream.GoAheadProposal) *appwire.WorkstreamGoAheadProposal {
	if p == nil {
		return nil
	}
	return &appwire.WorkstreamGoAheadProposal{Scope: p.Scope, Factory: workstreamOptional(p.Factory), Job: workstreamPositive(p.Job), Wave: workstreamPositive(p.Wave), MilestoneID: workstreamOptional(p.MilestoneID), CapUSD: p.CapUSD, EndsAt: p.EndsAt}
}

func workstreamGoAheadProposal(p *appwire.WorkstreamGoAheadProposal) *workstream.GoAheadProposal {
	if p == nil {
		return nil
	}
	return &workstream.GoAheadProposal{Scope: p.Scope, Factory: workstreamValue(p.Factory), Job: workstreamValue(p.Job), Wave: workstreamValue(p.Wave), MilestoneID: workstreamValue(p.MilestoneID), CapUSD: p.CapUSD, EndsAt: p.EndsAt}
}
func (b *appBackend) WorkstreamRecordList(p appwire.WorkstreamRecordListParams) (appwire.WorkstreamRecordListResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamRecordListResult, workstream.Change, error) {
		records, err := s.Records(context.Background(), p.WorkstreamID, workstreamValue(p.Kind), workstreamValue(p.State), workstreamValue(p.IncludeGoAheads))
		out := appwire.WorkstreamRecordListResult{Records: []appwire.WorkstreamRecord{}}
		for _, r := range records {
			out.Records = append(out.Records, appWorkstreamRecord(r))
		}
		return out, workstream.Change{}, err
	})
}
func (b *appBackend) WorkstreamRecordPropose(p appwire.WorkstreamRecordProposeParams) (appwire.WorkstreamRecordProposeResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamRecordProposeResult, workstream.Change, error) {
		v, c, err := s.ProposeRecord(context.Background(), workstream.RecordPropose{WorkstreamID: p.WorkstreamID, Kind: p.Kind, Text: p.Text, Source: workstreamSource(p.Source), Kept: workstreamValue(p.Kept), GoAhead: workstreamGoAheadProposal(p.GoAhead)})
		return appwire.WorkstreamRecordProposeResult{Record: appWorkstreamRecord(v)}, c, err
	})
}
func (b *appBackend) WorkstreamRecordKeep(p appwire.WorkstreamRecordKeepParams) (appwire.WorkstreamRecordKeepResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamRecordKeepResult, workstream.Change, error) {
		v, c, err := s.KeepRecord(context.Background(), p.ID)
		return appwire.WorkstreamRecordKeepResult{Record: appWorkstreamRecord(v)}, c, err
	})
}
func (b *appBackend) WorkstreamRecordDrop(p appwire.WorkstreamRecordDropParams) (appwire.WorkstreamRecordDropResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamRecordDropResult, workstream.Change, error) {
		v, c, err := s.DropRecord(context.Background(), p.ID)
		return appwire.WorkstreamRecordDropResult{Record: appWorkstreamRecord(v)}, c, err
	})
}
func (b *appBackend) WorkstreamRecordSupersede(p appwire.WorkstreamRecordSupersedeParams) (appwire.WorkstreamRecordSupersedeResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamRecordSupersedeResult, workstream.Change, error) {
		v, old, c, err := s.SupersedeRecord(context.Background(), p.ID, workstream.RecordReplacement{Kind: workstreamValue(p.Kind), Text: p.Text, Source: workstreamSource(p.Source)})
		return appwire.WorkstreamRecordSupersedeResult{Record: appWorkstreamRecord(v), Superseded: appWorkstreamRecord(old)}, c, err
	})
}
func appWorkstreamGoAhead(g workstream.GoAhead) appwire.WorkstreamGoAhead {
	return appwire.WorkstreamGoAhead{ID: g.ID, WorkstreamID: g.WorkstreamID, Scope: g.Scope, Factory: workstreamOptional(g.Factory), Job: workstreamPositive(g.Job), Wave: workstreamPositive(g.Wave), MilestoneID: workstreamOptional(g.MilestoneID), CapUSD: g.CapUSD, EndsAt: g.EndsAt, State: g.State, EndReason: workstreamOptional(g.EndReason), GrantedAt: g.GrantedAt, EndedAt: g.EndedAt}
}
func (b *appBackend) WorkstreamGoAheadList(p appwire.WorkstreamGoAheadListParams) (appwire.WorkstreamGoAheadListResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamGoAheadListResult, workstream.Change, error) {
		grants, err := s.GoAheads(context.Background(), p.WorkstreamID, workstreamValue(p.Ended))
		out := appwire.WorkstreamGoAheadListResult{GoAheads: []appwire.WorkstreamGoAhead{}}
		for _, g := range grants {
			out.GoAheads = append(out.GoAheads, appWorkstreamGoAhead(g))
		}
		return out, workstream.Change{}, err
	})
}
func (b *appBackend) WorkstreamGoAheadGrant(p appwire.WorkstreamGoAheadGrantParams) (appwire.WorkstreamGoAheadGrantResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamGoAheadGrantResult, workstream.Change, error) {
		v, c, err := s.GrantGoAhead(context.Background(), workstream.GoAheadGrant{WorkstreamID: p.WorkstreamID, Scope: p.Scope, Factory: workstreamValue(p.Factory), Job: workstreamValue(p.Job), Wave: workstreamValue(p.Wave), MilestoneID: workstreamValue(p.MilestoneID), CapUSD: p.CapUSD, EndsAt: p.EndsAt})
		return appwire.WorkstreamGoAheadGrantResult{GoAhead: appWorkstreamGoAhead(v)}, c, err
	})
}
func (b *appBackend) WorkstreamGoAheadRevoke(p appwire.WorkstreamGoAheadRevokeParams) (appwire.WorkstreamGoAheadRevokeResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamGoAheadRevokeResult, workstream.Change, error) {
		v, c, err := s.RevokeGoAhead(context.Background(), p.ID)
		return appwire.WorkstreamGoAheadRevokeResult{GoAhead: appWorkstreamGoAhead(v)}, c, err
	})
}

// workstreamQueue shares successful and failed reads briefly, including an
// in-flight read, so concurrent list/get calls cannot multiply Factory work.
func (b *appBackend) workstreamQueue(ctx context.Context, factory string) (entry workstreamQueueRead) {
	state := &b.workstreams
	entry, flight, readQueue := state.workstreamQueueStart(factory, b.now())
	if flight == nil {
		return entry
	}
	if !readQueue {
		select {
		case <-flight:
			return b.workstreamQueue(ctx, factory)
		case <-ctx.Done():
			return workstreamQueueRead{err: ctx.Err()}
		}
	}
	// Complete ownership even when the guarded reader panics. Only a completed
	// read replaces the unavailable entry, so a fault cannot publish success.
	entry = workstreamQueueRead{err: errors.New("the Factory queue read did not complete")}
	defer func() {
		entry.at = b.now()
		state.workstreamQueueFinish(factory, flight, entry)
	}()
	read := state.queue
	if read == nil {
		read = b.FactoryQueue
	}
	queue, err := read(ctx, factory)
	jobs := map[int]api.Job{}
	for _, j := range queue.Jobs {
		jobs[j.ID] = j
	}
	entry = workstreamQueueRead{err: err, jobs: jobs}
	return entry
}

func (state *appWorkstreams) workstreamQueueStart(factory string, now time.Time) (workstreamQueueRead, chan struct{}, bool) {
	state.cacheMu.Lock()
	defer state.cacheMu.Unlock()
	if entry, ok := state.cache[factory]; ok && now.Sub(entry.at) < workstreamFactoryFresh {
		return entry, nil, false
	}
	if flight := state.flights[factory]; flight != nil {
		return workstreamQueueRead{}, flight, false
	}
	if state.flights == nil {
		state.flights = map[string]chan struct{}{}
	}
	flight := make(chan struct{})
	state.flights[factory] = flight
	return workstreamQueueRead{}, flight, true
}

func (state *appWorkstreams) workstreamQueueFinish(factory string, flight chan struct{}, entry workstreamQueueRead) {
	state.cacheMu.Lock()
	defer state.cacheMu.Unlock()
	if state.cache == nil {
		state.cache = map[string]workstreamQueueRead{}
	}
	if len(state.cache) >= workstreamFactoryCacheMax {
		oldest := ""
		var at time.Time
		for id, v := range state.cache {
			if oldest == "" || v.at.Before(at) {
				oldest, at = id, v.at
			}
		}
		delete(state.cache, oldest)
	}
	state.cache[factory] = entry
	delete(state.flights, factory)
	close(flight)
}

type workstreamJobRead struct {
	job      api.Job
	err      error
	notFound bool
}

// workstreamJobReads gives each factory one concurrent queue read. Missing
// jobs share a global fallback allowance and the same overall deadline.
func (b *appBackend) workstreamJobReads(jobs []workstream.Job) map[workstream.Job]workstreamJobRead {
	out := map[workstream.Job]workstreamJobRead{}
	grouped := map[string][]workstream.Job{}
	for _, j := range jobs {
		if _, seen := out[j]; !seen {
			out[j] = workstreamJobRead{err: context.DeadlineExceeded}
			grouped[j.Factory] = append(grouped[j.Factory], j)
		}
	}
	if len(grouped) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), workstreamFactoryTimeout)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	missing := 0
	workstreamMissingAllowed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if missing >= workstreamMissingJobsMax {
			return false
		}
		missing++
		return true
	}
	workstreamRememberRead := func(j workstream.Job, read workstreamJobRead) {
		mu.Lock()
		defer mu.Unlock()
		out[j] = read
	}
	slots := make(chan struct{}, workstreamFactoryParallel)
	for factory, members := range grouped {
		wg.Add(1)
		guard.Go("app/workstream-factory", func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			queue := b.workstreamQueue(ctx, factory)
			for _, j := range members {
				job, ok := queue.jobs[j.Number]
				err := queue.err
				if err == nil && !ok {
					allowed := workstreamMissingAllowed()
					switch {
					case !allowed:
						err = errors.New("the missing-job read allowance was reached")
					case ctx.Err() != nil:
						err = ctx.Err()
					default:
						read := b.workstreams.job
						if read == nil {
							read = b.FactoryJob
						}
						job, err = read(ctx, j.Factory, j.Number)
						if err == nil && job.ID != j.Number {
							err = errors.New("the Factory returned another job")
						}
					}
				}
				var wire *appwire.Error
				notFound := queue.err == nil && errors.As(err, &wire) && wire.Code == appwire.CodeNotFound
				workstreamRememberRead(j, workstreamJobRead{job: job, err: err, notFound: notFound})
			}
		})
	}
	wg.Wait()
	return out
}

func (b *appBackend) workstreamFacts(jobs []workstream.Job) map[workstream.Job]workstream.JobFacts {
	out := map[workstream.Job]workstream.JobFacts{}
	for j, read := range b.workstreamJobReads(jobs) {
		if read.err != nil {
			continue
		}
		job := read.job
		merged := job.PR != nil && job.PR.MergedAt != nil && !job.PR.MergedAt.IsZero()
		// A job's money is all of its runs: Cost is only the current run once
		// it has run again, LifetimeCost keeps the earlier ones, and an older
		// factory that does not send LifetimeCost leaves Cost as the whole.
		cost := max(job.Cost, job.LifetimeCost)
		out[j] = workstream.JobFacts{State: job.State, Merged: &merged, CostUSD: &cost}
	}
	return out
}

// Collection resolution uses the same bounded batch reader, with each job
// asked once even when it appears through several nested collections.
func (r *collectionResolver) prepareWorkstreamRefs(members []workspace.Member) {
	jobs := []workstream.Job{}
	refs := map[workstream.Job]workspace.Ref{}
	for _, member := range members {
		if member.Kind != workspace.FactoryJobKind {
			continue
		}
		number, _ := strconv.Atoi(member.ID)
		job := workstream.Job{Factory: member.Factory, Number: number}
		jobs = append(jobs, job)
		refs[job] = member.Ref
	}
	r.factoryTargets = map[workspace.Ref]appwire.Target{}
	for job, read := range r.b.workstreamJobReads(jobs) {
		var target appwire.Target
		if read.err != nil {
			if read.notFound {
				target = appwire.Target{State: appwire.TargetDeleted}
			} else {
				target = appUnknown("the Factory", read.err)
				target.Status = "unknown"
			}
		} else {
			target = appwire.Target{State: appwire.TargetLive, Title: read.job.Title, Status: read.job.State, Repo: read.job.Repo}
		}
		r.factoryTargets[refs[job]] = target
	}
}

func (r *collectionResolver) prepareWorkstreamTree(level workspace.Level) {
	members := []workspace.Member{}
	var collect func(workspace.Level)
	collect = func(level workspace.Level) {
		for _, node := range level.Members {
			if node.Kind == workspace.FactoryJobKind {
				members = append(members, node.Member)
			}
			if node.Children != nil {
				collect(*node.Children)
			}
		}
	}
	collect(level)
	r.prepareWorkstreamRefs(members)
}

// workstreamObserved adds the first observed start to the caller's
// receipt, so mutations announce their write and observation in one event.
func (b *appBackend) workstreamObserved(s *workstream.Store, w workstream.Workstream, facts map[workstream.Job]workstream.JobFacts, c workstream.Change) (workstream.Workstream, workstream.Change, error) {
	if !w.StartedAt.IsZero() {
		return w, c, nil
	}
	states := map[workstream.Job]string{}
	active := false
	for _, j := range w.Jobs() {
		states[j] = facts[j].State
		switch states[j] {
		case "queued", "running", "needs_you", "done":
			active = true
		}
	}
	if !active {
		return w, c, nil
	}
	observed, err := s.ObserveJobStates(context.Background(), w.ID, states)
	if err != nil {
		return w, c, appWorkstreamError(err)
	}
	if c.ID == "" {
		c = observed
	} else {
		for _, part := range observed.Parts {
			if !slices.Contains(c.Parts, part) {
				c.Parts = append(c.Parts, part)
			}
		}
	}
	// Another reader may have won the write, so reread even with an empty receipt.
	w, err = s.Get(context.Background(), w.ID)
	return w, c, appWorkstreamError(err)
}

func (b *appBackend) workstreamSpend(id string) *float64 {
	if id == "" {
		return nil
	}
	if read := b.workstreams.spend; read != nil {
		return read(id)
	}
	a, err := b.find(id)
	if err != nil {
		row, ok := workstreamAgentRow(id)
		if !ok {
			return nil
		}
		b.remember(row)
		a, err = b.find(id)
		if err != nil {
			return nil
		}
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	if transcript == "" {
		return nil
	}
	if row, ok := session.ReadRows([]string{transcript})[transcript]; ok {
		if meta, err := session.LoadMeta(row.Dir); err != nil || meta.ID == "" {
			return nil
		}
		return &row.Spend
	}
	// The ordinary chat listing deliberately omits bare conversations. Their
	// freshly recorded metadata still establishes known zero spend here.
	meta, err := session.LoadMeta(session.DirOf(transcript))
	if err == nil && meta.ID != "" && meta.LastUserAt.IsZero() && meta.SpentUSD == 0 {
		zero := 0.0
		return &zero
	}
	return nil
}

// workstreamKeepsConversation protects a deliberately bare agent from both
// app and terminal blank-chat cleanup. Its durable owner is the SQLite row;
// an unreadable owner is a reason to retain a conversation, never to delete it.
func workstreamKeepsConversation(dir string) bool {
	path := home.Join("v3", "collections.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false
	} else if err != nil {
		return true
	}
	s, err := workspace.Open(path)
	if err != nil {
		return true
	}
	defer s.Close()
	kept := false
	err = s.Read(context.Background(), func(tx *sql.Tx, version int) error {
		if version < 3 {
			return nil
		}
		return tx.QueryRow("SELECT EXISTS(SELECT 1 FROM workstreams WHERE agent_id=?)", filepath.Base(filepath.Clean(dir))).Scan(&kept)
	})
	return kept || err != nil
}

// workstreamAgentRow restores a bare agent omitted by the ordinary chat
// listing. Workstream reads already obtained its id from the durable owner;
// remembering it lets agents.open use the existing opener after a restart.
func workstreamAgentRow(id string) (session.SessionRow, bool) {
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return session.SessionRow{}, false
	}
	root := session.PlacesRoot()
	buckets, err := os.ReadDir(root)
	if err != nil {
		return session.SessionRow{}, false
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		dir := filepath.Join(root, bucket.Name(), id)
		meta, err := session.LoadMeta(dir)
		if err != nil || meta.ID != id || !meta.LastUserAt.IsZero() {
			continue
		}
		transcript := (session.Place{Dir: dir}).Transcript()
		if _, err := os.Stat(transcript); err != nil {
			continue
		}
		return session.SessionRow{ID: id, Dir: dir, Transcript: transcript, Workspace: meta.Workspace, Owned: meta.Owned, Title: meta.Title, Model: meta.Model, Created: meta.Created, At: meta.Created, Spend: meta.SpentUSD, Archived: meta.Archived}, true
	}
	return session.SessionRow{}, false
}
