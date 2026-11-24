package session

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// drawingDoor keeps tasks, files and (when kept) canvases.
type drawingDoor struct {
	workingDoor
	kept  bool
	drawn []CanvasCall
}

func (d *drawingDoor) CanvasesKept() bool { return d.kept }

func (d *drawingDoor) Canvas(_ context.Context, call CanvasCall) (string, error) {
	d.drawn = append(d.drawn, call)
	return `{"id":"cv_aaaaaaaaaaaa"}`, nil
}

func canvasBeltNames(a *Agent) []string {
	a.tools = a.belt()
	names := []string{}
	for _, tool := range a.offeredTools() {
		names = append(names, tool.Name)
	}
	return names
}

// THE SERVER'S AFI CARRIES ws_canvas ONLY WHERE THE SERVER KEEPS CANVASES:
// a door that keeps them puts it on the belt beside the workstream tools,
// one that does not (no `canvas` program) leaves it absent. The manual names it.
func TestTheCanvasRidesOnlyADoorThatKeepsCanvases(t *testing.T) {
	kept := &Agent{config: Config{Workstream: &drawingDoor{kept: true}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	if names := canvasBeltNames(kept); !slices.Contains(names, WSCanvasTool) {
		t.Fatalf("a door that keeps canvases offered %v", names)
	}
	none := &Agent{config: Config{Workstream: &drawingDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	if names := canvasBeltNames(none); slices.Contains(names, WSCanvasTool) {
		t.Fatalf("a server without canvases offered %v", names)
	}
	if !manual.Chat().Mentions(WSCanvasTool) {
		t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", WSCanvasTool)
	}
}

// THE GUIDE COMES BEFORE THE FIRST CHANGE, AND IT IS THE PERSONAL ONE: a
// first change answers the guide and runs nothing; a read runs without it;
// once read, changes run, with the command split as a shell would.
func TestTheCanvasGuideComesBeforeTheFirstChange(t *testing.T) {
	door := &drawingDoor{kept: true}
	a := &Agent{config: Config{Workstream: door, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	tool := a.wsCanvasTool(door)
	call := func(args string) string {
		t.Helper()
		out, _, err := tool.Execute(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	call(`{"command":"list"}`)
	if len(door.drawn) != 1 {
		t.Fatalf("a read waited for the guide: %v", door.drawn)
	}
	first := call(`{"command":"canvas frame add cv_aaaaaaaaaaaa --kind slide --name \"The problem\""}`)
	if len(door.drawn) != 1 || !strings.Contains(first, "Read this guide first") {
		t.Fatalf("the first change ran before the guide: %v %.80q", door.drawn, first)
	}
	for _, want := range []string{"Making it look good", "Use only facts you were given", "You have no shell here", "Never reply with a workaf:// link"} {
		if !strings.Contains(first, want) {
			t.Errorf("the guide does not say %q", want)
		}
	}
	call(`{"command":"frame add cv_aaaaaaaaaaaa --kind slide --name \"The problem\" --json '{\"part\":\"Launch deck\"}'","input":"x"}`)
	got := door.drawn[len(door.drawn)-1]
	want := []string{"frame", "add", "cv_aaaaaaaaaaaa", "--kind", "slide", "--name", "The problem", "--json", `{"part":"Launch deck"}`}
	if !slices.Equal(got.Args, want) || got.Input != "x" {
		t.Fatalf("ran %q %q, want %q", got.Args, got.Input, want)
	}
	if out := call(`{"command":"put cv_aaaaaaaaaaaa --html ../x.html","files":{"../x.html":"<p>"}}`); !strings.Contains(out, "plain") {
		t.Fatalf("a file with a folder in its name was taken: %q", out)
	}
	if out := call(`{"command":"title cv_x --title \"open"}`); !strings.Contains(out, "not closed") {
		t.Fatalf("an open quote ran: %q", out)
	}
}

func TestSplitCanvasCommand(t *testing.T) {
	for line, want := range map[string][]string{
		`new --title "Launch cards"`:          {"new", "--title", "Launch cards"},
		`put CV --json '{"text":"It\'s"}'`:    nil,
		`put CV --json '{"text":"Hi there"}'`: {"put", "CV", "--json", `{"text":"Hi there"}`},
		`title CV --title "say \"hi\""`:       {"title", "CV", "--title", `say "hi"`},
		`title CV --title ""`:                 {"title", "CV", "--title", ""},
		"show  CV\n":                          {"show", "CV"},
	} {
		got, err := splitCanvasCommand(line)
		if want == nil {
			if err == nil {
				t.Errorf("%s split as %q", line, got)
			}
			continue
		}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s = %q %v, want %q", line, got, err, want)
		}
	}
}
