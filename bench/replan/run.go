package main

// run.go — the live half: seed, launch, watch the spend, read back, grade.
//
// One invocation is one `codeaf do` subprocess over a fresh copy of the cell's
// fixture, in a throwaway home, with the arm's switch in its environment. The
// driver watches two things while it runs — the plan store, for how many
// workers are going at once, and the home's model-call log, for what has been
// spent — and interrupts ONLY the process it started when a spending cap is
// reached. Nothing here ever signals a process it did not start itself.

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// pollInterval is how often a running invocation's store and call log are
// read. Two seconds resolves a worker's life (tens of seconds at least) and
// costs the machine nothing it would notice.
const pollInterval = 2 * time.Second

// interruptGrace is how long an interrupted door is given to land what it
// reached before the driver stops waiting and kills the one process it started.
const interruptGrace = 90 * time.Second

// profileTiers are the crew rows written into every throwaway profile, so no
// seat the flags do not name can fall to a closed model: the small and reflex
// rows ride the work model, the careful row the plan model. No key is written.
func profileTiers(work, planModel string) map[string]string {
	return map[string]string{
		"models.tiers.reflex":     work,
		"models.tiers.low":        work,
		"models.tiers.worker":     work,
		"models.tiers.high":       planModel,
		"models.tiers.mastermind": planModel,
	}
}

// ledger is the run-wide account every invocation adds to, so the total cap
// holds across invocations that run at once.
type ledger struct {
	mu    sync.Mutex
	spent float64
	live  map[string]float64
}

func (l *ledger) total() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.spent
	for _, v := range l.live {
		t += v
	}
	return t
}

func (l *ledger) setLive(label string, usd float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.live[label] = usd
}

func (l *ledger) settle(label string, usd float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.live, label)
	l.spent += usd
}

// live runs the plan, at most o.parallel invocations at once, in the plan's
// order, and writes the CSV and the summary as rows land.
func live(p plan, o options, out io.Writer) error {
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("the cells grade themselves with the go toolchain, and `go` is not on this machine")
	}
	bin, err := filepath.Abs(o.bin)
	if err != nil {
		return err
	}
	if info, err := os.Stat(bin); err != nil || info.IsDir() {
		return fmt.Errorf("no product binary at %s: build it with make build first", bin)
	}
	if strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) == "" && strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		return errors.New("no provider key in this process's environment: export it from the machine's own secrets before running, the driver never writes one")
	}
	if err := os.MkdirAll(p.Out, 0o700); err != nil {
		return err
	}
	goEnv := goCacheEnv()
	book := &ledger{live: map[string]float64{}}
	csvPath := filepath.Join(p.Out, "replan.csv")
	if err := writeCSVHeader(csvPath); err != nil {
		return err
	}

	var (
		mu    sync.Mutex
		rows  []row
		wg    sync.WaitGroup
		slots = make(chan struct{}, o.parallel)
	)
	for _, iv := range p.Invocations {
		iv.Bin = bin
		if spent := book.total(); spent >= o.totalCap {
			fmt.Fprintf(out, "· %s not started: the run has spent $%.4f of its $%.2f cap\n", iv.label(), spent, o.totalCap)
			mu.Lock()
			rows = append(rows, skippedRow(iv, fmt.Sprintf("not started: total cap reached at $%.4f", spent)))
			mu.Unlock()
			continue
		}
		slots <- struct{}{}
		wg.Add(1)
		go func(iv invocation) {
			defer wg.Done()
			defer func() { <-slots }()
			fmt.Fprintf(out, "· %s starting\n", iv.label())
			r := runOne(iv, o, goEnv, book)
			book.settle(iv.label(), r.USDCalls)
			mu.Lock()
			rows = append(rows, r)
			_ = appendCSV(csvPath, r)
			mu.Unlock()
			fmt.Fprintln(out, r.oneLine())
		}(iv)
	}
	wg.Wait()
	summary := summarize(p, rows)
	fmt.Fprintln(out)
	fmt.Fprint(out, summary)
	fmt.Fprintf(out, "\ntotal spend (call log): $%.4f\n", book.total())
	return os.WriteFile(filepath.Join(p.Out, "summary.md"), []byte(summary), 0o600)
}

