package tui3

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// These receipts are lost AFTER the engine records the correction. Reopening
// must ask about that event, rather than creating another delivery of its text.
func TestAClosedUnansweredCorrectionReopensUnderItsOriginalName(t *testing.T) {
	for _, before := range []bool{false, true} {
		name := "reopen after answer"
		if before {
			name = "reopen before answer"
		}
		t.Run(name, func(t *testing.T) {
			a, engine := namedEngine(t, true)
			file := a.file
			engine.loseAck = 2
			clickRail(t, a, 0)
			a.pastes = []pasteChip{{n: 1, text: "the full attached document\nsecond line"}}
			cmd := typeSteer(t, a, "make it CSV "+pasteToken(1, 2))
			var send *steerSend
			for _, one := range a.outbox.flying {
				send = one
			}
			if send == nil {
				t.Fatal("no send crossed")
			}
			if send == nil {
				t.Fatal("no send crossed")
			}
			want := send.snapshot(draftSendUnanswered)
			answers := runCmd(cmd)
			if len(engine.delivered()) != 1 {
				t.Fatal("fixture did not accept before losing its receipts")
			}
			a.closeRoom()
			a.forgetSteerOwner(send.at.owner)
			if err := commitDropped(a.draftFile); err != nil {
				t.Fatal(err)
			}
			a.file = file + ".other"
			a.composers = nil
			a.restoreDraft()
			reopenOwner := func() {
				a.closeRoom()
				a.file = file
				a.composers = nil
				a.restoreDraft()
				a.restoreSentDrafts()
				clickRail(t, a, 0)
			}
			if before {
				reopenOwner()
			}
			drive(t, a, answers...)
			if noted(a, steerKeptNowhere) {
				t.Fatal("an unanswered correction was called undelivered")
			}
			if !before {
				if !noted(a, steerUnsureClosed) {
					t.Fatal("closed owner has no truthful uncertainty notice")
				}
				if a.input.String() != "" || a.unsureSteer() != nil {
					t.Fatal("another conversation received the correction")
				}
				reopenOwner()
			}
			back := a.unsureSteer()
			if back == nil || !back.lost || back.gone {
				t.Fatalf("reopened send = %+v", back)
			}
			if got := back.snapshot(draftSendUnanswered); !reflect.DeepEqual(got, want) {
				t.Fatalf("snapshot changed: got %#v want %#v", got, want)
			}
			if a.input.String() != "" {
				t.Fatal("uncertain words became a new draft")
			}
			deliver(t, a, a.retrySteer(back.at))
			if len(engine.delivered()) != 1 || a.unsureSteer() != nil {
				t.Fatal("retry duplicated or failed to resolve the original delivery")
			}
			for _, from := range engine.asked() {
				if from.Scope != send.from.Scope || from.Seq != send.from.Seq || from.Conversation != file {
					t.Fatalf("retry changed identity: %+v", from)
				}
			}
		})
	}
}

