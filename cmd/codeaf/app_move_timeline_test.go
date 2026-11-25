package main

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// THE MOVE TIMELINE RIG. Two real `codeaf app` processes on one machine, two
// homes, a loopback relay and a scripted model, driven over the app's own wire
// exactly as the field measurement was (agents.take, then chats.list until the
// row says here). The sync cadence is the shipped default, never the 50 ms the
// other take tests use, because the overheads this rig exists to catch are the
// ones that cadence and the product's own waits add.
//
// CODEAF_MOVE_RIG_MB sizes the folder the chat works in (the field case was
// 600: thirty 10 MB files and a git repository). Unset, it skips; PERF.md's
// "Moving a chat" has the command. CODEAF_MOVE_RIG_OFFLINE=0 stops after the
// take-back. Each take prints one MOVE line: the wire's answer, the moment
// chats.list says here, the bytes the relay served and the continuer's own
// phase split. docs/design/app-v3/MOVE-TIMING.md reads them.
//
// THE CEILINGS ARE PERF.md's "Moving a chat" table, and changing one changes
// that table in the same commit. A take-back of a small change is real work of
// well under a second on loopback; before the fixes it measured 9.8 s at 600 MB,
// almost all of it a secret scan of unchanged files and engine processes
// re-verifying the store. A take from a computer that has gone away pays one
// copy-folder restore on top. A cold take is bounded by the bytes it moves.
const (
	moveTakeBackCeiling = 3 * time.Second
	moveOfflineCeiling  = 6 * time.Second
	// moveColdFixed and moveColdPerMB make the cold ceiling: a constant plus
	// the bytes at 100 MB/s, a floor any loopback or LAN box clears.
	moveColdFixed = 4 * time.Second
	moveColdPerMB = 10 * time.Millisecond
)

// moveRigFolder fills dir with mb megabytes of incompressible files in 10 MB
// pieces and makes it a git repository holding only its README, the shape of
// the field case (601 MB: the files and a small repository, not a second
// copy of every file in .git/objects).
func moveRigFolder(t *testing.T, dir string, mb int) {
	t.Helper()
	rng := rand.NewChaCha8([32]byte{7})
	buf := make([]byte, 10<<20)
	for i := 0; mb > 0; i++ {
		n := min(mb, 10) << 20
		_, _ = rng.Read(buf[:n])
		if err := os.WriteFile(filepath.Join(dir, "blob-"+strconv.Itoa(i)+".bin"), buf[:n], 0o600); err != nil {
			t.Fatal(err)
		}
		mb -= 10
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("move rig\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "README.md"}, {"-c", "user.email=rig@example.com", "-c", "user.name=rig", "commit", "-qm", "rig"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

var movePhasesLine = regexp.MustCompile(`continue (\S+) phases (.*)`)

// moveRigPhases is the continuer's own phase line for cell from an app's log,
// the newest one, so a step's split is the take it just made.
func moveRigPhases(t *testing.T, app *e2App, cell string) string {
	raw, _ := os.ReadFile(app.log)
	last := ""
	for _, m := range movePhasesLine.FindAllStringSubmatch(string(raw), -1) {
		if m[1] == cell {
			last = m[2]
		}
	}
	return last
}

// moveRigBytes counts the bytes the relay answered with, so a step's network
// share can be told from its local work.
var moveRigBytes atomic.Int64

type countingWriter struct{ http.ResponseWriter }

func (w countingWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	moveRigBytes.Add(int64(n))
	return n, err
}

func (w countingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func moveRigCounting(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(countingWriter{w}, r)
	})
}

type moveRigStep struct {
	name          string
	reply, landed time.Duration
	phases        string
	bytes         int64
}

// moveRigTake times one take the way the field did: from the request until the
// wire answers, and until chats.list says the chat is here.
func moveRigTake(t *testing.T, name string, app *e2App, row appwire.ChatRow) (appwire.Agent, moveRigStep) {
	t.Helper()
	var (
		got    appwire.Agent
		bytes  int64
		began  time.Time
		reply  time.Duration
		passes int
	)
	// A holder can still add a save after the row looked settled (a title, the
	// last usage line), and then the take is refused as stale, which is the
	// product doing its job. The rig reads the row again and times the take
	// that goes through.
	for {
		bytes, began = moveRigBytes.Load(), time.Now()
		raw, err := app.w.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen})
		reply = time.Since(began)
		if err == nil {
			var res appwire.TakeResult
			if jerr := json.Unmarshal(raw, &res); jerr != nil {
				t.Fatal(jerr)
			}
			got = res.Agent
			break
		}
		if passes++; err.Code != appwire.CodeConflict || passes > 3 {
			t.Fatalf("%s take: %s: %s", name, err.Code, err.Message)
		}
		t.Logf("%s take refused as stale (%s); reading the row again", name, err.Message)
		stale := row.Seen
		row = outageChatRow(t, app, row.Cell, func(r appwire.ChatRow) bool { return r.CanTake && r.Seen != stale })
	}
	outageChatRow(t, app, row.Cell, func(r appwire.ChatRow) bool { return r.Here })
	step := moveRigStep{name: name, reply: reply, landed: time.Since(began), phases: moveRigPhases(t, app, row.Cell), bytes: moveRigBytes.Load() - bytes}
	t.Logf("MOVE %-10s reply=%-8s here=%-8s relay-bytes=%d %s", name, step.reply.Round(time.Millisecond), step.landed.Round(time.Millisecond), step.bytes, step.phases)
	return got, step
}

