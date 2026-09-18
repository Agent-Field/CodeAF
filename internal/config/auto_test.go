package config

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewpick"
	"github.com/Agent-Field/codeaf/internal/pool/index"
)

// THE WORD, AND WHAT IS NOT THE WORD. `auto` is a bare word a tier row holds,
// and everything that only resembles one is a model id: a suffixed word is the
// rows' own `:<level>` notation, a slashed one is a vendor's namespace, and
// `automatic` is somebody's model that happens to begin with it.
func TestIsAutoReadsTheBareWordAlone(t *testing.T) {
	for word, want := range map[string]bool{
		"auto": true, " AUTO ": true, "Auto": true, "\tauto\n": true,
		"": false, "auto:high": false, "vendor/auto": false, "automatic": false,
	} {
		if got := IsAuto(word); got != want {
			t.Errorf("IsAuto(%q) = %t, want %t", word, got, want)
		}
	}
}

// autoTestRows is a small catalog with every fact a pick reads, and one row
// the provider priced "-1" on — which is not a candidate, for the reason a
// pick that is about cost cannot count a model whose cost nobody published.
func autoTestRows() []catalog.Model {
	return []catalog.Model{
		{ID: "a/cheap", OpenWeights: true, IntelligenceIndex: 30, CodingIndex: 35, AgenticIndex: 40,
			PromptPrice: 0.0000002, CompletionPrice: 0.0000009, CacheReadPrice: 0.00000002,
			ContextLength: 200_000, InputModalities: []string{"text", "image"}, Parameters: []string{"tools"}},
		{ID: "a/mid", OpenWeights: true, IntelligenceIndex: 45, CodingIndex: 50, AgenticIndex: 48,
			PromptPrice: 0.000001, CompletionPrice: 0.000003, CacheReadPrice: 0.0000001,
			ContextLength: 200_000, InputModalities: []string{"text"}, Parameters: []string{"tools"}},
		{ID: "b/care", IntelligenceIndex: 60, CodingIndex: 55, AgenticIndex: 50,
			PromptPrice: 0.000004, CompletionPrice: 0.00002, CacheReadPrice: 0.0000004,
			ContextLength: 400_000, InputModalities: []string{"text", "image"}, Parameters: []string{"tools"}},
		{ID: "c/mind", IntelligenceIndex: 75, CodingIndex: 70, AgenticIndex: 65,
			PromptPrice: 0.00001, CompletionPrice: 0.00005, ContextLength: 200_000,
			InputModalities: []string{"text"}, Parameters: []string{"tools"}},
		{ID: "r/router", PriceUnknown: true, IntelligenceIndex: 80, CodingIndex: 80, AgenticIndex: 80,
			PromptPrice: -1, CompletionPrice: -1, ContextLength: 200_000},
	}
}

// THE PICK IS PURE AND ITS ANSWER IS THE FRONT'S. The same rows give the same
// id however often it is asked, the rows are never touched, and a row whose
// price nobody published is in no seat. The front is read the way the shipped
// tables were (crew.go owns the method), so the pick answers exactly what
// crewpick answers for these rows — asserted here against crewpick itself
// rather than against a figure copied out of one.
func TestAutoPickAnswersTheFront(t *testing.T) {
	rows := autoTestRows()
	workers := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		id, ok := AutoPick(ModelTierWorker, CrewSourceAll, CrewFrugal, rows)
		if !ok || id == "" {
			t.Fatalf("AutoPick answered ok=%t id=%q", ok, id)
		}
		workers = append(workers, id)
	}
	for i := 1; i < len(workers); i++ {
		if workers[i] != workers[0] {
			t.Fatalf("the same rows answered %q then %q — the pick is not pure", workers[0], workers[i])
		}
	}
	_ = workers
	if rows[4].ID != "r/router" || !rows[4].PriceUnknown {
		t.Fatal("AutoPick modified the rows it was handed")
	}

	frugal, _, maxCrew := crewpick.Presets(crewpick.Front(autoCandidates(rows), crewpick.DefaultShapes(), crewpick.All))
	if len(frugal.Worker) == 0 {
		t.Fatal("the test rows field no crew — the rows are broken")
	}
	if got, _ := AutoPick(ModelTierWorker, CrewSourceAll, CrewFrugal, rows); got != frugal.Worker {
		t.Fatalf("worker frugal = %q, want the front's own %q", got, frugal.Worker)
	}
	if got, _ := AutoPick(ModelTierMastermind, CrewSourceAll, CrewMax, rows); got != maxCrew.Mastermind {
		t.Fatalf("mastermind max = %q, want the front's own %q", got, maxCrew.Mastermind)
	}
}

