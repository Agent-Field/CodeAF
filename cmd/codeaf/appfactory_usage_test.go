package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE FACTORY SAYS WHICH MODELS ITS JOBS USED: the calls this computer made for
// its jobs in the range, by model and by job, how each model is paid (the
// calls' own word first, the model catalog's one kind second, else nothing),
// dollars only where a call was priced, and no chat of the person's own.
func TestFactoryUsageByModelReadsTheJobsLedgerRows(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	floor := s.backend.local.host.floor
	a, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "a", Kind: factory.KindIssue, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	b, err := floor.Create(factory.Item{Repo: "acme/api", Num: 8, Title: "b", Kind: factory.KindIssue, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	row := func(job, model, billing string, in, out int, usd float64) session.UsageLine {
		return session.UsageLine{At: at, Day: "2026-10-28", FactoryJob: job, Model: model, Billing: billing, Calls: 1, Input: in, Output: out, USD: usd}
	}
	var text strings.Builder
	for _, l := range []session.UsageLine{
		row("acme/api#7", "vendor/big", "metered", 1000, 100, 0.40),
		row("acme/api#8", "vendor/big", "metered", 500, 50, 0.20),
		row("acme/api#7", "codex", "included", 300, 30, 0),
		row("acme/api#8", "qwen3-coder", "", 200, 20, 0),
		row("", "vendor/big", "metered", 9999, 999, 9),
	} {
		data, _ := json.Marshal(l)
		text.Write(append(data, '\n'))
	}
	ledger := filepath.Join(t.TempDir(), session.UsageLedgerName)
	if err := os.WriteFile(ledger, []byte(text.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath, oldKinds := appLedgerPath, appFactoryModelKinds
	appLedgerPath = func() string { return ledger }
	appFactoryModelKinds = func() map[string]string { return map[string]string{"qwen3-coder": "local", "vendor/big": "plan"} }
	t.Cleanup(func() { appLedgerPath, appFactoryModelKinds = oldPath, oldKinds })

	var week appwire.FactoryUsage
	s.must("factory.usage_by_model", map[string]any{"factory": st.Factory, "from": "2026-10-26", "to": "2026-11-01"}, &week)
	got, _ := json.Marshal(week.Models)
	want := `[{"model":"vendor/big","paid":"metered","calls":2,"tokens_in":1500,"tokens_out":150,"cost_usd":0.6},` +
		`{"model":"codex","paid":"plan","calls":1,"tokens_in":300,"tokens_out":30,"unpriced_calls":1},` +
		`{"model":"qwen3-coder","paid":"local","calls":1,"tokens_in":200,"tokens_out":20,"unpriced_calls":1}]`
	if string(got) != want {
		t.Fatalf("week models = %s", got)
	}
	if week.Scope != "this_computer" || len(week.Jobs) != 2 || week.Jobs[0].Job != a.ID || week.Jobs[0].Ref != "acme/api#7" || week.Jobs[1].Job != b.ID {
		t.Fatalf("week jobs = %+v", week.Jobs)
	}
	var one appwire.FactoryUsage
	s.must("factory.usage_by_model", map[string]any{"factory": st.Factory, "from": "2026-10-28", "to": "2026-10-28", "job": b.ID}, &one)
	if len(one.Jobs) != 1 || one.Jobs[0].Job != b.ID || len(one.Models) != 2 || one.Models[0].Model != "vendor/big" || one.Models[0].CostUSD == nil || *one.Models[0].CostUSD != 0.20 {
		t.Fatalf("one job = %+v", one)
	}
	var none appwire.FactoryUsage
	s.must("factory.usage_by_model", map[string]any{"factory": st.Factory, "from": "2026-10-29", "to": "2026-10-29"}, &none)
	if len(none.Models) != 0 || len(none.Jobs) != 0 {
		t.Fatalf("an empty day = %+v", none)
	}
	s.mustFail("factory.usage_by_model", map[string]any{"factory": "f-elsewhere", "from": "2026-10-28", "to": "2026-10-28"}, appwire.CodeNotFound)
}

// A step's meter and a job's chat both name their job for the ledger.
func TestAJobsMetersNameTheJobForTheLedger(t *testing.T) {
	it := factory.Item{ID: 3, Repo: "Acme/API", Num: 7}
	for _, meter := range []session.JobChat{
		&stageBudget{item: it},
		ownedStageBudget{&jobChat{id: it.ID, name: factory.LedgerName(it)}},
	} {
		named, ok := meter.(session.JobLedgerName)
		if !ok || named.FactoryJob() != "acme/api#7" {
			t.Fatalf("%T names %v", meter, ok)
		}
	}
}
