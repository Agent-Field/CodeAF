package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

const workstreamWakeTimeout = 3 * time.Minute

// One active call across backends also bounds tests and embedded app instances.
var workstreamWakeTurnMu sync.Mutex

type workstreamJobEvent struct {
	Factory string
	Job     int
	Kind    string
	ID      string
	At      time.Time
	Label   string
	Detail  string
}

type workstreamWakeTask struct{ Run workstream.WakeRun }

// WorkstreamJobEvent is the admission hook for the future job event source.
// Queue reads do not call it: each occurrence must have a stable source identity.
func (b *appBackend) WorkstreamJobEvent(ev workstreamJobEvent) (wakeID string, woke bool) {
	if ev.At.IsZero() || ev.Factory == "" || ev.Job <= 0 || ev.Kind == "morning" {
		return "", false
	}
	occurrence := ev.ID
	if occurrence == "" {
		occurrence = ev.At.UTC().Format(time.RFC3339)
	}
	key := fmt.Sprintf("job:%s:%d:%s:%s", ev.Factory, ev.Job, ev.Kind, occurrence)
	event := workstream.WakeEvent{Kind: ev.Kind, Factory: ev.Factory, Job: ev.Job, At: ev.At, Label: ev.Label, Detail: ev.Detail}
	return b.workstreamAdmitWake("", key, event)
}

func (b *appBackend) workstreamAdmitWake(id, key string, event workstream.WakeEvent) (string, bool) {
	s, err := b.workstreamStore(false)
	if err != nil || s == nil {
		return "", false
	}
	run, woke, err := s.BeginWake(context.Background(), id, key, event)
	if err != nil {
		log.Printf("workstream wake: %v", err)
		return "", false
	}
	if !woke {
		return "", false
	}
	b.workstreamEnqueueWake(workstreamWakeTask{Run: run})
	return run.ID, true
}

func (b *appBackend) workstreamEnqueueWake(task workstreamWakeTask) {
	state := &b.workstreams
	if state.wakeDispatch != nil {
		state.wakeDispatch(task)
		return
	}
	state.wakeMu.Lock()
	defer state.wakeMu.Unlock()
	if state.wakeSignal == nil {
		state.wakeSignal = make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		state.wakeCancel = cancel
		guard.Go("app/workstream-wake-worker", func() { b.workstreamWakeWorker(ctx) })
	}
	state.wakeQueue = append(state.wakeQueue, task)
	select {
	case state.wakeSignal <- struct{}{}:
	default:
	}
}

func (b *appBackend) workstreamWakeWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.workstreams.wakeSignal:
		}
		for {
			if ctx.Err() != nil {
				return
			}
			task, ok := b.workstreamNextWake()
			if !ok {
				break
			}
			b.workstreamRunWake(ctx, task)
		}
	}
}

func (b *appBackend) watchWorkstreamWakes(stop <-chan struct{}) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	defer func() {
		b.workstreams.wakeMu.Lock()
		defer b.workstreams.wakeMu.Unlock()
		if b.workstreams.wakeCancel != nil {
			b.workstreams.wakeCancel()
		}
	}()
	for {
		select {
		case <-stop:
			return
		case <-timer.C:
		}
		b.workstreamWakeTick(b.now())
		timer.Reset(time.Minute)
	}
}

func (b *appBackend) workstreamExpireWakes(s *workstream.Store) error {
	changes, err := s.ExpireWakes(context.Background())
	if err == nil {
		for _, change := range changes {
			b.workstreamChanged(change)
		}
	}
	return err
}

// workstreamMorningSlot uses the automation clock's local-zone cron semantics.
func workstreamMorningSlot(now time.Time) (time.Time, error) {
	local := now.In(time.Local)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	schedule := automation.Schedule{Every: "0 8 * * *"}
	slot, err := schedule.Next(midnight.Add(-time.Minute))
	if err != nil || slot.After(now) {
		return time.Time{}, err
	}
	return slot, nil
}
func (b *appBackend) workstreamWakeTick(now time.Time) {
	s, err := b.workstreamStore(false)
	if err != nil || s == nil {
		return
	}
	if err := b.workstreamExpireWakes(s); err != nil {
		log.Printf("workstream wake: %v", err)
		return
	}
	slot, err := workstreamMorningSlot(now)
	if err != nil || slot.IsZero() {
		return
	}
	streams, err := s.List(context.Background())
	if err != nil {
		return
	}
	for _, w := range streams {
		if w.Stage == "done" || w.Autonomy == "off" || !w.Wake.Morning || slot.Before(w.CreatedAt) {
			continue
		}
		b.workstreamAdmitWake(w.ID, "morning:"+slot.In(time.Local).Format("2006-01-02"), workstream.WakeEvent{Kind: "morning", At: slot})
	}
}

