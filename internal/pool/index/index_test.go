package index

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// example is the shape the whole reader holds in mind: one metric with dims,
// one alias pair, one cell, one wanted row.
const example = `{
	"schema": 1,
	"generated": "2026-09-17",
	"min_installs": 5,
	"judges": ["z-ai/glm-5.3"],
	"rubrics": {"planner": 3},
	"aliases": {"z-ai/glm-5.3": ["glm-5.3", "zai-org/GLM-5.3"]},
	"metrics": {"role_rating": {"kind": "gaussian", "unit": "elo", "dims": ["role", "model"]}},
	"cells": [
		{"metric": "role_rating", "role": "planner", "model": "z-ai/glm-5.3", "mean": 1312, "sd": 18, "n": 410}
	],
	"wanted": [{"role": "planner", "model": "qwen/qwen3.8-max-0902", "weight": 0.4}]
}`

func mustParse(t *testing.T, data string) *Index {
	t.Helper()
	x, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return x
}

// ── THE DOCUMENT, READ BACK ─────────────────────────────────────────────────

func TestTheDocumentReadsBack(t *testing.T) {
	x := mustParse(t, example)
	if x.Schema() != 1 {
		t.Errorf("schema is %d, want 1", x.Schema())
	}
	if got, want := x.Generated(), time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("generated is %v, want %v", got, want)
	}
	if x.MinInstalls() != 5 {
		t.Errorf("min_installs is %d, want 5", x.MinInstalls())
	}
	if judges := x.Judges(); len(judges) != 1 || judges[0] != "z-ai/glm-5.3" {
		t.Errorf("judges are %v", judges)
	}
	if rubrics := x.Rubrics(); rubrics["planner"] != 3 || len(rubrics) != 1 {
		t.Errorf("rubrics are %v", rubrics)
	}
	if metrics := x.Metrics(); len(metrics) != 1 || metrics[0] != "role_rating" {
		t.Errorf("metrics are %v", metrics)
	}
	if kind, ok := x.Kind("role_rating"); !ok || kind != "gaussian" {
		t.Errorf("kind is %q, %v", kind, ok)
	}
	c, ok := x.Cell("role_rating", "planner", "z-ai/glm-5.3", nil)
	if !ok {
		t.Fatal("the example cell was not found")
	}
	if c.Mean != 1312 || c.SD != 18 || c.N != 410 {
		t.Errorf("cell reads %v", c)
	}
	if len(x.Wanted()) != 1 || x.Wanted()[0].Weight != 0.4 {
		t.Errorf("wanted reads %v", x.Wanted())
	}
}

func TestReadTakesAReader(t *testing.T) {
	x, err := Read(bytes.NewBufferString(example))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(x.Cells("role_rating")) != 1 {
		t.Errorf("read through a reader found %d cells", len(x.Cells("role_rating")))
	}
}

// ── FORWARD COMPATIBILITY ───────────────────────────────────────────────────

// EVERY UNKNOWN THING IS IGNORED, because a reader that refused them would
// make every future field a flag day.
func TestUnknownThingsAreIgnoredNotErrors(t *testing.T) {
	x := mustParse(t, `{
		"schema": 1,
		"tomorrow_field": {"anything": true},
		"metrics": {"role_rating": {"kind": "not-a-kind-we-know", "dims": []}},
		"cells": [
			{"metric": "role_rating", "role": "planner", "model": "m", "mean": 1, "sd": 1, "n": 9, "unknown_cell_field": "x"}
		]
	}`)
	if kind, ok := x.Kind("role_rating"); !ok || kind != "not-a-kind-we-know" {
		t.Errorf("an unknown kind was refused or rewritten: %q %v", kind, ok)
	}
	if _, ok := x.Cell("role_rating", "planner", "m", nil); !ok {
		t.Error("a cell carrying an unknown field was dropped")
	}
}

func TestSchemaRefusesOnlyWhatIsTooNew(t *testing.T) {
	for _, row := range []struct {
		name, schema string
		want         int
		accepted     bool
	}{
		{"a missing schema", "", 0, true}, // missing reads as 0 and is accepted
		{"schema 0", `"schema": 0,`, 0, true},
		{"schema 1", `"schema": 1,`, 1, true},
		{"a schema below 1", `"schema": -1,`, -1, true},
		{"schema 2", `"schema": 2,`, 0, false},
		{"schema 99", `"schema": 99,`, 0, false},
	} {
		x, err := Parse([]byte("{" + row.schema + `"metrics": {}}`))
		if (err == nil) != row.accepted {
			t.Errorf("%s: err=%v, want accepted=%v", row.name, err, row.accepted)
			continue
		}
		if row.accepted && x.Schema() != row.want {
			t.Errorf("%s reads back as %d, want %d", row.name, x.Schema(), row.want)
		}
	}

	_, err := Parse([]byte(`{"schema": 2}`))
	if !errors.Is(err, ErrSchema) {
		t.Errorf("a too-new schema answered %v, want it wrapped in ErrSchema", err)
	}
}

