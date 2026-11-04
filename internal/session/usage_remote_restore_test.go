package session

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// StartTaskOn delivers remote charge callbacks through the real task adapter.
// Restoring the machine ledger must invalidate both present and absent events.
func TestRemoteUsageRestoreCountsEachChargeOnce(t *testing.T) {
	for _, restore := range []string{"replace", "rewrite", "rewrite_and_grow"} {
		for _, replay := range []uint64{1, 3} {
			t.Run(fmt.Sprintf("%s/replay_%d", restore, replay), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), UsageLedgerName)
				defer StopUsageWriter(path)
				result := make(chan error, 1)
				remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
					err := func() error {
						if err := spec.SaveRemote(remoteRecordFor(spec), true); err != nil {
							return err
						}
						charge := RunCharge{Model: "remote/model", TokensIn: 10, USD: .001}
						for _, seq := range []uint64{1, 2, 2} {
							if err := spec.OnRemoteCharge(seq, charge, "worker"); err != nil {
								return err
							}
						}
						raw, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						old := []byte("att_0123456789abcdef:1")
						if bytes.Count(raw, old) != 1 {
							return fmt.Errorf("expected exactly one original event in ledger")
						}
						raw = bytes.Replace(raw, old, []byte("att_0123456789abcdef:3"), 1)
						target := path
						if restore == "replace" {
							target += ".restore"
						}
						if err := os.WriteFile(target, raw, 0600); err != nil {
							return err
						}
						if restore == "replace" {
							if err := os.Rename(target, path); err != nil {
								return err
							}
						} else {
							// Make the changed generation independent of clock resolution.
							at := time.Unix(100, 0)
							if err := os.Chtimes(path, at, at); err != nil {
								return err
							}
						}
						if restore == "rewrite_and_grow" {
							RecordUsage(path, UsageLine{ID: "external-call", USD: .001})
							if !FlushUsage() {
								return fmt.Errorf("external charge did not flush")
							}
						}
						// Repeated delivery still represents one paid event.
						for range 2 {
							if err := spec.OnRemoteCharge(replay, charge, "worker"); err != nil {
								return err
							}
						}
						return nil
					}()
					result <- err
					return RunSummary{Outcome: "interrupted", KeepCopy: true}
				}}
				a, _ := remoteTaskAgent(t, remote, func(c *Config) { c.usageLedger = path })
				if _, _, _, err := a.StartTaskOn(t.Context(), "Repair export", false, "work computer"); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-result:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("remote charge script did not finish")
				}
				waitRemoteRun(t, a)
				rows, err := ReadUsage(path, time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				wantRows, want := 2, .002
				if replay == 1 {
					wantRows, want = 3, .003
				}
				if restore == "rewrite_and_grow" {
					wantRows++
					want += .001
				}
				var total float64
				for _, row := range rows {
					total += row.USD
				}
				t.Logf("expected=$%.6f/%d rows recorded=$%.6f/%d rows", want, wantRows, total, len(rows))
				if len(rows) != wantRows || math.Abs(total-want) > 1e-12 {
					t.Fatalf("restored remote charge was lost or counted twice")
				}
			})
		}
	}
}
