package main

// ── codeaf app: A STANDING ORDER MADE FROM A WORKSTREAM (Workstreams v5) ─────
//
// A workstream's People and rules sheet makes a standing order with words a
// person chose: what to do, and when. It is the same automation every other
// door makes, in the same store, run by the same clock; this file only adds the
// door, and the preview the sheet shows before the order is switched on.
//
// IT IS SAVED AS PROPOSED. Nothing runs before the person says yes, which on
// the sheet is the order's switch (orders.ratify); Cancel is orders.not_now.
// Its results are said nowhere but its own row and the workstream's tasks,
// which the desk makes from each run (state/wsOrderRunsLive.ts there).

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// orderTitleMost is the longest title a brief's first line gives an order.
const orderTitleMost = 80

// orderSchedule reads when the way orders.change does, in this machine's zone,
// and answers the schedule it makes.
func orderSchedule(when string, now time.Time) (automation.Schedule, error) {
	line, err := orderWhenLine("new", when)
	if err != nil {
		return automation.Schedule{}, err
	}
	cmd, err := automation.ParseCommand(line)
	if err != nil {
		return automation.Schedule{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	// THE PLACEHOLDERS ONLY LET THE SCHEDULE BE CHECKED: Apply validates a whole
	// automation, and only its schedule is kept.
	base := automation.Automation{Title: "new", Workspace: home.Dir(), Action: automation.Action{Do: "new"},
		Schedule: automation.Schedule{Zone: session.LocalZone()}}
	a, err := cmd.Apply(base, now)
	if err != nil {
		return automation.Schedule{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return a.Schedule, nil
}

// orderNextRuns is when a schedule next runs after now, up to
// [appwire.OrderNextRunsMost]; a one-time schedule runs once.
func orderNextRuns(s automation.Schedule, now time.Time) []time.Time {
	first, err := s.First(now)
	if err != nil || first.IsZero() {
		return nil
	}
	out := []time.Time{first}
	for s.Repeats() && len(out) < appwire.OrderNextRunsMost {
		next, err := s.Next(out[len(out)-1])
		if err != nil || next.IsZero() {
			break
		}
		out = append(out, next)
	}
	return out
}

// PreviewOrder says a when in words and exactly, and when it next runs.
func (b *appBackend) PreviewOrder(when string) (appwire.OrderPreview, error) {
	now := b.now()
	s, err := orderSchedule(when, now)
	if err != nil {
		return appwire.OrderPreview{}, err
	}
	return appwire.OrderPreview{When: automation.Describe(s, now), WhenExact: automation.Exact(s), NextRuns: orderNextRuns(s, now)}, nil
}

// orderTitleOf is the title an order made without one takes: the brief's first
// line, cut at a word.
func orderTitleOf(does string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(does), "\n")
	line = strings.TrimRight(strings.TrimSpace(line), ".")
	if r := []rune(line); len(r) > orderTitleMost {
		cut := string(r[:orderTitleMost])
		if at := strings.LastIndex(cut, " "); at > orderTitleMost/2 {
			cut = cut[:at]
		}
		line = cut + "…"
	}
	return line
}

// CreateOrder makes a standing order for a workstream, proposed.
func (b *appBackend) CreateOrder(in appwire.OrderCreate) (appwire.Order, error) {
	store, err := b.ordersOf()
	if err != nil {
		return appwire.Order{}, err
	}
	now := b.now()
	s, err := orderSchedule(in.When, now)
	if err != nil {
		return appwire.Order{}, err
	}
	title := in.Title
	if title == "" {
		title = orderTitleOf(in.Does)
	}
	a := automation.Automation{
		Title: title, Schedule: s, Action: automation.Action{Do: in.Does},
		// A WORKSTREAM'S ORDER BELONGS TO NO PROJECT: it runs from this person's
		// home, and what it makes is said on the workstream's tasks, not in a chat.
		Workspace: home.Dir(), Workstream: in.Workstream, AnswersIn: automation.ScopeNotHere,
		Status: automation.StatusProposed, Trust: automation.TrustAsksFirst,
	}
	if in.BudgetPerRunUSD != nil {
		a.Limits.USD = *in.BudgetPerRunUSD
	}
	saved, err := store.Create(a)
	if err != nil {
		return appwire.Order{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return b.sayOrder(store, saved.ID)
}