func TestMalformedAndEmptyDocumentsAreErrors(t *testing.T) {
	for _, row := range []struct {
		name string
		data string
	}{
		{"malformed json", `{"schema": 1,`},
		{"an empty document", ``},
		{"a whitespace document", "  \n\t "},
		{"a document that is not an object", `[1, 2, 3]`},
	} {
		if _, err := Parse([]byte(row.data)); err == nil {
			t.Errorf("%s was accepted", row.name)
		}
	}
}

func TestAGeneratedDateThatWillNotReadIsTheZeroTime(t *testing.T) {
	for _, row := range []struct {
		name, generated string
		wantZero        bool
	}{
		{"missing", "", true},
		{"unreadable", "17/09/2026", true},
		{"readable", "2026-09-17", false},
	} {
		data := `{"metrics": {}}`
		if row.generated != "" {
			data = `{"generated": "` + row.generated + `", "metrics": {}}`
		}
		got := mustParse(t, data).Generated()
		if row.wantZero != got.IsZero() {
			t.Errorf("%s generated date gave %v, want zero=%v", row.name, got, row.wantZero)
		}
	}
}

// ── METRICS AND DIMS ────────────────────────────────────────────────────────

func TestMetricsAreSortedAndCellsOfUndeclaredMetricsAreSkipped(t *testing.T) {
	x := mustParse(t, `{
		"metrics": {"zebra": {"kind": "a"}, "alpha": {"kind": "b"}, "mid": {"kind": "c"}},
		"cells": [
			{"metric": "zebra", "role": "r", "model": "m", "mean": 1, "sd": 1, "n": 9},
			{"metric": "undeclared", "role": "r", "model": "m", "mean": 1, "sd": 1, "n": 9}
		]
	}`)
	if got, want := strings.Join(x.Metrics(), ","), "alpha,mid,zebra"; got != want {
		t.Errorf("metrics are %q, want %q", got, want)
	}
	if len(x.Cells("zebra")) != 1 {
		t.Errorf("a declared metric's cell count is %d, want 1", len(x.Cells("zebra")))
	}
	if len(x.Cells("undeclared")) != 0 {
		t.Error("a cell whose metric is not declared was kept")
	}
	if _, ok := x.Kind("undeclared"); ok {
		t.Error("an undeclared metric answered a kind")
	}
}

func TestDeclaredDimsBecomeCellDims(t *testing.T) {
	x := mustParse(t, `{
		"metrics": {"m": {"kind": "a", "dims": ["role", "model", "region", "variant"]}},
		"cells": [
			{"metric": "m", "role": "r", "model": "mo", "mean": 1, "sd": 1, "n": 1,
			 "region": "EU", "variant": "lite", "mean_x": "not a dim", "n_other": 7}
		]
	}`)
	c, ok := x.Cell("m", "r", "mo", map[string]string{"region": "eu", "variant": "LITE"})
	if !ok {
		t.Fatal("the dims cell was not found")
	}
	if len(c.Dims) != 2 || c.Dims["region"] != "EU" || c.Dims["variant"] != "lite" {
		t.Errorf("dims read %v, want region=EU and variant=lite as spelled", c.Dims)
	}
	// role and model are declared dims but never cell dims — they are the
	// address itself.
	for _, banned := range []string{"role", "model"} {
		if _, is := c.Dims[banned]; is {
			t.Errorf("%s became a cell dim", banned)
		}
	}
}

func TestADimWhoseValueIsNotAStringIsIgnored(t *testing.T) {
	x := mustParse(t, `{
		"metrics": {"m": {"kind": "a", "dims": ["region"]}},
		"cells": [
			{"metric": "m", "role": "r", "model": "mo", "mean": 1, "sd": 1, "n": 1, "region": 4}
		]
	}`)
	c, ok := x.Cell("m", "r", "mo", nil)
	if !ok {
		t.Fatal("the cell was dropped over a non-string dim")
	}
	if len(c.Dims) != 0 {
		t.Errorf("a non-string dim became %v", c.Dims)
	}
}

// ── NORMALISATION AND ALIASES ───────────────────────────────────────────────

