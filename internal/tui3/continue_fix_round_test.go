package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

type cancelledIntentTaker struct{ *intentTaker }

func (f cancelledIntentTaker) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (Taken, error) {
	if id != "x" {
		return f.intentTaker.TakeSeen(ctx, id, seen)
	}
	f.started <- id
	<-ctx.Done()
	return Taken{}, ctx.Err()
}

func TestAnAbandonedAutomaticTakeBoundsTheWaitingChoice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, f, rows := intentRig(t)
		keepSurfaceWorkersInTest(t, a)
		a.taker = cancelledIntentTaker{f}
		cmd := beginAutomatic(t, a)
		defer a.homeTakeCancel()
		result := make(chan []tea.Msg, 1)
		go func() { result <- collectBatchMsgs(cmd) }()
		<-f.started
		// Time before abandonment does not shorten the person's waiting choice.
		time.Sleep(3 * time.Minute)
		standOn(t, a, rows[1].Cell)
		confirm(t, a)
		time.Sleep(2*time.Minute - time.Nanosecond)
		select {
		case <-result:
			t.Fatal("automatic take cancelled before two minutes after abandonment")
		default:
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		select {
		case messages := <-result:
			for _, msg := range messages {
				drive(t, a, msg)
			}
		default:
			t.Fatal("abandoned automatic take did not cancel after two minutes")
		}
		if a.file != f.files["y"] || a.homeTaking != "" || len(f.released) != 0 {
			t.Fatal("waiting choice did not open after the cancelled take settled")
		}
	})
}

func TestASecondEscapeNavigatesWhileAnAutomaticTakeSettles(t *testing.T) {
	a, f, _ := intentRig(t)
	keepSurfaceWorkersInTest(t, a)
	file := a.file
	cmd := beginAutomatic(t, a)
	drive(t, a, key("esc"))
	if !a.at(pageHome) {
		t.Fatal("first Escape left Home before withdrawing automatic navigation")
	}
	drive(t, a, key("esc"))
	if a.at(pageHome) {
		t.Fatal("second Escape was swallowed by the abandoned automatic take")
	}
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != file || !reflect.DeepEqual(f.released, []string{"x"}) {
		t.Fatal("automatic arrival opened after Escape")
	}
}

func TestInspectingAnotherCardLeavesTheAutomaticArrivalAlone(t *testing.T) {
	for _, answer := range []string{"leave it there", "escape"} {
		t.Run(answer, func(t *testing.T) {
			a, f, rows := intentRig(t)
			keepSurfaceWorkersInTest(t, a)
			cmd := beginAutomatic(t, a)
			standOn(t, a, rows[1].Cell)
			if answer == "escape" {
				drive(t, a, key("esc"))
			} else {
				drive(t, a, key("2"), key("enter"))
			}
			close(f.proceed)
			drain(t, a, cmd)
			if a.file != f.files["x"] || len(f.seen) != 1 || len(f.released) != 0 {
				t.Fatal("inspecting and declining a card withdrew the automatic arrival")
			}
		})
	}
}

func TestAlreadyHereRecoveryRetriesOnlyOnceAndKeepsTheDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: chatlist.Row{Cell: heldCell, Title: "returning chat", Status: chatlist.Here, Mine: true, DeviceID: "dev-here", Fence: 3, Head: "saved"}}
	f := &conflictTaker{fakeTaker: fakeTaker{err: directory.ErrLeaseHeld}}
	a, _ := heldLab(t, src, f)
	keepSurfaceWorkersInTest(t, a)
	root := session.DirOf(a.file)
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	a.input.setText("keep " + pasteToken(1, 2) + " these words")
	a.pastes = []pasteChip{{n: 1, text: "whole\npaste"}}
	a.chips = []chip{{path: "/tmp/kept", file: true}}
	want := a.mainComposer()
	row := src.row
	row.Fence = 2
	cmd := a.takeRemote(row)
	// Deliver only a bounded number of answers, so the old retry loop fails
	// the assertion rather than hanging the test's event-draining fixture.
	for step := 0; cmd != nil && step < 8; step++ {
		var next []tea.Cmd
		for _, msg := range collectBatchMsgs(cmd) {
			switch msg.(type) {
			case homeTakenMsg, continueConflictMsg:
				_, follow := a.Update(msg)
				next = append(next, follow)
			}
		}
		cmd = tea.Batch(next...)
	}
	q, ok := a.homeAsking()
	if len(f.seen) != 2 || !ok || q.question.Reason != continueFails || q.question.Options[0].Label != continueRetry || !reflect.DeepEqual(a.mainComposer(), want) {
		t.Fatalf("already-here disagreement: attempts=%d card=%+v draft=%+v", len(f.seen), q, a.mainComposer())
	}
	for _, seen := range f.seen {
		if seen.Device != "dev-here" || seen.Fence != 2 {
			t.Fatal("silent recovery changed the original consent", f.seen)
		}
	}
	// An explicit retry starts its own bounded recovery attempt.
	f.err = errors.New("stop on the explicit retry")
	confirm(t, a)
	if len(f.seen) != 3 {
		t.Fatal("failure card cannot be retried explicitly")
	}
}
