package tokens

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// The glyph repertoire tier (12.7).
//
// One axis, [GlyphSet], swaps the CHARACTERS used for meanings 5.17 already
// fixed — 1:1, inside our own language. It is not a skin: 8.3's refusal of
// oh-my-pi's nerd-font PRESET (its colours, its brackets, its powerline chrome,
// its status-bar layout) is untouched, and nothing here changes a hue, a
// separator, a bracket, a breakpoint or a line grammar.
//
// The governing invariant, stated once: the tier changes which glyph is drawn
// in a cell; it never changes how many cells a line occupies, which token tints
// it, or where a segment sits. Flipping the tier must not move one column —
// which is why every glyph on both sides of every binding measures one cell
// under both shipping rulers, asserted by glyphset_test.go rather than hoped
// for.
//
// Two rules decide what the tier touches (12.7 B):
//
//  1. Icons for meaning, geometry for structure. A slot whose glyph carries a
//     semantic — a state, an attention, a place, a prompt — is upgradable. A
//     slot whose glyph is line geometry — a separator, an accent rail, a
//     spawn-tree corner, a gauge step, a sparkline cell, a diff sign — is not.
//     Box drawing and block elements are already the right characters for a
//     grid, and an icon there would be strictly worse.
//  2. BMP private-use only, Font-Awesome-4-era and Powerline first. Those
//     codepoints have sat at the same addresses since Nerd Fonts v1 and are
//     present in every patched font, including minimal Powerline-only patches.
//     nf-md-* (Material) is refused: Nerd Fonts v3 relocated the whole set into
//     plane 15, so a v2-era patched font has nothing at the new addresses.

// GlyphSet is the glyph repertoire tier: which characters say the 5.17
// meanings. It composes with [Profile] and [Focus] and changes neither.
type GlyphSet uint8

const (
	// Plain is the 5.17 floor: metric-safe in every terminal, and a designed
	// floor rather than a degradation. A user who turns the tier off gets 5.17
	// exactly as the doc specified it — same segments, same order, same tints,
	// same widths.
	Plain GlyphSet = iota
	// NerdFont is the patched-font tier: one BMP private-use icon per
	// upgradable slot, each inheriting its meaning, its tint token and its cell
	// budget from the plain glyph it replaces.
	NerdFont
	// ASCII is the floor UNDER the floor: the spelling for a screen reader and
	// for a terminal with no Unicode at all, one character a reader can NAME
	// where the other two tiers draw a shape.
	//
	// IT IS NOT SOMETHING [DetectGlyphSet] EVER RETURNS. A font is a guess and
	// a repertoire is a detection, but "this surface is being read aloud" is a
	// thing the person said out loud (the linear option) — so the shell asks
	// for this tier by name and nothing infers it. Only the ICON slots carry an
	// ASCII spelling; a geometry slot resolves to its plain character here,
	// because the ASCII spelling of a grid is a RUN of characters ("+-> ") that
	// belongs to the renderer drawing the run, not one cell in a table.
	ASCII
	glyphSetCount
)

// String names the tier. These are also spellings [ParseGlyphSet] accepts, so a
// settings row, a flag and a log line share one vocabulary.
func (g GlyphSet) String() string {
	switch g {
	case Plain:
		return "plain"
	case NerdFont:
		return "nerdfont"
	case ASCII:
		return "ascii"
	}
	return "invalid"
}

// ParseGlyphSet parses a tier by name, reporting whether the spelling was
// recognized. An unrecognized spelling reports false rather than guessing,
// because an override that silently did something else would be the affordance
// lying about what it accepted (5.20).
func ParseGlyphSet(s string) (GlyphSet, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "plain", "none", "off", "no", "0", "false":
		return Plain, true
	case "nerd", "nerdfont", "nerd-font", "nf", "on", "yes", "1", "true":
		return NerdFont, true
	case "ascii", "text", "linear":
		return ASCII, true
	}
	return Plain, false
}

// GlyphID names one vocabulary SLOT — the meaning, independent of tier. It is
// the door a consumer uses when it wants "the working glyph" rather than a
// particular character, and it is the only door an ASCII slot has (see
// [GlyphBinding.AutoUpgrade]).
type GlyphID uint8

