package run

import (
 "errors"
 "testing"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestRepeatedHoldRefusesPersonStop(t *testing.T) {
 st, err := store.Open(t.TempDir())
 if err != nil { t.Fatal(err) }
 it, err := st.Create(factory.Item{Kind: factory.KindIssue, State: factory.StateRunning, Stream: &factory.Stream{Paused: true}})
 if err != nil { t.Fatal(err) }
 if err := st.Update(it.ID, func(cur *factory.Item) error { factory.MarkPersonStopped(cur); return nil }); err != nil { t.Fatal(err) }
 r := New(Options{Store: st})
 for n := 0; n < 3; n++ {
  if err := r.Hold(it.ID, true); !errors.Is(err, factory.ErrPersonStopped) { t.Fatalf("hold %d accepted Stop: %v", n, err) }
 }
}
