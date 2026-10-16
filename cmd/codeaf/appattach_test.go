package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// wireCode is the wire code of an engine error, or "".
func wireCode(err error) string {
	var wire *appwire.Error
	if errors.As(err, &wire) {
		return wire.Code
	}
	return ""
}

// staged says whether id is still staged on a.
func staged(backend *appBackend, agent, id string) bool {
	a := backend.agent(agent)
	ok := false
	backend.locked(func() { _, ok = a.attach[id] })
	return ok
}

// sentTexts is every message the fake engine was sent.
func sentTexts(conv *fakeConversation) []string {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	return append([]string(nil), conv.texts...)
}

// A CREATE WHOSE ATTACHMENTS CANNOT RIDE MAKES NOTHING: no folder minted, no
// conversation opened, no agent remembered and no event said.
func TestAppCreateRefusesBadAttachmentsAndMakesNothing(t *testing.T) {
	var mu sync.Mutex
	var opens, releases int
	var mints atomic.Int32
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(&fakeConversation{}, &opens, &releases, &mu))
	defer backend.Close()
	backend.mint = func(string) (string, error) { mints.Add(1); return appFakeTranscript, nil }
	work := t.TempDir()
	half := strings.Repeat("x", appwire.MaxAttachChars/2)
	eleven := make([]appwire.AttachSource, appwire.MaxAttachments+1)
	for i := range eleven {
		eleven[i] = appwire.AttachSource{Kind: appwire.AttachLink, URL: "https://example.com"}
	}
	for name, sources := range map[string][]appwire.AttachSource{
		"a missing file":        {{Kind: appwire.AttachFile, Path: "gone.go"}},
		"a missing file abroad": {{Kind: appwire.AttachFile, Path: filepath.Join(t.TempDir(), "gone.go")}},
		"a link with no host":   {{Kind: appwire.AttachLink, URL: "not a link"}},
		"an unknown kind":       {{Kind: "image", Path: "a.png"}},
		"a paste with no text":  {{Kind: appwire.AttachPaste}},
		"one paste too long":    {{Kind: appwire.AttachPaste, Text: strings.Repeat("y", appwire.MaxPasteChars+1)}},
		"pastes too long":       {{Kind: appwire.AttachPaste, Text: half}, {Kind: appwire.AttachPaste, Text: half + "x"}},
		"eleven":                eleven,
		"a good one then a bad": {{Kind: appwire.AttachPaste, Text: "log"}, {Kind: appwire.AttachFile, Path: "gone.go"}},
	} {
		_, err := backend.CreateAgent(appwire.CreateAsk{Workspace: work, Text: "look", Attachments: sources})
		if wireCode(err) != appwire.CodeBadParams {
			t.Errorf("%s answered %v", name, err)
		}
	}
	mu.Lock()
	opened := opens
	mu.Unlock()
	var held int
	backend.locked(func() { held = len(backend.agents) })
	rec.mu.Lock()
	said := len(rec.events)
	rec.mu.Unlock()
	if mints.Load() != 0 || opened != 0 || held != 0 || said != 0 {
		t.Fatalf("a refused create left %d mints, %d opens, %d agents and %d events", mints.Load(), opened, held, said)
	}
}

// A LINK OR A PASTE ALONE HAS NO FILE NAME TO BE NAMED FROM: the tab draws what
// its chip says until the person's conversation has a name of its own, and a
// path never reaches the title however the files are spelled.
func TestAppCreateNamesAMessageOfNonFilesFromItsChipAndNeverFromAPath(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "report.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name    string
		sources []appwire.AttachSource
		want    string
	}{
		{"a link", []appwire.AttachSource{{Kind: appwire.AttachLink, URL: "https://example.com/issue/7"}}, "example.com"},
		{"a paste", []appwire.AttachSource{{Kind: appwire.AttachPaste, Text: "one\ntwo"}}, "Pasted text"},
		{"a file by its absolute path", []appwire.AttachSource{{Kind: appwire.AttachFile, Path: filepath.Join(work, "report.pdf")}}, "report.pdf"},
	} {
		t.Run(row.name, func(t *testing.T) {
			conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
			var mu sync.Mutex
			var opens, releases int
			backend := newFakeBackend(newAppRecorder(), fakeOpener(conv, &opens, &releases, &mu))
			defer backend.Close()
			agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: work, Attachments: row.sources})
			if err != nil {
				t.Fatal(err)
			}
			if agent.Title != row.want || strings.Contains(agent.Title, string(filepath.Separator)) {
				t.Fatalf("title = %q, want %q", agent.Title, row.want)
			}
		})
	}
}

