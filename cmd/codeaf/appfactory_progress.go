package main

import (
	"slices"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A JOB'S PROGRESS LINES, AS THEY ARE WRITTEN ─────────────────────────────
//
// The factory runner reports each step into the job's own conversation
// (internal/factory/run's manager.go): `review started`, `review done · 4m · …`,
// `asking you: …`. Those lines land between turns, so no turn's stream carries
// them, and until this file the app saw one only at its next reading of the
// conversation. Each line written in this process is now told here
// ([session.OnProgress]) and said to the app as `entry.added`, carrying the
// step it reports on (`progress`), when the app holds that conversation.
//
// THE ENTRY IS READ BACK, NEVER MADE UP. An entry's id counts the
// conversation's messages from its first, so the one said live is the one
// `agents.entries` answers for the same line afterwards: it is the same
// reading of the same journal (the protocol's stable-id law).
//
// THE NEWS ARRIVES FROM A DESK (session's progressDesk), after the line is on
// disk and in the order the lines were written, but the runner does not wait
// for it: by the time it is read, a later line may be in the journal too. So
// the line is found by its words and its step among the newest entries, and
// one already said is passed over: two identical lines written close together
// are each said once, under their own ids.

// appProgressLook is how many of a conversation's newest entries are looked
// through for a line written, and how many of the ids said live are
// remembered. The runner writes a few lines a step, so the line is nearly
// always among the newest few; the margin is for lines and notices after it.
const appProgressLook = 8

// appProgressReader is what the app put in front of the runner's progress
// news, kept so closing puts the previous reader back.
type appProgressReader struct {
	previous atomic.Pointer[func(session.ProgressNews)]
}

// listenProgress starts hearing the factory runner's progress lines, and
// answers the function that stops.
func (b *appBackend) listenProgress() func() {
	reader := &appProgressReader{}
	previous := session.OnProgress(func(news session.ProgressNews) {
		if next := reader.previous.Load(); next != nil && *next != nil {
			(*next)(news)
		}
		b.progressSaid(news)
	})
	reader.previous.Store(&previous)
	return func() { session.OnProgress(previous) }
}

// progressSaid says one progress line to the app as the entry it is, when the
// app holds the conversation it was written into. A conversation the app does
// not hold is read whole when the app opens it, line and all.
func (b *appBackend) progressSaid(news session.ProgressNews) {
	id := appAgentID(news.Path)
	var live *appLive
	b.locked(func() {
		if a := b.agents[id]; a != nil {
			live = a.live
		}
	})
	if live == nil {
		return
	}
	page, _, err := b.Entries(id, "", appProgressLook)
	if err != nil {
		return
	}
	want := appEntryProgress(news.Step)
	var found *appwire.Entry
	b.locked(func() {
		for i := len(page) - 1; i >= 0; i-- {
			e := page[i]
			if e.Kind != appwire.EntryAnswer || e.Text != news.Text || !appSameProgress(e.Progress, want) || slices.Contains(live.progressSaid, e.ID) {
				continue
			}
			live.progressSaid = append(live.progressSaid, e.ID)
			if over := len(live.progressSaid) - appProgressLook; over > 0 {
				live.progressSaid = append([]string(nil), live.progressSaid[over:]...)
			}
			found = &page[i]
			return
		}
	})
	if found != nil {
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: *found})
	}
}

// appMarkProgress gives each factory runner's step line the step it reports
// on, from the record's own marks ([session.Record.Progress]): entries are the
// shaping of items, from each entry's item, and places each item's place in the
// record. Only an answer is a runner's line.
func appMarkProgress(entries []appwire.Entry, from, places []int, progress map[int]factory.StepProgress) {
	if len(progress) == 0 {
		return
	}
	for k := range entries {
		if entries[k].Kind != appwire.EntryAnswer || k >= len(from) || from[k] < 0 || from[k] >= len(places) {
			continue
		}
		if step, ok := progress[places[from[k]]]; ok {
			entries[k].Progress = appEntryProgress(&step)
		}
	}
}

// appEntryProgress is a runner line's step in the app's words, nil for none.
func appEntryProgress(step *factory.StepProgress) *appwire.EntryProgress {
	if step == nil {
		return nil
	}
	return &appwire.EntryProgress{Step: step.Step, Round: step.Round, Event: string(step.Event)}
}

// appSameProgress says two entries' steps are the same fact, both absent
// included.
func appSameProgress(a, b *appwire.EntryProgress) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
