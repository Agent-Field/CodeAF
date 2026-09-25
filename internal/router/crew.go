package router

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// A CREW'S DECISION AND ITS OUTCOME, IN THE SAME DIARY AS EVERY OTHER ROUTE.
//
// The crew router (internal/crewroute) picks a task's worker, planner and
// checker from the class of work the task is, and what it learns from —
// whether the person accepted the result or asked for it redone stronger — is
// exactly the kind of row this log exists to keep: a decision with the
// candidates it was made among, and a later row that settles it. So the crew
// writes here, under [CrewClass], rather than into a second file that would be
// a second answer to "what did the router do and how did it go".
//
// Two rows per task, joined by the call id: the DECISION, written when the
// crew is picked, and the OUTCOME, written Final when the task is accepted or
// redone. A reader takes the last row for a call id as the truth, the same
// rule every other row here keeps.

// CrewClass is the class every crew row is written under.
const CrewClass = "crew"

// The outcomes a crew row settles with.
const (
	// CrewAccepted is a task whose result was kept: it landed, or the person
	// took it.
	CrewAccepted = "accepted"
	// CrewRedone is a task the person asked to be done again by a stronger
	// crew. It is the learning signal: the class was under-served here.
	CrewRedone = "redo stronger"
	// CrewNotKept is a task that ended without a result anybody kept — it
	// failed, or was stopped. It teaches nothing about the crew's strength.
	CrewNotKept = "not kept"
)

// CrewRecord is the crew half of a row.
type CrewRecord struct {
	// TaskClass is the class the task was read as — bugfix, openended, other.
	TaskClass string `json:"task_class"`
	// TaskSubclass is, for a bugfix, "complex" or "simple", and TaskReach the
	// signals that made it complex. They are the log's alone: every line a
	// person reads says bugfix.
	TaskSubclass string `json:"task_subclass,omitempty"`
	TaskReach    string `json:"task_reach,omitempty"`
	// Repo is the repository the task ran in, the key the learned offset is
	// kept under.
	Repo   string `json:"repo,omitempty"`
	Title  string `json:"title,omitempty"`
	Effort string `json:"effort,omitempty"`
	Steps  int    `json:"steps,omitempty"`
	// Seats is seat → the id that ran; Providers seat → the route; Kinds
	// seat → how that route bills (metered, plan, local).
	Seats     map[string]string `json:"seats"`
	Providers map[string]string `json:"providers,omitempty"`
	Kinds     map[string]string `json:"kinds,omitempty"`
	Pinned    []string          `json:"pinned,omitempty"`
	EstUSD    float64           `json:"est_usd,omitempty"`
	// Failed is seat → the id that failed to start on it, on a row written
	// when a seat's first call was refused ([LogCrewRoute]), with the kind of
	// failure, the provider of the route and when it said it may be asked
	// again. Started is seat → the id whose first call answered. Both are
	// ROUTE outcomes — did the call work — and never the task's.
	Failed      map[string]string `json:"failed,omitempty"`
	FailKind    string            `json:"fail_kind,omitempty"`
	FailRoute   string            `json:"fail_route,omitempty"`
	FailUntil   time.Time         `json:"fail_until,omitempty"`
	Started     map[string]string `json:"started,omitempty"`
	StartedPaid bool              `json:"started_paid,omitempty"`
	// Redo is a row of a task that was itself a redo: its acceptance is the
	// redo's own and decays no offset — only a LATER accepted task of the
	// class there does.
	Redo bool `json:"redo,omitempty"`
	// EstBase is the estimate before this install's learned factor, the one
	// the factor is learned against ([CrewLog.CostFactor]).
	EstBase float64 `json:"est_base,omitempty"`
	// Top is, per seat, the best few candidates the decision weighed, with
	// their route, quality, cost and score: why the crew is the crew.
	Top map[string][]CrewScore `json:"top,omitempty"`
	// Learned is seat → the move this install's own outcomes made to the
	// seat's model's quality when the crew was picked ([CrewLog.Quality]).
	Learned map[string]float64 `json:"learned,omitempty"`
}

// CrewScore is one weighed candidate on a decision row, kept short.
type CrewScore struct {
	Model string  `json:"m"`
	Route string  `json:"r,omitempty"`
	Q     float64 `json:"q"`
	C     float64 `json:"c"`
	S     float64 `json:"s"`
}

