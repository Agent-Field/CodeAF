package tokens

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The glyph tier's gates (12.7 F). The width and ambiguity gates (F.1, F.2) and
// the ban gate (F.3) live in glyph_test.go beside the ones they extend, because
// the tier is a second column of the same vocabulary and not a second
// vocabulary. What follows is what the tier adds.

// TestNerdFontIsBMPPrivateUse is F.4, and it is the plane-15 refusal enforced
// rather than remembered. Nerd Fonts v3 relocated the whole Material set out of
// U+F500–U+FD46 and into plane 15, so a font patched at v2 has NOTHING at the
// new addresses — and astral-plane private use has the worst terminal and
// font-fallback support there is. A codepoint above the BMP in this table means
// somebody reached for nf-md-* .
func TestNerdFontIsBMPPrivateUse(t *testing.T) {
	for _, b := range Vocabulary() {
		if b.NerdFont == "" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(b.NerdFont)
		if r >= 0x10000 {
			t.Errorf("%s (%s) is %U: above the BMP (12.7 B.2 refuses plane-15 private use)", b.Name, b.NFName, r)
		}
		if !(r >= 0xE000 && r <= 0xF8FF) {
			t.Errorf("%s (%s) is %U: outside BMP private use, so it is not a Nerd Font address at all", b.Name, b.NFName, r)
		}
	}
}

// TestVocabularyIsCompleteAndFallsBackToFiveSeventeen is F.6, the tier's
// central law: the fallback provably IS the 5.17 glyph. Not a similar one, not
// a later revision of one — the same bytes as the exported constant a consumer
// that never heard of the tier is already rendering.
func TestVocabularyIsCompleteAndFallsBackToFiveSeventeen(t *testing.T) {
	plainOf := map[string]string{}
	for _, g := range Glyphs() {
		plainOf[g.Name] = g.Glyph
	}

	seenID := map[GlyphID]bool{}
	seenName := map[string]bool{}
	for _, b := range Vocabulary() {
		switch {
		case b.ID >= glyphIDCount:
			t.Errorf("%s has out-of-range id %d", b.Name, b.ID)
		case seenID[b.ID]:
			t.Errorf("%s repeats id %d", b.Name, b.ID)
		}
		seenID[b.ID] = true
		if seenName[b.Name] {
			t.Errorf("duplicate slot name %q", b.Name)
		}
		seenName[b.Name] = true

		if b.Plain == "" {
			t.Errorf("%s has no plain glyph; the floor is never empty", b.Name)
			continue
		}
		if want, ok := plainOf[b.Name]; !ok {
			t.Errorf("%s is not in Glyphs(), so it escapes the width gate", b.Name)
		} else if want != b.Plain {
			t.Errorf("%s: binding says %q, the 5.17 constant is %q — the fallback must be byte-identical",
				b.Name, b.Plain, want)
		}
		if strings.TrimSpace(b.Meaning) == "" {
			t.Errorf("%s carries no meaning; the table is what the ? help reads", b.Name)
		}
		if b.UsualTint >= tokenCount {
			t.Errorf("%s names a token that does not exist", b.Name)
		}

		if b.Geometry {
			if b.NerdFont != "" || b.NFName != "" {
				t.Errorf("%s is geometry and must have no nerd-font side (12.7 B.2)", b.Name)
			}
			if b.AutoUpgrade {
				t.Errorf("%s is geometry and cannot auto-upgrade", b.Name)
			}
			if b.NFAmbiguous {
				t.Errorf("%s is geometry and has no NF glyph to be ambiguous", b.Name)
			}
			continue
		}
		if b.NerdFont == "" {
			t.Errorf("%s is not geometry, so it owes an icon (12.7 B.1)", b.Name)
			continue
		}
		if utf8.RuneCountInString(b.NerdFont) != 1 {
			t.Errorf("%s: the icon is not a single rune", b.Name)
		}
		if !strings.HasPrefix(b.NFName, "nf-") {
			t.Errorf("%s: %q is not a Nerd Fonts class name, and the NAME is the contract", b.Name, b.NFName)
		}
		// The ASCII carve-out (12.7 D.3), stated as a law rather than as a
		// habit: a plain side a user can type is never rewritten automatically.
		typeable := b.Plain[0] < utf8.RuneSelf
		if why, named := typedPlainSides[b.Plain]; named {
			typeable = true
			if b.AutoUpgrade {
				t.Errorf("%s has a plain side a person types (%q: %s) and must not auto-upgrade",
					b.Name, b.Plain, why)
			}
		}
		if typeable && b.AutoUpgrade {
			t.Errorf("%s has an ASCII plain side (%q) and must not auto-upgrade: a painted cell "+
				"that is exactly that character is plausible content (5.20 rule 3)", b.Name, b.Plain)
		}
		if !typeable && !b.AutoUpgrade {
			t.Errorf("%s is a non-ASCII semantic slot and should upgrade automatically", b.Name)
		}
		if !b.Geometry && b.ASCII == "" {
			t.Errorf("%s is an icon slot and owes an ASCII spelling: the screen-reader tier "+
				"names a character where the other two draw a shape", b.Name)
		}
	}

	for id := GlyphID(0); id < glyphIDCount; id++ {
		if !seenID[id] {
			t.Errorf("glyph slot %d has no binding; Vocabulary() must cover every declared GlyphID", id)
		}
		for set := GlyphSet(0); set < glyphSetCount; set++ {
			if set.Glyph(id) == "" {
				t.Errorf("slot %d resolves to nothing under the %s tier", id, set)
			}
		}
	}
}

