package factory

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// ── A STANDING ORDER IN AN ORGANIZATION'S WORKSTREAM ───────────────────────
//
// An organization's standing order is KEPT AND RUN BY THE ORGANIZATION'S
// SERVER (fix wave X1, final run E4 F0 and F6). Until 2026-11-17 it lived on
// the computer of the person who made it and ran in their own clock as a
// general agent with their shell over their whole home; nobody else could see
// it, and with that computer off nothing ran. Now:
//
//   - It is kept with the workstream, and everyone who can see the workstream
//     sees it in People and rules › Keeps running. Only the person who looks
//     after the workstream adds, switches or removes one.
//   - Each run is ONE TASK in the workstream: Came from the order, done by
//     Afi, owned by the owner of incoming work, in the order's group. It asks
//     its owner "Run … now?" first unless "Start without asking" covers what
//     one run may spend; Go runs the workstream's agent on the server, in the
//     task's own seat, with the workstream's tools and nothing else; Stop on
//     the task stops it.
//   - It runs only where the server runs Afi (a model key from the operator).
//     A server that cannot does not offer orders at all.
//
// The schedule is internal/automation's, so "every Monday at 09:00" is read
// and said exactly as a Personal order's is.

// The bounds an organization's standing order is kept within.
const (
	// WorkstreamOrdersMost is how many orders one workstream keeps.
	WorkstreamOrdersMost = RulesOrdersMost
	// workstreamOrderDoesMost bounds what one does, as a task's request is.
	workstreamOrderDoesMost  = 4000
	workstreamOrderTitleMost = 80
)

// OrderAskPrefix is the id of a run's own "Run … now?" Ask: "order:<run>".
const OrderAskPrefix = "order:"

// OrderGroupPrefix is the id of an order's group in Tasks: "order:<id>".
const OrderGroupPrefix = "order:"

