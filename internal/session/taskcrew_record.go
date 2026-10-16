package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// TaskCrewRecord is the accepted routing policy and its accumulated call state.
// Version distinguishes an explicitly unrouted run from an old, incomplete row.
// An interrupted call cannot be priced reliably, so InFlight refuses recovery.
type TaskCrewRecord struct {
	Version                     int  `json:"version"`
	Routed                      bool `json:"routed,omitempty"`
	Work, Plan, Check, OneModel string
	Call, Title, Brief, Repo    string
	Decision                    crewroute.Decision
	Original                    map[crewroute.Seat]string
	Ladders                     map[crewroute.Seat][]crewroute.Pick
	Rescue                      map[crewroute.Seat][]crewroute.Pick
	Started, Bad, Broke, Gone   map[string]bool
	Swaps                       map[string]string
	FreeTried                   map[crewroute.Seat]int
	Failed                      map[string]crewFailureRecord
	HelperUSD, DayAtStart       float64
	Guard                       *crewGuardRecord
	InFlight                    int
	// Changes are the task's model changes, bounded; ChangeCount counts them
	// all. TaskPins and Pending are the roles a person pinned for this task
	// and those whose change has not answered yet. Restored is every time the
	// crew came back from this checkpoint. All are absent on older rows.
	Changes     []TaskCrewChange        `json:",omitempty"`
	ChangeCount int                     `json:",omitempty"`
	TaskPins    map[crewroute.Seat]bool `json:",omitempty"`
	Pending     map[crewroute.Seat]bool `json:",omitempty"`
	Restored    []time.Time             `json:",omitempty"`
	// RestoreCount counts every restart; Restored keeps the newest 16.
	RestoreCount int                       `json:",omitempty"`
	SeatSends    map[crewroute.Seat]string `json:",omitempty"`
	// RoleFacts is 1 on a crew whose per-role facts — SeatStarted and the
	// role tallies with their call counts — were kept since its admission.
	// Absent (0) is an older checkpoint: which role ran, and whether a zero
	// role spend was a real zero, is unknown.
	RoleFacts   int                     `json:",omitempty"`
	SeatStarted map[crewroute.Seat]bool `json:",omitempty"`
	// FailedSent is, for a send in Failed, when it failed, the provider it was
	// routed to and the print of the credential it was sent with — what lets
	// a refusal its credential has since answered go when the task is carried
	// on. One entry per failed send, never a credential; absent on older
	// rows, whose auth and payment refusals then stay held.
	FailedSent map[string]crewFailureSent `json:",omitempty"`
}

type crewFailureRecord struct {
	Kind    provider.RouteFailure
	Until   time.Time
	Message string
}

// crewFailureSent is a failed send's dispatch facts ([crewFailure]).
type crewFailureSent struct {
	At       time.Time `json:",omitzero"`
	Provider string    `json:",omitempty"`
	Cred     string    `json:",omitempty"`
}
type crewGuardRecord struct {
	Cap, TaskCap                         float64
	CapAction, TaskAction, CeilingAction string
	SeatCeilings                         map[crewroute.Seat]float64
	ModelSpent                           map[string]float64
	TaskSpent                            float64
	SeatSpent                            map[crewroute.Seat]float64
	// TaskCalls/TaskUnknown and SeatCalls/SeatUnknown count answered calls
	// and those whose cost was unknown; absent on older rows.
	TaskCalls, TaskUnknown int                    `json:",omitempty"`
	SeatCalls, SeatUnknown map[crewroute.Seat]int `json:",omitempty"`
	Day                    string
	DaySpent               float64
	DayLast                map[string]float64
}

func (a *Agent) unroutedCrewRecord() *TaskCrewRecord {
	r := &TaskCrewRecord{Version: 1}
	source := roles.Source(a.config.RolesSource)
	r.Work, _ = roles.TierModel(source, roles.TierWorker)
	r.Plan, _ = roles.TierModel(source, roles.TierMastermind)
	if a.config.OneModel {
		r.OneModel = a.Model()
		r.Work, r.Plan, r.Check = r.OneModel, r.OneModel, r.OneModel
	}
	return r
}

