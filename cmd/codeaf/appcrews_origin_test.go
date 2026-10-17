package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// creationWords is what each submission the fake was handed stated: "unknown"
// for no context, "none" for an explicit none, else the team id.
func creationWords(conv *fakeConversation) []string {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	var out []string
	for _, s := range conv.submissions {
		out = append(out, creationWord(s.Creation))
	}
	return out
}

func creationWord(c *session.CreationContext) string {
	switch {
	case c == nil:
		return "unknown"
	case c.Team == "":
		return "none"
	}
	return c.Team
}

// waitCreations waits until the fake has been handed n submissions.
func waitCreations(t *testing.T, conv *fakeConversation, n int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := creationWords(conv); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine was handed %q, want %d submissions", creationWords(conv), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// sendWhenFree retries an ordinary send until the agent's last turn ended.
func sendWhenFree(t *testing.T, send func() error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := send()
		if err == nil {
			return
		}
		if wireCode(err) != appwire.CodeBusy || time.Now().After(deadline) {
			t.Fatalf("the send answered %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A CREW MESSAGE STATES ITS CREW'S EXACT ID, AND EVERY ONE OF THE PERSON'S OWN
// SENDS STATES AN EXPLICIT NONE — never nothing, which would be unknown.
func TestAppCrewMessageStatesTheCrewAndPersonSendsStateNone(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	backend, _, manager, crew := crewBackendOn(t, conv)
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "make a weekly report"}); err != nil {
		t.Fatal(err)
	}
	sendWhenFree(t, func() error { _, err := backend.Send(manager.ID, "an ordinary word", ""); return err })
	said := "my words"
	sendWhenFree(t, func() error {
		_, err := backend.SendMessage(manager.ID, appwire.AgentMessage{Text: "expanded words", Said: &said})
		return err
	})
	sendWhenFree(t, func() error { _, err := backend.Queue(manager.ID, "queued while idle", ""); return err })
	got := waitCreations(t, conv, 4)
	if strings.Join(got, ",") != crew+",none,none,none" {
		t.Fatalf("the engine was handed %q, want the crew then three explicit nones", got)
	}
	// The person's own words are still theirs: an ordinary send with nothing
	// but its context is not "authored", and a Said one is.
	conv.mu.Lock()
	plain, authored := conv.submissions[1], conv.submissions[2]
	conv.mu.Unlock()
	if plain.Said != nil || plain.Text != "an ordinary word" || authored.Said == nil || *authored.Said != said {
		t.Fatalf("plain %+v, authored %+v", plain, authored)
	}
}

// WORDS THE APP WRITES ITSELF STATE NOTHING: a role's instructions, told to a
// member by the real door that tells them, reach the engine as plain words and
// their turn's team is unknown.
func TestAppRoleInstructionsStateNoContext(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	backend, _, manager, crew := crewBackendOn(t, conv)
	if err := backend.tellRole(manager.ID, crew, appwire.Role{Name: "web", Instructions: "Ship it."}); err != nil {
		t.Fatal(err)
	}
	conv.mu.Lock()
	submissions, texts := len(conv.submissions), append([]string(nil), conv.texts...)
	conv.mu.Unlock()
	if submissions != 0 || len(texts) != 1 || !strings.Contains(texts[0], "Ship it.") {
		t.Fatalf("role instructions reached the engine as %d submissions and %q", submissions, texts)
	}
}

// A MESSAGE WHOSE SAID EQUALS ITS TEXT IS STILL AUTHORED: the context rides
// beside the person's words and never takes their Said away.
func TestAppSaidEqualToTextStaysAuthoredWithAContext(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	backend, _, agent := workBackend(t, conv)
	same := "exactly these words"
	if _, err := backend.SendMessage(agent.ID, appwire.AgentMessage{Text: same, Said: &same}); err != nil {
		t.Fatal(err)
	}
	conv.mu.Lock()
	got := append([]session.Submission(nil), conv.submissions...)
	conv.mu.Unlock()
	if len(got) != 1 || got[0].Said == nil || *got[0].Said != same || creationWord(got[0].Creation) != "none" {
		t.Fatalf("the engine was handed %+v", got)
	}
}

// A CREW MESSAGE THAT WAITS IN A BUSY MEMBER'S QUEUE KEEPS ITS CREW FOR THE
// FLUSH, AND THROUGH AN EDIT OF ITS WORDS WHILE IT WAITS.
func TestAppCrewMessageQueuedAndEditedKeepsItsCrew(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "on it"}}, hold: hold}
	backend, _, manager, crew := crewBackendOn(t, conv)
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	waiting, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "second"})
	if err != nil || waiting.Turn != 0 {
		t.Fatalf("the second message = %+v, %v; want it queued", waiting, err)
	}
	queued := backend.queued(backend.agent(manager.ID))
	if len(queued) != 1 {
		t.Fatalf("queue = %+v", queued)
	}
	if _, err := backend.EditQueued(manager.ID, queued[0].ID, "second, corrected"); err != nil {
		t.Fatal(err)
	}
	conv.mu.Lock()
	conv.hold = nil
	conv.mu.Unlock()
	close(hold)
	if got := waitCreations(t, conv, 2); got[0] != crew || got[1] != crew {
		t.Fatalf("the engine was handed %q, want both %s", got, crew)
	}
	conv.mu.Lock()
	flushed := conv.submissions[1]
	conv.mu.Unlock()
	if flushed.Text != "second, corrected" || !flushed.Queued {
		t.Fatalf("the flushed message = %+v", flushed)
	}
}

