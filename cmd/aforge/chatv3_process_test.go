package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// THE PROCESS RESOURCES, THE SEAM, AND THE CLOSE (chatv3_process.go).
//
// One process now opens more than one conversation, and these are the four
// facts that makes true: the stores are built once and shared, a conversation's
// approval seams are bound to ITS agent, a conversation that fails half-open
// leaves no lock behind, and everything opened is closed however the surface
// returns.

// v3TestProcess is a process over a temp home and a temp profile, closed when
// the test ends. Nothing here touches the machine it runs on.
func v3TestProcess(t *testing.T) *v3Process {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AFORGE_HOME", t.TempDir())
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatalf("the process every v3 door builds once did not open: %v", err)
	}
	t.Cleanup(proc.closeAll)
	return proc
}

// The stores are the process's, and every launch carries the SAME ones. Two
// *store.Store values on one SQLite file would be two connection pools with no
// in-process lock between them, and the only thing arbitrating their writes
// would be the ten-second busy wait (internal/store's writelock.go).
func TestEveryLaunchCarriesTheProcessesOwnStores(t *testing.T) {
	proc := v3TestProcess(t)

	first, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("the first launch did not open: %v", err)
	}
	second, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("the second launch did not open: %v", err)
	}

	if first.Harnesses != proc.Harnesses || second.Harnesses != proc.Harnesses {
		t.Fatal("a launch opened its own harness registry — /harness and the offer card can now name different harnesses")
	}
	if first.Models != proc.Models || second.Models != proc.Models {
		t.Fatal("a launch warmed its own model catalog")
	}
	if first.Config.Memory != proc.Memory || second.Config.Memory != proc.Memory {
		t.Fatal("a launch opened its own handle on the chat database")
	}
	if first.Config.Connect != proc.Conns || second.Config.Connect != proc.Conns {
		t.Fatal("a launch built its own accounts manager — a token one refreshed would be invisible to the other")
	}
	if first.Config.ArtifactsIndex != proc.Artifacts || first.Config.ArtifactsIndex == "" {
		t.Fatalf("the deliverables index is %q, want the process's %q",
			first.Config.ArtifactsIndex, proc.Artifacts)
	}
	if first.Settings.ProfileDir != proc.ProfileDir {
		t.Fatal("a launch resolved its own profile")
	}
}

// recordingGate is the receiving end of a banked rule: it is what the running
// conversation's gate is, from the seam's point of view (chatv3_approval.go's
// [v3Gate]).
type recordingGate struct {
	pushed []*approval.Policy
}

func (r *recordingGate) SetApprovalPolicy(policy *approval.Policy) {
	r.pushed = append(r.pushed, policy)
}

// gateOnly is a conversation as [v3Seam.bundle] uses one: the surface's agent,
// which the bundle only ever stores, and the gate door, which is the one method
// it calls. The embedded interface is nil on purpose — a bundle that reached for
// anything else would panic here rather than pass quietly.
type gateOnly struct {
	tui3.Agent
	recordingGate
}

// THE BUG THIS LANE FIXES, at the door's end.
//
// The three approval seams used to be minted once, at boot, around the boot
// agent, and the surface went on holding them through every /new and every
// resume — so an "always" answered in a later conversation was written to the
// profile correctly and then pushed into a session that had already been closed.
// Minted with the agent, they cannot reach anything else.
func TestABankedRuleReachesTheAgentItWasMintedWith(t *testing.T) {
	proc := v3TestProcess(t)
	workspace := t.TempDir()
	seam := &v3Seam{proc: proc, boot: &v3Launch{Workspace: workspace, Bucket: t.TempDir()}}

	closed, front := &gateOnly{}, &gateOnly{}
	cfg := session.Config{Workspace: workspace, SessionFile: filepath.Join(workspace, "transcript.jsonl")}
	if _, err := seam.bundle(closed, seam.boot, cfg, false, ""); err != nil {
		t.Fatalf("the first conversation did not bundle: %v", err)
	}
	second, err := seam.bundle(front, seam.boot, cfg, false, "")
	if err != nil {
		t.Fatalf("the second conversation did not bundle: %v", err)
	}

	if err := second.SaveApproval("read"); err != nil {
		t.Fatalf("the always was not saved: %v", err)
	}
	if len(closed.pushed) != 0 {
		t.Fatal("the rebuilt gate went into the conversation that was closed — the bug this lane fixes")
	}
	if len(front.pushed) != 1 || front.pushed[0] == nil {
		t.Fatalf("the conversation in front was handed %d gates", len(front.pushed))
	}
	// And the rule that reached it is the one that was answered: the rebuild
	// reads the rows as they now stand, through the same policy the launch built.
	if got := front.pushed[0].Check("read", nil); got.Action != approval.ActionAllow {
		t.Fatalf("the gate handed to the running conversation still asks about read: %s", got)
	}

	// The other two seams of the trio are bound the same way.
	if err := second.SaveBashApproval("git status"); err != nil {
		t.Fatalf("the command was not saved: %v", err)
	}
	if err := second.ApplyApprovals(); err != nil {
		t.Fatalf("the panel's seam failed: %v", err)
	}
	if len(closed.pushed) != 0 {
		t.Fatalf("the closed conversation was written to %d times", len(closed.pushed))
	}
	if len(front.pushed) != 3 {
		t.Fatalf("the running conversation was handed %d gates, want three", len(front.pushed))
	}
}

