package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func creationStamps(conv *fakeConversation) []string {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	var out []string
	for _, s := range conv.submissions {
		out = append(out, s.CreationTeam)
	}
	return out
}

// A CREW MESSAGE STAMPS ITS CREW'S EXACT ID ON THE TURN IT OPENS, AND AN ORDINARY
// SEND STAMPS NOTHING (the engine records that as explicit none).
func TestAppCrewMessageStampsTheCrewAndOrdinarySendDoesNot(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	backend, _, manager, crew := crewBackendOn(t, conv)
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "make a weekly report"}); err != nil {
		t.Fatal(err)
	}
	if got := creationStamps(conv); len(got) != 1 || got[0] != crew {
		t.Fatalf("crew message stamps = %q, want [%s]", got, crew)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := backend.Send(manager.ID, "an ordinary word", ""); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the manager never became free for an ordinary send")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := creationStamps(conv); len(got) != 1 {
		t.Fatalf("an ordinary send went through SubmitMessage with stamps %q", got)
	}
}

// A CREW MESSAGE THAT WAITS IN A BUSY MEMBER'S QUEUE KEEPS ITS STAMP FOR THE FLUSH.
func TestAppCrewMessageQueuedKeepsItsStamp(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "on it"}}, hold: hold}
	backend, _, _, crew := crewBackendOn(t, conv)
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	waiting, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "second"})
	if err != nil || waiting.Turn != 0 {
		t.Fatalf("the second message = %+v, %v; want it queued", waiting, err)
	}
	close(hold)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := creationStamps(conv); len(got) == 2 {
			if got[0] != crew || got[1] != crew {
				t.Fatalf("stamps = %q, want both %s", got, crew)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the queued message never flushed; stamps %q", creationStamps(conv))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ONLY A STAMPED, OTHERWISE PLAIN SENDING IS EXEMPT FROM THE OLDER-HOST REFUSAL,
// queued or not; an ordinary one with the same words is not.
func TestAppStampOnlySendingIsTheOnlyExemptionFromTheMarkRefusal(t *testing.T) {
	ordinary := &appSending{text: "x"}
	if ordinary.marksNothing("x") {
		t.Fatal("an ordinary sending was exempted from the older-host refusal")
	}
	stamped := &appSending{text: "x", creationTeam: "aaaaaaaaaaaa", queued: true}
	if !stamped.marksNothing("x") {
		t.Fatal("a stamped queue flush would be refused by an older host")
	}
	stamped.reads = []session.MessageRead{{}}
	if stamped.marksNothing("x") {
		t.Fatal("a stamped sending carrying reads was exempted")
	}
}

// EDITING A QUEUED CREW MESSAGE KEEPS THE CREW IT WAS ACCEPTED FOR.
func TestAppEditingAQueuedCrewMessageKeepsItsStamp(t *testing.T) {
	old := &appSending{text: "before", creationTeam: "aaaaaaaaaaaa"}
	if got := appStampKept(old, nil, "after"); got == nil || got.creationTeam != "aaaaaaaaaaaa" || got.text != "after" {
		t.Fatalf("plain edit kept %+v", got)
	}
	if got := appStampKept(old, &appSending{text: "mine"}, "after"); got == nil || got.creationTeam != "aaaaaaaaaaaa" || got.text != "mine" {
		t.Fatalf("marked edit kept %+v", got)
	}
	if got := appStampKept(&appSending{text: "o"}, nil, "after"); got != nil {
		t.Fatalf("an unstamped edit gained %+v", got)
	}
}

// olderHostCrew is a crew whose manager is held on an engine that keeps no
// mark on any door.
func olderHostCrew(t *testing.T, inner *fakeConversation) (*appBackend, appwire.Agent, string) {
	t.Helper()
	backend, _, agent := markBackend(t, olderHostConversation{&steeringConversation{fakeConversation: inner}})
	backend.profileDir = t.TempDir()
	id := teamstore.NewID()
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: "harbor", Made: time.Now()})
		if err := f.AddMember(id, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Where: "/w", Word: "lead", Handle: "lead"}); err != nil {
			return err
		}
		return f.SetManager(id, appFakeTranscript)
	}); err != nil {
		t.Fatal(err)
	}
	return backend, agent, id
}

