package main

// run.go — the live half of the driver.
//
// One invocation is one task started through the engine's own task door
// (session.New + StartTask) against a throwaway home, waited on through the
// same event lane a surface subscribes to, and read off the disk the engine
// itself wrote: the usage ledger, the family's journals, the graph's
// checkpoint. The construction is the one internal/e2e proves: nothing here
// touches an unexported field, so a cell that runs here is a cell the
// product can actually reach.
//
// Nothing in this file runs in a dry pass: main composes the plan, prints
// it, and returns before this code is reached. The provider key is read
// once, the way the product reads it, and only when a live run is asked for.

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── one invocation's record ─────────────────────────────────────────────────

// row is one invocation's line: the plan's identity and the readings and
// grade that came of it.
type row struct {
	Date         string
	Arm          Arm
	Seats        Seats
	Cell         string
	Replicate    int
	Model        string
	ModelsUsed   string
	Graded       bool
	GradeDetail  string
	Ending       string
	Report       string
	Steps        int
	Calls        int
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Unbilled     int
	WallSeconds  float64
	WallSource   string

	ChangedFiles  int
	ChildrenDone  int
	ChildrenTotal int
	NodesFailed   int

	InvalidActions int
	Truncations    int
	EditIdiomFlags int

	RunDir string
}

// csvHeader is the row order, fixed once. Rows are appended, never inserted —
// the bench protocol's own CSV law.
var csvHeader = []string{
	"date", "arm", "seats", "cell", "replicate", "model", "models_used",
	"graded", "ending", "report", "steps", "cost_usd", "unbilled",
	"wall_seconds", "wall_source", "changed_files",
	"children_done", "children_total", "nodes_failed",
	"invalid_actions", "truncations", "edit_idiom_flags", "run_dir",
	"calls", "in_tokens", "out_tokens", "out_per_call", "in_per_call",
}

func (r row) csvValues() []string {
	return []string{
		r.Date, string(r.Arm), string(r.Seats), r.Cell, strconv.Itoa(r.Replicate),
		r.Model, r.ModelsUsed,
		gradeWord(r.Graded), r.Ending, r.Report,
		strconv.Itoa(r.Steps), money(r.CostUSD), strconv.Itoa(r.Unbilled),
		seconds(r.WallSeconds), r.WallSource, strconv.Itoa(r.ChangedFiles),
		strconv.Itoa(r.ChildrenDone), strconv.Itoa(r.ChildrenTotal), strconv.Itoa(r.NodesFailed),
		strconv.Itoa(r.InvalidActions), strconv.Itoa(r.Truncations), strconv.Itoa(r.EditIdiomFlags),
		r.RunDir,
		strconv.Itoa(r.Calls), strconv.Itoa(r.InputTokens), strconv.Itoa(r.OutputTokens),
		ratio(r.outPerCall()), ratio(r.inPerCall()),
	}
}

// outPerCall and inPerCall are the row's derived token-to-call readings, the
// same ratios the ledger's own fields answer.
func (r row) outPerCall() float64 { return perCall(r.OutputTokens, r.Calls) }
func (r row) inPerCall() float64  { return perCall(r.InputTokens, r.Calls) }

// gradeWord is the graded column's own word: yes or no, the grader's answer
// and nothing more. The verdict words stay in the design document.
func gradeWord(pass bool) string {
	if pass {
		return "yes"
	}
	return "no"
}

func money(usd float64) string { return strconv.FormatFloat(usd, 'f', 6, 64) }

func seconds(s float64) string { return strconv.FormatFloat(s, 'f', 1, 64) }

