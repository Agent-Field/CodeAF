package cellindex

import (
	"bytes"
	"context"
 "encoding/json"
 "time"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewDuplicateIdentityRowsDoNotHideNewReceipt(t *testing.T) {
	bin := celltest.Engine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	path := (session.Place{Dir: c.Root}).Transcript()
	if err := session.SeedConversation(path, "", "Job manager", "Manage this job."); err != nil {
		t.Fatal(err)
	}
	d, err := session.ReadDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := cellstore.NewRecorder(nil, cellstore.Engine{Binary: bin, DataRoot: t.TempDir()}, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, micro := range []int64{1000, 9000} {
		rec.NoteModelCall(executor.ModelCall{Model: "m", TokensIn: 1, CostMicroUSD: micro})
		if err := rec.Around(context.Background(), executor.Call{Tool: "edit", Args: []byte(`{}`)}, executor.EffectLocal, cellstore.AgentRun, func() ([]byte, bool) { return nil, false }); err != nil {
			t.Fatal(err)
		}
	}
	calls, err := cellstore.ModelCalls(c)
 if err != nil { t.Fatal(err) }
 // Older versions wrote this first call once live, once during rebuild.
	for _, id := range []string{d.ID, c.ID} {
		session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At:time.UnixMilli(calls[0].SealedAtMs), Session: id, Model: "m", Calls: 1, Input:1, USD: .001})
	}
	session.FlushUsage()
	before, err := os.ReadFile(session.UsageLedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := totalOf(t).micro; got != 11000 {
		t.Fatalf("ledger microUSD = %d; want 11000 (existing duplicate 2000 + new real call 9000)", got)
	}
	after, err := os.ReadFile(session.UsageLedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatal("refresh changed historical ledger rows")
	}
	if _, err := Refresh(c, ""); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(session.UsageLedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, again) {
		t.Fatal("refresh imported a saved call twice")
	}
}

func TestUsageAliasRowIdentity(t *testing.T) {
 for _, mode := range []string{"distinct calls", "rounded duplicate", "same row ID"} {
  t.Run(mode, func(t *testing.T) {
   t.Setenv(home.EnvVar, t.TempDir())
   c, err := cell.CreateIn(t.TempDir(), cell.Options{Class:cell.Sandboxed})
   if err != nil { t.Fatal(err) }
   path := (session.Place{Dir:c.Root}).Transcript()
   if err := session.SeedConversation(path,"","Manager","Manage."); err != nil { t.Fatal(err) }
   d, err := session.ReadDigest(path)
   if err != nil { t.Fatal(err) }
   at := time.Date(2026,1,1,0,0,0,0,time.UTC)
   saved := []session.UsageLine{
    {At:at, Model:"m", Calls:1, Input:1, USD:.001},
    {At:at.Add(time.Second), Model:"m", Calls:1, Input:1, USD:.009},
   }
   rows := []session.UsageLine{saved[0], saved[0]}
   want := int64(11000)
   switch mode {
   case "distinct calls":
    saved[1].USD = .001
    saved = append(saved, session.UsageLine{At:at.Add(2*time.Second), Model:"m", Calls:1, Input:1, USD:.001})
    rows[1] = saved[1]
    want = 3000
   case "rounded duplicate": rows[0].USD = .0010004
   case "same row ID": rows[0].ID = "same-call"; rows[1].ID = "same-call"
   }
   f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY,0600)
   if err != nil { t.Fatal(err) }
   for _, line := range saved {
    err = json.NewEncoder(f).Encode(map[string]any{"type":"usage", "timestamp":line.At.Format(time.RFC3339Nano), "usage":map[string]any{"model":line.Model,"calls":1,"input":1,"costUsd":line.USD}})
    if err != nil { t.Fatal(err) }
   }
   if err := f.Close(); err != nil { t.Fatal(err) }
   rows[0].Session = d.ID
   rows[1].Session = c.ID
   if mode == "same row ID" { rows[1].Session = d.ID }
   for _, row := range rows { session.RecordUsage(session.UsageLedgerPath(),row) }
   session.FlushUsage()
   if _, err := Refresh(c, ""); err != nil { t.Fatal(err) }
   if got := totalOf(t).micro; got != want { t.Fatalf("ledger microUSD = %d; want %d",got,want) }
   before, err := os.ReadFile(session.UsageLedgerPath())
   if err != nil { t.Fatal(err) }
   if _, err := Refresh(c, ""); err != nil { t.Fatal(err) }
   after, err := os.ReadFile(session.UsageLedgerPath())
   if err != nil { t.Fatal(err) }
   if !bytes.Equal(before,after) { t.Fatal("refresh imported a call twice") }
  })
 }
}