// workstreamNextWake takes the oldest queued wake, if any.
func (b *appBackend) workstreamNextWake() (workstreamWakeTask, bool) {
	b.workstreams.wakeMu.Lock()
	defer b.workstreams.wakeMu.Unlock()
	if len(b.workstreams.wakeQueue) == 0 {
		return workstreamWakeTask{}, false
	}
	task := b.workstreams.wakeQueue[0]
	b.workstreams.wakeQueue = b.workstreams.wakeQueue[1:]
	return task, true
}

func (b *appBackend) workstreamRunWake(parent context.Context, task workstreamWakeTask) {
	workstreamWakeTurnMu.Lock()
	defer workstreamWakeTurnMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, workstreamWakeTimeout)
	defer cancel()
	s, err := b.workstreamStore(false)
	if err != nil || s == nil {
		return
	}
	run, err := s.WakeRun(ctx, task.Run.ID)
	if err != nil || run.State != "running" {
		return
	}
	if b.now().Sub(run.StartedAt) > workstream.WakeAbandonedAfter {
		_ = b.workstreamExpireWakes(s)
		return
	}
	task.Run = run
	w, err := s.Get(ctx, task.Run.WorkstreamID)
	if err != nil {
		return
	}
	// A queued turn must not call the model after done or off was chosen.
	if w.Stage == "done" || w.Autonomy == "off" {
		_, change, err := s.SettleWake(context.Background(), task.Run.ID, workstream.WakeAnswer{}, nil, "")
		if err == nil {
			b.workstreamChanged(change)
		}
		return
	}
	ask, err := b.workstreamWakeSnapshot(ctx, s, w, task.Run)
	var answer session.WorkstreamWakeAnswer
	if err == nil {
		turn := b.workstreams.wakeTurn
		if turn == nil {
			turn = b.workstreamRealWakeTurn
		}
		answer, err = turn(ctx, ask)
	}
	parsed := workstream.WakeAnswer{}
	failure := ""
	if err != nil {
		failure = "The agent could not finish this wake."
	} else {
		parsed, err = workstream.ParseWakeAnswer(answer.Text)
		if err != nil {
			failure = workstream.WakeAnswerError
		}
	}
	_, change, err := s.SettleWake(context.Background(), task.Run.ID, parsed, answer.CostUSD, failure)
	if err != nil {
		// A refused small change rolls back the entire batch; its call cost is still owed.
		_, change, err = s.SettleWake(context.Background(), task.Run.ID, workstream.WakeAnswer{}, answer.CostUSD, "The agent's changes could not be saved.")
	}
	if err == nil {
		b.workstreamChanged(change)
	} else {
		log.Printf("workstream wake settle: %v", err)
	}
}

func (b *appBackend) workstreamWakeSnapshot(ctx context.Context, s *workstream.Store, w workstream.Workstream, run workstream.WakeRun) (session.WorkstreamWakeAsk, error) {
	door := &workstreamAgentDoor{backend: b, agentID: w.AgentID}
	snapshot := map[string]json.RawMessage{}
	for _, what := range []string{"workstream", "plan", "pages"} {
		raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: what})
		if err != nil {
			return session.WorkstreamWakeAsk{}, err
		}
		snapshot[what] = raw
	}
	records := appwire.WorkstreamRecordListResult{Records: []appwire.WorkstreamRecord{}}
	for _, state := range []string{"kept", "proposed"} {
		rows, err := s.Records(ctx, w.ID, "", state, true)
		if err != nil {
			return session.WorkstreamWakeAsk{}, err
		}
		for _, r := range rows {
			records.Records = append(records.Records, appWorkstreamRecord(r))
		}
	}
	rawRecords, err := json.Marshal(records)
	if err != nil {
		return session.WorkstreamWakeAsk{}, err
	}
	snapshot["records"] = rawRecords
	grants, err := s.GoAheads(ctx, w.ID, false)
	if err != nil {
		return session.WorkstreamWakeAsk{}, err
	}
	goaheads := appwire.WorkstreamGoAheadListResult{GoAheads: []appwire.WorkstreamGoAhead{}}
	for _, g := range grants {
		goaheads.GoAheads = append(goaheads.GoAheads, appWorkstreamGoAhead(g))
	}
	rawGrants, err := json.Marshal(goaheads)
	if err != nil {
		return session.WorkstreamWakeAsk{}, err
	}
	snapshot["goaheads"] = rawGrants
	pages, err := s.Pages(ctx, w.ID)
	if err != nil {
		return session.WorkstreamWakeAsk{}, err
	}
	for _, p := range pages {
		if p.Owner != "agent" || p.Title != session.WorkstreamWhereItStandsTitle {
			continue
		}
		raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "page", PageID: p.ID})
		if err != nil {
			return session.WorkstreamWakeAsk{}, err
		}
		snapshot["where_it_stands"] = raw
		break
	}
	// The existing read mappers preserve numbers, plan draft ids and page ownership.
	raw, err := json.Marshal(snapshot)
	return session.WorkstreamWakeAsk{ID: run.ID, AgentID: w.AgentID, Event: run.Event, Snapshot: raw}, err
}

