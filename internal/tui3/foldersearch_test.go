package tui3

// WHAT THE FOLDER SCORER OWES A PERSON WHO IS TYPING.
//
// Each test here is one sentence from the design brief turned into an
// assertion, and they are written against the ORDER OF THE LIST rather than
// against the numbers inside a hit — a rung constant is arithmetic somebody may
// rebalance, and "the folder I meant is on the first row" is the promise.

import (
	"strings"
	"testing"
)

// folderPaths is the standing fixture: a handful of directories shaped like the
// ones people actually have, including two that share a leaf name, one that is
// not a repository, one with a space in it and one with an accent.
var folderPaths = []string{
	"~/code/aforge-v2",
	"~/code/aforge-v2/internal/tui3",
	"~/code/aforge-v2/internal/session",
	"~/code/agentfield-site",
	"~/work/client/tui3",
	"~/Documents/tax returns",
	"~/Documents/résumés",
	"~/scratch/notes",
}

// rankOf ranks the standing fixture against one query with every candidate on
// the same layer and none of them ever picked, so the ladder alone decides.
func rankOf(t *testing.T, query string, paths ...string) []string {
	t.Helper()
	if len(paths) == 0 {
		paths = folderPaths
	}
	cands := make([]folderRankee, 0, len(paths))
	for at, path := range paths {
		cands = append(cands, folderRankee{Show: path, Rank: at})
	}
	var ranker folderRanker
	ranker.load(cands)
	out := make([]string, 0, len(paths))
	for _, at := range ranker.rank(query) {
		out = append(out, cands[at].Show)
	}
	return out
}

func TestTheLeafNameIsWhatAPersonIsThinkingOf(t *testing.T) {
	// `tui3` names two directories and nothing else on this disk, and both of
	// them lead — the folder whose whole name it is comes before every path
	// that merely contains the letters.
	got := rankOf(t, "tui3")
	if len(got) < 2 {
		t.Fatalf("tui3 reached %d folders, wanted at least the two named tui3: %v", len(got), got)
	}
	for _, want := range got[:2] {
		if !strings.HasSuffix(want, "/tui3") {
			t.Fatalf("tui3 put %q on the first two rows: %v", want, got)
		}
	}
}

func TestASegmentAboveTheLeafFindsWhatIsUnderIt(t *testing.T) {
	got := rankOf(t, "internal")
	if len(got) != 2 {
		t.Fatalf("internal reached %v, wanted the two folders under one: %v", len(got), got)
	}
	for _, path := range got {
		if !strings.Contains(path, "/internal/") {
			t.Fatalf("internal reached %q, which is not under one", path)
		}
	}
	// AND A RUN OF SEGMENTS IS THE SAME GESTURE, narrowed. Somebody who types
	// the shape of the path means exactly the one folder at the end of it.
	run := rankOf(t, "internal/tui3")
	if len(run) != 1 || run[0] != "~/code/aforge-v2/internal/tui3" {
		t.Fatalf("internal/tui3 reached %v, wanted only the one folder", run)
	}
}

func TestTheDeeperOfTwoSegmentsWithOneNameWins(t *testing.T) {
	// A path that names the same word twice is answered with the one nearer
	// the thing being pointed at.
	got := rankOf(t, "aforge", "~/code/aforge/vendor-copy/aforge/inner", "~/mirrors/aforge/one")
	if len(got) != 2 {
		t.Fatalf("aforge reached %v, wanted both: %v", len(got), got)
	}
	// Both are segment matches; the tie is broken by how far the matched
	// segment sat above the leaf, and `vendor-copy/aforge/inner` matched one
	// level up while `mirrors/aforge/one` matched one level up too — so this
	// asserts the scorer picked the DEEPER `aforge` inside the first path
	// rather than its first one.
	hit, ok := folderScoreOf("aforge", "~/code/aforge/vendor-copy/aforge/inner")
	if !ok || hit.tier != folderTierSegment {
		t.Fatalf("aforge scored the doubled path as tier %v (matched %v)", hit.tier, ok)
	}
	shallow, _ := folderScoreOf("aforge", "~/code/aforge/one/two/three/inner")
	if !(hit.detail < shallow.detail) {
		t.Fatalf("the deeper aforge scored %d, no better than the shallow one at %d", hit.detail, shallow.detail)
	}
}