// THE SAME ID CAN ARRIVE SPELLED FOUR WAYS, and a lookup written with one of
// them has to find a cell written with another: that is the only way an
// alias table earns its place in the document.
func TestAliasesMeetInBothDirections(t *testing.T) {
	x := mustParse(t, `{
		"aliases": {"Z-AI/GLM-5.3": ["glm-5.3"]},
		"metrics": {"m": {"kind": "a"}},
		"cells": [{"metric": "m", "role": "planner", "model": "glm-5.3", "mean": 2, "sd": 1, "n": 5}],
		"wanted": [{"role": " Planner ", "model": " glm-5.3 ", "weight": 1}]
	}`)
	if got, want := x.Canonical("~z-ai/glm-5.3"), "Z-AI/GLM-5.3"; got != want {
		t.Errorf("canonical of the folded id is %q, want %q", got, want)
	}
	if got, want := x.Canonical("~glm-5.3"), "Z-AI/GLM-5.3"; got != want {
		t.Errorf("canonical of the ~-stripped alias is %q, want %q", got, want)
	}
	if got, want := x.Canonical("never/named"), "never/named"; got != want {
		t.Errorf("canonical of an unnamed id is %q, want the normalised input %q", got, want)
	}
	// The cell was written under an alias; a lookup under a different alias
	// finds it because cells are stored under their canonical model.
	c, ok := x.Cell(" M ", "PLANNER", "~Z-AI/GLM-5.3", nil)
	if !ok {
		t.Fatal("a lookup under one alias did not find a cell written under another")
	}
	if c.Model != "Z-AI/GLM-5.3" {
		t.Errorf("the cell's model is %q, want the canonical id", c.Model)
	}
	if w := x.Wanted(); len(w) != 1 || w[0].Role != "planner" || w[0].Model != "Z-AI/GLM-5.3" {
		t.Errorf("wanted reads %v, want the role folded and the model canonical", w)
	}
}

func TestMetricNamesRolesAndDimsMatchFolded(t *testing.T) {
	x := mustParse(t, `{
		"metrics": {"Role_Rating": {"kind": "a", "dims": [" Region "]}},
		"cells": [{"metric": "role_rating", "role": " Planner ", "model": "m", "mean": 1, "sd": 1, "n": 1, "region": "eu"}]
	}`)
	if _, ok := x.Kind(" ROLE_RATING "); !ok {
		t.Error("a kind lookup did not fold the metric name")
	}
	if _, ok := x.Cell("ROLE_rating", "planner", "M", map[string]string{"REGION": "EU"}); !ok {
		t.Error("an exact address did not fold metric, role and dims")
	}
}

// ── THE EXACT ADDRESS ───────────────────────────────────────────────────────

// NIL DIMS AND EMPTY DIMS ARE THE SAME ADDRESS, and a lookup that carries
// some dims never finds a cell that carries none, nor the other way round.
func TestTheAddressIsExact(t *testing.T) {
	x := mustParse(t, `{
		"metrics": {"m": {"kind": "a", "dims": ["region", "tier"]}},
		"cells": [
			{"metric": "m", "role": "r", "model": "a", "mean": 1, "sd": 1, "n": 1},
			{"metric": "m", "role": "r", "model": "b", "mean": 2, "sd": 1, "n": 1, "region": "eu"},
			{"metric": "m", "role": "r", "model": "c", "mean": 3, "sd": 1, "n": 1, "region": "eu", "tier": "lite"}
		]
	}`)
	if c, ok := x.Cell("m", "r", "a", map[string]string{}); !ok || c.Mean != 1 {
		t.Error("an empty dims map did not find the cell that carries none")
	}
	if _, ok := x.Cell("m", "r", "b", nil); ok {
		t.Error("a no-dims lookup found a cell that carries dims")
	}
	if c, ok := x.Cell("m", "r", "b", map[string]string{"region": "eu"}); !ok || c.Mean != 2 {
		t.Error("the one-dim cell was not found by its one dim")
	}
	if _, ok := x.Cell("m", "r", "c", map[string]string{"region": "eu"}); ok {
		t.Error("a partial dims lookup found a cell with more dims")
	}
	if _, ok := x.Cell("m", "r", "b", map[string]string{"region": "us"}); ok {
		t.Error("a dims lookup with the wrong value found the cell")
	}
}

func TestMinInstallsHoldsBackThinCells(t *testing.T) {
	x := mustParse(t, `{
		"min_installs": 5,
		"metrics": {"m": {"kind": "a"}},
		"cells": [
			{"metric": "m", "role": "r", "model": "thin", "mean": 1, "sd": 1, "n": 4},
			{"metric": "m", "role": "r", "model": "held", "mean": 2, "sd": 1, "n": 5}
		]
	}`)
	if _, ok := x.Cell("m", "r", "thin", nil); ok {
		t.Error("a cell below min_installs was returned by Cell")
	}
	names := ""
	for _, c := range x.Cells("m") {
		names += c.Model
	}
	if names != "held" {
		t.Errorf("Cells answered %q, want only the cell that met min_installs", names)
	}
}

