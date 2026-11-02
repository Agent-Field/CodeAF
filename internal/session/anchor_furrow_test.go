//go:build unix

package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

// installFurrowWatching is a furrow that reports only the folder named by the
// marker file as attached, and refuses every other folder in furrow's own words.
func installFurrowWatching(t *testing.T, watched string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "furrow")
	writeFile(t, script, `#!/bin/sh
if [ "$1" = "--version" ]; then echo "furrow 0.1.0"; exit 0; fi
if [ "$2" = "`+watched+`" ]; then
  echo '{"workspace":"'"$2"'","head":"aaaabbbbcccc0001","watcher_running":true}'
  exit 0
fi
echo "this repository is not watched" >&2
exit 1
`)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(furrow.BinaryEnvVar, script)
	furrow.Forget()
	t.Cleanup(furrow.Forget)
}

// A conversation that starts in its own scratch folder and is anchored to a
// project afterwards gets the project's furrow verbs: the anchor asks furrow
// about the NEW folder from the background and arms the verbs when it answers.
func TestAnchoringAsksFurrowAboutTheNewFolder(t *testing.T) {
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installFurrowWatching(t, project)
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	place := Place{Dir: dir, Owned: true}
	place.Workspace = place.Work()
	if err := os.MkdirAll(place.Work(), 0o700); err != nil {
		t.Fatal(err)
	}
	agent, err := newAgent(Config{
		Workspace: place.Work(), Place: place, SessionFile: place.Transcript(), Model: "test/model", System: "SYSTEM",
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	answered(t, agent)
	if verbs := workspaceVerbs(carried(agent)); len(verbs) != 0 {
		t.Fatalf("the scratch folder carried %v", verbs)
	}
	if _, err := agent.AnchorWorkspace(project); err != nil {
		t.Fatal(err)
	}
	answered(t, agent)
	if got := len(workspaceVerbs(carried(agent))); got != len(furrow.Names()) {
		t.Fatalf("after anchoring to a watched project the belt carries %d workspace verbs; want %d", got, len(furrow.Names()))
	}
}