// LogCrewDecision appends a crew's decision row. Best-effort by construction:
// a log that will not open costs a lesson, never a task.
func LogCrewDecision(dir, call string, record CrewRecord, candidates []string) {
	events, err := OpenEvents(dir)
	if err != nil {
		return
	}
	defer events.Close()
	events.Append(Event{Call: call, Run: record.Repo, Class: CrewClass, Shape: record.TaskClass,
		Candidates: candidates, Model: record.Seats["worker"], Crew: &record})
}

// LogCrewOutcome appends the row that settles a crew's decision.
func LogCrewOutcome(dir, call string, record CrewRecord, outcome string, costUSD float64) {
	events, err := OpenEvents(dir)
	if err != nil {
		return
	}
	defer events.Close()
	events.Append(Event{Call: call, Run: record.Repo, Class: CrewClass, Shape: record.TaskClass,
		Model: record.Seats["worker"], Crew: &record, Final: true, Outcome: outcome, Cost: costUSD})
}

// CrewRouteOutcome is one seat's first call on one route, as the log keeps it:
// the route answered (Kind empty), or it failed with a kind of failure
// (internal/provider's RouteFailure words), on a provider, until a time it
// may be asked again.
type CrewRouteOutcome struct {
	At       time.Time
	Seat     string
	Send     string
	Provider string
	// Paid is whether the route bills per token — a success on a paid route is
	// what clears an account's payment failure.
	Paid  bool
	Kind  string
	Until time.Time
}

// LogCrewRoute appends one route outcome for a task's crew. It is not the
// task's ending, so it is written as a decision row, never a final one.
func LogCrewRoute(dir, call string, record CrewRecord, outcome CrewRouteOutcome) {
	events, err := OpenEvents(dir)
	if err != nil {
		return
	}
	defer events.Close()
	if outcome.Kind == "" {
		record.Started = map[string]string{outcome.Seat: outcome.Send}
		record.StartedPaid = outcome.Paid
	} else {
		record.Failed = map[string]string{outcome.Seat: outcome.Send}
		record.FailKind, record.FailRoute, record.FailUntil = outcome.Kind, outcome.Provider, outcome.Until
	}
	events.Append(Event{Call: call, Run: record.Repo, Class: CrewClass, Shape: record.TaskClass,
		Model: outcome.Send, Crew: &record})
}

// crewOutcomesFor is how far back route outcomes are read: longer than the
// longest quarantine any of them sets.
const crewOutcomesFor = 14 * 24 * time.Hour

// CrewTask is one task as the log remembers it.
type CrewTask struct {
	At      time.Time
	Call    string
	Record  CrewRecord
	Outcome string
	CostUSD float64
	Settled bool
}

// CrewLog is what the log says about crews.
type CrewLog struct {
	// SpentUSD is today's settled crew spend; Tasks, OnPlan and Local count
	// today's tasks, and how many of them ran their worker on a subscription
	// plan or a local model.
	SpentUSD float64
	Tasks    int
	OnPlan   int
	Local    int
	// ProviderUSD is today's settled crew spend by the provider that carried
	// it ([crewProviderShares]), for the /crew panel's list of providers.
	ProviderUSD map[string]float64
	// Recent is the last few tasks, newest first.
	Recent []CrewTask
	// Offsets is the learned escalation offset per repository and class,
	// keyed repo + "\x00" + class.
	Offsets map[string]int
	// LastGood is the crew of the newest task whose result was kept — the
	// crew known to work on this install, a seat's last resort before the
	// person's own model when nothing routed can start.
	LastGood *CrewRecord
	// Routes are the recent first-call outcomes of every route a crew seat
	// was sent on, oldest first — what route health is read from.
	Routes []CrewRouteOutcome
	// CostFactor is, per class, what this install's recent paid tasks cost
	// against the estimate they were routed with — the geometric mean of
	// actual over estimate, shrunk toward one while there are few, and kept
	// within a factor of three. A class with no settled paid task has none.
	CostFactor map[string]float64
	// Quality is this install's learned move to a model's quality in a seat,
	// keyed [QualityKey] with the id the seat ran: every settled task moves
	// each of its seats' models a small step toward +learnBound when its
	// result was kept, toward −learnBound when it was redone stronger, and
	// half as far down when it was not kept. The move never leaves
	// ±learnBound quality points.
	Quality map[string]float64
}