// THE FLOOR COUNTS INSTALLS, NOT ROWS, because a cell one contributor
// filled alone is thin however many rows it holds, and a cell three
// contributors share meets the floor however few rows each added. A cell
// that carries no installs at all — the shape the seed carries — keeps
// meeting the floor on rows, the only count it has.
func TestTheFloorCountsInstallsNotRows(t *testing.T) {
	x := mustParse(t, `{
		"min_installs": 3,
		"metrics": {"m": {"kind": "a"}},
		"cells": [
			{"metric": "m", "role": "r", "model": "lone", "mean": 1, "sd": 1, "n": 9, "installs": 1},
			{"metric": "m", "role": "r", "model": "shared", "mean": 2, "sd": 1, "n": 3, "installs": 3},
			{"metric": "m", "role": "r", "model": "seed", "mean": 3, "sd": 1, "n": 3}
		]
	}`)
	if _, ok := x.Cell("m", "r", "lone", nil); ok {
		t.Error("a cell one install filled alone passed the floor on its rows")
	}
	if _, ok := x.Cell("m", "r", "shared", nil); !ok {
		t.Error("a cell three installs share was held below the floor")
	}
	if _, ok := x.Cell("m", "r", "seed", nil); !ok {
		t.Error("a cell carrying no installs did not meet the floor on its rows")
	}
}

func TestCellsAreSortedByRoleThenModelThenDims(t *testing.T) {
	x := mustParse(t, `{
		"metrics": {"m": {"kind": "a", "dims": ["region", "tier"]}},
		"cells": [
			{"metric": "m", "role": "zebra", "model": "a", "mean": 0, "sd": 0, "n": 1},
			{"metric": "m", "role": "alpha", "model": "z", "mean": 0, "sd": 0, "n": 1},
			{"metric": "m", "role": "alpha", "model": "a", "mean": 0, "sd": 0, "n": 1},
			{"metric": "m", "role": "alpha", "model": "a", "mean": 0, "sd": 0, "n": 1, "region": "eu", "tier": "lite"},
			{"metric": "m", "role": "alpha", "model": "a", "mean": 0, "sd": 0, "n": 1, "region": "eu"},
			{"metric": "m", "role": "alpha", "model": "a", "mean": 0, "sd": 0, "n": 1, "tier": "lite"}
		]
	}`)
	got := x.Cells("m")
	order := ""
	for _, c := range got {
		order += c.Role + ":" + c.Model + ":" + dimsKey(c.Dims) + ";"
	}
	want := "alpha:a:;alpha:a:region=eu;alpha:a:region=eu,tier=lite;alpha:a:tier=lite;alpha:z:;zebra:a:;"
	if order != want {
		t.Errorf("cells came out as\n%s\nwant\n%s", order, want)
	}
}

func TestWantedIsSortedByWeightThenModel(t *testing.T) {
	x := mustParse(t, `{
		"aliases": {"c/one": []},
		"wanted": [
			{"role": "r", "model": "c/one", "weight": 0.5},
			{"role": "r", "model": "a/two", "weight": 0.5},
			{"role": "r", "model": "b/three", "weight": 0.9}
		]
	}`)
	got := x.Wanted()
	order := ""
	for _, w := range got {
		order += w.Model + ","
	}
	if order != "b/three,a/two,c/one," {
		t.Errorf("wanted came out as %q", order)
	}
}

// ── COPIES AND SHARING ──────────────────────────────────────────────────────

// NOTHING A CALLER HOLDS REACHES INTO THE SHARED DOCUMENT: every accessor
// hands back copies, and a mutation through one must not leak into another.
func TestReturnedValuesAreCopies(t *testing.T) {
	x := mustParse(t, example)

	judges := x.Judges()
	judges[0] = "mutated"
	if x.Judges()[0] == "mutated" {
		t.Error("Judges handed back the shared slice")
	}

	rubrics := x.Rubrics()
	rubrics["planner"] = 99
	if x.Rubrics()["planner"] == 99 {
		t.Error("Rubrics handed back the shared map")
	}

	metrics := x.Metrics()
	metrics[0] = "mutated"
	if x.Metrics()[0] == "mutated" {
		t.Error("Metrics handed back the shared slice")
	}

	c, _ := x.Cell("role_rating", "planner", "z-ai/glm-5.3", nil)
	if c.Dims != nil {
		c.Dims["region"] = "mutated"
	}
	cells := x.Cells("role_rating")
	if len(cells) == 1 && cells[0].Dims != nil {
		cells[0].Dims["region"] = "mutated"
		if again, _ := x.Cell("role_rating", "planner", "z-ai/glm-5.3", nil); again.Dims != nil && again.Dims["region"] == "mutated" {
			t.Error("Cells handed back the shared dims map")
		}
	}

	wanted := x.Wanted()
	wanted[0].Weight = -1
	if x.Wanted()[0].Weight == -1 {
		t.Error("Wanted handed back the shared slice")
	}
}

