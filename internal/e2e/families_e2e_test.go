//go:build e2e

// TASK FAMILIES, END TO END, ON BOTH GROUNDS.
//
// WHAT THIS LANE PROVES. A task that turns out to be wider than one worker hands
// parts out under itself, each part works somewhere of its own, and the whole
// family's product comes home into the person's material — on a git repository
// and on a plain folder alike. Everything below is asserted ON DISK: the files
// in the person's own directory, the mirror's git history, the session's task
// records and the division records in the workers' own journals. Not one
// assertion reads the model's prose, because a model saying it divided the work
// is exactly the claim this file exists to check.
//
// THE THREE SCENARIOS, AND THE ISSUES THEY STAND ON:
//
//   - a three-section report on a FOLDER ground (#230, #229): the mirror is a
//     repository, the parts cut worktrees off it and merge back into it, and the
//     person's folder gets every part's file laid over it by name — and never a
//     .git of its own.
//   - a two-part write-up on a REPOSITORY ground (#232): the parent writes its
//     plan down first, the parts start in a world that holds it, and the family
//     lands on the person's branch in one move.
//   - a division whose two parts claim the same file (#231): refused at
//     admission, before any part exists, and the worker carries on alone.
//
// WHAT IT COSTS AND HOW IT IS PINNED. Every call in the run — the conversation,
// the shaper, the sizing judge, the namer, each worker, the division's reviewer
// — rides deepseek/deepseek-v4-flash, because [pinEveryTextModel] writes the
// model into every row that can choose one: the four tiers, the task row, every
// text role and the fallback chain. That pinning is then CHECKED rather than
// assumed: each scenario reads the machine's own usage ledger back and fails if
// any other model answered.
//
//	go test -tags e2e -count=1 -timeout 45m -v -run TestFamilies ./internal/e2e/
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	// familyWall is how long one scenario may take from the moment the task is
	// admitted. A cheap model doing three eighty-word sections is minutes; ten
	// is the far edge of that and still an answer rather than a hang.
	familyWall = 10 * time.Minute
	// familyAttempts is the ask and one retry. A cheap model may keep small work
	// in its own hands the first time round, which is a reading of the work and
	// not a fault — so the ask is put twice before the lane calls it a failure,
	// and the failure quotes what the road actually decided.
	familyAttempts = 2
	// familyCap is what one scenario may spend before this lane says something is
	// wrong with the run rather than with the work. A settled family of three
	// parts on this model measures in cents; a dollar is a runaway.
	familyCap = 1.00
	// familyLanes is how many nodes may run at once. It is written down rather
	// than left to the person's own row because the division's second gate is
	// exactly this number — a machine pinned to one lane refuses every division
	// with `refused:lane`, and a lane that refused for that reason would be
	// measuring the profile rather than the road (task_divide.go's freeHands).
	familyLanes = 4
)

// ── the throwaway machine, with every model row pinned ──────────────────────

// pinEveryTextModel puts [e2eModel] in every row that can choose a model for a
// text call, so that a run of this file is one model's behaviour and not a
// profile's. [newWorld] has already written the talk row and the low and high
// tiers; this adds the two profile-only tiers, the task row, the role pins and
// the fallback chain.
//
// THE FOUR MEDIA ROLES ARE LEFT ALONE, deliberately, and it is cmd/aforge's own
// reasoning under `--one-model`: vision, image generation, speech and video are
// capability-qualified, so pinning them at a text model would not make the run
// single-model, it would make it broken. Nothing in this file makes media.
func pinEveryTextModel(t *testing.T) {
	t.Helper()
	registry := config.NewSettings(config.SettingsOptions{})
	write := func(key, value string) {
		row, found := registry.Row(key)
		if !found {
			t.Fatalf("the settings registry has no row %q", key)
		}
		if err := row.Apply(value); err != nil {
			t.Fatalf("write %s=%q: %v", key, value, err)
		}
	}
	write(config.KeyTierReflexModel, e2eModel)
	write(config.KeyTierMastermindModel, e2eModel)
	write(config.KeyTaskModel, e2eModel)
	write(config.KeyModelFallbacks, e2eModel)
	write(config.KeyTaskParallel, strconv.Itoa(familyLanes))
	var pins []string
	for _, role := range textRoles {
		pins = append(pins, string(role)+":"+e2eModel)
	}
	write(config.KeyModelRoles, strings.Join(pins, ","))
	t.Logf("PINNED every text model row at %s (tiers, task, %d roles, fallbacks); lanes=%d",
		e2eModel, len(textRoles), familyLanes)
}

// textRoles is every role in internal/roles that answers with words. The media
// four are absent for [pinEveryTextModel]'s reason.
var textRoles = []roles.Role{
	roles.RoleTitle, roles.RoleCompaction, roles.RoleConsolidate, roles.RoleGuardian,
	roles.RolePlanner, roles.RoleDesigner, roles.RoleWorker, roles.RoleAuditor,
	roles.RoleReflex, roles.RoleRouter, roles.RoleRouterConfirm, roles.RoleMarkReader,
	roles.RoleHandoff, roles.RoleTaskName, roles.RoleShaper, roles.RoleIntake,
	roles.RoleDivision, roles.RoleCareful,
}