// The per-install quality update: how far one task moves a model's learned
// quality toward the bound, and the bound itself, in quality points.
const (
	learnRate  = 0.1
	learnBound = 1.0
)

// QualityKey is the key [CrewLog.Quality] is kept under: the class, the seat
// and the id the seat ran.
func QualityKey(class, seat, model string) string {
	return class + "\x00" + seat + "\x00" + strings.ToLower(strings.TrimSpace(model))
}

// outcomeSignal is how a settled task reads for the models that sat it: +1
// kept, −1 redone stronger, −½ not kept, 0 for anything else.
func outcomeSignal(outcome string) float64 {
	switch outcome {
	case CrewAccepted:
		return 1
	case CrewRedone:
		return -1
	case CrewNotKept:
		return -0.5
	}
	return 0
}

// learnedQuality reads [CrewLog.Quality] off the settled tasks, oldest first.
func learnedQuality(order []string, tasks map[string]*CrewTask) map[string]float64 {
	out := map[string]float64{}
	for _, call := range order {
		task := tasks[call]
		signal := outcomeSignal(task.Outcome)
		if !task.Settled || signal == 0 {
			continue
		}
		for seat, model := range task.Record.Seats {
			if strings.TrimSpace(model) == "" {
				continue
			}
			key := QualityKey(task.Record.TaskClass, seat, model)
			out[key] += learnRate * (signal*learnBound - out[key])
		}
	}
	return out
}

// The cost factor's three numbers: how many recent tasks it reads, how many
// tasks' worth of "the estimate was right" it starts from, and how far it may
// move the estimate.
const (
	costFactorTasks = 30
	costFactorPrior = 4.0
	costFactorLimit = 3.0
)

// costFactors reads [CrewLog.CostFactor] off the settled tasks, oldest first.
func costFactors(order []string, tasks map[string]*CrewTask) map[string]float64 {
	logs := map[string][]float64{}
	for i := len(order) - 1; i >= 0; i-- {
		task := tasks[order[i]]
		class := task.Record.TaskClass
		base := task.Record.EstBase
		if base <= 0 {
			base = task.Record.EstUSD
		}
		if !task.Settled || base <= 0 || task.CostUSD <= 0 || len(logs[class]) >= costFactorTasks {
			continue
		}
		logs[class] = append(logs[class], math.Log(task.CostUSD/base))
	}
	out := map[string]float64{}
	for class, ls := range logs {
		var sum float64
		for _, l := range ls {
			sum += l
		}
		n := float64(len(ls))
		f := math.Exp(sum / (n + costFactorPrior))
		out[class] = math.Min(costFactorLimit, math.Max(1/costFactorLimit, f))
	}
	return out
}

// The learning rule's two numbers. A redo raises its repository and class by
// one step, to at most redoOffsetCeiling; redoDecayAfter accepted tasks of that
// class there take a step back off, so a class that was under-served once is
// not overpaid for ever.
const (
	redoOffsetCeiling = 3
	redoDecayAfter    = 1
	// crewRecent is how many tasks the panel lists.
	crewRecent = 8
	// crewTailBytes is how much of the log is read: the crew rows are a few
	// hundred bytes among every routed turn, and a day's tasks and the offsets
	// that matter live in the recent tail, not in months of history.
	crewTailBytes = 8 << 20
)

