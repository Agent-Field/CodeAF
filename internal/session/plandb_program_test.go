package session

// A program's task page, read off the two records a program's run leaves in the
// task's own folder: the program record the worker writes at the program's
// hello, and the conversation log the model API appends a turn to per call.
// Every fixture writes them through the contract's own doors
// (delegate.WriteProgram, delegate.AppendTurn) and seeds the store through its
// own API, exactly as the run does; no model is called and no program runs.

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// programPageFixture is a store holding one task that was handed to
// senior-dev: its record, a conversation of three calls — one answered, one
// refused by codeaf, one still in flight — the stage the worker published as
// its live step, and two spend rows the model API banked, one per call.
func programPageFixture(t *testing.T) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha", Description: "rewrite the auth middleware"})
	folder := plandb.TaskDir(dir, "alpha")
	if err := delegate.WriteProgram(folder, delegate.ProgramRecord{Name: "senior-dev", Stages: []string{"intake", "implement", "verification"}, CeilingUSD: 5}); err != nil {
		t.Fatalf("write the program record: %v", err)
	}
	began := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	for _, turn := range []delegate.Turn{
		// The first call, as it is written when it starts and again when it ends.
		{Seq: 1, Started: began, Model: "deepseek/deepseek-v4-flash", Sent: []delegate.Said{{Role: "system", Text: "you are senior-dev"}, {Role: "user", Text: "rewrite the auth middleware"}}},
		{Seq: 1, Started: began, Ended: began.Add(4 * time.Second), Model: "deepseek/deepseek-v4-flash",
			Sent:  []delegate.Said{{Role: "system", Text: "you are senior-dev"}, {Role: "user", Text: "rewrite the auth middleware"}},
			Reply: "I'll read the middleware first.", Calls: []delegate.ToolUse{{Name: "read", Args: `{"filePath":"internal/auth/middleware.go"}`}},
			TokensIn: 1200, TokensOut: 40, CostUSD: 0.01},
		{Seq: 2, Started: began.Add(5 * time.Second), Model: "deepseek/deepseek-v4-flash", Refused: "the run's dollar ceiling is reached"},
		{Seq: 3, Started: began.Add(6 * time.Second), Model: "deepseek/deepseek-v4-flash", Sent: []delegate.Said{{Role: "tool", Tool: "read", Text: "package auth\n\nfunc Middleware() {}"}}},
	} {
		if err := delegate.AppendTurn(folder, turn); err != nil {
			t.Fatalf("append a turn: %v", err)
		}
	}
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store: %v", err)
	}
	if err := store.SetLive("alpha", 2, "senior-dev: implement · running"); err != nil {
		t.Fatalf("publish the stage: %v", err)
	}
	for _, usd := range []float64{0.01, 0.02} {
		if err := store.AddSpend("alpha", "deepseek/deepseek-v4-flash", "work", usd, 10, 20); err != nil {
			t.Fatalf("bank a call: %v", err)
		}
	}
	_ = store.Close()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	return agent, dir
}

// A PROGRAM'S PAGE CARRIES ITS PROGRAM, ITS CONVERSATION AND THE STAGE IT IS IN,
// and the live step the page used to leave unset. The name and the stages are
// the record's; the turns are the log's, one per call, each as its latest
// record says; the calls counted are the ones that reached a model, the call in
// flight included and codeaf's refusal left out; the spend is the store's own
// spend rows, which is what a page read while the run is still going shows.
func TestAProgramsPageCarriesItsConversationStageAndLiveStep(t *testing.T) {
	agent, _ := programPageFixture(t)
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("the program's task answered no page")
	}
	program := page.Program
	if program == nil {
		t.Fatal("a task whose folder holds a program record and a conversation read as no program's")
	}
	if program.Name != "senior-dev" || strings.Join(program.Stages, ",") != "intake,implement,verification" {
		t.Fatalf("program = %q with stages %v, want senior-dev and its three stages", program.Name, program.Stages)
	}
	if len(program.Turns) != 3 {
		t.Fatalf("the page carries %d turns, want the three calls once each", len(program.Turns))
	}
	first := program.Turns[0]
	if first.Ended.IsZero() || first.Reply != "I'll read the middleware first." || len(first.Calls) != 1 || first.Calls[0].Name != "read" {
		t.Fatalf("the first call reads %+v, want its ending record with the reply and the call", first)
	}
	if program.Turns[1].Refused == "" {
		t.Fatalf("the refused call lost its refusal: %+v", program.Turns[1])
	}
	if last := program.Turns[2]; !last.InFlight() || len(last.Sent) != 1 || last.Sent[0].Tool != "read" {
		t.Fatalf("the call in flight reads %+v, want it open with what the program sent", last)
	}
	if program.CeilingUSD != 5 {
		t.Fatalf("ceiling = %v, want the 5 the program record carries", program.CeilingUSD)
	}
	if program.Calls != 2 || program.Earlier != 0 {
		t.Fatalf("calls = %d, earlier = %d; want 2 calls that reached a model and nothing earlier", program.Calls, program.Earlier)
	}
	if page.Live.Step != 2 || page.Live.Command != "senior-dev: implement · running" {
		t.Fatalf("the page's live step = %+v, want the stage the worker published", page.Live)
	}
	if page.Row.Program != "senior-dev" || page.Row.Stage != "implement" {
		t.Fatalf("the page's row names %q in stage %q, want senior-dev in implement", page.Row.Program, page.Row.Stage)
	}
	if math.Abs(page.Row.USD-0.03) > 1e-9 {
		t.Fatalf("the page's spend = %v, want the 0.03 its spend rows carry", page.Row.USD)
	}

	// AND THE SIDE LIST'S ROW SAYS THE SAME, off the same read.
	row := planRowByID(t, agent.PlanTasks(), "t-alpha")
	if row.Program != "senior-dev" || row.Stage != "implement" || math.Abs(row.USD-0.03) > 1e-9 {
		t.Fatalf("the row reads program %q, stage %q, spend %v", row.Program, row.Stage, row.USD)
	}
}

