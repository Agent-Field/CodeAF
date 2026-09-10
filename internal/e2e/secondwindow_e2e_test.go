//go:build e2e

package e2e

// secondwindow_e2e_test.go is #761 ON THE REAL SCREEN: two windows on one
// conversation against one engine host, and what the one that is not typing
// draws where the running work belongs.
//
// THE COLUMN'S OWN LABEL IS THE ASSERTION. `tasks` is drawn only when there is
// a row under it — an empty section would spend a line announcing absence, which
// the emptiness law forbids (internal/tui3's margin.go) — so a roster column
// holding `+ /task` and no label is a column that knows about no work at all.
// That is exactly what #761 photographed while the engine's own checkpoint held
// two nodes running.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// TestSecondWindowTaskRail drives the whole story in one workspace: a task
// started in the first window, a second window opened on the same conversation,
// and the engine host replaced under both of them.
func TestSecondWindowTaskRail(t *testing.T) {
	requireTmuxAndKey(t)

	root := shortRoot(t)
	home := shortHome(t, root)
	ws := workspaceAt(t, filepath.Join(root, "w"), false)

	a := start(t, "afe2e_second_a", home, ws, tuiPlain, 40)
	a.lit("/task solo write the word hello into hello.txt in this repo")
	a.keys("Enter")
	first := waitForRail(t, a, modelPatience)
	t.Logf("window A, with the task it started on its rail:\n%s", first)

	// ── the second window ────────────────────────────────────────────────────
	//
	// It lands in the same conversation through the same host, which is what
	// makes it a second window rather than a second conversation.
	b := start(t, "afe2e_second_b", home, ws, tuiPlain, 40)
	second := waitForRail(t, b, 60*time.Second)
	t.Logf("the second window's roster column:\n%s", second)

	// ── the host replaced under both of them ─────────────────────────────────
	//
	// THIS IS THE DEFECT ITSELF (#761). Every window redials when the host it is
	// attached to goes — a rebuilt binary, a machine waking up — and a redial
	// gets its transcript and its status line back because a turn's events are
	// numbered and replayed. The standing lanes are neither, so before this fix
	// the rail went quiet for the rest of the session while the engine went on
	// running the very tasks the column was for.
	//
	// IT IS THE SECOND WINDOW THAT IS ASKED, because opening the conversation
	// there MOVED it: window A was told so and stepped back to home, which is
	// what #691 promises and what makes B the window a person is sitting in
	// front of when the host under it is replaced.
	//
	// AND THE PROOF IS A TASK STARTED AFTERWARDS, not the rows already drawn. A
	// rail keeps whatever it has been told — nothing wipes it when a link dies —
	// so a column that still shows the first task proves only that the surface
	// has a memory. A task commissioned after the repair is a CALL and not a
	// turn, so its row can only reach the column down the standing lane
	// (internal/remote's tasklane.go): if that lane did not come back with the
	// link, this row never arrives.
	killHost(t, ws)
	startTaskAndSeeItOnTheRail(t, b, "zebra")
}

// startTaskAndSeeItOnTheRail commissions one task by hand and waits for its row.
//
// IT TYPES MORE THAN ONCE ON PURPOSE. The link is being repaired underneath, and
// a call made during the gap is refused with `try that again in a moment`
// (internal/remote's roamingRefusal) — which is the honest answer and not a
// failure, so the test does what a person does and asks again. Three tries is
// measured: the gap is a second or two of backoff and a host spawn, and the
// first ask is the only one that has ever met it.
func startTaskAndSeeItOnTheRail(t *testing.T, r *rig, mark string) {
	t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		r.lit("/task solo " + mark + ": write the word " + mark + " into " + mark + ".txt")
		r.keys("Enter")
		deadline := time.Now().Add(40 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(railColumn(r.capture()), mark) {
				t.Logf("the task started after the host was replaced reached the roster column:\n%s",
					railColumn(r.capture()))
				return
			}
			time.Sleep(pollEvery)
		}
	}
	t.Fatalf("a task started after the engine host was replaced never reached this window's roster column — "+
		"the standing task lane did not come back with the link (#761). The column:\n%s\nThe screen:\n%s",
		railColumn(r.capture()), r.capture())
}

