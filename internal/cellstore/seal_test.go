package cellstore

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func exec1(tool string, out string) Executed {
	return Executed{Call: Call{Tool: tool, ArgsHash: hashHex([]byte(tool)), Started: 10, Ended: 20,
		StdoutHash: hashHex([]byte(out)), StderrHash: hashHex(nil), SideEffect: "local"}, Exact: true}
}

func TestSealRoundTripAndChain(t *testing.T) {
	c := newCell(t)
	fake := &fakeEngine{}
	e := fake.engine(t)
	e.Now = func() time.Time { return time.UnixMilli(1759049990120) }

	transcript, _ := c.Path(cell.TranscriptPath)
	if err := os.WriteFile(transcript, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("bash", "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("edit", "")}})
	if err != nil {
		t.Fatal(err)
	}

	if first.Turn.Parent != "" || second.Turn.Parent != first.Turn.ID {
		t.Fatalf("parents: %q then %q, want none then %q", first.Turn.Parent, second.Turn.Parent, first.Turn.ID)
	}
	if first.Receipt.Transcript != (Range{0, 10}) || second.Receipt.Transcript != (Range{10, 16}) {
		t.Fatalf("ranges: %+v then %+v", first.Receipt.Transcript, second.Receipt.Transcript)
	}
	head, err := Head(c)
	if err != nil || head == nil {
		t.Fatalf("head: %v %v", head, err)
	}
	if head.Turn.ID != second.Turn.ID || head.Turn.Receipt != second.Turn.Receipt || head.Receipt.Calls[0].Tool != "edit" {
		t.Fatalf("head %+v != second %+v", head.Turn, second.Turn)
	}
	if second.Turn.SealedAtMs != 1759049990120 || second.Turn.Device != zeroDevice || second.Turn.Quality != Quiescent || second.Turn.Trigger != AgentRun {
		t.Fatalf("turn fields %+v", second.Turn)
	}
	if got := fake.count("git init"); got != 1 {
		t.Fatalf("git init ran %d times, want once", got)
	}
}

func TestPersistedObjectsCarryVersionFirst(t *testing.T) {
	c := newCell(t)
	e := (&fakeEngine{}).engine(t)
	s, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("bash", "")}})
	if err != nil {
		t.Fatal(err)
	}
	turns, _ := os.ReadFile(rel(c, TurnsPath))
	receipt, _ := os.ReadFile(rel(c, ReceiptsDir+"/"+s.Turn.Receipt+".json"))
	for name, raw := range map[string][]byte{"turn": turns, "receipt": receipt} {
		if !bytes.HasPrefix(raw, []byte(`{"V":1,`)) {
			t.Errorf("%s does not start with V: %.40s", name, raw)
		}
	}
	if hashHex(receipt) != s.Turn.Receipt {
		t.Error("receipt id is not the hash of its bytes")
	}
}

func TestTurbulentWhenACallLeftAProcess(t *testing.T) {
	c := newCell(t)
	e := (&fakeEngine{}).engine(t)
	x := exec1("bash", "")
	x.Exact = false
	x.Services = []ServiceRec{serviceRec(4711, []string{"/usr/bin/postgres", "-D", "var/pg"}, []int{5432})}
	s, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{x}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Turn.Quality != Turbulent || s.Receipt.Services[0].Argv[0] != "postgres" {
		t.Fatalf("%+v %+v", s.Turn, s.Receipt.Services)
	}
}

func TestExternalOutputIsStoredAndLocalIsHashedOnly(t *testing.T) {
	c := newCell(t)
	e := (&fakeEngine{}).engine(t)
	ext := exec1("bash", "body")
	ext.Call.SideEffect, ext.Stdout = "external", []byte("body")
	loc := exec1("edit", "")
	if _, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{ext, loc}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(rel(c, BlobsDir+"/"+ext.Call.StdoutHash))
	if err != nil || string(got) != "body" {
		t.Fatalf("blob %q %v", got, err)
	}
	if entries, _ := os.ReadDir(rel(c, BlobsDir)); len(entries) != 1 {
		t.Fatalf("%d blobs, want only the external call's", len(entries))
	}
}

func TestSealRestoresMetaAToolEdited(t *testing.T) {
	c := newCell(t)
	e := (&fakeEngine{}).engine(t)
	path, _ := c.Path(cell.MetaPath)
	want, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(`{"V":1,"class":"host-bound"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Seal(context.Background(), c, TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, want) {
		t.Fatalf("meta.json after seal: %s", strings.TrimSpace(string(got)))
	}
}

func TestSealFailureLeavesNoTurn(t *testing.T) {
	c := newCell(t)
	e := Engine{Binary: "engine", DataRoot: t.TempDir(), Run: func(context.Context, string, []string, ...string) ([]byte, error) {
		return []byte("not json"), nil
	}}
	if _, err := e.Seal(context.Background(), c, TurnInfo{}); err == nil {
		t.Fatal("want an error for an answer that is not a snapshot id")
	}
	if head, _ := Head(c); head != nil {
		t.Fatalf("a failed seal recorded %+v", head.Turn)
	}
}
