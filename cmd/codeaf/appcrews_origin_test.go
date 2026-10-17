package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
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