// ratio is a per-call token reading's own column: one decimal is enough to
// compare two arms' verbosity without inventing precision.
func ratio(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// oneLine is the progress line the driver prints as an invocation lands.
func (r row) oneLine() string {
	return fmt.Sprintf("%s  graded=%s  ending=%s  steps=%d  $%.4f  wall=%.0fs(%s)  changed=%d  parts=%d/%d  invalid=%d  trunc=%d  idiom=%d",
		fmt.Sprintf("%s-%s-r%d", armLabel(r.Arm, r.Seats), r.Cell, r.Replicate), gradeWord(r.Graded), r.Ending, r.Steps, r.CostUSD,
		r.WallSeconds, r.WallSource, r.ChangedFiles, r.ChildrenDone, r.ChildrenTotal,
		r.InvalidActions, r.Truncations, r.EditIdiomFlags)
}

// ── the run ─────────────────────────────────────────────────────────────────

// runner carries what one live run shares across its invocations.
type runner struct {
	out  io.Writer
	csv  string
	date string
	rows []row
	// crew is the plan's resolved five seat rows, written into the throwaway
	// home of every crew arm.
	crew []crewRow
}

// live runs the plan, one invocation at a time, in the plan's order.
func live(p plan, out, errOut io.Writer) error {
	if !goAvailable() {
		return fmt.Errorf("the code cells grade themselves with the go toolchain, and `go` is not on this machine")
	}
	if p.Door == doorDo {
		if err := ensureBinary(errOut); err != nil {
			return fmt.Errorf("build the product binary for the do door: %w", err)
		}
	}
	key := machineKey()
	if key == "" {
		return fmt.Errorf("no provider key on this machine by any road the product reads " +
			"(OPENROUTER_API_KEY, OPENAI_API_KEY, or the profile's api_key row): " +
			"this bench drives a real model or it says nothing")
	}
	if err := os.MkdirAll(p.Out, 0o700); err != nil {
		return fmt.Errorf("create the output root: %w", err)
	}
	r := &runner{
		out:  out,
		csv:  filepath.Join(p.Out, "bashloop.csv"),
		date: time.Now().Format("2006-01-02"),
		crew: p.Crew,
	}
	if err := r.openCSV(); err != nil {
		return err
	}
	for _, iv := range p.Invocations {
		fmt.Fprintf(r.out, "· %s starting…\n", iv.label())
		row := r.runOne(iv)
		r.rows = append(r.rows, row)
		if err := r.appendRow(row); err != nil {
			return fmt.Errorf("append the CSV row: %w", err)
		}
		fmt.Fprintln(r.out, row.oneLine())
	}
	printTable(p, r.rows, out)
	if p.Mode == "pair" {
		printPair(p, r.rows, out)
	}
	return nil
}

// machineKey is the key a launch on this machine would talk with, read the
// way the product reads it — internal/e2e's livekey gate keeps this law for
// its lanes, and the bench keeps it too.
func machineKey() string {
	dir := strings.TrimSpace(config.ProfileDir())
	if dir == "" {
		dir = home.InheritedDir()
	}
	return strings.TrimSpace(config.APIKeyAt(dir))
}

// runOne executes one invocation and answers with its row.
func (r *runner) runOne(iv invocation) row {
	fixtureDir := filepath.Join(iv.RunDir, "fixture")
	homeDir := filepath.Join(iv.RunDir, "home")
	cellDef, ok := findCell(allCells(), iv.Cell)
	if !ok {
		return r.failedRow(iv, fmt.Sprintf("no cell %s in this plan", iv.Cell))
	}

	if _, err := materializeFixture(fixtureDir, cellDef); err != nil {
		return r.failedRow(iv, fmt.Sprintf("seed the fixture: %v", err))
	}

	// The env is the arm: the home is per-invocation, the belt switch is the
	// only thing that differs between the arms. Both are restored on the way
	// out, so the next invocation inherits nothing.
	restoreHome, err := setEnvVar(home.EnvVar, homeDir)
	if err != nil {
		return r.failedRow(iv, err.Error())
	}
	defer restoreHome()
	if err := copyProfileInto(homeDir); err != nil {
		return r.failedRow(iv, err.Error())
	}
	if err := config.WriteAPIKey("", machineKey()); err != nil {
		return r.failedRow(iv, fmt.Sprintf("write the key into the throwaway profile: %v", err))
	}
	// The seats are the second thing the env decides: a one-model arm writes
	// the one -model as the profile's chat model, exactly as before; a crew arm
	// writes the five tier rows the machine's own profile holds, so the run
	// resolves its seats the way a person's own settings would.
	if iv.Seats == SeatsCrew {
		if err := writeCrewProfile(homeDir, r.crew); err != nil {
			return r.failedRow(iv, fmt.Sprintf("write the crew into the throwaway profile: %v", err))
		}
	} else if err := config.WriteChatModel("", iv.Model); err != nil {
		return r.failedRow(iv, fmt.Sprintf("write %s: %v", iv.Model, err))
	}
	settings, err := config.Load()
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("read the throwaway profile: %v", err))
	}
	if iv.Seats != SeatsCrew {
		if chosen := config.ChatModelAt(settings.ProfileDir); chosen != iv.Model {
			return r.failedRow(iv, fmt.Sprintf("the door would open on %q, not %q", chosen, iv.Model))
		}
	}
	restoreBelt, err := applyBeltEnv(iv.Arm)
	if err != nil {
		return r.failedRow(iv, err.Error())
	}
	defer restoreBelt()

	// THE DOOR. Task door: the agent in this process, run.go's own path. Do
	// door: the binary as a subprocess (door.go), the rig's invocation. The
	// fixture seeding, the throwaway home, the belt env, the readings and the
	// graders are the two doors' shared frame; the door is the one thing that
	// differs between a task row and a do row.
	if iv.Door == doorDo {
		return r.runDoDoor(iv, cellDef, fixtureDir, homeDir, time.Now())
	}

	policy, err := approval.Load(map[string]any{"default": "allow", "tools": map[string]any{}})
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("load the approval policy: %v", err))
	}
	agent, place, err := openTaskAgent(settings, &policy, fixtureDir, homeDir, iv, r.crew)
	if err != nil {
		return r.failedRow(iv, err.Error())
	}
	startedAt := time.Now()
	notice, timedOut := awaitLanding(agent, place, iv)
	_ = agent.Close()
	agent.SettleWrites()
	wallDriver := time.Since(startedAt)
	if timedOut {
		fmt.Fprintf(r.out, "· %s hit the wall; the row says so\n", iv.label())
	}

	// THE TREE, NOT THE GROUND, carries the work: the task edits its own copy
	// under the session folder ([session.Place.Trees]), and the ground is only
	// the seed that copy was cut from — reading the ground back grades the
	// seed, and the seed is red by design.
	treeDir := taskTreeDir(place.Dir)
	pristine, err := pristineFixture(cellDef)
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("read the pristine fixture: %v", err))
	}
	reading := collectReadings(homeDir, place.Dir, place.NodeJournals(), treeDir, pristine)
	if notice != nil {
		reading.applyNotice(string(notice.State), notice.Report, notice.StartedAt, notice.EndedAt)
	}
	reading.WallDriver = wallDriver
	wall, wallSource := reading.wallSeconds()
	g := gradeCell(cellDef, treeDir, reading, pristine)

	return row{
		Date: r.date, Arm: iv.Arm, Seats: iv.Seats, Cell: iv.Cell, Replicate: iv.Replicate,
		Model: iv.Model, ModelsUsed: reading.modelsUsedLine(),
		Graded: g.Pass, GradeDetail: g.Detail,
		Ending: reading.Ending, Report: reading.Report,
		Steps: reading.Steps, Calls: reading.Calls,
		InputTokens: reading.InputTokens, OutputTokens: reading.OutputTokens,
		CostUSD: reading.CostUSD, Unbilled: reading.Unbilled,
		WallSeconds: wall, WallSource: wallSource,
		ChangedFiles: reading.ChangedFiles,
		ChildrenDone: reading.ChildrenDone, ChildrenTotal: reading.ChildrenTotal,
		NodesFailed:    reading.NodesFailed,
		InvalidActions: reading.InvalidActions, Truncations: reading.Truncations,
		EditIdiomFlags: reading.EditIdiomFlags,
		RunDir:         iv.RunDir,
	}
}

