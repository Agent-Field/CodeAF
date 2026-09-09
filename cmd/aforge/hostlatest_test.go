package main

// THE HOST NEVER COLLIDES WITH ITSELF, tested from both ends of the one
// sentence that used to be the collision.
//
// A plain launch names no conversation. What that MEANS — this workspace's
// latest — was answered in two places that could disagree: the engine host's
// key, which said "nothing", and the boot, which read the disk. While the
// host's nothing-slot happened to hold the workspace's latest the two agreed;
// the moment that conversation ended under a host still holding another, the
// key said nothing, the boot resolved the journal this very process holds the
// flock on, and the host refused its own conversation with "this conversation
// is open in another window".

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// The key a plain hello resolves to is the journal the boot would open. One
// reading, asked twice.
func TestAPlainHelloKeysToTheConversationTheBootWouldOpen(t *testing.T) {
	t.Setenv("AFORGE_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	writeV3Session(t, bucket, "00000000000000b1", "last week", time.Now().Add(-7*24*time.Hour))
	newest := writeV3Session(t, bucket, "00000000000000b2", "this morning", time.Now().Add(-time.Hour))

	key := engineHelloKey(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, "")
	if key == "" {
		t.Fatal("a plain hello still keys to nothing, which is what the host filed a conversation under")
	}
	if want := filepath.Join(newest, "transcript.jsonl"); key != want {
		t.Fatalf("a plain hello keyed to %s, want the workspace's latest (%s)", key, want)
	}

	// And the boot, asked the same question with the disk in the same state,
	// opens exactly that journal.
	found, err := v3ResolveSession("", workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if found.Transcript != key {
		t.Fatalf("the boot opens %s while a hello keys to %s — the two readings have drifted", found.Transcript, key)
	}
}

// A workspace with no conversation in it keys to nothing, and that is honest:
// there is nothing to join, so the hello boots. The conversation that boot
// opens is what the NEXT plain hello keys to.
func TestAWorkspaceWithNoConversationKeysToNothingAndThenToWhatItOpened(t *testing.T) {
	t.Setenv("AFORGE_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()

	if key := engineHelloKey(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, ""); key != "" {
		t.Fatalf("an empty workspace keyed to %s", key)
	}
	found, err := v3ResolveSession("", workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	// The agent takes the journal's flock as it opens, which is what puts the
	// file on disk; the boot has only named the folder so far.
	if err := os.WriteFile(found.Transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Nobody has spoken in it — this is the empty folder a launch reuses — and
	// the key must still find it, or a second plain launch would open a second
	// agent onto the journal the first one holds.
	key := engineHelloKey(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, "")
	if key != found.Transcript {
		t.Fatalf("a plain hello keyed to %q after the boot opened %s", key, found.Transcript)
	}
}

// A hello that NAMED a conversation is spelled the way the boot spells it, so
// two surfaces naming one file two ways are asking for one conversation.
func TestANamedHelloKeysToTheSpellingTheBootUses(t *testing.T) {
	t.Setenv("AFORGE_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	named := filepath.Join(workspace, "somewhere", "transcript.jsonl")

	key := engineHelloKey(remote.Hello{Version: remote.Version, Workspace: workspace, Session: named}, workspace, "")
	if key != named {
		t.Fatalf("a named hello keyed to %s, want %s", key, named)
	}
	// The host's --session flag is the answer for a hello that named nothing.
	flagged := engineHelloKey(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, named)
	if flagged != named {
		t.Fatalf("the host's own --session keyed to %s, want %s", flagged, named)
	}
}

// ── a refusal keeps the engine road ─────────────────────────────────────────

// The refusal as it looks AFTER A TRIP OVER THE SOCKET: internal/remote carries
// a boot failure as `engine: ` plus the text, so the only thing the dialling
// side has to read is the sentence.
func TestAHeldRefusalIsRecognisedAfterATripOverTheSocket(t *testing.T) {
	over := errors.New("engine: " + sessionHeldElsewhereSentence("/home/somebody/api"))
	if !hostHeldRefusal(over) {
		t.Fatalf("the refusal this road exists to answer was not recognised: %v", over)
	}
	for _, other := range []error{
		nil,
		errors.New("engine: the workspace opened no conversation"),
		errors.New("dial unix /nowhere: connect: no such file or directory"),
	} {
		if hostHeldRefusal(other) {
			t.Fatalf("%v was read as a held journal", other)
		}
	}
}

// AND THE LAUNCH STAYS ON THE ENGINE ROAD. It asks again for a conversation of
// its own and carries the transcript it could not have, so the surface lands on
// home with that row armed — where one enter MOVES it, because a window on this
// road has an engine to ask (tui3.Options.EngineAnswers).
func TestAHeldRefusalAsksTheEngineAgainRatherThanFallingInProcess(t *testing.T) {
	t.Setenv("AFORGE_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	held := writeV3Session(t, bucket, "00000000000000c1", "this morning", time.Now().Add(-time.Hour))
	refusal := errors.New("engine: " + sessionHeldElsewhereSentence(workspace))

	takeOver, again := localAskAgainAfterRefusal(localLaunch{workspace: workspace}, refusal)
	if !again {
		t.Fatal("a held journal ended the engine road instead of opening a conversation beside it")
	}
	if want := filepath.Join(held, "transcript.jsonl"); takeOver != want {
		t.Fatalf("the surface would point at %q, want the held conversation (%s)", takeOver, want)
	}

	// A conversation named on the command line is the one that was refused, and
	// it is spelled the way the surface's rows spell it.
	named := filepath.Join(workspace, "elsewhere", "transcript.jsonl")
	takeOver, again = localAskAgainAfterRefusal(localLaunch{workspace: workspace, session: named}, refusal)
	if !again || takeOver != named {
		t.Fatalf("a named conversation gave (%q, %v)", takeOver, again)
	}

	// Everything else keeps the road it had. A launch the engine answered, a
	// launch it refused for another reason, and --once, which has no screen to
	// land an offer on and says the sentence instead.
	for _, kept := range []struct {
		why    string
		launch localLaunch
		err    error
	}{
		{"an engine that answered", localLaunch{workspace: workspace}, nil},
		{"an unreachable engine", localLaunch{workspace: workspace}, errors.New("dial unix: no such file or directory")},
		{"a headless run", localLaunch{workspace: workspace, once: "say hello"}, refusal},
	} {
		if _, again := localAskAgainAfterRefusal(kept.launch, kept.err); again {
			t.Fatalf("%s asked the engine for a second conversation", kept.why)
		}
	}
}

// ── the sentence names a project ────────────────────────────────────────────

// THE HELD SENTENCE NAMES A DIRECTORY A HOST IS KEYED BY, and never a session's
// own work folder.
//
// [sessionHeldElsewhereSentence] spells `aforge engine --stop --workspace X`.
// For an OWNED conversation session.Config.Workspace is that session's private
// work/ directory under ~/.aforge/v3/projects — a real path, and a command
// pointing at a host that does not exist. So no call may hand this door a
// `.Workspace`; the project is carried separately ([v3Launch.Project]).
func TestNoDoorHandsTheHeldSentenceAWorkspaceField(t *testing.T) {
	set := token.NewFileSet()
	pkgs, err := parser.ParseDir(set, ".", nil, 0)
	if err != nil {
		t.Fatalf("read cmd/aforge: %v", err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fn, ok := call.Fun.(*ast.Ident)
				if !ok || fn.Name != "openV3Agent" || len(call.Args) < 2 {
					return true
				}
				if sel, ok := call.Args[1].(*ast.SelectorExpr); ok && sel.Sel.Name == "Workspace" {
					t.Errorf("%s: openV3Agent is handed a .Workspace, which for an owned conversation is that session's own work directory — hand it the project", set.Position(call.Pos()))
				}
				return true
			})
		}
	}
}