// waitForRail waits for one window's roster column to hold the tasks section,
// and fails in the issue's own words when all it ever holds is the door.
func waitForRail(t *testing.T, r *rig, within time.Duration) string {
	t.Helper()
	label := say(t, "railTasksLabel")
	deadline := time.Now().Add(within)
	for {
		column := railColumn(r.capture())
		if strings.Contains(column, label) {
			return column
		}
		if time.Now().After(deadline) {
			t.Fatalf("this window's roster column never drew the conversation's work — it holds %q and nothing else, "+
				"which is an empty roster inviting the same task to be started twice (#761). The column:\n%s\nThe screen:\n%s",
				say(t, "railTaskDoorWord"), column, r.capture())
		}
		time.Sleep(pollEvery)
	}
}

// railColumn is the roster column alone: whatever stands to the right of the
// rail's rule on every line that has one. The transcript is to its left and the
// status line has no rule at all, so this is the column and only the column.
func railColumn(screen string) string {
	var b strings.Builder
	for _, line := range strings.Split(screen, "\n") {
		at := strings.LastIndex(line, "│")
		if at < 0 {
			continue
		}
		b.WriteString(strings.TrimSpace(line[at+len("│"):]))
		b.WriteString("\n")
	}
	return b.String()
}

// killHost ends this workspace's engine host the way a rebuilt binary does: the
// process goes, the windows attached to it redial, and the next dial starts a
// fresh host that resumes the conversation from its checkpoint.
//
// THE PATH IS RESOLVED FIRST. The host is spawned with the workspace as the
// LAUNCH resolved it (cmd/aforge's localLink.dial), and on macOS /tmp is a
// symlink to /private/tmp — so a pattern built from the path this test handed
// out matches nothing at all and the whole scenario passes by never happening.
func killHost(t *testing.T, ws string) {
	t.Helper()
	tried := []string{ws}
	if resolved, err := filepath.EvalSymlinks(ws); err == nil && resolved != ws {
		tried = append(tried, resolved)
	}
	for _, path := range tried {
		pattern := fmt.Sprintf("engine --daemon --workspace %s", path)
		if err := exec.Command("pkill", "-f", pattern).Run(); err == nil {
			t.Logf("the engine host holding %s has been ended; the windows on it now redial", path)
			return
		}
	}
	t.Fatalf("no engine host was holding any of %v to replace — this scenario is about a host being "+
		"replaced under two windows, and there was none", tried)
}

// ── a state root a unix socket fits under ───────────────────────────────────

// shortRoot is a working directory with a SHORT PATH, and it is a fixture
// rather than a taste.
//
// The engine host listens on `<state root>/v3/hosts/<16 hex>/host.sock`, which
// is thirty-five bytes on top of the root, and a unix socket path may weigh a
// hundred and four (internal/enginehost's socketLimit). macOS hands t.TempDir()
// a path under /var/folders long enough to spend the rest, so a rig built the
// ordinary way gets NO HOST AT ALL — it says `engine host: no host answered`,
// opens the conversation in its own process, and the second window opens a
// second conversation beside it. That is a perfectly good rig for every other
// scenario here and it is the one thing this one cannot use.
func shortRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "afe2e-")
	if err != nil {
		t.Fatalf("a short root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// shortHome is [newHome]'s state root under [shortRoot], with the same rows.
func shortHome(t *testing.T, root string) string {
	t.Helper()
	home := filepath.Join(root, "h")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("home: %v", err)
	}
	rows := map[string]any{}
	if userHome, err := os.UserHomeDir(); err == nil {
		if raw, err := os.ReadFile(filepath.Join(userHome, ".aforge", "config.json")); err == nil {
			if err := json.Unmarshal(raw, &rows); err != nil {
				t.Fatalf("the profile config would not parse: %v", err)
			}
		}
	}
	rows["model.talk"] = "deepseek/deepseek-v4-flash"
	rows[config.KeyIcons] = config.IconsPlain
	rows["tools.approvalMode"] = "allow"
	// AND THE ROSTER COLUMN IS PINNED OPEN, because it is the subject of this
	// scenario and it is a PERSON'S standing answer that survives the process
	// (internal/tui3's railAway, config's ui.task_column). The rows above are
	// copied out of whoever's profile is on the machine running this, and on a
	// machine where somebody has put the column away with ctrl+g every
	// assertion here would be about their taste rather than about the surface.
	rows[config.KeyTaskColumn] = true
	raw, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	return home
}
