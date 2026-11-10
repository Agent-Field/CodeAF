package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

func TestIdleRelease_R3_ResumeOfferIgnoresReleasedChatOfOnlineDevice(t *testing.T) {
	for _, presence := range []string{"online", "offline", "feed down", "no feed"} {
		for _, lease := range []struct {
			name     string
			status   chatlist.Status
			released bool
		}{
			{"live", chatlist.Running, false},
			{"lapsed", chatlist.Off, false},
			{"idle released", chatlist.Idle, true},
		} {
			t.Run(presence+"/"+lease.name, func(t *testing.T) {
				a, feed := devicesRig("dev_tower")
				a.taker = &fakeTaker{}
				switch presence {
				case "offline":
					feed.state.Online = nil
				case "feed down":
					feed.state.Up = false
				case "no feed":
					a.dirFeed = nil
				}
				row := leftOffRow("chat", "tower", lease.status, time.Minute)
				row.Released = lease.released
				a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{row}})
				want := presence == "offline" || presence != "online" && lease.status != chatlist.Running
				if got := strings.Contains(leftOffFrame(a), "Continue tower's latest chat here?"); got != want {
					t.Fatalf("offer = %v, want %v; frame = %q", got, want, leftOffFrame(a))
				}
				if want {
					a.resumeKeyHandler(resumeSkipKey)
					a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{row}})
					if leftOffFrame(a) != "" {
						t.Fatalf("answered offer returned: %q", leftOffFrame(a))
					}
				}
			})
		}
	}
}
