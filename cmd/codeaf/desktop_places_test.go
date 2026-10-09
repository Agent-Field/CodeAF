package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE BRIDGE AND THE ENGINE MUST READ ONE PLACE GRAPH. The path is chosen once
// by the bridge, carried in the hello (a session host started long before this
// window never sees a variable set on the child), and honoured only from a
// desktop surface.

func helloOverTheWire(t *testing.T, hello remote.Hello) remote.Hello {
	t.Helper()
	raw, err := json.Marshal(hello)
	if err != nil {
		t.Fatal(err)
	}
	var out remote.Hello
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheDefaultPlacesPathIsOneAbsoluteFileUnderTheStateRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	got, err := desktopPlacesPath("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "desktop", "places.json"); got != want {
		t.Fatalf("default places path %q, want %q", got, want)
	}
	custom, err := desktopPlacesPath("relative/places.json")
	if err != nil || !filepath.IsAbs(custom) {
		t.Fatalf("a custom path must be made absolute: %q %v", custom, err)
	}
}

func TestTheDesktopHelloCarriesThePlaceGraphToTheEngine(t *testing.T) {
	for name, flag := range map[string]string{"default": "", "custom": filepath.Join(t.TempDir(), "mine.json")} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			graph, err := desktopPlacesPath(flag)
			if err != nil {
				t.Fatal(err)
			}
			bridgeDoor, err := session.PlaceGraphDoorFor(graph)
			if err != nil {
				t.Fatal(err)
			}
			hello := helloOverTheWire(t, desktopHello("/srv/app", "", "deepseek/deepseek-v4.1-flash", bridgeDoor.Path))
			opts := engineLaunchOptions(hello, "/srv/app", "")
			if opts.PlaceGraph != bridgeDoor.Path {
				t.Fatalf("the engine was handed %q, the bridge opened %q", opts.PlaceGraph, bridgeDoor.Path)
			}
			engineDoor, err := session.PlaceGraphDoorFor(opts.PlaceGraph)
			if err != nil {
				t.Fatal(err)
			}
			if engineDoor.Path != bridgeDoor.Path || engineDoor.ChoicesPath != bridgeDoor.ChoicesPath {
				t.Fatalf("two files: engine %+v bridge %+v", engineDoor, bridgeDoor)
			}
			// The welcome echoes the shape; a shape asking for another graph is
			// another conversation's shape.
			if !hello.Launch.Same(desktopHello("/srv/app", "", "", bridgeDoor.Path).Launch) {
				t.Fatal("the same graph compared different")
			}
			if hello.Launch.Same(desktopHello("/srv/app", "", "", "/elsewhere/places.json").Launch) {
				t.Fatal("two graphs compared the same")
			}
		})
	}
}

func TestOnlyADesktopSurfaceGetsAPlaceGraph(t *testing.T) {
	shape := &remote.LaunchShape{OneModel: true, Interactive: true, PlaceGraph: "/home/you/.codeaf/desktop/places.json"}
	for _, surface := range []string{"", "terminal", "tui", "remote"} {
		opts := engineLaunchOptions(helloOverTheWire(t, remote.Hello{Surface: surface, Launch: shape}), "/srv/app", "")
		if opts.PlaceGraph != "" {
			t.Fatalf("surface %q was handed a place graph", surface)
		}
	}
	if opts := engineLaunchOptions(remote.Hello{Surface: "desktop"}, "/srv/app", ""); opts.PlaceGraph != "" {
		t.Fatal("a desktop hello with no shape got a place graph")
	}
	if words := launchShapeWords(shape); words == "" || words == launchShapeWords(&remote.LaunchShape{OneModel: true, Interactive: true}) {
		t.Fatalf("a terminal told it joined a desktop conversation must hear about its places: %q", words)
	}
}

func TestALaunchWithAPlaceGraphBuildsTheSessionDoorAndATerminalLaunchDoesNot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)

	terminal, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Config.PlaceGraph != nil {
		t.Fatalf("a terminal launch reads places: %+v", terminal.Config.PlaceGraph)
	}
	graph := filepath.Join(t.TempDir(), "places.json")
	desktop, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir(), OneModel: true, DesktopRoles: true, Interactive: true, PlaceGraph: graph})
	if err != nil {
		t.Fatal(err)
	}
	door := desktop.Config.PlaceGraph
	if door == nil || door.Path != graph || door.ChoicesPath != filepath.Join(filepath.Dir(graph), session.PlaceChoicesFile) || len(door.Sources.Deny) == 0 {
		t.Fatalf("door: %+v", door)
	}
	if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir(), PlaceGraph: "relative.json"}); err == nil {
		t.Fatal("a relative place graph was accepted")
	}
}
