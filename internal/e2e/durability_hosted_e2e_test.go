//go:build e2e

package e2e

// NOTHING COMPLETED IS EVER LOST, AGAINST THE HOSTED RELAY.
//
// The real binary is the holding machine (home A, in a real terminal) and runs
// a scripted model, so every tool call is the same call every time and the
// moment it finished is known to the millisecond. The relay is the hosted
// staging Worker. A machine that dies mid-work is simulated by what really
// kills one: SIGKILL, a dead network, a frozen process. Then the other machine
// (home B, in this process, over the same real take path the chat uses) takes
// the chat over, and the run counts: calls completed on A against calls present
// on B. Every test ends in one DURABILITY line that says PASS or FAIL and the
// two numbers.
//
// One identity per run, made fresh and shared between the homes, so a run never
// meets another run's chats on the relay. The relay is CODEAF_HOSTED_URL, and
// staging when unset.
//
//	go test -tags e2e -count=1 -run TestDurability -v -timeout 40m ./internal/e2e/

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const stagingRelay = "https://caf-relay-staging.instrument-santosh.workers.dev"

// durable is one run's world: two homes of one identity, the scripted model,
// the proxy that can cut the holder's network, and the chat on A.
type durable struct {
	t     *testing.T
	relay string
	homeA string
	homeB string
	ws    string
	brain *brain
	proxy *cutProxy
	a     *rig
	// launcher is words that wrap the chat's launch, or none.
	launcher []string
	roots    string // where B keeps the chats it takes
}

func newDurable(t *testing.T, script ...brainStep) *durable {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}
	relay := os.Getenv("CODEAF_HOSTED_URL")
	if relay == "" {
		relay = stagingRelay
	}
	overrides := map[string]any{
		"model.talk": durabilityModel, "model.work": durabilityModel, "model.plan": durabilityModel,
		"models.tiers.worker": durabilityModel, "models.tiers.high": durabilityModel,
		"tools.approvalMode": "allow", "daily_budget_usd": 0, "model_pool": "off",
	}
	d := &durable{t: t, relay: relay, roots: t.TempDir(), brain: newBrain(t, script...), proxy: newCutProxy(t)}
	d.homeA, d.homeB = newHome(t, overrides), newHome(t, overrides)
	d.ws = newWorkspace(t, "durable", false)
	shareIdentity(t, d.homeA, d.homeB)
	return d
}

// env is what the holder is started with: the scripted model, the relay, and
// the cuttable proxy as the only road to the relay.
func (d *durable) env() []string {
	return []string{
		"OPENROUTER_API_KEY=stub-key", "CODEAF_BASE_URL=" + d.brain.url(),
		"CODEAF_CELLS=1", "CODEAF_SYNC_URL=" + d.relay,
		"HTTPS_PROXY=" + d.proxy.url(), "https_proxy=" + d.proxy.url(),
		"CODEAF_TASK_BELT=node",
	}
}

// startA opens the chat on A and sends the one message that makes the script run.
func (d *durable) startA(name string, more ...string) {
	d.t.Helper()
	args := append([]string{"chat", "--model", durabilityModel, "--one-model", "--no-host"}, more...)
	d.a = startWithEnv(d.t, append(d.env(), d.launcher...), name, d.homeA, d.ws, 140, 40, args...)
	d.a.skipSetup(d.t)
}

func (d *durable) say(text string) {
	d.a.lit(text)
	d.a.keys("Enter")
}

// pidA is the chat process, verified to be the binary under test before anyone
// signals it: the pane's own pid, or the one process under it that runs the
// binary when the chat was started under [freezable]. Never a pattern match.
func (d *durable) pidA() int {
	d.t.Helper()
	raw, err := exec.Command("tmux", "display-message", "-p", "-t", d.a.name, "#{pane_pid}").Output()
	pane, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	candidates := []int{pane}
	kids, _ := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pane, pane))
	for _, k := range strings.Fields(string(kids)) {
		n, _ := strconv.Atoi(k)
		candidates = append(candidates, n)
	}
	for _, pid := range candidates {
		if exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); pid > 0 && exe == binary(d.t) {
			return pid
		}
	}
	d.t.Fatalf("no process under pane %d runs the binary under test (%v)", pane, err)
	return 0
}

