package competence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 15, 14, 2, 0, 0, time.UTC)

func result(t *testing.T, path, model, place, title, task string, ago time.Duration, outcomes ...Outcome) {
	t.Helper()
	checks := make([]CheckRun, 0, len(outcomes))
	for index, outcome := range outcomes {
		checks = append(checks, CheckRun{Command: fmt.Sprintf("go test ./%d/...", index), Outcome: outcome})
	}
	err := Append(path, Record{
		Kind: KindResult, ID: TaskID("conv1", task), At: testNow.Add(-ago),
		Model: model, Place: place, PlaceKind: PlaceRepo, Work: WorkOf(title), Title: title,
		Source: Source{Kind: SourceTask, Agent: "conv1", Task: task, Transcript: "/t/conv1/transcript.jsonl"},
		Checks: checks,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func undo(t *testing.T, path, model, place, title, task string, ago time.Duration) {
	t.Helper()
	err := Append(path, Record{
		Kind: KindUndo, ID: TaskID("conv1", task), At: testNow.Add(-ago),
		Model: model, Place: place, Work: WorkOf(title), Title: title,
		Source: Source{Kind: SourceTask, Agent: "conv1", Task: task},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func reader(path string) Reader {
	return Reader{Path: path, Now: func() time.Time { return testNow }}
}

func scopeIn(t *testing.T, m Map, class Class, words string) Scope {
	t.Helper()
	for _, group := range m.Groups {
		for _, scope := range group.Scopes {
			if scope.Work.Words == words {
				if group.Class != class || scope.Class != class {
					t.Fatalf("%q is %s, want %s", words, scope.Class, class)
				}
				return scope
			}
		}
	}
	t.Fatalf("no scope %q in %+v", words, m.Groups)
	return Scope{}
}

// AN EMPTY LEDGER IS AN EMPTY MAP, said honestly: no model, four empty groups.
func TestAnEmptyLedgerIsFourEmptyGroupsAndNoModel(t *testing.T) {
	m, err := reader(filepath.Join(t.TempDir(), "none.jsonl")).Map(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Model != "" || len(m.Models) != 0 || len(m.Groups) != 4 || m.Totals != (Totals{}) {
		t.Fatalf("empty ledger answered %+v", m)
	}
	for index, class := range Classes {
		if m.Groups[index].Class != class || m.Groups[index].Scopes == nil || len(m.Groups[index].Scopes) != 0 {
			t.Fatalf("group %d is %+v", index, m.Groups[index])
		}
	}
}

// ONLY A CHECK THAT RAN IS A RESULT, and a row naming no model is refused
// rather than guessed about.
func TestTheLedgerRefusesUncheckedAndUnattributedResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	base := Record{Kind: KindResult, ID: "task:c:1", At: testNow, Model: "m", Place: "/p", Work: "w",
		Source: Source{Kind: SourceTask, Agent: "c"}}
	if err := Append(path, base); err == nil {
		t.Fatal("a result with no check was written")
	}
	unread := base
	unread.Checks = []CheckRun{{Command: "go test", Outcome: "unread"}}
	if err := Append(path, unread); err == nil {
		t.Fatal("a result whose only check is neither a pass nor a failure was written")
	}
	anonymous := base
	anonymous.Model = ""
	anonymous.Checks = []CheckRun{{Command: "go test", Outcome: Passed}}
	if !errors.Is(Append(path, anonymous), ErrIncomplete) {
		t.Fatal("a result with no model was written")
	}
	// And a row that reached the file some other way makes the history
	// unavailable on read: it may have been a failure.
	if err := os.WriteFile(path, []byte(`{"v":1,"kind":"result","id":"x","model":"","place":"/p","work":"w","checks":[{"command":"a","outcome":"passed"}]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader(path).Map(context.Background(), "", ""); !errors.Is(err, ErrUnreadableHistory) {
		t.Fatalf("an unattributed row was read past: %v", err)
	}
}

// THE GROUPS: passes for, failures and undos against, thirty days, then stale.
func TestScopesAreGroupedFromChecksAndUndosAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	day := 24 * time.Hour
	// Strong: five passes.
	for i := 0; i < 5; i++ {
		result(t, path, "m/flash", "/w/api", "unit tests for the rail", fmt.Sprint("s", i), time.Duration(i+1)*day, Passed)
	}
	// Frontier: three passes, one failure, one passed-then-undone.
	for i := 0; i < 3; i++ {
		result(t, path, "m/flash", "/w/api", "parser changes", fmt.Sprint("f", i), time.Duration(i+1)*day, Passed, Passed)
	}
	result(t, path, "m/flash", "/w/api", "parser changes", "f3", 2*day, Passed, Failed)
	result(t, path, "m/flash", "/w/api", "parser changes", "f4", 3*day, Passed)
	undo(t, path, "m/flash", "/w/api", "parser changes", "f4", 2*day)
	// Weak: one pass, three failures.
	result(t, path, "m/flash", "/w/infra", "sql migrations", "w0", day, Passed)
	for i := 1; i < 4; i++ {
		result(t, path, "m/flash", "/w/infra", "sql migrations", fmt.Sprint("w", i), day, Failed)
	}
	// Stale: everything older than thirty days, counted in full.
	result(t, path, "m/flash", "/w/ffi", "rust bindings", "o0", 40*day, Passed)
	result(t, path, "m/flash", "/w/ffi", "rust bindings", "o1", 50*day, Failed)
	// Another model's results never reach this model's map.
	result(t, path, "m/other", "/w/api", "unit tests for the rail", "x0", day, Failed)

	m, err := reader(path).Map(context.Background(), "m/flash", "")
	if err != nil {
		t.Fatal(err)
	}
	strong := scopeIn(t, m, Strong, "unit tests for the rail")
	if strong.Results != 5 || strong.Passed != 5 || strong.Since.IsZero() {
		t.Fatalf("strong scope %+v", strong)
	}
	frontier := scopeIn(t, m, Frontier, "parser changes")
	if frontier.Results != 5 || frontier.Passed != 3 || frontier.Failed != 1 || frontier.Undone != 1 {
		t.Fatalf("frontier scope %+v", frontier)
	}
	weak := scopeIn(t, m, Weak, "sql migrations")
	if weak.Passed != 1 || weak.Failed != 3 {
		t.Fatalf("weak scope %+v", weak)
	}
	stale := scopeIn(t, m, Stale, "rust bindings")
	if stale.Results != 2 || !stale.Since.IsZero() {
		t.Fatalf("stale scope %+v", stale)
	}
	if m.Totals.Scopes != 4 || m.Totals.Results != 16 {
		t.Fatalf("totals %+v", m.Totals)
	}
	if len(m.Models) != 2 || m.Policy.StaleAfterDays != 30 {
		t.Fatalf("models %+v policy %+v", m.Models, m.Policy)
	}

	// A shorter policy is a policy, not a fork of the arithmetic.
	short := reader(path)
	short.Policy = DefaultPolicy()
	short.Policy.StaleAfter = 12 * time.Hour
	m, err = short.Map(context.Background(), "m/flash", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Groups[3].Scopes) != 4 {
		t.Fatalf("with a twelve-hour window every scope is stale: %+v", m.Groups)
	}
}

// A FILTER NARROWS THE GROUPS AND NEVER THE MENU, and an unknown one is refused.
func TestTheScopeFilterNarrowsByPlaceWorkOrScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	result(t, path, "m", "/w/api", "unit tests", "1", time.Hour, Passed)
	result(t, path, "m", "/w/web", "unit tests", "2", time.Hour, Passed)
	result(t, path, "m", "/w/web", "css fixes", "3", time.Hour, Failed)
	all, err := reader(path).Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	if all.Totals.Scopes != 3 || len(all.Filters) != 4 {
		t.Fatalf("all scopes %+v filters %+v", all.Totals, all.Filters)
	}
	var place, work string
	for _, filter := range all.Filters {
		if filter.Kind == FilterPlace && filter.Path == "/w/web" {
			place = filter.ID
		}
		if filter.Kind == FilterWork && filter.Words == "unit tests" {
			work = filter.ID
		}
	}
	for _, test := range []struct {
		scope string
		want  int
	}{{place, 2}, {work, 2}, {all.Groups[1].Scopes[0].ID, 1}} {
		m, err := reader(path).Map(context.Background(), "m", test.scope)
		if err != nil {
			t.Fatal(err)
		}
		if m.Totals.Scopes != test.want || len(m.Filters) != 4 || m.Scope != test.scope {
			t.Fatalf("filter %s: %d scopes, %d filters", test.scope, m.Totals.Scopes, len(m.Filters))
		}
	}
	if _, err := reader(path).Map(context.Background(), "m", "place:nope"); !errors.Is(err, ErrUnknownScope) {
		t.Fatalf("an unknown filter answered %v", err)
	}
}

// A SCOPE IS ITS PLACE AND ITS EXACT KIND, and its id does not move when other
// results arrive or older ones leave the read window.
func TestScopeIDsAreStableExactKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	result(t, path, "m", "/w", "tests for the composer", "2", 2*time.Hour, Passed)
	first, err := reader(path).Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	id := first.Groups[1].Scopes[0].ID
	result(t, path, "m", "/w", "tests for the rail", "1", 3*time.Hour, Passed)
	result(t, path, "m", "/w", "Tests, for the composer!", "3", time.Hour, Failed)
	m, err := reader(path).Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Totals.Scopes != 2 {
		t.Fatalf("want two exact kinds, got %+v", m.Groups)
	}
	found := false
	for _, scope := range m.Groups[1].Scopes {
		if scope.ID == id {
			found = scope.Results == 2 && scope.Work.Words == "tests for the composer"
		}
	}
	if !found {
		t.Fatalf("the composer scope's id moved or lost its result: %+v", m.Groups[1].Scopes)
	}
}

// THE DEFAULT MODEL IS THE ONE WITH THE NEWEST RESULT; nothing is invented.
func TestNoModelAsksForTheNewestEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	result(t, path, "m/old", "/w", "a", "1", 3*time.Hour, Passed)
	result(t, path, "m/new", "/w", "a", "2", time.Hour, Passed)
	m, err := reader(path).Map(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Model != "m/new" || m.Models[0].ID != "m/new" || m.Models[1].ID != "m/old" {
		t.Fatalf("model %q models %+v", m.Model, m.Models)
	}
	// A model nobody has evidence for is an empty map, not somebody else's.
	m, err = reader(path).Map(context.Background(), "m/never", "")
	if err != nil || m.Model != "m/never" || m.Totals.Results != 0 {
		t.Fatalf("unknown model answered %+v %v", m, err)
	}
}

// A REMOVED PLACE IS SAID, AND THE SCOPE STAYS IN ITS GROUP.
func TestARemovedPlaceIsLabelledAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	result(t, path, "m", "/w/gone", "rust bindings", "1", time.Hour, Passed)
	r := reader(path)
	r.PlaceAdded = func(place string) bool { return place != "/w/gone" }
	m, err := r.Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	scope := scopeIn(t, m, Frontier, "rust bindings")
	if !scope.Place.Removed || !m.Filters[0].Removed {
		t.Fatalf("removed place not said: %+v %+v", scope.Place, m.Filters)
	}
}

// EVIDENCE OUTLIVES ITS SOURCE, pages by a stable cursor, and filters by outcome.
func TestEvidencePagesAndKeepsResultsWhoseSourceIsGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	for i := 0; i < 12; i++ {
		outcome := Passed
		if i%4 == 0 {
			outcome = Failed
		}
		result(t, path, "m", "/w", "parser changes", fmt.Sprint(i), time.Duration(i+1)*time.Hour, Passed, outcome)
	}
	undo(t, path, "m", "/w", "parser changes", "1", 30*time.Minute)
	r := reader(path)
	r.SourceGone = func(source Source) string {
		if source.Task == "2" {
			return SourceTask
		}
		return ""
	}
	r.SourceTitle = func(Source) string { return "Release" }
	m, err := r.Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	scope := m.Groups[1].Scopes[0].ID

	var seen []string
	cursor := ""
	for pages := 0; ; pages++ {
		page, err := r.Evidence(context.Background(), "m", scope, "", cursor, 0)
		if err != nil {
			t.Fatal(err)
		}
		if page.Counts != (Counts{All: 12, Passed: 8, Failed: 3, Undone: 1}) {
			t.Fatalf("counts %+v", page.Counts)
		}
		if len(page.Items) > DefaultEvidenceLimit {
			t.Fatalf("page of %d", len(page.Items))
		}
		for _, item := range page.Items {
			seen = append(seen, item.Source.Task)
			if item.Source.Task == "2" && item.SourceMissing != SourceTask {
				t.Fatalf("a deleted task's result lost its marker: %+v", item)
			}
			if item.Source.Task == "1" && (item.Outcome != Undone || item.UndoneAt.IsZero() || item.Check.Outcome != Passed) {
				t.Fatalf("passed-then-undone reads %+v", item)
			}
			if item.Source.Task == "0" && (item.Outcome != Failed || item.Check.Outcome != Failed || len(item.Checks) != 2) {
				t.Fatalf("a failed result reads %+v", item)
			}
			if item.Source.Transcript != "" || item.SourceTitle != "Release" {
				t.Fatalf("the source said %+v / %q", item.Source, item.SourceTitle)
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 3 {
			t.Fatal("the cursor never ended")
		}
	}
	if strings.Join(seen, ",") != "0,1,2,3,4,5,6,7,8,9,10,11" {
		t.Fatalf("pages walked %v", seen)
	}

	failed, err := r.Evidence(context.Background(), "m", scope, Failed, "", 100)
	if err != nil || len(failed.Items) != 3 || failed.NextCursor != "" {
		t.Fatalf("failed filter %+v %v", failed, err)
	}
	if _, err := r.Evidence(context.Background(), "other", scope, "", cursor, 5); !errors.Is(err, ErrBadCursor) {
		t.Fatalf("another model's cursor answered %v", err)
	}
	if _, err := r.Evidence(context.Background(), "m", "scope:nope", "", "", 5); !errors.Is(err, ErrUnknownScope) {
		t.Fatalf("unknown scope answered %v", err)
	}
	big, err := r.Evidence(context.Background(), "m", scope, "", "", 1000)
	if err != nil || len(big.Items) != 12 {
		t.Fatalf("a limit past the ceiling answered %d %v", len(big.Items), err)
	}
}

// A LATER RESULT FOR THE SAME WORK REPLACES THE EARLIER, and work done again
// after an undo is no longer undone.
func TestTheLatestResultStands(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	result(t, path, "m", "/w", "a", "1", 3*time.Hour, Failed)
	undo(t, path, "m", "/w", "a", "1", 2*time.Hour)
	result(t, path, "m", "/w", "a", "1", time.Hour, Passed)
	m, err := reader(path).Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	scope := m.Groups[1].Scopes[0]
	if scope.Results != 1 || scope.Passed != 1 {
		t.Fatalf("scope %+v", scope)
	}
}

// THE READ IS BOUNDED AND NEVER PARTIAL: a ledger past the bound answers
// ErrHistoryLimit for the map and for evidence, with no groups or counts; a
// cancelled read answers nothing.
func TestTheReadIsBoundedAndCancellable(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	for i := 0; i < 50; i++ {
		result(t, path, fmt.Sprint("m", i), "/w", "a", fmt.Sprint(i), time.Duration(50-i)*time.Minute, Passed)
	}
	r := reader(path)
	r.MaxBytes = 2048
	m, err := r.Map(context.Background(), "", "")
	if !errors.Is(err, ErrHistoryLimit) || len(m.Groups) != 0 || len(m.Models) != 0 {
		t.Fatalf("a ledger past the bound answered %+v %v", m, err)
	}
	if page, err := r.Evidence(context.Background(), "m49", "scope:x", "", "", 5); !errors.Is(err, ErrHistoryLimit) || page.Counts.All != 0 {
		t.Fatalf("evidence past the bound answered %+v %v", page, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader(path).Map(ctx, "", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled read answered %v", err)
	}
}

// AN EARLY FAILURE THE BOUND WOULD HIDE MUST NOT BECOME STRONG. A failed check
// inside the thirty days sits before enough later passes to push it past
// MaxBytes; reading only the tail would class the scope Strong.
func TestAFailureBeyondTheBoundIsNeverClassedAsStrong(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	day := 24 * time.Hour
	result(t, path, "m", "/w", "a", "early", 20*day, Failed)
	for i := 0; i < 40; i++ {
		result(t, path, "m", "/w", "a", fmt.Sprint("late", i), time.Duration(i+1)*time.Hour, Passed)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	r := reader(path)
	r.MaxBytes = info.Size() * 3 / 4
	m, err := r.Map(context.Background(), "m", "")
	if !errors.Is(err, ErrHistoryLimit) {
		t.Fatalf("map answered %v", err)
	}
	for _, group := range m.Groups {
		if len(group.Scopes) != 0 {
			t.Fatalf("a partial classification escaped: %+v", m.Groups)
		}
	}
	if m.Totals != (Totals{}) || m.Truncated {
		t.Fatalf("partial counts escaped: %+v", m)
	}
	if _, err := r.Evidence(context.Background(), "m", "scope:x", "", "", 5); !errors.Is(err, ErrHistoryLimit) {
		t.Fatalf("evidence answered %v", err)
	}
	// Whole, the same ledger counts the failure the bound would have hidden.
	whole, err := reader(path).Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	if scope := scopeIn(t, whole, Strong, "a"); scope.Failed != 1 || scope.Passed != 40 {
		t.Fatalf("whole read %+v", scope)
	}
}

// A ROW THAT CANNOT BE READ MAKES THE HISTORY UNAVAILABLE and the file is
// left byte for byte as it was; a blank line is harmless.
func TestAnUnreadableRowMakesTheHistoryUnavailable(t *testing.T) {
	good := func(path string) {
		result(t, path, "m", "/w", "a", "late", time.Hour, Passed)
	}
	cases := map[string]string{
		"corrupt failed row": `{"v":1,"kind":"result","id":"task:c:1","model":"m","place":"/w","work":"a","at":"2026-10-14T00:00:00Z","checks":[{"command":"go te`,
		"not json":           `not a row at all`,
		"unknown kind":       `{"v":1,"kind":"mystery","id":"x","at":"2026-10-14T00:00:00Z","model":"m","place":"/w","work":"a"}`,
		"unknown version":    `{"v":2,"kind":"result","id":"x","at":"2026-10-14T00:00:00Z","model":"m","place":"/w","work":"a","checks":[{"command":"a","outcome":"failed"}]}`,
		"no checks":          `{"v":1,"kind":"result","id":"x","at":"2026-10-14T00:00:00Z","model":"m","place":"/w","work":"a"}`,
		"overlong":           `{"v":1,"kind":"result","id":"` + strings.Repeat("x", maxRowBytes) + `"}`,
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), LedgerName)
			if err := os.WriteFile(path, []byte(row+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			good(path)
			before, _ := os.ReadFile(path)
			m, err := reader(path).Map(context.Background(), "m", "")
			if !errors.Is(err, ErrUnreadableHistory) || m.Totals != (Totals{}) || len(m.Groups) != 0 {
				t.Fatalf("map answered %+v %v", m, err)
			}
			if _, err := reader(path).Evidence(context.Background(), "m", "scope:x", "", "", 5); !errors.Is(err, ErrUnreadableHistory) {
				t.Fatalf("evidence answered %v", err)
			}
			if after, _ := os.ReadFile(path); string(after) != string(before) {
				t.Fatal("the read rewrote the ledger")
			}
		})
	}
	path := filepath.Join(t.TempDir(), LedgerName)
	good(path)
	file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	file.WriteString("\n   \n")
	file.Close()
	m, err := reader(path).Map(context.Background(), "m", "")
	if err != nil || m.Totals.Results != 1 {
		t.Fatalf("blank lines broke the read: %+v %v", m.Totals, err)
	}
}

// THE READ WRITES NOTHING.
func TestReadingNeverWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LedgerName)
	if _, err := reader(path).Map(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a read made the ledger: %v", err)
	}
	result(t, path, "m", "/w", "a", "1", time.Hour, Passed)
	before, _ := os.ReadFile(path)
	m, _ := reader(path).Map(context.Background(), "m", "")
	if _, err := reader(path).Evidence(context.Background(), "m", m.Groups[1].Scopes[0].ID, "", "", 5); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("a read changed the ledger")
	}
}

// THE OUTCOME IS DECIDED OVER EVERY CHECK, BEFORE THE LIST IS CUT. Nine checks
// whose first failed and whose last eight passed are a failure, and the check
// that failed is one of those shown.
func TestAFailureIsNeverCutOutOfAResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	checks := []CheckRun{{Command: "go vet ./...", Outcome: Failed}}
	for i := 0; i < 8; i++ {
		checks = append(checks, CheckRun{Command: fmt.Sprintf("go test ./p%d", i), Outcome: Passed})
	}
	err := Append(path, Record{
		Kind: KindResult, ID: "task:c:1", At: testNow, Model: "m", Place: "/w", Work: "a",
		Source: Source{Kind: SourceTask, Agent: "c", Task: "1"}, Checks: checks,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := reader(path)
	m, err := r.Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	scope := m.Groups[1].Scopes[0]
	if scope.Failed != 1 || scope.Passed != 0 {
		t.Fatalf("nine checks with the first red read %+v", scope)
	}
	page, err := r.Evidence(context.Background(), "m", scope.ID, "", "", 7)
	if err != nil {
		t.Fatal(err)
	}
	item := page.Items[0]
	if item.Outcome != Failed || len(item.Checks) != MaxChecks || item.Check == nil || item.Check.Command != "go vet ./..." {
		t.Fatalf("item %+v", item)
	}
}

// A CHECK RUN AGAIN IS ONE CHECK, AT ITS LAST RUN; and the decider kept past
// the cut is the first that failed.
func TestARepeatedCheckIsItsLastRun(t *testing.T) {
	runs := []CheckRun{
		{Command: "go test ./...", Outcome: Failed},
		{Command: "go  test   ./...", Outcome: Passed},
	}
	if got := lastRunOfEach(runs); len(got) != 1 || got[0].Outcome != Passed {
		t.Fatalf("a red check run green again reads %+v", got)
	}
	runs = []CheckRun{{Command: "go test ./...", Outcome: Passed}, {Command: "go test ./...", Outcome: Failed}}
	if got := lastRunOfEach(runs); len(got) != 1 || got[0].Outcome != Failed {
		t.Fatalf("a green check run red again reads %+v", got)
	}
	var many []CheckRun
	for i := 0; i < 12; i++ {
		outcome := Passed
		if i == 10 || i == 11 {
			outcome = Failed
		}
		many = append(many, CheckRun{Command: fmt.Sprint("c", i), Outcome: outcome})
	}
	shown := shownChecks(many)
	if len(shown) != MaxChecks || shown[MaxChecks-1].Command != "c10" || shown[0].Command != "c0" {
		t.Fatalf("shown %+v", shown)
	}
}

// AN UNDO MARKS A CHECKED RESULT AND NEVER MAKES ONE. Work that finished with
// no check is not evidence, and taking it back does not make it evidence.
func TestAnUndoWithNoCheckedResultIsNoEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	undo(t, path, "m", "/w", "a", "9", time.Hour)
	m, err := reader(path).Map(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Model != "" || len(m.Models) != 0 || m.Totals.Results != 0 {
		t.Fatalf("an undo of unchecked work became evidence: %+v", m)
	}
	// An undo of OLDER work than the result standing does not mark it.
	result(t, path, "m", "/w", "a", "9", 30*time.Minute, Passed)
	m, _ = reader(path).Map(context.Background(), "", "")
	if scope := m.Groups[1].Scopes[0]; scope.Passed != 1 || scope.Undone != 0 {
		t.Fatalf("scope %+v", scope)
	}
}

// AN IDENTITY THAT DOES NOT FIT IS REFUSED, NOT CUT; and a secret never
// reaches the file.
func TestRowsAreBoundedAndCarryNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	good := Record{
		Kind: KindResult, ID: "task:c:1", At: testNow, Model: "m", Place: "/w", Work: "a",
		Source: Source{Kind: SourceTask, Agent: "c", Task: "1"},
		Checks: []CheckRun{{Command: "curl -H 'Authorization: Bearer sk-ant-api03-" + strings.Repeat("x", 40) + "' localhost && " + strings.Repeat("y", 2000), Outcome: Passed}},
	}
	for name, mutate := range map[string]func(*Record){
		"long model":     func(r *Record) { r.Model = strings.Repeat("m", 300) },
		"long place":     func(r *Record) { r.Place = "/" + strings.Repeat("p", 5000) },
		"relative place": func(r *Record) { r.Place = "w" },
		"newline id":     func(r *Record) { r.ID = "task:c:1\n{}" },
		"long agent":     func(r *Record) { r.Source.Agent = strings.Repeat("a", 200) },
		"bad source":     func(r *Record) { r.Source.Kind = "turn" },
	} {
		bad := good
		mutate(&bad)
		if Append(path, bad) == nil {
			t.Errorf("%s was written", name)
		}
	}
	if err := Append(path, good); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "sk-ant-api03-") || len(raw) > 2048 {
		t.Fatalf("the row carried a secret or an unbounded command: %d bytes", len(raw))
	}
}

// THE ANSWER IS BOUNDED, AND SAYS WHAT IT LEFT OUT.
func TestTheMapAnswerIsBoundedAndHonest(t *testing.T) {
	path := filepath.Join(t.TempDir(), LedgerName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxMapScopes+20; i++ {
		record, _ := normalized(Record{
			Kind: KindResult, ID: fmt.Sprint("task:c:", i), At: testNow.Add(-time.Duration(i) * time.Minute),
			Model: "m", Place: fmt.Sprint("/w/", i), Work: fmt.Sprint("work", i),
			Source: Source{Kind: SourceTask, Agent: "c"}, Checks: []CheckRun{{Command: "t", Outcome: Passed}},
		})
		line, _ := json.Marshal(record)
		file.Write(append(line, '\n'))
	}
	for i := 0; i < MaxMapModels+5; i++ {
		record, _ := normalized(Record{
			Kind: KindResult, ID: "task:c:x", At: testNow.Add(-48 * time.Hour), Model: fmt.Sprint("other", i),
			Place: "/w", Work: "a", Source: Source{Kind: SourceTask, Agent: "c"}, Checks: []CheckRun{{Command: "t", Outcome: Passed}},
		})
		line, _ := json.Marshal(record)
		file.Write(append(line, '\n'))
	}
	file.Close()
	asked := 0
	r := reader(path)
	r.PlaceAdded = func(string) bool { asked++; return true }
	m, err := r.Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	drawn := 0
	for _, group := range m.Groups {
		drawn += len(group.Scopes)
	}
	if drawn != MaxMapScopes || m.Omitted.Scopes != 20 || m.Totals.Scopes != MaxMapScopes+20 {
		t.Fatalf("drawn %d omitted %+v totals %+v", drawn, m.Omitted, m.Totals)
	}
	if len(m.Filters) != MaxMapFilters || m.Omitted.Filters != 2*(MaxMapScopes+20)-MaxMapFilters {
		t.Fatalf("filters %d omitted %d", len(m.Filters), m.Omitted.Filters)
	}
	if len(m.Models) != MaxMapModels || m.Omitted.Models != 6 {
		t.Fatalf("models %d omitted %d", len(m.Models), m.Omitted.Models)
	}
	if asked > MaxMapScopes {
		t.Fatalf("asked about %d places for %d drawn", asked, MaxMapScopes)
	}
}

// A READ THAT RUNS OUT OF TIME STOPS, IN THE GROUPING AS WELL AS THE SCAN, and
// a large ledger of unique kinds is grouped in one pass.
func TestALargeLedgerIsGroupedInOnePassAndStopsWhenCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a large ledger")
	}
	path := filepath.Join(t.TempDir(), LedgerName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	const rows = 50000
	for i := 0; i < rows; i++ {
		record, _ := normalized(Record{
			Kind: KindResult, ID: fmt.Sprint("task:c:", i), At: testNow.Add(-time.Duration(i) * time.Second),
			Model: "m", Place: "/w", Work: fmt.Sprintf("unique kind %d of work", i),
			Source: Source{Kind: SourceTask, Agent: "c"}, Checks: []CheckRun{{Command: "t", Outcome: Passed}},
		})
		line, _ := json.Marshal(record)
		file.Write(append(line, '\n'))
	}
	file.Close()

	started := time.Now()
	m, err := reader(path).Map(context.Background(), "m", "")
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("%d unique kinds took %s", rows, took)
	}
	drawn := 0
	for _, group := range m.Groups {
		drawn += len(group.Scopes)
	}
	if m.Totals.Scopes != rows || drawn != MaxMapScopes || len(m.Filters) != MaxMapFilters {
		t.Fatalf("totals %+v drawn %d filters %d", m.Totals, drawn, len(m.Filters))
	}

	// A deadline that passes during the read ends it with the deadline's error.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := reader(path).Map(ctx, "m", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an expired read answered %v", err)
	}
	// And a scope built from a folded read stops on cancellation.
	f, err := reader(path).read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := f.scopes(cancelled, "m"); !errors.Is(err, context.Canceled) {
		t.Fatalf("grouping a cancelled read answered %v", err)
	}
}

// A ROW APPENDED AFTER THE SIZE WAS CHECKED BELONGS TO THE NEXT READ: the scan
// stops at the snapshot, and bytes past it — even half a line — are neither
// read nor an error.
func TestTheScanStopsAtTheSnapshotItWasGiven(t *testing.T) {
	row := func(id string) string {
		return `{"v":1,"kind":"result","id":"` + id + `","at":"2026-10-14T00:00:00Z","model":"m","place":"/w","work":"a",` +
			`"source":{"kind":"task","agent":"c"},"outcome":"passed","checks":[{"command":"go test","outcome":"passed"}]}` + "\n"
	}
	snapshot := row("one") + row("two")
	grown := snapshot + row("three")[:40]
	var ids []string
	err := scanRows(context.Background(), strings.NewReader(grown), int64(len(snapshot)), func(r Record) { ids = append(ids, r.ID) })
	if err != nil || strings.Join(ids, ",") != "one,two" {
		t.Fatalf("snapshot read %v %v", ids, err)
	}
}

// A ROW IS HELD TO WHAT THE WRITER WRITES: an outcome or check that is not a
// pass or a failure, an empty command or an unknown source never folds.
func TestValidRowHoldsARowToTheWriter(t *testing.T) {
	good := func() Record {
		return Record{V: 1, Kind: KindResult, ID: "task:c:1", At: testNow, Model: "m", Place: "/w", Work: "a",
			Source: Source{Kind: SourceTask, Agent: "c"}, Outcome: Failed,
			Checks: []CheckRun{{Command: "go test", Outcome: Failed}}}
	}
	if !validRow(good()) {
		t.Fatal("a row the writer would write was refused")
	}
	legacy := good()
	legacy.Outcome = ""
	if !validRow(legacy) {
		t.Fatal("a row with no outcome and valid checks was refused")
	}
	undone := Record{V: 1, Kind: KindUndo, ID: "x", At: testNow, Model: "m", Place: "/w", Work: "a", Source: Source{Kind: SourceTask, Agent: "c"}}
	if !validRow(undone) {
		t.Fatal("an undo was refused")
	}
	for name, mutate := range map[string]func(*Record){
		"garbage outcome":       func(r *Record) { r.Outcome = "garbage" },
		"undone outcome":        func(r *Record) { r.Outcome = Undone },
		"unknown check outcome": func(r *Record) { r.Checks[0].Outcome = "unread" },
		"empty check outcome":   func(r *Record) { r.Checks[0].Outcome = "" },
		"empty command":         func(r *Record) { r.Checks[0].Command = "  " },
		"huge command":          func(r *Record) { r.Checks[0].Command = strings.Repeat("c", 4*maxCommand+1) },
		"too many checks":       func(r *Record) { r.Checks = make([]CheckRun, MaxChecks+1) },
		"passed beside failed":  func(r *Record) { r.Outcome = Passed },
		"unknown source":        func(r *Record) { r.Source.Kind = "dream" },
		"no agent":              func(r *Record) { r.Source.Agent = "" },
		"no time":               func(r *Record) { r.At = time.Time{} },
		"unknown version":       func(r *Record) { r.V = 2 },
	} {
		r := good()
		r.Checks = append([]CheckRun(nil), r.Checks...)
		mutate(&r)
		if validRow(r) {
			t.Fatalf("%s was accepted", name)
		}
	}
}