const (
	GQueued GlyphID = iota
	GWorking
	GSettled
	GFailed
	GStopped
	GPaused
	GNeedsHuman
	GWaitsOn
	GWithdrawn
	GAssumed
	GCollapsed
	GExpanded
	GScopeUp
	GTruncated
	GEllipsis
	GCut
	GPromptChat
	GPromptSteer
	GReplyIn
	GThought
	GShell
	GSearch
	GWrite
	GActionRead
	GActionCreate
	GActionTest
	GActionBrowse
	GActionTransfer
	GActionCommunicate
	GActionCoordinate
	GActionPlan
	GActionWait
	GActionWork
	GBoosted
	GSeparator
	GMissing
	GEstimate
	GAccentRail
	GDragHandle
	GStepDone
	GStepRunning
	GStepPending
	GStepBlocked
	GQueuePill
	GDiffAdd
	GDiffDel
	GTreeBranch
	GTreeLast
	GTreeVert
	GTreeDash
	GHome
	GFolder
	GGitBranch
	GModel
	GSpend
	GFileDocument
	GFileImage
	GFileAudio
	GFileVideo
	GProseBullet
	GProseQuote
	GCodeGutter
	glyphIDCount
)

// GlyphBinding is one slot of the vocabulary: its meaning, the 5.17 character
// that always says it, and the tier's icon when there is one.
type GlyphBinding struct {
	ID GlyphID
	// Name is the slot's name, matching the [GlyphInfo.Name] the width gate
	// already walks.
	Name string
	// Meaning is the 5.17 meaning, carried verbatim, so the `?` help surface
	// and a glyph-preview screen read the vocabulary out of the table rather
	// than out of a second prose list that can drift.
	Meaning string
	// Plain is the 5.17 glyph and is ALWAYS non-empty: the fallback is not a
	// degradation, it is the floor.
	Plain string
	// NerdFont is the tier's icon, empty exactly when [GlyphBinding.Geometry]
	// is true.
	NerdFont string
	// ASCII is the [ASCII] tier's spelling: ONE character a screen reader can
	// name, for a slot whose plain glyph carries its meaning by shape. It is
	// non-empty for every icon slot and empty for every geometry slot, which
	// falls back to [GlyphBinding.Plain] — glyphvocab_test.go pins both halves.
	ASCII string
	// NFName is the Nerd Fonts class name — "nf-fa-adjust". The NAME is the
	// contract and the codepoint is a binding verified against the pinned
	// glyphnames extract in testdata (12.7 B.4).
	NFName string
	// UsualTint documents the token the slot is normally painted with. It is
	// documentation, not a binding: tinting stays a pure product of the state ×
	// hue axes through [ResolveToken], which is the whole reason a mono icon is
	// admissible where an emoji is not.
	UsualTint Token
	// PlainAmbiguous and NFAmbiguous record East_Asian_Width=Ambiguous on each
	// side. ALL of private use is Ambiguous, so the NF side is always true —
	// which is exactly why a CJK locale vetoes the tier (12.7 B.3, E.2): under
	// ambiguous-wide the icons draw at two cells while several plain glyphs
	// draw at one, and width parity, which holds under both shipping rulers,
	// would break.
	PlainAmbiguous bool
	NFAmbiguous    bool
	// Geometry marks a slot the tier deliberately does not touch: line
	// geometry, where box drawing and block elements are already the right
	// characters for a grid.
	Geometry bool
	// AutoUpgrade allows [GlyphSet.Upgrade] to rewrite this slot's plain glyph
	// when it arrives as a whole painted cell. It is FALSE for every slot whose
	// plain glyph is ASCII, because a painted cell that is exactly "?" or "$"
	// is plausible CONTENT — 5.20 rule 3 makes "?" a thing a user types — and a
	// mechanism that rewrote it would be a mechanism that can lie. ASCII slots
	// are adopted explicitly, through [GlyphSet.Glyph], by the one consumer
	// that owns each.
	AutoUpgrade bool
}

// Vocabulary returns every slot in declaration order. Tests, the `?` help
// surface and a glyph-preview screen all walk it. The slice shares the
// package's table and is not to be written to.
func Vocabulary() []GlyphBinding {
	return vocabulary[:len(vocabulary):len(vocabulary)]
}