func TestTheInitialsFindAFolderNobodySpelledOut(t *testing.T) {
	hit, ok := folderScoreOf("av2", "~/code/aforge-v2")
	if !ok || hit.tier != folderTierShort {
		t.Fatalf("av2 scored ~/code/aforge-v2 as tier %v (matched %v), wanted the initials", hit.tier, ok)
	}
	// The same letters read across a whole path are the same rung, one step
	// behind the leaf's own initials.
	across, ok := folderScoreOf("ait", "~/code/aforge-v2/internal/tui3")
	if !ok || across.tier != folderTierShort {
		t.Fatalf("ait scored the deep path as tier %v (matched %v)", across.tier, ok)
	}
	if !(hit.detail < across.detail) {
		t.Fatalf("the leaf's own initials scored %d and a whole-path reading %d", hit.detail, across.detail)
	}
	// AND LETTERS THAT MERELY APPEAR IN ORDER ARE NOT INITIALS. `ag2` is the
	// loose rung, which is what keeps the initials rung meaning something.
	loose, ok := folderScoreOf("ag2", "~/code/aforge-v2")
	if !ok || loose.tier != folderTierLoose {
		t.Fatalf("ag2 scored ~/code/aforge-v2 as tier %v (matched %v), wanted the loose rung", loose.tier, ok)
	}
}

func TestTheInitialsReachPastAFalseStart(t *testing.T) {
	// A greedy pass anchored on the first matching letter answers this one
	// wrongly: it takes the `a` of `ab`, finds no `c` after it and gives up on
	// a name that abbreviates perfectly from its second word.
	if _, ok := folderShort("ab-ac", "ac"); !ok {
		t.Fatal("ac did not abbreviate ab-ac")
	}
}

func TestATransposedPairStillFindsTheFolder(t *testing.T) {
	// THE TYPO THE BRIEF NAMES. Every rung above the last one answers `afroge`
	// with nothing at all, and a list that says "no folder matches" to a query
	// one swapped pair of letters from the folder in front of somebody is the
	// defect this rung exists for.
	got := rankOf(t, "afroge")
	if len(got) == 0 || !strings.Contains(got[0], "aforge-v2") {
		t.Fatalf("afroge reached %v, wanted ~/code/aforge-v2 first", got)
	}
	hit, ok := folderScoreOf("afroge", "~/code/aforge-v2")
	if !ok || hit.tier != folderTierSlip {
		t.Fatalf("afroge scored ~/code/aforge-v2 as tier %v (matched %v), wanted the slip rung", hit.tier, ok)
	}
	// And a whole name mistyped is nearer than the start of a longer one.
	whole, ok := folderScoreOf("afroge", "~/code/aforge")
	if !ok || whole.tier != folderTierSlip {
		t.Fatalf("afroge scored ~/code/aforge as tier %v (matched %v)", whole.tier, ok)
	}
	if !(whole.detail < hit.detail) {
		t.Fatalf("the whole mistyped name scored %d, no better than the mistyped lead at %d", whole.detail, hit.detail)
	}
}

func TestASlipIsTheLastThingTried(t *testing.T) {
	// A folder that genuinely contains what was typed comes before one that is
	// a slip away from it, however used the second one is.
	cands := []folderRankee{
		{Show: "~/old/aforge", Freq: 40, Rank: 0},
		{Show: "~/code/afroge-notes", Rank: 1},
	}
	var ranker folderRanker
	ranker.load(cands)
	hits := ranker.rank("afroge")
	if len(hits) != 2 {
		t.Fatalf("afroge reached %d of the two folders", len(hits))
	}
	if cands[hits[0]].Show != "~/code/afroge-notes" {
		t.Fatalf("a slip outranked a real match: %v", cands[hits[0]].Show)
	}
}

func TestAQueryTooShortToGuessAtNeverGuesses(t *testing.T) {
	// Below four characters a single slip reaches so many names that the rung
	// stops meaning anything, so it is switched off rather than tuned.
	if bound := folderSlipBound("abc"); bound != 0 {
		t.Fatalf("a three-letter query was allowed %d slips", bound)
	}
	if _, ok := folderScoreOf("xyq", "~/code/xyz"); ok {
		t.Fatal("xyq reached ~/code/xyz, which only a slip could explain")
	}
}