func TestDimsMapsAreCopiesOnEveryCell(t *testing.T) {
	x := mustParse(t, `{
		"metrics": {"m": {"kind": "a", "dims": ["region"]}},
		"cells": [{"metric": "m", "role": "r", "model": "a", "mean": 1, "sd": 1, "n": 1, "region": "eu"}]
	}`)
	first, _ := x.Cell("m", "r", "a", map[string]string{"region": "eu"})
	first.Dims["region"] = "mutated"
	second, _ := x.Cell("m", "r", "a", map[string]string{"region": "eu"})
	if second.Dims["region"] == "mutated" {
		t.Error("a dims map handed back by Cell was the shared one")
	}
}

// ONE INDEX, MANY GOROUTINES, NO LOCK: the immutability is the design, and
// this is the cheapest way to catch anything that quietly broke it.
func TestOneIndexServesManyReadersAtOnce(t *testing.T) {
	x := mustParse(t, `{
		"min_installs": 2,
		"metrics": {"m": {"kind": "a", "dims": ["region"]}},
		"cells": [
			{"metric": "m", "role": "r", "model": "a", "mean": 1, "sd": 1, "n": 9, "region": "eu"}
		],
		"wanted": [{"role": "r", "model": "a", "weight": 1}]
	}`)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				x.Metrics()
				x.Kind("m")
				x.Cells("m")
				x.Cell("m", "r", "a", map[string]string{"region": "eu"})
				x.Wanted()
				x.Judges()
				x.Rubrics()
				x.Canonical("a")
			}
		}()
	}
	wg.Wait()
}

// ── FALLBACK ────────────────────────────────────────────────────────────────

const fallbackEmbedded = `{"schema": 1, "generated": "2026-09-01", "metrics": {"old": {"kind": "a"}}}`

func TestFallbackPrefersPrimaryAndFallsBackOnItsFailures(t *testing.T) {
	fresh := `{"schema": 1, "generated": "2026-09-17", "metrics": {"fresh": {"kind": "a"}}}`
	for _, row := range []struct {
		name, primary string
		wantMetric    string
	}{
		{"a fresh primary", fresh, "fresh"},
		{"a missing primary", "", "old"},
		{"an empty primary", "  ", "old"},
		{"an unparsable primary", `{"schema": `, "old"},
		{"a too-new primary", `{"schema": 2}`, "old"},
		{"an older primary", `{"schema": 1, "generated": "2026-08-31", "metrics": {"stale": {"kind": "a"}}}`, "old"},
	} {
		x, err := Fallback([]byte(row.primary), []byte(fallbackEmbedded))
		if err != nil {
			t.Errorf("%s: %v", row.name, err)
			continue
		}
		if got := x.Metrics(); len(got) != 1 || got[0] != row.wantMetric {
			t.Errorf("%s answered with %v, want the %s document", row.name, got, row.wantMetric)
		}
	}
}

func TestFallbackKeepsPrimaryOnEqualDates(t *testing.T) {
	x, err := Fallback(
		[]byte(`{"schema": 1, "generated": "2026-09-17", "metrics": {"primary": {"kind": "a"}}}`),
		[]byte(fallbackEmbedded),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := x.Metrics(); len(got) != 1 || got[0] != "primary" {
		t.Errorf("equal dates answered with %v, want primary kept", got)
	}
}

func TestFallbackKeepsPrimaryWhenEmbeddedWillNotParse(t *testing.T) {
	x, err := Fallback([]byte(`{"metrics": {"primary": {"kind": "a"}}}`), []byte("not json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := x.Metrics(); len(got) != 1 || got[0] != "primary" {
		t.Errorf("a good primary lost to a broken embedded: %v", got)
	}
}

func TestFallbackReturnsTheEmbeddedErrorWhenNeitherParses(t *testing.T) {
	_, err := Fallback([]byte(""), []byte(`{"schema": `))
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("neither parsing answered %v, want the embedded document's error", err)
	}
}
