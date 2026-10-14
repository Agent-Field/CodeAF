package draft

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func runMain(t *testing.T, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Main(args, strings.NewReader(stdin), &o, &e)
	return code, o.String(), e.String()
}

func TestMainExitCodes(t *testing.T) {
	dir := shortDir(t)
	t.Setenv(DirEnv, dir)
	tests := []struct {
		name  string
		stdin string
		args  []string
		code  int
		out   string // substring of stdout
		err   string // substring of stderr
	}{
		{"ok", "", []string{"name", "CRM"}, ExitOK, "ok Name · CRM", ""},
		{"refused", "", []string{"plan", "ready"}, ExitRefused, "", "error: the plan is not ready"},
		{"usage", "", []string{"name"}, ExitUsage, "", "error: expected 1 argument(s)"},
		{"unknown verb", "", []string{"nope"}, ExitUsage, "", `error: unknown verb "nope"`},
		{"phase is not a worker verb", "", []string{"phase", "done"}, ExitUsage, "", `unknown verb "phase"`},
		{"no builder", "", []string{"check"}, ExitNoBuilder, "", "error: the Builder is not running"},
		{"no builder for done", "", []string{"done", "all built"}, ExitNoBuilder, "", "error: the Builder is not running"},
		{"server verb usage comes first", "", []string{"try"}, ExitUsage, "", "usage: codeaf app try <run-id>"},
		{"show", "", []string{"show"}, ExitOK, "name: CRM", ""},
		{"show a missing file", "", []string{"show", "app"}, ExitRefused, "", "error: app.json is not made yet"},
		{"catalog", "", []string{"catalog"}, ExitOK, "RecordList [records]", ""},
		{"schema", "", []string{"schema", "field"}, ExitOK, "codeaf app field <record>", ""},
		{"help", "", []string{"help"}, ExitOK, "codeaf app plan ready", ""},
		{"no verb", "", nil, ExitUsage, "codeaf app plan ready", ""},
	}
	for _, tc := range tests {
		code, out, errOut := runMain(t, tc.stdin, tc.args...)
		if code != tc.code || !strings.Contains(out, tc.out) || !strings.Contains(errOut, tc.err) {
			t.Errorf("%s: code %d out %q err %q; want code %d out~%q err~%q", tc.name, code, out, errOut, tc.code, tc.out, tc.err)
		}
	}
}

func TestMainBodyComesFromStdin(t *testing.T) {
	t.Setenv(DirEnv, shortDir(t))
	for _, line := range planned() {
		if code, _, e := runMain(t, line.body, append(strings.Fields(line.verb), line.args...)...); code != 0 {
			t.Fatalf("%s %v: %s", line.verb, line.args, e)
		}
	}
	code, out, _ := runMain(t, screenWith(`["name"]`), "screen", "pipeline")
	if code != 0 || out != "ok Screen · Pipeline\n" {
		t.Fatalf("screen: %d %q", code, out)
	}
	if _, shown, _ := runMain(t, "", "show", "screen", "pipeline"); !strings.Contains(shown, `"screen": "pipeline"`) && !strings.Contains(shown, `"screen":"pipeline"`) {
		t.Fatalf("show screen = %s", shown)
	}
}

