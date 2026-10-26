package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// F5 keeps old-path receipts visible through history, merges both files by
// entry id on write, and leaves the old file intact through repeated writes.
func TestAppFolderFooterUpgrade(t *testing.T) {
	for _, corrected := range []bool{false, true} {
		name := "old-only"
		if corrected {
			name = "both-locations"
		}
		t.Run(name, func(t *testing.T) {
			eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
				old := []appKeptFooter{
					{After: 0, Entry: appFooter("old", ".cell", 0, appwire.FooterStoppedByClose, "old receipt", f.clock.now())},
					{After: 0, Entry: appFooter("shared", f.id, 0, appwire.FooterResumedAfterQuit, "old shared receipt", f.clock.now())},
				}
				old[0].Entry.Link = &appwire.EntryLink{Label: "Settings › Permissions", Target: "settings:permissions"}
				old[0].Entry.Why = "Matches your saved rule"
				oldPath := filepath.Join(filepath.Dir(f.transcript), appFootersName)
				writeCloseJSON(t, oldPath, old)
				want := append([]appKeptFooter(nil), old...)
				if corrected {
					current := []appKeptFooter{
						{After: 0, Entry: appFooter("shared", f.id, 0, appwire.FooterResumedAfterQuit, "corrected shared receipt", f.clock.now())},
						{After: 0, Entry: appFooter("current", f.id, 0, appwire.FooterStoppedByClose, "current receipt", f.clock.now())},
					}
					writeCloseJSON(t, filepath.Join(f.dir, appFootersName), current)
					want[1] = current[0]
					want = append(want, current[1])
				}
				before := appFolderSnapshot(t, f.dir)
				assertHistory := func(want []appKeptFooter) {
					t.Helper()
					entries, more, err := f.b.Entries(f.id, "", 100)
					if err != nil || more || len(entries) != len(want) {
						t.Fatalf("receipt history = %+v, %v, %v; want %+v", entries, more, err, want)
					}
					for i, kept := range want {
						if !reflect.DeepEqual(entries[i], kept.Entry) {
							t.Fatalf("history receipt %d = %+v, want %+v", i, entries[i], kept.Entry)
						}
					}
				}
				assertHistory(want)
				if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
					t.Fatal("reading receipts changed the chat")
				}
				empty := ""
				for write := 0; write < 2; write++ {
					entry := appFooter("following-"+strconv.Itoa(write), f.id, 0, appwire.FooterStoppedByClose, "following receipt", f.clock.now())
					want = append(want, appKeptFooter{AfterID: &empty, Entry: entry})
					// A repeated id in one exchange is also one receipt.
					if err := f.b.keepAppEntries(f.a, []appwire.Entry{entry, entry}, ""); err != nil {
						t.Fatal(err)
					}
					raw, err := os.ReadFile(filepath.Join(f.dir, appFootersName))
					if err != nil {
						t.Fatal(err)
					}
					var saved []appKeptFooter
					if err := json.Unmarshal(raw, &saved); err != nil || !reflect.DeepEqual(saved, want) {
						t.Fatalf("write %d kept %+v, %v; want %+v", write, saved, err, want)
					}
					assertHistory(want)
					after := appFolderSnapshot(t, f.dir)
					delete(after, appFootersName)
					delete(before, appFootersName)
					if !reflect.DeepEqual(after, before) {
						t.Fatal("saving receipts changed files other than the corrected sidecar")
					}
				}
			}, "cell")
		})
	}
}

// F5 refuses to replace either sidecar when one cannot be decoded; F6 keeps
// every file, including the old receipts, unchanged on that failure.
func TestAppFolderFooterUpgradeUnreadable(t *testing.T) {
	for _, bad := range []string{"old", "corrected"} {
		t.Run(bad, func(t *testing.T) {
			eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
				old := filepath.Join(filepath.Dir(f.transcript), appFootersName)
				corrected := filepath.Join(f.dir, appFootersName)
				kept := []appKeptFooter{{Entry: appFooter("kept", f.id, 0, appwire.FooterStoppedByClose, "kept", f.clock.now())}}
				writeCloseJSON(t, old, kept)
				writeCloseJSON(t, corrected, kept)
				path := old
				if bad == "corrected" {
					path = corrected
				}
				if err := os.WriteFile(path, []byte("["), 0o600); err != nil {
					t.Fatal(err)
				}
				before := appFolderSnapshot(t, f.dir)
				if _, _, err := f.b.Entries(f.id, "", 100); err == nil {
					t.Fatal("unreadable receipt history appeared empty")
				}
				if err := f.b.keepAppFooter(f.a, "next", appwire.FooterStoppedByClose, "next"); err == nil {
					t.Fatal("saving over unreadable receipts succeeded")
				}
				if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
					t.Fatal("failed upgrade changed the chat")
				}
			}, "cell")
		})
	}
}