// taskTreeDir is the invocation's one working copy: the single tree the
// task machinery cut under the session folder. One task per invocation, so
// the first tree is the tree; a run that prepared none answers an empty dir,
// and its rows say so.
func taskTreeDir(placeDir string) string {
	entries, err := os.ReadDir(filepath.Join(placeDir, "trees"))
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(placeDir, "trees", entry.Name())
		}
	}
	return ""
}

// failedRow is the row a run that never started still leaves: graded no,
// ending naming the door that stopped it. The spend backstop's own honesty.
func (r *runner) failedRow(iv invocation, why string) row {
	return row{
		Date: r.date, Arm: iv.Arm, Seats: iv.Seats, Cell: iv.Cell, Replicate: iv.Replicate,
		Model: iv.Model, Ending: "setup-failed", Report: why,
		WallSource: "driver", RunDir: iv.RunDir,
	}
}

// writeCrewProfile writes the five tier rows into the throwaway home, each to
// the seat the machine's own profile holds. It goes through the settings rows'
// own validated write, so a value the sheet could refuse is refused here too.
func writeCrewProfile(homeDir string, crew []crewRow) error {
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: homeDir})
	for _, row := range crew {
		setting, ok := registry.Row(crewTierKey(row.Tier))
		if !ok {
			return fmt.Errorf("no settings row for the %s seat", row.Tier)
		}
		if err := setting.Apply(row.Model); err != nil {
			return fmt.Errorf("write the %s seat: %w", row.Tier, err)
		}
	}
	return nil
}

