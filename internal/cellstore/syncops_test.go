package cellstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

const (
	testCellKey = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	testDedup   = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22"
)

func testKeys() SyncKeys {
	return SyncKeys{CellKey: mustHex(testCellKey), Dedup: mustHex(testDedup)}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// scripted is a Transport that answers each verb with a canned reply and
// remembers what it was asked.
type scripted struct {
	replies map[string]string
	errs    map[string]error
	ops     []Op
	targets []Target
}

func (s *scripted) Do(_ context.Context, t Target, op Op) ([]byte, error) {
	s.ops, s.targets = append(s.ops, op), append(s.targets, t)
	if err := s.errs[op.Verb()]; err != nil {
		return nil, err
	}
	return []byte(s.replies[op.Verb()]), nil
}

const rid = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var contractReplies = map[string]string{
	"export":      `{"frames":[{"path":"/o/f1","objects":3,"bytes":900}],"head_rid":"` + rid + `","objects":3,"bytes":900}`,
	"published":   `{"recorded":3}`,
	"want":        `{"want":["` + rid + `"]}`,
	"import":      `{"imported":4}`,
	"materialize": `{"snapshot":"` + rid + `"}`,
}

func syncEngineOver(tr Transport) SyncEngine {
	return SyncEngine{
		Transport: tr, Keys: testKeys(), Ledger: LedgerName("http://relay", "id_x"),
		Target: func(c cell.Cell) Target { return Target{Tree: c.Root, DataDir: "/data/" + c.ID} },
		Inbox:  func(c cell.Cell) string { return "/inbox/" + c.ID },
	}
}

func TestSyncEngineOpsParseContractJSON(t *testing.T) {
	c := cell.Cell{ID: "c1", Root: "/tree"}
	tr := &scripted{replies: contractReplies}
	e := syncEngineOver(tr)
	ctx := context.Background()

	exp, err := e.Export(ctx, c, "h")
	if err != nil {
		t.Fatal(err)
	}
	wantExp := Export{Frames: []FrameFile{{Path: "/o/f1", Objects: 3, Bytes: 900}}, HeadRID: rid, Objects: 3, Bytes: 900}
	if !reflect.DeepEqual(exp, wantExp) {
		t.Fatalf("export = %+v", exp)
	}
	if err := e.Published(ctx, c, []string{"/o/f1"}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Want(ctx, c, "h"); err != nil || !reflect.DeepEqual(got, []string{rid}) {
		t.Fatalf("want = %v, %v", got, err)
	}
	if n, err := e.Import(ctx, c, "h", "/inbox/c1"); err != nil || n != 4 {
		t.Fatalf("import = %d, %v", n, err)
	}
	if err := e.Materialize(ctx, c, "h"); err != nil {
		t.Fatal(err)
	}
	if tr.targets[0].DataDir != "/data/c1" || tr.targets[0].Tree != "/tree" {
		t.Fatalf("target = %+v", tr.targets[0])
	}
}

func TestSyncEngineDaemonArgsCarryKeys(t *testing.T) {
	var cases = []struct {
		op   Op
		want map[string]any
	}{
		{exportOp{Head: "h", Outbox: "/o", Ledger: "l", MaxFrame: 7, keyArgs: hexKeys(testKeys())},
			map[string]any{"head": "h", "outbox": "/o", "ledger": "l", "max_frame": 7.0, "cell_key": testCellKey, "dedup": testDedup}},
		{publishedOp{Ledger: "l", Frames: []string{"/f"}},
			map[string]any{"ledger": "l", "frames": []any{"/f"}}},
		{wantOp{Head: "h", keyArgs: hexKeys(testKeys())},
			map[string]any{"head": "h", "cell_key": testCellKey, "dedup": testDedup}},
		{importOp{Head: "h", Inbox: "/i", keyArgs: hexKeys(testKeys())},
			map[string]any{"head": "h", "inbox": "/i", "cell_key": testCellKey, "dedup": testDedup}},
		{materializeOp{Head: "h"}, map[string]any{"head": "h"}},
	}
	for _, tc := range cases {
		var got map[string]any
		if err := json.Unmarshal(newRequest(Target{}, tc.op).Args, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s args = %v, want %v", tc.op.Verb(), got, tc.want)
		}
	}
}

func TestSyncEngineErrorsNameVerbAndCell(t *testing.T) {
	c := cell.Cell{ID: "cell-9"}
	boom := errors.New("engine refused")
	e := syncEngineOver(&scripted{
		errs:    map[string]error{"export": boom, "published": boom, "want": boom, "import": boom, "materialize": boom},
		replies: map[string]string{},
	})
	ctx := context.Background()
	_, exportErr := e.Export(ctx, c, "h")
	_, wantErr := e.Want(ctx, c, "h")
	_, importErr := e.Import(ctx, c, "h", "/i")
	for verb, err := range map[string]error{
		"export": exportErr, "want": wantErr, "import": importErr,
		"published": e.Published(ctx, c, nil), "materialize": e.Materialize(ctx, c, "h"),
	} {
		if err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), verb) || !strings.Contains(err.Error(), "cell-9") {
			t.Errorf("%s error = %v", verb, err)
		}
	}
}