// familyConfig is what the v3 door wires for work, applied to a conversation
// this lane drives directly. Everything here is a row cmd/aforge reads
// (chatv3.go's applyV3Governance); the two departures from a person's own
// launch are named where they are made.
func familyConfig(w *world) func(*session.Config) {
	profile := w.settings.ProfileDir
	return func(cfg *session.Config) {
		// THE DIVISION ROAD ITSELF. Nil here is the whole feature off — no
		// divide_work on any worker's belt — so every scenario below depends on
		// this line, exactly as the ambient lane depends on Standing.
		cfg.Divide = true
		cfg.TaskModel = config.TaskModelAt(profile)
		cfg.TaskParallel = config.TaskParallelAt(profile)
		cfg.TaskSettle = config.TaskSettleAt(profile)
		cfg.ModelFallbacks = config.ParseModelFallbacks(config.ModelFallbacksAt(profile))
		// NOBODY IS AT A KEYBOARD. A proposal a node makes under itself would
		// otherwise wait for a card nobody can press until the turn's context
		// dies; false is the headless reading the engine already has for it
		// (task.go's askTask).
		cfg.AskConsent = false
		cfg.TaskAutoApproveSeconds = 0
		// THE VERIFIED FRONTIER IS OFF, AND IT IS A SCOPE RULING RATHER THAN A
		// CONVENIENCE. What this lane measures is where a part works and how its
		// work comes home; the check is a separate gate with its own tests, and
		// leaving it on would put a cheap checker's opinion between a finished
		// part and the person's folder — so a red here would say nothing about
		// isolation or landing. Repair rounds go with it for the same reason.
		cfg.TaskAudit = false
		cfg.TaskRepairRounds = 0
		// AND THE WORKERS MAY WRITE. The ambient lane's policy allows the
		// reading tools only, which is right for a conversation that reads and
		// remembers; a task worker writes files and runs git, and a node's own
		// consent question reaches no drain loop out here.
		policy, err := approval.Load(map[string]any{"default": "allow"})
		if err != nil {
			panic("approval.Load: " + err.Error())
		}
		cfg.ApprovalPolicy = &policy
	}
}

// ── one scenario's run ──────────────────────────────────────────────────────

// taskRow is one node of the graph checkpoint, as the file on disk holds it
// (internal/session's task_store.go). It is re-declared here rather than
// imported because the record is unexported — which is the point of an
// end-to-end lane: what a reader outside the engine can see is the file.
type taskRow struct {
	ID       uint64   `json:"id"`
	Parent   uint64   `json:"parent"`
	Title    string   `json:"title"`
	State    string   `json:"state"`
	Report   string   `json:"report"`
	Ending   string   `json:"ending"`
	Changed  []string `json:"changed"`
	Branch   string   `json:"branch"`
	Worktree string   `json:"worktree"`
	Merge    string   `json:"merge"`
	Ground   string   `json:"ground"`
	Mode     string   `json:"groundMode"`
	Rung     string   `json:"groundRung"`
	CostUSD  float64  `json:"costUsd"`
}

// settled reports whether this node has come to rest. The engine spells no word
// for it: a node is settled when it is neither queued nor running, which is the
// same reading task_run.go's reportTaskNode makes before it writes the project's
// index row.
func (r taskRow) settled() bool {
	return r.State != string(session.TaskQueued) && r.State != string(session.TaskRunning)
}

// division is one line of a worker's journal saying what the division road
// decided (internal/session's journalDivision). Re-declared for taskRow's
// reason.
type division struct {
	TaskID    uint64   `json:"taskId"`
	Source    string   `json:"source"`
	Requested int      `json:"requested"`
	Admitted  int      `json:"admitted"`
	Decision  string   `json:"decision"`
	Error     string   `json:"error"`
	Parts     []string `json:"parts"`
}

// familyRun is one attempt at one scenario: a fresh ground, a fresh
// conversation, one task launched at the person's own door and waited out.
type familyRun struct {
	t         *testing.T
	w         *world
	agent     *session.Agent
	place     session.Place
	ground    string
	root      uint64
	rows      []taskRow
	divisions []division
	wall      time.Duration
	usd       float64
	models    []string
	timedOut  bool
}

