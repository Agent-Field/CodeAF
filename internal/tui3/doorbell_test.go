package tui3

import (
	"bufio"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE DOORBELL'S LAWS (doorbell.go) ───────────────────────────────────────
//
// They are counts and outcomes, never stopwatches (PERF.md: gate on work, never
// on time). The defect they guard was binary — the answer came back, or the wire
// gave up on it ten seconds later — so the laws are binary too, and the one
// figure worth knowing is logged rather than gated on.

// answerWakesTheTurn is an engine on a real pipe that answers every call at
// once, except that its answer to a question is preceded by a "phase" frame —
// the turn that answer woke, saying it is connecting. That is the real engine's
// order: the turn's goroutine gets its news out before the dispatch goroutine
// gets its result out, and it is the order that deadlocked the window.
func answerWakesTheTurn(t *testing.T) *remote.Agent {
	t.Helper()
	surface, engine := net.Pipe()
	go func() {
		lines := bufio.NewScanner(engine)
		lines.Buffer(make([]byte, 0, 1<<20), 1<<24)
		send := func(f remote.Frame) {
			line, _ := json.Marshal(f)
			_, _ = engine.Write(append(line, '\n'))
		}
		for lines.Scan() {
			var frame remote.Frame
			if json.Unmarshal(lines.Bytes(), &frame) != nil {
				return
			}
			switch frame.Kind {
			case "hello":
				welcome, _ := json.Marshal(remote.Welcome{Version: remote.Version, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"})
				send(remote.Frame{Kind: "welcome", Payload: welcome})
			case "call":
				if frame.Method == remote.MethodQuestionResolve {
					phase, _ := json.Marshal(remote.PhaseWire{Phase: "connecting", Model: "a/b", Role: "talk"})
					send(remote.Frame{Kind: "phase", Payload: phase})
				}
				send(remote.Frame{Kind: "result", ID: frame.ID})
			}
		}
	}()
	client, err := remote.Dial(surface, "", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("dial the engine: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = engine.Close() })
	return client.Agent()
}

// busyLoop is a program whose Update does the worst thing an update loop can do
// on the answer road — asks the engine's door itself and waits for it — and
// reports how that went. If the news the answer causes could hold the loop, this
// is the loop it would hold.
type busyLoop struct {
	agent  *remote.Agent
	answer chan error
	took   chan time.Duration
}

type pressEnterMsg struct{}

func (m busyLoop) Init() tea.Cmd { return nil }

func (m busyLoop) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(pressEnterMsg); ok {
		began := time.Now()
		err := m.agent.ResolveQuestion(session.Answer{Kind: session.QuestionAsk, ID: 1, Key: "1", Picked: []string{"1"}})
		m.took <- time.Since(began)
		m.answer <- err
	}
	return m, nil
}

func (m busyLoop) View() tea.View { return tea.NewView("") }

// AN ANSWER IS ANSWERED WHILE THE LOOP THAT SENT IT IS STILL WAITING FOR IT.
//
// This is the ten-second freeze, reproduced on the real pieces: the real wire
// client, a real Bubble Tea program, and the news readers exactly as [Run]
// registers them. Before the doorbell, the reader handed the phase frame to
// Program.Send, the loop was inside Update waiting for this very call, and the
// call came back as "did not answer in time" after exactly ten seconds
// (measured on the parent commit: 10.000362601s). Now the reader rings and
// returns, the result frame behind it is read, and the call is answered.
func TestAnAnswerIsAnsweredWhileTheLoopThatSentItIsBusy(t *testing.T) {
	agent := answerWakesTheTurn(t)
	loop := busyLoop{agent: agent, answer: make(chan error, 1), took: make(chan time.Duration, 1)}
	program := tea.NewProgram(loop, tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	door := newDoorbell(newsMsg{})
	defer door.close()
	defer listenForNews(door)()
	go func() { _, _ = program.Run() }()
	defer program.Kill()

	// The one Program.Send in this package's tests: it is the keystroke, sent
	// from the test's own goroutine, which no busy loop can deadlock with.
	program.Send(pressEnterMsg{})
	took := <-loop.took
	if err := <-loop.answer; err != nil {
		t.Fatalf("the answer was not answered while the loop was busy — the news it caused held the wire's reader: %v", err)
	}
	t.Logf("the answer round trip took %v with the loop busy (it was 10s)", took)
}

// A NEWS READER NEVER WAITS FOR THE LOOP, however much news there is and however
// long the loop has been away. A thousand phases and a thousand sightings are
// posted with nothing draining the door — the loop is busy — and every post
// returns. Reaching the assertions is the law: a reader that waited would still
// be waiting.
func TestTheNewsReadersNeverWaitForTheLoop(t *testing.T) {
	door := newDoorbell(newsMsg{})
	defer door.close()
	defer listenForNews(door)()
	for i := range 1000 {
		session.TellPhase(session.PhaseNews{Phase: "writing", Model: "a/b", Role: "talk", Detail: string(rune('a' + i%26))})
		session.TellLane(session.LaneNews{Model: "a/b", Lane: "friendli", Role: "talk"})
	}
	// AND TWO THOUSAND POSTS OWE THE LOOP EXACTLY ONE FRAME. The desk already
	// holds every one of them; a frame draws the desk, so one frame is all of it.
	if owed := len(door.rung); owed != 1 {
		t.Fatalf("two thousand posts left %d frames owed, want exactly one", owed)
	}
}

// A RING IS NEVER LOST. The loop takes the token, and news that arrives while
// the loop is folding that message in rings a door with an empty slot — so the
// command the loop parks again in the same Update finds it, and the frame after
// the news is drawn.
func TestARingWhileTheLoopIsFoldingTheLastOneIsNotLost(t *testing.T) {
	door := newDoorbell(newsMsg{})
	defer door.close()
	door.ring()
	if _, ok := door.waitRing()().(newsMsg); !ok {
		t.Fatal("a rung door did not deliver its message")
	}
	// Between the command returning and the Update that parks it again.
	door.ring()
	if _, ok := door.waitRing()().(newsMsg); !ok {
		t.Fatal("a ring made while the loop was folding the previous one was lost")
	}
	// AND A CLOSED DOOR ENDS ITS COMMAND, so a program that has stopped leaves
	// nothing parked on a slot nobody will ring.
	door.close()
	if msg := door.waitRing()(); msg != nil {
		t.Fatalf("a closed door delivered %T", msg)
	}
}

// NOTHING IN THE SURFACE CALLS Program.Send. Every caller of it is a goroutine
// that is not the loop, and every such goroutine can be made to wait on a busy
// loop — which is a deadlock the moment the loop is waiting on that goroutine,
// as the wire's reader was. The doorbell is the door (doorbell.go). The check is
// structural: any `.Send` selected off a value this package took from
// tea.NewProgram or declared as a *tea.Program.
func TestNothingInTheSurfaceCallsProgramSend(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading the surface's source: %v", err)
	}
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			programs := programNames(file)
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Send" {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && programs[id.Name] {
					t.Errorf("%s:%d calls %s.Send — a goroutine that is not the loop waits on it whenever the loop is busy; ring a doorbell instead (doorbell.go)",
						filepath.Base(path), fset.Position(call.Pos()).Line, id.Name)
				}
				return true
			})
		}
	}
}