func TestSyncEngineUnreadableAnswerNamesVerbAndCell(t *testing.T) {
	e := syncEngineOver(&scripted{replies: map[string]string{"want": "not json"}})
	_, err := e.Want(context.Background(), cell.Cell{ID: "c7"}, "h")
	if err == nil || !strings.Contains(err.Error(), "want") || !strings.Contains(err.Error(), "c7") {
		t.Fatalf("err = %v", err)
	}
}

func TestSyncEngineSpawnKeepsKeysOffArgvAndInEnv(t *testing.T) {
	var argv, environ []string
	run := func(_ context.Context, _ string, env []string, a ...string) ([]byte, error) {
		argv, environ = a, env
		return []byte(contractReplies["want"]), nil
	}
	e := syncEngineOver(Spawn{Binary: "engine", Run: run})
	if _, err := e.Want(context.Background(), cell.Cell{ID: "c1"}, "h"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(argv, " "), testCellKey) || strings.Contains(strings.Join(argv, " "), testDedup) {
		t.Fatalf("a key is on argv: %v", argv)
	}
	if !contains(environ, "FURROW_CELL_KEY="+testCellKey) || !contains(environ, "FURROW_DEDUP_SECRET="+testDedup) {
		t.Fatalf("keys missing from env")
	}
	if want := []string{"engine", "--json", "want", "--head", "h"}; !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v", argv)
	}
}

func TestSyncEngineSpawnLeavesKeylessVerbsWithoutKeys(t *testing.T) {
	var environ []string
	run := func(_ context.Context, _ string, env []string, _ ...string) ([]byte, error) {
		environ = env
		return []byte(`{"snapshot":"s"}`), nil
	}
	e := syncEngineOver(Spawn{Binary: "engine", Run: run})
	if err := e.Materialize(context.Background(), cell.Cell{ID: "c1"}, "h"); err != nil {
		t.Fatal(err)
	}
	for _, kv := range environ {
		if strings.HasPrefix(kv, "FURROW_CELL_KEY=") && strings.Contains(kv, testCellKey) {
			t.Fatalf("materialize carried a key")
		}
	}
}

func TestSyncEngineExportArgsSpellTheContract(t *testing.T) {
	op := exportOp{Head: "h", Outbox: "/o", Ledger: "l", MaxFrame: 5}
	want := []string{"--json", "export", "--head", "h", "--outbox", "/o", "--ledger", "l", "--max-frame", "5"}
	if !reflect.DeepEqual(op.Args(), want) {
		t.Fatalf("args = %v", op.Args())
	}
	if got := (exportOp{Head: "h", Outbox: "/o", Ledger: "l"}).Args(); len(got) != 8 {
		t.Fatalf("default frame size must be left to the engine: %v", got)
	}
	if got := (publishedOp{Ledger: "l", Frames: []string{"/a", "/b"}}).Args(); !reflect.DeepEqual(got, []string{"--json", "published", "--ledger", "l", "/a", "/b"}) {
		t.Fatalf("published args = %v", got)
	}
}

func TestLedgerNameFollowsStore(t *testing.T) {
	a := LedgerName("http://one", "id_x")
	if len(a) != 16 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Fatalf("ledger %q is not 16 lowercase hex", a)
	}
	if a != LedgerName("http://one", "id_x") {
		t.Fatal("ledger is not stable")
	}
	if a == LedgerName("http://two", "id_x") || a == LedgerName("http://one", "id_y") {
		t.Fatal("a new store or identity must name a new ledger")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
