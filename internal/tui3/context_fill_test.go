package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── /status SAYS WHICH RULE FOLDED YOUR WORK ────────────────────────────────
//
// The context row says how full the conversation is. Until this line existed
// nothing on the surface said how full it is ALLOWED to get, so a person whose
// conversation had just folded had no way to tell whether it followed the
// window or a fill they had pinned months ago in the settings sheet.

// TestStatusNamesTheDerivedRuleWhenNobodySetOne is the wording, on the ordinary
// session: the line, the window it is a share of, and the rule that drew it.
func TestStatusNamesTheDerivedRuleWhenNobodySetOne(t *testing.T) {
	const claimed = 1_310_720
	a := newTestApp(&fakeAgent{model: "openrouter/deepseek-v4-flash", window: claimed, weight: 40_000})
	a.model, a.ctxWindow, a.ctxTokens = "openrouter/deepseek-v4-flash", claimed, 40_000

	a.slash("/status")
	text := lastNote(t, a)
	if !strings.Contains(text, "compacts at") {
		t.Fatalf("/status does not say where the fold line is:\n%s", text)
	}
	if !strings.Contains(text, "85% of 1.3M (derived)") {
		t.Fatalf("/status does not name the derived rule:\n%s", text)
	}
	if strings.Contains(text, "(pinned)") {
		t.Fatalf("a session nobody has pinned reads as pinned:\n%s", text)
	}
}

// TestStatusNamesThePinnedRuleWhenSomebodyDid is the other half, and it is the
// question a person opens /status to answer: my conversation folded early — was
// that me?
func TestStatusNamesThePinnedRuleWhenSomebodyDid(t *testing.T) {
	t.Setenv("AFORGE_CONTEXT_FILL_PCT", "60")
	const claimed = 1_000_000
	a := newTestApp(&fakeAgent{model: "m", window: claimed, weight: 40_000})
	a.model, a.ctxWindow, a.ctxTokens = "m", claimed, 40_000

	a.slash("/status")
	text := lastNote(t, a)
	if !strings.Contains(text, "60% of 1M (pinned)") {
		t.Fatalf("/status does not name the pinned rule:\n%s", text)
	}
	if strings.Contains(text, "(derived)") {
		t.Fatalf("a pinned session reads as derived:\n%s", text)
	}
}

// THE FIGURE IS THE TRIGGER'S OWN. A surface that worked the percentage out
// from the fill setting rather than from the function the fold fires on would
// agree with it right up until either clamp bit, and would then be quietly
// wrong — which is the failure session.CompactThresholdFor is exported to
// prevent.
func TestTheFoldLineDrawnIsTheOneTheTriggerUses(t *testing.T) {
	t.Setenv("AFORGE_CONTEXT_FILL_PCT", "90")
	// A window small enough that the ceiling bites: ninety percent of it is more
	// than the law will leave the answer, so the line comes back clamped and the
	// drawn percentage is NOT the ninety somebody typed.
	const claimed = 500_000
	a := newTestApp(&fakeAgent{model: "m", window: claimed, weight: 40_000})
	a.model, a.ctxWindow, a.ctxTokens = "m", claimed, 40_000

	threshold := session.CompactThresholdFor("m", claimed)
	want := itoa((threshold*200/claimed+1)/2) + "% of " + tokenWord(claimed) + " (pinned)"
	if got := a.compactionRuleWord(); got != want {
		t.Fatalf("the line drawn is %q, want %q", got, want)
	}
	if strings.HasPrefix(want, "90%") {
		t.Fatalf("this test needs a window where the clamp bites; it drew %q", want)
	}
}

// A WINDOW NOBODY HAS NAMED HAS NO FOLD LINE. The threshold against an unknown
// window is a figure that means nothing, so the row is absent rather than
// guessing — the same silence the context meter beside it keeps.
func TestStatusSaysNothingAboutAFoldLineItCannotDraw(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.model = "m"

	a.slash("/status")
	if text := lastNote(t, a); strings.Contains(text, "compacts at") {
		t.Fatalf("the note drew a fold line against an unknown window:\n%s", text)
	}
	if got := a.compactionRuleWord(); got != "" {
		t.Fatalf("an unknown window drew %q", got)
	}
}
