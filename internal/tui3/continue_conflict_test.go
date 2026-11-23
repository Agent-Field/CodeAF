package tui3

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestALostRaceKeepsItsRetryCardAndDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	src.row.Fence = 3
	taker := &fakeTaker{err: directory.ErrLeaseHeld}
	a, _ := heldLab(t, src, taker)
	a.closeHome()
	a.input.setText(" keep " + pasteToken(1, 2) + " these words")
	a.pastes = []pasteChip{{n: 1, text: "whole\npaste"}}
	a.chips = []chip{{path: "/tmp/attachment", file: true}}
	want := a.mainComposer()
	drive(t, a, key("enter"))
	if _, ok := a.homeAsking(); !ok {
		t.Fatalf("send did not raise a card: %s notes=%v", homeText(a), noteTexts(a))
	}
	taker.on = func(string) {
		src.change(func(r *chatlist.Row) { r.Device, r.DeviceID, r.Fence = "desktop", "dev-desktop", 8 })
	}
	confirm(t, a)
	q, ok := a.homeAsking()
	if !ok || q.question.Options[0].Label != continueRetry || q.question.Options[1].Label != continueStay {
		t.Fatalf("missing retry card: %+v", q)
	}
	if !strings.Contains(homeText(a), lostRaceWord) || q.question.Subject.Name != src.row.Title {
		t.Fatal(homeText(a))
	}
	if !reflect.DeepEqual(a.mainComposer(), want) {
		t.Fatal("draft changed")
	}
}

func TestARefreshedHoldAsksAgainInsteadOfBlamingAnotherDevice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	row := leftOffRow("c-off", "tower", chatlist.Running, time.Minute)
	row.Fence = 3
	src := &continuationListing{row: row}
	taker := &conflictTaker{fakeTaker: fakeTaker{err: directory.ErrLeaseHeld}}
	a := continueHome(t, taker, src, 200)
	standOn(t, a, row.Cell)
	src.change(func(r *chatlist.Row) { r.Fence = 4 })
	confirm(t, a)
	q, ok := a.homeAsking()
	if strings.Contains(homeText(a), lostRaceWord) {
		t.Fatalf("blamed another device for the same holder reopening:\n%s", homeText(a))
	}
	if !ok {
		t.Fatalf("no refreshed card:\n%s", homeText(a))
	}
	if a.home.msg != "tower picked this chat up again; press enter to continue it here" {
		t.Fatal(a.home.msg)
	}
	if len(taker.seen) != 1 {
		t.Fatal(taker.seen)
	}
	taker.err = nil
	confirm(t, a)
	if len(taker.seen) != 2 || taker.seen[1].Device != row.DeviceID || taker.seen[1].Fence != 4 {
		t.Fatalf("new consent not captured: %+v card=%+v", taker.seen, q)
	}
}

// conflictTaker records the consent the person actually confirmed on each card.
type conflictTaker struct {
	fakeTaker
	seen []directory.LeaseExpectation
}

func (f *conflictTaker) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (Taken, error) {
	f.seen = append(f.seen, *seen)
	return f.fakeTaker.Take(ctx, id)
}

func TestALostRaceFromASilentReturnKeepsItsCardAndDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: chatlist.Row{Cell: heldCell, Title: "returning chat", Status: chatlist.Off, Mine: true, DeviceID: "dev-here", Fence: 2, Head: "saved"}}
	taker := &conflictTaker{fakeTaker: fakeTaker{err: directory.ErrLeaseHeld}}
	a, old := heldLab(t, src, taker)
	a.closeHome()
	root := session.DirOf(a.file)
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	taker.taken = Taken{Fence: 5, Head: "saved", Transcript: a.file}
	taker.on = func(string) {
		src.change(func(r *chatlist.Row) {
			r.Mine, r.Status, r.DeviceID, r.Device, r.Fence = false, chatlist.Running, "dev-away", "tower", 4
		})
	}
	a.input.setText("keep " + pasteToken(1, 2) + " these words")
	a.pastes = []pasteChip{{n: 1, text: "whole\npaste"}}
	want := a.mainComposer()
	drive(t, a, key("enter"))
	q, ok := a.homeAsking()
	if !a.at(pageHome) || !ok || q.question.Options[0].Label != continueRetry {
		t.Fatalf("silent refusal has no card: %s", homeText(a))
	}
	if len(old.sent) != 0 || !reflect.DeepEqual(a.mainComposer(), want) {
		t.Fatal("refusal spent the draft")
	}
	taker.err = nil
	taker.on = func(string) { src.change(func(r *chatlist.Row) { r.Mine, r.Status, r.Fence = true, chatlist.Here, 5 }) }
	confirm(t, a)
	if len(taker.seen) != 2 || taker.seen[1].Device != "dev-away" || taker.seen[1].Fence != 4 {
		t.Fatal(taker.seen)
	}
	arrived, ok := a.agent.(*switchAgent)
	if !ok || len(arrived.sent) != 0 || !reflect.DeepEqual(a.mainComposer(), want) {
		t.Fatal("retry did not retain second-enter policy")
	}
}