// record freezes maps while their owner is locked; the returned checkpoint is
// detached from live routing and spend mutations before the store serializes it.
func (c *taskCrew) record() *TaskCrewRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := &TaskCrewRecord{Version: 1, Routed: true, Call: c.call, Title: c.title, Brief: c.brief, Repo: c.repo,
		Decision: c.decision, Original: c.original, Ladders: c.ladders, Rescue: c.rescue, Started: c.started, Bad: c.bad, Broke: c.broke, Gone: c.gone,
		Swaps: c.swaps, FreeTried: c.freeTried, HelperUSD: c.helperUSD, DayAtStart: c.dayAtStart, InFlight: c.inFlight,
		Changes: c.changes, ChangeCount: c.changeCount, TaskPins: c.taskPins, Pending: c.pending, Restored: c.restored, RestoreCount: c.restoreCount, SeatSends: c.seatSends, SeatStarted: c.seatStarted}
	if c.roleFacts {
		r.RoleFacts = 1
	}
	r.Failed, r.FailedSent = c.failureRecordsLocked()
	if g := c.guard; g != nil {
		g.mu.Lock()
		r.Guard = &crewGuardRecord{Cap: g.Cap, TaskCap: g.TaskCap, CapAction: g.CapAction, TaskAction: g.TaskAction, CeilingAction: g.CeilingAction,
			SeatCeilings: maps.Clone(g.SeatCeilings), ModelSpent: maps.Clone(g.modelSpent), SeatSpent: map[crewroute.Seat]float64{}, Day: time.Now().Format("2006-01-02")}
		// The task's and the roles' figures are read as one booking left them.
		g.bookMu.Lock()
		r.Guard.TaskSpent, r.Guard.TaskCalls, r.Guard.TaskUnknown = g.Task.snapshot()
		for seat, tally := range g.seatSpent {
			spent, calls, unknown := tally.snapshot()
			r.Guard.SeatSpent[seat] = spent
			if calls > 0 {
				if r.Guard.SeatCalls == nil {
					r.Guard.SeatCalls, r.Guard.SeatUnknown = map[crewroute.Seat]int{}, map[crewroute.Seat]int{}
				}
				r.Guard.SeatCalls[seat], r.Guard.SeatUnknown[seat] = calls, unknown
			}
		}
		g.bookMu.Unlock()
		if g.Day != nil {
			g.Day.mu.Lock()
			g.Day.refreshLocked()
			if g.Day.date != "" {
				r.Guard.Day = g.Day.date
			}
			r.Guard.DaySpent = g.Day.totalLocked()
			r.Guard.DayLast = maps.Clone(g.Day.last)
			g.Day.mu.Unlock()
		}
		g.mu.Unlock()
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	var frozen TaskCrewRecord
	if json.Unmarshal(data, &frozen) != nil {
		return nil
	}
	return &frozen
}

// failureRecordsLocked is the crew's failures as its checkpoint keeps them:
// how each send failed, and, apart, what is known of the call that failed —
// absent for a failure that knows nothing of it. The caller holds c.mu.
func (c *taskCrew) failureRecordsLocked() (map[string]crewFailureRecord, map[string]crewFailureSent) {
	failed := map[string]crewFailureRecord{}
	var sent map[string]crewFailureSent
	for send, f := range c.failed {
		msg := ""
		if f.err != nil {
			msg = f.err.Error()
		}
		failed[send] = crewFailureRecord{f.kind, f.until, msg}
		if f.at.IsZero() && f.provider == "" && f.sent.Cred == "" {
			continue
		}
		if sent == nil {
			sent = map[string]crewFailureSent{}
		}
		sent[send] = crewFailureSent{At: f.at, Provider: f.provider, Cred: f.sent.Cred}
	}
	return failed, sent
}