// crewTierKey names the settings row one tier word writes, so the surface and
// the bench agree on the key rather than spelling it twice.
func crewTierKey(tier string) string {
	switch tier {
	case config.ModelTierHigh:
		return config.KeyTierHighModel
	case config.ModelTierWorker:
		return config.KeyTierWorkerModel
	case config.ModelTierReflex:
		return config.KeyTierReflexModel
	case config.ModelTierMastermind:
		return config.KeyTierMastermindModel
	}
	return config.KeyTierLowModel
}

// crewWorkModel answers the model a crew arm opens its conversation on: the
// crew's own worker seat, the seat that does the work and pays most of a
// task's bill. An empty worker row follows the conversation, so it falls back
// to the pinned model rather than opening on nothing.
func crewWorkModel(homeDir string, crew []crewRow) string {
	for _, row := range crew {
		if row.Tier == config.ModelTierWorker {
			if model := strings.TrimSpace(config.TierSeatAt(homeDir, row.Tier).Model); model != "" {
				return model
			}
		}
	}
	return pinModel
}

// crewRoles answers the seat one roles key resolves to, read off the throwaway
// profile's tier rows. It is the seam internal/roles climbs its ladder through,
// which is how a crew arm's worker, planner and auxiliary calls each land on
// their own seat instead of the one pinned model.
func crewRoles(profileDir string) func(string) (string, bool) {
	byKey := map[string]string{
		config.KeyTierReflexModel:     config.ModelTierReflex,
		config.KeyTierLowModel:        config.ModelTierLow,
		config.KeyTierWorkerModel:     config.ModelTierWorker,
		config.KeyTierHighModel:       config.ModelTierHigh,
		config.KeyTierMastermindModel: config.ModelTierMastermind,
	}
	return func(key string) (string, bool) {
		tier, ok := byKey[key]
		if !ok {
			return "", false
		}
		model := strings.TrimSpace(config.TierSeatAt(profileDir, tier).Model)
		if model == "" {
			return "", false
		}
		return model, true
	}
}

// ── the task door ───────────────────────────────────────────────────────────

// openTaskAgent builds one live conversation in a session folder of its own
// under the throwaway home, assembled the way cmd/codeaf and internal/e2e
// assemble one: the run's model and key, an allow posture with the product's
// own floor, and nobody watching — so the clock, not a reader, admits the
// work, and every proposal the work itself makes is approved the same way.
func openTaskAgent(settings config.Config, policy *approval.Policy, workspace, homeDir string, iv invocation, crew []crewRow) (*session.Agent, session.Place, error) {
	id := session.NewSessionID()
	dir := filepath.Join(homeDir, "v3", "projects", "bashloop", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, session.Place{}, fmt.Errorf("mint a session folder: %w", err)
	}
	place := session.Place{Dir: dir, Workspace: workspace}
	if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: workspace, Created: time.Now()}); err != nil {
		return nil, session.Place{}, fmt.Errorf("write the session folder: %w", err)
	}
	// The seats decide the model the conversation opens on and the ladder every
	// auxiliary call climbs. A one-model arm opens on the pin and leaves the
	// ladder unwired — exactly the run the grid made before seats existed. A
	// crew arm opens on the crew's own worker seat and wires the ladder to the
	// throwaway profile's tier rows, so the worker, the planner and every
	// auxiliary call each ride their own seat.
	model, roles := pinModel, (func(string) (string, bool))(nil)
	if iv.Seats == SeatsCrew {
		model = crewWorkModel(homeDir, crew)
		roles = crewRoles(homeDir)
	}
	cfg := session.Config{
		Workspace:      workspace,
		Model:          model,
		RolesSource:    roles,
		APIKey:         settings.APIKey,
		BaseURL:        settings.BaseURL,
		CompactEnabled: true,
		SessionFile:    place.Transcript(),
		Place:          place,
		WorktreeRoot:   place.Trees(),
		ArtifactsIndex: home.Join("v3", "artifacts.jsonl"),
		ProfileDir:     settings.ProfileDir,
		ApprovalPolicy: policy,
		AskConsent:     false,
		// The product's own posture: the spending row on, one repair round.
		// Both arms get the same posture — that is what makes the comparison
		// honest (DESIGN.md, "What stays untouched").
		TaskAudit:        true,
		TaskRepairRounds: config.DefaultTaskRepairRounds,
	}
	agent, err := session.New(cfg)
	if err != nil {
		return nil, session.Place{}, err
	}
	return agent, place, nil
}