// THE TWO TIERS THAT NEVER VARY HAVE NO ANSWER HERE, the same law their
// columns in the shipped tables keep — reflex and small work are the same
// near-free models in all three presets, and a word that computes for them
// would be a second opinion about a row that has none.
func TestAutoPickRefusesTheTiersWithoutOpinions(t *testing.T) {
	for _, tier := range []string{ModelTierReflex, ModelTierLow, "", "banana"} {
		if id, ok := AutoPick(tier, CrewSourceAll, CrewFrugal, autoTestRows()); ok || id != "" {
			t.Errorf("AutoPick(%q) answered %q, want no answer", tier, id)
		}
	}
}

// THE THREE WORDS ARE THE ONLY PRESETS, `open` IS THE ONLY OTHER FAMILY, AND
// NOTHING IS AN ERROR: no answer is ok false and an empty id, on no rows,
// on an empty front, on a word that is not a preset, and on a tier the
// catalog cannot field.
func TestAutoPickAnswersNothingWhenNothingCanBePicked(t *testing.T) {
	if id, ok := AutoPick(ModelTierWorker, CrewSourceAll, "banana", autoTestRows()); ok || id != "" {
		t.Errorf("an unknown preset answered %q", id)
	}
	if id, ok := AutoPick(ModelTierWorker, CrewSourceAll, CrewFrugal, nil); ok || id != "" {
		t.Errorf("no rows answered %q", id)
	}
	if id, ok := AutoPick(ModelTierHigh, CrewSourceAll, CrewFrugal, []catalog.Model{{ID: "a/cheap", PromptPrice: 1}}); ok || id != "" {
		t.Errorf("rows that field no crew answered %q", id)
	}
	// A family word this build does not know reads as the default family,
	// the way [CrewSourceAt] reads one — and that family answers here.
	if _, ok := AutoPick(ModelTierWorker, "misplaced", CrewFrugal, autoTestRows()); !ok {
		t.Error("an unknown family word read as no family at all")
	}
}

// ── the ladder ──────────────────────────────────────────────────────────────

// A ROW THAT SAYS AUTO RESOLVES THROUGH THE ONE SEAM, ON BOTH LADDERS, to the
// computed id with the rung that says so — and with the catalog absent, to
// the family's table row for the preset the other four rows name, on the
// table rung. It never resolves to `auto` and never to empty.
func TestAnAutoRowResolvesOnBothLadders(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	restore := AutoModels
	AutoModels = func() []catalog.Model { return autoTestRows() }
	defer func() { AutoModels = restore }()

	rows := writeProfileRows(t, map[string]string{
		KeyTierWorkerModel:     " auto ",
		KeyTierMastermindModel: "AUTO",
	})
	for _, seat := range []Seat{
		TierSeatAt(rows, ModelTierWorker),
		ResolveSeats(rows, "", "").Work,
	} {
		if seat.Source != SeatComputed {
			t.Errorf("conversation ladder: the seat reads %s, want computed", seat.Rung())
		}
		if strings.TrimSpace(seat.Model) == "" || IsAuto(seat.Model) {
			t.Errorf("conversation ladder: the seat reads %q, want a computed id", seat.Model)
		}
	}
	plan := ResolveSeats(rows, "", "").Plan
	if plan.Source != SeatComputed {
		t.Errorf("headless ladder: the plan seat reads %s, want computed", plan.Rung())
	}

	// The catalog reaches nothing: the family's table row answers, on the
	// table rung, at the preset the stored rows make. These rows pin nothing
	// else, so they are the default crew — and the worker column of that
	// preset in the DEFAULT family is the answer the table owes.
	AutoModels = nil
	table, _ := CrewModelsForSource(DefaultCrewSource, DefaultCrew)
	want := table[ModelTierWorker]
	for name, seat := range map[string]Seat{
		"conversation": TierSeatAt(rows, ModelTierWorker),
		"headless":     ResolveSeats(rows, "", "").Work,
	} {
		if seat.Model != want || seat.Source != SeatTable {
			t.Errorf("%s ladder with no catalog: %q (%s), want %q (table)", name, seat.Model, seat.Rung(), want)
		}
	}
}