// A CREW MESSAGE WHOSE TURN FAILED IS RETRIED IN THE SAME CREW, through the
// real retry door.
func TestAppRetriedCrewMessageKeepsItsCrew(t *testing.T) {
	refused := &provider.APIError{Status: 429, Message: "upstream is rate limited"}
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventError, Err: refused}}}
	backend, rec, manager, crew := crewBackendOn(t, conv)
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "make a weekly report"}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == manager.ID && c.Agent.State == appwire.StateIdle
	})
	conv.mu.Lock()
	conv.script = []session.Event{{Kind: session.EventTurnDone}}
	conv.mu.Unlock()
	if _, err := backend.Retry(manager.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitCreations(t, conv, 2); got[0] != crew || got[1] != crew {
		t.Fatalf("the engine was handed %q, want the crew twice", got)
	}
}

// A NEW MEMBER'S FIRST BRIEF STATES THE CREW IT WAS STARTED IN.
func TestAppNewMemberBriefStatesTheCrew(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	files := t.TempDir()
	rec := newAppRecorder()
	// Each conversation is the transcript the backend minted for it, so the
	// member the crew records is the agent the brief is sent to.
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.profileDir = t.TempDir()
	where := t.TempDir()
	crew := teamstore.NewID()
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "harbor", Made: time.Now()})
		lead := filepath.Join(files, "lead.jsonl")
		if err := f.AddMember(crew, teamstore.Member{Key: lead, File: lead, Where: where, Word: "lead", Handle: "lead"}); err != nil {
			return err
		}
		return f.SetManager(crew, lead)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Member("start", appwire.MemberAsk{Crew: crew, Handle: "web", Brief: "build the landing page"}); err != nil {
		t.Fatal(err)
	}
	got := waitCreations(t, conv, 1)
	if got[len(got)-1] != crew {
		t.Fatalf("the brief stated %q, want %s", got, crew)
	}
}

// ONLY A SENDING WHOSE ONE EXTRA IS ITS CONTEXT IS SENT AS WORDS TO A HOST THAT
// KEEPS NO MARKS; one carrying anything else is still refused there.
func TestAppContextOnlySendingIsTheOnlyOneSentAsWords(t *testing.T) {
	ordinary := &appSending{text: "x"}
	if ordinary.marksNothing("x") {
		t.Fatal("a sending that states nothing counted as context-only")
	}
	if authored := (&appSending{text: "x", creation: appNoTeam()}); authored.marksNothing("x") {
		t.Fatal("a sending built for the person's Said counted as context-only")
	}
	for _, creation := range []*session.CreationContext{appInTeam("aaaaaaaaaaaa"), appNoTeam()} {
		stated := &appSending{text: "x", creation: creation, queued: true, bare: true}
		if !stated.marksNothing("x") {
			t.Fatalf("a %s sending would be refused by an older host", creationWord(creation))
		}
		stated.reads = []session.MessageRead{{}}
		if stated.marksNothing("x") {
			t.Fatal("a sending carrying reads counted as context-only")
		}
	}
}

