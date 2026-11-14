package session

import (
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestAgentFixtureJoinsLateReceiptAndUsageWriter(t *testing.T) {
	for _, found := range []bool{false, true} {
		name := "missing"
		if found {
			name = "priced"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			path := UsageLedgerPath()
			returnedEarly, stoppedByFixture, stopped := false, false, false
			synctest.Test(t, func(t *testing.T) {
				releaseReceipt := make(chan struct{})
				releaseWrite := make(chan struct{})
				receiptDone := make(chan struct{})
				fixtureDone := make(chan struct{})
				bodyDone := make(chan struct{})
				t.Cleanup(func() { close(fixtureDone) })
				agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
				agent.stopLaneBeat()
				// A cancelled provider arm admits this token before its turn
				// returns, then banks its receipt on a different goroutine.
				settled := agent.pendingReceipt()
				var writer *usageWriter
				go func() {
					<-releaseReceipt
					writer = usageWriterFor(path)
					writer.beforeWrite = func() { <-releaseWrite }
					agent.reconciled(provider.Reconciled{
						Found: found, Ref: "cancelled-call", Tag: "chat",
						Billed: provider.Billed{Model: "test/model", PromptTokens: 1, Cost: .01},
					})
					settled()
					close(receiptDone)
				}()
				go func() {
					<-bodyDone
					// The fixture has either blocked in its cleanup or finished
					// incorrectly while its receipt still owes accounting.
					synctest.Wait()
					select {
					case <-fixtureDone:
						returnedEarly = true
					default:
					}
					close(releaseReceipt)
					<-receiptDone
					synctest.Wait()
					select {
					case <-fixtureDone:
						returnedEarly = true
					default:
					}
					close(releaseWrite)
					<-fixtureDone
					select {
					case <-writer.stopped:
						stoppedByFixture = true
					default:
					}
					// Join even with the broken fixture, so this regression
					// cannot leave a writer for its own TempDir cleanup.
					stopped = StopUsageWriter(path)
					<-writer.stopped
				}()
				close(bodyDone)
			})
			if !stopped {
				t.Fatal("usage writer did not stop after release")
			}
			rows, err := ReadUsage(path, time.Time{})
			if err != nil || len(rows) != 1 {
				t.Fatalf("cancelled call rows=%+v error=%v, want exactly one", rows, err)
			}
			if rows[0].Unbilled == found || found && rows[0].USD != .01 {
				t.Fatalf("cancelled call accounting=%+v", rows[0])
			}
			if !stoppedByFixture {
				t.Error("fixture cleanup returned with its usage writer still running")
			}
			if returnedEarly {
				t.Error("fixture cleanup returned before pending receipt and ledger write; accounting landed after close")
			}
		})
	}
}

func TestAgentFixtureClosesItsCapturedUsageLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	t.Run("fixture", func(t *testing.T) {
		_, _ = newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.usageLedger = path })
		RecordUsage(path, UsageLine{ID: "captured-ledger", Input: 1})
	})
	left := len(usageWritersAt(path))
	StopUsageWriter(path)
	if left != 0 {
		t.Fatal("fixture cleanup left its captured ledger writer running")
	}
}