// A REACHED-THROUGH-THE-LINEAGE ROW SAYS AUTO THE SAME WAY. The worker row
// landed after profiles held only the small-work row, and a profile of that
// vintage whose small-work row says auto hands its work seat the same answer
// the conversation hands it — computed when the catalog can, and never the
// bare word.
func TestAnInheritedAutoRowResolvesTheSameWay(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	restore := AutoModels
	AutoModels = func() []catalog.Model { return autoTestRows() }
	defer func() { AutoModels = restore }()

	dir := writeProfileRows(t, map[string]string{KeyTierLowModel: AutoValue})
	for name, seat := range map[string]Seat{
		"conversation": TierSeatAt(dir, ModelTierWorker),
		"headless":     ResolveSeats(dir, "", "").Work,
	} {
		if seat.Model == AutoValue || seat.Model == "" {
			t.Errorf("%s ladder: the seat reads %q, want what the row was reached through", name, seat.Model)
		}
	}
}

// THE FLAG AND THE VARIABLE STILL OUTRANK A ROW THAT SAYS AUTO, and a flag
// whose text is the word is handed on whole — a person who named a model
// called auto got a model called auto, exactly as they did before this rung
// existed.
func TestTheInvocationRungsStillOutrankAnAutoRow(t *testing.T) {
	t.Setenv(ModelEnv, "vendor/from-the-environment")
	t.Setenv(PlanModelEnv, "vendor/plans-from-the-environment")
	dir := writeProfileRows(t, map[string]string{KeyTierWorkerModel: AutoValue})
	seats := ResolveSeats(dir, "", "")
	if seats.Work.Model != "vendor/from-the-environment" || seats.Work.Source != SeatEnv {
		t.Errorf("the environment rung read %q (%s)", seats.Work.Model, seats.Work.Rung())
	}
	if seats.Plan.Model != "vendor/plans-from-the-environment" || seats.Plan.Source != SeatEnv {
		t.Errorf("the plan seat read %q (%s)", seats.Plan.Model, seats.Plan.Rung())
	}
	seats = ResolveSeats(dir, AutoValue, "")
	if seats.Work.Model != AutoValue || seats.Work.Source != SeatFlag {
		t.Errorf("a flag that says auto read %q (%s), want the word handed on whole", seats.Work.Model, seats.Work.Rung())
	}
}

// THE PRESET AN AUTO SEAT RUNS AT IS READ FROM THE STORED ROWS, never through
// seat resolution — the seam cannot ask the ladder that is asking it — and a
// row that says auto matches whichever preset the other rows name.
//
// MAX DIFFERS FROM BALANCED ONLY IN THE WORKER SEAT NOW, so an auto row on the
// worker can no longer tell the two apart — balanced, the default preset, wins
// that tie — and the identifying shape is the other way round: the worker
// pinned to max's own id, which only max names, with the auto row on a seat
// above it.
func TestThePresetAnAutoSeatRunsAtIsReadFromTheStoredRows(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	models, _ := CrewModelsForSource(DefaultCrewSource, CrewMax)
	rows := map[string]string{KeyTierMastermindModel: AutoValue}
	for _, tier := range ModelTiers {
		if tier != ModelTierMastermind {
			rows[tierKeyFor(tier)] = models[tier]
		}
	}
	dir := writeProfileRows(t, rows)
	restore := AutoModels
	AutoModels = func() []catalog.Model { return autoTestRows() }
	seat := TierSeatAt(dir, ModelTierMastermind)
	AutoModels = restore
	if seat.Crew != CrewMax {
		t.Errorf("max's rows and an auto row read as %q", seat.Crew)
	}
	if seat.Rung() != "crew "+CrewMax+", computed from the catalog" {
		t.Errorf("the rung reads %q, want crew max, computed from the catalog", seat.Rung())
	}
	// A profile with no auto row anywhere reads as it always read — the
	// default rung, no crew word — which is the unchanged-behaviour law: the
	// seam fires only on a row that says the word.
	seat = TierSeatAt(t.TempDir(), ModelTierWorker)
	if seat.Rung() != "default" || seat.Crew != "" {
		t.Errorf("an untouched profile reads %q (%s), want the default rung as before", seat.Rung(), seat.Crew)
	}
}

