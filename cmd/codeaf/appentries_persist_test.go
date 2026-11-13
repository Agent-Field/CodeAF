package main

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appRigSend drives one scripted turn against a real-transcript rig and waits
// for it to settle, exactly as a relaunching client would observe the durable
// sidecar afterwards.
func appRigSend(t *testing.T, r *closeRig, words string, script []session.Event) {
	t.Helper()
	r.conv.mu.Lock()
	r.conv.script = script
	r.conv.mu.Unlock()
	if _, err := r.b.Send(r.id, words, ""); err != nil {
		t.Fatalf("send = %v", err)
	}
	r.rec.waitFor(t, 10*time.Second, appTurnDone)
}

func appRigFooters(t *testing.T, r *closeRig) []appKeptFooter {
	t.Helper()
	kept, err := appReadFooters(r.file)
	if err != nil {
		t.Fatal(err)
	}
	return kept
}

// A QUIET NOTICE SURVIVES RELAUNCH. The engine's dim notices are persisted to
// the sidecar the listing replays, not merely emitted live.
func TestAppPersistsQuietNoticesAcrossRelaunch(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	appRigSend(t, r, "list files", []session.Event{
		{Kind: session.EventNotice, Text: "skills carried forward", NoticeKind: session.NoticeKindSkills},
		{Kind: session.EventTurnDone},
	})
	kept := appRigFooters(t, r)
	for _, f := range kept {
		if f.Entry.Kind == appwire.EntryNotice && f.Entry.Quiet && f.Entry.Text == "skills carried forward" && f.Entry.NoticeKind == session.NoticeKindSkills {
			// The replayed listing, not just the raw sidecar, must show it.
			for _, e := range r.entries(t) {
				if e.Kind == appwire.EntryNotice && e.Quiet && e.Text == "skills carried forward" && e.NoticeKind == session.NoticeKindSkills {
					return
				}
			}
			t.Fatalf("quiet notice kept but not replayed: entries=%+v", r.entries(t))
		}
	}
	t.Fatalf("quiet notice not persisted: %+v", kept)
}

// AN UNRECORDED NOTICE IS NOT KEPT. Memory's lines and a factory's progress are
// said for the moment ([session.Event.Unrecorded]); the sidecar is a record a
// returning surface redoes, so the app must not write one into it.
func TestAppDoesNotPersistUnrecordedNoticesAcrossRelaunch(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	appRigSend(t, r, "list files", []session.Event{
		{Kind: session.EventNotice, Text: "remembered · keep the ansible host", NoticeKind: session.NoticeKindMemory, Unrecorded: true},
		{Kind: session.EventTurnDone},
	})
	kept := appRigFooters(t, r)
	for _, f := range kept {
		if f.Entry.Kind == appwire.EntryNotice && f.Entry.Text == "remembered · keep the ansible host" {
			t.Fatalf("unrecorded notice was persisted: %+v", kept)
		}
	}
}

// A TURN FAILURE LINE SURVIVES RELAUNCH. A provider failure's entry is
// persisted to the sidecar, not merely emitted live.
func TestAppPersistsTurnFailureAcrossRelaunch(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	appRigSend(t, r, "run", []session.Event{
		{Kind: session.EventError, ErrKind: session.TurnErrorProvider, Err: errors.New("401 Unauthorized")},
	})
	kept := appRigFooters(t, r)
	for _, f := range kept {
		if f.Entry.Kind == appwire.EntryFooter && f.Entry.Footer == appwire.FooterProviderFailed && f.Entry.Text == "401 Unauthorized" {
			return
		}
	}
	t.Fatalf("failure entry not persisted: %+v", kept)
}

// AN ANSWERED RECEIPT SURVIVES RELAUNCH. Answering an ask from the app keeps
// its receipt even when no permission scope was saved.
func TestAppPersistsAnsweredReceiptAcrossRelaunch(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	r.conv.mu.Lock()
	r.conv.onAnswer = func(answer session.Answer) {
		q := session.Question{Kind: answer.Kind, ID: answer.ID}
		r.conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
	}
	r.conv.script = []session.Event{{Kind: session.EventTurnDone}}
	r.conv.mu.Unlock()
	if _, err := r.b.Send(r.id, "open", ""); err != nil {
		t.Fatal(err)
	}
	r.rec.waitFor(t, 10*time.Second, appTurnDone)

	question := session.Question{ID: 7, Kind: session.QuestionAsk, Head: "Which database?", Options: []session.AnswerOption{{Key: "1", Label: "sqlite"}}}
	r.conv.raise(session.Event{Kind: session.EventQuestion, Question: &question})
	r.rec.waitFor(t, 10*time.Second, appAskEvent(appwire.EventAskOpened))
	if err := r.b.AnswerAsk(r.id, "ask:7", "1", ""); err != nil {
		t.Fatal(err)
	}
	r.rec.waitFor(t, 10*time.Second, func(event string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.Kind == appwire.EntryReceipt && e.Entry.ID == "rask:7"
	})

	kept := appRigFooters(t, r)
	for _, f := range kept {
		if f.Entry.Kind == appwire.EntryReceipt && f.Entry.ID == "rask:7" {
			return
		}
	}
	t.Fatalf("answered receipt not persisted: %+v", kept)
}
