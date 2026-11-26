package chatlist

import (
	"strings"
	"testing"
)

// liveReasons is the closed set of close reasons a live view can end with. It is
// written out here from the contract and not read from the code, so a reason
// added on the wire without words fails this test instead of rendering blank.
var liveReasons = []string{
	"not-running", "not-allowed", "holder-gone", "holder-old", "removed", "permit-changed",
	"lagging", "frame-too-large", "budget", "idle", "host-closing", "too-many", "remote-control-off",
}

// V12/V25 vocabulary: a live view is part of the pairing journey, so none of its
// sentences may use the words that journey bans, and every reason has its own
// sentence that names the computer when it is about one.
func TestEveryLiveCloseReasonHasItsOwnSentenceInThePairingVocabulary(t *testing.T) {
	banned := []string{"relay", "lease", "cell", "directory", "manifest", "take ", "node"}
	seen := map[string]string{}
	for _, reason := range liveReasons {
		said := LiveSaid(reason, "Desktop")
		if strings.TrimSpace(said) == "" {
			t.Fatalf("reason %q has no sentence", reason)
		}
		if other, dup := seen[said]; dup {
			t.Fatalf("reasons %q and %q say the same thing: %q", reason, other, said)
		}
		seen[said] = reason
		for _, word := range banned {
			if strings.Contains(strings.ToLower(said), word) {
				t.Fatalf("reason %q says %q, which uses the banned word %q", reason, said, word)
			}
		}
	}
	if said := LiveSaid("something-new", "Desktop"); !strings.Contains(said, "Desktop") {
		t.Fatalf("an unknown reason should still name the computer: %q", said)
	}
}

// A host whose name is not known yet never leaves a hole in a sentence.
func TestALiveSentenceWithNoHostNameReadsAsTheOtherComputer(t *testing.T) {
	for _, said := range []string{LiveWatchOnly(""), LiveWentOffline(""), LiveSaid("not-running", ""), LiveNeedsItsOwnOK(""), LiveNotAllowedHere("")} {
		if !strings.Contains(said, otherComputer) || strings.HasPrefix(said, " ") || strings.Contains(said, "  ") {
			t.Fatalf("a sentence with no host name has a hole in it: %q", said)
		}
	}
}

// The needles the terminal e2e suite waits for are spelled in this package.
func TestTheLiveWordsTheTmuxSuiteWaitsForAreSpelledHere(t *testing.T) {
	for want, said := range map[string]string{
		"Moving here":     LiveMoving,
		"went offline":    LiveWentOffline("Desktop"),
		"watch, not type": LiveWatchOnly("Desktop"),
		"typing from":     LiveTypingFrom("Desktop"),
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("%q does not contain %q", said, want)
		}
	}
}

// V30 and V41: the road row has exactly two values and one home, whichever
// surface draws it, and a road nobody has named yet draws as nothing.
func TestTheRoadWordsHaveOneHome(t *testing.T) {
	for road, want := range map[string]string{
		"direct":         "live · direct",
		"relay":          "live · relay",
		" direct ":       "live · direct",
		"somewhere else": "live · relay",
		"":               "",
	} {
		if got := LiveRoad(road); got != want {
			t.Fatalf("LiveRoad(%q) = %q, want %q", road, got, want)
		}
	}
}

// A road change is the viewer's own business and is never spoken: it has no
// sentence of its own, so asking for one gets the generic fallback every unknown
// reason gets, and the closed set above stays closed.
func TestARoadChangeIsViewerLocalAndHasNoSentenceOfItsOwn(t *testing.T) {
	if got, fallback := LiveSaid("road-change", "Desktop"), LiveSaid("unknown-reason-sentinel", "Desktop"); got != fallback {
		t.Fatalf("road-change has a sentence of its own: %q", got)
	}
}