// awaitLanding waits for the root task's own ending on the event lane — the
// same lane every surface subscribes to — until the wall arrives first. On
// the wall it stops the work the way a person would, through the one cancel
// door, and gives the landing two minutes to say so before giving up on it.
func awaitLanding(agent *session.Agent, place session.Place, iv invocation) (*session.TaskNotice, bool) {
	updates, stop := agent.WatchTaskUpdates()
	defer stop()
	ctx := context.Background()

	rootID, _, _, err := agent.StartTask(ctx, iv.Brief, false)
	if err != nil {
		return nil, true
	}
	terminal := make(chan session.TaskNotice, 1)
	go func() {
		for event := range updates {
			if event.Task == nil || event.Task.ID != rootID {
				continue
			}
			notice := *event.Task
			switch notice.State {
			case session.TaskDone, session.TaskFailed, session.TaskUnverified:
				select {
				case terminal <- notice:
				default:
				}
				return
			}
			if notice.Stopped {
				select {
				case terminal <- notice:
				default:
				}
				return
			}
		}
	}()

	timeout := time.After(iv.Wall)
	select {
	case n := <-terminal:
		return &n, false
	case <-timeout:
		_, _ = agent.Cancel(session.CancelTask + ":" + strconv.FormatUint(rootID, 10))
		select {
		case n := <-terminal:
			return &n, true
		case <-time.After(2 * time.Minute):
			return nil, true
		}
	}
}

// applyBeltEnv sets the arm's belt switch and answers the restore. BOTH ARMS
// SET IT: the bash belt is the default, so an arm that unset the variable would
// ride the same belt as the other one and the run would be a comparison of a
// thing with itself. The save and the restore are writes, which os owns; the
// read is env's static door — the one door every owned name reads through.
func applyBeltEnv(arm Arm) (func(), error) {
	value := beltEnvFor(arm)
	oldValue, had := env.Lookup(beltEnvVar)
	if err := os.Setenv(beltEnvVar, value); err != nil {
		return nil, err
	}
	return func() {
		if had {
			_ = os.Setenv(beltEnvVar, oldValue)
		} else {
			_ = os.Unsetenv(beltEnvVar)
		}
	}, nil
}

// copyProfileInto borrows the machine's own profile the way internal/e2e
// does: read before any override lands, copied into the throwaway home, and
// never printed.
func copyProfileInto(homeDir string) error {
	src := filepath.Join(home.InheritedDir(), "config.json")
	raw, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read the profile at %s: %w", src, err)
	}
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return fmt.Errorf("make the throwaway home: %w", err)
	}
	return os.WriteFile(filepath.Join(homeDir, "config.json"), raw, 0o600)
}

// setEnvVar sets one variable and answers the restore. The name arrives at
// runtime, so the read is env's dynamic door: an owned name keeps its
// fallback, a foreign one passes through exactly as os records it.
func setEnvVar(name, value string) (func(), error) {
	oldValue, had := env.LookupValue(name)
	if err := os.Setenv(name, value); err != nil {
		return nil, err
	}
	return func() {
		if had {
			_ = os.Setenv(name, oldValue)
		} else {
			_ = os.Unsetenv(name)
		}
	}, nil
}

// ── the fixtures, from the embedded copy ────────────────────────────────────

// pristineFixture reads one cell's fixture from the branch's own copy: the
// reference state the not-weakened checks are made against. The fixtures
// are files under bench/bashloop/fixtures — a stranger can run one by hand,
// which is the replication law — and the driver finds them the way the
// other bench drivers find their own material: beside the source, or named
// with -fixtures.
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
	return files, err
}