// ATTACHMENTS ALONE ARE A FIRST MESSAGE: the agent answers working, named for
// its attached files, its first entry shows no words and every attachment,
// and the model is sent exactly their words, once.
func TestAppCreateWithAttachmentsAloneStartsOnTheirWords(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "notes.md"), []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(work, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: work, ClientID: "c-9", Attachments: []appwire.AttachSource{
		{Kind: appwire.AttachFile, Path: " notes.md "},
		{Kind: appwire.AttachFile, Path: "docs"},
		{Kind: appwire.AttachPaste, Text: "line one\nline two\n"},
		{Kind: appwire.AttachLink, URL: "https://example.com/issue/7"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != appwire.StateWorking || agent.Title != "notes.md, docs" {
		t.Fatalf("the created agent = %+v", agent)
	}
	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser })
	if user.Text != "" || user.ClientID != "c-9" || user.ID != "u0" || len(user.Attachments) != 4 {
		t.Fatalf("the first entry = %+v", user)
	}
	ids := []string{}
	for _, at := range user.Attachments {
		ids = append(ids, at.ID)
	}
	file, dir, paste, link := user.Attachments[0], user.Attachments[1], user.Attachments[2], user.Attachments[3]
	if strings.Join(ids, " ") != "at1 at2 at3 at4" || file.Path != "notes.md" || file.State != appwire.FileExists ||
		dir.State != appwire.FileExists || paste.Lines == nil || *paste.Lines != 2 || link.Title != "example.com" {
		t.Fatalf("the attachments = %+v", user.Attachments)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	want := "Attached file: notes.md\n\nAttached file: docs\n\nPasted text:\n```\nline one\nline two\n```\n\nLink: https://example.com/issue/7"
	if sent := sentTexts(conv); len(sent) != 1 || sent[0] != want {
		t.Fatalf("the engine was sent %q", sent)
	}
	// A chip staged afterwards is numbered after the first message's.
	next, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachLink, URL: "https://example.com"}, "")
	if err != nil || next.ID != "at5" {
		t.Fatalf("the next chip = %+v %v", next, err)
	}
}

// WORDS AND ATTACHMENTS TOGETHER: the entry shows the words as typed, the
// model is sent them first, and a paste of any length rides (the composer's
// short-paste rule is the composer's).
func TestAppCreateKeepsItsWordsBeforeItsAttachments(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "  why does this fail?  ", Attachments: []appwire.AttachSource{
		{Kind: appwire.AttachPaste, Text: "--- FAIL: TestLoad\n"},
	}})
	if err != nil || agent.Title != "why does this fail?" {
		t.Fatalf("the created agent = %+v %v", agent, err)
	}
	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser })
	if user.Text != "why does this fail?" || len(user.Attachments) != 1 || user.Attachments[0].Kind != appwire.AttachPaste {
		t.Fatalf("the first entry = %+v", user)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	if sent := sentTexts(conv); len(sent) != 1 || sent[0] != "why does this fail?\n\nPasted text:\n```\n--- FAIL: TestLoad\n```" {
		t.Fatalf("the engine was sent %q", sent)
	}
}

