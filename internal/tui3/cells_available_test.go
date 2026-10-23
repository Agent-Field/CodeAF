package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// TestC27UnavailableSurface proves C27.3: no live pairing,
// device, sync or takeover door survives, and every explicit gesture explains
// the same reason without starting work or drawing a move card.
func TestC27UnavailableSurface(t *testing.T) {
	for _, reason := range []string{cell.WindowsReason, furrowbin.CellsUnavailable} {
		t.Run(reason, func(t *testing.T) {
			a := newApp(context.Background(), Options{
				Agent: &fakeAgent{model: "m"}, CellsUnavailable: reason,
				ProfileDir: t.TempDir(), UsageLedger: labLedger(),
				Pairing: &fakePairing{}, Approvals: &fakeApprovals{},
				Fleet: &fakeFleet{size: 1}, Takeover: &fakeTaker{},
			})
			a.width, a.height = 160, 40
			a.dismissWelcome()
			a.openHome()
			if _, claimed := (placeHome{}).owns(a, key(addMachineKey)); !claimed {
				t.Fatal("home swallowed the add-device chord without explaining availability")
			}
			if a.pairing != nil || a.approvals != nil || a.fleet != nil || a.taker != nil || a.machines != nil {
				t.Fatal("unavailable capability retained a live door")
			}
			for _, gesture := range []func() tea.Cmd{
				func() tea.Cmd { return a.runPair("") },
				func() tea.Cmd { return a.runPair("https://example.com/pair#token") },
				func() tea.Cmd { return a.openDevices() },
				func() tea.Cmd { return a.toggleAddMachine() },
				func() tea.Cmd { return a.raiseContinue(chatlist.Row{}, 0) },
			} {
				if gesture() != nil {
					t.Fatal("refusal started work")
				}
				if a.pair.open || a.addMachine.open {
					t.Fatal("refusal opened a card")
				}
				if !strings.Contains(plain(frame(a)), reason) {
					t.Fatalf("gesture omitted %q:\n%s", reason, plain(frame(a)))
				}
			}
		})
	}
}
