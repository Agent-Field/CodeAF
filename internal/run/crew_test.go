package run_test

// The crew's tests read the profile the way internal/config's own tests do —
// one JSON object under the profile directory, written with the keys the
// settings sheet names — and judge the factory by the model it asks the
// completer for, since that is the model the worker's calls go out on and the
// model its spend row carries. Nothing here runs a provider: the completerFor
// seam records what it was asked for, and the one test that reads a spend row
// runs the worker through its scripted seat and reads the store's ledger back.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// crewProfile makes a throwaway profile holding exactly these crew rows — a
// value for a row somebody wrote, the empty string for a row they cleared, and
// nothing at all for a key of a vintage that never had one — and answers the
// directory the factory is pointed at.
func crewProfile(t *testing.T, rows map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeCrew(t, dir, rows)
	return dir
}

// writeCrew rewrites the crew the profile holds, in place, the shape a second
// launch reads: the same one-file write internal/config's tests make.
func writeCrew(t *testing.T, dir string, rows map[string]string) {
	t.Helper()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(dir), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// recordingCompleter is the completerFor seam: it remembers the model the
// factory resolved for each seat it was asked to build, in the order asked,
// and hands back a scripted seat so nothing goes near a provider.
type recordingCompleter struct {
	models []string
	script []step
}

func (r *recordingCompleter) forModel(model string) session.Completer {
	r.models = append(r.models, model)
	return &seat{script: r.script}
}

// TestSeatForMapsBeltRolesToCrewSeats keeps the work, fix, and plan seats in
// place while a check takes the careful work seat.
func TestSeatForMapsBeltRolesToCrewSeats(t *testing.T) {
	for _, test := range []struct {
		role, want string
	}{
		{plandb.RoleWork, config.ModelTierWorker},
		{plandb.RoleCheck, config.ModelTierHigh},
		{"fix", config.ModelTierWorker},
		{plandb.RolePlan, config.ModelTierMastermind},
	} {
		if got := run.SeatFor(test.role); got != test.want {
			t.Errorf("SeatFor(%q) = %q, want %q", test.role, got, test.want)
		}
	}
}

// TestCrewFactorySeatsEachRoleInItsTier is the shape rule the factory exists
// for: a task with children is a coordinator and takes the mastermind row, a
// leaf takes the worker row, and check and probe are read off the role they were
// declared with.
func TestCrewFactorySeatsEachRoleInItsTier(t *testing.T) {
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "one", Title: "One", ParentID: store.RootID()},
		{ID: "two", Title: "Two", ParentID: store.RootID()},
		{ID: "review", Title: "Review", Role: plandb.RoleCheck},
		{ID: "discriminate", Title: "Discriminate", Role: plandb.RoleProbe},
	}); err != nil {
		t.Fatalf("add the leaves: %v", err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierReflexModel:     "vendor/reflex",
		config.KeyTierLowModel:        "vendor/small",
		config.KeyTierWorkerModel:     "vendor/worker",
		config.KeyTierHighModel:       "vendor/careful",
		config.KeyTierMastermindModel: "vendor/thinking",
	})
	recorder := &recordingCompleter{}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, recorder.forModel)

	for _, test := range []struct {
		task, want string
	}{
		{store.RootID(), "vendor/thinking"},
		{"one", "vendor/worker"},
		{"review", "vendor/careful"},
		{"discriminate", "vendor/small"},
	} {
		recorder.models = nil
		factory(*store.Task(test.task))
		if len(recorder.models) != 1 || recorder.models[0] != test.want {
			t.Errorf("task %s was seated on %v, want the %s seat's model", test.task, recorder.models, test.want)
		}
	}
}

// TestCrewFactoryRunsTheDoorsSeatsWhateverTheProfileSays is the defect this
// file exists for: the seat a door resolved is the seat EVERY launch takes,
// including the root woken again to fold its children and every leaf a planner
// adds mid-run. The profile's own mastermind and worker rows name OTHER models,
// so a factory that fell back to the profile for those seats would seat the
// wrong model and this would see it.
//
// The check rides the careful work tier from the profile; the probe row
// is the profile's still, since no door names it.
func TestCrewFactoryRunsTheDoorsSeatsWhateverTheProfileSays(t *testing.T) {
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "one", Title: "One", ParentID: store.RootID()},
		{ID: "two", Title: "Two", ParentID: store.RootID()},
		{ID: "review", Title: "Review", Role: plandb.RoleCheck},
		{ID: "discriminate", Title: "Discriminate", Role: plandb.RoleProbe},
	}); err != nil {
		t.Fatalf("add the leaves: %v", err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierLowModel:        "vendor/profile-small",
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	recorder := &recordingCompleter{}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{
		Work: "vendor/named-work",
		Plan: "vendor/named-plan",
	}, recorder.forModel)

	for _, test := range []struct {
		task, want string
	}{
		// The root is a coordinator; every launch of it — the first and the
		// wake that folds its children — rides the plan seat.
		{store.RootID(), "vendor/named-plan"},
		// A leaf does the work itself and rides the work seat.
		{"one", "vendor/named-work"},
		// A check rides the profile's careful work seat; the probe row
		// is the profile's own.
		{"review", "vendor/profile-careful"},
		{"discriminate", "vendor/profile-small"},
	} {
		recorder.models = nil
		factory(*store.Task(test.task))
		if len(recorder.models) != 1 || recorder.models[0] != test.want {
			t.Errorf("task %s was seated on %v, want %q", test.task, recorder.models, test.want)
		}
	}
}

