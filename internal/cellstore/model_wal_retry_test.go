package cellstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestModelWALFailureIsNonfatalAndRetriesOnce(t *testing.T) {
	c := newCell(t)
	parent := filepath.Join(t.TempDir(), "blocked")
	path := filepath.Join(parent, "wal")
	var reports atomic.Int64
	var recorder *Recorder
	var err error
	recorder, err = NewRecorder(nil, nil, c, path, Options{Report: func(err error) {
		if err != nil {
			reports.Add(1)
			recorder.mu.Lock()
			recorder.mu.Unlock()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(parent, []byte("disk blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	recorder.NoteModelCall(executor.ModelCall{ID: "retry-model", Model: "fixture/model", TokensIn: 10, CostMicroUSD: 9000})
	// Repeated notifications share one pending identity and one error report.
	recorder.NoteModelCall(executor.ModelCall{ID: "retry-model", Model: "fixture/model", TokensIn: 10, CostMicroUSD: 9000})
	if reports.Load() != 1 {
		t.Fatalf("reports=%d", reports.Load())
	}
	if err = os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, got, err := OpenWAL(path)
		if err == nil && len(got.Models) == 1 {
			if got.Models[0].ID != "retry-model" || got.Models[0].CostMicroUSD != 9000 {
				t.Fatalf("recovered %+v", got.Models)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no retry recovery: %+v %v", got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reports.Load() != 1 {
		t.Fatalf("retry repeated report: %d", reports.Load())
	}
}

func TestModelWALCompactionPreservesWireAndOtherModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal")
	wal, _, err := OpenWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	first := ModelCall{ID: "first", Model: "fixture/first", CostMicroUSD: 1}
	second := ModelCall{ID: "second", Model: "fixture/second", Role: "worker", ParamsHash: "params", RequestHash: "request", ResponseHash: "response", TokensIn: 10, TokensOut: 2, TokensCached: 5, CostMicroUSD: 9000}
	if err = wal.Model(first); err != nil {
		t.Fatal(err)
	}
	if err = wal.Model(second); err != nil {
		t.Fatal(err)
	}
	if err = wal.Sealed(nil, first); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire) != 3 || string(wire["V"]) != "3" || string(wire["op"]) != `"model"` {
		t.Fatalf("model wire keys=%v", wire)
	}
	_, recovered, err := OpenWAL(path)
	if err != nil || len(recovered.Models) != 1 || recovered.Models[0] != second {
		t.Fatalf("recovered=%+v %v", recovered, err)
	}
}

func TestModelWALPreservesRepeatedAnonymousCalls(t *testing.T) {
	c := newCell(t)
	path := filepath.Join(t.TempDir(), "wal")
	rec, err := NewRecorder(nil, nil, c, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	call := executor.ModelCall{Model: "fixture/model", TokensIn: 10, CostMicroUSD: 9000}
	rec.NoteModelCall(call)
	rec.NoteModelCall(call)
	_, got, err := OpenWAL(path)
	if err != nil || len(got.Models) != 2 {
		t.Fatalf("calls=%+v %v", got.Models, err)
	}
	reopened, err := NewRecorder(nil, nil, c, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	reopened.NoteModelCall(call)
	_, got, err = OpenWAL(path)
	if err != nil || len(got.Models) != 3 {
		t.Fatalf("after reopen=%+v %v", got.Models, err)
	}
	for _, model := range got.Models {
		if model.ID != "" {
			t.Fatal("anonymous call acquired a shared identity")
		}
	}
}

func TestSealRecoveredDoesNotRepeatAlreadySealedModel(t *testing.T) {
	c := newCell(t)
	fake := &fakeEngine{}
	engine := fake.engine(t)
	path := engine.WALPath(c)
	wal, _, err := OpenWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	model := ModelCall{ID: "already-sealed", Model: "fixture/model", CostMicroUSD: 9000}
	if err = wal.Model(model); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Seal(t.Context(), c, TurnInfo{Models: []ModelCall{model}}); err != nil {
		t.Fatal(err)
	}
	// The receipt reached disk but the process died before removing its WAL row.
	if _, err = engine.SealRecovered(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	calls, err := ModelCalls(c)
	if err != nil || len(calls) != 1 || calls[0].ID != model.ID {
		t.Fatalf("calls=%+v %v", calls, err)
	}
	_, recovery, err := OpenWAL(path)
	if err != nil || len(recovery.Models) != 0 {
		t.Fatalf("WAL not pruned: %+v %v", recovery, err)
	}
}