// reread reads an earlier run's output root again from its records alone —
// the plan store, the trajectories and the call log each invocation left —
// and prints the table. It calls no model and starts no door: a reader that
// improves is applied to rows already paid for. What only the live run could
// see (the pass, the door's exit and envelope, the width polled while it ran)
// is carried over from each invocation's own row.json.
func reread(root string, cells []cell, arms []arm, out io.Writer) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	p := plan{Cells: cells, Arms: arms, Out: root}
	var rows []row
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		var old row
		raw, err := os.ReadFile(filepath.Join(dir, "row.json"))
		if err != nil || json.Unmarshal(raw, &old) != nil {
			continue
		}
		c, okCell := findCell(cells, old.Cell)
		var a arm
		okArm := false
		for _, candidate := range arms {
			if candidate.Name == old.Arm {
				a, okArm = candidate, true
			}
		}
		if !okCell || !okArm {
			continue
		}
		pristine, err := pristineFixture(c)
		if err != nil {
			return err
		}
		iv := invocation{Arm: a, Cell: c, Replicate: old.Replicate, RunDir: dir}
		w := &watcher{peak: int(old.PeakParallel)}
		r := readBack(iv, pristine, w)
		r.PeakParallel, r.MeanParallel, r.MultiShare = old.PeakParallel, old.MeanParallel, old.MultiShare
		r.Pass, r.GradeDetail, r.Exit, r.Ending = old.Pass, old.GradeDetail, old.Exit, old.Ending
		r.USDEnvelope, r.WallSeconds = old.USDEnvelope, old.WallSeconds
		rows = append(rows, r)
	}
	csvPath := filepath.Join(root, "replan-reread.csv")
	_ = os.Remove(csvPath)
	if err := writeCSVHeader(csvPath); err != nil {
		return err
	}
	for _, r := range rows {
		if err := appendCSV(csvPath, r); err != nil {
			return err
		}
		fmt.Fprintln(out, r.oneLine())
	}
	summary := summarize(p, rows)
	fmt.Fprintln(out)
	fmt.Fprint(out, summary)
	return os.WriteFile(filepath.Join(root, "summary-reread.md"), []byte(summary), 0o600)
}

// goCacheEnv pins the toolchain's caches to the ones this machine already
// has. Every invocation moves HOME to its throwaway home, and without this the
// workers' `go test` would rebuild the standard library in every cell.
func goCacheEnv() []string {
	var env []string
	for _, name := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		out, err := exec.Command("go", "env", name).Output()
		if err != nil {
			continue
		}
		if value := strings.TrimSpace(string(out)); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// runOne executes one invocation and answers its row.
func runOne(iv invocation, o options, goEnv []string, book *ledger) row {
	if err := os.MkdirAll(iv.RunDir, 0o700); err != nil {
		return skippedRow(iv, err.Error())
	}
	pristine, err := pristineFixture(iv.Cell)
	if err != nil {
		return skippedRow(iv, "read the fixture: "+err.Error())
	}
	if err := materializeFixture(iv.fixtureDir(), pristine); err != nil {
		return skippedRow(iv, "seed the fixture: "+err.Error())
	}
	if err := writeProfile(iv); err != nil {
		return skippedRow(iv, "write the profile: "+err.Error())
	}
	if err := os.WriteFile(iv.briefFile(), []byte(iv.Cell.brief), 0o600); err != nil {
		return skippedRow(iv, "write the brief: "+err.Error())
	}

	stdin, err := os.Open(iv.briefFile())
	if err != nil {
		return skippedRow(iv, err.Error())
	}
	defer stdin.Close()
	envelope, err := os.Create(filepath.Join(iv.RunDir, "do.json"))
	if err != nil {
		return skippedRow(iv, err.Error())
	}
	defer envelope.Close()
	stream, err := os.Create(filepath.Join(iv.RunDir, "run.log"))
	if err != nil {
		return skippedRow(iv, err.Error())
	}
	defer stream.Close()

	cmd := exec.Command(iv.Bin, iv.argv()...)
	cmd.Dir = iv.fixtureDir()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, envelope, stream
	cmd.Env = childEnv(os.Environ(), append(iv.armEnv(), goEnv...))
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return skippedRow(iv, "start the door: "+err.Error())
	}
	pid := cmd.Process.Pid
	_ = os.WriteFile(filepath.Join(iv.RunDir, "pid"), []byte(strconv.Itoa(pid)+"\n"), 0o600)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	watch := &watcher{store: storePath(iv.fixtureDir()), calls: callLogPath(iv.homeDir())}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	backstop := time.After(iv.Wall + 3*time.Minute)
	var (
		waitErr     error
		interrupted string
		killAt      <-chan time.Time
	)
loop:
	for {
		select {
		case waitErr = <-done:
			break loop
		case <-ticker.C:
			watch.poll()
			book.setLive(iv.label(), watch.usd)
			if interrupted == "" {
				switch {
				case watch.usd >= o.cellCap:
					interrupted = fmt.Sprintf("cell cap: $%.4f spent of $%.2f", watch.usd, o.cellCap)
				case book.total() >= o.totalCap:
					interrupted = fmt.Sprintf("total cap: $%.4f spent of $%.2f", book.total(), o.totalCap)
				}
				if interrupted != "" {
					// THE ONE PROCESS THIS DRIVER STARTED, BY ITS RECORDED PID, and
					// nothing else: the door lands what it reached on an interrupt.
					_ = cmd.Process.Signal(os.Interrupt)
					killAt = time.After(interruptGrace)
				}
			}
		case <-killAt:
			_ = cmd.Process.Kill()
			killAt = nil
		case <-backstop:
			interrupted = "the driver's backstop wall"
			_ = cmd.Process.Kill()
			backstop = nil
		}
	}
	wall := time.Since(started)
	watch.poll()
	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if waitErr != nil {
		exitCode = -1
	}

	r := readBack(iv, pristine, watch)
	r.Exit = exitCode
	r.Ending = exitWord(exitCode)
	if interrupted != "" {
		r.Ending += " (" + interrupted + ")"
	}
	env := readEnvelope(filepath.Join(iv.RunDir, "do.json"))
	r.USDEnvelope = env.SpendUSD
	r.WallSeconds = env.Seconds
	if r.WallSeconds <= 0 {
		r.WallSeconds = wall.Seconds()
	}
	g := gradeCell(iv.Cell, iv.fixtureDir(), pristine)
	r.Pass, r.GradeDetail = g.Pass, g.Detail
	_ = writeJSON(filepath.Join(iv.RunDir, "row.json"), r)
	return r
}