// F6 removes the whole unused chat, keeps siblings, and preserves chats with
// work at the owning root or truth in .cell. F2 keeps the legacy judgement.
func TestAppFolderReapBlank(t *testing.T) {
	for _, kept := range []string{"blank", "spoken", "unreadable", "work", "trees", "artifacts", "tasks", "turns", "crew"} {
		t.Run(kept, func(t *testing.T) {
			layouts := []string{"cell", "legacy"}
			if kept == "turns" {
				layouts = []string{"cell"}
			}
			eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
				for _, path := range []string{filepath.Join(f.dir, "work"), f.artifacts} {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				switch kept {
				case "spoken":
					appendCloseMessage(t, f.transcript, map[string]string{"type": "message", "role": "user", "content": "keep my chat"})
				case "unreadable":
					if err := os.WriteFile(f.transcript, []byte("{"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "work", "trees", "artifacts":
					path := filepath.Join(f.dir, kept)
					if kept == "artifacts" {
						path = f.artifacts
					}
					if err := os.MkdirAll(path, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(path, "kept"), []byte("kept"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "turns":
					if err := os.WriteFile(filepath.Join(f.dir, cellstore.TurnsPath), []byte("sealed turns"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "tasks":
					if err := os.WriteFile((session.Place{Dir: f.dir}).Tasks(), []byte("saved tasks"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "crew":
					if err := teamstore.Update(f.b.crewProfile(), func(file *teamstore.File) error {
						id := teamstore.NewID()
						file.Teams = append(file.Teams, teamstore.Team{ID: id, Name: "kept", Made: f.clock.now()})
						return file.AddMember(id, teamstore.Member{Key: f.transcript, File: f.transcript, Where: f.workspace})
					}); err != nil {
						t.Fatal(err)
					}
				}
				sibling := filepath.Join(filepath.Dir(f.dir), "sibling")
				if err := os.MkdirAll(sibling, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sibling, "kept"), []byte("sibling"), 0o600); err != nil {
					t.Fatal(err)
				}
				before, beside := appFolderSnapshot(t, f.dir), appFolderSnapshot(t, sibling)
				f.a.minted = true
				if gone := f.b.reapBlank(f.a); gone != (kept == "blank") {
					t.Fatalf("reap %s = %v", kept, gone)
				}
				if kept == "blank" {
					if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
						t.Fatalf("blank chat folder remains: %v", err)
					}
					f.b.locked(func() {
						if f.b.agents[f.id] != nil {
							t.Error("reaped chat remains registered")
						}
					})
				} else if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
					t.Fatal("reaping changed a protected chat")
				}
				if after := appFolderSnapshot(t, sibling); !reflect.DeepEqual(after, beside) {
					t.Fatal("reaping changed a sibling")
				}
			}, layouts...)
		})
	}
}

// F6 keeps takeBack's metadata-only allowlist and root boundary. A cell's
// .cell directory is outside that allowlist, so it is retained in full; a
// legacy mint with only meta.json is removed. F2 preserves that legacy rule.
func TestAppFolderTakeBack(t *testing.T) {
	for _, name := range []string{"mint-only", "used", "metadata-only-state"} {
		t.Run(name, func(t *testing.T) {
			layouts := []string{"cell", "legacy"}
			if name == "metadata-only-state" {
				layouts = []string{"cell"}
			}
			eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
				if name != "used" {
					for _, path := range []string{filepath.Join(f.dir, "work"), f.artifacts, f.transcript} {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
				}
				if name == "metadata-only-state" {
					for _, path := range []string{filepath.Join(f.dir, cell.EnvPath), filepath.Join(f.dir, cell.StateDir, "session.json")} {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := appFolderSnapshot(t, f.dir)
				f.b.takeBack(f.transcript)
				cellChat := filepath.Base(filepath.Dir(f.transcript)) == cell.StateDir
				if !cellChat && name == "mint-only" {
					if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
						t.Fatalf("unused legacy mint remains: %v", err)
					}
				} else if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
					t.Fatal("takeBack changed a folder outside its allowlist")
				}
				// An equally empty folder outside the places root is never removed.
				outside := t.TempDir()
				writeCloseJSON(t, filepath.Join(outside, "meta.json"), session.Meta{ID: "outside"})
				before = appFolderSnapshot(t, outside)
				f.b.takeBack(filepath.Join(outside, "transcript.jsonl"))
				if after := appFolderSnapshot(t, outside); !reflect.DeepEqual(after, before) {
					t.Fatal("takeBack changed a folder outside the places root")
				}
			}, layouts...)
		})
	}
}

// F6 changes only the owning chat's archive metadata and leaves the cell's
// own meta, truth records and workspace intact. F2/F3 keep the other layouts.
func TestAppFolderArchiveAndReopen(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		meta, err := session.LoadMeta(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		meta.Effort, meta.Approval = "high", "ask"
		if err := session.SaveMeta(f.dir, meta); err != nil {
			t.Fatal(err)
		}
		if old := filepath.Dir(f.transcript); old != f.dir {
			if err := os.WriteFile(filepath.Join(old, "meta.lock"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		before := appFolderSnapshot(t, f.dir)
		for _, archived := range []bool{true, false} {
			if archived {
				err = f.b.Archive([]string{f.id})
			} else {
				_, err = f.b.Reopen(f.id)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := session.LoadMeta(f.dir)
			want := meta
			want.Archived = archived
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("archive %v metadata = %+v, %v; want %+v", archived, got, err, want)
			}
			after := appFolderSnapshot(t, f.dir)
			if raw, ok := after["meta.lock"]; !ok || raw != "" {
				t.Fatal("session metadata lock is missing or was written as data")
			}
			for _, path := range []string{"meta.json", ".cell/session.json", "meta.lock"} {
				delete(after, path)
				delete(before, path)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatal("archiving changed files other than session metadata")
			}
			var marked bool
			if err := f.b.state.read(func(state *appStateFile) { _, marked = state.Archived[f.id] }); err != nil || marked != archived {
				t.Fatalf("app archive mark = %v, %v", marked, err)
			}
		}
	})
}

// F6 reads person activity from the session summary without rewriting any
// chat file, and clears only that chat's recovery guard in the app state.
func TestAppFolderCloseMetaFiles(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		meta, err := session.LoadMeta(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		meta.LastUserAt = f.clock.now()
		if err := session.SaveMeta(f.dir, meta); err != nil {
			t.Fatal(err)
		}
		stamp := f.clock.now().Add(-time.Minute)
		if err := f.b.state.write(func(state *appStateFile) error {
			for _, id := range []string{f.id, "other"} {
				state.CloseResumes[id] = 1
				state.CloseResumedAt[id] = stamp
				state.ClosePending[id] = "pending"
				state.ClosePendingAt[id] = stamp
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		before := appFolderSnapshot(t, f.dir)
		if reset, err := f.b.resetCloseAfterPerson(f.a); err != nil || !reset {
			t.Fatalf("reset close guard = %v, %v", reset, err)
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("reading person activity rewrote chat files")
		}
		if err := f.b.state.read(func(state *appStateFile) {
			if state.CloseResumes[f.id] != 0 || !state.CloseResumedAt[f.id].IsZero() || state.ClosePending[f.id] != "" || !state.ClosePendingAt[f.id].IsZero() {
				t.Error("the chat's close guard remains")
			}
			if state.CloseResumes["other"] != 1 || !state.CloseResumedAt["other"].Equal(stamp) || state.ClosePending["other"] != "pending" || !state.ClosePendingAt["other"].Equal(stamp) {
				t.Error("another chat's close guard changed")
			}
		}); err != nil {
			t.Fatal(err)
		}
	})
}

// F5/F6 resolve old and corrected artifact spellings to the same truth
// directory. Removing an unsent reference deletes only its private snapshot.
func TestAppFolderReferenceFiles(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		old := (session.Place{Dir: filepath.Dir(f.transcript), Owned: true}).Artifacts()
		if old != f.artifacts {
			t.Fatalf("artifact truth moved from %q to %q", old, f.artifacts)
		}
		original := filepath.Join(old, "original.png")
		data := writePNG(t, original, 2, 2)
		source := session.RecordMedia(appMediaIndex(), session.Artifact{Path: original, Session: f.id, Title: "original.png", Kind: "image", Created: f.clock.now()}, session.GeneratedMedia(original, data, "fixture"))
		media, err := f.b.GetMedia(f.id, source)
		if err != nil {
			t.Fatal(err)
		}
		before := appFolderSnapshot(t, f.dir)
		chip, err := f.b.AttachMedia(f.id, source, media.Version)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(filepath.Dir(chip.Path)) != f.artifacts {
			t.Fatalf("reference was saved outside chat artifacts: %s", chip.Path)
		}
		if err := f.b.RemoveAttachment(f.id, chip.ID); err != nil {
			t.Fatal(err)
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("removing a reference changed the source or other chat files")
		}
	})
}

// F1/F5 keep lazy recovery responsive to records already saved beside the
// journal after its folder moves to the owning root. F6 only reads those files.
func TestAppFolderLazyRecoveryFiles(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		run := &appCrewRun{since: f.clock.now(), wakeStamps: map[string]string{}}
		read := appCrewRead{file: &teamstore.File{}}
		if needed := f.b.neededMembers(run, read, false); len(needed) != 0 {
			t.Fatalf("idle chat needs recovery: %+v", needed)
		}
		job := session.JobNotice{ID: 7, Kind: session.JobKindCommand, State: session.JobRunning, Started: f.clock.now()}
		seedRecords(t, filepath.Dir(f.transcript), job)
		before := appFolderSnapshot(t, f.dir)
		needed := f.b.neededMembers(run, read, false)
		if _, ok := needed[f.id]; !ok || len(needed) != 1 {
			t.Fatalf("saved process did not wake recovery: %+v", needed)
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("checking recovery changed chat files")
		}
	})
}

// F6 copies only the chosen deliverable, never overwrites the destination,
// and records Remove and Snapshot without changing the chat's source files.
func TestAppFolderDeliverableChanges(t *testing.T) {
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		path := filepath.Join(f.dir, "work", "notes.md")
		if err := os.WriteFile(path, []byte("kept notes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: f.clock.now(), Origin: session.DeliverOriginAgent, State: session.DeliverDraft, Authored: true}
		if !session.RecordArtifact(appMediaIndex(), session.Artifact{Path: path, Session: f.id, Title: "notes.md", Kind: "markdown", Created: f.clock.now(), Deliver: &fact}) {
			t.Fatal("deliverable was not recorded")
		}
		dest := t.TempDir()
		if err := os.WriteFile(filepath.Join(dest, "notes.md"), []byte("my notes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, destination := appFolderSnapshot(t, f.dir), appFolderSnapshot(t, dest)
		if _, err := f.b.KeepDeliverable(f.id, fact.ID, dest, "notes.md", ""); !hasCode(err, appwire.CodeConflict) {
			t.Fatalf("existing destination was not refused: %v", err)
		}
		if after := appFolderSnapshot(t, dest); !reflect.DeepEqual(after, destination) {
			t.Fatal("refused copy changed the destination")
		}
		kept, err := f.b.KeepDeliverable(f.id, fact.ID, dest, "copy.md", "")
		if err != nil || kept.State != session.DeliverKept || kept.Kept == nil || kept.Kept.Path != filepath.Join(canon(dest), "copy.md") {
			t.Fatalf("kept deliverable = %+v, %v", kept, err)
		}
		destination["copy.md"] = "kept notes\n"
		if after := appFolderSnapshot(t, dest); !reflect.DeepEqual(after, destination) {
			t.Fatal("copy changed files other than its named destination")
		}
		if err := f.b.RemoveDeliverable(f.id, fact.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.b.GetDeliverable(f.id, fact.ID); !hasCode(err, appwire.CodeNotFound) {
			t.Fatalf("removed deliverable remains listed: %v", err)
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("keeping or removing a deliverable changed chat files")
		}
		shot := filepath.Join(f.artifacts, "snapshot.png")
		writePNG(t, shot, 4, 4)
		before = appFolderSnapshot(t, f.dir)
		one, err := f.b.SnapshotDeliverable(f.id, shot, "http://localhost:3000/", "")
		if err != nil {
			t.Fatal(err)
		}
		two, err := f.b.SnapshotDeliverable(f.id, shot, "http://localhost:3000/", "")
		if err != nil || two.ID != one.ID {
			t.Fatalf("snapshot was not idempotent: %+v, %v", two, err)
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("registering a snapshot rewrote chat files")
		}
	})
}

// F1/F5 keep task records and tombstones at their existing truth paths. F6
// rechecks them before landing, changes only the planned checkout, and keeps
// every file in the owning chat intact. These are real temporary git repos.
func TestAppFolderBranchLandFiles(t *testing.T) {
	// A land refuses a repository whose git config runs a filter program, and
	// the runner's global config carries one. The land strips GIT_* from its
	// git, so only a home of the test's own keeps that config out.
	gitHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(gitHome, ".gitconfig"), []byte("[user]\n\tname = t\n\temail = t@t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", gitHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(gitHome, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	eachAppFolder(t, func(t *testing.T, f appFolderFixture) {
		landGit(t, f.workspace, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(f.workspace, "a.txt"), []byte("base\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		landGit(t, f.workspace, "add", "a.txt")
		landGit(t, f.workspace, "commit", "-q", "-m", "base")
		landGit(t, f.workspace, "checkout", "-q", "-b", "task/paint-abc123")
		if err := os.WriteFile(filepath.Join(f.workspace, "b.txt"), []byte("task\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		landGit(t, f.workspace, "add", "b.txt")
		landGit(t, f.workspace, "commit", "-q", "-m", "task")
		landGit(t, f.workspace, "checkout", "-q", "main")
		place := session.Place{Dir: f.dir}
		old := (session.Place{Dir: filepath.Dir(f.transcript)}).Tasks()
		if old != place.Tasks() {
			t.Fatalf("task truth moved from %q to %q", old, place.Tasks())
		}
		doc := `{"type":"tasks","version":1,"seq":1,"nodes":[],"runs":[{"id":1,"title":"Paint","state":"done","branch":"task/paint-abc123"}]}`
		if err := os.WriteFile(old, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		branches, err := f.b.TaskBranches(appwire.BranchesQuery{Agent: f.id})
		if err != nil || len(branches.Branches) != 1 || branches.Branches[0].Name != "task/paint-abc123" {
			t.Fatalf("saved task branches = %+v, %v", branches, err)
		}
		head := landGit(t, f.workspace, "rev-parse", "HEAD")
		plan, err := f.b.PlanBranchLand(appwire.BranchLandPlanQuery{Agent: f.id, Branch: "task/paint-abc123"})
		if err != nil {
			t.Fatal(err)
		}
		deleted := filepath.Join(filepath.Dir(f.transcript), ".deleted-tasks.json")
		writeCloseJSON(t, deleted, map[string]bool{"1": true})
		before := appFolderSnapshot(t, f.dir)
		branches, err = f.b.TaskBranches(appwire.BranchesQuery{Agent: f.id})
		if err != nil || len(branches.Branches) != 0 {
			t.Fatalf("deleted task branches = %+v, %v", branches, err)
		}
		if _, err := f.b.LandBranch(appwire.BranchLandQuery{Agent: f.id, Plan: plan.Plan, Revision: plan.Revision}); !hasCode(err, appwire.CodeConflict) {
			t.Fatalf("landing a deleted task was not refused: %v", err)
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) || landGit(t, f.workspace, "rev-parse", "HEAD") != head {
			t.Fatal("refused land changed the chat or checkout")
		}
		if err := os.Remove(deleted); err != nil {
			t.Fatal(err)
		}
		before = appFolderSnapshot(t, f.dir)
		plan, err = f.b.PlanBranchLand(appwire.BranchLandPlanQuery{Agent: f.id, Branch: "task/paint-abc123"})
		if err != nil {
			t.Fatal(err)
		}
		landed, err := f.b.LandBranch(appwire.BranchLandQuery{Agent: f.id, Plan: plan.Plan, Revision: plan.Revision})
		if err != nil || landed.HeadBefore != head || landed.HeadAfter != plan.BranchHead || landGit(t, f.workspace, "rev-parse", "HEAD") != plan.BranchHead {
			t.Fatalf("landed branch = %+v, %v", landed, err)
		}
		for name, want := range map[string]string{"a.txt": "base\n", "b.txt": "task\n"} {
			if raw, err := os.ReadFile(filepath.Join(f.workspace, name)); err != nil || string(raw) != want {
				t.Fatalf("checkout file %s = %q, %v", name, raw, err)
			}
		}
		if after := appFolderSnapshot(t, f.dir); !reflect.DeepEqual(after, before) {
			t.Fatal("landing changed files in the chat folder")
		}
	})
}

func appFolderSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if entry.IsDir() {
			files[key+"/"] = ""
			return nil
		}
		raw, err := os.ReadFile(path)
		files[key] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
