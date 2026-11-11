package cellstore_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
)

func TestOpenWALSkipsV3ModelWithoutLosingV1V2Records(t *testing.T) {
	// Literal wire records exercise the current reader without a V3 encoder.
	raw := []byte(`{"V":1,"op":"intent","intent":{"V":1,"tool":"bash","args_hash":"before","started":100,"side_effect":"external"}}
{"V":1,"op":"done","intent":{"V":1,"tool":"bash","args_hash":"before","started":100,"side_effect":"external"},"done":{"call":{"tool":"bash","args_hash":"before","started":100,"ended":110,"exit":0,"stdout_hash":"out-before","stderr_hash":"err-before","side_effect":"external"},"exact":true}}
{"V":1,"op":"intent","intent":{"V":1,"tool":"bash","args_hash":"open-before","started":120,"side_effect":"external"}}
{"V":3,"op":"model","model":{"id":"compat-charge","model":"fixture/model","role":"worker","params_hash":"params","request_hash":"request","response_hash":"response","tokens_in":1000,"tokens_out":200,"tokens_cached":500,"cost_micro_usd":9000}}
{"V":2,"op":"intent","intent":{"V":2,"tool":"bash","args_hash":"after","started":200,"side_effect":"local","brief":"fixture after"}}
{"V":2,"op":"done","intent":{"V":2,"tool":"bash","args_hash":"after","started":200,"side_effect":"local","brief":"fixture after"},"done":{"call":{"tool":"bash","args_hash":"after","started":200,"ended":210,"exit":7,"stdout_hash":"out-after","stderr_hash":"err-after","side_effect":"local"},"exact":false}}
{"V":2,"op":"intent","intent":{"V":2,"tool":"bash","args_hash":"open-after","started":220,"side_effect":"local","brief":"still open"}}
`)
	path := filepath.Join(t.TempDir(), "wal.jsonl")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	want := cellstore.Recovery{
		Models: []cellstore.ModelCall{{ID: "compat-charge", Model: "fixture/model", Role: "worker", ParamsHash: "params", RequestHash: "request", ResponseHash: "response", TokensIn: 1000, TokensOut: 200, TokensCached: 500, CostMicroUSD: 9000}},
		Incomplete: []cellstore.Intent{
			{V: 1, Tool: "bash", ArgsHash: "open-before", Started: 120, SideEffect: "external"},
			{V: 2, Tool: "bash", ArgsHash: "open-after", Started: 220, SideEffect: "local", Brief: "still open"},
		},
		Completed: []cellstore.Executed{
			{Call: cellstore.Call{Tool: "bash", ArgsHash: "before", Started: 100, Ended: 110, Exit: 0, StdoutHash: "out-before", StderrHash: "err-before", SideEffect: "external"}, Exact: true},
			{Call: cellstore.Call{Tool: "bash", ArgsHash: "after", Started: 200, Ended: 210, Exit: 7, StdoutHash: "out-after", StderrHash: "err-after", SideEffect: "local"}},
		},
	}
	for reopen := 1; reopen <= 2; reopen++ {
		wal, got, err := cellstore.OpenWAL(path)
		if err != nil {
			t.Fatalf("open %d: %v", reopen, err)
		}
		if wal == nil {
			t.Fatalf("open %d: nil WAL without error", reopen)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("open %d: recovery = %+v; want %+v", reopen, got, want)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, raw) {
			t.Fatalf("open %d changed the mixed-version WAL", reopen)
		}
	}
}
