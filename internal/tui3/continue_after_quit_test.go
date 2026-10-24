package tui3

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"strings"
	"testing"
	"time"
)

type safeContinueTaker struct {
	fakeTaker
	clean bool
}

func (s *safeContinueTaker) SafeToContinue(context.Context, string, string, bool) (bool, error) {
	return s.clean, nil
}

// TestC31QuitContinuesWithoutAKey proves C31.1/C31.3: only a recent released
// chat on bare startup is eligible; busy and asleep chats never move themselves.
func TestC31QuitContinuesWithoutAKey(t *testing.T) {
	for _, status := range []chatlist.Status{chatlist.Idle, chatlist.Off, chatlist.Running} {
		t.Run(string(status), func(t *testing.T) {
			a := leftOffApp(&safeContinueTaker{clean: true})
			a.autoContinue = true
			row := leftOffRow("chat", "laptop", status, time.Minute)
			row.Released = status == chatlist.Idle
			a.machineRead.rows = []chatlist.Row{row}
			cmd := a.continueAfterQuit(nil)
			if status != chatlist.Idle {
				if cmd != nil {
					t.Fatal("unreleased chat moved automatically")
				}
				return
			}
			if cmd == nil {
				t.Fatal("released chat requires a key")
			}
			checked := cmd().(continuationChecked)
			if !checked.clean || !checked.automatic {
				t.Fatal("automatic check lost its intent")
			}
			if cmd := a.continuationReady(checked); cmd == nil {
				t.Fatal("clean released chat was not taken")
			}
		})
	}
}

// TestC31AsleepIsOneKeyUnlessLocalWorkNeedsAChoice proves C31.2: alt+c starts
// the move immediately when clean, while local edits retain the branch card.
func TestC31AsleepIsOneKeyUnlessLocalWorkNeedsAChoice(t *testing.T) {
	for _, clean := range []bool{true, false} {
		a := leftOffApp(&safeContinueTaker{clean: clean})
		row := leftOffRow("chat", "laptop", chatlist.Off, time.Minute)
		a.leftOff.row = &row
		cmd, took := a.resumeKeyHandler(resumeKey)
		if !took || cmd == nil {
			t.Fatal("one key did not check the move")
		}
		next := a.continuationReady(cmd().(continuationChecked))
		_, card := a.homeAsking()
		if clean && (next == nil || card) {
			t.Fatal("clean continuation asked a second question")
		}
		if !clean && (next != nil || !card) {
			t.Fatal("local edits were not offered as a choice")
		}
	}
}

// TestC31TypingCancelsLateAutomaticContinue proves C31.3: an asynchronous
// startup answer cannot navigate after the person has begun using this window.
func TestC31TypingCancelsLateAutomaticContinue(t *testing.T) {
	a := leftOffApp(&safeContinueTaker{clean: true})
	a.autoContinue = true
	row := leftOffRow("chat", "laptop", chatlist.Idle, time.Minute)
	row.Released = true
	a.machineRead.rows = []chatlist.Row{row}
	cmd := a.continueAfterQuit(nil)
	a.continueGeneration++
	if next := a.continuationReady(cmd().(continuationChecked)); next != nil {
		t.Fatal("late automatic move ignored the person's input")
	}
}

// TestC31NewestQuitWins proves C31.1: the most recently released conversation
// is continued, even when its last saved turn is older than another chat's.
func TestC31NewestQuitWins(t *testing.T) {
	a := leftOffApp(&safeContinueTaker{clean: true})
	a.autoContinue = true
	olderTurn := leftOffRow("quit-last", "laptop", chatlist.Idle, 48*time.Hour)
	olderTurn.Released, olderTurn.ReleasedAgo = true, time.Minute
	newerTurn := leftOffRow("quit-first", "desktop", chatlist.Idle, time.Hour)
	newerTurn.Released, newerTurn.ReleasedAgo = true, 2*time.Hour
	a.machineRead.rows = []chatlist.Row{newerTurn, olderTurn}
	cmd := a.continueAfterQuit(nil)
	if cmd == nil || cmd().(continuationChecked).row.Cell != "quit-last" {
		t.Fatal("startup chose the last turn instead of the last quit")
	}
}

// TestC31FailedListingKeepsTheAccountSentence proves C31.4: failure leaves
// Home's existing removal explanation visible beside the failed listing.
func TestC31FailedListingKeepsTheAccountSentence(t *testing.T) {
	for _, onHome := range []bool{false, true} {
		a := leftOffApp(&safeContinueTaker{clean: true})
		if onHome {
			_ = a.openHome()
		} else {
			_ = a.showPage(pageNone)
		}
		const line = "This computer was removed from your chats."
		a.outsideDesk = NewNotices()
		a.outsideDesk.SayHomeOnce(line)
		a.saidNotices()
		a.autoContinue = true
		a.machineRead.down = true
		_ = a.continueAfterQuit(errors.New("device removed"))
		if !strings.Contains(homeText(a), line) {
			t.Fatalf("failed startup listing erased account explanation; began on Home=%v", onHome)
		}
	}
}