// AN EDIT KEEPS THE CONTEXT OF THE MESSAGE IT REPLACES, crew or none, and adds
// none to one that stated nothing.
func TestAppEditingAQueuedMessageKeepsItsContext(t *testing.T) {
	for _, creation := range []*session.CreationContext{appInTeam("aaaaaaaaaaaa"), appNoTeam()} {
		old := &appSending{text: "before", creation: creation}
		if got := appStampKept(old, nil, "after"); got == nil || got.creation != creation || got.text != "after" {
			t.Fatalf("plain edit kept %+v", got)
		}
		if got := appStampKept(old, &appSending{text: "mine"}, "after"); got == nil || got.creation != creation || got.text != "mine" {
			t.Fatalf("marked edit kept %+v", got)
		}
	}
	if got := appStampKept(&appSending{text: "o"}, nil, "after"); got != nil {
		t.Fatalf("an edit of a message that stated nothing gained %+v", got)
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

// A CREW MESSAGE TO A HOST THAT KEEPS NO MARKS IS SENT AS ITS WORDS, exactly as
// before: the host would drop the context, so the team is unknown there. It is
// queued and edited the same way and flushed as words, while a message that
// carries marks is still refused whole.
func TestAppCrewMessageOnAnOlderHostIsSentAsWords(t *testing.T) {
	hold := make(chan struct{})
	inner := &fakeConversation{hold: hold}
	backend, agent, crew := olderHostCrew(t, inner)

	// While the agent is idle a message with a mark is refused whole by the
	// older host's gate (not by busy), and nothing is sent.
	said := "mine"
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "marked", Said: &said}); wireCode(err) != appwire.CodeUnknownMethod {
		t.Fatalf("a marked crew message to an older host answered %v, want unknown_method", err)
	}
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "first"}); err != nil {
		t.Fatalf("a plain crew message was refused by an older host: %v", err)
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
	inner.mu.Lock()
	inner.hold = nil
	inner.mu.Unlock()
	close(hold)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, texts := appSubmissionsOf(inner)
		if len(texts) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the queued message never flushed; the engine heard %q", texts)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if submissions, texts := appSubmissionsOf(inner); len(submissions) != 0 || texts[0] != "first" || texts[1] != "second, corrected" {
		t.Fatalf("the older host was handed %+v and heard %q", submissions, texts)
	}
}

// A RETRIED QUEUED MESSAGE KEEPS ITS CONTEXT; ONE THAT STATED NOTHING IS WORDS.
func TestAppRetryOfAQueuedMessageKeepsItsContext(t *testing.T) {
	crew := appInTeam("aaaaaaaaaaaa")
	got := appRetryShown(appSending{text: "x", queued: true, creation: crew}, "x")
	if got == nil || got.queued || got.creation != crew {
		t.Fatalf("retry of a crew message = %+v", got)
	}
	if plain := appRetryShown(appSending{text: "x", queued: true}, "x"); plain != nil {
		t.Fatalf("retry of a queued message that stated nothing = %+v, want plain words", plain)
	}
	if marked := appRetryShown(appSending{text: "x", queued: true, reads: []session.MessageRead{{}}}, "x"); marked == nil || marked.queued {
		t.Fatalf("retry of a marked message = %+v", marked)
	}
}

// A CREW MESSAGE CARRYING ATTACHMENTS STATES ITS CREW TOO.
func TestAppAttachedCrewMessageStatesTheCrew(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	backend, _, manager, crew := crewBackendOn(t, conv)
	at, err := backend.AddAttachment(manager.ID, appwire.Attachment{Kind: appwire.AttachPaste}, strings.Repeat("panic: nil map\n", 20))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "look at this", Attachments: []string{at.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := creationWords(conv); len(got) != 1 || got[0] != crew {
		t.Fatalf("attached crew message stated %q, want [%s]", got, crew)
	}
}

// THE FIRST WORDS OF agents.create ARE THE PERSON'S OWN, AND STATE AN EXPLICIT
// NONE.
func TestAppCreateFirstWordsStateNone(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "ok"}}}
	backend, _, _ := workBackend(t, conv)
	if _, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), Text: "look at the build"}); err != nil {
		t.Fatal(err)
	}
	if got := waitCreations(t, conv, 1); got[0] != "none" {
		t.Fatalf("the first words stated %q, want none", got)
	}
}
