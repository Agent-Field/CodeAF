package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// appSendMessageWhenIdle sends once the agent's last turn has let go of it.
func appSendMessageWhenIdle(t *testing.T, backend *appBackend, agent string, message appwire.AgentMessage) uint64 {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		turn, err := backend.SendMessage(agent, message)
		if err == nil {
			return turn
		}
		if wireCode(err) != appwire.CodeBusy || time.Now().After(deadline) {
			t.Fatalf("send %q = %v", message.Text, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// appLiveUser is the user entry the backend said live for one client id.
func appLiveUser(t *testing.T, rec *appRecorder, clientID string) appwire.Entry {
	t.Helper()
	i := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Kind == appwire.EntryUser && added.Entry.ClientID == clientID
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.data[i].(appwire.EntryAdded).Entry
}

// appSpecsRead is a whole read of two hundred references, one of them gone.
func appSpecsRead() appwire.MessageRead {
	return appwire.MessageRead{
		Source: appwire.MessageReference{Kind: "collection", ID: "c1", Name: "Specs"},
		Count:  200, Missing: 1, Complete: true,
		Warnings: []string{"one reference is gone"},
		Items: []appwire.MessageReference{
			{Kind: "file", ID: "r1", Name: "a.md", Path: "docs/a.md"},
			{Kind: "link", ID: "r2", URL: "https://example.com/b"},
		},
	}
}

// appCutRead is a read that could not ask about two references and was cut.
func appCutRead() appwire.MessageRead {
	return appwire.MessageRead{
		Source: appwire.MessageReference{Kind: "collection", ID: "c2", Name: "Links"},
		Count:  1, Unavailable: 2, Unread: 1, More: true,
		Items: []appwire.MessageReference{{Kind: "link", URL: "https://example.com/c"}},
	}
}

// WHAT THE APP READ TO WRITE A MESSAGE IS WHAT A REOPENED CONVERSATION SHOWS.
// A real conversation against a scripted model is sent the composer's expanded
// words with the person's own sentence, a read and an attachment; then a read
// alone with no words of theirs; then plain words. The live entry shows their
// sentence, the read and the attachment; the model is sent the expanded words;
// a read that says it is whole while cut is refused and records nothing. A NEW
// backend reading the record off the disk shows exactly the same.
func TestAppReadsSurviveAReopenedRecord(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	if err := os.WriteFile(filepath.Join(workspace, "notes.md"), []byte("# notes\n"), 0o644); err != nil {
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
	agent, err := mustCreate(t, first, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	file, err := first.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachFile, Path: "notes.md"}, "")
	if err != nil {
		t.Fatal(err)
	}
	said, none := "compare these specs", ""
	expanded := "compare these specs\n\nSpecs:\n- docs/a.md\n- https://example.com/b"
	before := len(appModelRequests(model))
	turn := appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{
		Text: expanded, Said: &said, ClientID: "r-1", Attachments: []string{file.ID}, Reads: []appwire.MessageRead{appSpecsRead(), appCutRead()},
	})
	live := appLiveUser(t, rec, "r-1")
	if live.Text != said || appAttachIDs(live) != file.ID || !reflect.DeepEqual(live.Reads, []appwire.MessageRead{appSpecsRead(), appCutRead()}) {
		t.Fatalf("the live entry = %+v", live)
	}
	appTurnDoneFor(t, rec, turn)
	sent := false
	for _, request := range appModelRequests(model)[before:] {
		if strings.Contains(strings.Join(request.texts(), "\n"), expanded+"\n\nAttached file: notes.md") {
			sent = true
		}
	}
	if !sent {
		t.Fatal("the model was not sent the expanded words and the attachment")
	}

	appTurnDoneFor(t, rec, appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{
		Text: "Links:\n- https://example.com/c", Said: &none, ClientID: "r-2", Reads: []appwire.MessageRead{appCutRead()},
	}))
	if live := appLiveUser(t, rec, "r-2"); live.Text != "" || !reflect.DeepEqual(live.Reads, []appwire.MessageRead{appCutRead()}) {
		t.Fatalf("the read alone went live as %+v", live)
	}
	lying := appCutRead()
	lying.Complete = true
	asked := len(appModelRequests(model))
	if _, err := first.SendMessage(agent.ID, appwire.AgentMessage{Text: "x", Said: &said, ClientID: "r-x", Reads: []appwire.MessageRead{lying}}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a read that says it is whole while cut answered %v", err)
	}
	if got := len(appModelRequests(model)); got != asked {
		t.Fatalf("a refused message asked the model %d times", got-asked)
	}
	appTurnDoneFor(t, rec, appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{Text: "plain words", ClientID: "r-3"}))
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	second := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer second.Close()
	entries, _, err := second.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	users := appUsersOf(entries)
	if len(users) != 3 {
		t.Fatalf("the reopened record holds %d messages of the person's: %+v", len(users), users)
	}
	if users[0].Text != said || appAttachIDs(users[0]) != file.ID || !reflect.DeepEqual(users[0].Reads, []appwire.MessageRead{appSpecsRead(), appCutRead()}) {
		t.Fatalf("the message with reads reopened as %+v", users[0])
	}
	if users[1].Text != "" || !reflect.DeepEqual(users[1].Reads, []appwire.MessageRead{appCutRead()}) {
		t.Fatalf("the read alone reopened as %+v", users[1])
	}
	if users[2].Text != "plain words" || users[2].Reads != nil {
		t.Fatalf("the plain words reopened as %+v", users[2])
	}
}

// A CREW MESSAGE'S READS ARE KEPT ON THE CONVERSATION IT WENT TO, the person's
// own words with them, and its entry says both; a reopened record says the same.
func TestAppCrewReadsAreKeptOnTheirTarget(t *testing.T) {
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
	lead := appTranscriptOf(first, manager.ID)
	crew := teamstore.NewID()
	err = teamstore.Update(profile, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "harbor", Made: time.Now()})
		if err := f.AddMember(crew, teamstore.Member{Key: lead, File: lead, Where: workspace, Word: "lead", Handle: "lead"}); err != nil {
			return err
		}
		return f.SetManager(crew, lead)
	})
	if err != nil {
		t.Fatal(err)
	}
	said := "read the specs"
	entry, err := first.CrewMessage(appwire.CrewMessage{Crew: crew, Text: "read the specs\n\n- docs/a.md", Said: &said, ClientID: "k-1", Reads: []appwire.MessageRead{appSpecsRead()}})
	if err != nil || entry.Agent != manager.ID || entry.Text != said || !reflect.DeepEqual(entry.Reads, []appwire.MessageRead{appSpecsRead()}) {
		t.Fatalf("the message to the manager = %+v %v", entry, err)
	}
	appTurnDoneFor(t, rec, entry.Turn)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	second := newAppBackend(newAppRecorder(), appOpenInProcess)
	second.profileDir = profile
	defer second.Close()
	entries, _, err := second.Entries(manager.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	users := appUsersOf(entries)
	if len(users) != 1 || users[0].Text != said || !reflect.DeepEqual(users[0].Reads, []appwire.MessageRead{appSpecsRead()}) {
		t.Fatalf("the manager reopened with %+v", users)
	}
}