// A CREATE THAT CANNOT OPEN KEEPS ITS AGENT, stopped with a failed turn, and
// its retry sends the same words and shows the same attachments.
func TestAppCreateBootFailureRetriesTheSameMessage(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	var mu sync.Mutex
	var opens, releases int
	var tries atomic.Int32
	inner := fakeOpener(conv, &opens, &releases, &mu)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		if tries.Add(1) == 1 {
			return nil, errors.New("the profile could not be read")
		}
		return inner(ask)
	})
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "look", Attachments: []appwire.AttachSource{
		{Kind: appwire.AttachLink, URL: "https://example.com/x"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && c.Agent.State == appwire.StateStopped
	})
	if _, err := backend.Retry(agent.ID); err != nil {
		t.Fatal(err)
	}
	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser })
	if user.Text != "look" || len(user.Attachments) != 1 || user.Attachments[0].URL != "https://example.com/x" {
		t.Fatalf("the retried entry = %+v", user)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.OK
	})
	if sent := sentTexts(conv); len(sent) != 1 || sent[0] != "look\n\nLink: https://example.com/x" {
		t.Fatalf("the engine was sent %q", sent)
	}
}

// A SEND THAT DOES NOT START A TURN CONSUMES NOTHING: busy, an id named twice,
// pastes over the message's bound — every chip stays staged, a chip added
// meanwhile is untouched, and the send goes once the turn ends.
func TestAppAttachedSendThatFailsKeepsItsAttachments(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "working"}}, hold: hold}
	backend, rec, agent := workBackend(t, conv)
	long := strings.Repeat("--- FAIL: TestLoad\n", 20)
	one, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, long)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "first", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SendWith(agent.ID, "and this", "m-1", []string{one.ID}); wireCode(err) != appwire.CodeBusy {
		t.Fatalf("a send to a working agent answered %v", err)
	}
	two, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, long)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SendWith(agent.ID, "x", "", []string{two.ID, two.ID}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("an id named twice answered %v", err)
	}
	var huge []string
	for i := 0; i < 3; i++ {
		big, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, strings.Repeat("z", appwire.MaxAttachChars/3+1))
		if err != nil {
			t.Fatal(err)
		}
		huge = append(huge, big.ID)
	}
	if _, err := backend.SendWith(agent.ID, "x", "", huge); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("pastes over the bound answered %v", err)
	}
	if _, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, strings.Repeat("z", appwire.MaxPasteChars+1)); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a paste over its bound was staged: %v", err)
	}
	for _, id := range append([]string{one.ID, two.ID}, huge...) {
		if !staged(backend, agent.ID, id) {
			t.Fatalf("%s was consumed by a send that did not start", id)
		}
	}
	close(hold)
	rec.waitFor(t, 5*time.Second, appTurnDone)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := backend.SendWith(agent.ID, "and this", "m-1", []string{one.ID})
		if err == nil {
			break
		}
		if wireCode(err) != appwire.CodeBusy || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser && e.ClientID == "m-1" })
	if user.Text != "and this" || len(user.Attachments) != 1 || user.Attachments[0].ID != one.ID {
		t.Fatalf("the sent entry = %+v", user)
	}
	if staged(backend, agent.ID, one.ID) || !staged(backend, agent.ID, two.ID) {
		t.Fatal("the send consumed other than what it carried")
	}
}