// runFamily launches one task on one ground and waits for the whole family to
// come to rest.
//
// THE TASK IS STARTED AT THE PERSON'S OWN DOOR ([session.Agent.StartTask], which
// is what `/task` calls) rather than through a chat turn that hopes the model
// reaches for propose_task. Both doors end at the same line — [TaskGraph.admit],
// which resolves the ground, arms the division road and turns the frontier — and
// the typed one takes one model's whim out of a test whose subject is what
// happens AFTER a task starts.
//
// AND THE GROUND IS SAID RATHER THAN GUESSED. [session.Agent.ReferPlace] is the
// door a surface calls when a person names a folder — the picker, a directory
// dropped on the window — and it answers the ground ladder at its `said` rung
// (taskstands.go), which is how the work comes to be about the person's material
// rather than about the directory this conversation happens to stand in.
func runFamily(t *testing.T, w *world, ground, ask string, attempt int) *familyRun {
	t.Helper()
	desk := filepath.Join(t.TempDir(), "desk")
	if err := os.MkdirAll(desk, 0o755); err != nil {
		t.Fatalf("make the conversation's own folder: %v", err)
	}
	place := w.place(w.projectBucket(desk), desk)
	agent := w.openAt(desk, place, familyConfig(w))

	if _, err := agent.ReferPlace(ground, session.PlaceSaid); err != nil {
		t.Fatalf("refer %s: %v", ground, err)
	}
	run := &familyRun{t: t, w: w, agent: agent, place: place, ground: ground}

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), familyWall)
	defer cancel()

	// THE SIZING JUDGE FIRST, exactly as the typed door asks it: a yes is banked
	// against this text and is what arms the worker to hand the work out
	// (task_person.go, task_divide.go's armDivision). A no is not a failure of
	// this call — the brief's own text can arm the road too — so it is logged
	// and the task starts either way.
	wide, parts, why := agent.JudgeDecomposable(ctx, ask)
	t.Logf("SIZING attempt %d → wide=%v parts=%v why=%q", attempt, wide, parts, why)

	id, title, err := agent.StartTask(ctx, ask)
	if err != nil {
		t.Fatalf("start the task: %v", err)
	}
	run.root = id
	t.Logf("TASK %d started: %q  (ground %s)", id, title, ground)

	run.timedOut = !run.waitForRest(ctx)
	run.wall = time.Since(started)
	run.rows = readTaskRows(t, place.Tasks())
	run.divisions = readDivisions(t, place)
	run.usd, run.models = ledgerSince(t, started)
	run.report(attempt)
	if run.timedOut {
		t.Fatalf("attempt %d did not come to rest inside %s; the nodes are:\n%s\nand the divisions on record are:\n%s",
			attempt, familyWall, run.nodeLog(), run.divisionLog())
	}
	return run
}

