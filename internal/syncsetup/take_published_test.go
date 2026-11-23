package syncsetup

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestTakeBackDoesNotKeepPublishedOrBookkeepingTurns(t *testing.T) {
	for _, closingSave := range []bool{false, true} {
		t.Run(map[bool]string{false: "published turns", true: "retired closing save"}[closingSave], func(t *testing.T) {
			h := newTwoHomes(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			a := h.openA()
			a.mustSay("first shared reply")
			a.mustSay("second shared reply")
			h.durable(h.cell.ID, h.cell)
			first, err := h.continuerB().Take(ctx, h.cell.ID)
			mustNil(t, err)
			b := h.openOn(h.b, h.engB, first.Taken.Cell, workspaceOf(first.Taken.Cell.Root), nameB)
			b.mustSay("reply on the other computer")
			h.durable(h.cell.ID, first.Taken.Cell)
			mustNil(t, b.drive.Close(ctx))
			mustNil(t, a.drive.Close(ctx))
			if closingSave {
				journal, err := os.Open(transcriptOf(h.cell))
				mustNil(t, err)
				defer journal.Close()
				mustNil(t, filelock.Lock(journal, true, true))
				defer filelock.Unlock(journal)
				done := make(chan error, 1)
				go func() {
					tick := time.NewTicker(time.Millisecond)
					defer tick.Stop()
					for {
						select {
						case <-ctx.Done():
							done <- ctx.Err()
							return
						case <-tick.C:
							if _, err := os.Stat(session.TakeoverPath(h.cell.Root)); err == nil {
								_, err = h.engine.Seal(ctx, h.cell, cellstore.TurnInfo{})
								_ = filelock.Unlock(journal)
								done <- err
								return
							}
						}
					}
				}()
				t.Cleanup(func() {
					cancel()
					if err := <-done; err != nil {
						t.Error(err)
					}
				})
			}
			back, err := h.continuerA().Take(ctx, h.cell.ID)
			mustNil(t, err)
			if back.Taken.Kept != "" || back.KeptTurns != 0 {
				t.Errorf("shared history was kept as a branch: %s (%d turns)", back.Taken.Kept, back.KeptTurns)
			}
			if branch, ok := branchRowOf(t, h.b, h.cell.ID); ok {
				t.Errorf("other Home gained a spurious kept row: %+v", branch)
			}
			raw, err := os.ReadFile(transcriptOf(back.Taken.Cell))
			mustNil(t, err)
			for _, reply := range []string{"first shared reply", "second shared reply", "reply on the other computer"} {
				if !strings.Contains(string(raw), reply) {
					t.Errorf("arrived chat lost %q", reply)
				}
			}
		})
	}
}