// AN ORDINARY TASK IS NO PROGRAM'S. A task with neither record in its folder
// carries no program on its page and no program or stage on its row, so every
// page but a program's draws exactly what it drew — and its live step, which
// is a command, is never read as a stage.
func TestAnOrdinaryTaskCarriesNoProgram(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLive("alpha", 4, "go test: ./internal/api · -run TestX"); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("the task answered no page")
	}
	if page.Program != nil || page.Row.Program != "" || page.Row.Stage != "" {
		t.Fatalf("an ordinary task reads as a program's: page %+v, row program %q stage %q", page.Program, page.Row.Program, page.Row.Stage)
	}
	if page.Live.Step != 4 {
		t.Fatalf("an ordinary page's live step = %+v, want the one its row carries", page.Live)
	}
}

// A LONG RUN'S PAGE CARRIES ITS NEWEST CALLS AND COUNTS THE REST. The page
// opens at its bottom edge, so the newest calls are the ones it holds; the ones
// before them are a number the page says, and the call count is the whole
// log's, however long the run has gone.
func TestAProgramsPageCarriesTheNewestCallsAndCountsTheRest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})
	folder := plandb.TaskDir(dir, "alpha")
	if err := delegate.WriteProgram(folder, delegate.ProgramRecord{Name: "senior-dev"}); err != nil {
		t.Fatal(err)
	}
	began := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	total := planProgramTurns + 5
	for seq := 1; seq <= total; seq++ {
		at := began.Add(time.Duration(seq) * time.Second)
		if err := delegate.AppendTurn(folder, delegate.Turn{Seq: seq, Started: at, Ended: at.Add(time.Second), Model: "m", Reply: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok || page.Program == nil {
		t.Fatal("the program's task answered no program page")
	}
	if got := len(page.Program.Turns); got != planProgramTurns {
		t.Fatalf("the page carries %d turns, want the newest %d", got, planProgramTurns)
	}
	if page.Program.Earlier != 5 || page.Program.Calls != total {
		t.Fatalf("earlier = %d, calls = %d; want 5 earlier and %d calls", page.Program.Earlier, page.Program.Calls, total)
	}
	if first := page.Program.Turns[0].Seq; first != 6 {
		t.Fatalf("the first call the page carries is %d, want 6", first)
	}
}

// EVERY TEXT A PAGE CARRIES IS ITS HEAD, AND THE RUN'S COPY IS TAKEN OUT OF IT.
// A program's tools name files by their absolute path inside the copy, and a
// row fitted from the right drew the copy and cut the file off; a reply of a
// hundred lines crossed the wire every beat to have one line drawn. The record
// on disk is not touched: the page is a reading of it.
func TestAProgramsTurnsAreCutToTheirHeadsAndLeaveTheCopyOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})
	folder := plandb.TaskDir(dir, "alpha")
	if err := delegate.WriteProgram(folder, delegate.ProgramRecord{Name: "senior-dev"}); err != nil {
		t.Fatal(err)
	}
	// THE CONVERSATION HAS A FOLDER OF ITS OWN, and its copies are cut under it:
	// an ended run's copy is one folder down from there.
	place := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Place = Place{Dir: place} })
	armPlanStore(t, agent, path, "chat-a")
	if agent.treesDir() == "" {
		t.Fatal("the conversation has no folder of copies to cut a run's copy under")
	}
	copyDir := filepath.Join(agent.treesDir(), "7")
	began := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	long := strings.Repeat("word ", 200)
	if err := delegate.AppendTurn(folder, delegate.Turn{
		Seq: 1, Started: began, Ended: began.Add(time.Second), Model: "m",
		Sent:  []delegate.Said{{Role: "tool", Tool: "bash", Text: "\n\n  ok  " + copyDir + "/internal/auth\t0.3s\nPASS\n"}},
		Reply: "\nFirst line of the answer.\nSecond line.\n" + long,
		Calls: []delegate.ToolUse{{Name: "edit", Args: `{"filePath":"` + copyDir + `/internal/auth/middleware.go","oldString":"x"}`}},
	}); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(filepath.Join(folder, delegate.ConversationFile))
	if err != nil {
		t.Fatal(err)
	}
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok || page.Program == nil || len(page.Program.Turns) != 1 {
		t.Fatalf("the program's page = %+v", page.Program)
	}
	turn := page.Program.Turns[0]
	if turn.Reply != "First line of the answer." {
		t.Fatalf("the reply's head = %q, want its first line that says anything", turn.Reply)
	}
	if got := turn.Sent[0].Text; got != "ok  internal/auth\t0.3s" {
		t.Fatalf("the tool result's head = %q, want its first line with the copy taken out", got)
	}
	if got := turn.Calls[0].Args; strings.Contains(got, copyDir) || !strings.Contains(got, `"internal/auth/middleware.go"`) {
		t.Fatalf("the call's arguments = %q, want the file named inside the copy", got)
	}
	after, err := os.ReadFile(filepath.Join(folder, delegate.ConversationFile))
	if err != nil || string(after) != string(onDisk) {
		t.Fatalf("reading the page changed the record on disk (err %v)", err)
	}
	if len(turn.Reply) > planProgramHead {
		t.Fatalf("a head is %d bytes, over the %d a page carries", len(turn.Reply), planProgramHead)
	}
}

