package tui3

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestSendContinuationKeepsTheWholeComposer(t *testing.T) {
	for _, outcome := range []string{"continue", "leave", "failed", "lost race"} {
		t.Run(outcome, func(t *testing.T) {
			src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
			taker := &fakeTaker{}
			a, old := heldLab(t, src, taker)
			a.closeHome()
			originalFile := a.file
			taker.taken = Taken{Transcript: originalFile}
			if outcome == "failed" {
				taker.err = errors.New("take failed")
			}
			if outcome == "lost race" {
				taker.err = directory.ErrLeaseHeld
			}
			taker.on = func(string) {
				if taker.err == nil {
					src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here })
				}
			}
			a.input.setText("  keep these words\nexactly as typed  ")
			a.input.cursor = 7
			a.chips = []chip{{path: "/tmp/l2lfix-picture.png"}, {path: "/tmp/l2lfix-notes.txt", file: true}}
			a.pastes = []pasteChip{{n: 1, text: "a pasted document\nwith several\nunchanged lines"}}
			want := a.liveComposer()
			drive(t, a, key("enter"))
			if _, card := a.homeAsking(); !card {
				t.Fatal("send did not raise the continuation door")
			}
			if outcome == "leave" {
				drive(t, a, key("2"), key("enter"))
			} else {
				confirm(t, a)
			}
			got := a.liveComposer()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("composer after %s = %+v, want %+v", outcome, got, want)
			}
			if len(old.sent) != 0 {
				t.Fatalf("held words were sent: %v", old.sent)
			}
			if outcome == "continue" {
				if a.at(pageHome) || a.file != originalFile || a.agent == old {
					t.Fatal("take did not reopen the arrived chat")
				}
				arrived := a.agent.(*switchAgent).fakeAgent
				if len(arrived.sent) != 0 {
					t.Fatal("take sent without a second enter")
				}
				a.chips = nil
				drive(t, a, key("enter"))
				if len(arrived.sent) != 1 {
					t.Fatalf("second enter did not send: %v", arrived.sent)
				}
			}
		})
	}
}