// childEnv is the door's environment: the driver's own, with every name the
// arm sets replaced by the arm's value. The key rides in from the driver's
// environment untouched.
func childEnv(base, set []string) []string {
	names := map[string]bool{}
	for _, kv := range set {
		name, _, _ := strings.Cut(kv, "=")
		names[name] = true
	}
	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !names[name] {
			out = append(out, kv)
		}
	}
	return append(out, set...)
}

// writeProfile writes the throwaway profile: the crew rows and nothing else.
func writeProfile(iv invocation) error {
	if err := os.MkdirAll(iv.homeDir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(profileTiers(iv.Model, iv.PlanModel), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(iv.homeDir(), "config.json"), raw, 0o600)
}

// storePath is where `codeaf do` keeps a run's plan store: the working copy's
// own .codeaf/plandb.db (internal/session's PlanStorePath).
func storePath(workspace string) string {
	return filepath.Join(workspace, ".codeaf", "plandb.db")
}

// callLogPath is the throwaway home's model-call log (internal/calllog).
func callLogPath(home string) string {
	return filepath.Join(home, "logs", "calls.jsonl")
}

// exitWords is the door's exit ladder in the words the rows quote.
var exitWords = map[int]string{
	0:   "done",
	1:   "could-not-run",
	2:   "ran-not-finished",
	3:   "limit-stopped",
	4:   "needed-answer",
	124: "wall",
}

func exitWord(code int) string {
	if word, ok := exitWords[code]; ok {
		return word
	}
	return "exit-" + strconv.Itoa(code)
}

// envelope is the part of the door's JSON envelope the bench reads.
type envelope struct {
	OK       bool    `json:"ok"`
	Stop     string  `json:"stop"`
	SpendUSD float64 `json:"spend_usd"`
	Seconds  float64 `json:"seconds"`
}

func readEnvelope(path string) envelope {
	var e envelope
	raw, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(raw, &e)
	}
	return e
}

// ── the fixtures ───────────────────────────────────────────────────────────

// fixtureFiles is a fixture's own files by slash path: the pristine state
// every tests-were-not-weakened check is made against.
type fixtureFiles map[string][]byte

var fixturesFlag string

// fixturesRoot finds the fixtures: the flag, then beside this package's
// source, then from the checkout's root.
func fixturesRoot() string {
	if fixturesFlag != "" {
		return fixturesFlag
	}
	for _, candidate := range []string{"fixtures", filepath.Join("bench", "replan", "fixtures")} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return filepath.Join("bench", "replan", "fixtures")
}

func pristineFixture(c cell) (fixtureFiles, error) {
	files := fixtureFiles{}
	root := filepath.Join(fixturesRoot(), c.fixture)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = body
		return nil
	})
	if err == nil && len(files) == 0 {
		err = fmt.Errorf("fixture %s is empty", root)
	}
	return files, err
}

// materializeFixture writes the fixture fresh into dir and commits it, so the
// run starts from one known commit and its landing has a branch to stand on.
func materializeFixture(dir string, files fixtureFiles) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for name, body := range files {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, 0o600); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=replan bench", "-c", "user.email=bench@localhost", "add", "-A"},
		{"-c", "user.name=replan bench", "-c", "user.email=bench@localhost", "commit", "-q", "-m", "fixture seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// ── the CSV ────────────────────────────────────────────────────────────────

func writeCSVHeader(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write(csvHeader)
	w.Flush()
	return w.Error()
}

func appendCSV(path string, r row) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write(r.csvValues())
	w.Flush()
	return w.Error()
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