// freezable is the words that run the chat as a child of a launcher that stays
// in the pane, in the pane's own group. tmux resumes any pane process that stops
// (it sends the process SIGCONT), which would undo a SIGSTOP and make a lid that
// never closes; it is told only of its own child, so a stopped grandchild stays
// stopped. The chat keeps the terminal on its input. The words go last in the
// environment list, so the launcher runs them and passes the rest of its own
// command line to them.
var freezable = []string{"bash", "-c", `env "$@" <&0 & wait $!`, "_"}

func (d *durable) signalA(sig syscall.Signal) {
	d.t.Helper()
	if err := syscall.Kill(d.pidA(), sig); err != nil {
		d.t.Fatalf("signal %v: %v", sig, err)
	}
}

// at sleeps until t0+after, and answers the moment it woke.
func at(t0 time.Time, after time.Duration) {
	time.Sleep(time.Until(t0.Add(after)))
}

// chatRoot is A's folder of the chat: the one that holds a transcript.
func (d *durable) chatRoot() string {
	d.t.Helper()
	for path := range sessionTranscripts(d.t, d.homeA) {
		return filepath.Dir(filepath.Dir(path)) // the transcript lives in the chat's .cell folder
	}
	d.t.Fatalf("no chat folder under %s", d.homeA)
	return ""
}

func (d *durable) chatID() string { return filepath.Base(d.chatRoot()) }

// chatIDSoon is chatID for a chat that may not have its folder yet.
func (d *durable) chatIDSoon() string {
	d.t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		for path := range sessionTranscripts(d.t, d.homeA) {
			return filepath.Base(filepath.Dir(filepath.Dir(path)))
		}
	}
	d.t.Fatalf("the chat never got a folder under %s", d.homeA)
	return ""
}

// headWatch reads the relay's head for the chat from the other machine's seat,
// as often as it can, and keeps when each new head was first seen. It is how a
// run knows how long a call took to become durable, with no help from A.
type headWatch struct {
	mu   sync.Mutex
	seen []headSeen
	stop chan struct{}
}

type headSeen struct {
	at   time.Time
	head string
}

func (b *machineB) watchHead(id string) *headWatch {
	w := &headWatch{stop: make(chan struct{})}
	b.d.t.Cleanup(w.Stop)
	go func() {
		for {
			select {
			case <-w.stop:
				return
			case <-time.After(100 * time.Millisecond):
			}
			if v, err := b.sync.Dir.Cell(context.Background(), id); err == nil {
				w.note(v.Cell.Head)
			}
		}
	}()
	return w
}

func (w *headWatch) note(head string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if n := len(w.seen); head != "" && (n == 0 || w.seen[n-1].head != head) {
		w.seen = append(w.seen, headSeen{time.Now(), head})
	}
}

func (w *headWatch) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
}

// durableAfter is how long after t0 the relay's head first moved, or 0 when it had not.
func (w *headWatch) durableAfter(t0 time.Time) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, h := range w.seen {
		if h.at.After(t0) {
			return h.at.Sub(t0).Round(10 * time.Millisecond)
		}
	}
	return 0
}

// ── B: the other machine ─────────────────────────────────────────────────────

// machineB is home B's sync and the engine it takes through, in this process.
type machineB struct {
	sync *syncsetup.Sync
	eng  cellstore.Engine
	d    *durable
}

func (d *durable) machineB() *machineB {
	d.t.Helper()
	d.t.Setenv("CODEAF_HOME", d.homeB)
	d.t.Setenv("CODEAF_CELLS", "1")
	d.t.Setenv("CODEAF_SYNC_URL", d.relay)
	d.t.Setenv("HTTPS_PROXY", "")
	d.t.Setenv("https_proxy", "")
	s, ok, err := syncsetup.Open(d.homeB)
	if err != nil || !ok {
		d.t.Fatalf("home B cannot open the relay: ok=%v err=%v", ok, err)
	}
	bin, err := furrow.ResolveOwned()
	if err != nil {
		d.t.Fatal(err)
	}
	eng := cellstore.EngineFor("")
	eng.DataRoot, eng.Binary, eng.Transport = d.t.TempDir(), bin, cellstore.Spawn{Binary: bin}
	return &machineB{sync: s, eng: eng, d: d}
}

// take continues chat id on B, in a folder of B's own.
func (b *machineB) take(id string) (cell.Cell, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := b.sync.Continuer(b.eng, syncsetup.TakeOptions{DeviceName: "machine-b",
		RootFor: func(id string) string { return filepath.Join(b.d.roots, id) },
		Notify:  func(s string) { b.d.t.Logf("B: %s", s) }})
	got, err := c.Take(ctx, id)
	return got.Taken.Cell, err
}