// waitForRest polls the graph's own checkpoint until every node in it has
// settled. It reads THE FILE rather than the live graph, because the file is
// what a surface, a resumed process and this test all have — and because the
// checkpoint is rewritten on every transition, so a poll cannot miss one.
func (r *familyRun) waitForRest(ctx context.Context) bool {
	for {
		rows := readTaskRows(r.t, r.place.Tasks())
		if len(rows) > 0 && allSettled(rows) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}

func allSettled(rows []taskRow) bool {
	for _, row := range rows {
		if !row.settled() {
			return false
		}
	}
	return true
}

// node answers one row by id.
func (r *familyRun) node(id uint64) (taskRow, bool) {
	for _, row := range r.rows {
		if row.ID == id {
			return row, true
		}
	}
	return taskRow{}, false
}

// rootRow is the family's own node, or a failure naming what is there.
func (r *familyRun) rootRow() taskRow {
	row, found := r.node(r.root)
	if !found {
		r.t.Fatalf("the checkpoint holds no node %d; it holds:\n%s", r.root, r.nodeLog())
	}
	return row
}

// parts are the nodes the family handed out, in id order.
func (r *familyRun) parts() []taskRow {
	var out []taskRow
	for _, row := range r.rows {
		if row.Parent == r.root {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// admitted is every division the road admitted for this family's root.
func (r *familyRun) admitted() []division {
	return r.divisionsDeciding("admitted")
}

// divisionsDeciding is every record for this family's root carrying one decision.
func (r *familyRun) divisionsDeciding(decision string) []division {
	var out []division
	for _, one := range r.divisions {
		if one.TaskID == r.root && one.Decision == decision {
			out = append(out, one)
		}
	}
	return out
}

// report is the one block a person reading the log wants per attempt: what it
// took in wall time, what it cost, and which models actually answered.
func (r *familyRun) report(attempt int) {
	// WHICH READER ALLOWED THIS WORK TO SPLIT, in the project's own word:
	// `judged` for the sizing call at the typed door, `counted` for a brief that
	// named enough separate items on its own, and ABSENT for a task that was
	// never given the verb at all (task_index.go's MaySplit). It is the first
	// thing an autopsy of a family that stayed one worker needs.
	arming := "(no settled row)"
	if row, found := indexRowFor(r, r.root); found {
		if arming = row.MaySplit; arming == "" {
			arming = "(never armed — the worker had no divide_work at all)"
		}
	}
	r.t.Logf("SCENARIO attempt %d · wall %s · cost $%.6f · models %s · parts %d · may split: %s",
		attempt, r.wall.Round(time.Second), r.usd, strings.Join(r.models, ","), len(r.parts()), arming)
	r.t.Logf("  nodes:\n%s", r.nodeLog())
	r.t.Logf("  divisions:\n%s", r.divisionLog())
	if said := r.refusalLog(); said != "" {
		r.t.Logf("  what the road told the worker:\n%s", said)
	}
	if r.usd > familyCap {
		r.t.Errorf("this scenario spent $%.4f, past the $%.2f a settled family of this size costs on %s",
			r.usd, familyCap, e2eModel)
	}
	// THE PIN IS CHECKED AND NEVER ASSUMED. Every row was written at one model;
	// a second id in the ledger means a row this file does not know about chose
	// a model, and every figure above would be about a mixture.
	for _, model := range r.models {
		if model != e2eModel {
			r.t.Errorf("a call rode %q; every model row in this run is pinned at %q", model, e2eModel)
		}
	}
}

func (r *familyRun) nodeLog() string {
	var lines []string
	for _, row := range r.rows {
		lines = append(lines, fmt.Sprintf("    #%d parent=%d state=%s merge=%s mode=%s rung=%s cost=$%.4f wrote=%v title=%q ending=%q",
			row.ID, row.Parent, row.State, row.Merge, row.Mode, row.Rung, row.CostUSD, row.Changed, row.Title, row.Ending))
	}
	if len(lines) == 0 {
		return "    (none)"
	}
	return strings.Join(lines, "\n")
}

func (r *familyRun) divisionLog() string {
	var lines []string
	for _, one := range r.divisions {
		lines = append(lines, fmt.Sprintf("    task=%d source=%s requested=%d admitted=%d decision=%s parts=%v error=%q",
			one.TaskID, one.Source, one.Requested, one.Admitted, one.Decision, one.Parts, one.Error))
	}
	if len(lines) == 0 {
		return "    (the road was never asked)"
	}
	return strings.Join(lines, "\n")
}

// refusalLog is every sentence the division road handed back to a worker, as the
// worker's own journal holds it. The RECORD says which gate refused and the
// SENTENCE says what about — the path two parts claimed, whether anything was
// spent — and an autopsy of a family that stayed one worker needs both.
func (r *familyRun) refusalLog() string {
	said := r.journals()
	var lines []string
	seen := map[string]bool{}
	for at := 0; ; {
		found := strings.Index(said[at:], "not split:")
		if found < 0 {
			break
		}
		start := at + found
		end := start + refusalWindow
		if end > len(said) {
			end = len(said)
		}
		one := strings.SplitN(said[start:end], `\n`, 2)[0]
		if !seen[one] {
			seen[one] = true
			lines = append(lines, "    "+one)
		}
		at = start + len("not split:")
	}
	return strings.Join(lines, "\n")
}

// refusalWindow is how much of a refusal is quoted: enough for the path it names
// and the reason, and not the whole paragraph twice per attempt.
const refusalWindow = 320

// journals is every line of every journal this family wrote, joined — what an
// autopsy reads, and what a refusal's own sentence is asserted against.
func (r *familyRun) journals() string {
	var whole strings.Builder
	for _, path := range nodeJournals(r.t, r.place) {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		whole.Write(raw)
	}
	return whole.String()
}

// ── the scenarios ───────────────────────────────────────────────────────────

func TestFamilies(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	// THE WIDTH FLOOR IS OFF FOR THIS LANE, and it is the switch cmd/aforge's own
	// tests use (partsroute_test.go, method_test.go). internal/splitgate refuses a
	// division whose evidence names fewer than six separate items, which is a
	// finding about whether handing work out PAYS — a question this lane is not
	// asking. Three sections and two write-ups are the smallest families that can
	// prove isolation and landing, and they are deliberately below that floor.
	t.Setenv("AFORGE_SPLITGATE", "0")

	t.Run("a three-section report on a folder ground", func(t *testing.T) {
		threeSectionsOnAFolder(t, &world{t: t, home: w.home, settings: w.settings, store: w.store})
	})
	t.Run("a two-part write-up on a repository ground", func(t *testing.T) {
		twoPartsOnARepository(t, &world{t: t, home: w.home, settings: w.settings, store: w.store})
	})
	t.Run("two parts that claim one file", func(t *testing.T) {
		oneFileClaimedTwice(t, &world{t: t, home: w.home, settings: w.settings, store: w.store})
	})
}

// threeSectionsOnAFolder is #230 and #229 together: a family on a plain folder
// gets a family tree of its own, its parts get worktrees off it, and everything
// they wrote comes home into the person's directory.
func threeSectionsOnAFolder(t *testing.T, w *world) {
	const ask = `Write a three-section report into the folder this conversation is already about.
It covers nine topics and lands as three separate files: a.md, b.md and c.md —
a.md holds the first three topics, b.md the next three, c.md the last three, about
eighty words each. The three sections do not depend on each other, so split this
with divide_work into THREE parts, one file each.

In each part's brief name ONLY the one file that part writes, and say that the
part writes that one file and nothing else. Name no other file anywhere in that
brief — not a sibling's file, not something to read, not a path in the folder —
because a file named in two briefs is two parts claiming one file.`

	run := familyWithParts(t, w, newFolderGround, ask, func(run *familyRun) bool {
		root, found := run.node(run.root)
		return found && root.Mode == string(session.TaskModeMirror) && len(run.parts()) >= 2
	}, "no mirrored family with parts beside each other came of this ask")

	root := run.rootRow()
	if !samePath(t, root.Ground, run.ground) {
		t.Fatalf("the family stood on %q, not on the person's folder %q", root.Ground, run.ground)
	}
	if _, err := os.Stat(filepath.Join(run.ground, ".git")); err == nil {
		t.Errorf("the person's plain folder gained a .git; the family tree is the MIRROR and nothing of it reaches their directory (#230)")
	}
	if root.Merge != "inplace" {
		t.Errorf("the family came home as %q; a mirrored family lays its ledger over the person's folder by name", root.Merge)
	}

	// THE FAMILY TREE. A mirrored family's tree is the private copy of the
	// person's folder, under this session's own trees/, opened as a repository of
	// its own with one baseline commit for its parts to cut worktrees from (#230).
	mirror := root.Worktree
	if mirror == "" {
		t.Fatalf("the family root recorded no working copy; there is no family tree to read")
	}
	if !withinTrees(t, run.place, mirror) {
		t.Errorf("the family tree is at %q, which is not under this session's trees/ (%s)", mirror, run.place.Trees())
	}
	if info, err := os.Stat(filepath.Join(mirror, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("the family tree at %s is not a repository of its own (#230 opens it with git init): %v", mirror, err)
	}
	if log := gitAt(t, mirror, "log", "--all", "--format=%s"); !strings.Contains(log, "the material this work started from") {
		t.Errorf("the family tree has no baseline commit; its history is:\n%s", log)
	}

	eachPartWorkedApart(t, run, root)
	brought := whatCameBack(t, run, mirror)
	theLedgerShips(t, run, brought)
}

// twoPartsOnARepository is the repository half, and #232's with it: the parent
// writes its plan down first, hands two parts out, and the whole family lands on
// the person's branch in one move.
//
// THE PLAN TOKEN IS THE WHOLE OF THE #232 ASSERTION. It is minted here and
// written into NOTES.md by the parent BEFORE it divides, so a part cut off a
// world that held the parent's work has NOTES.md in the tree of its own commit
// and a part cut off the ground's HEAD does not. Nothing about it can be guessed,
// and no part is asked to say anything about it.
func twoPartsOnARepository(t *testing.T, w *world) {
	token := "PLAN-TOKEN-" + planToken()
	ask := fmt.Sprintf(`This work is about the repository this conversation is already about. There are
eight topics to write up. Write NOTES.md first: a short plan for a two-part
write-up with, on a line of its own, exactly this: %s

Then split the rest with divide_work into TWO parts. One writes x.md and takes the
first four topics; the other writes y.md and takes the last four. Each is about
sixty words and the two are independent of each other.

In each part's brief say which ONE file that part writes and what goes in it, and
name no other file anywhere in that brief — not the plan, not the sibling's file,
not a path in the project — because a file named in two briefs is two parts
claiming one file. Say in each brief that the part writes that one file and
nothing else.`, token)

	run := familyWithParts(t, w, newRepositoryGround, ask, func(run *familyRun) bool {
		root, found := run.node(run.root)
		return found && root.Mode == string(session.TaskModeWorktree) && len(run.parts()) >= 2
	}, "no repository family with parts beside each other came of this ask")

	root := run.rootRow()
	if !samePath(t, root.Ground, run.ground) {
		t.Fatalf("the family stood on %q, not on the person's repository %q", root.Ground, run.ground)
	}
	if root.Merge != "merged" {
		t.Errorf("the family came home as %q; a repository family merges its branch into the person's", root.Merge)
	}

	eachPartWorkedApart(t, run, root)
	brought := whatCameBack(t, run, run.ground)
	theLedgerShips(t, run, brought)

	// ONE LANDING. The family's whole history arrives on the person's branch in a
	// single move. A merge that was a fast-forward writes no commit and is that
	// same one move, which is why the reading is a bound rather than an equality:
	// what would be wrong is the family arriving in several.
	merges := lines(gitAt(t, run.ground, "log", "--merges", "--format=%h %s"))
	t.Logf("  the person's branch carries %d merge commit(s): %v", len(merges), merges)
	if len(merges) > 1 {
		t.Errorf("the person's branch took %d merges; one landing is one move onto their branch", len(merges))
	}

	// #232: DID EACH PART START IN A WORLD THAT HELD THE PARENT'S NOTES?
	//
	// IT IS READ OFF THE PART'S OWN COMMIT AND NEVER OUT OF ITS PROSE. The commit
	// a part's file arrived on is the tip of the branch that part worked on, and
	// that branch was cut from whatever world the part was handed — so asking git
	// for NOTES.md in that commit's tree asks exactly the question the issue does:
	// was the parent's on-disk work under the part when it started? A part told to
	// quote the plan would be answering with words instead, which is the one kind
	// of evidence this file does not take.
	//
	// The finding is reported two ways. Where the family history holds a `wip:`
	// checkpoint the token is required, because that commit is the mechanism; where
	// it does not, the absence is printed as a pending finding rather than as this
	// lane's own red, since a test failing for work that has not landed says
	// nothing about the work that has.
	checkpoint := checkpointCommit(t, run.ground)
	if checkpoint != "" {
		t.Logf("  the family history holds the divide-time checkpoint %q (#232)", checkpoint)
	}
	for name, commit := range brought {
		if name == "NOTES.md" {
			// The plan file itself says nothing about this: a part that wrote it
			// wrote the token in with it, and the reading has to be about a
			// commit the part did NOT make the plan on.
			continue
		}
		if strings.Contains(gitTry(run.ground, "show", commit+":NOTES.md"), token) {
			t.Logf("  the commit %s arrived on carries the parent's NOTES.md with its plan token, so that part opened on the parent's own work (#232)", name)
			continue
		}
		if checkpoint != "" {
			t.Errorf("the commit %s arrived on holds no NOTES.md carrying %q, though the family history holds the checkpoint %q — a part cut off that commit had it on disk (#232)",
				name, token, checkpoint)
			continue
		}
		t.Logf("  PENDING #232 — the commit %s arrived on holds no NOTES.md with the plan token, and there is no wip: checkpoint in the family history; that part did not open on the parent's uncommitted work", name)
	}
}

// ── the two laws both grounds keep ──────────────────────────────────────────

// eachPartWorkedApart is the isolation law read off the records: every part in a
// working copy of ITS OWN, under this session's trees/, on a branch of its own,
// and never in its parent's. It is the same law on both grounds because that is
// the whole of what #230 came to make true — isolation is always a worktree of
// the family tree, whether that tree is the person's repository or the mirror of
// their folder.
func eachPartWorkedApart(t *testing.T, run *familyRun, parent taskRow) {
	t.Helper()
	seen := map[string]bool{}
	for _, part := range run.parts() {
		if part.Worktree == "" {
			t.Errorf("part #%d recorded no working copy of its own — it worked in the parent's (the defect #230 repairs)", part.ID)
			continue
		}
		if samePath(t, part.Worktree, parent.Worktree) {
			t.Errorf("part #%d worked in the family tree itself, not in a copy of its own", part.ID)
		}
		if !withinTrees(t, run.place, part.Worktree) {
			t.Errorf("part #%d worked at %q, which is not under this session's trees/", part.ID, part.Worktree)
		}
		if seen[resolved(part.Worktree)] {
			t.Errorf("part #%d shared a working copy with a sibling", part.ID)
		}
		seen[resolved(part.Worktree)] = true
		if !strings.HasPrefix(part.Branch, "task/") {
			t.Errorf("part #%d has branch %q; a part cuts a branch of its own off the family tree", part.ID, part.Branch)
		}
	}
}

// whatCameBack is every file a part MERGED back into the family tree, with the
// commit it arrived on. A part's work reaches that tree exactly one way — its own
// commit, on its own branch, merged in — so the history is the proof, and the
// answer is what the landing laws below are then read against.
//
// A PART WHOSE BRANCH DID NOT MERGE IS SAID OUT LOUD AND IS NOT THIS LANE'S RED.
// A conflicted branch is kept and named, by design, and it happens here when a
// part writes outside the file its brief gave it — which is a finding about the
// worker rather than about isolation or landing. What must never happen is a
// file that came back and then did not ship, and that is the law below.
func whatCameBack(t *testing.T, run *familyRun, tree string) map[string]string {
	t.Helper()
	brought := map[string]string{}
	merged := 0
	for _, part := range run.parts() {
		switch {
		case len(part.Changed) == 0:
			t.Logf("  part #%d %q settled holding no files of its own", part.ID, part.Title)
			continue
		case part.Merge != "merged":
			t.Logf("  part #%d %q wrote %v and came home as %q, so its branch was kept rather than merged",
				part.ID, part.Title, part.Changed, part.Merge)
			continue
		}
		merged++
		for _, name := range part.Changed {
			line := strings.TrimSpace(gitAt(t, tree, "log", "--format=%H %s", "--", name))
			if line == "" {
				t.Errorf("%s, which part #%d merged, is in no commit of the family tree; its work never came back (#230)", name, part.ID)
				continue
			}
			first := strings.SplitN(strings.Split(line, "\n")[0], " ", 2)
			brought[name] = first[0]
			if len(first) > 1 && !strings.HasPrefix(first[1], "task: ") {
				t.Errorf("%s arrived on %q, which is not a node's own work commit", name, first[1])
			}
		}
	}
	if merged == 0 {
		t.Errorf("no part brought work back into the family tree at all; %d part(s) ran", len(run.parts()))
	}
	if len(brought) > 1 && distinctValues(brought) < 2 {
		t.Errorf("every part's file arrived on one commit; the parts did not come back separately")
	}
	t.Logf("  %d part(s) merged, bringing back %v", merged, sortedNames(brought))
	return brought
}

// theLedgerShips is #229 read from both ends. A file a part brought back is on
// the FAMILY'S OWN ledger — the parent absorbs what its parts landed — and every
// file on that ledger is in the person's material. The second half is the whole
// of the data loss the issue names: a ledger that says a file shipped, and a
// folder that does not hold it.
func theLedgerShips(t *testing.T, run *familyRun, brought map[string]string) {
	t.Helper()
	ledger := indexFilesFor(t, run, run.root)
	for _, name := range sortedNames(brought) {
		if !contains(ledger, name) {
			t.Errorf("%s came back from a part and is not on the family's ledger, which names %v — a part's file that is not on the parent's ledger is a file that never ships (#229)", name, ledger)
		}
	}
	if len(ledger) == 0 {
		t.Errorf("the family settled with an empty ledger")
	}
	for _, name := range ledger {
		if body := readGroundFile(t, run.ground, name); strings.TrimSpace(body) == "" {
			t.Errorf("%s is on the family's ledger and is not in the person's material, or is empty — the family's product did not come home (#229's landing)", name)
		}
	}
	t.Logf("  the family's ledger is %v, and all of it is in the person's material", ledger)
}

// oneFileClaimedTwice is #231: a division whose parts claim the same path is
// refused before any part exists, and the worker carries on with the work in its
// own hands.
func oneFileClaimedTwice(t *testing.T, w *world) {
	const ask = `Write report.md, a short report on seven findings, in two sections.

Before you write anything, make ONE divide_work call, with exactly two parts and
with "seven findings to write up" as the evidence:
  · part one is called "opening section" and its brief says it writes the opening
    section of report.md;
  · part two is called "closing section" and its brief says it writes the closing
    section of report.md.
Both briefs name report.md. That is deliberate: this ask is about what happens
when two parts are given the same file, so write it that way and do not redraw the
boundary.

Then, whatever answer comes back, write report.md yourself with both sections in
it and say in your report what you were told about the split.`

	run := familyWithParts(t, w, newFolderGround, ask, func(run *familyRun) bool {
		return len(run.divisionsDeciding("refused:scope")) > 0
	}, "no division was ever refused for scope")

	refusals := run.divisionsDeciding("refused:scope")
	for _, one := range refusals {
		if one.Admitted != 0 {
			t.Errorf("a scope refusal admitted %d part(s); the refusal stands BEFORE any part exists (#231)", one.Admitted)
		}
		if one.Requested < 2 {
			t.Errorf("a scope refusal was recorded for %d part(s); two parts are what can claim one path", one.Requested)
		}
	}
	t.Logf("  the road refused %d division(s) for scope: %v", len(refusals), refusals)

	// THE REFUSAL NAMED THE FILE. The record carries the decision and the parts'
	// titles; the sentence the worker was handed is what names the path, and it
	// is in that worker's own journal as an ordinary tool result.
	said := run.journals()
	if !strings.Contains(said, "report.md is claimed by more than one part") {
		t.Errorf("no refusal in this family's journals names report.md; a refusal a worker cannot act on is the thing #231's sentence exists to avoid")
	}
	if !strings.Contains(said, "nothing is cancelled and nothing is spent.") {
		t.Errorf("the refusal did not say the division cost nothing; the free reading is what makes a worker willing to redraw the boundary")
	}

	// AND NOTHING WAS BORN OF IT.
	if parts := run.parts(); len(parts) > 0 {
		for _, part := range parts {
			t.Logf("    part #%d %q wrote %v", part.ID, part.Title, part.Changed)
		}
		t.Errorf("%d part(s) exist under a family whose only division was refused for scope", len(parts))
	}

	// THE WORKER CARRIED ON. A refusal is a finding about these boundaries and
	// never about the work, so the task still reaches rest and the deliverable is
	// still on the person's disk.
	root := run.rootRow()
	if root.State == string(session.TaskFailed) {
		t.Errorf("the family failed (%s: %s) after a refusal it was meant to carry on through", root.Ending, root.Report)
	}
	// WHERE THE WORK STOOD IS WHERE THE DELIVERABLE IS. This scenario says nothing
	// about the ground ladder — a report is work a shaper may legitimately place
	// in the conversation's own folder — so the file is looked for where the node
	// recorded that it worked, which is the question actually being asked: did the
	// worker carry on after the refusal.
	if body := readGroundFile(t, root.Ground, "report.md"); strings.TrimSpace(body) == "" {
		t.Errorf("report.md is not in %s, where this work stood; the refused division stopped the work instead of redirecting it", root.Ground)
	}
}

// ── the ask, and the one retry ──────────────────────────────────────────────

// familyWithParts runs one scenario until the road did what the scenario is
// about, and no more than [familyAttempts] times.
//
// A CHEAP MODEL MAY KEEP SMALL WORK IN ITS OWN HANDS, which is a reading of the
// work rather than a fault in the road — so the ask is put twice on a fresh
// ground. What is NOT allowed is passing quietly: a scenario that never divided
// fails with every division record the run wrote, so an autopsy can tell a model
// that never asked from a road that said no and from which gate said it.
func familyWithParts(t *testing.T, w *world, ground func(*testing.T) string, ask string,
	enough func(*familyRun) bool, missing string) *familyRun {
	t.Helper()
	var last *familyRun
	for attempt := 1; attempt <= familyAttempts; attempt++ {
		run := runFamily(t, w, ground(t), ask, attempt)
		if enough(run) {
			return run
		}
		t.Logf("attempt %d: %s", attempt, missing)
		last = run
	}
	t.Fatalf("%s in %d attempts.\nthe nodes were:\n%s\nthe divisions on record were:\n%s",
		missing, familyAttempts, last.nodeLog(), last.divisionLog())
	return nil
}

// ── the two grounds ─────────────────────────────────────────────────────────

// newFolderGround is the person's plain directory: material, no history.
func newFolderGround(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "material")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make the person's folder: %v", err)
	}
	// IT IS DELIBERATELY EMPTY. Anything seeded here would be read by every part
	// and therefore NAMED in every part's brief, and a path two briefs name is two
	// parts claiming one file (#231) — so a seed file would refuse the division
	// this scenario is about before it began. A family that starts from nothing
	// and writes everything is the ordinary shape of a drafting task anyway, and
	// the family tree is opened with an empty baseline for exactly that case.
	return dir
}

// newRepositoryGround is the person's checkout: one commit, one branch, an
// identity of their own so the harness's commits are told apart from theirs.
func newRepositoryGround(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make the person's repository: %v", err)
	}
	gitAt(t, dir, "init", "--quiet")
	gitAt(t, dir, "config", "user.name", "the person")
	gitAt(t, dir, "config", "user.email", "person@localhost")
	gitAt(t, dir, "config", "commit.gpgsign", "false")
	// The one file is a marker rather than material, for [newFolderGround]'s
	// reason: a repository needs a commit to cut a branch from, and material here
	// would be named in every part's brief.
	if err := os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644); err != nil {
		t.Fatalf("seed the person's repository: %v", err)
	}
	gitAt(t, dir, "add", ".keep")
	gitAt(t, dir, "commit", "--quiet", "-m", "the material")
	return dir
}

// ── readers ─────────────────────────────────────────────────────────────────

// readTaskRows reads the graph's checkpoint. A file that is not there yet is no
// nodes, which is what a task admitted a moment ago looks like.
func readTaskRows(t *testing.T, path string) []taskRow {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var document struct {
		Nodes []taskRow `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		// A checkpoint is written atomically, so a parse failure is a real
		// finding rather than a torn read.
		t.Fatalf("parse %s: %v", path, err)
	}
	return document.Nodes
}

// nodeJournals is every journal this session's nodes wrote, newest last.
func nodeJournals(t *testing.T, place session.Place) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(place.NodeJournals(), "*.jsonl"))
	if err != nil {
		t.Fatalf("read the node journals: %v", err)
	}
	sort.Strings(found)
	return found
}

// readDivisions is every division record every worker of this session wrote.
func readDivisions(t *testing.T, place session.Place) []division {
	t.Helper()
	var out []division
	for _, path := range nodeJournals(t, place) {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, `"division"`) {
				continue
			}
			var entry struct {
				Type     string    `json:"type"`
				Division *division `json:"division"`
			}
			if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "division" || entry.Division == nil {
				continue
			}
			out = append(out, *entry.Division)
		}
	}
	return out
}

// indexRowFor is one node AS THE PROJECT'S OWN RECORD HOLDS IT — the row written
// when the node settled, which is what an accept or a re-audit hours later reads
// and the only place the node's ledger outlives this process.
func indexRowFor(run *familyRun, id uint64) (session.TaskIndexEntry, bool) {
	rows := session.ReadTaskIndex(session.TaskIndexPath(run.place.Transcript()))
	want := strconv.FormatUint(id, 10)
	for _, row := range rows {
		if row.ID == want && row.SessionID == session.PlaceSession(run.place) {
			return row, true
		}
	}
	return session.TaskIndexEntry{}, false
}

// indexFilesFor is that row's ledger.
func indexFilesFor(t *testing.T, run *familyRun, id uint64) []string {
	t.Helper()
	row, found := indexRowFor(run, id)
	if !found {
		// The index row is written the moment a node settles, so a family at
		// rest with no row is a finding in itself.
		t.Fatalf("the project's index holds no settled row for node %d", id)
	}
	return row.Files
}

// ledgerSince is what this run spent and which models answered, read off the
// MACHINE'S OWN ledger under the throwaway home — the one file that carries
// every call at every depth, the auxiliary ones included, with no fold lines to
// double-count (internal/session's usage_ledger.go).
func ledgerSince(t *testing.T, since time.Time) (float64, []string) {
	t.Helper()
	raw, err := os.ReadFile(session.UsageLedgerPath())
	if err != nil {
		return 0, nil
	}
	var total float64
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var one session.UsageLine
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			continue
		}
		if one.At.Before(since) {
			continue
		}
		total += one.USD
		if one.Model != "" {
			seen[one.Model] = true
		}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return total, models
}

// checkpointCommit answers the divide-time checkpoint's subject where the family
// history holds one, and the empty string where it does not (#232).
func checkpointCommit(t *testing.T, repo string) string {
	t.Helper()
	for _, subject := range lines(gitAt(t, repo, "log", "--all", "--format=%s")) {
		if strings.HasPrefix(strings.ToLower(subject), "wip:") {
			return subject
		}
	}
	return ""
}

// readGroundFile is one file of the person's material, read raw off the disk so
// that an assertion about their folder is about their folder.
func readGroundFile(t *testing.T, ground, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ground, name))
	if err != nil {
		return ""
	}
	return string(raw)
}

// ── small tools ─────────────────────────────────────────────────────────────

// gitAt runs one git command and fails the test on anything it will not do.
func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitTry asks git something whose failure is an answer — "is this path in that
// commit" — and hands back the empty string where it is not.
func gitTry(dir string, args ...string) string {
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// samePath compares two paths as the disk sees them, because a temporary
// directory and the engine's own canonical spelling of it differ by symlinks on
// more than one platform.
func samePath(t *testing.T, left, right string) bool {
	t.Helper()
	return resolved(left) == resolved(right)
}

func resolved(path string) string {
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// withinTrees reports whether a directory is one of this session's own working
// copies.
func withinTrees(t *testing.T, place session.Place, dir string) bool {
	t.Helper()
	root := resolved(place.Trees())
	return root != "" && strings.HasPrefix(resolved(dir)+string(filepath.Separator), root+string(filepath.Separator))
}

func lines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// sortedNames is a map's keys in a stable order, so a log line and a failure name
// the same files in the same order twice running.
func sortedNames(byName map[string]string) []string {
	out := make([]string, 0, len(byName))
	for name := range byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want || filepath.Base(item) == want {
			return true
		}
	}
	return false
}

func distinctValues(byName map[string]string) int {
	seen := map[string]bool{}
	for _, value := range byName {
		seen[value] = true
	}
	return len(seen)
}

// planToken is a word no model could guess and no earlier run could leave
// behind, which is the whole of what makes the #232 reading honest.
func planToken() string {
	return strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 36)
}