// THE LIVE RUN NAMES ITS PROGRAM BEFORE THE PROGRAM HAS SAID HELLO. A run is
// published the moment it starts and its program writes its record a moment
// later; the row read in between wears the name the run was handed, and once
// the record is there it is the record that answers.
func TestTheLiveRunNamesItsProgramBeforeTheRecordIsOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	agent.beltMu.Lock()
	agent.beltRun = &beltRun{root: "alpha", delegate: &delegate.Delegate{Name: "senior-dev"}}
	agent.beltMu.Unlock()
	t.Cleanup(func() {
		agent.beltMu.Lock()
		agent.beltRun = nil
		agent.beltMu.Unlock()
	})
	if row := planRowByID(t, agent.PlanTasks(), "t-alpha"); row.Program != "senior-dev" {
		t.Fatalf("the live run's row names %q before the hello, want senior-dev", row.Program)
	}
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok || page.Program == nil || page.Program.Name != "senior-dev" || len(page.Program.Turns) != 0 {
		t.Fatalf("the live run's page before the hello = %+v", page.Program)
	}
	if other := planRowByID(t, agent.PlanTasks(), "t-"+planRootID); other.Program != "" {
		t.Fatalf("a task the program was not handed names %q", other.Program)
	}
}

// THE STAGE IS THE PROGRAM'S PHASE AND NOTHING ELSE: the worker's label without
// the program's name in front and without the status behind, and nothing for a
// task no program runs or a program with no step live.
func TestTheStageIsReadOffTheProgramsLiveStep(t *testing.T) {
	for _, tc := range []struct {
		name, label, want string
	}{
		{"senior-dev", "senior-dev: implement · running", "implement"},
		{"senior-dev", "senior-dev: verification", "verification"},
		{"senior-dev", "implement · submitted", "implement"},
		{"senior-dev", "", ""},
		{"", "senior-dev: implement · running", ""},
	} {
		if got := planProgramStage(tc.name, plandb.LiveStep{Step: 1, Command: tc.label}); got != tc.want {
			t.Errorf("planProgramStage(%q, %q) = %q, want %q", tc.name, tc.label, got, tc.want)
		}
	}
}

// THE COPY IS STRIPPED WHERE IT IS A COPY AND NOWHERE ELSE: the live copy and
// any folder one step under the conversation's folder of copies, and never a
// path that merely starts at that folder.
func TestStripTakesOnlyTheRunsCopyOut(t *testing.T) {
	copies := planRunCopies{live: "/w/trees/9", root: "/w/trees"}
	for _, tc := range []struct{ in, want string }{
		{"/w/trees/9/a.go and /w/trees/3/b.go", "a.go and b.go"},
		{"cat /w/trees/README", "cat /w/trees/README"},
		{`{"path":"/w/trees/my copy/x"}`, `{"path":"/w/trees/my copy/x"}`},
		{"/elsewhere/x.go", "/elsewhere/x.go"},
	} {
		if got := copies.strip(tc.in); got != tc.want {
			t.Errorf("strip(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
