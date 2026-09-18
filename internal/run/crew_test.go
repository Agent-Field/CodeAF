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

// TestCrewFactorySeatsEachRoleInItsTier is the shape rule the factory exists
// for: a task with children is a coordinator and takes the mastermind row, a
// leaf takes the worker row, and check and probe are read off the role they were
// declared with — the check rides the plan tier (it is planning work), and the
// probe takes the small row.
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
		{"review", "vendor/thinking"},
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
// The check rides the plan tier, so it takes the door's plan seat; the probe row
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
		// A check is planning work and rides the door's plan seat; the probe row
		// is the profile's own.
		{"review", "vendor/named-plan"},
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
// seating the task on nothing. The check rides the plan tier, whose row is
// cleared here.
func TestCrewFactoryFallsBackToTheWorkerRowForAnEmptyTier(t *testing.T) {
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "Review", Role: plandb.RoleCheck}}); err != nil {
		t.Fatalf("add the review: %v", err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierMastermindModel: "",
		config.KeyTierWorkerModel:     "vendor/worker",
	})
	recorder := &recordingCompleter{}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, recorder.forModel)

	factory(*store.Task("review"))

	if len(recorder.models) != 1 || recorder.models[0] != "vendor/worker" {
		t.Fatalf("the review was seated on %v, want the worker row's model", recorder.models)
	}
}

// TestCrewFactoryRefusesATaskTheCrewCannotSeat: a tier row and a worker row both
// empty is a task no model can run, and the refusal names the tier so a person
// knows which row has to be filled.
func TestCrewFactoryRefusesATaskTheCrewCannotSeat(t *testing.T) {
	store := runOpenStore(t)
	dir := crewProfile(t, map[string]string{config.KeyTierWorkerModel: ""})
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, (&recordingCompleter{}).forModel)

	worker := factory(*store.Task(store.RootID()))
	_, err := worker.Run(runContext(t), *store.Task(store.RootID()))

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

	factory(*store.Task(store.RootID()))
	writeCrew(t, dir, map[string]string{config.KeyTierWorkerModel: "vendor/second-longer"})
	factory(*store.Task(store.RootID()))

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
			return toolReply(finishCommand("root", "the work is done")), nil
		},
	}}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{}, recorder.forModel)

	worker := factory(*store.Task(store.RootID()))
	if len(recorder.models) != 1 || recorder.models[0] != "vendor/seat-model" {
		t.Fatalf("the seat was built on %v, want the crew's worker row", recorder.models)
	}
	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 5), *store.Task(store.RootID())); err != nil {
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