func TestAQueryThatMatchesNothingMatchesNothing(t *testing.T) {
	if got := rankOf(t, "zzqqwwvv"); len(got) != 0 {
		t.Fatalf("a query nothing answers reached %v", got)
	}
}

func TestWhatIsUsedWinsBetweenTwoEquallyGoodAnswers(t *testing.T) {
	// Both are the same rung — the leaf name started — so the folder somebody
	// actually picks leads. A scorer that let the shorter leftover decide would
	// reorder the top of the list every time a letter was added.
	cands := []folderRankee{
		{Show: "~/code/afternoon", Rank: 0},
		{Show: "~/code/aforge-v2", Rank: 1, Freq: 8},
	}
	var ranker folderRanker
	ranker.load(cands)
	hits := ranker.rank("af")
	if len(hits) != 2 {
		t.Fatalf("af reached %d of the two folders", len(hits))
	}
	if cands[hits[0]].Show != "~/code/aforge-v2" {
		t.Fatalf("the folder in use lost to %q", cands[hits[0]].Show)
	}
	// AND THE LAYER STILL OUTRANKS USE, which is the picker's own ladder and
	// this file passes it through untouched.
	cands[0].Layer, cands[1].Layer = 0, 1
	ranker.load(cands)
	hits = ranker.rank("af")
	if cands[hits[0]].Show != "~/code/afternoon" {
		t.Fatalf("a nearer layer lost to a more-used folder: %q", cands[hits[0]].Show)
	}
}

func TestAnEmptyBoxLeavesTheSourcesOwnOrderAlone(t *testing.T) {
	// The opening frame: every candidate ties on the ladder and the layers, the
	// frecency and the source's order alone decide, exactly as they did before
	// this file existed.
	cands := []folderRankee{
		{Show: "~/z", Rank: 0},
		{Show: "~/a/very/deep/one", Rank: 1},
		{Show: "~/m", Rank: 2},
	}
	var ranker folderRanker
	ranker.load(cands)
	hits := ranker.rank("   ")
	if len(hits) != 3 {
		t.Fatalf("an empty box reached %d of the three folders", len(hits))
	}
	for at, hit := range hits {
		if hit != at {
			t.Fatalf("an empty box reordered the list: %v", hits)
		}
	}
}

func TestANameWithASpaceOrAnAccentIsFoundAndMistyped(t *testing.T) {
	got := rankOf(t, "tax returns")
	if len(got) != 1 || got[0] != "~/Documents/tax returns" {
		t.Fatalf("`tax returns` reached %v", got)
	}
	// The initials reach it too, because the space is a word boundary.
	if _, ok := folderScoreOf("tr", "~/Documents/tax returns"); !ok {
		t.Fatal("tr did not abbreviate `tax returns`")
	}
	// AND THE SLIP RUNG COUNTS RUNES AND NOT BYTES. `résumés` mistyped as
	// `résumées` is one slip to a person and two bytes to a machine, and the
	// second reading would put it out of reach.
	hit, ok := folderScoreOf("résumées", "~/Documents/résumés")
	if !ok || hit.tier != folderTierSlip {
		t.Fatalf("résumées scored ~/Documents/résumés as tier %v (matched %v)", hit.tier, ok)
	}
}

func TestAnAbsolutePathIsSplitIntoTheNamesItHas(t *testing.T) {
	// `/usr/local` has two names in it and not three, and an empty segment
	// before the first slash would put every absolute path one level further
	// from its own leaf than it is.
	f := foldFolder("/usr/local")
	if len(f.starts) != 2 || f.segment(0) != "usr" || f.segment(1) != "local" {
		t.Fatalf("/usr/local folded into %d segments: %q, %q", len(f.starts), f.segment(0), f.segment(len(f.starts)-1))
	}
	if f.base() != "local" {
		t.Fatalf("the leaf of /usr/local read as %q", f.base())
	}
	if got := rankOf(t, "usr", "/usr/local", "~/code/tui3"); len(got) != 1 || got[0] != "/usr/local" {
		t.Fatalf("usr reached %v", got)
	}
}