// typedPlainSides are the plain glyphs that are NOT ASCII and are still things
// a person types, with the reason each is one. They take the same carve-out an
// ASCII plain side takes: the automatic whole-cell rewrite may not touch them,
// because a painted cell that is exactly one of these is plausible CONTENT and
// a mechanism that rewrote it would be a mechanism that can lie (12.7 D.3).
var typedPlainSides = map[string]string{
	GlyphActionCommunicate: "the guillemet, which is a quotation mark in half of Europe",
}

// TestTheASCIITierNamesACharacterForEveryIcon is the third tier's own gate.
//
// THE FLOOR UNDER THE FLOOR IS ONE CHARACTER A READER CAN NAME. A screen reader
// announces "▤" as a character nobody has a word for; it announces "<" as
// "less than". So every ICON slot owes a spelling here, that spelling is one
// ASCII byte so no column moves when the tier flips, and every GEOMETRY slot
// resolves to its plain character — because the ASCII spelling of a grid is a
// RUN ("+-> ") owned by the renderer that draws the run.
func TestTheASCIITierNamesACharacterForEveryIcon(t *testing.T) {
	for _, b := range Vocabulary() {
		got := ASCII.Glyph(b.ID)
		if b.Geometry {
			if b.ASCII != "" {
				t.Errorf("%s is geometry and must not carry an ASCII spelling of its own", b.Name)
			}
			if got != b.Plain {
				t.Errorf("%s is geometry and draws %q under the ascii tier, want its plain %q", b.Name, got, b.Plain)
			}
			continue
		}
		if got != b.ASCII {
			t.Errorf("%s draws %q under the ascii tier, want %q", b.Name, got, b.ASCII)
		}
		if len(b.ASCII) != 1 || b.ASCII[0] >= utf8.RuneSelf || b.ASCII[0] < 0x20 {
			t.Errorf("%s: the ascii spelling %q is not one printable ASCII byte", b.Name, b.ASCII)
		}
	}
	if got := ASCII.Upgrade(GlyphWorking); got != GlyphWorking {
		t.Errorf("the ascii tier rewrote a painted cell to %q; only the nerd-font tier upgrades", got)
	}
}

