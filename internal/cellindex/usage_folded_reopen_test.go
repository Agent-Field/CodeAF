package cellindex

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestReopeningFoldedTaskSpendNeverRebillsWorkerCalls(t *testing.T) {
	for _, paidParent := range []bool{false, true} {
		for _, marked := range []bool{false, true} {
			t.Run(fmt.Sprintf("parent-paid=%v/fold-marked=%v", paidParent, marked), func(t *testing.T) {
				t.Setenv(home.EnvVar, t.TempDir())
				c := chatCell(t)
				if paidParent {
					writeTranscript(t, c, .001)
					session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), Session: c.ID, Model: "m", Calls: 1, Input: 10, Output: 5, USD: .001})
				} else {
					writeTranscript(t, c)
				}
				path := session.Place{Dir: c.Root}.Transcript()
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				for i := range 23 {
					session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), Session: "worker", Task: "1", Model: "m", Calls: 1, Input: 10, Output: 2, USD: .000239216})
					// Older bash workers folded only dollars, with no model or calls.
					usage := `"aux":true,"costUsd":0.000239216`
					if marked {
						usage += `,"folded":true,"model":"m","calls":1,"input":10,"output":2`
					}
					if _, err := fmt.Fprintf(f, "{\"type\":\"usage\",\"usage\":{%s}}\n", usage); err != nil {
						t.Fatalf("fold %d: %v", i, err)
					}
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				session.FlushUsage()
				before, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				for _, restore := range []func() error{
					func() error { _, err := Rebuild(c, ""); return err },
					func() error { _, err := Refresh(c, ""); return err },
				} {
					if err := restore(); err != nil {
						t.Fatal(err)
					}
					session.FlushUsage()
					got, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, before) {
						t.Fatalf("reopen changed paid ledger rows: before=%d after=%d", len(before), len(got))
					}
				}
				d, err := session.ReadDigest(path)
				if err != nil {
					t.Fatal(err)
				}
				usd, _ := d.Spend()
				want := 23 * .000239216
				if paidParent {
					want += .001
				}
				if usd < want-1e-12 || usd > want+1e-12 {
					t.Fatalf("conversation books lost folded task spend: got=%g want=%g", usd, want)
				}
			})
		}
	}
}