// THE DEFAULT PRESET WINS EVERY TIE. An auto row matches whichever preset is
// being compared, so a profile whose OTHER rows match two presets at once has
// no single answer from the rows alone — and the answer is the default preset,
// balanced, the budget an undecided profile already runs at.
//
// The tie is built here rather than assumed: the worker holds auto and the
// other four rows are taken from max, and the test first states that those
// four are balanced's rows too, which is what makes this a tie at all. Should
// the tables move so the two presets differ somewhere above the worker, that
// first check fails and says so, rather than the test quietly pinning nothing.
func TestTheDefaultPresetWinsWhenTheStoredRowsMatchTwoPresets(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	balanced, ok := CrewModelsForSource(DefaultCrewSource, CrewBalanced)
	if !ok {
		t.Fatalf("there is no %s preset in the %s family", CrewBalanced, DefaultCrewSource)
	}
	max, ok := CrewModelsForSource(DefaultCrewSource, CrewMax)
	if !ok {
		t.Fatalf("there is no %s preset in the %s family", CrewMax, DefaultCrewSource)
	}
	rows := map[string]string{KeyTierWorkerModel: AutoValue}
	for _, tier := range ModelTiers {
		if tier == ModelTierWorker {
			continue
		}
		if balanced[tier] != max[tier] {
			t.Fatalf("the %s seat differs between balanced (%s) and max (%s), so an auto worker is no longer a tie",
				tier, balanced[tier], max[tier])
		}
		rows[tierKeyFor(tier)] = max[tier]
	}
	if balanced[ModelTierWorker] == max[ModelTierWorker] {
		t.Fatalf("balanced and max name the same worker, so the two presets are not two")
	}

	dir := writeProfileRows(t, rows)
	restore := AutoModels
	AutoModels = func() []catalog.Model { return autoTestRows() }
	seat := TierSeatAt(dir, ModelTierWorker)
	AutoModels = restore

	if seat.Crew != CrewBalanced {
		t.Errorf("rows matching both presets read as %q, want %s, the default preset", seat.Crew, CrewBalanced)
	}
	if seat.Rung() != "crew "+CrewBalanced+", computed from the catalog" {
		t.Errorf("the rung reads %q, want crew %s, computed from the catalog", seat.Rung(), CrewBalanced)
	}
	// And the same tie on the table rung, with nothing to compute from: the
	// default preset decides the fallback id too, not just the budget word.
	dir = writeProfileRows(t, rows)
	seat = TierSeatAt(dir, ModelTierWorker)
	if seat.Crew != CrewBalanced || seat.Source != SeatTable {
		t.Errorf("with no catalog the tie reads %q (%s), want balanced on the table rung", seat.Crew, seat.Rung())
	}
	if seat.Model != balanced[ModelTierWorker] {
		t.Errorf("the tie's table id is %q, want balanced's own worker %q", seat.Model, balanced[ModelTierWorker])
	}
}

