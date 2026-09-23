package main

// readings.go — the numbers, read from where the engine already keeps them.
//
// The run engine writes three records a bench can read without asking a
// model anything: the plan store (every task, its role, its ending, its
// result, and a spend row per worker launch with the model and the role it
// rode), each task's trajectory beside the store (one line per command the
// worker ran, with its exit, and one opening line per launch), and the home's
// model-call log (one priced row per provider call). Everything below is a
// reading of those three, plus a poll of the store while the run is going,
// which is the only way to see how many workers were out at once.

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // the plan store is SQLite, read read-only
)

// ── the poll ───────────────────────────────────────────────────────────────

// watcher is what the driver learns while an invocation runs.
type watcher struct {
	store string
	calls string

	usd       float64 // the call log's priced rows so far
	polls     int     // polls taken since the store first had a working task
	activeSum int     // the sum of working tasks over those polls
	peak      int     // the most tasks working at once
	multi     int     // polls with two or more tasks working
}

// poll reads the call log's spend and the store's working set once.
func (w *watcher) poll() {
	if usd, _, _ := readCallLog(w.calls); usd > w.usd {
		w.usd = usd
	}
	active, ok := workingNow(w.store)
	if !ok {
		return
	}
	if active == 0 && w.polls == 0 {
		return
	}
	w.polls++
	w.activeSum += active
	if active > w.peak {
		w.peak = active
	}
	if active >= 2 {
		w.multi++
	}
}

// workingNow counts the leaf tasks a worker holds right now: claimed or
// running, not parked, not a coordinator. A coordinator's own turn (the
// split, a wake) is short beside its children's and is not counted, so the
// figure is the width of the work rather than of the bookkeeping.
func workingNow(path string) (int, bool) {
	if _, err := os.Stat(path); err != nil {
		return 0, false
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return 0, false
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`SELECT count(*) FROM tasks WHERE status IN ('claimed','running') AND composite = 0 AND waiting = 0`).Scan(&n)
	if err != nil {
		return 0, false
	}
	return n, true
}

// readCallLog sums the priced rows of a home's model-call log, and names the
// models they were billed on.
func readCallLog(path string) (usd float64, calls int, models map[string]float64) {
	models = map[string]float64{}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, models
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scan.Scan() {
		var rec struct {
			Model string  `json:"model"`
			Cost  float64 `json:"cost"`
		}
		if json.Unmarshal(scan.Bytes(), &rec) != nil || rec.Cost <= 0 {
			continue
		}
		usd += rec.Cost
		calls++
		models[rec.Model] += rec.Cost
	}
	return usd, calls, models
}

// ── the store ──────────────────────────────────────────────────────────────

// storeTask is one row of the plan store, as much of it as the bench reads.
type storeTask struct {
	ID        string
	Parent    string
	Title     string
	Role      string
	Status    string
	Composite bool
	Result    string
}

// spendRow is one worker launch's spend: the store writes one when each
// worker's turn ends, with the model it was seated on and the role its shape
// gave it then.
type spendRow struct {
	Task  string
	Model string
	Role  string
	USD   float64
	At    string
}