// ── counting ─────────────────────────────────────────────────────────────────

var markWord = regexp.MustCompile(`MARK-(\d+)`)

// callsIn is the calls a chat folder holds as COMPLETED: the marks the
// transcript carries in a tool result, and the marks the tree carries as files.
// A call counts as present only when both agree, so a result without its file
// (or a file without its result) is reported as the difference it is.
func callsIn(root string) (results, files map[int]bool) {
	results, files = map[int]bool{}, map[int]bool{}
	raw, _ := os.ReadFile(filepath.Join(root, ".cell", "transcript.jsonl"))
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, `"role":"tool"`) && !strings.Contains(line, `"role": "tool"`) {
			continue
		}
		for _, m := range markWord.FindAllStringSubmatch(line, -1) {
			n, _ := strconv.Atoi(m[1])
			results[n] = true
		}
	}
	tree := workspaceFolder(root)
	found, _ := filepath.Glob(filepath.Join(tree, "m*.txt"))
	for _, f := range found {
		if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "m"), ".txt")); err == nil {
			files[n] = true
		}
	}
	return results, files
}

// workspaceFolder is the tree a taken chat landed in.
func workspaceFolder(root string) string {
	if w := filepath.Join(root, "work"); dirExists(w) {
		return w
	}
	return root
}

func dirExists(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

func sorted(m map[int]bool) string {
	var ns []int
	for n := range m {
		ns = append(ns, n)
	}
	for i := range ns {
		for j := i + 1; j < len(ns); j++ {
			if ns[j] < ns[i] {
				ns[i], ns[j] = ns[j], ns[i]
			}
		}
	}
	return fmt.Sprint(ns)
}

// verdict states the run's one line, and fails the test when a call the holder
// completed is missing on B. A call is present when BOTH its effect (the file it
// wrote, in the tree) and its record (the tool result, in the transcript) are
// there; the two are counted apart because they are sealed apart.
func (d *durable) verdict(mode string, completed int, got cell.Cell, note string) {
	d.t.Helper()
	results, files := callsIn(got.Root)
	var lostEffects, lostRecords []int
	for i := 1; i <= completed; i++ {
		if !files[i] {
			lostEffects = append(lostEffects, i)
		}
		if !results[i] {
			lostRecords = append(lostRecords, i)
		}
	}
	// FAIL means a completed call's WORK is missing on B. FAIL-RECORD means every
	// call's work arrived and only the tool-result lines of the transcript did
	// not: the transcript is sealed one call behind the work (see BENCH-MOVE).
	status := "PASS"
	switch {
	case len(lostEffects) > 0:
		status = "FAIL"
	case len(lostRecords) > 0:
		status = "FAIL-RECORD"
	}
	if status != "PASS" {
		d.t.Fail()
	}
	d.t.Logf("DURABILITY %s mode=%s completedOnA=%d effectsOnB=%s recordsOnB=%s lostEffects=%v lostRecords=%v %s",
		status, mode, completed, sorted(files), sorted(results), lostEffects, lostRecords, note)
}

// waitDirectory waits until the relay holds a head for the chat, and answers the cell.
func (d *durable) waitDirectory(b *machineB, id string, within time.Duration) (directory.Cell, bool) {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		v, err := b.sync.Dir.Cell(context.Background(), id)
		if err == nil && v.Cell.Head != "" {
			return v.Cell, true
		}
	}
	return directory.Cell{}, false
}

// ── the moments a run kills at ───────────────────────────────────────────────

// anchor is a clock of the script: when call n finished, or when call n+1 began.
type anchor func(b *brain, within time.Duration) (time.Time, bool)

func finished(n int) anchor {
	return func(b *brain, w time.Duration) (time.Time, bool) { return b.finishedAt(n, w) }
}
func began(n int) anchor {
	return func(b *brain, w time.Duration) (time.Time, bool) { return b.startedAt(n, w) }
}

// holdRun is one holder run up to the moment of a failure: the chat is open, the
// message is sent, the head is being watched from B, and the anchor has come.
type holdRun struct {
	d        *durable
	b        *machineB
	id       string
	watch    *headWatch
	t0       time.Time
	killedAt time.Time // when the ending was applied
}

