package factory

import "time"

// Fixture is a small, still floor for drawing and tests: three repos, a dozen
// items in every state, a handover with something in it. It never changes
// under a frame, which is what a golden test wants. The mock seam
// (internal/factory/mock) is the one that moves.
func Fixture(now time.Time) Snapshot {
	codeaf := Repo{Name: "agentfield/codeaf", Team: "codeaf-core", Areas: []string{"tui", "standing", "spend"},
		Habits: []string{"every PR on dev gets a review pass", "issues labelled factory are briefs"},
		Recipe: Recipe{
			Stages: []Stage{
				{Name: "plan", Ask: "read the issue and say how", Until: "done", Gate: GatePlan, On: true},
				{Name: "write", Ask: "do it in a worktree", Fanout: "per-file", Until: "done", On: true},
				{Name: "test", Ask: "run what the change implies", Until: "green", Max: 2, On: true},
				{Name: "review", Ask: "read it as a stranger would", Until: "clean", Max: 2, Fanout: "per-finding", On: true},
				{Name: "neaten", Ask: "make the code neater, same behaviour", Until: "done", On: true},
				{Name: "proof", Ask: "show each claim in its own medium", Until: "proven", Gate: GateShip, On: true},
			},
			Policy: []string{"complexity within +10% of main", "no new dependencies without asking"},
		}}
	platform := Repo{Name: "agentfield/agentfield", Team: "platform", Areas: []string{"api", "auth", "billing"},
		Habits: []string{"review every PR, comment-only"},
		Recipe: Recipe{Stages: []Stage{
			{Name: "read", Ask: "the diff and its claims", On: true},
			{Name: "checks", Ask: "run what the claims imply", Fanout: "per-claim", On: true},
			{Name: "review", Ask: "findings as a comment", Until: "clean", Max: 1, Gate: GateShip, On: true},
		}, Policy: []string{"migrations reversible"}}, Hue: 1}
	whisper := Repo{Name: "santoshkumar/whisper", Areas: []string{"messages", "sync"}, CIRed: true, Hue: 2}

	h := func(n int) time.Time { return now.Add(-time.Duration(n) * time.Hour) }
	m := func(n int) time.Time { return now.Add(-time.Duration(n) * time.Minute) }
	stages := func(r Repo) []Stage { return append([]Stage{}, r.Recipe.Stages...) }

	running := &Stream{Started: m(26), Spent: 1.42, Bench: 1, Activity: []int{1, 2, 5, 3, 6, 4, 2, 5},
		Phases: []Phase{{Name: "plan", State: PhaseDone}, {Name: "write", State: PhaseDone, Tasks: 3}, {Name: "test", State: PhaseDone}, {Name: "review", State: PhaseRunning, Round: 1, Left: 4 * time.Minute, Note: "3 findings"}, {Name: "neaten", State: PhasePending}, {Name: "proof", State: PhasePending}},
		Cur:    3,
		Log: []LogLine{
			{At: m(26), Glyph: "✳", Tone: "thought", Text: "reading #1551 · the filter is read before the tree exists; ordering, not state"},
			{At: m(21), Glyph: "»", Tone: "said", Text: "plan: one measure at the seam, a test that pins the width, no new state"},
			{At: m(19), Glyph: "✎", Tone: "write", Text: "editing internal/tui3/tree.go · 3 tasks in parallel"},
			{At: m(9), Glyph: "◎", Tone: "test", Text: "go test ./internal/tui3/ · 12/12"},
			{At: m(4), Glyph: "»", Tone: "said", Text: "review 1/2: 3 findings · fixing"},
		}}
	waiting := &Stream{Started: h(7), Spent: 0.88, Bench: 2, Activity: []int{3, 4, 2, 0, 0, 0, 0, 0}, Cur: 0,
		Phases: []Phase{{Name: "plan", State: PhaseWaiting}, {Name: "write", State: PhasePending}, {Name: "test", State: PhasePending}, {Name: "review", State: PhasePending}, {Name: "neaten", State: PhasePending}, {Name: "proof", State: PhasePending}},
		Log:    []LogLine{{At: h(7), Glyph: "»", Tone: "said", Text: "plan: queue the writes behind one owner; three files; the migration is reversible"}, {At: h(7), Glyph: "?", Tone: "ask", Text: "plan is ready · go, or change it?"}}}
	landed := &Stream{Started: h(3), Ended: h(1), Spent: 2.87, Bench: 3, Activity: []int{0, 0, 0, 0, 0, 0, 0, 0}, Cur: 5,
		Phases: []Phase{{Name: "plan", State: PhaseDone}, {Name: "write", State: PhaseDone, Tasks: 2}, {Name: "test", State: PhaseDone}, {Name: "review", State: PhaseDone, Round: 2}, {Name: "neaten", State: PhaseDone}, {Name: "proof", State: PhaseDone}},
		Log:    []LogLine{{At: h(1), Glyph: "»", Tone: "said", Text: "landed · proof sheet ready · your sign-off"}}}
	// THE SHIPPED ITEM IS THE ONE PLAN ADAPTED: it added a security pass after
	// review and skipped neaten, and its stream ran the stages it was left
	// with, so the record under its stages line has something true to say.
	shipped := &Stream{Started: h(9), Ended: h(6), Spent: 1.9, Bench: 4, Activity: make([]int, 8), Cur: 5,
		Phases: []Phase{{Name: "plan", State: PhaseDone}, {Name: "write", State: PhaseDone}, {Name: "test", State: PhaseDone}, {Name: "review", State: PhaseDone}, {Name: "security", State: PhaseDone}, {Name: "proof", State: PhaseDone}}}
	adapted := func(r Repo) []Stage {
		out := PlaceStage(stages(r), Stage{Name: "security", Kind: StageChat, Ask: "secrets, injection and authz", Until: "clean", On: true}, "after review")
		out[StageIndex(out, "neaten")].On = false
		return out
	}

	items := []Item{
		{ID: 1, Repo: codeaf.Name, Num: 1538, URL: "https://github.com/agentfield/codeaf/issues/1538", Kind: KindIssue, Title: "budget caps per task", Author: "santosh", Tier: TierOwner, Origin: OriginForge, Created: h(9), Changed: h(7), State: StateNeedsYou, Stream: waiting, Question: "plan is ready · go, or change it?", QKind: "plan", Cap: 8, Gate: GatePlan, Stages: stages(codeaf),
			Triage: Triage{Type: "feat", Size: "L", Area: "spend", Readiness: 88, Est: 9, Risk: []string{"touches money"}, Read: "touches three packages; wants a plan first", Priority: 1, Reason: "money at risk"}},
		{ID: 2, Repo: codeaf.Name, Num: 1551, URL: "https://github.com/agentfield/codeaf/issues/1551", Kind: KindIssue, Title: "filters lost on compact", Author: "priya", Tier: TierCollab, Origin: OriginForge, Created: h(5), Changed: m(4), State: StateRunning, Stream: running, Cap: 5, Gate: GateShip, Stages: stages(codeaf),
			Triage: Triage{Type: "bug", Size: "M", Area: "tui", Readiness: 72, Est: 3, Read: "the filter is read before the tree exists; ordering, not state", Priority: 2}},
		{ID: 3, Repo: codeaf.Name, Num: 1660, URL: "https://github.com/agentfield/codeaf/issues/1660", Kind: KindIssue, Title: "standing order ignores a paste with newlines", Author: "abir", Tier: TierCollab, Origin: OriginForge, Created: h(4), Changed: m(30), State: StateQueued, Cap: 5, Gate: GateShip, Stages: stages(codeaf),
			Triage: Triage{Type: "bug", Size: "S", Area: "standing", Readiness: 80, Est: 2, Read: "probably the paste path; a repro should take ten minutes", Priority: 3}},
		{ID: 4, Repo: codeaf.Name, Num: 1662, URL: "https://github.com/agentfield/codeaf/pull/1662", Kind: KindPR, Title: "fix(media): tree rails on narrow widths", Author: "priya", Tier: TierCollab, Origin: OriginForge, Created: h(1), Changed: h(1), State: StateNew, Cap: 3, Gate: GateShip, Stages: stages(platform), Checks: "ci ✓", Diff: "+218 −44 · 6 files",
			Body:      "## Claims\n\n- rails follow the tree on narrow widths\n- no behaviour change on wide\n- no perf impact\n\nThe fix is in `media.go`: the rail width is read from `treeCols` after the resize, not before.",
			Files:     []FileChange{{Path: "internal/tui3/media.go", Added: 120, Removed: 30}, {Path: "internal/tui3/media_test.go", Added: 74}, {Path: "internal/tui3/tree.go", Added: 12, Removed: 9}, {Path: "internal/tui3/rail.go", Added: 8, Removed: 3}, {Path: "internal/tui3/layout.go", Added: 3, Removed: 2}, {Path: "docs/changes/unreleased/1662.md", Added: 1}},
			CheckRuns: []CheckRun{{Name: "build", State: "success"}, {Name: "touched packages", State: "success"}, {Name: "laws", State: "pending"}},
			Comments:  []Comment{{Author: "abir", Body: "Does this hold at **80** columns too? The `rail.go` change looks like it only covers 100.", At: h(1)}, {Author: "priya", Body: "Yes: the test walks 60, 80 and 100.", At: m(40)}},
			Activity:  []Event{{At: h(1), What: "opened by priya"}, {At: m(50), What: "labelled factory"}, {At: m(40), What: "review requested from abir"}},
			Triage:    Triage{Type: "review", Size: "M", Area: "tui", Readiness: 90, Est: 2, Read: "claims are testable; three checks cover them", Priority: 2}},
		{ID: 5, Repo: whisper.Name, Num: 0, Kind: KindCI, Title: "main is red · sync · TestCompactKeepsFilters", Author: "ci", Tier: TierOwner, Origin: OriginForge, Created: h(6), Changed: h(6), State: StateNew, Cap: 3, Gate: GateShip,
			Body:   "The last three runs on main failed in the same test. Started after the merge at 02:14.",
			Triage: Triage{Type: "ci", Size: "S", Area: "sync", Readiness: 95, Est: 1.5, Read: "same test, three runs in a row; a regression, not a flake", Priority: 1, Reason: "main is red"}},
		{ID: 6, Repo: whisper.Name, Num: 31, URL: "https://github.com/santoshkumar/whisper/issues/31", Kind: KindIssue, Title: "messages drops replies after a slow network", Author: "olu", Tier: TierStranger, Origin: OriginForge, Created: h(4), Changed: h(4), State: StateNew, Cap: 5, Gate: GateShip,
			Body:     "Seen twice on a train. No repro yet.",
			Comments: []Comment{{Author: "olu", Body: "Third time today, on the *same* train. Replies sent while the tunnel drops never show up.", At: h(3)}, {Author: "santosh", Body: "Which build? `codeaf --version` prints it.", At: h(2)}, {Author: "olu", Body: "0.9.4, from the script install.", At: m(90)}},
			Activity: []Event{{At: h(4), What: "opened by olu"}, {At: h(2), What: "commented by santosh"}},
			Triage:   Triage{Type: "bug", Size: "M", Area: "messages", Readiness: 40, Est: 4, Read: "underspecified; two questions for the author first", Questions: []string{"which network, and how slow?", "does it happen on a fresh install too?"}, Priority: 4}},
		{ID: 7, Repo: platform.Name, Num: 702, Kind: KindIssue, Title: "split from chat: spend ledger store, the write half", Author: "santosh", Tier: TierOwner, Origin: OriginChat, Created: h(2), Changed: h(2), State: StateNew, Cap: 8, Gate: GateShip, Stages: stages(codeaf),
			Triage: Triage{Type: "feat", Size: "M", Area: "billing", Readiness: 90, Est: 6, Read: "your own chat wrote the brief; sized from it"}},
		{ID: 8, Repo: codeaf.Name, Num: 1540, Kind: KindIssue, Title: "meter crashes at midnight rollover", Author: "santosh", Tier: TierOwner, Origin: OriginTerminal, Synced: false, Created: m(12), Changed: m(12), State: StateNew, Cap: 5, Gate: GateShip, Stages: stages(codeaf),
			Body:   "Written in the terminal. Not on GitHub yet.",
			Triage: Triage{Type: "bug", Size: "S", Area: "tui", Readiness: 75, Est: 2, Read: "your own words; no triage needed beyond sizing", Priority: 3}},
		{ID: 9, Repo: codeaf.Name, Num: 1661, URL: "https://github.com/agentfield/codeaf/issues/1661", Kind: KindIssue, Title: "probes fire once, then retire", Author: "santosh", Tier: TierOwner, Origin: OriginForge, Created: h(3), Changed: h(1), State: StateLanded, Stream: landed, Cap: 5, Gate: GateShip, Stages: stages(codeaf), Diff: "+218 −44",
			Proof: []Claim{
				{Text: "fires on first true, never again", OK: true, Evidence: "test · 0.3s", Medium: "test"},
				{Text: "two firings scheduled, one delivered", OK: true, Evidence: "test · 1.8s", Medium: "test"},
				{Text: "retired probe leaves no row on home", OK: true, Evidence: "screenshot", Medium: "screenshot"},
				{Text: "survives a codeaf restart", OK: false, Evidence: "no check covers it", Medium: ""},
			},
			Policy: []Claim{{Text: "complexity within +10% of main", OK: true, Evidence: "complexity +3%", Medium: "policy"}, {Text: "no new dependencies without asking", OK: true, Evidence: "go.mod unchanged", Medium: "policy"}},
			Triage: Triage{Type: "bug", Size: "M", Area: "standing", Readiness: 85, Est: 3, Priority: 2}},
		{ID: 10, Repo: codeaf.Name, Num: 1663, URL: "https://github.com/agentfield/codeaf/issues/1663", Kind: KindIssue, Title: "spend row shows stale after compact", Author: "mateo", Tier: TierCollab, Origin: OriginForge, Created: h(9), Changed: h(6), State: StateShipped, Stream: shipped, Cap: 5, Gate: GateNone, Stages: adapted(codeaf),
			Adapted: []string{"plan added security", "plan skipped neaten", "why: touches the billing cache"},
			Triage:  Triage{Type: "bug", Size: "S", Area: "spend", Readiness: 80, Est: 2}},
	}
	var hours [24]int
	hours[(now.Hour()+24-6)%24] = 3
	hours[(now.Hour()+24-4)%24] = 5
	hours[(now.Hour()+24-1)%24] = 2
	return Snapshot{
		Now:     now,
		Repos:   []Repo{codeaf, platform, whisper},
		Items:   items,
		Benches: 6,
		Daily:   11.31,
		Rail:    60,
		Shift:   Shift{Since: h(8), Shipped: 1, Arrived: 4, Asked: 1, Handled: 0, Spent: 8.44, Hours: hours, Shipping: []string{"#1663"}},
		Sources: []SourceInfo{{Name: "github", Writes: true, Polled: now.Add(-14 * time.Second)}, {Name: string(OriginChat)}},
	}
}

// FixtureSeam is a seam whose one door is a read of [Fixture], taken at now.
// Every verb is nil, so the surface draws no key for any of them: a still
// floor that cannot be changed is shown as exactly that, and nothing on the
// page promises an act the fixture cannot perform.
//
// It is what `CODEAF_FACTORY_FIXTURE=1` hands the surface while no real
// engine stands behind the factory page.
func FixtureSeam(now time.Time) Seam {
	return Seam{Load: func() (Snapshot, error) { return Fixture(now), nil }}
}