func TestAClosedCorrectionSurvivesRestartWithoutRestoringPlainDrafts(t *testing.T) {
	for _, front := range []bool{false, true} {
		name := "close kept"
		if front {
			name = "close front"
		}
		t.Run(name, func(t *testing.T) {
			a, engine := namedEngine(t, true)
			original := a.front()
			engine.loseAck = 2
			clickRail(t, a, 0)
			a.pastes = []pasteChip{{n: 2, text: "full document\nkept over restart"}}
			cmd := typeSteer(t, a, "make it CSV "+pasteToken(2, 2))
			answers := runCmd(cmd)
			var send *steerSend
			for _, one := range a.outbox.flying {
				send = one
			}
			if send == nil {
				t.Fatal("no send crossed")
			}
			want := send.snapshot(draftSendUnanswered)
			a.input.setText("discard this ordinary draft")
			a.writeDraftsNow(a.leavingDraft())
			stowOne(t, a, &switchAgent{fakeAgent: &fakeAgent{model: "m"}}, original.SessionFile+".other")
			if front {
				cmd, ok := a.bringForward(original.SessionFile)
				if !ok {
					t.Fatal("original owner did not reopen")
				}
				drain(t, a, cmd)
				cmd, ok = a.closeFront()
				if !ok {
					t.Fatal("original owner did not close")
				}
				drain(t, a, cmd)
			} else if !a.closeKept(original.SessionFile) {
				t.Fatal("kept owner did not close")
			}
			drive(t, a, answers...)
			next, _, _ := roomApp(t)
			next.workspace, next.file, next.draftFile = original.Workspace, original.SessionFile, original.DraftFile
			next.host, next.agent = a.host, engine
			next.restoreDraft()
			next.restoreSentDrafts()
			clickRail(t, next, 0)
			back := next.unsureSteer()
			if back == nil {
				t.Fatal("closed correction disappeared across restart")
			}
			if got := back.snapshot(draftSendUnanswered); !reflect.DeepEqual(got, want) {
				t.Fatalf("restart changed payload: got %#v want %#v", got, want)
			}
			if next.input.String() != "" {
				t.Fatal("close resurrected an ordinary draft")
			}
			deliver(t, next, next.retrySteer(back.at))
			if len(engine.delivered()) != 1 {
				t.Fatal("restart retry duplicated the accepted correction")
			}
		})
	}
}

func TestAClosedLostCorrectionStaysUncertainAfterARefusal(t *testing.T) {
	a, engine := namedEngine(t, true)
	engine.loseAck = 2
	clickRail(t, a, 0)
	deliver(t, a, typeSteer(t, a, "make it CSV"))
	send := a.unsureSteer()
	cmd := a.retrySteer(send.at)
	a.closeRoom()
	a.forgetSteerOwner(send.at.owner)
	a.file += ".other"
	engine.roomFake.steerErr = session.ErrNotThatConversation
	deliver(t, a, cmd)
	held := a.outbox.unsure[send.at]
	if len(held) != 1 || held[0] != send || !send.lost {
		t.Fatal("later refusal erased the unknown first delivery")
	}
	if noted(a, steerKeptNowhere) {
		t.Fatal("unknown delivery was called a refusal")
	}
}

// The automatic second ask is part of the same crossing, so its refusal must
// not erase the first answer's uncertainty before the surface receives it.
func TestAClosedCrossingKeepsTheLostFirstAnswerWhenItsAutomaticRetryRefuses(t *testing.T) {
	a, engine := namedEngine(t, true)
	engine.loseAck, engine.afterAckLoss = 1, session.ErrNotThatConversation
	clickRail(t, a, 0)
	cmd := typeSteer(t, a, "make it CSV")
	a.closeRoom()
	a.forgetSteerOwner(a.steerOwner())
	a.file += ".other"
	deliver(t, a, cmd)
	if len(engine.delivered()) != 1 {
		t.Fatal("fixture did not accept the first crossing")
	}
	if len(a.outbox.unsure) != 1 || noted(a, steerKeptNowhere) {
		t.Fatal("the retry refusal erased the first crossing's uncertainty")
	}
}

func TestARestoredCorrectionBoundsItsCaretWithoutChangingItsWords(t *testing.T) {
	for _, caret := range []int{-10, 1000} {
		a, _ := namedEngine(t, true)
		a.restoreSteerSnapshots(a.file, 7, []outboxSnapshot{{scope: "old", seq: 1, state: draftSendUnanswered, line: "héllo", words: "héllo", caret: caret}})
		held := a.outbox.unsure[steerAddress{owner: a.steerOwner(), task: 7}]
		if len(held) != 1 {
			t.Fatal("snapshot was not restored")
		}
		snap := held[0].snapshot(draftSendUnanswered)
		want := 0
		if caret > 0 {
			want = len([]rune("héllo"))
		}
		if snap.line != "héllo" || snap.caret != want {
			t.Fatalf("restored snapshot = %+v", snap)
		}
	}
}
