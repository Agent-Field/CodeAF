package modelui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE ROLE WORDS, and the report that produced them: "when I click to choose a
// model I don't get an OpenRouter list, rather some weird lists like voice,
// architect, skeptic". Those are the roles table's own names for its slots — a
// metaphor at the store's altitude — and §14 is user-facing words only, on every
// surface. A reader has planning and checking; nobody has an architect.

// bannedRoleWords is the vocabulary that must never reach a frame of this
// surface again. It is the roles table's spelling, read from the table itself
// rather than copied, so a rename there cannot quietly empty this list.
func bannedRoleWords() []string {
	out := make([]string, 0, len(store.ModelRoles()))
	for _, role := range store.ModelRoles() {
		if word := role.Word(); word != "" && word != RoleWord(role) {
			out = append(out, word)
		}
	}
	return out
}

// ONE SPELLING, EVERYWHERE. The words this picker draws are the settings page's
// own, read back out of the registry that page renders, so a slot cannot be
// called one thing on the sheet and another in the picker the sheet opens.
//
// It is also the totality gate: a sixth role, or a role whose plain word nobody
// wrote, fails here rather than reaching a reader as machinery.
func TestTheRoleWordsAreTheSettingsPagesOwn(t *testing.T) {
	t.Parallel()
	labels := map[store.ModelRole]string{}
	for _, slot := range config.ModelSlots() {
		if slot.Role != "" {
			labels[slot.Role] = slot.Label
		}
	}
	for _, role := range store.ModelRoles() {
		want, named := labels[role]
		if !named {
			t.Fatalf("the settings page has no row for role %q", role)
		}
		if got := RoleWord(role); got != want {
			t.Errorf("role %q is %q here and %q on the settings page — one slot, one word",
				role, got, want)
		}
	}
}

// A word for every role, and none of them the journal's. The degradation in
// [RoleWord] exists so a missing word is still a name; this is what keeps it
// unreachable.
func TestEveryRoleHasAPlainWordAndNoneIsTheJournals(t *testing.T) {
	t.Parallel()
	for _, role := range store.ModelRoles() {
		word := RoleWord(role)
		switch {
		case word == "":
			t.Errorf("role %q has no word", role)
		case word == string(role) && role != store.RoleWork:
			// Falling back to the role's own spelling means nobody wrote a word.
			// `work` is deliberately not exempt — it simply is not spelled that
			// way — so any hit here is a missing entry.
			t.Errorf("role %q fell back to its journal spelling", role)
		case word != strings.ToLower(word):
			t.Errorf("role %q is titled %q — chrome words are all-lowercase (§16 CASE)", role, word)
		}
	}
}

// THE FRAME SCAN. Not the function — the pixels. Every level of this surface,
// at the widths a reader actually sees, walked for the banned vocabulary.
func TestNoInventedRoleWordReachesAFrame(t *testing.T) {
	t.Parallel()
	banned := bannedRoleWords()
	if len(banned) == 0 {
		t.Fatal("nothing is banned, so this test is asserting nothing")
	}

	frames := map[string][]string{}
	for _, width := range []int{60, 90, 120} {
		p := newPicker(sampleCatalog())
		frames["roles"] = append(frames["roles"], lines(p, width, 12)...)
		for _, role := range store.ModelRoles() {
			p.Open(role)
			frames["models"] = append(frames["models"], lines(p, width, 12)...)
		}
		// And the chip, which is the other place a role names itself.
		for _, role := range store.ModelRoles() {
			chip := Chip{Role: role, Model: "anthropic/claude-opus-5"}
			frames["chip"] = append(frames["chip"], chip.Text())
		}
	}

	for where, rendered := range frames {
		for _, line := range rendered {
			for _, word := range banned {
				if strings.Contains(line, word) {
					t.Errorf("%s frame carries the invented word %q: %q", where, word, line)
				}
			}
		}
	}
}

// And the plain words are actually there — a scan that passed because the
// surface drew nothing would prove nothing.
func TestThePlainRoleWordsAreOnTheRoleLevel(t *testing.T) {
	t.Parallel()
	rendered := lines(newPicker(sampleCatalog()), 90, 12)
	for _, role := range store.ModelRoles() {
		if _, ok := find(rendered, RoleWord(role)); !ok {
			t.Errorf("the role level does not say %q:\n%s", RoleWord(role), strings.Join(rendered, "\n"))
		}
	}
}