// glyphTable is the resolution table: one string per (tier, slot), built once
// at package initialization. [GlyphSet.Glyph] is an array index into it — no
// map, no allocation, nothing on the hot path.
var glyphTable [glyphSetCount][glyphIDCount]string

// upgradeTable is the whole-cell rewrite table, sorted by plain rune. It is
// walked directly only for a key outside [upgradeLo, upgradeHi); everything
// inside that window — which is every glyph the vocabulary actually carries —
// resolves through upgradeIndex in one array index, because this runs once per
// painted cell and the production bar (12.7 F.12) says no map probe there.
var upgradeTable [glyphSetCount][]upgradeEntry

type upgradeEntry struct {
	from rune
	to   string
}

// The window every semantic glyph in the 5.17 vocabulary falls in: general
// punctuation, arrows, technical symbols, geometric shapes, miscellaneous
// symbols and dingbats run from U+2000 to the braille block at U+2800. The
// index is one byte per codepoint in it — four kilobytes of package data, built
// once — holding the upgrade entry's position plus one, so zero means "not an
// upgradable cell". A key outside the window still resolves, through the binary
// search below; glyphset_test.go reports if one ever appears, because it would
// be the one cell on the screen paying more than its neighbours.
const (
	upgradeLo   rune = 0x2000
	upgradeHi   rune = 0x2800
	upgradeSpan      = upgradeHi - upgradeLo
)

var upgradeIndex [glyphSetCount][upgradeSpan]uint8

func init() {
	for id := GlyphID(0); id < glyphIDCount; id++ {
		binding, ok := bindingOf(id)
		if !ok {
			panic("tokens: glyph slot " + itoa(int(id)) + " has no binding")
		}
		glyphTable[Plain][id] = binding.Plain
		if binding.NerdFont != "" {
			glyphTable[NerdFont][id] = binding.NerdFont
		} else {
			glyphTable[NerdFont][id] = binding.Plain
		}
		if binding.ASCII != "" {
			glyphTable[ASCII][id] = binding.ASCII
		} else {
			glyphTable[ASCII][id] = binding.Plain
		}
	}

	seen := map[rune]string{}
	for _, b := range vocabulary {
		if b.NerdFont == "" || !b.AutoUpgrade {
			continue
		}
		from, _ := utf8.DecodeRuneInString(b.Plain)
		if prior, dup := seen[from]; dup {
			// Three plain glyphs serve two slots each (○ queued/step-pending,
			// ◐ working/step-running, ⚑ waits-on/step-blocked). That is by
			// design, and it is only safe while both slots upgrade to the SAME
			// icon — otherwise a whole-cell rewrite would have to know which
			// slot it was looking at, which it cannot.
			if prior != b.NerdFont {
				panic("tokens: plain glyph " + b.Plain + " upgrades two ways")
			}
			continue
		}
		seen[from] = b.NerdFont
		upgradeTable[NerdFont] = append(upgradeTable[NerdFont], upgradeEntry{from: from, to: b.NerdFont})
	}
	sort.Slice(upgradeTable[NerdFont], func(i, j int) bool {
		return upgradeTable[NerdFont][i].from < upgradeTable[NerdFont][j].from
	})
	for set := GlyphSet(0); set < glyphSetCount; set++ {
		for i, e := range upgradeTable[set] {
			if e.from < upgradeLo || e.from >= upgradeHi {
				continue
			}
			if i+1 > 0xFF {
				panic("tokens: the upgrade index cannot address this many slots")
			}
			upgradeIndex[set][e.from-upgradeLo] = uint8(i + 1)
		}
	}
}

func bindingOf(id GlyphID) (GlyphBinding, bool) {
	for _, b := range vocabulary {
		if b.ID == id {
			return b, true
		}
	}
	return GlyphBinding{}, false
}

// Glyph resolves a slot under a tier: one array index. An out-of-range tier
// reads as [Plain] and an out-of-range slot returns "" — a render must not die
// because a caller handed it a number.
func (g GlyphSet) Glyph(id GlyphID) string {
	if g >= glyphSetCount {
		g = Plain
	}
	if id >= glyphIDCount {
		return ""
	}
	return glyphTable[g][id]
}

