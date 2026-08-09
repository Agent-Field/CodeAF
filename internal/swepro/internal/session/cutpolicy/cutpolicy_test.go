package cutpolicy

import (
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/capability"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

// Translation of src/session/cut-policy.test.ts — verbatim describe/test names
// as Go subtest paths, same assertion semantics. Nothing in cut-policy.ts
// throws, so there is no TS-throw-to-Go-error mapping to make here; the
// `if (d.kind !== "...") throw new Error("unreachable")` narrowing guards
// become plain kind assertions.

// ---------------------------------------------------------------------------
// Fixtures. `scope:*` tags pin a task to an exact band regardless of any other
// signal (see size-band.test.ts), so tests can name the band directly. The
// CapabilityTracker is used prior-only (no observations) unless a test feeds
// it — a HIGH-tier model is reliable through "l", a LOW-tier model through "s".
var (
	highA = capability.ModelRef{ProviderID: "openrouter", ModelID: "high-a"}
	highB = capability.ModelRef{ProviderID: "openrouter", ModelID: "high-b"}
	low   = capability.ModelRef{ProviderID: "openrouter", ModelID: "low-1"}
)

var tierMap = map[string]capability.ModelTierName{
	"high-a": capability.TierHigh,
	"high-b": capability.TierHigh,
	"low-1":  capability.TierLow,
}

func tracker() *capability.CapabilityTracker {
	return capability.NewCapabilityTracker(&capability.CapabilityOptions{TierMap: tierMap})
}

func repeatJoin(x string, n int, sep string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = x
	}
	return strings.Join(parts, sep)
}

func task(band string) CutTask {
	scope := map[string]string{"xs": "tiny", "s": "small", "m": "medium", "l": "large", "xl": "xlarge"}[band]
	// "xlarge" is not a recognized scope alias, so band "xl" is produced by
	// maxing every static signal instead (see the xl case below).
	if band == "xl" {
		fanIn := 50.0
		return CutTask{
			Description:     strings.Repeat("x", 5000) + strings.Repeat("\n", 60) + repeatJoin("- [ ] item", 20, "\n"),
			Tags:            []string{},
			DependencyFanIn: &fanIn,
		}
	}
	return CutTask{Description: "", Tags: []string{"scope:" + scope}}
}

func outcome(modelID string, band sizeband.SizeBand, verdict capability.LeafVerdict) capability.LeafOutcome {
	return capability.LeafOutcome{
		TaskID:        "t",
		Model:         &capability.LeafOutcomeModel{ProviderID: "openrouter", ModelID: modelID},
		SizeBand:      band,
		Verdict:       verdict,
		RepairRounds:  0,
		Turns:         5,
		ToolErrors:    0,
		CostUsd:       0.01,
		WallMs:        1000,
		MergeConflict: false,
		Timestamp:     0,
	}
}

func expectKind(t *testing.T, got CutDecision, want CutDecisionKind) {
	t.Helper()
	if got.Kind != want {
		t.Fatalf("kind = %q, want %q", got.Kind, want)
	}
}

func expectContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("%q does not contain %q", got, want)
	}
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------------

func TestDecideCutDispatchAsLeaf(t *testing.T) {
	t.Run("band within the assigned model's reliable band dispatches", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("m"), Model: highA, Tracker: tracker(), Alternatives: nil})
		expectKind(t, d, DispatchAsLeaf)
	})

	t.Run("band exactly equal to the reliable band dispatches", func(t *testing.T) {
		// HIGH is reliable through "l" on priors.
		d := DecideCut(DecideCutInput{Task: task("l"), Model: highA, Tracker: tracker(), Alternatives: []capability.ModelRef{highB}})
		expectKind(t, d, DispatchAsLeaf)
	})
}

func TestDecideCutXsFloor(t *testing.T) {
	t.Run("an xs task always dispatches, even for a weak model that covers nothing bigger", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("xs"), Model: low, Tracker: tracker(), Alternatives: []capability.ModelRef{highA}})
		expectKind(t, d, DispatchAsLeaf)
	})
}

