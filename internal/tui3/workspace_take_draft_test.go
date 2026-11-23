package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

type draftBindingTaker struct {
	bindingTaker
	taken Taken
	err   error
}

func (f *draftBindingTaker) TakeSeenWithWorkspaceRoots(_ context.Context, _ string, seen *directory.LeaseExpectation, roots map[string]string) (Taken, error) {
	f.calls++
	if f.seen == nil || seen == nil || *f.seen != *seen {
		return Taken{}, directory.ErrConditionalTake
	}
	f.roots = roots
	return f.taken, f.err
}

func TestTakeRootPickerPreservesDraftAndSendsOnlyAfterSuccess(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancel first", "cancel second", "removed"} {
		t.Run(outcome, func(t *testing.T) {
			src := &continuationListing{row: leftOffRow(heldCell, "other", chatlist.Running, time.Minute)}
			fake := &draftBindingTaker{}
			a, _ := heldLab(t, src, fake)
			a.closeHome()
			a.input.setText("  waiting " + pasteToken(1, 2) + " words  ")
			a.input.cursor = 4
			a.pastes = []pasteChip{{n: 1, text: "whole\npaste"}}
			a.chips = []chip{{path: filepath.Join(t.TempDir(), "image.png")}}
			want := a.mainComposer()
			fake.taken = Taken{AlreadyHere: true, Transcript: a.file, Fence: 18}
			if outcome == "failure" {
				fake.err = errors.New("restore failed")
			}
			first, second := t.TempDir(), t.TempDir()
			a.home.world = session.World{Projects: []session.Project{{Path: first}, {Path: second}}}
			cmd := a.takeRemote(src.row)
			msg := cmd().(homeTakenMsg)
			sends := 0
			msg.send = func() tea.Cmd {
				sends++
				if !reflect.DeepEqual(a.mainComposer(), want) {
					t.Error("send ran before the captured draft arrived")
				}
				return nil
			}
			a.putComposer(composerState{})
			spend(t, a, a.tookTakeover(msg))
			if noted(a, continueFails) {
				t.Fatal("a folder choice was reported as a failed take")
			}
			if outcome == "cancel first" {
				drive(t, a, key("esc"))
			} else {
				chooseTakeFolder(t, a, first)
				if outcome == "cancel second" {
					drive(t, a, key("esc"))
				} else {
					if outcome == "removed" {
						a.machineRead.computerRemoved = true
					}
					chooseTakeFolder(t, a, second)
				}
			}
			if !reflect.DeepEqual(a.mainComposer(), want) {
				t.Fatalf("take chooser lost captured composer: got %+v want %+v", a.mainComposer(), want)
			}
			if outcome == "success" {
				if sends != 1 || fake.calls != 2 {
					t.Fatalf("successful retry: sends=%d calls=%d", sends, fake.calls)
				}
			} else if sends != 0 {
				t.Fatal("unsuccessful move sent a message")
			}
			if (outcome == "cancel first" || outcome == "cancel second" || outcome == "removed") && fake.calls != 1 {
				t.Fatal("cancelled or removed computer retried the take")
			}
			if (outcome == "cancel first" || outcome == "cancel second") && noted(a, continueFails) {
				t.Fatal("cancellation reported a failed take")
			}
			if outcome == "failure" && !a.machineRead.sendHolds[heldCell].blocked {
				t.Fatal("failed retry left sending enabled")
			}
			if outcome == "success" || outcome == "failure" {
				if fake.roots["root-app"] != first || fake.roots["root-library"] != second {
					t.Fatalf("root choices changed: %v", fake.roots)
				}
			}
		})
	}
}

func chooseTakeFolder(t *testing.T, a *app, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	msgs := []tea.Msg{tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}}
	for _, char := range path + string(filepath.Separator) {
		msgs = append(msgs, key(string(char)))
	}
	drive(t, a, msgs...)
	drive(t, a, key("enter"))
}

func TestRemovedComputerCannotTakeWithWorkspaceRoots(t *testing.T) {
	fake := &bindingTaker{}
	a := continueHome(t, fake, chatlist.Static{}, 200)
	a.machineRead.computerRemoved = true
	row := chatlist.Row{Cell: "chat", DeviceID: "other", Fence: 17}
	if a.takeRemote(row) != nil || a.takeRemoteWithRoots(row, map[string]string{"root": t.TempDir()}) != nil || fake.calls != 0 {
		t.Fatal("removed computer offered a take")
	}
}