func TestMainFindsTheDraftFromPlandbThenFromTheWorkingDirectory(t *testing.T) {
	dir := shortDir(t)
	t.Setenv(DirEnv, "")
	t.Setenv("PLANDB_DB", filepath.Join(dir, "plan.db"))
	if code, _, e := runMain(t, "", "name", "FromPlandb"); code != 0 {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(dir, JournalFile)); err != nil {
		t.Fatal("the journal is not beside plan.db")
	}
	t.Setenv("PLANDB_DB", "")
	sub := filepath.Join(dir, "work", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if code, out, e := runMain(t, "", "show"); code != 0 || !strings.Contains(out, "name: FromPlandb") {
		t.Fatalf("walk up: %d %q %q", code, out, e)
	}
	t.Chdir(t.TempDir())
	if code, _, e := runMain(t, "", "show"); code != ExitUsage || !strings.Contains(e, "no app draft found") {
		t.Fatalf("no draft: %d %q", code, e)
	}
}

func TestServerVerbsReachTheRunningBuilder(t *testing.T) {
	dir := shortDir(t)
	t.Setenv(DirEnv, dir)
	var got Request
	stop, err := Serve(context.Background(), dir, func(_ context.Context, r Request) Reply {
		got = r
		if r.Verb == "try" {
			return Reply{Text: "error: run follow-up fails on step draft"}
		}
		return Reply{OK: true, Text: "ok checked\nline two\n"}
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, SocketFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v", info, err)
	}
	code, out, _ := runMain(t, "", "check", "--json")
	if code != 0 || out != "ok checked\nline two\n" || got.Verb != "check" || strings.Join(got.Args, " ") != "--json" {
		t.Fatalf("check: %d %q %+v", code, out, got)
	}
	if code, _, e := runMain(t, "", "try", "follow-up"); code != ExitRefused || e != "error: run follow-up fails on step draft\n" {
		t.Fatalf("try: %d %q", code, e)
	}
	stop()
	if _, err := os.Stat(filepath.Join(dir, SocketFile)); !os.IsNotExist(err) {
		t.Fatal("stop left the socket behind")
	}
	if code, _, _ := runMain(t, "", "check"); code != ExitNoBuilder {
		t.Fatalf("after stop: exit %d", code)
	}
}

func TestServeStopsWhenItsContextEnds(t *testing.T) {
	dir := shortDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	stop, err := Serve(ctx, dir, func(context.Context, Request) Reply { return Reply{OK: true} })
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	stop() // waits for the context's own stop to finish
	if _, err := os.Stat(filepath.Join(dir, SocketFile)); !os.IsNotExist(err) {
		t.Fatal("socket remains")
	}
}

// Two stores on the same folder stand for the worker's CLI and the server:
// the lock must give every append its own seq and keep every line whole.
func TestConcurrentWritersKeepTheJournalWhole(t *testing.T) {
	dir := t.TempDir()
	const writers, each = 6, 12
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(dir)
			if err != nil {
				t.Error(err)
				return
			}
			for i := 0; i < each; i++ {
				id := "w" + strconv.Itoa(w) + "-" + strconv.Itoa(i)
				if _, err := s.Append(AuthorBuilder, "plan data", []string{id, "line " + id}, ""); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	s, _ := Open(dir)
	entries, err := s.Entries(0)
	if err != nil || len(entries) != writers*each {
		t.Fatalf("entries = %d, err %v", len(entries), err)
	}
	for i, e := range entries {
		if e.Seq != i+1 {
			t.Fatalf("entry %d has seq %d", i, e.Seq)
		}
	}
	if st, _ := s.State(); len(st.Plan.Data) != writers*each {
		t.Fatalf("data lines = %d", len(st.Plan.Data))
	}
}

func TestEntriesIgnoresALineStillBeingWritten(t *testing.T) {
	s := openTemp(t)
	play(t, s, []cmd{c("name", "CRM")})
	f, _ := os.OpenFile(filepath.Join(s.Dir(), JournalFile), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"seq":2,"verb":"na`)
	f.Close()
	if entries, err := s.Entries(0); err != nil || len(entries) != 1 {
		t.Fatalf("entries = %d, err %v", len(entries), err)
	}
}

func TestRenderOfAHalfDraft(t *testing.T) {
	d := Defaults{AppID: "crm", Ask: "A CRM", AgentModel: "m", BudgetMonth: 20, Kit: 2}
	s := openTemp(t)
	st, _ := s.State()
	if files := Render(st, d); len(files) != 1 || files["plan.json"] == nil {
		t.Fatalf("empty draft renders %v", files.Paths())
	}
	play(t, s, planned())
	st, _ = s.State()
	files := Render(st, d)
	if got := strings.Join(files.Paths(), " "); got != "app.json plan.json" {
		t.Fatalf("files = %s", got)
	}
	app := string(files["app.json"])
	for _, want := range []string{`"deal": "rw"`, `"company": "rw"`, `"name": "CRM agent"`, `"per_run": 0.5`} {
		if !strings.Contains(app, want) {
			t.Errorf("app.json lacks %s:\n%s", want, app)
		}
	}
}

func TestVerbsFolderIsFreeOfServices(t *testing.T) {
	banned := []string{"svc", "store", "records", "check", "builder"}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				for _, b := range banned {
					if strings.HasPrefix(path, "github.com/Agent-Field/codeaf/internal/apps/"+b) {
						t.Errorf("%s imports %s: the draft store must not reach a service", name, path)
					}
				}
			}
		}
	}
}

func TestSocketOfADeepFolderMovesToTheTempFolder(t *testing.T) {
	dir := filepath.Join(shortDir(t), strings.Repeat("d", 60), strings.Repeat("e", 60))
	if err := os.MkdirAll(dir, 0o755); err != nil || len(dir) <= 110 {
		t.Fatalf("setup: %v, %d bytes", err, len(dir))
	}
	t.Setenv(DirEnv, dir)
	stop, err := Serve(context.Background(), dir, func(context.Context, Request) Reply { return Reply{OK: true, Text: "ok deep"} })
	if err != nil {
		t.Fatal(err)
	}
	addr, err := os.ReadFile(filepath.Join(dir, AddrFile))
	if err != nil || filepath.Dir(string(addr)) != os.TempDir() || !strings.Contains(string(addr), "codeaf-b-") {
		t.Fatalf("addr = %q, %v", addr, err)
	}
	if info, _ := os.Stat(filepath.Join(dir, AddrFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("addr mode = %v", info.Mode())
	}
	if code, out, _ := runMain(t, "", "check"); code != 0 || out != "ok deep\n" {
		t.Fatalf("check: %d %q", code, out)
	}
	stop()
	for _, p := range []string{string(addr), filepath.Join(dir, AddrFile)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", p)
		}
	}
	if code, _, _ := runMain(t, "", "check"); code != ExitNoBuilder {
		t.Fatalf("after stop: %d", code)
	}
}

func TestTintTheWorkersChoiceWinsOverTheDefault(t *testing.T) {
	s := openTemp(t)
	play(t, s, planned())
	st, _ := s.State()
	d := Defaults{AppID: "crm", Tint: "tide"}
	files := Render(st, d)
	if !strings.Contains(string(files["app.json"]), `"tint": "tide"`) || !strings.Contains(string(files["plan.json"]), `"tint": "tide"`) {
		t.Fatal("Defaults.Tint was not written to app.json and plan.json")
	}
	play(t, s, []cmd{c("tint", "sage")})
	st, _ = s.State()
	files = Render(st, d)
	if !strings.Contains(string(files["app.json"]), `"tint": "sage"`) || !strings.Contains(string(files["plan.json"]), `"tint": "sage"`) {
		t.Fatal("the worker's tint did not win")
	}
}
