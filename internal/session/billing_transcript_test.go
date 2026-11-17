package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDigestKeepsOnlyStatedEventBillingFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	raw := `{"type":"session","id":"chat"}
{"type":"usage","usage":{"remote_event":"event:1","model":"m","input":20,"calls":1,"billing":"metered","billing_basis":"door:test","unbilled":true,"factory_job":"o/r#1"}}
{"type":"usage","usage":{"model":"m","input":40,"calls":2}}
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := ReadDigest(path)
	if err != nil || len(d.Usage) != 2 {
		t.Fatalf("digest=%+v err=%v", d, err)
	}
	got := d.Usage[0]
	if got.Billing != "metered" || got.BillingBasis != "door:test" || !got.Unbilled || got.FactoryJob != "o/r#1" {
		t.Errorf("event facts=%+v", got)
	}
	old := d.Usage[1]
	if old.Billing != "" || old.BillingBasis != "" || old.Unbilled || old.FactoryJob != "" {
		t.Errorf("invented aggregate facts=%+v", old)
	}
}

func TestRemoteEventJournalsItsKnownFactoryJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	t.Cleanup(func() { StopUsageWriter(path) })
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		if err := spec.SaveRemote(remoteRecordFor(spec), true); err != nil {
			t.Error(err)
			return RunSummary{Outcome: "interrupted"}
		}
		if err := spec.OnRemoteCharge(1, RunCharge{Model: "remote/m", TokensIn: 10, USD: .001}, "worker"); err != nil {
			t.Error(err)
		}
		return RunSummary{Outcome: "interrupted", KeepCopy: true}
	}}
	agent, _ := remoteTaskAgent(t, remote, func(c *Config) {
		c.usageLedger = path
		c.JobBudget = &namedBudgetStub{liveBudgetStub: liveBudgetStub{cap: 5}, job: "o/r#1"}
	})
	if _, _, _, err := agent.StartTaskOn(t.Context(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, agent)
	d, err := ReadDigest(agent.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	var events int
	for _, row := range d.Usage {
		if row.RemoteEvent != "" {
			events++
			if row.FactoryJob != "o/r#1" || row.Billing != "" || row.BillingBasis != "" || row.Unbilled {
				t.Errorf("mirrored facts=%+v", row)
			}
		}
	}
	if events != 1 {
		t.Fatalf("events=%d want 1", events)
	}
}