// SENDS RACING ON ONE AGENT EACH CARRY THEIR OWN SNAPSHOT: an entry shows
// exactly the chip its send named, never another send's, and a send that lost
// the race keeps its chip.
func TestAppRacingAttachedSendsCarryTheirOwnAttachments(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	backend, rec, agent := workBackend(t, conv)
	const n = 6
	ids := make([]string, n)
	for i := range ids {
		at, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachLink, URL: "https://example.com/" + string(rune('a'+i))}, "")
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = at.ID
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = backend.SendWith(agent.ID, "", "r-"+ids[i], []string{ids[i]})
		}(i)
	}
	wg.Wait()
	sentOK := 0
	for i, err := range errs {
		switch {
		case err == nil:
			sentOK++
			user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser && e.ClientID == "r-"+ids[i] })
			if len(user.Attachments) != 1 || user.Attachments[0].ID != ids[i] || staged(backend, agent.ID, ids[i]) {
				t.Fatalf("send %s showed %+v", ids[i], user.Attachments)
			}
		case wireCode(err) == appwire.CodeBusy:
			if !staged(backend, agent.ID, ids[i]) {
				t.Fatalf("a busy send consumed %s", ids[i])
			}
		default:
			t.Fatal(err)
		}
	}
	if sentOK == 0 {
		t.Fatal("no racing send started a turn")
	}
	for _, sent := range sentTexts(conv) {
		if strings.Count(sent, "Link: ") != 1 {
			t.Fatalf("a turn carried %q", sent)
		}
	}
}

// A CREW MESSAGE CARRIES ATTACHMENTS TO ITS TARGET: the manager by default,
// a member named exactly; ids staged on another agent are refused and stay
// where they are; a busy target refuses an attached message and keeps its
// chips, while words alone still wait in its queue.
func TestAppCrewMessageCarriesAttachmentsToItsTarget(t *testing.T) {
	hold := make(chan struct{})
	defer func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	}()
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTextDelta, Text: "on it"}}, hold: hold}
	backend, _, manager, crew := crewBackendOn(t, conv)
	long := strings.Repeat("panic: nil map\n", 20)
	paste := func(agent string) string {
		t.Helper()
		at, err := backend.AddAttachment(agent, appwire.Attachment{Kind: appwire.AttachPaste}, long)
		if err != nil {
			t.Fatal(err)
		}
		return at.ID
	}

	m1 := paste(manager.ID)
	entry, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Attachments: []string{m1}, ClientID: "k-1"})
	if err != nil || entry.Agent != manager.ID || entry.Turn == 0 || len(entry.Attachments) != 1 || entry.Attachments[0].ID != m1 {
		t.Fatalf("the message to the manager = %+v %v", entry, err)
	}

	// The manager is now mid-turn.
	m2 := paste(manager.ID)
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "and this", Attachments: []string{m2}}); wireCode(err) != appwire.CodeBusy {
		t.Fatalf("an attached message to a busy manager answered %v", err)
	}
	if !staged(backend, manager.ID, m2) {
		t.Fatal("a refused crew message consumed its attachment")
	}
	words, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "words alone"})
	if err != nil || words.Turn != 0 {
		t.Fatalf("words to a busy manager = %+v %v", words, err)
	}
	if queued := backend.queued(backend.agent(manager.ID)); len(queued) != 1 || queued[0].Text != "words alone" {
		t.Fatalf("the manager's queue = %+v", queued)
	}

	// A member, named exactly, whose conversation this process can open.
	const member = "b0b0b0b0"
	a := backend.agent(member)
	backend.locked(func() {
		a.transcript = "/state/v3/places/proj/b0b0b0b0/transcript.jsonl"
		a.view.Workspace = manager.Workspace
	})
	if _, err := backend.CrewMessage(appwire.CrewMessage{Crew: crew, To: member, Attachments: []string{m2}}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("the manager's attachment sent to a member answered %v", err)
	}
	if !staged(backend, manager.ID, m2) {
		t.Fatal("a refused message moved another agent's attachment")
	}
	w1 := paste(member)
	entry, err = backend.CrewMessage(appwire.CrewMessage{Crew: crew, To: member, Attachments: []string{w1}})
	if err != nil || entry.Agent != member || len(entry.Attachments) != 1 || entry.Attachments[0].ID != w1 {
		t.Fatalf("the message to the member = %+v %v", entry, err)
	}
	lines, _, err := backend.CrewLog(crew, "", 0)
	if err != nil || len(lines) == 0 || !strings.Contains(lines[len(lines)-1].Text, "Attached: Pasted text") {
		t.Fatalf("the manager was told %+v %v", lines, err)
	}
}