func (d *durable) begin(name string, from anchor) *holdRun {
	d.t.Helper()
	b := d.machineB()
	d.startA(name)
	d.say(probeWords)
	id := d.chatIDSoon()
	s := &holdRun{d: d, b: b, id: id, watch: b.watchHead(id)}
	t0, ok := from(d.brain, 120*time.Second)
	if !ok {
		d.t.Fatalf("the script never reached its anchor:\n%s", d.a.capture())
	}
	s.t0 = t0
	return s
}

// ending is what a run does to the holder at t0+after, before B takes over.
type ending func(s *holdRun)

func sigkill(s *holdRun) { s.d.signalA(syscall.SIGKILL) }

// strike waits until after the anchor and applies the ending.
func (s *holdRun) strike(after time.Duration, end ending) {
	at(s.t0, after)
	s.killedAt = time.Now()
	end(s)
}

// finish takes the chat on B from A's dead machine and states the verdict. The
// project folder is moved out of B's sight first, as it is when B is another
// computer, so the take must bring the files from the relay.
func (s *holdRun) finish(mode, note string) {
	s.d.t.Helper()
	completed := s.d.brain.completed()
	s.d.awayFromA()
	got, err := s.b.take(s.id)
	if err != nil {
		s.d.t.Fatalf("DURABILITY FAIL mode=%s completedOnA=%d: B could not take the chat: %v", mode, completed, err)
	}
	s.d.logSyncStats()
	s.d.verdict(mode, completed, got, fmt.Sprintf("%s durableAfterAnchor=%v timeline=%s", note, s.watch.durableAfter(s.t0), s.timeline()))
}

// timeline is every call's finish and every head the relay showed, in seconds
// from the moment the first call finished, so a lost call can be read against
// the upload window it fell in.
func (s *holdRun) timeline() string {
	finishes := s.d.brain.finishTimes()
	zero := finishes[1]
	rel := func(t time.Time) string { return fmt.Sprintf("%.1f", t.Sub(zero).Seconds()) }
	var out []string
	for n := 1; n <= s.d.brain.completed(); n++ {
		out = append(out, fmt.Sprintf("call%d@%s", n, rel(finishes[n])))
	}
	s.watch.mu.Lock()
	defer s.watch.mu.Unlock()
	for _, h := range s.watch.seen {
		out = append(out, "head@"+rel(h.at))
	}
	out = append(out, "killed@"+rel(s.killedAt))
	return strings.Join(out, ",")
}

// logSyncStats prints what A's own flush log says it sent, which is the holder's
// side of the story the relay's head only tells from outside.
func (d *durable) logSyncStats() {
	found, _ := filepath.Glob(filepath.Join(d.homeA, "v3", "sync", "stats", "*.jsonl"))
	for _, f := range found {
		raw, _ := os.ReadFile(f)
		d.t.Logf("A's flush log %s:\n%s", filepath.Base(f), raw)
	}
}

// awayFromA moves A's project folder aside until the test ends.
func (d *durable) awayFromA() {
	d.t.Helper()
	away := d.ws + ".away"
	if err := os.Rename(d.ws, away); err != nil {
		d.t.Fatal(err)
	}
	d.t.Cleanup(func() { _ = os.Rename(away, d.ws) })
}

// ── failure mode 1: SIGKILL a beat after a call completes ───────────────────

var killDelays = []time.Duration{500 * time.Millisecond, 2 * time.Second, 5 * time.Second}

// burstDelays add a delay past the whole window (five seconds) and the upload that ends it.
var burstDelays = append(append([]time.Duration{}, killDelays...), 9*time.Second)

// A lone call: nothing else was sealed in the window, so it goes up at once.
func TestDurabilityKillAfterLoneCall(t *testing.T) {
	for _, after := range killDelays {
		t.Run(after.String(), func(t *testing.T) {
			d := newDurable(t, marks(1, 0)...)
			s := d.begin("lone", finished(1))
			s.strike(after, sigkill)
			s.finish("kill-"+after.String()+"-after-a-lone-call", "")
		})
	}
}

// A burst: three calls 200 ms apart, so the second and third sit in the window
// that the first one's upload opened. Three is the most a chat runs before it
// hands a long turn to a task (a task's calls seal in the task's own copy, which
// is a different seam), so a longer burst would stop measuring the chat.
func TestDurabilityKillAfterBurst(t *testing.T) {
	for _, after := range burstDelays {
		t.Run(after.String(), func(t *testing.T) {
			d := newDurable(t, marks(3, 200*time.Millisecond)...)
			s := d.begin("burst", finished(3))
			s.strike(after, sigkill)
			s.finish("kill-"+after.String()+"-after-the-third-of-a-burst", "")
		})
	}
}

