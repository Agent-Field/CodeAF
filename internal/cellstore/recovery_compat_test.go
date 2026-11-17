package cellstore

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// baseCall and baseReceipt are the receipt shapes a reader built before calls
// carried an ID decodes into, copied from app/v3 before this change.
type baseCall struct {
	Tool       string `json:"tool"`
	ArgsHash   string `json:"args_hash"`
	Started    int64  `json:"started"`
	Ended      int64  `json:"ended"`
	Exit       int    `json:"exit"`
	StdoutHash string `json:"stdout_hash"`
	StderrHash string `json:"stderr_hash"`
	SideEffect string `json:"side_effect"`
}

type baseReceipt struct {
	V          uint16     `json:"V"`
	Calls      []baseCall `json:"calls"`
	SealedAtMs int64      `json:"sealed_at_ms,omitempty"`
}

func TestOlderReceiptReaderReadsACallIDAsIfItWereAbsent(t *testing.T) {
	call := Call{Tool: "exec", ArgsHash: "same", Started: 7, Ended: 8, StdoutHash: "out", StderrHash: "err", SideEffect: "external"}
	with, without := call, call
	with.ID = "call-1"
	decode := func(c Call) baseReceipt {
		t.Helper()
		raw, err := json.Marshal(Receipt{V: 1, Calls: []Call{c}, SealedAtMs: 9})
		if err != nil {
			t.Fatal(err)
		}
		var old baseReceipt
		if err := json.Unmarshal(raw, &old); err != nil {
			t.Fatalf("older reader refused a receipt: %v", err)
		}
		return old
	}
	if got, want := decode(with), decode(without); !reflect.DeepEqual(got, want) {
		t.Fatalf("older reader saw %+v with an ID, %+v without", got, want)
	}
}

func TestRecoveryBooksAnOldReceiptAndANewWALCallOnceEach(t *testing.T) {
	c := newCell(t)
	e := (&fakeEngine{}).engine(t)
	wal, _, err := OpenWAL(e.WALPath(c))
	if err != nil {
		t.Fatal(err)
	}
	// An older binary sealed this call: no ID in its WAL record or its receipt.
	old := Intent{V: walV, Tool: "exec", ArgsHash: "old", Started: 1, SideEffect: "external"}
	// A newer binary finished this one and died before sealing it.
	fresh := Intent{V: walV, ID: "fresh-1", Tool: "exec", ArgsHash: "fresh", Started: 2, SideEffect: "external"}
	for _, i := range []Intent{old, fresh} {
		if err := wal.Begin(i); err != nil {
			t.Fatal(err)
		}
		done := executed(i, executor.ExecResult{Stdout: []byte("charged")}, nil, i.Started+1)
		if err := wal.Finish(i, done); err != nil {
			t.Fatal(err)
		}
		if i.ArgsHash == old.ArgsHash {
			if done.Call.ID != "" {
				t.Fatalf("old-format call carried an ID: %+v", done.Call)
			}
			if _, err := e.Seal(t.Context(), c, TurnInfo{Calls: []Executed{done}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := e.SealRecovered(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	entries, err := Log(c)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, entry := range entries {
		for _, call := range entry.Receipt.Calls {
			counts[call.ArgsHash]++
		}
	}
	if counts["old"] != 1 || counts["fresh"] != 1 || len(counts) != 2 {
		t.Fatalf("completion counts = %v, want old and fresh once each", counts)
	}
}
