package tui3

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestASendReadingPreservesOutstandingHomeProgress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	for _, answer := range []error{nil, directory.ErrUnreachable, context.DeadlineExceeded} {
		t.Run(map[error]string{nil: "success", directory.ErrUnreachable: "unreachable", context.DeadlineExceeded: "deadline"}[answer], func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			keepSurfaceWorkersInTest(t, a)
			drain(t, a, a.openHome())
			src := &slowRelayListing{entered: make(chan context.Context, 1), answer: make(chan error, 1)}
			a.machines = src
			commands := a.askChats()().(tea.BatchMsg)
			result := make(chan tea.Msg, 1)
			go func() { result <- commands[0]() }()
			<-src.entered
			drive(t, a, waitOut(commands[1]))
			sent := false
			drain(t, a, a.afterOwnershipRead(ownershipReadMsg{file: a.file, draft: a.input.String(), reading: homeMachinesMsg{readWithin: sendOwnershipTimeout}, resume: func() tea.Cmd { sent = true; return nil }}))
			if !sent || !a.machinesAsking || !strings.Contains(homeText(a), "waiting for relay.example; still checking") {
				t.Errorf("send reading dropped outstanding Home progress: %s", homeText(a))
			}
			src.answer <- answer
			drive(t, a, <-result)
			if answer == nil {
				if strings.Contains(homeText(a), "still checking") {
					t.Fatal("completed listing retained progress")
				}
			} else {
				want := "relay.example is unreachable; still trying"
				if answer == context.DeadlineExceeded {
					want = "relay.example did not answer in 25s; still trying"
				}
				if !strings.Contains(homeText(a), want) {
					t.Fatal("listing failure lost its host: ", homeText(a))
				}
			}
		})
	}
}

type zeroRenewalFactsWriter struct{ *startingSendAgent }

func (*zeroRenewalFactsWriter) ReasoningLevels() map[string]string { return nil }

func TestDisplacementEndsAWriterWaitWithZeroRenewalFacts(t *testing.T) {
	for _, notice := range []bool{false, true} {
		t.Run(map[bool]string{false: "listing", true: "notice"}[notice], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, _, rows := intentRig(t)
				keepSurfaceWorkersInTest(t, a)
				a.closeHome()
				writer := &zeroRenewalFactsWriter{&startingSendAgent{fakeAgent: &fakeAgent{model: "m"}, ready: make(chan struct{}), observed: make(chan struct{}, 1)}}
				a.agent = writer
				a.machines = chatlist.Static{}
				id := filepath.Base(session.DirOf(a.file))
				a.machineRead.sendHolds = map[string]sendHold{id: {fence: 4, awaitingWriter: true}}
				a.input.setText("keep " + pasteToken(1, 2) + " these words")
				a.pastes = []pasteChip{{n: 1, text: "whole\npaste"}}
				a.chips = []chip{{path: "/tmp/kept", file: true}}
				want := a.mainComposer()
				// A real viewer's (zero, 0, false) is omitted by FactsOf, so
				// writer startup and displacement have identical renewal facts.
				if session.FactsOf(writer).Renewal != nil {
					t.Fatal("fixture does not match the drive's viewer facts")
				}
				cmd := stepKeyNoDrain(t, a, "enter")
				result := make(chan []tea.Msg, 1)
				go func() { result <- collectBatchMsgs(cmd) }()
				<-writer.observed
				if notice {
					a.note(chatlist.Superseded("tower"))
				} else {
					row := rows[0]
					row.Cell, row.Status, row.Released, row.Device, row.Fence = id, chatlist.Running, false, "tower", 5
					drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{row}}))
				}
				synctest.Wait()
				select {
				case messages := <-result:
					for _, msg := range messages {
						drive(t, a, msg)
					}
				default:
					t.Error("displaced writer still waits for its startup timeout")
					// The fake clock finishes the old timeout without wall time.
					for _, msg := range <-result {
						drive(t, a, msg)
					}
				}
				if !reflect.DeepEqual(a.mainComposer(), want) || len(writer.sent) != 0 {
					t.Fatal("displacement spent or changed the submitted message")
				}
				notes := strings.Join(noteTexts(a), "\n")
				if !strings.Contains(notes, chatlist.Superseded("tower")) || strings.Contains(notes, "has not answered") {
					t.Fatal("displacement blamed the relay: ", notes)
				}
			})
		})
	}
}
