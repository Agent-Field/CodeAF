package cellindex

import (
 "context"
 "path/filepath"
 "testing"
 "github.com/Agent-Field/codeaf/internal/cell"
 "github.com/Agent-Field/codeaf/internal/cell/celltest"
 "github.com/Agent-Field/codeaf/internal/cellstore"
 "github.com/Agent-Field/codeaf/internal/executor"
 "github.com/Agent-Field/codeaf/internal/home"
 "github.com/Agent-Field/codeaf/internal/session"
)

func TestReviewDuplicateIdentityRowsDoNotHideNewReceipt(t *testing.T) {
 bin := celltest.Engine(t)
 t.Setenv(home.EnvVar, t.TempDir())
 c, err := cell.CreateIn(t.TempDir(), cell.Options{Class:cell.Sandboxed})
 if err != nil { t.Fatal(err) }
 rec, err := cellstore.NewRecorder(nil,cellstore.Engine{Binary:bin,DataRoot:t.TempDir()},c,filepath.Join(t.TempDir(),"wal"),cellstore.Options{})
 if err != nil { t.Fatal(err) }
 for _, micro := range []int64{1000,9000} {
  rec.NoteModelCall(executor.ModelCall{Model:"m",TokensIn:1,CostMicroUSD:micro})
  if err := rec.Around(context.Background(),executor.Call{Tool:"edit",Args:[]byte(`{}`)},executor.EffectLocal,cellstore.AgentRun,func()([]byte,bool){return nil,false}); err != nil {t.Fatal(err)}
 }
 d := session.Digest{ID:"old-seeded-identity"}
 // Older versions wrote this first call once live, once during rebuild.
 for _, id := range []string{d.ID,c.ID} {
  session.RecordUsage(session.UsageLedgerPath(),session.UsageLine{Session:id,Model:"m",Calls:1,USD:.001})
 }
 session.FlushUsage()
 if err := (usageIndex{}).Build(c,d); err != nil {t.Fatal(err)}
 if got := totalOf(t).micro; got != 11000 {t.Fatalf("ledger microUSD = %d; want 11000 (existing duplicate 2000 + new real call 9000)",got)}
}