// TestCrewFactoryFallsBackToTheWorkerRowForAnEmptyTier: a row cleared on purpose
// reads empty, and a tier with no model falls to the worker row rather than
// seating the task on nothing. The check rides the careful work tier, whose
// row is cleared here.
func TestCrewFactoryFallsBackToTheWorkerRowForAnEmptyTier(t *testing.T) {
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "Review", Role: plandb.RoleCheck}}); err != nil {
		t.Fatalf("add the review: %v", err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierHighModel:   "",
		config.KeyTierWorkerModel: "vendor/worker",
	})
	recorder := &recordingCompleter{}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, recorder.forModel)

	factory(*store.Task("review"))

	if len(recorder.models) != 1 || recorder.models[0] != "vendor/worker" {
		t.Fatalf("the review was seated on %v, want the worker row's model", recorder.models)
	}
}

// runWorkTask adds one ordinary part to the run's plan and answers it.
//
// A TEST ABOUT THE WORKER ROW ASKS FOR A WORK TASK. Reaching for the root was
// convenient rather than meant: the root is a planning task from the moment the
// store opens (plandb's loadOrCreate seeds RolePlan on it, so that the pass
// which decides the split is seated on the thinking tier), and a fixture that
// picks it up gets the thinking seat while asserting the worker row. The three
// tests below are about the crew's worker row and the profile memo, and neither
// has anything to do with which seat a root takes.
func runWorkTask(t *testing.T, store *plandb.Store) plandb.Task {
	t.Helper()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "work-one", Title: "One", Description: "one ordinary part"}}); err != nil {
		t.Fatalf("add a work task: %v", err)
	}
	// CLAIMED BY THE NAME THE FINISH WILL USE. `plandb done <id> --agent <id>`
	// is how these tests report work, and the store only accepts a finish from
	// the agent holding the claim — the root had a special case for that and a
	// leaf does not.
	if _, err := store.Claim("work-one", "work-one"); err != nil {
		t.Fatalf("claim the work task: %v", err)
	}
	task := store.Task("work-one")
	if task == nil {
		t.Fatal("the store lost the work task it just added")
	}
	return *task
}

// TestCrewFactoryRefusesATaskTheCrewCannotSeat: a tier row and a worker row both
// empty is a task no model can run, and the refusal names the tier so a person
// knows which row has to be filled.
func TestCrewFactoryRefusesATaskTheCrewCannotSeat(t *testing.T) {
	store := runOpenStore(t)
	dir := crewProfile(t, map[string]string{config.KeyTierWorkerModel: ""})
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, (&recordingCompleter{}).forModel)

	task := runWorkTask(t, store)
	worker := factory(task)
	_, err := worker.Run(runContext(t), task)

	if err == nil {
		t.Fatal("a task the crew cannot seat ran")
	}
	if !strings.Contains(err.Error(), config.ModelTierWorker) {
		t.Fatalf("the refusal = %q, want the tier it could not fill", err)
	}
}

// TestCrewFactoryReadsTheCrewAgainAtEachLaunch: the profile is asked per task,
// so a crew change between two launches is seen by the second rather than frozen
// into the first. The second value is longer on purpose, so the profile memo's
// own size reading sees the write whatever the filesystem's clock resolution is.
func TestCrewFactoryReadsTheCrewAgainAtEachLaunch(t *testing.T) {
	store := runOpenStore(t)
	dir := crewProfile(t, map[string]string{config.KeyTierWorkerModel: "vendor/first"})
	recorder := &recordingCompleter{}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, recorder.forModel)

	task := runWorkTask(t, store)
	factory(task)
	writeCrew(t, dir, map[string]string{config.KeyTierWorkerModel: "vendor/second-longer"})
	factory(task)

	want := []string{"vendor/first", "vendor/second-longer"}
	if len(recorder.models) != 2 || recorder.models[0] != want[0] || recorder.models[1] != want[1] {
		t.Fatalf("the two launches seated %v, want %v", recorder.models, want)
	}
}