// A SETTINGS ROW SHOWS THE MODEL RUNNING, because it reads through the
// resolver — and a profile that says auto nowhere is untouched by any of
// this: the same rows, the same ids, the same rungs.
func TestAutoNowhereChangesNothing(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	restore := AutoModels
	AutoModels = func() []catalog.Model { return autoTestRows() }
	defer func() { AutoModels = restore }()

	dir := t.TempDir()
	if err := ApplyCrew(dir, CrewBalanced); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, tier := range ModelTiers {
		before[tier] = TierModelAt(dir, tier)
	}
	raw, err := os.ReadFile(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 {
		t.Fatal("the write stored nothing")
	}
	for tier, want := range before {
		if got := TierModelAt(dir, tier); got != want {
			t.Errorf("%s read %q then %q — a read rewrote a row", tier, want, got)
		}
	}
	if got := CrewAt(dir); got != CrewBalanced {
		t.Errorf("the crew word reads %q, want balanced", got)
	}
}

// THE SEAM'S TABLE RUNG IS THE PRESET'S OWN ID, not merely a non-empty
// answer: with the catalog gone, an auto row under rows that pin max reads
// to that preset's id for the tier, not to the default preset's. The worker
// is the pinned seat — it is the one cell where max differs from balanced —
// and the auto row rides the mastermind.
func TestTheTableRungAnswersThePresetTheRowsName(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	models, _ := CrewModelsForSource(DefaultCrewSource, CrewMax)
	rows := map[string]string{KeyTierMastermindModel: AutoValue}
	for _, tier := range ModelTiers {
		if tier != ModelTierMastermind {
			rows[tierKeyFor(tier)] = models[tier]
		}
	}
	dir := writeProfileRows(t, rows)
	seat := TierSeatAt(dir, ModelTierMastermind)
	if seat.Model != models[ModelTierMastermind] || seat.Source != SeatTable {
		t.Errorf("with no catalog the seat reads %q (%s), want max's own mastermind id on the table rung",
			seat.Model, seat.Rung())
	}
}

// mustIndex parses a measurement document or fails the test.
func mustIndex(t *testing.T, doc string) *index.Index {
	t.Helper()
	x, err := index.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse index: %v", err)
	}
	return x
}

// priorDocument is the smallest index document that rates one catalog row on
// the worker seat: a bernoulli acceptable metric and a cell well above the
// document's min_installs.
const priorDocument = `{
	"schema": 1,
	"generated": "2026-09-17",
	"min_installs": 5,
	"metrics": {"acceptable": {"kind": "bernoulli", "dims": ["role", "model"]}},
	"cells": [
		{"metric": "acceptable", "role": "worker", "model": "a/cheap", "mean": 95, "sd": 5, "n": 1000}
	]
}`

// THE INDEX REACHES THE PICK THROUGH THE PRIOR. An index whose acceptable metric
// metric rates one catalog row well above its published worker quality moves
// AutoPick's worker answer to that row, and with no index the pick answers
// exactly what the front answers, as before.
func TestAutoPickReadsAMeasuredQualityPrior(t *testing.T) {
	restore := AutoIndex
	defer func() { AutoIndex = restore }()

	rows := autoTestRows()

	AutoIndex = nil
	frugal, _, _ := crewpick.Presets(crewpick.Front(autoCandidates(rows), crewpick.DefaultShapes(), crewpick.All))
	before, ok := AutoPick(ModelTierWorker, CrewSourceAll, CrewFrugal, rows)
	if !ok || before != frugal.Worker {
		t.Fatalf("with no index the worker pick reads %q, want the front's own %q", before, frugal.Worker)
	}

	AutoIndex = func() *index.Index { return mustIndex(t, priorDocument) }
	after, ok := AutoPick(ModelTierWorker, CrewSourceAll, CrewFrugal, rows)
	if !ok {
		t.Fatal("the pick answers nothing with the index in hand")
	}
	if after == before {
		t.Fatalf("the rated row left the worker pick at %q; the index never reached the quality", after)
	}
	if after != "a/cheap" {
		t.Fatalf("the rated row did not take the worker seat: got %q", after)
	}
}

