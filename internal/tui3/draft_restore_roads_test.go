package tui3

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestColdHomeOpenRestoresSavedComposerOnEveryRoad(t *testing.T) {
	for _, door := range []string{"Home", "alt+k"} {
		for _, shared := range []bool{false, true} {
			name := "engine"
			if shared {
				name = "shared"
			}
			t.Run(door+"/"+name, func(t *testing.T) {
				lab := newHomeLab(t)
				work := lab.workspace("draft")
				first := lab.session("-draft", "aaaa000000000001", "first", work, time.Now())
				target := lab.session("-draft", "bbbb000000000001", "saved", work, time.Now())
				path := filepath.Join(t.TempDir(), "draft.txt")
				if err := os.WriteFile(path, []byte("unsent words after restart"), 0600); err != nil {
					t.Fatal(err)
				}
				a := lab.app(first)
				t.Cleanup(a.doorLine.close)
				a.shared = shared
				a.open = func(workspace, file string) (Conversation, error) {
					return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: work, SessionFile: target, DraftFile: path}, nil
				}
				if door == "Home" {
					drain(t, a, a.openHome())
					drain(t, a, a.homeOpenDoor(homeLine{row: session.SessionRow{ProjectDir: work, Transcript: target}}))
				} else {
					drive(t, a, key("alt+k"))
					a.hopSpread(true)
					found := false
					for i, row := range a.hop.rows {
						if row.file == target {
							a.hop.at = i
							found = true
							break
						}
					}
					if !found {
						t.Fatal("saved conversation absent from alt+k")
					}
					drive(t, a, key("enter"))
				}
				if got := a.input.String(); got != "unsent words after restart" {
					t.Fatalf("Home restored %q", got)
				}
			})
		}
	}
}

func TestAnUnreadComposerCannotOverwriteSavedWords(t *testing.T) {
	for _, format := range []string{"plain", "record", "orphan"} {
		for _, text := range []string{"", "different words"} {
			t.Run(format+"/"+text, func(t *testing.T) {
				a := newTestApp(&fakeAgent{model: "m"})
				dir := t.TempDir()
				a.file = filepath.Join(dir, "aaaa000000000001", "transcript.jsonl")
				a.workspace = dir
				a.draftFile = filepath.Join(dir, "draft.txt")
				saved := a.draftFile
				if format == "plain" {
					if err := os.WriteFile(saved, []byte("the saved words"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					saved = draftKeepPath(a.draftFile)
					if format == "orphan" {
						saved = filepath.Join(dir, "draft-orphan-2147483646.json")
					}
					if err := putDraftKeep(saved, a.draftKeepBuild("the saved words")); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.ReadFile(saved)
				if err != nil {
					t.Fatal(err)
				}
				a.input.setText(text)
				drain(t, a, a.saveDraft(a.draftFile))
				after, err := os.ReadFile(saved)
				if err != nil || string(after) != string(before) {
					t.Fatalf("unread composer replaced saved words: %q (%v)", after, err)
				}
				a.restoreDraft()
				if got := a.input.String(); got != "the saved words" {
					t.Fatalf("restore gave %q", got)
				}
				a.input.setText("")
				drain(t, a, a.saveDraft(a.draftFile))
				if got := readDraft(a.draftFile); got != "" {
					t.Fatalf("deliberate clear kept %q", got)
				}
				if keep, how := readDraftKeep(draftKeepPath(a.draftFile)); how == draftKeepFound && !keep.empty() {
					t.Fatalf("deliberate clear kept structured words: %+v", keep)
				}
			})
		}
	}
}