func TestDecideCutEscalateModel(t *testing.T) {
	t.Run("weak assigned model, task above its band, a stronger alternative covers", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("l"), Model: low, Tracker: tracker(), Alternatives: []capability.ModelRef{highA}})
		expectKind(t, d, EscalateModel)
		if d.ToModelID != "high-a" {
			t.Fatalf("toModelID = %q, want %q", d.ToModelID, "high-a")
		}
		// Reason names both bands and the target.
		expectContains(t, d.Reason, "task=l")
		expectContains(t, d.Reason, "model-reliable=s")
		expectContains(t, d.Reason, "high-a")
		expectContains(t, d.Reason, "reliable=l")
	})

	t.Run("preference order: first covering alternative in the list wins", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("l"), Model: low, Tracker: tracker(), Alternatives: []capability.ModelRef{highA, highB}})
		expectKind(t, d, EscalateModel)
		if d.ToModelID != "high-a" {
			t.Fatalf("toModelID = %q, want %q", d.ToModelID, "high-a")
		}
	})

	t.Run("preference order respected when the list is reversed", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("l"), Model: low, Tracker: tracker(), Alternatives: []capability.ModelRef{highB, highA}})
		expectKind(t, d, EscalateModel)
		if d.ToModelID != "high-b" {
			t.Fatalf("toModelID = %q, want %q", d.ToModelID, "high-b")
		}
	})

	t.Run("the assigned model appearing among alternatives is skipped (no self-escalation)", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("l"), Model: low, Tracker: tracker(), Alternatives: []capability.ModelRef{low, highA}})
		expectKind(t, d, EscalateModel)
		if d.ToModelID != "high-a" {
			t.Fatalf("toModelID = %q, want %q", d.ToModelID, "high-a")
		}
	})
}

func TestDecideCutSplitFirst(t *testing.T) {
	t.Run("no alternative covers the band -> split", func(t *testing.T) {
		// task "l", only a LOW alternative that can't cover it.
		d := DecideCut(DecideCutInput{Task: task("l"), Model: low, Tracker: tracker(), Alternatives: []capability.ModelRef{low}})
		expectKind(t, d, SplitFirst)
		expectContains(t, d.Reason, "task=l")
		expectContains(t, d.Reason, "splitting")
	})

	t.Run("xl task nobody covers (even HIGH tops out at l on priors) -> split, never escalate", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("xl"), Model: highA, Tracker: tracker(), Alternatives: []capability.ModelRef{highB}})
		expectKind(t, d, SplitFirst)
		expectContains(t, d.Reason, "task=xl")
	})

	t.Run("no alternatives at all and assigned model can't cover -> split", func(t *testing.T) {
		d := DecideCut(DecideCutInput{Task: task("l"), Model: low, Tracker: tracker(), Alternatives: nil})
		expectKind(t, d, SplitFirst)
	})
}

func TestDecideCutThresholdOverride(t *testing.T) {
	t.Run("a stricter threshold shrinks the reliable band and flips dispatch -> split", func(t *testing.T) {
		// HIGH's prior mean at "l" is 0.75. Default threshold 0.7 -> reliable "l"
		// -> an "l" task dispatches. Threshold 0.8 -> "l" (0.75) drops below the
		// line, reliable falls to "m", and the "l" task can no longer be a leaf.
		dispatch := DecideCut(DecideCutInput{Task: task("l"), Model: highA, Tracker: tracker(), Alternatives: nil})
		expectKind(t, dispatch, DispatchAsLeaf)

		split := DecideCut(DecideCutInput{Task: task("l"), Model: highA, Tracker: tracker(), Alternatives: nil, Threshold: ptr(0.8)})
		expectKind(t, split, SplitFirst)
	})
}

func TestDecideCutLearnedEvidence(t *testing.T) {
	t.Run("after enough clean passes at 'xl', the model covers it and dispatches", func(t *testing.T) {
		tr := tracker()
		// Feed clean passes at xl until HIGH-A's reliable band reaches xl.
		for i := 0; i < 10; i++ {
			tr.Observe(outcome("high-a", sizeband.BandXL, capability.VerdictPass))
		}
		d := DecideCut(DecideCutInput{Task: task("xl"), Model: highA, Tracker: tr, Alternatives: nil})
		expectKind(t, d, DispatchAsLeaf)
	})
}

// ---------------------------------------------------------------------------
// T5 pure helpers.

func TestBandToIndex(t *testing.T) {
	t.Run("maps the band ordering to 0..4", func(t *testing.T) {
		got := []float64{}
		for _, b := range []sizeband.SizeBand{"xs", "s", "m", "l", "xl"} {
			got = append(got, BandToIndex(b))
		}
		want := []float64{0, 1, 2, 3, 4}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("bandToIndex map = %v, want %v", got, want)
			}
		}
	})
}

