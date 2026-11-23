package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// C22 (contract C1.1 and C1.2 of the pass-through follow-ups): a file that was
// edited IN PLACE on B, by something other than a tool call codeaf ran, is in the
// chat A takes back. The hand test of 2026-11-19 lost it: B's next turn ran no
// tool that changed anything, so the engine was promised that nothing else had
// moved, and an append that left the folder's own modified time where it was
// never reached the turn. A's folder came back with the file as it was.
//
// The turn is the commonest one: the model answers and runs no tool. The
// recorder then seals the transcript alone, and a seal with no call behind it
// promises the engine that nothing in the folder changed ([cellstore.TurnInfo]),
// so only the capture index's own look at the folder stands between an edit made
// in place and the old bytes. (A scripted turn that runs a shell command promises
// nothing and walks the whole tree, which is why the other tests never saw it.)
func TestLiveTransferC22AnInPlaceEditOnBTravelsBackToA(t *testing.T) {
	for _, road := range []string{"live transfer", "storage approved"} {
		t.Run(road, func(t *testing.T) {
			r := newLiveRig(t)
			if road == "storage approved" {
				r.approveStorage()
			}
			a := liveChat(t, r)
			mustNil(t, a.drive.Close(context.Background()))
			took := liveTake(t, r)
			workB := workspaceOf(took.Taken.Cell.Root)
			b := r.openSeated(r.b, r.engB, took.Taken.Cell, workB, nameB, nil, cellstore.WithPipeline(), cellstore.WithWarmCapture())

			headOf := func() string {
				head, err := cellstore.Head(took.Taken.Cell)
				if err != nil || head == nil {
					return ""
				}
				return head.Turn.ID
			}
			// answer is one model answer with no tool in it, and it waits for the
			// seal it must cause: a test that did not see the head move would pass
			// on a turn that never sealed, which proves nothing about the next one.
			answer := func(words string) {
				t.Helper()
				was := headOf()
				// The turn's end, as the surface writes it: a model call was made, its
				// words are in the transcript, and the seat is told the turn is over.
				executor.NoteModelCall(b.seat, executor.ModelCall{Model: "m", Role: "turn", TokensIn: 100, TokensOut: 10, CostMicroUSD: 1000})
				appendTo(t, transcriptOf(b.cell), "{\"type\":\"message\",\"role\":\"assistant\",\"content\":\""+words+"\"}\n")
				executor.Settle(context.Background(), b.seat)
				mustNil(t, executor.Drain(context.Background(), b.seat))
				waitFor(t, "the answer to be sealed on B", func() bool { return headOf() != was })
				waitFor(t, "B's turn to reach the directory", func() bool { return r.directoryHead(r.cell.ID) == headOf() })
			}

			// The first turn on B is the one the engine has no history for, so it
			// looks at the whole folder; the second is the one that is told what
			// changed.
			answer("a first answer on B, with no tool")

			path := filepath.Join(workB, "README.md")
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
			mustNil(t, err)
			_, err = f.WriteString("edited in place on B\n")
			mustNil(t, err)
			mustNil(t, f.Close())

			answer("a second answer on B, with no tool")

			back, err := r.continuerA().Take(context.Background(), r.cell.ID)
			if err != nil {
				t.Fatalf("A could not take the chat back: %v", err)
			}
			if back.Wait != nil {
				mustNil(t, back.Wait(context.Background()))
			}
			got, err := os.ReadFile(filepath.Join(workspaceOf(back.Taken.Cell.Root), "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(string(got), "edited in place on B\n") {
				t.Fatalf("A's README.md after taking the chat back = %q; the edit made in place on B was lost on the move", got)
			}
		})
	}
}