// programNames is every name in one file bound to a Bubble Tea program: a
// variable assigned from tea.NewProgram, and a parameter or field declared as
// *tea.Program.
func programNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	isProgram := func(expr ast.Expr) bool {
		star, ok := expr.(*ast.StarExpr)
		if !ok {
			return false
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "Program"
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Field:
			if isProgram(node.Type) {
				for _, name := range node.Names {
					names[name.Name] = true
				}
			}
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok || i >= len(node.Lhs) {
					continue
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewProgram" {
					if id, ok := node.Lhs[i].(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
		}
		return true
	})
	return names
}

// AND OVER A CONNECTION, UPDATE ASKS THE FAR MACHINE NOTHING.
//
// This is the law offloop.go names, and it is the runtime half of
// [TestNoEngineDoorIsAskedFromTheUpdateLoop]: the structural one reads the
// source, this one watches the wire. The figure it gates on is the client's own
// count of calls made ([remote.Client.CallsMade]) — a count, never a stopwatch
// (PERF.md) — measured across a real Update that answers a real question on a
// real socket. The answer still arrives: it goes out on the door line's
// goroutine, and nothing on the loop waited for it.
func TestAnAnswerOverAConnectionAsksTheFarMachineNothingFromUpdate(t *testing.T) {
	surface, engine := net.Pipe()
	answered := make(chan struct{}, 4)
	go func() {
		lines := bufio.NewScanner(engine)
		lines.Buffer(make([]byte, 0, 1<<20), 1<<24)
		send := func(f remote.Frame) {
			line, _ := json.Marshal(f)
			_, _ = engine.Write(append(line, '\n'))
		}
		for lines.Scan() {
			var frame remote.Frame
			if json.Unmarshal(lines.Bytes(), &frame) != nil {
				return
			}
			switch frame.Kind {
			case "hello":
				welcome, _ := json.Marshal(remote.Welcome{Version: remote.Version, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"})
				send(remote.Frame{Kind: "welcome", Payload: welcome})
			case "call":
				if frame.Method == remote.MethodQuestionResolve {
					answered <- struct{}{}
				}
				send(remote.Frame{Kind: "result", ID: frame.ID})
			}
		}
	}()
	client, err := remote.Dial(surface, "", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("dial the engine: %v", err)
	}
	defer func() { _ = client.Close(); _ = engine.Close() }()

	a := newTestApp(client.Agent())
	defer a.doorLine.close()
	q := session.Question{
		ID: 4242, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Head: "which store?", Asker: session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{{Key: "1", Label: "sqlite"}, {Key: "2", Label: "postgres"}},
		Asked:   a.now(),
	}
	a.raiseQuestion(questionShown{question: q})
	a.questionRows(a.width)
	head, ok := a.questionHead()
	if !ok {
		t.Fatal("the question is not on the block")
	}
	before := client.CallsMade()
	cmd := a.answerQuestion(head, session.Answer{Key: "1", Picked: []string{"1"}})
	// THE ASSERTION IS HERE, BETWEEN UPDATE RETURNING AND THE COMMAND RUNNING.
	// Update has done everything it is going to do about this answer.
	if now := client.CallsMade(); now != before {
		t.Fatalf("Update made %d call(s) to the far machine — the answer road is back on the loop", now-before)
	}
	if a.questioning() {
		t.Fatal("the question did not settle on the keystroke")
	}
	// AND THE ANSWER STILL GOES. It travels on the command, through the door
	// line, and the far machine is asked exactly once.
	spend(t, a, cmd)
	select {
	case <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("the far machine was never asked, so the answer reached nobody")
	}
	if now := client.CallsMade(); now != before+1 {
		t.Fatalf("the far machine was asked %d times for one answer", now-before)
	}
}