// spendRow is one row of the store's spend table, read back raw so the
// assertions name the fields the worker is meant to fill.
type spendRow struct {
	model string
	role  string
}

// readSpendRows opens the store read-only and reads its spend table whole —
// its own connection, so the read never races the store's own handle.
func readSpendRows(t *testing.T, storePath string) []spendRow {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+storePath+"?mode=ro")
	if err != nil {
		t.Fatalf("open the store read-only: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT model, role FROM spend`)
	if err != nil {
		t.Fatalf("read the spend table: %v", err)
	}
	defer rows.Close()
	var ledger []spendRow
	for rows.Next() {
		var row spendRow
		if err := rows.Scan(&row.model, &row.role); err != nil {
			t.Fatalf("scan a spend row: %v", err)
		}
		ledger = append(ledger, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the spend table: %v", err)
	}
	return ledger
}

// TestBashWorkerChargesTheSeatModelToTheTaskSpendRow: the row the task's ledger
// gets names the model the seat was built on and the role its shape gave it, so
// the crew's per-role front is read off real work rather than a bench cell. The
// seat is the factory's, seated from the crew; the model on the row is the model
// the completer was asked for.
func TestBashWorkerChargesTheSeatModelToTheTaskSpendRow(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	dir := crewProfile(t, map[string]string{config.KeyTierWorkerModel: "vendor/seat-model"})
	recorder := &recordingCompleter{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("work-one", "the work is done")), nil
		},
	}}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, recorder.forModel)

	task := runWorkTask(t, store)
	worker := factory(task)
	if len(recorder.models) != 1 || recorder.models[0] != "vendor/seat-model" {
		t.Fatalf("the seat was built on %v, want the crew's worker row", recorder.models)
	}
	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 5), task); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	rows := readSpendRows(t, store.Path())
	if len(rows) != 1 {
		t.Fatalf("the store holds %d spend rows, want the one charge for the task", len(rows))
	}
	if rows[0].model != "vendor/seat-model" {
		t.Fatalf("the spend row names the model %q, want the seat's model", rows[0].model)
	}
	if rows[0].role != plandb.RoleWork {
		t.Fatalf("the spend row carries the role %q, want the seat the task ran in", rows[0].role)
	}
}

// A CHECK RIDES THE SEAT THE DOOR NAMED, when the door named one. [SeatFor]
// puts a check on the careful work tier, and the factory seats that tier on the
// door's check seat where the door carried one, so a run that typed
// `--check-model` checks on that model and no other seat moves.
func TestCrewFactorySeatsACheckOnTheCheckSeat(t *testing.T) {
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "one", Title: "One"},
		{ID: "review", Title: "Review", Role: plandb.RoleCheck},
	}); err != nil {
		t.Fatal(err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierLowModel:        "vendor/profile-small",
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	recorder := &recordingCompleter{}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{
		Work:  "vendor/named-work",
		Plan:  "vendor/named-plan",
		Check: "vendor/named-check",
	}, recorder.forModel)

	for _, test := range []struct {
		task, want string
	}{
		{store.RootID(), "vendor/named-plan"},
		{"one", "vendor/named-work"},
		{"review", "vendor/named-check"},
	} {
		recorder.models = nil
		factory(*store.Task(test.task))
		if len(recorder.models) != 1 || recorder.models[0] != test.want {
			t.Errorf("task %s was seated on %v, want %q", test.task, recorder.models, test.want)
		}
	}
}

// THE CHAT DOOR NAMES NO CHECK SEAT. The belt door carries only the work and
// plan seats it read off the conversation's ladder, so its check seat arrives
// empty and the factory seats the check on the profile's careful row, which is
// the crew's checker and what the manual promises: one model that works, one
// that checks, one that thinks.
func TestCrewFactorySeatsTheChatDoorsCheckOnTheCrewsChecker(t *testing.T) {
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "Review", Role: plandb.RoleCheck}}); err != nil {
		t.Fatal(err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	for _, seats := range []run.Seats{
		// The belt door's own shape: the conversation's work and plan seats,
		// and no check seat.
		{Work: "vendor/chat-work", Plan: "vendor/chat-plan"},
		// A door that named nothing at all.
		{},
	} {
		recorder := &recordingCompleter{}
		factory := run.CrewFactory(store, t.TempDir(), dir, seats, recorder.forModel)
		recorder.models = nil
		factory(*store.Task("review"))
		if len(recorder.models) != 1 || recorder.models[0] != "vendor/profile-careful" {
			t.Errorf("seats %+v seated the review on %v, want the profile's careful row",
				seats, recorder.models)
		}
	}
}
