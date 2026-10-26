package tui3

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestEveryFailedTakeReturnsTheCapturedComposer(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{"lost race", directory.ErrLeaseHeld},
		{"unreachable", directory.ErrUnreachable},
		{"conditional refusal", directory.ErrConditionalTake},
		{"restore cancelled", errors.New("rewind cancelled before mutation: workspace path changed during rewind: .git")},
	} {
		t.Run(failure.name, func(t *testing.T) {
			src := &continuationListing{row: leftOffRow(heldCell, "other", chatlist.Running, time.Minute)}
			taker := &fakeTaker{err: failure.err}
			a, old := heldLab(t, src, taker)
			a.closeHome()
			a.input.setText("  keep " + pasteToken(1, 3) + "\nall these words  ")
			a.input.cursor = 5
			a.pastes = []pasteChip{{n: 1, text: "pasted\nbody\nhere"}}
			a.chips = []chip{{path: "/tmp/l2lfix-picture.png"}, {path: "/tmp/l2lfix-file.txt", file: true}}
			want := a.mainComposer()
			drive(t, a, key("enter"))
			taker.on = func(string) {
				spend(t, a, a.taskEvent(session.Event{Kind: session.EventTakeover, Text: session.TakeoverWord}))
			}
			confirm(t, a)
			if got := a.mainComposer(); !reflect.DeepEqual(got, want) {
				t.Errorf("returned composer = %+v, want %+v", got, want)
			}
			if len(old.sent) != 0 {
				t.Errorf("failure sent words: %v", old.sent)
			}
			if a.at(pageHome) {
				a.closeHome()
			}
			if got := a.liveComposer(); !reflect.DeepEqual(got, want) {
				t.Errorf("visible composer = %+v, want %+v", got, want)
			}
		})
	}
}
