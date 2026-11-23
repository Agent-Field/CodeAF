package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// appTurnDoneFor waits for one turn's end and says whether it ended well.
func appTurnDoneFor(t *testing.T, rec *appRecorder, turn uint64) {
	t.Helper()
	i := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && done.Turn == turn
	})
	rec.mu.Lock()
	done := rec.data[i].(appwire.TurnDone)
	rec.mu.Unlock()
	if !done.OK {
		t.Fatalf("turn %d = %+v", turn, done)
	}
}

// appSendWithWhenIdle sends once the agent's last turn has let go of it.
func appSendWithWhenIdle(t *testing.T, backend *appBackend, agent, text, clientID string, ids []string) uint64 {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		turn, err := backend.SendWith(agent, text, clientID, ids)
		if err == nil {
			return turn
		}
		if wireCode(err) != appwire.CodeBusy || time.Now().After(deadline) {
			t.Fatalf("send %q = %v", text, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// appUsersOf is the person's entries of a listing, oldest first.
func appUsersOf(entries []appwire.Entry) []appwire.Entry {
	var users []appwire.Entry
	for _, entry := range entries {
		if entry.Kind == appwire.EntryUser {
			users = append(users, entry)
		}
	}
	return users
}

// appAttachIDs is an entry's attachment ids, joined.
func appAttachIDs(entry appwire.Entry) string {
	ids := make([]string, 0, len(entry.Attachments))
	for _, at := range entry.Attachments {
		ids = append(ids, at.ID)
	}
	return strings.Join(ids, " ")
}

// appModelRequests is every request the scripted model has been sent so far.
func appModelRequests(model *recordingModel) []recordedRequest {
	model.mu.Lock()
	defer model.mu.Unlock()
	return append([]recordedRequest(nil), model.requests...)
}

// appTranscriptOf is where an agent's record is.
func appTranscriptOf(backend *appBackend, agent string) string {
	a := backend.agent(agent)
	var transcript string
	backend.locked(func() { transcript = a.transcript })
	return transcript
}

// WHAT A PERSON SENT IS WHAT A REOPENED CONVERSATION SHOWS. A real
// conversation, built in this process against a scripted model, is created
// with words and attachments, sent more with attachments, sent attachments
// alone, and sent plain words that merely mention a path. The backend is let
// go and a NEW one — nothing of the first process's memory — reads the record
// off the disk: each message is the person's own words with exactly the
// attachments that rode on it, once; the attachments alone are attachments
// with no words; the plain words stay words with none invented out of them.
// The model was sent the attachments as words, and still is after the reopen.
func TestAppAttachmentsSurviveAReopenedRecord(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	if err := os.WriteFile(filepath.Join(workspace, "notes.md"), []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	first := newAppBackend(rec, appOpenInProcess)
	closed := false
	defer func() {
		if !closed {
			first.Close()
		}
	}()
	agent, err := first.CreateAgent(appwire.CreateAsk{Workspace: workspace, Text: "why does this fail?", ClientID: "c-0", Attachments: []appwire.AttachSource{
		{Kind: appwire.AttachFile, Path: "notes.md"},
		{Kind: appwire.AttachPaste, Text: "--- FAIL: TestLoad\nexpected 1\n"},
		{Kind: appwire.AttachLink, URL: "https://example.com/issue/7"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 30*time.Second, appTurnDone)
	file, err := first.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachFile, Path: "main.go"}, "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, appSendWithWhenIdle(t, first, agent.ID, "and look at this", "c-1", []string{file.ID}))
	long := strings.Repeat("panic: nil map\n", 20)
	paste, err := first.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, long)
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, appSendWithWhenIdle(t, first, agent.ID, "", "c-2", []string{paste.ID}))
	// A SEND OF AN ID ALREADY SENT IS REFUSED AND RECORDS NOTHING: the chip
	// went with the message it rode on.
	if _, err := first.SendWith(agent.ID, "again", "c-x", []string{paste.ID}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("a send of a sent attachment answered %v", err)
	}
	plain := "plain words that mention Attached file: notes.md"
	appTurnDoneFor(t, rec, appSendWithWhenIdle(t, first, agent.ID, plain, "c-3", nil))
	transcript := appTranscriptOf(first, agent.ID)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	rec2 := newAppRecorder()
	second := newAppBackend(rec2, appOpenInProcess)
	defer second.Close()
	entries, _, err := second.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	users := appUsersOf(entries)
	if len(users) != 4 {
		t.Fatalf("the reopened record holds %d messages of the person's: %+v", len(users), users)
	}
	created, sent, alone, words := users[0], users[1], users[2], users[3]
	if created.Text != "why does this fail?" || appAttachIDs(created) != "at1 at2 at3" {
		t.Fatalf("the created message reopened as %+v", created)
	}
	if at := created.Attachments; at[0].Kind != appwire.AttachFile || at[0].Path != "notes.md" || at[0].Title != "notes.md" ||
		at[1].Kind != appwire.AttachPaste || at[1].Lines == nil || *at[1].Lines != 2 || at[1].Title != "Pasted text" ||
		at[2].Kind != appwire.AttachLink || at[2].URL != "https://example.com/issue/7" || at[2].Title != "example.com" {
		t.Fatalf("the created message's attachments = %+v", at)
	}
	if sent.Text != "and look at this" || appAttachIDs(sent) != file.ID || sent.Attachments[0].Path != "main.go" {
		t.Fatalf("the sent message reopened as %+v", sent)
	}
	if alone.Text != "" || appAttachIDs(alone) != paste.ID || alone.Attachments[0].Kind != appwire.AttachPaste {
		t.Fatalf("the attachments alone reopened as %+v", alone)
	}
	if words.Text != plain || len(words.Attachments) != 0 {
		t.Fatalf("the plain words reopened as %+v", words)
	}
	for _, user := range users[:3] {
		if strings.Contains(user.Text, "Attached file:") || strings.Contains(user.Text, "Pasted text:") || strings.Contains(user.Text, "Link:") {
			t.Fatalf("a message reopened showing the words the model was sent: %q", user.Text)
		}
	}

	// The record's own reading says the same, with what the app measured.
	var described []session.DisplayEntry
	for _, item := range session.ReadTranscript(transcript).Entries {
		if item.Role == "user" {
			described = append(described, item)
		}
	}
	if len(described) != 4 || described[2].Person == nil || !described[2].Person.Authored || described[2].Text != "" {
		t.Fatalf("the record reads %+v", described)
	}
	// A NEW PLAIN MESSAGE IS NOT UNMARKED: every message the app sends now
	// carries a durable id minted by the engine, so the plain words have a
	// person mark. What they must NOT have is anything forged around it — no
	// attachments, no pictures, no annotation, no reads, and no delivery
	// beyond the true one: words reached the model as words, with no vision
	// model involved.
	plainMark := described[3].Person
	// Authored stays false: no words were recorded apart from the line itself,
	// so the line's own Text is the person's words, as it was before ids.
	if plainMark == nil || plainMark.Authored || described[3].Text != plain || len(plainMark.Attachments) != 0 || len(plainMark.Reads) != 0 {
		t.Fatalf("the plain words' record = %+v person=%+v", described[3], plainMark)
	}
	if plainMark.Composed == nil || plainMark.Composed.MessageID == "" || len(plainMark.Composed.Media) != 0 ||
		plainMark.Composed.Annotation != nil ||
		plainMark.Composed.Delivery == nil || plainMark.Composed.Delivery.Mode != session.DeliveryText || plainMark.Composed.Delivery.Model != "" {
		t.Fatalf("the plain words' durable mark = %+v", plainMark.Composed)
	}
	// Each message's id is its own: none repeats across the four.
	seen := map[string]bool{}
	for index, item := range described {
		if item.Person == nil || item.Person.Composed == nil || item.Person.Composed.MessageID == "" || seen[item.Person.Composed.MessageID] {
			t.Fatalf("message %d has no distinct durable id: %+v", index, item.Person)
		}
		seen[item.Person.Composed.MessageID] = true
	}
	if got := described[2].Person.Attachments; len(got) != 1 || got[0].Bytes == nil || *got[0].Bytes != int64(len(long)) || got[0].Lines == nil || *got[0].Lines != 20 {
		t.Fatalf("the paste's description = %+v", got)
	}
	if got := described[0].Person.Attachments; got[1].Bytes == nil || *got[1].Bytes != int64(len("--- FAIL: TestLoad\nexpected 1\n")) || got[0].Bytes != nil || got[0].Lines != nil {
		t.Fatalf("the created message's descriptions = %+v", got)
	}

	// AND THE MODEL STILL READS WHAT IT READ: the attachments as words, in the
	// history a turn on the reopened conversation sends.
	before := len(appModelRequests(model))
	turn, err := second.Send(agent.ID, "and now?", "c-4")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec2, turn)
	replayed := false
	for _, request := range appModelRequests(model)[before:] {
		all := strings.Join(request.texts(), "\n")
		if strings.Contains(all, "why does this fail?\n\nAttached file: notes.md") && strings.Contains(all, "Pasted text:\n```\n"+strings.TrimRight(long, "\n")) {
			replayed = true
		}
	}
	if !replayed {
		t.Fatal("the reopened conversation did not send the model the attachments it was sent before")
	}
}

// A CREW'S MESSAGES ARE KEPT ON THE CONVERSATION THEY WENT TO. Two real
// conversations make a crew; a message to the manager and one to the member,
// each with attachments staged on its own target, and one naming the
// manager's chip to the member, which is refused. Read back by a new process,
// each record holds its own message with its own attachments, the refused one
// is nowhere, and the manager's chip is still the manager's to send.
func TestAppCrewAttachmentsAreKeptOnTheirTarget(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	profile := filepath.Join(root, "crew-profile")
	rec := newAppRecorder()
	first := newAppBackend(rec, appOpenInProcess)
	first.profileDir = profile
	closed := false
	defer func() {
		if !closed {
			first.Close()
		}
	}()
	manager, err := mustCreate(t, first, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	member, err := mustCreate(t, first, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	lead, web := appTranscriptOf(first, manager.ID), appTranscriptOf(first, member.ID)
	crew := teamstore.NewID()
	err = teamstore.Update(profile, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "harbor", Made: time.Now()})
		if err := f.AddMember(crew, teamstore.Member{Key: lead, File: lead, Where: workspace, Word: "lead", Handle: "lead"}); err != nil {
			return err
		}
		if err := f.AddMember(crew, teamstore.Member{Key: web, File: web, Where: workspace, Word: "web", Handle: "web"}); err != nil {
			return err
		}
		return f.SetManager(crew, lead)
	})
	if err != nil {
		t.Fatal(err)
	}
	stage := func(agent, url string) string {
		t.Helper()
		at, err := first.AddAttachment(agent, appwire.Attachment{Kind: appwire.AttachLink, URL: url}, "")
		if err != nil {
			t.Fatal(err)
		}
		return at.ID
	}
	toLead := stage(manager.ID, "https://example.com/lead")
	kept := stage(manager.ID, "https://example.com/kept")
	toWeb := stage(member.ID, "https://example.com/web")

	entry, err := first.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "for the lead", Attachments: []string{toLead}, ClientID: "k-1"})
	if err != nil || entry.Agent != manager.ID {
		t.Fatalf("the message to the manager = %+v %v", entry, err)
	}
	appTurnDoneFor(t, rec, entry.Turn)
	if _, err := first.CrewMessage(appwire.CrewMessage{Crew: crew, To: member.ID, Text: "not yours", Attachments: []string{kept}}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("the manager's attachment sent to the member answered %v", err)
	}
	entry, err = first.CrewMessage(appwire.CrewMessage{Crew: crew, To: member.ID, Text: "for the web", Attachments: []string{toWeb}, ClientID: "k-2"})
	if err != nil || entry.Agent != member.ID {
		t.Fatalf("the message to the member = %+v %v", entry, err)
	}
	appTurnDoneFor(t, rec, entry.Turn)
	if !staged(first, manager.ID, kept) || staged(first, member.ID, kept) {
		t.Fatal("a refused message moved the manager's attachment")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	second := newAppBackend(newAppRecorder(), appOpenInProcess)
	second.profileDir = profile
	defer second.Close()
	for _, want := range []struct {
		agent, text, ids, url string
	}{
		{manager.ID, "for the lead", toLead, "https://example.com/lead"},
		{member.ID, "for the web", toWeb, "https://example.com/web"},
	} {
		entries, _, err := second.Entries(want.agent, "", appwire.MaxEntries)
		if err != nil {
			t.Fatal(err)
		}
		users := appUsersOf(entries)
		if len(users) != 1 || users[0].Text != want.text || appAttachIDs(users[0]) != want.ids || users[0].Attachments[0].URL != want.url {
			t.Fatalf("%s reopened with %+v", want.agent, users)
		}
	}
}

// A RECORD WRITTEN BEFORE MESSAGES CARRIED A DURABLE ID STILL READS AS IT
// ALWAYS DID. The fixture is a journal written by hand, independent of the
// live backend: a plain user line with no presentation mark. It has no person
// mark at all, and nothing is invented to give it one.
func TestALegacyPlainMessageHasNoPersonMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	lines := []string{
		`{"type":"session","version":1,"id":"legacy-1","cwd":"/tmp","model":"m","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"message","role":"user","content":"an old plain message","timestamp":"2026-01-01T00:00:01Z"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var users []session.DisplayEntry
	for _, item := range session.ReadTranscript(path).Entries {
		if item.Role == "user" {
			users = append(users, item)
		}
	}
	if len(users) != 1 || users[0].Text != "an old plain message" || users[0].Person != nil {
		t.Fatalf("the legacy message reads %+v", users)
	}
}