// WorkstreamOrder is one standing order an organization's workstream keeps.
type WorkstreamOrder struct {
	ID string `json:"id"`
	WS string `json:"ws"`
	// Title is its short name, Does the brief each run carries out.
	Title string `json:"title"`
	Does  string `json:"does"`
	// Every and Zone are its schedule (automation.Schedule): a cron line
	// ("0 9 * * 1") or an interval ("1h"), read in Zone.
	Every string `json:"every"`
	Zone  string `json:"zone,omitempty"`
	// On says it runs. Off keeps it, with nothing run.
	On bool `json:"on"`
	// BudgetPerRunUSD, when set, is the most one run may spend; with "Start
	// without asking" at or above it, a run starts without asking.
	BudgetPerRunUSD float64 `json:"budget_per_run_usd,omitempty"`
	// Next is when it next runs (RFC 3339), empty while it is off.
	Next string `json:"next,omitempty"`
	// Runs counts its runs; the newest run's number names its task.
	Runs      int64     `json:"runs"`
	CreatedBy PersonRef `json:"created_by"`
	// WordsBy is who wrote its title and brief, as a kind of writer
	// (TaskByPerson, TaskByAfi, …), written by the server only; each run's
	// task carries it (TaskCreate.WordsBy reads the same way). Empty on an
	// order kept before it, which reads as not a person.
	WordsBy   string `json:"words_by,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Rev       int    `json:"rev"`
}

// Schedule is the order's schedule.
func (o WorkstreamOrder) Schedule() automation.Schedule {
	return automation.Schedule{Every: o.Every, Zone: o.Zone}
}

// NextAfter is when o next runs after now, or zero when it never will.
func (o WorkstreamOrder) NextAfter(now time.Time) time.Time {
	next, err := o.Schedule().Next(now)
	if err != nil {
		return time.Time{}
	}
	return next
}

// NextAt is o.Next read, zero when it has none.
func (o WorkstreamOrder) NextAt() time.Time {
	at, err := time.Parse(time.RFC3339Nano, o.Next)
	if err != nil {
		return time.Time{}
	}
	return at
}

// When says o's schedule in a person's words: "every Monday at 09:00".
func (o WorkstreamOrder) When(now time.Time) string {
	return automation.Describe(o.Schedule(), now)
}

// StartsAlone says a run of o starts without asking under rules: only when
// it names what one run may spend and "Start without asking" covers that.
// The desk's wsOrders.ts startsAlone reads the same rule.
func (o WorkstreamOrder) StartsAlone(rules WorkstreamRules) bool {
	return o.BudgetPerRunUSD > 0 && rules.StartWithoutAskingUSD > 0 && o.BudgetPerRunUSD <= rules.StartWithoutAskingUSD
}

// WorkstreamOrderNew is what a new order is made from.
type WorkstreamOrderNew struct {
	Title string `json:"title,omitempty"`
	// When is "every 0 9 * * 1" or "every 1h": a repeating schedule, the
	// spelling orders.preview reads.
	When            string  `json:"when"`
	Zone            string  `json:"zone,omitempty"`
	Does            string  `json:"does"`
	On              *bool   `json:"on,omitempty"`
	BudgetPerRunUSD float64 `json:"budget_per_run_usd,omitempty"`
	// WordsBy only ever lowers trust in its words (TaskCreate.WordsBy).
	WordsBy string `json:"words_by,omitempty"`
}

// WorkstreamOrderPatch is what may change on an order.
type WorkstreamOrderPatch struct {
	On *bool `json:"on,omitempty"`
}

// NewWorkstreamOrder checks in and makes the order it describes, switched on
// unless it says otherwise.
func NewWorkstreamOrder(ws, id string, in WorkstreamOrderNew, by PersonRef, now time.Time) (WorkstreamOrder, error) {
	does := strings.TrimSpace(in.Does)
	switch {
	case does == "":
		return WorkstreamOrder{}, TaskInvalid("a standing order needs something to do")
	case utf8.RuneCountInString(does) > workstreamOrderDoesMost:
		return WorkstreamOrder{}, taskInvalidf("what a standing order does is at most %d characters", workstreamOrderDoesMost)
	case in.BudgetPerRunUSD < 0:
		return WorkstreamOrder{}, TaskInvalid("a budget per run is above zero")
	}
	every, ok := strings.CutPrefix(strings.TrimSpace(in.When), "every ")
	every = strings.TrimSpace(every)
	if !ok || every == "" {
		return WorkstreamOrder{}, TaskInvalid(`a standing order repeats: its when is "every …"`)
	}
	zone := strings.TrimSpace(in.Zone)
	if zone == "" {
		zone = "UTC"
	}
	s := automation.Schedule{Every: every, Zone: zone}
	if err := s.Validate(); err != nil {
		return WorkstreamOrder{}, TaskInvalid(err.Error())
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = orderTitleOf(does)
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	o := WorkstreamOrder{ID: id, WS: ws, Title: title, Does: does, Every: every, Zone: zone, On: in.On == nil || *in.On,
		BudgetPerRunUSD: in.BudgetPerRunUSD, CreatedBy: by, CreatedAt: stamp, UpdatedAt: stamp, Rev: 1}
	if o.On {
		o.Next = stampOrZero(o.NextAfter(now))
	}
	return o, nil
}

// Switch turns o on or off. On, its next run is the schedule's next after
// now, so a run missed while it was off is not run late.
func (o WorkstreamOrder) Switch(on bool, now time.Time) WorkstreamOrder {
	o.On, o.Next = on, ""
	if on {
		o.Next = stampOrZero(o.NextAfter(now))
	}
	o.Rev, o.UpdatedAt = o.Rev+1, now.UTC().Format(time.RFC3339Nano)
	return o
}

// Took counts one run of o that came due, and moves it on to its next slot
// after now. HOWEVER MANY SLOTS WERE MISSED, ONE RUN IS MADE: a server back
// after a weekend makes the weekly report once, not twice.
func (o WorkstreamOrder) Took(now time.Time) WorkstreamOrder {
	o.Runs++
	o.Next = stampOrZero(o.NextAfter(now))
	o.Rev, o.UpdatedAt = o.Rev+1, now.UTC().Format(time.RFC3339Nano)
	return o
}

func stampOrZero(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// orderTitleOf is the title an order made without one takes: the brief's
// first line, cut at a word.
func orderTitleOf(does string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(does), "\n")
	line = strings.TrimRight(strings.TrimSpace(line), ".")
	if r := []rune(line); len(r) > workstreamOrderTitleMost {
		cut := string(r[:workstreamOrderTitleMost])
		if at := strings.LastIndex(cut, " "); at > workstreamOrderTitleMost/2 {
			cut = cut[:at]
		}
		line = cut + "…"
	}
	return line
}
