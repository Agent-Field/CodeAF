package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

type appFolderFixture struct {
	b                                         *appBackend
	a                                         *appAgent
	dir, transcript, id, workspace, artifacts string
	clock                                     *closeClock
}

func eachAppFolder(t *testing.T, check func(*testing.T, appFolderFixture), layouts ...string) {
	t.Helper()
	if len(layouts) == 0 {
		layouts = []string{"cell", "legacy", "flat"}
	}
	for _, layout := range layouts {
		t.Run(layout, func(t *testing.T) {
			appDayProfile(t)
			id := session.NewSessionID()
			bucket := filepath.Join(session.PlacesRoot(), "app-folder-fixtures")
			dir := filepath.Join(bucket, id)
			if layout == "cell" {
				// v3NewFolder uses this same constructor and class for app chats.
				c, err := cell.CreateIn(bucket, cell.Options{Class: cell.HostBound})
				if err != nil {
					t.Fatal(err)
				}
				dir, id = c.Root, c.ID
			}
			transcript := filepath.Join(dir, "transcript.jsonl")
			artifacts := filepath.Join(dir, "artifacts")
			switch layout {
			case "cell":
				transcript = filepath.Join(dir, cell.TranscriptPath)
				artifacts = filepath.Join(dir, cell.StateDir, "artifacts")
			case "flat":
				transcript = filepath.Join(dir, "other.jsonl")
			}
			for _, path := range []string{filepath.Join(dir, "work"), artifacts} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			workspace := t.TempDir()
			clock := &closeClock{at: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
			if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: workspace, Created: clock.now().Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}
			header, err := json.Marshal(map[string]any{"type": "session", "version": 1, "id": id})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(transcript, append(header, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			b := newAppBackend(newAppRecorder(), nil)
			b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
			b.now, b.after = clock.now, clock.after
			a := &appAgent{transcript: transcript, view: appwire.Agent{ID: id, Workspace: workspace, State: appwire.StateIdle}}
			b.locked(func() { b.agents[id] = a })
			t.Cleanup(func() { _ = b.Close() })
			index := filepath.Join(t.TempDir(), "artifacts.jsonl")
			oldIndex := appMediaIndex
			appMediaIndex = func() string { return index }
			t.Cleanup(func() { appMediaIndex = oldIndex })
			check(t, appFolderFixture{b: b, a: a, dir: dir, transcript: transcript, id: id, workspace: workspace, artifacts: artifacts, clock: clock})
		})
	}
}

// F1 accepts and serves the chat's managed log; F2 preserves legacy logs;
// F3 uses the transcript's own directory when its name is unrecognized.
func TestAppFolderProcessLogs(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		log := filepath.Join(f.dir, "logs", "jobs", "7.log")
		if err := os.MkdirAll(filepath.Dir(log), 0o700); err != nil {
			t.Fatal(err)
		}
		const output = "started\nfolder output\n"
		if err := os.WriteFile(log, []byte(output), 0o600); err != nil {
			t.Fatal(err)
		}
		job := session.JobNotice{ID: 7, Kind: session.JobKindCommand, State: session.JobDone, Started: f.clock.now().Add(-time.Minute), Ended: f.clock.now(), LogPath: log}
		// The process store stays beside the journal, including inside .cell/.
		seedRecords(t, filepath.Dir(f.transcript), job)
		page, err := f.b.PageProcesses(context.Background(), appwire.ProcessPageAsk{Agent: f.id})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Processes) != 1 || page.Processes[0].LastLine != "folder output" || page.Processes[0].LogGone {
			t.Fatalf("process page = %+v", page)
		}
		id := appProcessID(f.id, job)
		read, err := f.b.ReadProcessLog(context.Background(), appwire.ProcessLogAsk{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if read.Text != output || !read.EOF || read.LogGone {
			t.Fatalf("managed log = %+v", read)
		}
		actions, err := f.b.ProcessActions(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !actions.Log.Available {
			t.Fatalf("managed log action = %+v", actions.Log)
		}
	})
}

// F1 uses the cell chat's session id and roots for media reads and attachment
// records; F2 and F3 retain the same scope for legacy and unrecognized paths.
// F6 appends only to the global index and preserves the existing rows.
func TestAppFolderMediaScope(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		id, roots, err := f.b.mediaScope(f.id)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{f.workspace, filepath.Join(f.dir, "work"), f.artifacts}
		if id != f.id || !reflect.DeepEqual(roots, want) {
			t.Fatalf("media scope = %q %v, want %q %v", id, roots, f.id, want)
		}
		picture := filepath.Join(f.dir, "work", "picture.png")
		data := writePNG(t, picture, 3, 2)
		source := session.RecordMedia(appMediaIndex(), session.Artifact{Path: picture, Session: f.id, Title: "picture.png", Kind: "image", Created: f.clock.now()}, session.GeneratedMedia(picture, data, "fixture"))
		media, err := f.b.GetMedia(f.id, source)
		if err != nil || media.State != appwire.MediaAvailable {
			t.Fatalf("media in chat work folder = %+v, %v", media, err)
		}
		attached := filepath.Join(f.workspace, "attached.png")
		writePNG(t, attached, 2, 3)
		before := appFolderSnapshot(t, f.dir)
		index, err := os.ReadFile(appMediaIndex())
		if err != nil {
			t.Fatal(err)
		}
		f.b.recordAttachedMedia(f.a, []appwire.Attachment{{Kind: appwire.AttachFile, Path: attached}})
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("recording attached media changed chat files")
		}
		if after, err := os.ReadFile(appMediaIndex()); err != nil || len(after) <= len(index) || !bytes.HasPrefix(after, index) {
			t.Fatalf("recording attached media replaced the existing global index: %v", err)
		}
		page, err := f.b.ListMedia(f.id, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		attachedFound := false
		for _, source := range page.Sources {
			attachedFound = attachedFound || source.Origin == session.MediaAttachment
		}
		if len(page.Sources) != 2 || !attachedFound {
			t.Fatalf("media and attachment ownership = %+v", page)
		}
	})
}