// THE PRIOR IS THE ARGUMENT, AND NOTHING HIDES IT. AutoPickWith carries
// whatever prior it is given: nil leaves the worker on the front's own
// answer, and the same measured cell the AutoPick test reads moves it — which
// is exactly the difference between the `catalog` and `learn` pick words
// (crew.go's [CrewPickAt]).
func TestAutoPickWithCarriesThePriorItIsGiven(t *testing.T) {
	rows := autoTestRows()
	front, _, _ := crewpick.Presets(crewpick.Front(autoCandidates(rows), crewpick.DefaultShapes(), crewpick.All))
	bare, ok := AutoPickWith(ModelTierWorker, CrewSourceAll, CrewFrugal, rows, nil)
	if !ok || bare != front.Worker {
		t.Fatalf("with no prior the worker pick reads %q, want the front's own %q", bare, front.Worker)
	}
	measured, ok := AutoPickWith(ModelTierWorker, CrewSourceAll, CrewFrugal, rows, autoPrior(mustIndex(t, priorDocument)))
	if !ok {
		t.Fatal("the pick answers nothing with the prior in hand")
	}
	if measured == bare {
		t.Fatalf("the measured cell left the worker pick at %q; the prior never reached the front", measured)
	}
	if measured != "a/cheap" {
		t.Fatalf("the rated row did not take the worker seat: got %q", measured)
	}
}

// ── the two seams ───────────────────────────────────────────────────────────

// A BARE AUTO ROW AND THE PICK WORD'S OWN SEAT ANSWER ONE CATALOG THE SAME
// WAY. Under `picked from = catalog` both read the catalog's published
// figures alone — the Model Pool's measurements enter neither seat — and
// under `learn` both carry them, on the learned rung. The fixture still
// splits the two answers, asserted rather than assumed, so rows or a prior
// that stop moving the worker pick fail here saying so instead of pinning
// nothing.
func TestABareAutoRowAnswersTheCatalogThePickWordNames(t *testing.T) {
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")
	restoreModels, restoreIndex := AutoModels, AutoIndex
	defer func() { AutoModels, AutoIndex = restoreModels, restoreIndex }()
	AutoModels = func() []catalog.Model { return autoTestRows() }
	AutoIndex = func() *index.Index { return mustIndex(t, priorDocument) }

	published, ok := AutoPickWith(ModelTierWorker, DefaultCrewSource, DefaultCrew, autoTestRows(), nil)
	if !ok || published == "a/cheap" {
		t.Fatalf("the fixture does not split the two answers: the published pick reads %q (ok %t), so the assertions below cannot tell them apart", published, ok)
	}

	dir := writeProfileRows(t, map[string]string{
		KeyTierWorkerModel: AutoValue,
		KeyCrewPick:        CrewPickCatalog,
	})
	model, source, preset := autoRow(dir, DefaultCrewSource, ModelTierWorker)
	want, _ := pickedModel(ModelTierWorker, DefaultCrewSource, preset, CrewPickCatalog)
	if model != want {
		t.Fatalf("under picked from = catalog the bare auto row answers %q and the pick word's own seat answers %q — the two seams disagreed about what the catalog means", model, want)
	}
	if source != SeatComputed {
		t.Errorf("under picked from = catalog the bare row's rung reads %q, want computed", source)
	}

	dir = writeProfileRows(t, map[string]string{
		KeyTierWorkerModel: AutoValue,
		KeyCrewPick:        CrewPickLearn,
	})
	model, source, preset = autoRow(dir, DefaultCrewSource, ModelTierWorker)
	want, _ = pickedModel(ModelTierWorker, DefaultCrewSource, preset, CrewPickLearn)
	if model != want || model != "a/cheap" {
		t.Errorf("under picked from = learn the bare auto row answers %q and the pick word's own seat answers %q, want the measured pick a/cheap", model, want)
	}
	if source != SeatLearned {
		t.Errorf("under picked from = learn the bare row's rung reads %q, want learned", source)
	}
}

// ── the install's own sheet ─────────────────────────────────────────────────

// ownCells is the install's own evidence for the prior tests: one cell the
// index document also carries, one it does not, both on the worker seat.
func ownCells() []crewpick.Cell {
	return []crewpick.Cell{
		{Role: "worker", Model: "a/cheap", Mean: 90, N: 3},
		{Role: "worker", Model: "b/only", Mean: 40, N: 1},
	}
}