func readStore(path string) (tasks []storeTask, spends []spendRow, rootID string, err error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, nil, "", err
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT root_id FROM meta WHERE id = 1`).Scan(&rootID); err != nil {
		return nil, nil, "", err
	}
	rows, err := db.Query(`SELECT id, parent_id, title, role, status, composite, result FROM tasks ORDER BY ord`)
	if err != nil {
		return nil, nil, rootID, err
	}
	for rows.Next() {
		var t storeTask
		var composite int
		if err := rows.Scan(&t.ID, &t.Parent, &t.Title, &t.Role, &t.Status, &composite, &t.Result); err == nil {
			t.Composite = composite != 0
			tasks = append(tasks, t)
		}
	}
	rows.Close()
	spent, err := db.Query(`SELECT task_id, model, role, usd, at FROM spend ORDER BY at`)
	if err == nil {
		for spent.Next() {
			var s spendRow
			if spent.Scan(&s.Task, &s.Model, &s.Role, &s.USD, &s.At) == nil {
				spends = append(spends, s)
			}
		}
		spent.Close()
	}
	return tasks, spends, rootID, nil
}

// ── the trajectories ───────────────────────────────────────────────────────

// trajectoryLine is the part of a trajectory line the bench reads
// (internal/run's Step).
type trajectoryLine struct {
	Kind     string `json:"kind"`
	Command  string `json:"command"`
	ExitCode *int   `json:"exit_code"`
	NotRun   bool   `json:"not_run"`
}

// taskTrace is one task's record, summed.
type taskTrace struct {
	Launches    int
	Steps       int
	FailedCmds  int
	TestRuns    int
	TestFailed  int
	Edited      map[string]int // basename -> edit steps
	StepsByLife []int          // steps per launch, in order
}

func readTrajectory(storeDir, id string) taskTrace {
	tr := taskTrace{Edited: map[string]int{}}
	f, err := os.Open(filepath.Join(storeDir, "tasks", id, "trajectory.jsonl"))
	if err != nil {
		return tr
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scan.Scan() {
		var line trajectoryLine
		if json.Unmarshal(scan.Bytes(), &line) != nil {
			continue
		}
		switch line.Kind {
		case "begin":
			tr.Launches++
			tr.StepsByLife = append(tr.StepsByLife, 0)
		case "step":
			if line.NotRun {
				continue
			}
			if len(tr.StepsByLife) == 0 {
				// A record from before launches were marked: one launch.
				tr.Launches = 1
				tr.StepsByLife = append(tr.StepsByLife, 0)
			}
			tr.Steps++
			tr.StepsByLife[len(tr.StepsByLife)-1]++
			failed := line.ExitCode != nil && *line.ExitCode != 0
			if failed {
				tr.FailedCmds++
			}
			if isTestRun(line.Command) {
				tr.TestRuns++
				if failed {
					tr.TestFailed++
				}
			}
			for _, name := range editedFiles(line.Command) {
				tr.Edited[name]++
			}
		}
	}
	return tr
}

// isTestRun recognizes a run of the module's tests.
func isTestRun(command string) bool {
	return strings.Contains(command, "go test")
}

var (
	patchTarget   = regexp.MustCompile(`codeaf\s+patch\s+['"]?([\w./-]+)`)
	redirectInto  = regexp.MustCompile(`(?:^|[^0-9&<>])>{1,2}\s*['"]?([\w./-]+)`)
	teeInto       = regexp.MustCompile(`\btee\s+(?:-a\s+)?['"]?([\w./-]+)`)
	openForWrite  = regexp.MustCompile(`open\(\s*['"]([\w./-]+)['"]\s*,\s*['"][wa]`)
	removeTargets = regexp.MustCompile(`\b(?:git\s+)?rm\s+((?:-\w+\s+)*)([\w./ -]+)`)
	goFileToken   = regexp.MustCompile(`[\w./-]+\.go\b`)
)

// editedFiles names, by basename, the files one shell command wrote,
// patched or removed. It reads the command as the model spelled it, so it is
// a heuristic: it knows the belt's own edit hand (`codeaf patch`), shell
// redirection, tee, in-place sed and perl, gofmt -w, python's open for
// writing, and rm. A read (cat, sed -n, grep) is never an edit. Basenames are
// enough because every fixture's file names are unique within it.
func editedFiles(command string) []string {
	seen := map[string]bool{}
	// add keeps a module file: a Go source or the module file. A bare name
	// (a directory) is kept only where the command removed it, which is how a
	// package is deleted; anywhere else a bare word after a ">" is far more
	// often a comparison inside a heredoc than a file.
	add := func(p string, bare bool) {
		p = strings.Trim(strings.TrimSpace(p), `'"`)
		if p == "" || strings.HasPrefix(p, "/dev/") || strings.HasPrefix(p, "&") {
			return
		}
		base := filepath.Base(strings.TrimRight(p, "/"))
		if base == "." || base == "/" || base == "" || base == ".." {
			return
		}
		switch {
		case strings.HasSuffix(base, ".go"), base == "go.mod":
			seen[base] = true
		case bare && !strings.Contains(base, "."):
			seen[base+"/"] = true
		}
	}
	for _, m := range patchTarget.FindAllStringSubmatch(command, -1) {
		add(m[1], false)
	}
	for _, m := range redirectInto.FindAllStringSubmatch(command, -1) {
		add(m[1], false)
	}
	for _, m := range teeInto.FindAllStringSubmatch(command, -1) {
		add(m[1], false)
	}
	for _, m := range openForWrite.FindAllStringSubmatch(command, -1) {
		add(m[1], false)
	}
	if strings.Contains(command, "sed -i") || strings.Contains(command, "perl -pi") ||
		strings.Contains(command, "perl -i") || strings.Contains(command, "gofmt -w") ||
		strings.Contains(command, "goimports -w") {
		for _, token := range goFileToken.FindAllString(command, -1) {
			add(token, false)
		}
	}
	for _, m := range removeTargets.FindAllStringSubmatch(command, -1) {
		for _, target := range strings.Fields(m[2]) {
			add(target, true)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ── the row ────────────────────────────────────────────────────────────────

// row is one invocation's line.
type row struct {
	Arm       string
	Cell      string
	Replicate int

	Pass        bool
	GradeDetail string
	Exit        int
	Ending      string

	USDLedger   float64 // the store's spend rows: what every worker launch cost
	USDCalls    float64 // the call log's priced rows: every provider call
	USDEnvelope float64 // the door's own account
	Calls       int
	WallSeconds float64

	Steps      int
	Launches   int
	Tasks      int // every task but the root
	Leaves     int // work tasks that are not checks, fixes or probes
	Checks     int
	Fixes      int
	Probes     int
	Composites int // tasks that split, the root included

	CheckHolds    int
	CheckNotHolds int

	RootLaunches int
	RootWakeUSD  float64 // what the root's launches after its first cost
	RootWakeStep int     // steps the root took after its first launch
	RootFirstUSD float64 // what the root's first launch cost
	RootModel    string  // the model the root's first launch rode

	PeakParallel float64
	MeanParallel float64
	MultiShare   float64 // share of polls with two or more workers

	TestRuns     int
	TestFailed   int
	FailedCmds   int
	OverlapFiles int    // files edited by two or more tasks
	OverlapNames string // which
	CellMetric   string // the cell's own measure, spelled key=value;...
	SpendByRole  string
	ModelsByUSD  string
	RunDir       string
}

var csvHeader = []string{
	"arm", "cell", "replicate", "pass", "grade", "exit", "ending",
	"usd_ledger", "usd_calls", "usd_envelope", "calls", "wall_s",
	"steps", "launches", "tasks", "leaves", "checks", "fixes", "probes", "composites",
	"check_holds", "check_not_holds",
	"root_launches", "root_first_usd", "root_wake_usd", "root_wake_steps", "root_model",
	"peak_parallel", "mean_parallel", "multi_share",
	"test_runs", "test_failed", "failed_cmds", "overlap_files", "overlap_names",
	"cell_metric", "spend_by_role", "models", "run_dir",
}

func (r row) csvValues() []string {
	f := func(v float64, prec int) string { return strconv.FormatFloat(v, 'f', prec, 64) }
	i := strconv.Itoa
	pass := "no"
	if r.Pass {
		pass = "yes"
	}
	return []string{
		r.Arm, r.Cell, i(r.Replicate), pass, r.GradeDetail, i(r.Exit), r.Ending,
		f(r.USDLedger, 6), f(r.USDCalls, 6), f(r.USDEnvelope, 6), i(r.Calls), f(r.WallSeconds, 1),
		i(r.Steps), i(r.Launches), i(r.Tasks), i(r.Leaves), i(r.Checks), i(r.Fixes), i(r.Probes), i(r.Composites),
		i(r.CheckHolds), i(r.CheckNotHolds),
		i(r.RootLaunches), f(r.RootFirstUSD, 6), f(r.RootWakeUSD, 6), i(r.RootWakeStep), r.RootModel,
		f(r.PeakParallel, 0), f(r.MeanParallel, 2), f(r.MultiShare, 2),
		i(r.TestRuns), i(r.TestFailed), i(r.FailedCmds), i(r.OverlapFiles), r.OverlapNames,
		r.CellMetric, r.SpendByRole, r.ModelsByUSD, r.RunDir,
	}
}

func (r row) oneLine() string {
	pass := "no"
	if r.Pass {
		pass = "yes"
	}
	return fmt.Sprintf("%s-%s-r%d pass=%s ending=%s $%.4f wall=%.0fs steps=%d tasks=%d leaves=%d checks=%d/%d fixes=%d peak=%.0f root_wakes=%d %s",
		r.Arm, r.Cell, r.Replicate, pass, r.Ending, r.USDCalls, r.WallSeconds, r.Steps, r.Tasks, r.Leaves,
		r.CheckNotHolds, r.Checks, r.Fixes, r.PeakParallel, max(r.RootLaunches-1, 0), r.CellMetric)
}

// skippedRow is the row an invocation that never ran still leaves.
func skippedRow(iv invocation, why string) row {
	return row{Arm: iv.Arm.Name, Cell: iv.Cell.id, Replicate: iv.Replicate, Ending: "setup-failed", GradeDetail: why, RunDir: iv.RunDir}
}

// readBack harvests one invocation's numbers after the door has ended.
func readBack(iv invocation, pristine fixtureFiles, w *watcher) row {
	r := row{Arm: iv.Arm.Name, Cell: iv.Cell.id, Replicate: iv.Replicate, RunDir: iv.RunDir}
	usd, calls, models := readCallLog(callLogPath(iv.homeDir()))
	r.USDCalls, r.Calls = usd, calls
	r.ModelsByUSD = joinSpend(models)
	if w.polls > 0 {
		r.PeakParallel = float64(w.peak)
		r.MeanParallel = float64(w.activeSum) / float64(w.polls)
		r.MultiShare = float64(w.multi) / float64(w.polls)
	}

	storeFile := storePath(iv.fixtureDir())
	tasks, spends, rootID, err := readStore(storeFile)
	if err != nil {
		r.CellMetric = "store unreadable: " + err.Error()
		return r
	}
	byRole := map[string]float64{}
	var rootSpends []spendRow
	for _, s := range spends {
		r.USDLedger += s.USD
		byRole[s.Role] += s.USD
		if s.Task == rootID {
			rootSpends = append(rootSpends, s)
		}
	}
	r.SpendByRole = joinSpend(byRole)
	if len(rootSpends) > 0 {
		r.RootFirstUSD = rootSpends[0].USD
		r.RootModel = rootSpends[0].Model
		for _, s := range rootSpends[1:] {
			r.RootWakeUSD += s.USD
		}
	}

	storeDir := filepath.Dir(storeFile)
	editors := map[string]map[string]bool{} // basename -> task ids that edited it
	traces := map[string]taskTrace{}
	for _, t := range tasks {
		tr := readTrajectory(storeDir, t.ID)
		traces[t.ID] = tr
		r.Steps += tr.Steps
		r.Launches += tr.Launches
		r.TestRuns += tr.TestRuns
		r.TestFailed += tr.TestFailed
		r.FailedCmds += tr.FailedCmds
		for name := range tr.Edited {
			if editors[name] == nil {
				editors[name] = map[string]bool{}
			}
			editors[name][t.ID] = true
		}
		if t.Composite {
			r.Composites++
		}
		if t.ID == rootID {
			r.RootLaunches = tr.Launches
			for i, n := range tr.StepsByLife {
				if i > 0 {
					r.RootWakeStep += n
				}
			}
			continue
		}
		r.Tasks++
		switch {
		case t.Role == "check":
			r.Checks++
			result := strings.ToLower(strings.TrimSpace(t.Result))
			switch {
			case strings.HasPrefix(result, "does not hold"):
				r.CheckNotHolds++
			case strings.HasPrefix(result, "holds"):
				r.CheckHolds++
			}
		case strings.HasPrefix(t.Title, "fix: "):
			r.Fixes++
		case t.Role == "probe":
			r.Probes++
		case !t.Composite:
			r.Leaves++
		}
	}
	var overlap []string
	for name, ids := range editors {
		if len(ids) >= 2 {
			overlap = append(overlap, fmt.Sprintf("%s×%d", name, len(ids)))
		}
	}
	sort.Strings(overlap)
	r.OverlapFiles = len(overlap)
	r.OverlapNames = strings.Join(overlap, " ")
	r.CellMetric = cellMetric(iv.Cell, iv.fixtureDir(), pristine, tasks, traces, editors)
	_ = writeJSON(filepath.Join(iv.RunDir, "tasks.json"), tasks)
	return r
}

// cellMetric is each cell's own measure of repeated or wasted work.
func cellMetric(c cell, workDir string, pristine fixtureFiles, tasks []storeTask, traces map[string]taskTrace, editors map[string]map[string]bool) string {
	switch c.id {
	case "r1":
		// How many tasks touched the one function behind five reports, and how
		// many of the five symptom files were patched where the bug showed.
		local := 0
		var patched []string
		for _, name := range r1SymptomFiles {
			if fileChanged(workDir, name, pristine) {
				local++
				patched = append(patched, name)
			}
		}
		return fmt.Sprintf("canon_editors=%d;symptom_files_patched=%d;patched=%s",
			len(editors["canon.go"]), local, strings.Join(patched, "+"))
	case "r2":
		// How many tasks edited a caller, and how many distinct callers were
		// edited by more than one task.
		callers := []string{"billing.go", "catalog.go", "inventory.go", "mailer.go", "reviews.go", "shipping.go"}
		editing := map[string]bool{}
		shared := 0
		for _, name := range callers {
			for id := range editors[name] {
				editing[id] = true
			}
			if len(editors[name]) > 1 {
				shared++
			}
		}
		return fmt.Sprintf("caller_editing_tasks=%d;callers_edited_twice=%d", len(editing), shared)
	case "r3":
		// How many times money.go was edited, and whether anybody fixed the
		// package the brief never named.
		edits := 0
		for _, tr := range traces {
			edits += tr.Edited["money.go"]
		}
		return fmt.Sprintf("money_edit_steps=%d;export_changed=%t;report_changed=%t",
			edits, fileChanged(workDir, "export/export.go", pristine), fileChanged(workDir, "report/report.go", pristine))
	case "r4":
		return fmt.Sprintf("words_edit_steps=%d", sumEdits(traces, "words.go"))
	}
	return ""
}

func sumEdits(traces map[string]taskTrace, name string) int {
	n := 0
	for _, tr := range traces {
		n += tr.Edited[name]
	}
	return n
}

// fileChanged says whether a fixture file no longer holds its seed bytes.
func fileChanged(dir, name string, pristine fixtureFiles) bool {
	want, ok := pristine[name]
	if !ok {
		return false
	}
	have, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	return err != nil || string(have) != string(want)
}

// joinSpend spells a dollar map as key:$x, largest first.
func joinSpend(m map[string]float64) string {
	type kv struct {
		k string
		v float64
	}
	var list []kv
	for k, v := range m {
		if k == "" {
			k = "?"
		}
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	parts := make([]string, 0, len(list))
	for _, e := range list {
		parts = append(parts, fmt.Sprintf("%s:$%.4f", e.k, e.v))
	}
	return strings.Join(parts, " ")
}
