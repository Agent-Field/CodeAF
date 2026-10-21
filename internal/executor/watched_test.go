package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
)

func TestCommandNamesAreTheFirstWordOfEachSimpleCommand(t *testing.T) {
	cases := map[string][]string{
		"jq . a.json":                        {"jq"},
		"cd sub && make -j4 | tee out; ls":   {"cd", "make", "tee", "ls"},
		"FOO=1 BAR=x node build.js":          {"node"},
		`echo "a && b" ; 'python3' -c 'x;y'`: {"echo", "python3"},
		"(cd x; go test ./...)\nrg foo":      {"cd", "go", "rg"},
		"":                                   nil,
	}
	for line, want := range cases {
		if got := commandNames(line); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", line, got, want)
		}
	}
}

type sightings struct{ names []string }

func (s *sightings) Observe(req ExecRequest, _ ExecResult) { s.names = append(s.names, req.Argv[0]) }

type plainSeat struct{ Stance }

func shellCall(command string) Call {
	args, _ := json.Marshal(map[string]string{"command": command})
	return Call{Tool: "bash", Args: args}
}

func TestAShellCallTellsTheObserverTheToolsItRan(t *testing.T) {
	seen := &sightings{}
	seat := Watching(plainSeat{}, seen)
	if err := seat.Around(context.Background(), shellCall("jq . x | sort"), func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
	if want := []string{"jq", "sort"}; !reflect.DeepEqual(seen.names, want) {
		t.Fatalf("observed %v, want %v", seen.names, want)
	}
	if err := ForSetup(seat).Around(context.Background(), shellCall("apt-get x"), func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
	if got := seen.names[len(seen.names)-1]; got != "apt-get" {
		t.Fatalf("the setup form of the seat is not watched: %v", seen.names)
	}
}

func TestOnlyShellCallsAreReadForTools(t *testing.T) {
	seen := &sightings{}
	seat := Watching(plainSeat{}, seen)
	call := shellCall("jq .")
	call.Tool = "read"
	_ = seat.Around(context.Background(), call, func() ([]byte, bool) { return nil, false })
	if len(seen.names) != 0 {
		t.Fatalf("a %s call was read for tools: %v", call.Tool, seen.names)
	}
}

type refusingSeat struct{ Stance }

func (refusingSeat) Around(context.Context, Call, func() ([]byte, bool)) error {
	return errors.New("not run")
}

func TestARefusedCallObservesNothing(t *testing.T) {
	seen := &sightings{}
	if err := Watching(refusingSeat{}, seen).Around(context.Background(), shellCall("jq ."), nil); err == nil {
		t.Fatal("the seat's refusal was swallowed")
	}
	if len(seen.names) != 0 {
		t.Fatalf("observed %v for a call that did not run", seen.names)
	}
}

type whole struct {
	lines [][]string
	exits []int
}

func (w *whole) Observe(req ExecRequest, res ExecResult) {
	w.lines = append(w.lines, req.Argv)
	w.exits = append(w.exits, res.Exit)
}

func TestAShellCallTellsTheObserverEachCommandWithAllItsWordsAndTheCallsOutcome(t *testing.T) {
	seen := &whole{}
	seat := Watching(plainSeat{}, seen)
	run := func(failed bool) func() ([]byte, bool) { return func() ([]byte, bool) { return nil, failed } }
	if err := seat.Around(context.Background(), shellCall("FOO=1 docker compose up -d && 'ls' -la"), run(false)); err != nil {
		t.Fatal(err)
	}
	if err := seat.Around(context.Background(), shellCall("make"), run(true)); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"docker", "compose", "up", "-d"}, {"ls", "-la"}, {"make"}}
	if !reflect.DeepEqual(seen.lines, want) || !reflect.DeepEqual(seen.exits, []int{0, 0, 1}) {
		t.Fatalf("observed %v with exits %v", seen.lines, seen.exits)
	}
}

// A shell tool starts its process through Command, so a server it leaves behind
// reaches the observer only through the groups the call reported starting.
func TestAShellCallTellsTheObserverWhatItLeftRunning(t *testing.T) {
	var got []ExecResult
	seat := Watching(plainSeat{}, observerFunc(func(_ ExecRequest, res ExecResult) { got = append(got, res) }))
	ctx, spawns := WithSpawns(context.Background())
	call := shellCall("sleep 30 &")
	call.Spawns = spawns
	err := seat.Around(ctx, call, func() ([]byte, bool) {
		cmd := exec.Command("sleep", "30")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
		Spawned(ctx, cmd.Process.Pid)
		return nil, false
	})
	if err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if len(last.Services) != 1 || last.Services[0].Name != "sleep" {
		t.Fatalf("the left-running sleep was not observed: %+v", got)
	}
}

type observerFunc func(ExecRequest, ExecResult)

func (f observerFunc) Observe(req ExecRequest, res ExecResult) { f(req, res) }