// The turn's own end: the last call's result and the model's closing words are
// written after the last seal, so they are the first thing a kill takes.
func TestDurabilityKillAfterTurnEnds(t *testing.T) {
	for _, after := range killDelays {
		t.Run(after.String(), func(t *testing.T) {
			d := newDurable(t, brainStep{cmd: mark(1)}, brainStep{})
			s := d.begin("turnend", began(1))
			s.strike(after, sigkill)
			s.finish("kill-"+after.String()+"-after-the-turn-ended", "")
		})
	}
}

// A lone call in a chat the relay already knows: the first call has long been
// durable and its window closed, so this is the incremental case, the one the
// "durable in a fraction of a second" claim is about. The first call of a chat
// is heavier (it creates the chat's record and sends the whole tree), so it is
// timed apart, in TestDurabilityKillAfterLoneCall.
func TestDurabilityKillAfterLaterLoneCall(t *testing.T) {
	for _, after := range killDelays {
		t.Run(after.String(), func(t *testing.T) {
			script := []brainStep{{cmd: mark(1)}, {wait: 9 * time.Second, cmd: mark(2)}, {wait: forever}}
			d := newDurable(t, script...)
			s := d.begin("later", finished(2))
			s.strike(after, sigkill)
			s.finish("kill-"+after.String()+"-after-a-later-lone-call", "")
		})
	}
}

// ── failure mode 2: SIGKILL during a tool call ──────────────────────────────

// The third call is a long one and is killed three seconds into it. It is
// in flight, so it may be lost; the two completed before it may not be. The
// tight run starts it a moment after the second finished; the settled run
// waits for the upload window to have closed first.
func TestDurabilityKillDuringCall(t *testing.T) {
	slow := "sleep 30; echo MARK-3 > m3.txt && echo MARK-3"
	for name, gap := range map[string]time.Duration{"tight": 200 * time.Millisecond, "settled": 9 * time.Second} {
		t.Run(name, func(t *testing.T) {
			script := []brainStep{{cmd: mark(1)}, {wait: time.Second, cmd: mark(2)}, {wait: gap, cmd: slow}, {wait: forever}}
			d := newDurable(t, script...)
			s := d.begin("during", began(2))
			s.strike(3*time.Second, sigkill)
			s.finish("kill-during-the-third-call-"+name, "inFlight=3")
		})
	}
}

// ── failure mode 3: the network is cut right after a call, then the holder dies ─

// localHead is the head A has sealed on its own disk, read from the chat folder.
func (d *durable) localHead() string {
	d.t.Helper()
	root := d.chatRoot()
	c, err := cell.OpenAt(root, filepath.Base(root))
	if err != nil {
		d.t.Fatal(err)
	}
	h, err := cellstore.Head(c)
	if err != nil || h == nil {
		d.t.Fatalf("no sealed head at %s: %v", root, err)
	}
	return h.Turn.ID
}

// restartA opens the same chat again on A's own home and folder, with a model
// that has nothing to say, and the network as it is now.
func (d *durable) restartA(name string) {
	d.t.Helper()
	d.brain = newBrain(d.t)
	var transcript string
	for path := range sessionTranscripts(d.t, d.homeA) {
		transcript = path
	}
	d.startA(name, "--session", transcript)
}