func TestMoveTimelineRig(t *testing.T) {
	mb, _ := strconv.Atoi(os.Getenv("CODEAF_MOVE_RIG_MB"))
	if mb <= 0 {
		t.Skip("CODEAF_MOVE_RIG_MB is unset: the move timeline rig runs on request")
	}
	steps := moveRigJourney(t, mb, os.Getenv("CODEAF_MOVE_RIG_OFFLINE") != "0")
	ceiling := map[string]time.Duration{
		"cold":      moveColdFixed + time.Duration(mb)*moveColdPerMB,
		"take-back": moveTakeBackCeiling,
		"offline":   moveOfflineCeiling,
	}
	for _, s := range steps {
		t.Logf("SUMMARY %-10s reply=%s here=%s ceiling=%s", s.name, s.reply.Round(time.Millisecond), s.landed.Round(time.Millisecond), ceiling[s.name])
		if s.landed > ceiling[s.name] {
			t.Errorf("%s take of a %d MB chat took %s, over its %s ceiling (PERF.md, Moving a chat): %s", s.name, mb, s.landed.Round(time.Millisecond), ceiling[s.name], s.phases)
		}
	}
}

// moveRigJourney is the field journey: turns on A, a cold take to B, a turn on
// B, a take back to A with A's app and its old session still running, then B
// takes it again once A has quit. Both apps run the engine daemon, as a shipped
// `codeaf app` does. It answers each take's timing.
func moveRigJourney(t *testing.T, mb int, offline bool) []moveRigStep {
	celltest.RequireEngine(t)
	root := e2TempDir(t)
	wakeEnvironment(t, root)
	model := appTwoMessageSource(t, "date +%s%N >> turns.txt")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	relay := e2WireRelay(t, moveRigCounting)
	homeA, homeB := outageHomes(t, relay.URL)
	// The shipped cadence, not the test one outageHomes sets.
	t.Setenv(syncsetup.IntervalVar, "")
	folderA, folderB := filepath.Join(root, "person-a"), filepath.Join(root, "person-b")
	for _, dir := range []string{folderA, folderB} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	work := filepath.Join(folderA, "project")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	moveRigFolder(t, work, mb)
	t.Setenv("HOME", folderA)
	a := e2StartApp(t, homeA, true)
	t.Setenv("HOME", folderB)
	b := e2StartApp(t, homeB, true)

	created := machineCall[struct{ Agent appwire.Agent }](t, a.w, "agents.create", map[string]any{"workspace": work}).Agent
	for i := range 3 {
		e2Turn(t, a, created.ID, "turn "+strconv.Itoa(i)+" on A")
	}
	row := c1bSettledRow(t, "B sees A's last save", b, a, "", func(r appwire.ChatRow) bool { return r.Title != "" && r.Seen.Head != "" })
	var steps []moveRigStep
	there, step := moveRigTake(t, "cold", b, row)
	steps = append(steps, step)
	e2Turn(t, b, there.ID, "turn on B")

	back := c1bSettledRow(t, "A sees B's last save", a, b, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head != row.Seen.Head })
	taken, step := moveRigTake(t, "take-back", a, back)
	steps = append(steps, step)
	if !offline {
		return steps
	}
	// No turn on A first: whether a turn right after a take-back publishes
	// promptly is Drive.firstPublishOwed's business (#2654), and this step
	// measures only the take from a computer that has gone away.
	_ = taken
	outageChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Fence != back.Seen.Fence })
	e2Quit(t, a)
	again := outageChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return r.CanTake && r.Seen.Fence != back.Seen.Fence })
	_, step = moveRigTake(t, "offline", b, again)
	steps = append(steps, step)
	return steps
}