func (b *appBackend) workstreamRealWakeTurn(ctx context.Context, ask session.WorkstreamWakeAsk) (session.WorkstreamWakeAnswer, error) {
	source, err := v3RolesSource("", b.crewProfile())
	if err != nil {
		return session.WorkstreamWakeAnswer{}, err
	}
	proc, err := openAppEngineProcess()
	if err != nil {
		return session.WorkstreamWakeAnswer{}, err
	}
	settings := proc.Settings
	return session.RunWorkstreamWake(ctx, session.TeamProposerConfig{Model: settings.Model, APIKey: settings.APIKey, BaseURL: settings.BaseURL, Sources: settings.Sources, ProfileDir: settings.ProfileDir, RolesSource: source}, ask)
}

func appWorkstreamWake(run workstream.WakeRun) appwire.WorkstreamWakeRun {
	out := appwire.WorkstreamWakeRun{ID: run.ID, WorkstreamID: run.WorkstreamID, Event: appwire.WorkstreamWakeEvent{Kind: run.Event.Kind, Factory: workstreamOptional(run.Event.Factory), Job: workstreamPositive(run.Event.Job), Label: workstreamOptional(run.Event.Label)}, State: run.State, StartedAt: run.StartedAt, SettledAt: run.SettledAt, CostUSD: run.CostUSD, Summary: workstreamOptional(run.Summary), Error: workstreamOptional(run.Error), DismissedAt: run.DismissedAt, Items: []appwire.WorkstreamWakeItem{}}
	for _, item := range run.Items {
		out.Items = append(out.Items, appwire.WorkstreamWakeItem{ID: item.ID, Kind: item.Kind, State: item.State, Text: item.Text, Why: workstreamOptional(item.Why), Factory: workstreamOptional(item.Factory), Job: workstreamPositive(item.Job), DraftID: workstreamOptional(item.DraftID), Wave: workstreamPositive(item.Wave), PageID: workstreamOptional(item.PageID), RecordID: workstreamOptional(item.RecordID), BudgetUSD: item.BudgetUSD, Note: workstreamOptional(item.Note), AppliedAt: item.AppliedAt, UndoneAt: item.UndoneAt})
	}
	return out
}

func (b *appBackend) WorkstreamWakeList(p appwire.WorkstreamWakeListParams) (appwire.WorkstreamWakeListResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamWakeListResult, workstream.Change, error) {
		out := appwire.WorkstreamWakeListResult{Wakes: []appwire.WorkstreamWakeRun{}}
		if err := b.workstreamExpireWakes(s); err != nil {
			return out, workstream.Change{}, err
		}
		limit := 20
		if p.Limit != nil {
			limit = *p.Limit
		}
		wakes, err := s.Wakes(context.Background(), p.WorkstreamID, limit)
		for _, run := range wakes {
			out.Wakes = append(out.Wakes, appWorkstreamWake(run))
		}
		return out, workstream.Change{}, err
	})
}
func (b *appBackend) WorkstreamWakeApply(p appwire.WorkstreamWakeApplyParams) (appwire.WorkstreamWakeApplyResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamWakeApplyResult, workstream.Change, error) {
		item := ""
		if p.ItemID != nil {
			item = *p.ItemID
		}
		run, c, err := s.ActOnWake(context.Background(), p.ID, item, "apply")
		return appwire.WorkstreamWakeApplyResult{Wake: appWorkstreamWake(run)}, c, err
	})
}
func (b *appBackend) WorkstreamWakeUndo(p appwire.WorkstreamWakeUndoParams) (appwire.WorkstreamWakeUndoResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamWakeUndoResult, workstream.Change, error) {
		item := ""
		if p.ItemID != nil {
			item = *p.ItemID
		}
		run, c, err := s.ActOnWake(context.Background(), p.ID, item, "undo")
		return appwire.WorkstreamWakeUndoResult{Wake: appWorkstreamWake(run)}, c, err
	})
}
func (b *appBackend) WorkstreamWakeNotNow(p appwire.WorkstreamWakeNotNowParams) (appwire.WorkstreamWakeNotNowResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamWakeNotNowResult, workstream.Change, error) {
		run, c, err := s.ActOnWake(context.Background(), p.ID, "", "not_now")
		return appwire.WorkstreamWakeNotNowResult{Wake: appWorkstreamWake(run)}, c, err
	})
}