// restoreTaskCrew never asks the router again: a restarted run has already
// accepted its seats, fallback history and spend ceilings.
func (a *Agent) restoreTaskCrew(row uint64, r *TaskCrewRecord) (*taskCrew, error) {
	if r == nil || r.Version != 1 {
		return nil, errors.New("the saved task has no complete crew policy; ask for the task again")
	}
	if r.InFlight != 0 {
		return nil, errors.New("a model call was interrupted before its cost was saved; inspect the work and ask for the task again")
	}
	if !r.Routed {
		return nil, nil
	}
	if r.Guard == nil {
		return nil, errors.New("the saved task has no complete spending policy; ask for the task again")
	}
	for _, seat := range crewroute.Seats {
		if r.Original[seat] == "" || r.Decision.Seat(seat).Send == "" {
			return nil, errors.New("the saved task is missing an accepted crew seat; ask for the task again")
		}
	}
	// Decode another copy so restored mutable maps never change the durable row.
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var saved TaskCrewRecord
	if err = json.Unmarshal(data, &saved); err != nil {
		return nil, err
	}
	r = &saved
	c := &taskCrew{call: r.Call, title: r.Title, brief: r.Brief, repo: r.Repo, decision: r.Decision, original: r.Original, ladders: r.Ladders, rescue: r.Rescue,
		started: r.Started, bad: r.Bad, broke: r.Broke, gone: r.Gone, swaps: r.Swaps, freeTried: r.FreeTried, helperUSD: r.HelperUSD, dayAtStart: r.DayAtStart, failed: map[string]crewFailure{},
		changes: r.Changes, changeCount: r.ChangeCount, taskPins: r.TaskPins, pending: r.Pending, seatSends: r.SeatSends,
		seatStarted: r.SeatStarted, roleFacts: r.RoleFacts >= 1}
	// THE RESTART IS A FACT OF THE TASK, said once in its history and kept:
	// the picks below are the ones it was admitted with, not a new decision.
	c.restoreCount = r.RestoreCount
	c.noteRestored(a.taskClockNow(), r.Restored)
	for send, f := range r.Failed {
		sent := r.FailedSent[send]
		c.failed[send] = crewFailure{kind: f.Kind, until: f.Until, err: errors.New(f.Message),
			at: sent.At, provider: sent.Provider, sent: config.CrewDispatch{Cred: sent.Cred}}
	}
	c.decision.Ladder = r.Ladders
	s := r.Guard
	day := a.crewDay()
	restoreCrewDay(day, s)
	c.day = day
	// A RESTORED CREW HOLDS THE LIMITS AS THEY STAND NOW: the saved Cap folded
	// the daily limit in as it was at admission, and a raise since would never
	// reach it. The crew's own cap is read afresh and the daily limit at each call.
	crewCap, crewCapAction := config.CrewSpendCap(a.config.ProfileDir, false)
	c.guard = &SpendGuard{Price: config.CrewCallPriceAt(a.config.ProfileDir), Day: day, Cap: crewCap, TaskCap: s.TaskCap, CapAction: crewCapAction, TaskAction: s.TaskAction,
		DayLimit:      config.CrewDailyLimit(a.config.ProfileDir),
		CeilingAction: s.CeilingAction, SeatCeilings: s.SeatCeilings, modelSpent: s.ModelSpent, Task: &SpendTask{spent: s.TaskSpent, calls: s.TaskCalls, unknown: s.TaskUnknown}, seatSpent: map[crewroute.Seat]*SpendTask{}}
	for seat, spent := range s.SeatSpent {
		c.guard.seatSpent[seat] = &SpendTask{spent: spent, calls: s.SeatCalls[seat], unknown: s.SeatUnknown[seat]}
	}
	a.bindCrewCheckpoint(row, c)
	a.crews.put(row, c)
	return c, nil
}

func (a *Agent) bindCrewCheckpoint(row uint64, c *taskCrew) {
	c.persist = func() error {
		c.persistMu.Lock()
		defer c.persistMu.Unlock()
		record := c.record()
		if record == nil {
			return errors.New("could not save the task's crew policy")
		}
		g := a.graph()
		if g == nil {
			return errors.New("the task has no durable conversation")
		}
		g.mu.Lock()
		rows := g.runs[row]
		found := false
		for i := range rows {
			if rows[i].ID == row {
				rows[i].CrewState = record
				decision := record.Decision
				rows[i].Crew = &decision
				found = true
			}
		}
		g.mu.Unlock()
		if !found {
			return errors.New("the task's crew row is not saved")
		}
		if err := g.store.write(g); err != nil {
			return fmt.Errorf("save task crew before continuing: %w", err)
		}
		return nil
	}
}

// beginCall records uncertainty before any request leaves the process. Ending
// a call checkpoints its routing and spend together; a crash in between refuses
// automatic replay instead of resetting a budget or trying a pinned seat again.
func (c *taskCrew) beginCall() error {
	if c == nil || c.persist == nil {
		return nil
	}
	c.mu.Lock()
	c.inFlight++
	c.mu.Unlock()
	return c.persist()
}
func (c *taskCrew) endCall() {
	if c == nil || c.persist == nil {
		return
	}
	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	// A failed completion write leaves the earlier in-flight checkpoint on disk.
	_ = c.persist()
}

// restoreCrewDay retains the saved same-day lower bounds without reducing the
// current ledger reading or carrying yesterday's spend into a fresh day.
func restoreCrewDay(day *SpendDay, s *crewGuardRecord) {
	day.mu.Lock()
	defer day.mu.Unlock()
	// Refresh under the day's lock before copying any saved lower bound. The
	// ledger may still lag, but a calendar rollover must expire yesterday first.
	day.refreshLocked()
	today := time.Now().Format("2006-01-02")
	if day.date != "" {
		today = day.date
	}
	if s.Day != today {
		return
	}
	if s.DaySpent > day.usd+day.since {
		day.usd = s.DaySpent - day.since
	}
	if day.last == nil {
		day.last = map[string]float64{}
	}
	for model, cost := range s.DayLast {
		if cost > day.last[model] {
			day.last[model] = cost
		}
	}
}