func TestBandWithinReliable(t *testing.T) {
	t.Run("true when the task band is at or below the reliable band", func(t *testing.T) {
		if !BandWithinReliable("xs", "l") {
			t.Fatal("xs within l should be true")
		}
		if !BandWithinReliable("m", "l") {
			t.Fatal("m within l should be true")
		}
		if !BandWithinReliable("l", "l") { // equal ⇒ covered
			t.Fatal("l within l should be true")
		}
	})

	t.Run("false when the task band exceeds the reliable band", func(t *testing.T) {
		if BandWithinReliable("xl", "l") {
			t.Fatal("xl within l should be false")
		}
		if BandWithinReliable("m", "s") {
			t.Fatal("m within s should be false")
		}
	})

	t.Run("matches the HIGH-prior reliable band a fresh run compares against", func(t *testing.T) {
		// HIGH tier is reliable through "l" on priors — so an "l" whole-task band
		// root-cuts, an "xl" does not.
		reliable := tracker().MaxReliableBand(highA) // "l"
		if !BandWithinReliable("l", reliable) {
			t.Fatal("l within the HIGH prior reliable band should be true")
		}
		if BandWithinReliable("xl", reliable) {
			t.Fatal("xl within the HIGH prior reliable band should be false")
		}
	})
}

func TestShouldRootCut(t *testing.T) {
	// reliable = "l" mirrors a HIGH model's cold-start reliable band.
	t.Run("small/medium tasks root-cut exactly as bandWithinReliable (byte-identical default path)", func(t *testing.T) {
		for _, band := range []sizeband.SizeBand{"xs", "s", "m"} {
			if got, want := ShouldRootCut(ShouldRootCutInput{Band: band, Reliable: "l", HardMode: false}), BandWithinReliable(band, "l"); got != want {
				t.Fatalf("shouldRootCut(%s) = %v, want %v", band, got, want)
			}
			if !ShouldRootCut(ShouldRootCutInput{Band: band, Reliable: "l", HardMode: false}) {
				t.Fatalf("shouldRootCut(%s) should be true", band)
			}
		}
	})

	t.Run("a LARGE band (l) DECOMPOSES even though bandWithinReliable(l, l) is true — the P4 fix", func(t *testing.T) {
		if !BandWithinReliable("l", "l") { // old gate would root-cut
			t.Fatal("bandWithinReliable(l, l) should be true")
		}
		if ShouldRootCut(ShouldRootCutInput{Band: "l", Reliable: "l", HardMode: false}) {
			t.Fatal("shouldRootCut(l) should be false")
		}
		if ShouldRootCut(ShouldRootCutInput{Band: "xl", Reliable: "l", HardMode: false}) {
			t.Fatal("shouldRootCut(xl) should be false")
		}
	})

	t.Run("--hard decomposes a non-trivial (>= m) task but leaves a trivial one a single leaf", func(t *testing.T) {
		if ShouldRootCut(ShouldRootCutInput{Band: "m", Reliable: "l", HardMode: true}) { // hard + m → decompose
			t.Fatal("hard + m should be false")
		}
		if !ShouldRootCut(ShouldRootCutInput{Band: "s", Reliable: "l", HardMode: true}) { // hard + s → still one leaf
			t.Fatal("hard + s should be true")
		}
		if !ShouldRootCut(ShouldRootCutInput{Band: "xs", Reliable: "l", HardMode: true}) {
			t.Fatal("hard + xs should be true")
		}
	})

	t.Run("without --hard a medium task is unaffected by hard-mode logic", func(t *testing.T) {
		if !ShouldRootCut(ShouldRootCutInput{Band: "m", Reliable: "l", HardMode: false}) {
			t.Fatal("m without hard should be true")
		}
	})
}

func TestPickProbeLeaf(t *testing.T) {
	type leaf struct {
		id   string
		band sizeband.SizeBand
		real bool
	}
	bandOf := func(l leaf) float64 { return BandToIndex(l.band) }
	isReal := func(l leaf) bool { return l.real }

	pickedID := func(leaves []leaf) (string, bool) {
		best, ok := PickProbeLeaf(leaves, bandOf, isReal)
		if !ok {
			return "", false
		}
		return best.id, true
	}

	t.Run("picks the lowest-band real-impl leaf", func(t *testing.T) {
		leaves := []leaf{
			{id: "a", band: "l", real: true},
			{id: "b", band: "s", real: true},
			{id: "c", band: "m", real: true},
		}
		if id, ok := pickedID(leaves); !ok || id != "b" {
			t.Fatalf("picked %q (%v), want b", id, ok)
		}
	})

	t.Run("skips research/explore (non-real) leaves even when they are smaller", func(t *testing.T) {
		leaves := []leaf{
			{id: "explore", band: "xs", real: false},
			{id: "impl", band: "m", real: true},
		}
		if id, ok := pickedID(leaves); !ok || id != "impl" {
			t.Fatalf("picked %q (%v), want impl", id, ok)
		}
	})

	t.Run("stable on ties — the earlier leaf in the given order wins", func(t *testing.T) {
		leaves := []leaf{
			{id: "first", band: "s", real: true},
			{id: "second", band: "s", real: true},
		}
		if id, ok := pickedID(leaves); !ok || id != "first" {
			t.Fatalf("picked %q (%v), want first", id, ok)
		}
	})

	t.Run("returns undefined when no candidate is real impl work", func(t *testing.T) {
		leaves := []leaf{
			{id: "explore-1", band: "xs", real: false},
			{id: "explore-2", band: "s", real: false},
		}
		if _, ok := pickedID(leaves); ok {
			t.Fatal("expected undefined")
		}
	})

	t.Run("returns undefined on an empty candidate set", func(t *testing.T) {
		if _, ok := pickedID([]leaf{}); ok {
			t.Fatal("expected undefined")
		}
	})
}

