package tui3

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestAnEngineTaskReadFailureIsVisibleAndRecovers(t *testing.T) {
	for _, state := range []session.TaskState{session.TaskRunning, session.TaskDone} {
		t.Run(string(state), func(t *testing.T) {
			a, engine := localTaskRoomLab(t)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", state, session.TaskNotice{})})
			reads := 0
			a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) {
				reads++
				if reads == 1 {
					return session.TaskRecord{}, errors.New("engine temporarily unavailable")
				}
				body, err := os.ReadFile(engine.journal)
				return session.TaskRecord{Journal: body, Kept: true}, err
			}
			a.openRoomFor(7, "Fix the nil-map crash")
			a.farRoomRead(a.takeRoomPump()().(roomRecordMsg))
			text := roomText(a)
			if !strings.Contains(text, "retrying") || strings.Contains(text, roomYetWord) || strings.Contains(text, roomGoneWord) {
				t.Fatalf("a failed read was presented as missing history:\n%s", text)
			}
			cmd := a.farRoomPoll(a.room.gen)
			if cmd == nil {
				t.Fatal("the failed read has no way to recover")
			}
			a.farRoomRead(cmd().(roomRecordMsg))
			text = roomText(a)
			if !strings.Contains(text, "Checking the parser now.") || strings.Contains(text, "retrying") {
				t.Fatalf("recovery did not replace the failure:\n%s", text)
			}
			a.farRoomRead(roomRecordMsg{gen: a.room.gen, err: errors.New("connection interrupted")})
			text = roomText(a)
			if !strings.Contains(text, "Checking the parser now.") || !strings.Contains(text, "retrying") {
				t.Fatalf("a later failure erased the saved transcript or hid the failed read:\n%s", text)
			}
		})
	}
}