func TestUpperCaseIsFoundByLowerCase(t *testing.T) {
	if got := rankOf(t, "documents"); len(got) != 2 {
		t.Fatalf("documents reached %v, wanted the two folders under it", got)
	}
}

func TestSlipsCountASwappedPairAsOne(t *testing.T) {
	for _, probe := range []struct {
		a, b  string
		bound int
		want  int
		ok    bool
	}{
		{"aforge", "afroge", 1, 1, true},  // the pair swapped
		{"aforge", "aforgee", 1, 1, true}, // one too many
		{"aforge", "aforg", 1, 1, true},   // one too few
		{"aforge", "afergo", 1, 0, false}, // two moves is a different word
		{"aforge", "aforge", 1, 0, true},  // the same word
		{"", "ab", 1, 2, false},           // nothing is two away from two
	} {
		got, ok := folderSlips(probe.a, probe.b, probe.bound)
		if ok != probe.ok || (ok && got != probe.want) {
			t.Errorf("%q against %q within %d: %d %v, wanted %d %v",
				probe.a, probe.b, probe.bound, got, ok, probe.want, probe.ok)
		}
	}
}

func TestEveryRungIsReachable(t *testing.T) {
	// The ladder as a table, so a rebalance that quietly folded two rungs into
	// one is a red test rather than a list that behaves differently.
	for _, probe := range []struct {
		query, show string
		want        folderTier
	}{
		{"tui3", "~/code/tui3", folderTierName},
		{"tui", "~/code/tui3", folderTierNameLead},
		{"~/co", "~/code/tui3", folderTierPathLead},
		{"code", "~/code/tui3", folderTierSegment},
		{"ui3", "~/code/tui3", folderTierNameIn},
		{"av2", "~/code/aforge-v2", folderTierShort},
		{"ode/t", "~/code/tui3", folderTierPathIn},
		{"ag2", "~/code/aforge-v2", folderTierLoose},
		{"afroge", "~/code/aforge-v2", folderTierSlip},
	} {
		hit, ok := folderScoreOf(probe.query, probe.show)
		if !ok {
			t.Errorf("%q did not reach %q at all", probe.query, probe.show)
			continue
		}
		if hit.tier != probe.want {
			t.Errorf("%q reached %q on rung %d, wanted %d", probe.query, probe.show, hit.tier, probe.want)
		}
	}
}

func TestARankerFoldsOnceAndKeepsItsOwnScratch(t *testing.T) {
	// A KEYSTROKE IS ONE PASS AND NO ALLOCATION. The list is re-ranked on every
	// character typed into the filter box, so a scorer that folded or split a
	// few hundred paths per keystroke would be doing all of its work again for
	// each one.
	cands := make([]folderRankee, 0, 400)
	for at := 0; at < 400; at++ {
		cands = append(cands, folderRankee{Show: "~/code/project-" + string(rune('a'+at%26)) + "/internal/tui3", Rank: at})
	}
	var ranker folderRanker
	ranker.load(cands)
	ranker.rank("tui")
	got := testing.AllocsPerRun(20, func() { ranker.rank("tui") })
	// The sort's own closure is the one allocation this path is allowed.
	if got > 2 {
		t.Fatalf("a keystroke over 400 folders allocated %.0f times", got)
	}
}

func TestTheAtCompletionScorerIsUntouched(t *testing.T) {
	// THIS FILE DOES NOT TOUCH THE `@` LIST. [pathScore] ranks files under one
	// directory and answers a bare query with the path's own length, which is
	// right there and exactly wrong here — the two want opposite tiebreaks, so
	// they get two scorers and this one pins that the first still behaves.
	if got, ok := pathScore("internal/tui3/app.go", ""); !ok || got != len("internal/tui3/app.go") {
		t.Fatalf("pathScore answered a bare query with %d %v", got, ok)
	}
	if got, ok := pathScore("internal/tui3/app.go", "app.go"); !ok || got < tierPrefix || got >= tierSubstring {
		t.Fatalf("pathScore no longer ranks a base name in the prefix tier: %d %v", got, ok)
	}
}