// F1 roots deliverables and task working copies at the chat folder; F2 and F3
// preserve the Place and readable deliverables for the other transcript paths.
func TestAppFolderDeliverablesPlace(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		known := f.b.deliverEnv().agent(f.id)
		work := filepath.Join(f.dir, "work")
		if !known.known || known.place.Dir != f.dir || known.place.Work() != work || known.place.Artifacts() != f.artifacts {
			t.Fatalf("deliverables place = %+v", known)
		}
		if !reflect.DeepEqual(known.roots, []string{f.workspace, work, f.artifacts}) || !reflect.DeepEqual(known.folders, []string{work, f.artifacts}) {
			t.Fatalf("deliverable roots = %v, folders = %v", known.roots, known.folders)
		}
		real, err := filepath.EvalSymlinks(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := known.place.Trees(), filepath.Join(real, "trees"); got != want {
			t.Fatalf("task working copies = %q, want %q", got, want)
		}
		for _, folder := range []string{work, f.artifacts} {
			path := filepath.Join(folder, "notes.md")
			if err := os.WriteFile(path, []byte("folder deliverable\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: f.clock.now(), Origin: session.DeliverOriginAgent, Authored: true}
			if !session.RecordArtifact(appMediaIndex(), session.Artifact{Path: path, Session: f.id, Title: "notes.md", Kind: "markdown", Created: f.clock.now(), Deliver: &fact}) {
				t.Fatal("deliverable was not recorded")
			}
			got, err := f.b.GetDeliverable(f.id, fact.ID)
			if err != nil || got.Live != "available" || got.Place != "agent_folder" {
				t.Fatalf("deliverable in %s = %+v, %v", folder, got, err)
			}
			read, err := f.b.ReadDeliverable(f.id, fact.ID, "", appwire.DeliverableReadMax)
			if err != nil || read.Text != "folder deliverable\n" {
				t.Fatalf("read deliverable in %s = %+v, %v", folder, read, err)
			}
		}
	})
}

// F1 reads presence and enrolls close claims at the same <chat>/presence.json
// the session writes; F2 and F3 preserve that path for legacy and flat files.
// F6 leaves every chat file intact while observing another holder.
func TestAppFolderClosePresence(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		presence := session.SessionPresence{Schema: 1, SessionID: f.id, Workspace: f.workspace, PID: os.Getpid() + 1, UpdatedAt: f.clock.now(), State: session.PresenceWorking}
		raw, err := json.Marshal(presence)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.dir, "presence.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, fresh := session.ReadSessionPresence(f.dir, f.clock.now()); !fresh || got.PID != presence.PID {
			t.Fatalf("session presence = %+v, fresh = %v", got, fresh)
		}
		before := appFolderSnapshot(t, f.dir)
		f.b.refreshElsewhere(f.a)
		if f.a.view.State != appwire.StateWorking {
			t.Fatalf("fresh holder state = %s", f.a.view.State)
		}
		f.b.closeRecovery.locked(func() {
			claim := f.b.closeRecovery.claims[f.dir]
			if len(f.b.closeRecovery.claims) != 1 || claim == nil || claim.presence.Dir != f.dir {
				t.Errorf("close claims = %+v", f.b.closeRecovery.claims)
			}
		})
		f.b.locked(func() { f.a.view.State = appwire.StateIdle })
		if !f.b.abandonCloseToHolder(f.a) || f.a.view.State != appwire.StateWorking {
			t.Fatalf("close recovery missed the holder at %s", f.dir)
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("observing another holder changed chat files")
		}
		// A person's newer metadata clears the close guard from the same folder.
		if err := f.b.state.write(func(state *appStateFile) error {
			state.CloseResumes[f.id] = 1
			state.CloseResumedAt[f.id] = f.clock.now().Add(-time.Minute)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		meta, err := session.LoadMeta(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		meta.LastUserAt = f.clock.now()
		if err := session.SaveMeta(f.dir, meta); err != nil {
			t.Fatal(err)
		}
		if reset, err := f.b.resetCloseAfterPerson(f.a); err != nil || !reset {
			t.Fatalf("new directions did not reset close recovery: %v, %v", reset, err)
		}
	})
}

// F1 keeps app receipts and crew deletion checks beside the cell chat; F2 and
// F3 keep their previous locations. F4 keeps the existing receipt wire fields.
func TestAppFolderSidecarsAndMembership(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		if got := f.b.renderDir(f.a); got != f.dir {
			t.Fatalf("render folder = %q, want %q", got, f.dir)
		}
		if err := f.b.keepAppFooter(f.a, "folder-receipt", appwire.FooterStoppedByClose, appCloseStoppedWord); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(f.dir, appFootersName)); err != nil {
			t.Fatal(err)
		}
		kept, err := appReadFooters(f.transcript)
		if err != nil || len(kept) != 1 || kept[0].Entry.ID != "folder-receipt" || kept[0].Entry.Agent != f.id || kept[0].Entry.Text != appCloseStoppedWord {
			t.Fatalf("saved receipt = %+v, %v", kept, err)
		}
		team := teamstore.Team{Members: []teamstore.Member{{Key: f.transcript, File: f.transcript}}}
		if here, err := appConversationHere(team, f.transcript); err != nil || !here {
			t.Fatalf("existing conversation = %v, %v", here, err)
		}
		if err := os.WriteFile(filepath.Join(f.dir, teamstore.ConversationDeletedFile), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if here, err := appConversationHere(team, f.transcript); err != nil || here {
			t.Fatalf("deleted conversation = %v, %v", here, err)
		}
	})
}
