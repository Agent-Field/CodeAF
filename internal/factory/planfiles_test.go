package factory

import (
	"reflect"
	"strings"
	"testing"
)

func planItem(state PhaseState, output string, claims ...Claim) Item {
	return Item{Stream: &Stream{Phases: []Phase{
		{Name: "plan", State: state, Result: &StageResult{Done: true, Output: output, Claims: claims}},
		{Name: "write", State: PhasePending},
	}}}
}

// Claims quoted from a real plan that named no list: none of their tokens is
// a declared file.
func TestClaimsAreNeverPathMetadata(t *testing.T) {
	it := planItem(PhaseDone, "Plan: add an Amount field.",
		Claim{Text: "Entry.Amount is read in ledger.go", Evidence: "README.md:12"},
		Claim{Text: "read, grep and ls found nothing else"},
	)
	if got := PlanFilesOf(it); got != nil {
		t.Fatalf("claims became files: %v", got)
	}
}

func TestOnlyTheDeclarationIsPathMetadata(t *testing.T) {
	cases := []struct {
		name, out string
		want      []string
	}{
		{"heading bullets", "Wrap it.\n\n## Files\n- internal/a.go\n- `internal/b.go`\n\nThen stop.", []string{"internal/a.go", "internal/b.go"}},
		{"numbered, blank gaps", "## Files\n\n1. a.go\n\n2) b.go", []string{"a.go", "b.go"}},
		{"inline", "files: internal/a.go, ./internal/b.go", []string{"internal/a.go", "internal/b.go"}},
		{"new, dotfile, extensionless", "Files: pkg/new/thing.go, .gitignore, Makefile, docs/LICENSE", []string{"pkg/new/thing.go", ".gitignore", "Makefile", "docs/LICENSE"}},
		{"spaces quoted", "files: `docs/my notes.md`, \"a b/c.txt\", d.go", []string{"docs/my notes.md", "a b/c.txt", "d.go"}},
		{"spaces bullet", "## Files\n- `docs/my notes.md`", []string{"docs/my notes.md"}},
		{"duplicates fold", "files: a.go, a.go", []string{"a.go"}},
		{"unheaded bullets are prose", "plan\n- deliver.go\n- api/retry.go", nil},
		{"path in prose", "see docs/design/DESIGN.md and stop", nil},
		{"unquoted spaces are ambiguous", "files: docs/my notes.md, a.go", nil},
		{"bullet with a note is ambiguous", "## Files\n- a.go - the sender\n- b.go", nil},
		{"one bad entry rejects the list", "files: a.go, ../b.go", nil},
		{"empty entry", "files: a.go,, b.go", nil},
		{"unclosed quote", "files: `a.go, b.go", nil},
		{"junk after quote", "files: `a.go` b.go", nil},
		{"empty heading", "## Files\n\nNothing else.", nil},
		{"bracket route", "files: app/[id]/page.tsx, app/layout.tsx", []string{"app/[id]/page.tsx", "app/layout.tsx"}},
		{"bracket and brace bullets", "## Files\n- app/[slug]/page.tsx\n- src/routes/{id}.ts\n- src/pages/[...all].tsx", []string{"app/[slug]/page.tsx", "src/routes/{id}.ts", "src/pages/[...all].tsx"}},
		{"fenced example before real list", "Example:\n```\nfiles: example.go\n```\n\n## Files\n- real/a.go\n- real/b.go", []string{"real/a.go", "real/b.go"}},
		{"fenced example only", "```\n## Files\n- example.go\n```", nil},
		{"tilde fence and unclosed fence", "~~~md\nfiles: x.go\n~~~\nfiles: y.go\n```\nfiles: z.go", []string{"y.go"}},
		{"contradictory declarations", "## Files\n- a.go\n\n## Files\n- b.go", nil},
		{"repeated identical declaration", "files: a.go\n\n## Files\n- a.go", []string{"a.go"}},
		{"files none", "Files: none", nil},
		{"files none bullet", "## Files\n- None", nil},
		{"first declaration only", "files: a b\nfiles: c.go", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParsePlanFiles(c.out); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestUnsafePathsRejectTheWholeList(t *testing.T) {
	for _, bad := range []string{
		"/etc/passwd", "../x.go", "a/../../x.go", "a/./b.go", "a//b.go", "dir/", "C:/x.go", `a\b.go`,
		"https://x.test/a.go", "src/*.go", "a?.go", "~/x.go", "a\x00b.go", "a\x1b[31m.go", "a\u202eb.go",
		"a\u009bb.go", "a\nb.go", ".", "..", "file.", strings.Repeat("a/", 300) + "b.go",
	} {
		if ValidPlanPath(bad) {
			t.Errorf("%q accepted", bad)
		}
		if got := ParsePlanFiles("files: ok.go, `" + bad + "`"); got != nil {
			t.Errorf("%q kept a list: %v", bad, got)
		}
	}
	if ValidPlanPath("") {
		t.Error("empty accepted")
	}
}

func TestCurrentDeclarationWinsAndOtherStatesAreUnknown(t *testing.T) {
	want := []string{"a.go"}
	if got := PlanFilesOf(planItem(PhaseDone, "files: a.go")); !reflect.DeepEqual(got, want) {
		t.Fatalf("done: %v", got)
	}
	// A bare waiting plan is a question mid-round, not a plan anybody approved.
	if got := PlanFilesOf(planItem(PhaseWaiting, "files: a.go")); got != nil {
		t.Fatalf("waiting: %v", got)
	}
	// A plan sent back to run again keeps its old result until the next one
	// lands; the old list is not the new plan's.
	for _, st := range []PhaseState{PhasePending, PhaseRunning, PhaseFailed} {
		if got := PlanFilesOf(planItem(st, "files: a.go")); got != nil {
			t.Fatalf("%s kept %v", st, got)
		}
	}
	// A newer plan without a list does not inherit the cache.
	it := planItem(PhaseDone, "a better plan, no list")
	it.PlannedFiles = []string{"stale.go", "old.go", "older.go", "oldest.go"}
	if got := PlanFilesOf(it); got != nil {
		t.Fatalf("stale cache read: %v", got)
	}
	// A stage that is not the plan cannot speak for it.
	other := planItem(PhaseDone, "no list")
	other.Stream.Phases[1] = Phase{Name: "write", State: PhaseDone, Result: &StageResult{Output: "files: w.go"}}
	if got := PlanFilesOf(other); got != nil {
		t.Fatalf("write spoke for plan: %v", got)
	}
	// Two steps called plan: which is current is not known.
	two := planItem(PhaseDone, "files: a.go")
	two.Stream.Phases = append(two.Stream.Phases, Phase{Name: "Plan", State: PhaseDone, Result: &StageResult{Output: "files: b.go"}})
	if got := PlanFilesOf(two); got != nil {
		t.Fatalf("ambiguous plan: %v", got)
	}
	if PlanFilesOf(Item{}) != nil || PlanFilesOf(Item{PlannedFiles: []string{"x.go"}}) != nil {
		t.Fatal("no stream is unknown")
	}
}

// shaped is a plan item in the exact stored shape a run leaves: round, round
// lines, the question kind, and the gate after the plan.
func shaped(plan PhaseState, round int, lines []int, done bool, gate PhaseState, cur int, qkind string) Item {
	var rl []RoundLine
	for _, n := range lines {
		rl = append(rl, RoundLine{Round: n})
	}
	return Item{QKind: qkind, Stream: &Stream{Cur: cur, Phases: []Phase{
		{Name: "plan", Kind: StageChat, State: plan, Round: round, Rounds: rl, Result: &StageResult{Done: done, Output: "files: a.go"}},
		{Name: ApproveName, Kind: StageGate, State: gate},
		{Name: "write", Kind: StageChat, State: PhasePending},
	}}}
}

// A plan's list speaks only for a finished plan OF ITS CURRENT ROUND. A step
// sent back keeps its old result and moves to a new round, so the old result
// is the rejected plan even though it finished (Done is true on it).
func TestAPlanSpeaksOnlyWhenItsCurrentRoundFinished(t *testing.T) {
	want := []string{"a.go"}
	for _, c := range []struct {
		name string
		it   Item
		want []string
	}{
		{"done, gate waiting", shaped(PhaseDone, 1, []int{1}, true, PhaseWaiting, 1, QKindApprove), want},
		{"legacy shape: plan waiting, gate pending", shaped(PhaseWaiting, 1, []int{1}, true, PhasePending, 0, QKindApprove), want},
		{"legacy shape, unfinished result", shaped(PhaseWaiting, 1, []int{1}, false, PhasePending, 0, QKindApprove), nil},
		{"legacy shape, out of attempts", shaped(PhaseWaiting, 1, []int{1}, true, PhasePending, 0, QKindOutOfAttempts), nil},
		{"mid-round ask on the same round", shaped(PhaseWaiting, 1, []int{1}, true, PhasePending, 0, "step"), nil},
		{"mid-round ask after a send-back", shaped(PhaseWaiting, 2, []int{1}, true, PhaseWaiting, 0, "step"), nil},
		{"misfiled approve after a send-back", shaped(PhaseWaiting, 2, []int{1}, true, PhasePending, 0, QKindApprove), nil},
		{"done reported with no new round", shaped(PhaseDone, 2, []int{1}, true, PhaseWaiting, 1, QKindApprove), nil},
		{"done in round 2", shaped(PhaseDone, 2, []int{1, 2}, true, PhaseWaiting, 1, QKindApprove), want},
		{"done, round lines trimmed to the newest", shaped(PhaseDone, 3, []int{2, 3}, true, PhaseWaiting, 1, QKindApprove), want},
		{"done, saved before round lines", shaped(PhaseDone, 0, nil, true, PhaseWaiting, 1, QKindApprove), want},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := PlanFilesOf(c.it); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