// ReadCrewLog reads the crew rows out of the log's tail. now decides which
// day is today, in the local time zone the person lives in.
func ReadCrewLog(dir string, now time.Time) CrewLog {
	out := CrewLog{Offsets: map[string]int{}, ProviderUSD: map[string]float64{}}
	path, err := statePath(dir, "router-events.jsonl")
	if err != nil {
		return out
	}
	file, err := os.Open(path)
	if err != nil {
		return out
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > crewTailBytes {
		if _, err := file.Seek(info.Size()-crewTailBytes, io.SeekStart); err != nil {
			return out
		}
	}
	tasks := map[string]*CrewTask{}
	var order []string
	needle := []byte(`"class":"` + CrewClass + `"`)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, needle) {
			continue
		}
		var row Event
		if json.Unmarshal(line, &row) != nil || row.Class != CrewClass || row.Crew == nil {
			continue
		}
		if len(row.Crew.Failed) > 0 || len(row.Crew.Started) > 0 {
			// A ROUTE OUTCOME is a lesson about a route, not a new state of the
			// task: the task's own rows stay as they were.
			if now.Sub(row.At) < crewOutcomesFor {
				for seat, send := range row.Crew.Failed {
					out.Routes = append(out.Routes, CrewRouteOutcome{At: row.At, Seat: seat, Send: send,
						Provider: row.Crew.FailRoute, Kind: row.Crew.FailKind, Until: row.Crew.FailUntil})
				}
				for seat, send := range row.Crew.Started {
					out.Routes = append(out.Routes, CrewRouteOutcome{At: row.At, Seat: seat, Send: send,
						Provider: row.Crew.Providers[seat], Paid: row.Crew.StartedPaid})
				}
			}
			continue
		}
		task, seen := tasks[row.Call]
		if !seen {
			task = &CrewTask{At: row.At, Call: row.Call}
			tasks[row.Call] = task
			order = append(order, row.Call)
		}
		task.Record = *row.Crew
		if row.Final {
			task.Outcome, task.CostUSD, task.Settled = row.Outcome, row.Cost, true
			task.At = row.At
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return tasks[order[i]].At.Before(tasks[order[j]].At) })
	out.CostFactor = costFactors(order, tasks)
	out.Quality = learnedQuality(order, tasks)
	year, month, day := now.Local().Date()
	accepted := map[string]int{}
	for _, call := range order {
		task := tasks[call]
		key := task.Record.Repo + "\x00" + task.Record.TaskClass
		switch task.Outcome {
		case CrewRedone:
			if out.Offsets[key] < redoOffsetCeiling {
				out.Offsets[key]++
			}
			accepted[key] = 0
		case CrewAccepted:
			record := task.Record
			out.LastGood = &record
			if record.Redo {
				// THE REDO'S OWN ACCEPTANCE TEACHES NOTHING NEW: the step it
				// added is for the NEXT task of the class here to start on.
				break
			}
			accepted[key]++
			if accepted[key] >= redoDecayAfter && out.Offsets[key] > 0 {
				out.Offsets[key]--
				accepted[key] = 0
			}
		}
		if y, m, d := task.At.Local().Date(); y == year && m == month && d == day {
			out.Tasks++
			out.SpentUSD += task.CostUSD
			for provider, share := range crewProviderShares(task.Record, task.CostUSD) {
				out.ProviderUSD[provider] += share
			}
			switch task.Record.Kinds["worker"] {
			case "plan":
				out.OnPlan++
			case "local":
				out.Local++
			}
		}
	}
	for key, offset := range out.Offsets {
		if offset == 0 {
			delete(out.Offsets, key)
		}
	}
	for i := len(order) - 1; i >= 0 && len(out.Recent) < crewRecent; i-- {
		out.Recent = append(out.Recent, *tasks[order[i]])
	}
	return out
}

// crewProviderShares is one task's cost laid on the providers that carried it.
//
// THE LOG KEEPS A TASK'S COST WHOLE and each seat's provider beside it, not a
// cost per seat, so the share is by seat: the cost is split evenly across the
// seats that rode a metered route, because a plan or a local model adds
// nothing to what a task costs. A task whose seats all rode a plan or a local
// model — or that names no provider at all — lays its cost, if it has one, on
// nobody rather than on a provider that cannot have charged it.
func crewProviderShares(record CrewRecord, costUSD float64) map[string]float64 {
	if costUSD <= 0 {
		return nil
	}
	var metered []string
	for seat, provider := range record.Providers {
		if provider == "" {
			continue
		}
		if kind := record.Kinds[seat]; kind == "plan" || kind == "local" {
			continue
		}
		metered = append(metered, provider)
	}
	if len(metered) == 0 {
		return nil
	}
	out := map[string]float64{}
	for _, provider := range metered {
		out[provider] += costUSD / float64(len(metered))
	}
	return out
}

// CrewCallID names one task's crew rows. It is the task's own identity where
// the caller has one — a redo must settle the SAME call — and fresh otherwise.
func CrewCallID(identity string) string {
	if identity = strings.TrimSpace(identity); identity != "" {
		return "crew:" + identity
	}
	return "crew:" + callID()
}
