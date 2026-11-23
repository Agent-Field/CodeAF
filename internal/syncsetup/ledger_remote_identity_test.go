package syncsetup

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Saved transcript and ledger fixtures carry the historical wire formats.
// All reconciliation runs through real take, storage and the public ledger.
func TestTakeLedgerRemoteIdentityCombinations(t *testing.T) {
	for _, source := range []string{"remote_only", "receipt_only_ordinary", "transcript_legacy", "sealed_legacy", "sealed_modern"} {
		for _, remoteRow := range []string{"old_no_id", "old_independent_id", "new"} {
			coverage := []bool{true}
			if source == "transcript_legacy" || source == "sealed_legacy" {
				coverage = []bool{false, true}
			}
			for _, covered := range coverage {
				t.Run(fmt.Sprintf("%s/%s/ordinary_covered_%t", source, remoteRow, covered), func(t *testing.T) {
					h := newTwoHomes(t)
					seedTree(t, h.work)
					t.Setenv(home.EnvVar, h.homeA)
					pathA := session.UsageLedgerPath()
					a := h.openA()
					t.Cleanup(func() { session.StopUsageWriter(pathA) })
					ordinary := source != "remote_only"
					if ordinary {
						id := ""
						if source == "sealed_modern" || source == "receipt_only_ordinary" {
							id = "ordinary-a"
						}
						if source != "transcript_legacy" {
							executor.NoteModelCall(a.seat, executor.ModelCall{ID: id, Model: "m", Role: "turn", TokensIn: 100, TokensOut: 10, CostMicroUSD: 1000})
						}
						if source != "receipt_only_ordinary" {
							appendTo(t, transcriptOf(a.cell), `{"type":"usage","timestamp":"2026-09-29T10:00:01Z","usage":{"model":"m","calls":1,"input":100,"output":10,"costUsd":0.001}}`+"\n")
						}
						if covered {
							raw, _ := json.Marshal(session.UsageLine{At: time.Date(2026, 9, 29, 10, 0, 1, 0, time.UTC), ID: id, SharedID: id != "", Session: h.cell.ID, Model: "m", Calls: 1, Input: 100, Output: 10, USD: .001})
							appendTo(t, pathA, string(raw)+"\n")
						}
					}
					addRemote := func(o *openChat, path, event string, usd float64) {
						t.Helper()
						row := session.UsageLine{At: time.Date(2026, 9, 29, 10, 0, 1, 0, time.UTC), Session: h.cell.ID, RemoteEvent: event, Model: "remote/m", Calls: 1, Input: 30, Output: 3, USD: usd}
						if remoteRow == "old_independent_id" {
							row.ID = "row-" + event
						}
						if remoteRow == "new" {
							row.ID = "modern-" + event
							row.SharedID = true
						}
						raw, err := json.Marshal(row)
						if err != nil {
							t.Fatal(err)
						}
						appendTo(t, path, string(raw)+"\n")
						appendTo(t, transcriptOf(o.cell), fmt.Sprintf(`{"type":"usage","timestamp":"2026-09-29T10:00:01Z","usage":{"remote_event":%q,"model":"remote/m","calls":1,"input":30,"output":3,"costUsd":%v,"aux":true}}`+"\n", event, usd))
						o.mustSay("remote charge saved")
					}
					addRemote(a, pathA, "attempt-a:1", .003)
					h.durable(h.cell.ID, a.cell)
					if err := a.drive.Close(t.Context()); err != nil {
						t.Fatal(err)
					}
					t.Setenv(home.EnvVar, h.homeB)
					took, err := h.continuerB().Take(t.Context(), h.cell.ID)
					if err != nil {
						t.Fatal(err)
					}
					pathB := session.UsageLedgerPath()
					t.Cleanup(func() { session.StopUsageWriter(pathB) })
					want, rows := .003, 1
					if ordinary {
						want, rows = .004, 2
					}
					assertTakeLedger(t, pathB, h.cell.ID, want, rows)
					b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
					addRemote(b, pathB, "attempt-b:1", .005)
					h.durable(h.cell.ID, b.cell)
					if err := b.drive.Close(t.Context()); err != nil {
						t.Fatal(err)
					}
					t.Setenv(home.EnvVar, h.homeA)
					if _, err := h.continuerA().Take(t.Context(), h.cell.ID); err != nil {
						t.Fatal(err)
					}
					want, rows = .008, 2
					if ordinary {
						want, rows = .009, 3
					}
					assertTakeLedger(t, pathA, h.cell.ID, want, rows)
					// Import replay uses another row ID but the same remote event identity.
					session.RecordImportedUsage(pathA, session.UsageLine{ID: "replayed-row", RemoteEvent: "attempt-b:1", Session: h.cell.ID, Input: 30, Output: 3, USD: .005})
					assertTakeLedger(t, pathA, h.cell.ID, want, rows)
					t.Setenv(home.EnvVar, h.homeB)
					if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
						t.Fatal(err)
					}
					assertTakeLedger(t, pathB, h.cell.ID, want, rows)
				})
			}
		}
	}
}
