//go:build !windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/praf"
	"github.com/Agent-Field/codeaf/internal/secaf"
	"github.com/Agent-Field/codeaf/internal/seniordev"
	"github.com/Agent-Field/codeaf/internal/seniordev/app"
	"github.com/Agent-Field/codeaf/internal/session"
)

// programViewFromLog is a program's task view built the way the engine builds
// it: lines written through the log's own door, read back and shown through the
// program's own reader.
func programViewFromLog(t *testing.T, program delegate.Delegate, stages []string, lines ...delegate.Action) *session.AppProgramView {
	t.Helper()
	dir := t.TempDir()
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	for i, line := range lines {
		line.At = at.Add(time.Duration(i) * time.Second)
		if err := delegate.AppendAction(dir, line); err != nil {
			t.Fatal(err)
		}
	}
	logged, err := delegate.ReadActions(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	read := program.Reader()
	calls := 3
	view := &session.AppProgramView{Name: program.Name, Listens: program.Listens, CeilingUSD: 5, Calls: &calls, Models: []string{"m/one"},
		Read: session.AppProgramReadComplete}
	standing := ""
	for _, stage := range stages {
		standing = session.AppProgramStageWord(program, stage, standing)
		view.Stages = append(view.Stages, session.AppProgramStage{Name: stage, Word: standing})
	}
	for _, action := range logged {
		if action.Kind == delegate.ActionStage {
			view.LastStage = action.Stage
		}
		if shown, ok := read(action); ok {
			view.Actions = append(view.Actions, shown)
			if action.Kind == delegate.ActionEnd {
				ending := shown
				view.Ending, view.EndStatus = &ending, action.Status
			}
		}
	}
	return view
}

// words groups consecutive equal words, the way the app does.
func words(stages []appwire.ProgramStage) []string {
	var out []string
	for _, stage := range stages {
		if len(out) == 0 || out[len(out)-1] != stage.Word {
			out = append(out, stage.Word)
		}
	}
	return out
}

// THE HELLO'S STAGE COUNT IS THE PROGRAM'S OWN, and the words merge only where
// they are consecutive: nothing here is forced to six.
func TestProgramStagesFollowEachRealProgramsHello(t *testing.T) {
	for _, c := range []struct {
		program delegate.Delegate
		stages  []string
	}{{seniordev.Program, app.Stages}, {secaf.Program, secaf.Stages}, {praf.Program, praf.Stages}} {
		wire := appTaskProgram(programViewFromLog(t, c.program, c.stages))
		if len(wire.Stages) != len(c.stages) {
			t.Fatalf("%s: %d stages on the wire, hello named %d", c.program.Name, len(wire.Stages), len(c.stages))
		}
		for i, stage := range wire.Stages {
			if stage.Name != c.stages[i] || (stage.Word == "" && i > 0) {
				t.Errorf("%s stage %d = %+v", c.program.Name, i, stage)
			}
		}
		t.Logf("%s: %d stages read as %d consecutive words %v", c.program.Name, len(wire.Stages), len(words(wire.Stages)), words(wire.Stages))
		if c.program.Name == "senior-dev" && len(words(wire.Stages)) == len(wire.Stages) {
			t.Errorf("senior-dev's consecutive stages with one word did not merge: %v", words(wire.Stages))
		}
	}
}

func TestProgramOnTheWireIsBoundedRedactedAndHonestlyPartial(t *testing.T) {
	if appwire.ProgramActionsMax != session.AppProgramActionsMax {
		t.Fatalf("the wire bound %d and the engine's %d drifted", appwire.ProgramActionsMax, session.AppProgramActionsMax)
	}
	lines := []delegate.Action{delegate.StageAction(time.Time{}, delegate.StageRecord{Stage: "intake", Status: "captured"})}
	for i := 0; i < appwire.ProgramActionsMax+25; i++ {
		lines = append(lines, delegate.StepAction(time.Time{}, delegate.StepRecord{Tool: "read", Step: "explore", Command: "read file.go key sk-abcdefghijklmnopqrstuvwxyz123456"}))
	}
	view := programViewFromLog(t, seniordev.Program, app.Stages, lines...)
	view.EarlierActions = 7
	wire := appTaskProgram(view)
	if len(wire.Actions) != appwire.ProgramActionsMax || wire.EarlierActions != 7+len(view.Actions)-appwire.ProgramActionsMax || wire.EarlierActions < 26 {
		t.Fatalf("%d actions, %d earlier", len(wire.Actions), wire.EarlierActions)
	}
	secret := "sk-abcdefghijklmnopqrstuvwxyz123456"
	view.Stages = []session.AppProgramStage{{Name: secret, Word: secret}}
	view.Ending = &delegate.Shown{Text: "done", Reason: secret, Ending: &delegate.EndFacts{Claim: secret, Reports: []string{secret}}}
	view.Actions[0].Reason, view.Actions[0].Model = secret, secret
	wire = appTaskProgram(view)
	raw, _ := json.Marshal(wire)
	if strings.Contains(string(raw), "sk-abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("a secret reached the wire: %s", raw)
	}
	if wire.StartedAt != nil || appTaskProgram(nil) != nil {
		t.Fatalf("an unstarted run claims a clock: %+v", wire)
	}
	// ONLY A COMPLETE READ COUNTS OR POINTS BACK. A partial read carries what it
	// read and nothing about how much more there is; an engine that sent no read
	// state — one from before it — has its process's work dropped, not passed on
	// as the current run's.
	view.Before = "cursor"
	if wire := appTaskProgram(view); wire.Before != "cursor" || wire.Read != appwire.ProgramReadComplete || wire.Calls == nil || *wire.Calls != 3 {
		t.Fatalf("complete: %+v", wire)
	}
	view.Read, view.ReadCut = session.AppProgramReadPartial, delegate.ReadCutBudget
	if wire := appTaskProgram(view); wire.Before != "" || wire.EarlierActions != 0 || len(wire.Actions) != appwire.ProgramActionsMax || wire.ReadCut != appwire.ProgramCutBudget {
		t.Fatalf("partial: before %q earlier %d actions %d cut %q", wire.Before, wire.EarlierActions, len(wire.Actions), wire.ReadCut)
	}
	view.Read = ""
	old := appTaskProgram(view)
	if old.Read != appwire.ProgramReadUnknown || old.Actions != nil || old.Calls != nil || old.LastStage != "" || old.Ending != nil || old.EarlierActions != 0 || old.Name != "senior-dev" {
		t.Fatalf("an older engine's program = %+v", old)
	}
}

// THE ENGINE'S CUT WORDS ARE THE WIRE'S: a partial read's reason crosses as the
// word the protocol names.
func TestProgramReadWordsAreTheWires(t *testing.T) {
	for engine, wire := range map[string]string{
		session.AppProgramReadComplete: appwire.ProgramReadComplete, session.AppProgramReadPartial: appwire.ProgramReadPartial,
		session.AppProgramReadUnknown: appwire.ProgramReadUnknown, delegate.ReadCutBudget: appwire.ProgramCutBudget,
		delegate.ReadCutDamaged: appwire.ProgramCutDamaged, delegate.ReadCutUnreadable: appwire.ProgramCutUnreadable,
		delegate.ReadCutDeadline: appwire.ProgramCutDeadline,
	} {
		if engine != wire {
			t.Errorf("engine %q, wire %q", engine, wire)
		}
	}
}

// THROUGH THE REAL SERVER: tasks.get answers the program with its typed ending,
// a count nobody said absent, and a task with no program answers none.
func TestTasksGetCarriesTheProgramOverTheRealServer(t *testing.T) {
	end := delegate.EndAction(time.Time{}, delegate.Terminal{Status: delegate.StatusPass, Message: "7 findings, 2 blocking",
		Data: map[string]json.RawMessage{"findings": json.RawMessage(`7`), "blocking": json.RawMessage(`2`), "reports": json.RawMessage(`["review-report.md"]`), "transcript": json.RawMessage(`"x"`)}})
	view := programViewFromLog(t, praf.Program, praf.Stages,
		delegate.StageAction(time.Time{}, delegate.StageRecord{Stage: praf.Stages[1], Status: "running"}), end)
	conv := &fakeConversation{appTasks: []session.AppTaskView{
		{ID: "4", Title: "review the change", State: "done", Program: view},
		{ID: "5", Title: "plain", State: "done"},
	}}
	backend, _, agent := workBackend(t, conv)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	get := func(n int, id string) map[string]any {
		t.Helper()
		if _, err := io.WriteString(inW, `{"id":`+string(rune('0'+n))+`,"method":"tasks.get","params":{"id":"`+id+`"}}`+"\n"); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() {
			t.Fatal("the wire ended")
		}
		var out struct {
			Result struct {
				Page map[string]any `json:"page"`
			} `json:"result"`
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &out); err != nil || out.Error != nil {
			t.Fatalf("answer %s (%v)", scanner.Text(), err)
		}
		return out.Result.Page
	}
	page := get(1, agent.ID+":4")
	program, _ := page["program"].(map[string]any)
	ending, _ := program["ending"].(map[string]any)
	facts, _ := ending["facts"].(map[string]any)
	if program["name"] != "review" || program["last_stage"] != praf.Stages[1] || ending["status"] != delegate.StatusPass || ending["text"] != "7 findings, 2 blocking" ||
		facts["findings"] != float64(7) || facts["blocking"] != float64(2) {
		t.Fatalf("program = %v", program)
	}
	if _, said := facts["confirmed"]; said {
		t.Fatalf("a count nobody said is on the wire: %v", facts)
	}
	if _, leaked := facts["transcript"]; leaked || len(program["stages"].([]any)) != len(praf.Stages) {
		t.Fatalf("facts = %v", facts)
	}
	if program["read"] != appwire.ProgramReadComplete || program["calls"] != float64(3) {
		t.Fatalf("read %v calls %v", program["read"], program["calls"])
	}
	if plain := get(2, agent.ID+":5"); plain["program"] != nil {
		t.Fatalf("an ordinary task carries a program: %v", plain["program"])
	}
}

// pagingConversation is a conversation that pages its programs' actions.
type pagingConversation struct {
	*fakeConversation
	pages map[string]session.AppProgramActionsPage
}

func (p *pagingConversation) AppProgramActions(id, before string) (session.AppProgramActionsPage, bool, error) {
	page, ok := p.pages[id+"@"+before]
	return page, ok, nil
}

// THROUGH THE REAL SERVER: tasks.program_actions answers an older page with its
// own read state and the cursor before it, a cursor of an earlier run is a
// conflict, and a task with no program is not found.
func TestTasksProgramActionsPagesOverTheRealServer(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwxyz123456"
	conv := &pagingConversation{fakeConversation: &fakeConversation{}, pages: map[string]session.AppProgramActionsPage{
		"4@c.60": {Read: session.AppProgramReadComplete, Actions: []delegate.Shown{{Text: "read a.go " + secret}}, EarlierActions: 59, Before: "c.59"},
		"4@gone": {Gone: true},
	}}
	var mu sync.Mutex
	var opens, releases int
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		conn, err := fakeOpener(conv.fakeConversation, &opens, &releases, &mu)(ask)
		if conn != nil {
			conn.Conv = conv
		}
		return conn, err
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	ask := func(n int, id, before string) (map[string]any, map[string]any) {
		t.Helper()
		if _, err := io.WriteString(inW, `{"id":`+string(rune('0'+n))+`,"method":"tasks.program_actions","params":{"id":"`+id+`","before":"`+before+`"}}`+"\n"); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() {
			t.Fatal("the wire ended")
		}
		var out struct {
			Result map[string]any `json:"result"`
			Error  map[string]any `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Result, out.Error
	}
	page, failed := ask(1, agent.ID+":4", "c.60")
	if failed != nil || page["read"] != appwire.ProgramReadComplete || page["before"] != "c.59" || page["earlier_actions"] != float64(59) {
		t.Fatalf("page = %v (%v)", page, failed)
	}
	if raw, _ := json.Marshal(page); strings.Contains(string(raw), secret) {
		t.Fatalf("a secret crossed: %s", raw)
	}
	if _, failed := ask(2, agent.ID+":4", "gone"); failed == nil || failed["code"] != appwire.CodeConflict {
		t.Fatalf("an earlier run's cursor answered %v", failed)
	}
	if _, failed := ask(3, agent.ID+":5", "c.1"); failed == nil || failed["code"] != appwire.CodeNotFound {
		t.Fatalf("a task with no program answered %v", failed)
	}
	if _, failed := ask(4, agent.ID+":4", ""); failed == nil || failed["code"] != appwire.CodeBadParams {
		t.Fatalf("no cursor answered %v", failed)
	}
}