// Upgrade is the automatic path (12.7 D.2), and it is the identity function for
// [Plain].
//
// It rewrites a painted cell into this tier's glyph under ONE precise
// condition: the entire string is exactly one rune, and that rune is an
// [GlyphBinding.AutoUpgrade] slot's plain glyph. Never a substring rewrite
// anywhere in a line, and never an ASCII slot.
//
// The warrant for the whole-cell rule is blocks' own header grammar: the glyph
// cell is painted as its own span (blocks/header.go), so it arrives at a Styler
// as a one-rune string, which is how a surface built before this tier existed
// gets the tier for free.
func (g GlyphSet) Upgrade(cell string) string {
	if g != NerdFont || cell == "" {
		return cell
	}
	r, size := utf8.DecodeRuneInString(cell)
	if size != len(cell) {
		return cell
	}
	if up, ok := lookupUpgrade(g, r); ok {
		return up
	}
	return cell
}

// UpgradeChrome is the EXPLICIT door for a chrome string that leads with a
// glyph and then says something — blocks.Disclose's "▸ 12 lines" is the
// case it exists for. It upgrades a whole cell exactly as [GlyphSet.Upgrade]
// does, and additionally rewrites a leading glyph that is followed by a space.
//
// It is deliberately NOT wired into [Styler.Paint] under blocks.StateChrome,
// which is what 12.7 D.2 proposed as rule (b). The rule's warrant was that
// StateChrome means "separators, meta, fold lines, hints" and therefore that
// prose never travels that path — and in this tree it does: the v2 chat surface
// paints a receipt's own headline as a chrome-state header Title and a
// commission's summary of the user's words as a chrome-state Desc. Both are
// content read out of the journal, and an automatic lead-rune rewrite would
// edit a user's sentence. D.2 named the remedy for exactly this finding: the
// rule is dropped and the callers that want it ask for it by name.
func (g GlyphSet) UpgradeChrome(cell string) string {
	if g != NerdFont || cell == "" {
		return cell
	}
	r, size := utf8.DecodeRuneInString(cell)
	if size == len(cell) {
		if up, ok := lookupUpgrade(g, r); ok {
			return up
		}
		return cell
	}
	if cell[size] != ' ' {
		return cell
	}
	if up, ok := lookupUpgrade(g, r); ok {
		return up + cell[size:]
	}
	return cell
}

// lookupUpgrade is the bounded rune check the hot path is allowed: one array
// index for every glyph the vocabulary carries, and a binary search over a
// couple of dozen entries for a key outside the index's window.
func lookupUpgrade(g GlyphSet, r rune) (string, bool) {
	table := upgradeTable[g]
	if r >= upgradeLo && r < upgradeHi {
		i := upgradeIndex[g][r-upgradeLo]
		if i == 0 {
			return "", false
		}
		return table[i-1].to, true
	}
	lo, hi := 0, len(table)-1
	for lo <= hi {
		mid := int(uint(lo+hi) >> 1)
		switch {
		case table[mid].from < r:
			lo = mid + 1
		case table[mid].from > r:
			hi = mid - 1
		default:
			return table[mid].to, true
		}
	}
	return "", false
}

// GlyphsIn is the width gate's walk, per tier.
//
// For [Plain] it is the whole 5.17 floor, [Glyphs] exactly — every slot plus
// the animated sets. For [NerdFont] it is the icons the tier INTRODUCES: one
// entry per upgradable slot, and nothing else, because the geometry slots and
// the animated sets have no NF side at all (12.7 B.2) and walking their plain
// characters again under a tier name would say the tier drew something it does
// not draw.
func GlyphsIn(g GlyphSet) []GlyphInfo {
	switch g {
	case NerdFont:
		out := make([]GlyphInfo, 0, len(vocabulary))
		for _, b := range vocabulary {
			if b.NerdFont == "" {
				continue
			}
			r, _ := utf8.DecodeRuneInString(b.NerdFont)
			out = append(out, GlyphInfo{Name: b.Name, Glyph: b.NerdFont, Rune: r, AmbiguousWidth: b.NFAmbiguous})
		}
		return out
	case ASCII:
		out := make([]GlyphInfo, 0, len(vocabulary))
		for _, b := range vocabulary {
			if b.ASCII == "" {
				continue
			}
			r, _ := utf8.DecodeRuneInString(b.ASCII)
			out = append(out, GlyphInfo{Name: b.Name, Glyph: b.ASCII, Rune: r})
		}
		return out
	}
	return Glyphs()
}