// A conversation that fails after its agent exists leaves NO LOCK BEHIND. The
// agent takes the session file's flock and starts a presence heartbeat as it
// opens; a step after that returning an error without closing it would leave a
// transcript every window on the machine reads as held by another window until
// this process exits.
func TestAConversationThatFailsHalfOpenClosesItsAgent(t *testing.T) {
	proc := v3TestProcess(t)
	workspace := t.TempDir()
	// The workspace this conversation WORKS in has a settings file that will not
	// parse, which is read after the agent is open (chatv3_process.go's bundle).
	broken := t.TempDir()
	path := config.ProjectConfigPath(broken)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"history.enabled": `), 0o600); err != nil {
		t.Fatal(err)
	}

	bucket := t.TempDir()
	place, err := v3MintSession(bucket, workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	seam := &v3Seam{proc: proc, boot: &v3Launch{Workspace: workspace, Bucket: bucket}}
	cfg := session.Config{
		Workspace: broken, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	}

	if _, err := seam.open(seam.boot, cfg, false); err == nil {
		t.Fatal("a conversation whose project file will not parse opened anyway")
	}
	if session.InUse(place.Transcript()) {
		t.Fatal("the partial conversation kept the session file's lock")
	}
}

// closeAll closes every conversation this process opened, and says so twice
// without doing it twice.
func TestCloseAllClosesEveryConversationAndIsIdempotent(t *testing.T) {
	proc := v3TestProcess(t)
	workspace := t.TempDir()

	transcripts := make([]string, 0, 2)
	for range 2 {
		place, err := v3MintSession(t.TempDir(), workspace, workspace, false)
		if err != nil {
			t.Fatal(err)
		}
		agent, _, _, err := openV3Agent(session.Config{
			Workspace: workspace, Model: "test/model", APIKey: "test-key",
			BaseURL: "https://example.invalid/v1",
			Place:   place, SessionFile: place.Transcript(),
		}, workspace, v3OpenSession)
		if err != nil {
			t.Fatalf("a conversation did not open: %v", err)
		}
		proc.track(agent)
		transcripts = append(transcripts, place.Transcript())
	}
	for _, transcript := range transcripts {
		if !session.InUse(transcript) {
			t.Fatalf("%s is not held by the conversation that opened it", transcript)
		}
	}

	proc.closeAll()
	proc.closeAll()

	for _, transcript := range transcripts {
		if session.InUse(transcript) {
			t.Fatalf("%s is still locked after every conversation was closed", transcript)
		}
	}
}

// LAUNCH DIR KEEPS ITS CONTRACT. It is where the person stood, it is distinct
// from the workspace, and it is left EMPTY in the one case where recording it
// would mark a real project's conversation as litter: a process started under a
// temp directory that opens a conversation somewhere that is not.
func TestATempLaunchDirIsNotStampedOnAConversationInARealProject(t *testing.T) {
	temporary := filepath.Join(os.TempDir(), "af-k2-launch")
	project := t.TempDir()
	if v3TempPath(project) {
		// t.TempDir() is itself under the temp directory on most machines, which
		// is precisely the collision this rule is about — so the project side of
		// the test needs a directory that is not.
		project = filepath.Join(v3HomeForTest(t), "work", "repo")
	}

	if got := v3StampLaunchDir(temporary, project); got != "" {
		t.Fatalf("a conversation in %s was stamped with the temp launch dir %q — the sweep would reap it", project, got)
	}
	// And every other combination records it, because every other combination is
	// either honest bookkeeping or litter the sweep is right about.
	if got := v3StampLaunchDir(temporary, temporary); got != temporary {
		t.Fatalf("a conversation that IS in a temp directory hid its launch dir: %q", got)
	}
	subdir := filepath.Join(project, "cmd")
	if got := v3StampLaunchDir(subdir, project); got != subdir {
		t.Fatalf("the repository subdirectory the person stood in was dropped: %q", got)
	}
	if got := v3StampLaunchDir("", project); got != "" {
		t.Fatalf("an unreadable working directory became %q", got)
	}
}

// v3HomeForTest is a directory that is NOT under the machine's temp directory,
// for the one test that needs the distinction to be real.
func v3HomeForTest(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil || v3TempPath(home) {
		t.Skip("this machine's home directory is itself temporary")
	}
	return home
}

// A workspace a caller names is canonicalised and has to be a directory. The
// refusal is one sentence and names the path the caller gave, because the caller
// is home and home already printed that path on the row.
func TestOpeningAWorkspaceThatIsNotThereIsRefusedInASentence(t *testing.T) {
	// Nothing named is this door's own workspace, and it is not checked.
	if got, err := v3OpenTarget("  "); got != "" || err != nil {
		t.Fatalf("an unnamed workspace resolved to %q (%v)", got, err)
	}

	missing := filepath.Join(t.TempDir(), "gone")
	_, err := v3OpenTarget(missing)
	if err == nil {
		t.Fatal("a workspace that is not there opened")
	}
	if !strings.HasPrefix(err.Error(), v3GoneWord+" · ") || !strings.HasSuffix(err.Error(), missing) {
		t.Fatalf("the refusal reads %q", err.Error())
	}

	// A FILE is not a workspace, and says the same thing rather than failing
	// somewhere deeper with a message about a directory nobody asked to create.
	file := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := v3OpenTarget(file); err == nil {
		t.Fatal("a file opened as a workspace")
	}

	// And a real directory comes back canonical: symlinks resolved, cleaned, so
	// two spellings of one directory are one workspace.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("this machine will not make a symlink: %v", err)
	}
	viaLink, err := v3OpenTarget(link + "/./")
	if err != nil {
		t.Fatalf("a directory reached through a symlink was refused: %v", err)
	}
	direct, err := v3OpenTarget(real)
	if err != nil {
		t.Fatal(err)
	}
	if viaLink != direct {
		t.Fatalf("two spellings of one directory are two workspaces: %q and %q", viaLink, direct)
	}
}
