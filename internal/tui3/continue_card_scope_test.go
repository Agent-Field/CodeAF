package tui3

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract J23.3 returns a displaced chat's card to the window that raised it.
func TestDisplacedChatCardEscapeReturnsToItsWindow(t *testing.T) {
	for _, answer := range [][]string{{"esc"}, {"2", "enter"}} {
		t.Run(strings.Join(answer, "+"), func(t *testing.T) {
			src := &continuationListing{row: leftOffRow(heldCell, "other computer", chatlist.Running, time.Minute)}
			a, agent := heldLab(t, src, &fakeTaker{})
			a.closeHome()
			file := a.file
			a.input.setText("  words for A1 only  ")
			a.input.cursor = 5
			want := a.mainComposer()
			drive(t, a, key("enter"))
			if _, card := a.homeAsking(); !card {
				t.Fatal("the displaced window did not raise its card")
			}
			for _, k := range answer {
				drive(t, a, key(k))
			}
			if a.at(pageHome) || a.file != file {
				t.Error("leave it there did not return to A1's window")
			}
			if got := a.mainComposer(); !reflect.DeepEqual(got, want) {
				t.Errorf("A1's composer changed: %+v", got)
			}
			if len(agent.sent) != 0 {
				t.Fatalf("leaving the card sent words: %v", agent.sent)
			}
		})
	}
}

// Contract J23.1 gives a Home card only its selected chat's words and project.
func TestHomeContinueCardBelongsToTheSelectedCopy(t *testing.T) {
	lab := newHomeLab(t)
	work1, work2 := lab.workspace("ws9"), lab.workspace("ws")
	file1 := lab.session("copy-one", heldCell, "A1", work1, time.Now())
	const second = "0000000000000002"
	file2 := lab.session("copy-two", second, "A2", work2, time.Now())
	for file, origin := range map[string]string{file1: work1, file2: work2} {
		meta := session.Meta{ID: filepath.Base(session.DirOf(file)), Title: map[string]string{file1: "A1", file2: "A2"}[file], Workspace: origin, Origin: origin, Owned: true, LastUserAt: time.Now()}
		if err := session.SaveMeta(session.DirOf(file), meta); err != nil {
			t.Fatal(err)
		}
	}
	a := lab.app(file1)
	a.width, a.height = 200, 70
	a.machines = chatlist.Static{
		{Cell: heldCell, Title: "A1", Device: "other computer", Status: chatlist.Running},
		{Cell: second, Title: "A2", Device: "other computer", Status: chatlist.Running},
	}
	a.taker = &fakeTaker{}
	a.input.setText("words belonging only to A1")
	drive(t, a, key("enter"))
	if _, card := a.homeAsking(); !card {
		t.Fatal("A1 did not raise a card")
	}
	if frame := homeText(a); !strings.Contains(frame, "words belonging only to A1") || !strings.Contains(frame, "project: ws9 (copy from another machine)") {
		t.Fatalf("A1's card did not show its own words and project:\n%s", frame)
	}
	drive(t, a, key("esc"))
	drain(t, a, a.openHome())
	standOn(t, a, second)
	frame := homeText(a)
	if strings.Contains(frame, "words belonging only to A1") {
		t.Errorf("A2's card shows A1's words:\n%s", frame)
	}
	if !strings.Contains(frame, "project: ws (copy from another machine)") || strings.Contains(frame, "project: ws9") {
		t.Errorf("A2's card names the wrong project:\n%s", frame)
	}
	drive(t, a, key("esc"))
	if !a.at(pageHome) {
		t.Fatal("a card opened from Home left Home")
	}
	a.closeHome()
	if a.file != file1 || a.input.String() != "words belonging only to A1" {
		t.Fatal("A1 did not keep its unsent words")
	}
}