// ---------------------------------------------------------------------------

func TestSelectCoalesceGroup(t *testing.T) {
	// Reliable band index m=2 for "model-A": leaves smaller than m are eligible.
	reliable := func(modelID string) float64 {
		if modelID == "model-A" {
			return BandToIndex("m")
		}
		return BandToIndex("s")
	}
	cand := func(id string, parent *string, band sizeband.SizeBand, fileScope []string, modelID ...string) CoalesceCandidate {
		m := "model-A"
		if len(modelID) > 0 {
			m = modelID[0]
		}
		return CoalesceCandidate{
			ID:          id,
			Parent:      parent,
			ModelID:     m,
			BandIndex:   jscompat.JSNumber(BandToIndex(band)),
			FileScope:   fileScope,
			Description: "",
		}
	}
	// A combined-band estimator that just sums member points so growth is visible.
	summing := func(g []CoalesceCandidate) float64 {
		s := 0.0
		for _, c := range g {
			s += float64(c.BandIndex)
		}
		if x := BandToIndex("xl"); x < s {
			return x
		}
		return s
	}
	p := ptr("p")
	sortedIDs := func(g []CoalesceCandidate) []string {
		ids := make([]string, 0, len(g))
		for _, c := range g {
			ids = append(ids, c.ID)
		}
		sort.Strings(ids)
		return ids
	}
	equalIDs := func(t *testing.T, g []CoalesceCandidate, want ...string) {
		t.Helper()
		got := sortedIDs(g)
		if len(got) != len(want) {
			t.Fatalf("ids = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("ids = %v, want %v", got, want)
			}
		}
	}

	t.Run("merges two small compatible siblings under the same parent", func(t *testing.T) {
		g := SelectCoalesceGroup(
			[]CoalesceCandidate{cand("a", p, "xs", []string{"src/x.ts"}), cand("b", p, "xs", []string{"src/x.ts"})},
			reliable, nil)
		equalIDs(t, g, "a", "b")
	})

	t.Run("does not coalesce leaves already at/above the model's reliable band", func(t *testing.T) {
		// Both at "m" == reliable(m); neither is "too small".
		g := SelectCoalesceGroup(
			[]CoalesceCandidate{cand("a", p, "m", []string{}), cand("b", p, "m", []string{})},
			reliable, nil)
		if g != nil {
			t.Fatalf("expected undefined, got %v", sortedIDs(g))
		}
	})

	t.Run("never coalesces across parents", func(t *testing.T) {
		g := SelectCoalesceGroup(
			[]CoalesceCandidate{cand("a", ptr("p1"), "xs", []string{"src/x.ts"}), cand("b", ptr("p2"), "xs", []string{"src/x.ts"})},
			reliable, nil)
		if g != nil {
			t.Fatalf("expected undefined, got %v", sortedIDs(g))
		}
	})

	t.Run("never coalesces across models (different reliable bands)", func(t *testing.T) {
		g := SelectCoalesceGroup(
			[]CoalesceCandidate{cand("a", p, "xs", []string{"src/x.ts"}, "model-A"), cand("b", p, "xs", []string{"src/x.ts"}, "model-B")},
			reliable, nil)
		if g != nil {
			t.Fatalf("expected undefined, got %v", sortedIDs(g))
		}
	})

	t.Run("excludes file_scope-incompatible siblings (disjoint scopes)", func(t *testing.T) {
		// a & b disjoint; a & c share x.ts. Seed is smallest band; only compatible
		// members join. With three xs leaves, a+c merge, b is left out.
		g := SelectCoalesceGroup(
			[]CoalesceCandidate{
				cand("a", p, "xs", []string{"src/x.ts"}),
				cand("b", p, "xs", []string{"src/z.ts"}),
				cand("c", p, "xs", []string{"src/x.ts"}),
			},
			reliable, nil)
		equalIDs(t, g, "a", "c")
	})

	t.Run("empty file_scope is a wildcard — compatible with anything", func(t *testing.T) {
		g := SelectCoalesceGroup(
			[]CoalesceCandidate{cand("a", p, "xs", []string{}), cand("b", p, "xs", []string{"src/anything.ts"})},
			reliable, nil)
		equalIDs(t, g, "a", "b")
	})

	t.Run("stops adding members before the combined band exceeds the reliable band", func(t *testing.T) {
		// Three "s" (index 1) leaves; summing estimator: 1+1=2 (==m, ok), 1+1+1=3 (>m, reject).
		// So only two of the three are taken.
		g := SelectCoalesceGroup(
			[]CoalesceCandidate{
				cand("a", p, "s", []string{"src/x.ts"}),
				cand("b", p, "s", []string{"src/x.ts"}),
				cand("c", p, "s", []string{"src/x.ts"}),
			},
			reliable, &SelectCoalesceGroupOptions{CombinedBandIndex: summing})
		if len(g) != 2 {
			t.Fatalf("group length = %d, want 2", len(g))
		}
	})

	t.Run("returns undefined when only one eligible small sibling exists", func(t *testing.T) {
		g := SelectCoalesceGroup([]CoalesceCandidate{cand("a", p, "xs", []string{"src/x.ts"})}, reliable, nil)
		if g != nil {
			t.Fatalf("expected undefined, got %v", sortedIDs(g))
		}
	})
}

