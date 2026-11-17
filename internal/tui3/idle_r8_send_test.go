package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

type r8SendSource struct {
	*r7SendSource
	holding bool
}

func (s *r8SendSource) SendLease(id string) (string, uint64, error) {
	if !s.holding {
		return "", 0, os.ErrNotExist
	}
	return s.r7SendSource.SendLease(id)
}

type r8SendAgent struct {
	*fakeAgent
	source *r8SendSource
	checks int
	retake bool
}

func (a *r8SendAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, time.Second, a.source.holding
}

func (a *r8SendAgent) CheckWork() error {
	a.checks++
	a.source.holding = true
	if a.retake {
		a.source.change(func(row *chatlist.Row) { row.Fence++ })
	}
	return nil
}

func r8OwnLiveWindow(t *testing.T, retake bool) (*app, *r8SendAgent, *r8SendSource) {
	t.Helper()
	a, old, source := r7ReleasedWindow(t)
	source.change(func(row *chatlist.Row) {
		row.Status, row.Released, row.AutoOpen = chatlist.Here, false, false
	})
	s := &r8SendSource{r7SendSource: source}
	agent := &r8SendAgent{fakeAgent: old, source: s, retake: retake}
	a.machines, a.agent = s, agent
	a.machineRead.sendHolds = map[string]sendHold{heldCell: {fence: 2}}
	return a, agent, s
}

func TestIdleOpen_R8_OwnLiveRowAfterBlipSendsNoCard(t *testing.T) {
	for _, tc := range []struct {
		name   string
		retake bool
		known  bool
		dirty  bool
	}{
		{"same fence", false, true, false},
		{"uncertain release retakes", true, true, false},
		{"first fence not yet listed", true, false, false},
		{"unuploaded local head on retained hold", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			retake := tc.retake
			a, agent, source := r8OwnLiveWindow(t, retake)
			if !tc.known {
				a.machineRead.sendHolds = nil
			}
			if tc.dirty {
				path := filepath.Join(session.DirOf(a.file), ".cell", "adopted.json")
				if err := os.WriteFile(path, []byte(`{"V":1,"head":"unsent","turns_size":0}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a.input.setText("send after the blip")
			drive(t, a, key("enter"))
			wantFence := uint64(2)
			if retake {
				wantFence++
			}
			rows, _ := source.Rows(t.Context())
			if agent.checks != 1 || len(agent.sent) != 1 || agent.sent[0] != "send after the blip" || rows[0].Fence != wantFence || a.at(pageHome) || a.agent != agent {
				t.Fatalf("own live send: checks=%d sent=%v rows=%+v home=%v notes=%v", agent.checks, agent.sent, rows, a.at(pageHome), noteTexts(a))
			}
			if strings.Contains(homeText(a), continueAsk) || strings.Contains(strings.Join(noteTexts(a), "\n"), "last held here") {
				t.Fatal("own live row raised a continuation card")
			}
		})
	}
}

func TestIdleOpen_R8_OtherHolderStillGetsCard(t *testing.T) {
	for _, ownNewer := range []bool{false, true} {
		t.Run(map[bool]string{false: "another computer", true: "another window on this computer"}[ownNewer], func(t *testing.T) {
			a, agent, source := r8OwnLiveWindow(t, false)
			source.change(func(row *chatlist.Row) {
				row.Mine, row.DeviceID, row.Device, row.Status = false, "B", "laptop", chatlist.Running
				if ownNewer {
					row.Mine, row.DeviceID, row.Status, row.Fence = true, "A", chatlist.Here, 3
				}
			})
			a.input.setText("keep this draft")
			drive(t, a, key("enter"))
			if agent.checks != 0 || len(agent.sent) != 0 || !a.at(pageHome) || !strings.Contains(homeText(a), continueAsk) || a.input.String() != "keep this draft" {
				t.Fatalf("changed holder skipped its card: checks=%d sent=%v home=%s draft=%q", agent.checks, agent.sent, homeText(a), a.input.String())
			}
		})
	}
}