// materializeFixture writes one cell's fixture fresh into dir and commits
// it, so the task starts from one known commit and the diff it leaves is
// measurable. Every invocation gets its own copy — bench/README.md's fresh
// clone, at Go scale.
func materializeFixture(dir string, c cell) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	files, err := pristineFixture(c)
	if err != nil {
		return "", err
	}
	for name, body := range files {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, body, 0o600); err != nil {
			return "", err
		}
	}
	for _, arg := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=bashloop bench", "-c", "user.email=bench@localhost", "add", "-A"},
		{"-c", "user.name=bashloop bench", "-c", "user.email=bench@localhost", "commit", "-q", "-m", "fixture seed"},
	} {
		if out, err := gitOutput(dir, arg...); err != nil {
			return "", fmt.Errorf("git %v: %v — %s", arg, err, strings.TrimSpace(out))
		}
	}
	sha, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// findCell answers one cell definition.
func findCell(cells []cell, id string) (cell, bool) {
	for _, c := range cells {
		if c.id == id {
			return c, true
		}
	}
	return cell{}, false
}

// fixturesRoot is where the driver finds the fixtures it seeds and grades
// against, resolved once per run: the -fixtures flag when given, then the
// directory beside this package's own source (running from bench/bashloop),
// then the repo-rooted spelling (running from the checkout's root). A
// resolver that finds nothing answers an error naming what it looked under —
// the door says where it looked, the way bench/e2e's own prerequisites do.
var fixturesFlag string

func fixturesRoot() string {
	if fixturesFlag != "" {
		return fixturesFlag
	}
	for _, candidate := range []string{"fixtures", filepath.Join("bench", "bashloop", "fixtures")} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	// Fall back to the default spelling; the failure message names it.
	return filepath.Join("bench", "bashloop", "fixtures")
}

// ── command helpers ─────────────────────────────────────────────────────────