// A CREW MESSAGE TO AN OLDER HOST IS NOT REFUSED FOR ITS STAMP: it is handed
// over with it, waits in the queue with it, keeps it through an edit, and goes
// when the turn ends — while a message carrying marks is still refused whole.
func TestAppCrewMessageOnAnOlderHostKeepsItsStampThroughQueueAndEdit(t *testing.T) {
	hold := make(chan struct{})
	inner := &fakeConversation{hold: hold}
	backend, agent, crew := olderHostCrew(t, inner)

	// While the agent is idle a message with a mark is refused whole by the
	// older host's gate (not by busy), and nothing is sent.
	said := "mine"
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "marked", Said: &said}); wireCode(err) != appwire.CodeUnknownMethod {
		t.Fatalf("a marked crew message to an older host answered %v, want unknown_method", err)
	}
	if got := creationStamps(inner); len(got) != 0 {
		t.Fatalf("a refused message reached the engine: %q", got)
	}
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "first"}); err != nil {
		t.Fatalf("a plain crew message was refused by an older host: %v", err)
	}
	if got := creationStamps(inner); len(got) != 1 || got[0] != crew {
		t.Fatalf("the plain message was handed stamps %q", got)
	}
	waiting, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "second"})
	if err != nil || waiting.Turn != 0 {
		t.Fatalf("the busy message = %+v, %v; want it queued", waiting, err)
	}
	queued := backend.queued(backend.agent(agent.ID))
	if len(queued) != 1 || queued[0].Text != "second" {
		t.Fatalf("queue = %+v", queued)
	}
	if _, err := backend.EditQueued(agent.ID, queued[0].ID, "second, corrected"); err != nil {
		t.Fatal(err)
	}
	close(hold)
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := creationStamps(inner)
		if len(got) == 2 {
			if got[1] != crew {
				t.Fatalf("the edited, flushed message was stamped %q, want %s", got[1], crew)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the queued message never flushed; stamps %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, texts := appSubmissionsOf(inner); len(texts) != 2 || texts[1] != "second, corrected" {
		t.Fatalf("the engine heard %q", texts)
	}
}

// A RETRIED QUEUED CREW MESSAGE KEEPS ITS STAMP; AN UNSTAMPED ONE IS PLAIN WORDS.
func TestAppRetryOfAQueuedCrewMessageKeepsItsStamp(t *testing.T) {
	stamped := appSending{text: "x", queued: true, creationTeam: "aaaaaaaaaaaa"}
	got := appRetryShown(stamped, "x")
	if got == nil || got.queued || got.creationTeam != "aaaaaaaaaaaa" {
		t.Fatalf("retry of a stamped message = %+v", got)
	}
	if plain := appRetryShown(appSending{text: "x", queued: true}, "x"); plain != nil {
		t.Fatalf("retry of an unstamped queued message = %+v, want plain words", plain)
	}
	if marked := appRetryShown(appSending{text: "x", queued: true, reads: []session.MessageRead{{}}}, "x"); marked == nil || marked.queued {
		t.Fatalf("retry of a marked message = %+v", marked)
	}
}

// A CREW MESSAGE CARRYING ATTACHMENTS STAMPS ITS CREW TOO.
func TestAppAttachedCrewMessageStampsTheCrew(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	backend, _, manager, crew := crewBackendOn(t, conv)
	at, err := backend.AddAttachment(manager.ID, appwire.Attachment{Kind: appwire.AttachPaste}, strings.Repeat("panic: nil map\n", 20))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "look at this", Attachments: []string{at.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := creationStamps(conv); len(got) != 1 || got[0] != crew {
		t.Fatalf("attached crew message stamps = %q, want [%s]", got, crew)
	}
}