// TestTierResolutionIsTotal: a render must not die because a caller handed it a
// number. Same forgiveness [NewStyler] extends to a profile that came off a
// terminal probe, for the same reason.
func TestTierResolutionIsTotal(t *testing.T) {
	if got := GlyphSet(200).Glyph(GWorking); got != GlyphWorking {
		t.Errorf("an out-of-range tier resolved to %q, want the plain floor", got)
	}
	if got := NerdFont.Glyph(GlyphID(250)); got != "" {
		t.Errorf("an out-of-range slot resolved to %q, want nothing", got)
	}
	if got := GlyphSet(200).String(); got != "invalid" {
		t.Errorf("GlyphSet(200).String() = %q", got)
	}
	for _, c := range []struct {
		in   string
		want GlyphSet
		ok   bool
	}{
		{"plain", Plain, true}, {"PLAIN", Plain, true}, {" off ", Plain, true},
		{"none", Plain, true}, {"0", Plain, true}, {"false", Plain, true},
		{"nerd", NerdFont, true}, {"nerdfont", NerdFont, true}, {"nf", NerdFont, true},
		{"nerd-font", NerdFont, true}, {"on", NerdFont, true}, {"1", NerdFont, true},
		{"ascii", ASCII, true}, {"TEXT", ASCII, true}, {" linear ", ASCII, true},
		{"", Plain, false}, {"fancy", Plain, false},
	} {
		got, ok := ParseGlyphSet(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseGlyphSet(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if Plain.String() != "plain" || NerdFont.String() != "nerdfont" || ASCII.String() != "ascii" {
		t.Error("the tier names are the spellings the flag, the env pin and the log line share")
	}
}

// TestUpgradeNeverRewritesContent is F.9, and it is the difference between a
// mechanism that is clever and one that cannot lie.
func TestUpgradeNeverRewritesContent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a whole working cell upgrades", GlyphWorking, NerdFont.Glyph(GWorking)},
		{"a whole collapsed cell upgrades", GlyphCollapsed, NerdFont.Glyph(GCollapsed)},
		{"an ASCII question mark is left alone", "?", "?"},
		{"an ASCII dollar is left alone", "$", "$"},
		{"an ASCII slash is left alone", "/", "/"},
		{"an ASCII equals is left alone", "=", "="},
		{"a sentence carrying a question mark is left alone", "why? because", "why? because"},
		{"a sentence LEADING with a glyph is left alone", GlyphWorking + " working on it", GlyphWorking + " working on it"},
		{"a sentence containing a glyph is left alone", "it is " + GlyphSettled + " now", "it is " + GlyphSettled + " now"},
		{"a geometry cell is left alone", GlyphSeparator, GlyphSeparator},
		{"a diff sign is left alone", GlyphDiffAdd, GlyphDiffAdd},
		{"an accent rail is left alone", GlyphAccentRail, GlyphAccentRail},
		{"a spinner frame is left alone", SpinnerFrames[0], SpinnerFrames[0]},
		{"empty stays empty", "", ""},
		{"an unrelated rune is left alone", "x", "x"},
	}
	for _, c := range cases {
		if got := NerdFont.Upgrade(c.in); got != c.want {
			t.Errorf("%s: Upgrade(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
		if got := Plain.Upgrade(c.in); got != c.in {
			t.Errorf("%s: the plain tier must be the identity function, got %q", c.name, got)
		}
	}
}

// TestUpgradeChromeIsAnExplicitDoor records the fate of 12.7 D.2's rule (b) —
// the automatic chrome-lead rewrite — and why this package ships the mechanism
// as a door instead of as a rule.
//
// Rule (b) would have upgraded the leading glyph of any string painted at
// blocks.StateChrome, and it existed for exactly one caller: blocks.Disclose
// returns "▸ 12 lines", which is not a single rune. Its warrant was blocks' own
// definition of the state — "separators, meta, fold lines, hints" — and D.2
// bound the rule to a test proving no CONTENT path paints StateChrome.
//
// That proof cannot be made in this tree. TestChromeStateCarriesContent below
// shows the header grammar painting a caller's Title and Desc at StateChrome,
// and the v2 chat surface uses exactly that: a receipt's headline is a
// chrome-state Title read out of the journal, and a commission's Desc is a
// summary of the user's own words. An automatic lead-rune rewrite would edit a
// sentence somebody wrote. D.2 named the remedy for that finding, and this is
// it: the rule is dropped, and the callers that want the behaviour ask for it
// by name, one token at the call site.
func TestUpgradeChromeIsAnExplicitDoor(t *testing.T) {
	hint := blocks.Disclose(false, 12, "line", "lines")
	if got := NerdFont.UpgradeChrome(hint); got != NerdFont.Glyph(GCollapsed)+" 12 lines" {
		t.Errorf("UpgradeChrome(%q) = %q", hint, got)
	}
	if got := NerdFont.Upgrade(hint); got != hint {
		t.Errorf("the automatic path must NOT rewrite a lead glyph: got %q", got)
	}
	if got := NerdFont.UpgradeChrome(blocks.Disclose(true, 0, "line", "lines")); got != NerdFont.Glyph(GExpanded) {
		t.Errorf("the expanded hint is a whole cell and upgrades: got %q", got)
	}
	// Even at the explicit door, an ASCII slot and a mid-line glyph are safe.
	for _, c := range []string{"? did it land", "$ 8.65", "why? because", "ok " + GlyphSettled + " done"} {
		if got := NerdFont.UpgradeChrome(c); got != c {
			t.Errorf("UpgradeChrome(%q) = %q, want it untouched", c, got)
		}
	}
	if got := Plain.UpgradeChrome(hint); got != hint {
		t.Errorf("the plain tier must be the identity function, got %q", got)
	}
}

// painted is one span blocks handed to a Styler, and recorder collects them.
type painted struct {
	text  string
	state blocks.State
}

type recorder struct{ seen []painted }

func (r *recorder) Paint(text string, state blocks.State, _ blocks.Hue) string {
	r.seen = append(r.seen, painted{text, state})
	return text
}

// TestChromeStateCarriesContent is the D.2 assertion, run and reported
// honestly. It asserts the finding rather than the hope: blocks paints a
// header's Title and Desc at whatever state the caller set, so a caller that
// dresses a journal line as chrome — which the v2 chat surface does for every
// receipt and every commissioning row — puts content on the StateChrome path.
//
// The test's job is to fail if that ever stops being true, at which point rule
// (b) becomes available again and this comment becomes wrong.
func TestChromeStateCarriesContent(t *testing.T) {
	var rec recorder
	head := blocks.Header{
		Glyph: GlyphCollapsed,
		State: blocks.StateChrome,
		Title: GlyphSettled + " wrote 3 files",
		Desc:  GlyphSettled + " ship it",
	}
	head.Render(80, &rec)

	title, desc := false, false
	for _, s := range rec.seen {
		if s.state != blocks.StateChrome {
			continue
		}
		if s.text == head.Title {
			title = true
		}
		if s.text == head.Desc {
			desc = true
		}
	}
	if !title || !desc {
		t.Fatalf("the header no longer paints Title (%v) and Desc (%v) at StateChrome; "+
			"12.7 D.2's rule (b) may be reconsidered", title, desc)
	}

	// And the consequence that matters: with rule (b) dropped, every one of
	// those chrome spans survives the tier byte for byte. Only the glyph cell,
	// which arrives as one whole rune, is rewritten.
	for _, s := range rec.seen {
		if s.text == head.Glyph {
			continue
		}
		if got := NerdFont.Upgrade(s.text); got != s.text {
			t.Errorf("the tier rewrote a painted span: %q became %q", s.text, got)
		}
	}
}

// TestProvenance is F.10: every class name and codepoint in the table checked
// against the pinned nerd-fonts glyphnames extract in testdata. The NAME is the
// contract; the codepoint is a binding, and a release that moves one must fail
// a test rather than quietly draw a different shape.
func TestProvenance(t *testing.T) {
	raw, err := os.ReadFile("testdata/nerdfont_glyphnames.json")
	if err != nil {
		t.Fatalf("read the pinned glyphnames extract: %v", err)
	}
	var extract struct {
		Metadata struct {
			Version string `json:"version"`
			Date    string `json:"date"`
		} `json:"METADATA"`
	}
	if err := json.Unmarshal(raw, &extract); err != nil {
		t.Fatalf("parse the extract: %v", err)
	}
	var entries map[string]struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("parse the extract entries: %v", err)
	}
	if extract.Metadata.Version == "" {
		t.Fatal("the extract has no version; a provenance gate with no pinned release is a footnote")
	}
	t.Logf("checked against ryanoasis/nerd-fonts glyphnames.json %s (%s)",
		extract.Metadata.Version, extract.Metadata.Date)

	for _, b := range Vocabulary() {
		if b.NerdFont == "" {
			continue
		}
		entry, ok := entries[b.NFName]
		if !ok {
			t.Errorf("%s: %q is not in the pinned extract", b.Name, b.NFName)
			continue
		}
		want, err := strconv.ParseUint(entry.Code, 16, 32)
		if err != nil {
			t.Errorf("%s: the extract's code %q does not parse", b.Name, entry.Code)
			continue
		}
		got, _ := utf8.DecodeRuneInString(b.NerdFont)
		if rune(want) != got {
			t.Errorf("%s (%s): the table draws %U, the pinned release says %U",
				b.Name, b.NFName, got, rune(want))
		}
	}
}

// TestPowerlineSeparatorsAreBannedButTheBranchIsNot pins the one distinction
// 12.7 G draws inside the powerline block, because it is the distinction a
// reviewer is most likely to flatten: the SEPARATORS are refused and the BRANCH
// SYMBOL is adopted. They are neighbours in the codepoint space and opposites
// in kind — a separator is a shape-join that needs someone else's background to
// tile against, and a branch is an icon that stands alone.
func TestPowerlineSeparatorsAreBannedButTheBranchIsNot(t *testing.T) {
	banned := map[rune]bool{}
	for _, b := range BannedGlyphs {
		banned[b.Rune] = true
	}
	for _, r := range []rune{0xE0B0, 0xE0B1, 0xE0B2, 0xE0B3,
		0xE0B8, 0xE0B9, 0xE0BA, 0xE0BB, 0xE0BC, 0xE0BD, 0xE0BE, 0xE0BF} {
		if !banned[r] {
			t.Errorf("powerline separator %U is not banned; 8.3's refusal is meant to be enforced "+
				"by the build rather than by review", r)
		}
	}
	if banned[0xE0A0] {
		t.Error("U+E0A0 is the powerline BRANCH symbol, the highest-coverage glyph in the whole " +
			"repertoire, and 12.7 G adopts it")
	}
	if got := NerdFont.Glyph(GGitBranch); got != "\uE0A0" {
		t.Errorf("the branch slot draws %q, want U+E0A0", got)
	}
}

// TestUpgradeIndexCoversTheWholeVocabulary keeps the production bar honest
// (12.7 F.12). The upgrade path is allowed one array index per painted cell,
// and it is only one index while every key sits inside the index's window; a
// key outside it still resolves, through the binary search, but it would be the
// one cell on the screen paying more than the others, which is exactly the kind
// of quiet asymmetry that goes unnoticed until a profile says so.
func TestUpgradeIndexCoversTheWholeVocabulary(t *testing.T) {
	for set := GlyphSet(0); set < glyphSetCount; set++ {
		for _, e := range upgradeTable[set] {
			if e.from < upgradeLo || e.from >= upgradeHi {
				t.Errorf("%s: the upgrade key %U falls outside the index window %U..%U",
					set, e.from, upgradeLo, upgradeHi)
			}
		}
	}
	// Every auto-upgradable slot is reachable, and no other slot is.
	for _, b := range Vocabulary() {
		r, _ := utf8.DecodeRuneInString(b.Plain)
		_, found := lookupUpgrade(NerdFont, r)
		if b.AutoUpgrade && !found {
			t.Errorf("%s does not resolve through the upgrade index", b.Name)
		}
		if !b.AutoUpgrade && found && b.Plain != GlyphStepRunning && b.Plain != GlyphStepPending {
			t.Errorf("%s upgrades automatically and must not", b.Name)
		}
	}
	if _, found := lookupUpgrade(Plain, []rune(GlyphWorking)[0]); found {
		t.Error("the plain tier upgrades nothing")
	}
}