func TestSplitForConcurrency(t *testing.T) {
	baseTask := func() ConcurrencyCutTask {
		return ConcurrencyCutTask{
			CutTask: CutTask{
				Description: "Implement the independent file changes.\nfile_scope: src/a.ts, src/b.ts",
				Tags:        []string{},
			},
			FileScope: []string{"src/a.ts", "src/b.ts"},
		}
	}
	base := func() SplitForConcurrencyInput {
		return SplitForConcurrencyInput{Task: baseTask(), IdleSlots: 2, ReliableBand: "m", ActiveProbeWave: false}
	}
	equalGroups := func(t *testing.T, got [][]string, want [][]string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("groups = %v, want %v", got, want)
		}
		for i := range want {
			if len(got[i]) != len(want[i]) {
				t.Fatalf("groups = %v, want %v", got, want)
			}
			for j := range want[i] {
				if got[i][j] != want[i][j] {
					t.Fatalf("groups = %v, want %v", got, want)
				}
			}
		}
	}

	t.Run("partitions exact disjoint files into mechanical singleton groups", func(t *testing.T) {
		equalGroups(t, PartitionFileScope([]string{"src/a.ts", "src/b.ts"}), [][]string{{"src/a.ts"}, {"src/b.ts"}})
		equalGroups(t, SplitForConcurrency(base()), [][]string{{"src/a.ts"}, {"src/b.ts"}})
	})

	t.Run("requires two idle slots and no active probe wave", func(t *testing.T) {
		in := base()
		in.IdleSlots = 1
		if g := SplitForConcurrency(in); g != nil {
			t.Fatalf("expected undefined, got %v", g)
		}
		in = base()
		in.ActiveProbeWave = true
		if g := SplitForConcurrency(in); g != nil {
			t.Fatalf("expected undefined, got %v", g)
		}
	})

	t.Run("requires provably disjoint exact paths", func(t *testing.T) {
		if g := PartitionFileScope([]string{"src/a.ts", "src/a.ts"}); g != nil {
			t.Fatalf("expected undefined, got %v", g)
		}
		if g := PartitionFileScope([]string{"src", "src/a.ts"}); g != nil {
			t.Fatalf("expected undefined, got %v", g)
		}
		if g := PartitionFileScope([]string{"src/*.ts", "test/*.ts"}); g != nil {
			t.Fatalf("expected undefined, got %v", g)
		}
	})

	t.Run("rejects a split when any narrowed part still exceeds reliability", func(t *testing.T) {
		in := base()
		in.Task.Description = strings.Repeat("x", 1400) + "\nfile_scope: src/a.ts, src/b.ts"
		in.ReliableBand = "s"
		if g := SplitForConcurrency(in); g != nil {
			t.Fatalf("expected undefined, got %v", g)
		}
	})
}