// goTest runs the fixture's own suite. Its counts are the verdict, never the
// run's own account of itself.
func goTest(dir string) (string, error) {
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// goDoc reads a module's public surface the way a reader sees it.
func goDoc(dir string) (string, error) {
	cmd := exec.Command("go", "doc", "-all", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// gitOutput runs one git command in a directory and answers its stdout.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ── the CSV ─────────────────────────────────────────────────────────────────

// openCSV writes the header if the file is new and leaves an existing one
// exactly as it stands — rows append, columns never move.
func (r *runner) openCSV() error {
	if _, err := os.Stat(r.csv); err == nil {
		return nil
	}
	f, err := os.OpenFile(r.csv, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(csvHeader); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

// appendRow writes one row and flushes it, so a run stopped mid-grid keeps
// the rows it already paid for.
func (r *runner) appendRow(row row) error {
	f, err := os.OpenFile(r.csv, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(row.csvValues()); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

// ── the table ───────────────────────────────────────────────────────────────

// cellSummary is one arm-and-cell line of the printed table. Arm is the belt
// letter and Seats the seat word, together the arm's own name.
type cellSummary struct {
	Arm           Arm
	Seats         Seats
	Cell          string
	N             int
	Passes        int
	Models        string
	MedCalls      float64
	MedSteps      float64
	MedOutPerCall float64
	MedCost       float64
	MedWall       float64
	Invalid       int
	Truncs        int
	IdiomFlag     int
}

// summarize folds one arm-and-cell's rows into the doc's quoted readings:
// success rate, and over the successful cells the medians of steps, cost and
// wall — plus the branch-only diagnostics, counted over every row.
func summarize(rows []row, arm Arm, seats Seats, cellID string) (cellSummary, bool) {
	var mine []row
	for _, r := range rows {
		if r.Arm == arm && r.Seats == seats && r.Cell == cellID {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		return cellSummary{}, false
	}
	s := cellSummary{Arm: arm, Seats: seats, Cell: cellID, N: len(mine)}
	s.Models = modelsAcross(mine)
	var steps, costs, walls, calls, outPerCalls []float64
	for _, r := range mine {
		if r.Graded {
			s.Passes++
			steps = append(steps, float64(r.Steps))
			costs = append(costs, r.CostUSD)
			walls = append(walls, r.WallSeconds)
			calls = append(calls, float64(r.Calls))
			outPerCalls = append(outPerCalls, r.outPerCall())
		}
		s.Invalid += r.InvalidActions
		s.Truncs += r.Truncations
		s.IdiomFlag += r.EditIdiomFlags
	}
	s.MedSteps = median(steps)
	s.MedCalls = median(calls)
	s.MedOutPerCall = median(outPerCalls)
	s.MedCost = median(costs)
	s.MedWall = median(walls)
	return s, true
}

// modelsAcross names every model a group's rows billed, deduplicated in the
// order the readings met them. It is the arm's own account of what it ran on,
// which a crew arm is exactly what needs to be read.
func modelsAcross(rows []row) string {
	seen := map[string]bool{}
	var models []string
	for _, r := range rows {
		for _, model := range strings.Split(r.ModelsUsed, "+") {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			models = append(models, model)
		}
	}
	return strings.Join(models, "+")
}

// printTable prints the comparison as numbers, per arm and cell. The verdict
// words stay in the design document and the report; the driver prints
// numbers only. The arm column carries each arm's own name (belt AND seats),
// and the models column every model its rows billed.
func printTable(p plan, rows []row, out io.Writer) {
	dates := map[string]bool{}
	for _, r := range rows {
		dates[r.Date] = true
	}
	fmt.Fprintf(out, "\nper arm and cell (mode %s, model %s, out %s)\n", p.Mode, pinModel, p.Out)
	if len(dates) > 1 {
		fmt.Fprintf(out, "  note: rows span more than one day (%v); quote medians per day only\n", sortedKeys(dates))
	}
	fmt.Fprintf(out, "%-4s %-7s %3s %5s %6s %10s %10s %12s %10s %10s %8s %7s %7s %s\n",
		"cell", "arm", "n", "pass", "rate", "med calls", "med steps", "med out/call", "med $", "med wall", "invalid", "trunc", "idiom", "models")
	for _, c := range p.Cells {
		for _, arm := range p.Arms {
			s, ok := summarize(rows, arm.Belt, arm.Seats, c.id)
			if !ok {
				continue
			}
			fmt.Fprintf(out, "%-4s %-7s %3d %5d %6.2f %10.1f %10.1f %12.1f %10.4f %10.1f %8d %7d %7d %s\n",
				s.Cell, arm.name(), s.N, s.Passes, rate(s), s.MedCalls, s.MedSteps, s.MedOutPerCall, s.MedCost, s.MedWall,
				s.Invalid, s.Truncs, s.IdiomFlag, s.Models)
		}
	}
	for _, arm := range p.Arms {
		passed, total := 0, 0
		var armOutPerCalls []float64
		for _, r := range rows {
			if r.Arm != arm.Belt || r.Seats != arm.Seats {
				continue
			}
			total++
			if r.Graded {
				passed++
				armOutPerCalls = append(armOutPerCalls, r.outPerCall())
			}
		}
		if total == 0 {
			continue
		}
		fmt.Fprintf(out, "arm %s: %d of %d cells graded pass (%.0f%%), median out/call %.1f\n", arm.name(), passed, total,
			100.0*float64(passed)/float64(total), median(armOutPerCalls))
	}
}

func labelOf(arm Arm, seats Seats, cell string, replicate int) string {
	return fmt.Sprintf("%s-%s-r%d", armLabel(arm, seats), cell, replicate)
}

// sortedKeys answers a string-keyed set as a sorted list.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func rate(s cellSummary) float64 {
	if s.N == 0 {
		return 0
	}
	return float64(s.Passes) / float64(s.N)
}

// median answers the middle value, or the mean of the two middle ones. An
// empty list answers zero: there is nothing to quote, not a zero result.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// printPair is the pair mode's extra: the same question, answered on both
// belts, side by side — the reading the caller runs the grid on.
func printPair(p plan, rows []row, out io.Writer) {
	fmt.Fprintf(out, "\nsame question, both belts — brief:\n")
	for _, line := range splitLines(strings.TrimRight(p.Cells[0].brief, "\n")) {
		fmt.Fprintf(out, "  %s\n", line)
	}
	fmt.Fprintln(out)
	for _, r := range rows {
		fmt.Fprintf(out, "%s\n", labelOf(r.Arm, r.Seats, r.Cell, r.Replicate))
		fmt.Fprintf(out, "  graded: %s (%s)\n", gradeWord(r.Graded), r.GradeDetail)
		fmt.Fprintf(out, "  ending: %s — %s\n", r.Ending, firstLine(r.Report))
		fmt.Fprintf(out, "  steps=%d  calls=%d  out/call=%.1f  $%.4f  wall=%.0fs(%s)  models=%s\n",
			r.Steps, r.Calls, r.outPerCall(), r.CostUSD, r.WallSeconds, r.WallSource, r.ModelsUsed)
	}
}
