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
}

type crewFailureRecord struct {
	Kind    provider.RouteFailure
	Until   time.Time
	Message string
}
type crewGuardRecord struct {
	Cap, TaskCap                         float64
	CapAction, TaskAction, CeilingAction string
	SeatCeilings                         map[crewroute.Seat]float64
	ModelSpent                           map[string]float64
	TaskSpent                            float64
	SeatSpent                            map[crewroute.Seat]float64
	Day                                  string
	DaySpent                             float64
	DayLast                              map[string]float64
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
		Swaps: c.swaps, FreeTried: c.freeTried, HelperUSD: c.helperUSD, DayAtStart: c.dayAtStart, InFlight: c.inFlight, Failed: map[string]crewFailureRecord{}}
	for send, f := range c.failed {
		msg := ""
		if f.err != nil {
			msg = f.err.Error()
		}
		r.Failed[send] = crewFailureRecord{f.kind, f.until, msg}
	}
	if g := c.guard; g != nil {
		g.mu.Lock()
		r.Guard = &crewGuardRecord{Cap: g.Cap, TaskCap: g.TaskCap, CapAction: g.CapAction, TaskAction: g.TaskAction, CeilingAction: g.CeilingAction,
			SeatCeilings: maps.Clone(g.SeatCeilings), ModelSpent: maps.Clone(g.modelSpent), TaskSpent: g.Task.Total(), SeatSpent: map[crewroute.Seat]float64{}, Day: time.Now().Format("2006-01-02")}
		for seat, tally := range g.seatSpent {
			r.Guard.SeatSpent[seat] = tally.Total()
		}
		if g.Day != nil {
			g.Day.mu.Lock()
			r.Guard.DaySpent = g.Day.usd + g.Day.since
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
		started: r.Started, bad: r.Bad, broke: r.Broke, gone: r.Gone, swaps: r.Swaps, freeTried: r.FreeTried, helperUSD: r.HelperUSD, dayAtStart: r.DayAtStart, failed: map[string]crewFailure{}}
	for send, f := range r.Failed {
		c.failed[send] = crewFailure{kind: f.Kind, until: f.Until, err: errors.New(f.Message)}
	}
	c.decision.Ladder = r.Ladders
	s := r.Guard
	day := a.crewDay()
	if s.Day == time.Now().Format("2006-01-02") {
		day.mu.Lock()
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
		day.mu.Unlock()
	}
	c.day = day
	c.guard = &SpendGuard{Price: config.CrewCallPriceAt(a.config.ProfileDir), Day: day, Cap: s.Cap, TaskCap: s.TaskCap, CapAction: s.CapAction, TaskAction: s.TaskAction,
		CeilingAction: s.CeilingAction, SeatCeilings: s.SeatCeilings, modelSpent: s.ModelSpent, Task: &SpendTask{spent: s.TaskSpent}, seatSpent: map[crewroute.Seat]*SpendTask{}}
	for seat, spent := range s.SeatSpent {
		c.guard.seatSpent[seat] = &SpendTask{spent: spent}
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