// waitHead waits until the relay's head for the chat is head, and says how long it took.
func (d *durable) waitHead(b *machineB, id, head string, within time.Duration) (time.Duration, bool) {
	begun := time.Now()
	for time.Since(begun) < within {
		if v, err := b.sync.Dir.Cell(context.Background(), id); err == nil && v.Cell.Head == head {
			return time.Since(begun).Round(100 * time.Millisecond), true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0, false
}

// The network is cut the moment the third call finishes, the holder is killed a
// second later with its sealed work still on its own disk, and the network comes
// back. The same chat is opened again on the same machine, and what was pending
// must reach the relay without anyone asking; only then does B take over.
func TestDurabilityNetworkCutThenKill(t *testing.T) {
	d := newDurable(t, marks(3, 200*time.Millisecond)...)
	s := d.begin("cut", finished(3))
	completed := d.brain.completed()
	d.proxy.Cut()
	s.strike(time.Second, sigkill)
	sealed := d.localHead()
	if v, err := s.b.sync.Dir.Cell(context.Background(), s.id); err == nil && v.Cell.Head == sealed {
		t.Fatalf("the premise failed: the relay already held A's newest head before the kill")
	}
	d.proxy.Restore()
	first := d.brain
	d.restartA("cut-restart")
	took, ok := d.waitHead(s.b, s.id, sealed, 90*time.Second)
	if !ok {
		t.Fatalf("DURABILITY FAIL mode=network-cut-then-kill completedOnA=%d: the restarted chat never uploaded what was pending:\n%s", completed, d.a.capture())
	}
	d.signalA(syscall.SIGKILL)
	d.brain = first // the restarted model saw no calls: the count and the clocks are the first run's
	s.finish("network-cut-then-kill-then-restart", fmt.Sprintf("uploadedAfterRestart=%v", took))
}

// ── failure mode 4: the lid closes, B takes over, the lid opens ─────────────

// work is one call of B's own on a chat B has taken: it is sealed and published
// through the same drive side the chat uses, so it moves the relay's head.
func (b *machineB) work(c cell.Cell, name string) (head string) {
	b.d.t.Helper()
	tree := workspaceFolder(c.Root)
	eng := b.eng
	eng.Workspace = tree
	title := func() string { m, _ := session.LoadMeta(c.Root); return m.Title }
	drive, err := b.sync.Drive(context.Background(), eng, c, syncsetup.DriveOptions{DeviceName: "machine-b", Title: title})
	if err != nil {
		b.d.t.Fatal(err)
	}
	b.d.t.Cleanup(func() { _ = drive.Close(context.Background()) })
	seat, err := cellstore.SeatOver(executor.HostBound, c, tree, nil, nil, func(cellstore.Engine) cellstore.Store { return drive.Store(eng) })
	if err != nil {
		b.d.t.Fatal(err)
	}
	seat = executor.Gated(seat, drive.Gate)
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"echo"}`)}
	err = seat.Around(context.Background(), call, func() ([]byte, bool) {
		return nil, os.WriteFile(filepath.Join(tree, name), []byte("work on B\n"), 0o600) != nil
	})
	if err != nil {
		b.d.t.Fatal(err)
	}
	drive.Idle()
	h, err := cellstore.Head(c)
	if err != nil || h == nil {
		b.d.t.Fatalf("B sealed no head: %v", err)
	}
	return h.Turn.ID
}

// branchesOf lists the cells the relay holds as branches of chat id.
func (b *machineB) branchesOf(id string) []string {
	l, err := b.sync.Dir.List(context.Background())
	if err != nil {
		return nil
	}
	var out []string
	for cid, c := range l.Cells {
		if c.ParentCell == id {
			out = append(out, cid)
		}
	}
	return out
}

// leaseHeld reads whether the relay still shows A's lease as held, the way the
// chat list does: stored expiry later than the relay's own clock.
func (b *machineB) leaseHeld(id string) (held bool, left time.Duration, ok bool) {
	v, err := b.sync.Dir.Cell(context.Background(), id)
	if err != nil {
		return false, 0, false
	}
	left = time.Duration(v.Cell.Lease.Expires-v.Now) * time.Millisecond
	return left > 0, left, true
}

// frozenFor is how long the lid stays shut. The lease lapses on its own within
// about ninety seconds; three minutes is well past it.
const frozenFor = 3 * time.Minute

// A is frozen (SIGSTOP: no thread runs, every socket goes silent, as with a
// closed lid) the instant a second call completed on it: the call sits in the
// window the first one's upload opened, so it cannot have been uploaded. B takes the chat over three minutes later and does work of its
// own. Then A wakes (SIGCONT). A must not overwrite B, and the call A completed
// and never uploaded must come out as a kept branch, not vanish.
func TestDurabilityLidClose(t *testing.T) {
	d := newDurable(t, brainStep{cmd: mark(1)}, brainStep{wait: 3 * time.Second, cmd: mark(2)}, brainStep{wait: forever})
	d.launcher = freezable
	s := d.begin("lid", finished(2))
	pid := d.pidA()
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGCONT) }) // a stopped process cannot be reaped
	s.strike(0, func(s *holdRun) { s.d.signalA(syscall.SIGSTOP) })
	frozen := time.Now()
	for i := 0; i < 3; i++ {
		time.Sleep(300 * time.Millisecond)
		t.Logf("right after SIGSTOP +%dms: %s", (i+1)*300, d.holderState())
	}
	sealed := d.localHead()
	if v, err := s.b.sync.Dir.Cell(context.Background(), s.id); err != nil || v.Cell.Head == sealed {
		t.Fatalf("the premise failed: the relay already held A's newest head (%v)", err)
	}
	lapsedAfter := d.watchLeaseLapse(s, frozen)
	time.Sleep(time.Until(frozen.Add(frozenFor)))

	d.awayFromA()
	got, err := s.b.take(s.id)
	if err != nil {
		t.Fatalf("DURABILITY FAIL mode=lid-close: B could not take the chat: %v", err)
	}
	bHead := s.b.work(got, "b1.txt")
	if _, ok := d.waitHead(s.b, s.id, bHead, 30*time.Second); !ok {
		t.Fatalf("B's own work never became the relay's head")
	}

	_ = syscall.Kill(pid, syscall.SIGCONT)
	woke := time.Now()
	kept := d.waitBranch(s.b, s.id, 120*time.Second)
	v, _ := s.b.sync.Dir.Cell(context.Background(), s.id)
	overwritten := v.Cell.Head != bHead
	t.Logf("A after waking:\n%s", d.a.capture())

	branchCalls := "none"
	if kept != "" {
		branch, err := s.b.take(kept)
		if err != nil {
			t.Fatalf("B could not read the kept branch %s: %v", kept, err)
		}
		results, files := callsIn(branch.Root)
		branchCalls = fmt.Sprintf("effects=%s records=%s", sorted(files), sorted(results))
		if !files[2] {
			t.Errorf("the kept branch lacks call 2's work")
		}
	}
	status := "PASS"
	if overwritten || kept == "" {
		status = "FAIL"
		t.Fail()
	}
	results, files := callsIn(got.Root)
	t.Logf("DURABILITY %s mode=lid-close completedOnA=2 effectsOnB=%s recordsOnB=%s (B took the chat; call 2 was never uploaded) "+
		"leaseLapsedAfter=%v frozen=%v relayHeadOverwrittenByA=%v keptBranch=%v branchHolds{%s} supersededAfterWake=%v",
		status, sorted(files), sorted(results), lapsedAfter, frozenFor, overwritten, kept != "", branchCalls, time.Since(woke).Round(time.Second))
}

// watchLeaseLapse polls the relay while A is frozen and answers how long after
// the freeze the lease first read as not held, or 0 when it never did.
func (d *durable) watchLeaseLapse(s *holdRun, frozen time.Time) time.Duration {
	var lapsed time.Duration
	for time.Since(frozen) < frozenFor-5*time.Second {
		held, left, ok := s.b.leaseHeld(s.id)
		d.t.Logf("lease at +%v: held=%v expiresIn=%v read=%v; holder process: %s", time.Since(frozen).Round(time.Second), held, left.Round(time.Second), ok, d.holderState())
		if ok && !held && lapsed == 0 {
			lapsed = time.Since(frozen).Round(time.Second)
		}
		touchAlive(d.t)
		time.Sleep(5 * time.Second)
	}
	return lapsed
}

// holderState is the state letter of A's process and the processes under it,
// read from the kernel: T is stopped, S is asleep, R is running.
func (d *durable) holderState() string {
	pid := d.pidA()
	out := []string{fmt.Sprintf("%d:%s", pid, procState(pid))}
	kids, _ := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
	for _, k := range strings.Fields(string(kids)) {
		n, _ := strconv.Atoi(k)
		out = append(out, fmt.Sprintf("%d:%s", n, procState(n)))
	}
	return strings.Join(out, " ")
}

func procState(pid int) string {
	raw, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if end := strings.LastIndexByte(string(raw), ')'); end >= 0 && len(raw) > end+2 {
		return string(raw[end+2])
	}
	return "?"
}

// waitBranch waits until the relay lists a branch of chat id, and answers its id.
func (d *durable) waitBranch(b *machineB, id string, within time.Duration) string {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(time.Second) {
		if found := b.branchesOf(id); len(found) > 0 {
			return found[0]
		}
	}
	return ""
}

// touchAlive tells the orchestrator watching the worktree that a long wait is a wait.
func touchAlive(t *testing.T) {
	root := repoRoot(t)
	_ = os.MkdirAll(filepath.Join(root, ".lane"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".lane", "alive"), []byte(time.Now().Format(time.RFC3339)), 0o644)
}