// With no index in hand the own sheet's cells are the whole prior — and a
// one-observation cell of this install's own is worth having, where an
// index's min_installs would have dropped it.
func TestAutoPriorReadsTheOwnCellsAloneWithNoIndex(t *testing.T) {
	restore := AutoOwnCells
	defer func() { AutoOwnCells = restore }()
	AutoOwnCells = ownCells

	prior := autoPrior(nil)
	if len(prior[crewpick.Worker]) != 2 {
		t.Fatalf("the own cells made a prior of %d worker ratings, want two: %v", len(prior[crewpick.Worker]), prior)
	}
	if r := prior[crewpick.Worker]["b/only"]; r.Mean != 40 || r.N != 1 {
		t.Fatalf("a one-observation own cell did not survive: %v", r)
	}
	if r := prior[crewpick.Worker]["a/cheap"]; r.Mean != 90 || r.N != 3 {
		t.Fatalf("the shared cell came back %v, want mean 90 over 3", r)
	}
}

// A model both the index and the own sheet rate folds by observation count:
// the counts add, the mean is the mean of the means weighted by them, and the
// cells either one holds alone are carried beside it.
func TestAutoPriorFoldsTheOwnCellsIntoTheIndexByObservationCount(t *testing.T) {
	restoreIndex, restoreOwn := AutoIndex, AutoOwnCells
	defer func() { AutoIndex, AutoOwnCells = restoreIndex, restoreOwn }()
	AutoIndex = func() *index.Index { return mustIndex(t, priorDocument) }
	AutoOwnCells = ownCells

	prior := autoPrior(AutoIndex())
	shared := prior[crewpick.Worker]["a/cheap"]
	wantMean := (1000*95.0 + 3*90.0) / 1003
	if shared.N != 1003 {
		t.Fatalf("the shared cell folded to N %d, want 1003", shared.N)
	}
	if math.Abs(shared.Mean-wantMean) > 1e-9 {
		t.Fatalf("the shared cell folded to mean %v, want %v", shared.Mean, wantMean)
	}
	if r := prior[crewpick.Worker]["b/only"]; r.Mean != 40 || r.N != 1 {
		t.Fatalf("the own-only cell did not survive the fold: %v", r)
	}
}

// A nil seam changes nothing: the index's prior is exactly what it was, and
// with no index there is no prior at all.
func TestANilOwnCellsSeamLeavesThePriorAlone(t *testing.T) {
	restoreIndex, restoreOwn := AutoIndex, AutoOwnCells
	defer func() { AutoIndex, AutoOwnCells = restoreIndex, restoreOwn }()
	AutoIndex = func() *index.Index { return mustIndex(t, priorDocument) }
	AutoOwnCells = nil

	prior := autoPrior(AutoIndex())
	if len(prior[crewpick.Worker]) != 1 || prior[crewpick.Worker]["a/cheap"] != (crewpick.Rating{Mean: 95, N: 1000}) {
		t.Fatalf("a nil seam changed the index prior: %v", prior)
	}
	if prior := autoPrior(nil); prior != nil {
		t.Fatalf("a nil seam and no index made a prior: %v", prior)
	}
}

// The own sheet's scores are on the 0-100 scale a judge answers on whatever
// the index's metric says, so they are read even beside an index whose
// acceptable is not bernoulli — while the index's own cells are not.
func TestAutoPriorReadsTheOwnCellsBesideANonGaussianIndex(t *testing.T) {
	restoreIndex, restoreOwn := AutoIndex, AutoOwnCells
	defer func() { AutoIndex, AutoOwnCells = restoreIndex, restoreOwn }()
	AutoIndex = func() *index.Index {
		return mustIndex(t, `{
		"schema": 1,
		"generated": "2026-09-17",
		"min_installs": 1,
		"metrics": {"acceptable": {"kind": "tally", "dims": ["role", "model"]}},
		"cells": [{"metric": "acceptable", "role": "worker", "model": "a/cheap", "mean": 5, "n": 900}]
	}`)
	}
	AutoOwnCells = ownCells

	prior := autoPrior(AutoIndex())
	if r, ok := prior[crewpick.Worker]["a/cheap"]; !ok || r.Mean != 90 || r.N != 3 {
		t.Fatalf("the own cell was not read beside a non-bernoulli index: %v ok %v", prior[crewpick.Worker]["a/cheap"], ok)
	}
	if _, ok := prior[crewpick.Worker]["b/only"]; !ok {
		t.Fatalf("the second own cell did not survive: %v", prior)
	}
}
