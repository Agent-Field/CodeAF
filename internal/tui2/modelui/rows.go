package modelui

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The two levels, as rows.
//
// 5.23 is explicit about the shape and this file is only that sentence in Go:
// "the settings surface shows the five roles as five rows — not a model catalog
// — with the catalog one level deeper". So the first level is ALWAYS five rows,
// in ladder order, whatever the wiring managed to resolve; and the second is
// one role's models, reached by opening a role and not by scrolling past it.
//
// The first level is five rows even when the catalog is empty. That is not
// defensiveness — it is the complexity cap made visible: the ladder is a
// property of the product, not of this machine's configuration, and a surface
// that showed three roles because two were unbound would teach that the ladder
// has three rungs.

// row is one line of either level.
type row struct {
	// glyph is the gutter mark: the settled ✓ on the model that is currently
	// bound here, and nothing at all on every other row.
	glyph    string
	glyphTok tokens.Token

	// verb is the row's name — a role word, or a model word.
	verb      string
	lowerVerb string

	// lowerSlug and lowerName are the two spellings the row is ALSO searched
	// by and never shows: the provider id and the provider's display name. A
	// reader who types "anthropic" or "Opus" is naming the model in a
	// vocabulary the word column does not carry, and a filter that answered
	// "no model matches" to either would be lying about a list it is holding.
	lowerSlug, lowerName string

	// hint is the description column: what a role does, or the wiring's note
	// about a model. A disabled row spends the column on its reason instead,
	// and a role row spends it on the chip and puts the hint after it if the
	// cells are there.
	hint      string
	lowerHint string

	// chip is drawn in the description column instead of hint, on role rows.
	// The row IS the chip (5.23): the verb column already said which role, so
	// the chip carries no role word and the two never repeat each other.
	chip    Chip
	hasChip bool

	// right is the right-aligned column at each width step, widest first: a
	// provenance word on a role row, and the receipt block — price, window,
	// score — on a model row. A disabled row advertises none of it, for the
	// same reason the palette drops a disabled row's accelerator: a column
	// beside a reason it cannot be used is the row lying about itself.
	//
	// Three strings rather than one because the block is a TABLE (§16
	// ALIGNMENT). Every row's step-k string is the same width as every other
	// row's, so whichever step a width can afford, the sub-columns line up down
	// the list; truncating one shared string instead would have left a ragged
	// edge that reads as three different columns.
	right [receiptSteps]string

	// disabled is why this row cannot be chosen, in the wiring's words.
	disabled string

	// result is what choosing the row emits. Nil with descend set is a role
	// row, which opens the level beneath it instead of finishing.
	result  Result
	descend bool
	role    store.ModelRole

	// option is the model this row was built from, kept for the sort and the
	// receipts and read for nothing else. A role row's is the zero value.
	option ModelOption
	// bound marks the model the role is running on now, which is where the
	// cursor lands when the level opens. It is the same fact the ✓ in the
	// gutter draws, held separately because a glyph is not a thing to search.
	bound bool
}

func (r *row) enabled() bool { return r.disabled == "" }

// buildRoles is the first level: the five, in ladder order.
func buildRoles(dst []row, c Catalog) []row {
	dst = dst[:0]
	for _, role := range store.ModelRoles() {
		binding := c.roleRow(role)
		reason := binding.Disabled
		if reason == "" {
			reason = c.Disabled
		}
		item := row{
			// The product's word, never the roles table's own (see [RoleWord]).
			verb:    RoleWord(role),
			hint:    roleHint(role),
			descend: true,
			role:    role,
			// No role word on the chip: the name column is already the role
			// word, and a chip that repeated it would spend cells saying the
			// same thing twice on one line.
			chip: Chip{
				Model:   binding.Model,
				Boosted: binding.Boosted,
				Used:    binding.Used,
				Window:  binding.Window,
			}.WithoutRole(),
			hasChip:  true,
			disabled: reason,
		}
		item.lowerVerb, item.lowerHint = lower(item.verb), lower(item.hint)
		// A role row's provenance word does not shrink: it is one short word at
		// every step, and a column that had a narrower spelling of itself would
		// be inventing a second vocabulary for width.
		word := provenanceWord(binding.Source)
		for step := range item.right {
			item.right[step] = word
		}
		dst = append(dst, item)
	}
	return dst
}

// inheritVerb is the clear row's name. "inherit" rather than "clear" because it
// names the RESULT the user gets — the wider scope answering again — and not
// the mechanic that produces it.
const inheritVerb = "inherit"

// buildModels is the second level: one role's catalog, with the clear row on
// top when there is a binding here to clear.
func buildModels(dst []row, c Catalog, role store.ModelRole) []row {
	dst = dst[:0]
	binding := c.roleRow(role)
	scope := c.scope()
	if binding.BoundHere {
		item := row{
			verb:     inheritVerb,
			hint:     "unbind here — the wider scope answers again",
			result:   ClearRole{Role: role, Scope: scope},
			disabled: c.Disabled,
		}
		item.lowerVerb, item.lowerHint = lower(item.verb), lower(item.hint)
		dst = append(dst, item)
	}
	first := len(dst)
	for _, model := range c.Models {
		if model.Slug == "" {
			continue
		}
		reason := model.Disabled
		if reason == "" {
			reason = c.Disabled
		}
		// A variant slug shows its variant word. ModelWord strips the suffix,
		// which once left ":batch" and its base model as two identical rows —
		// the reader picked the dead one and every plan call 404ed after.
		verb := ModelWord(model.Slug)
		if v := variantWord(model.Slug); v != "" {
			verb += " " + tokens.GlyphSeparator + " " + v
		}
		item := row{
			verb:     verb,
			hint:     model.Note,
			result:   SetRole{Role: role, ModelSlug: model.Slug, Scope: scope},
			disabled: reason,
			option:   model,
		}
		if model.Slug == binding.Model {
			// The one that is running now. ✓ is the settled glyph and it means
			// the same thing here as it does on a card: this is the state, not
			// a recommendation.
			item.glyph, item.glyphTok = tokens.GlyphSettled, tokens.Green
			item.bound = true
		}
		item.lowerVerb, item.lowerHint = lower(item.verb), lower(item.hint)
		item.lowerSlug, item.lowerName = lower(model.Slug), lower(model.Name)
		dst = append(dst, item)
	}
	sortModels(dst[first:])
	fillReceipts(dst[first:])
	return dst
}

// sortModels is the order four hundred rows are read in: family, then the base
// word, then price, then the full word.
//
// FAMILY FIRST because the vendor prefix is the one part of the slug the row's
// word drops (see [ModelWord]), and dropping it from a list this long without
// grouping by it would put "claude-opus-5" and "command-r" beside each other
// with nothing to say why. Sorted, the families are visible from the words
// themselves and need no header to announce them (§15: structure is never
// labeled).
//
// BASE WORD SECOND so a model and its variants (":free", ":thinking") sit
// together and read as one family of spellings, not as strangers separated by
// price. Price-second used to hold here, and it is what put a half-price
// ":batch" twin ABOVE the model it was a variant of, wearing the same word.
//
// PRICE THIRD, cheapest-first, inside a name group: "how much am I willing to
// spend" is still the question, now asked among a model's own spellings. A
// model whose price nobody published sorts LAST inside its group: an unknown
// price is not a low one.
//
// The sort is stable, so two rows the keys cannot separate keep the order the
// wiring handed them — the provider's own, which is at least a fact.
func sortModels(models []row) {
	sort.SliceStable(models, func(i, j int) bool {
		a, b := &models[i], &models[j]
		if fa, fb := family(a.option.Slug), family(b.option.Slug); fa != fb {
			return fa < fb
		}
		if ba, bb := lower(ModelWord(a.option.Slug)), lower(ModelWord(b.option.Slug)); ba != bb {
			return ba < bb
		}
		if pa, pb := sortPrice(a.option.Price), sortPrice(b.option.Price); pa != pb {
			return pa < pb
		}
		return a.lowerVerb < b.lowerVerb
	})
}

// family is the vendor prefix of a slug ("anthropic/claude-opus-5" →
// "anthropic"), lowered, or the whole slug when there is no prefix to take.
func family(slug string) string {
	if at := strings.IndexByte(slug, '/'); at > 0 {
		return lower(slug[:at])
	}
	return lower(slug)
}

// sortPrice is the key a model sorts by, and an unpriced model sinks. The
// figure is the input price: it is the one that scales with everything a
// prompt carries, and a model that is cheap in and dear out is still the cheap
// one to try.
func sortPrice(p Price) float64 {
	if !p.Known {
		return math.Inf(1)
	}
	return p.In
}

// The receipt block: what a model row says on its right edge, and the whole of
// what this surface can honestly say about a model.
//
// THREE FACTS, IN THIS ORDER: price, context window, published score. It is the
// order of what a choice actually turns on — money first, then whether the work
// will fit, then somebody else's opinion — and it is also the order they are
// given up in, since the block drops its columns from the right (see
// [receiptSteps]).
//
// There is no fourth. OpenRouter publishes throughput and latency per PROVIDER
// endpoint rather than per model, and nothing in this tree fetches those; a
// speed cell here would be a guess wearing a receipt's clothes. Anything else a
// model catalog is imagined to know — a tier word, a "recommended", a quality
// star — is not published by anyone and would be this package inventing
// economics three layers from whoever could check it.
const (
	// receiptSteps is how many widths the block has: everything, everything
	// but the score, and the price alone.
	receiptSteps = 3
	// receiptGap separates two receipt columns, and is the padding rhythm's
	// two cells (§16) rather than one — at one cell "$3.00/$15.00 200K" reads
	// as a single number with a space in it.
	receiptGap = 2
	// scoreMark attributes the published score to the people who published it
	// (Artificial Analysis, republished by OpenRouter). A bare "63.1" beside
	// money is a number the reader has no way to weigh; two letters of
	// provenance is the cheapest honest label there is, and the alternative —
	// spelling out the source on every row — is a column nobody could afford.
	scoreMark = "aa"
	// freeWord is a known price of zero on both sides. "$0.00/$0.00" is
	// eleven cells of noise for something a single word says, and eighteen
	// rows of the live catalog are exactly this.
	freeWord = "free"
	// perMillion is the unit the two prices are quoted in. It sits after the
	// pair rather than after each figure, because the pair is one fact.
	perMillion = " M"
)

// fillReceipts composes every model row's right column, at each of the three
// widths, with the sub-columns padded to a shared width so the block reads as a
// table down the list (§16 ALIGNMENT).
//
// A SUB-COLUMN NOBODY CAN FILL DOES NOT EXIST. If no row on this level knows a
// price, there is no price column — not a column of marks repeated four hundred
// times, which is a surface spending its width to say nothing over and over.
//
// Where SOME rows know and others do not, what the ignorant rows draw depends
// on WHAT KIND of fact is missing, and the two kinds are not the same absence:
//
//   - A price and a context window are properties every model HAS. The provider
//     merely declined to publish one, and the reader needs to see that it is
//     the provider who was silent — so those cells draw [tokens.GlyphMissing]
//     (10.2.8), the product's mark for a fact that exists and did not arrive.
//   - A benchmark score is something a third party either did or did not do.
//     An unscored model is not a model with a missing score, and three hundred
//     dashes in that column would invent an expectation nobody has — so it
//     draws nothing at all and keeps its width, which is §16's other spelling
//     of absence.
func fillReceipts(models []row) {
	// Which columns mark their gaps, in the order the columns are drawn.
	markAbsent := [receiptSteps]bool{true, true, false}
	var cells [receiptSteps][]string
	var widths [receiptSteps]int
	known := [receiptSteps]bool{}
	for i := range models {
		r := &models[i]
		values := [receiptSteps]string{
			priceWord(r.option.Price),
			windowWord(r.option.Window),
			scoreWord(r.option.Intelligence),
		}
		for col, value := range values {
			if value == "" && markAbsent[col] {
				value = tokens.GlyphMissing
			} else if value != "" {
				known[col] = true
			}
			cells[col] = append(cells[col], value)
			if w := blocks.Width(value); w > widths[col] {
				widths[col] = w
			}
		}
	}

	// The columns that survive, left to right. Dropping a step drops the
	// rightmost survivor, so price is the last thing standing.
	live := make([]int, 0, receiptSteps)
	for col := range known {
		if known[col] {
			live = append(live, col)
		}
	}
	for i := range models {
		r := &models[i]
		for step := range r.right {
			keep := len(live) - step
			if keep < 1 {
				keep = 1
			}
			r.right[step] = joinReceipt(cells, widths, live[:keep], i)
		}
	}
}

// joinReceipt lays one row's surviving columns out at their shared widths. A
// column this row cannot fill still spends its cells, because the point of the
// width is that the column below it starts in the same place.
func joinReceipt(cells [receiptSteps][]string, widths [receiptSteps]int, live []int, at int) string {
	var out strings.Builder
	for i, col := range live {
		if i > 0 {
			out.WriteString(spaces(receiptGap))
		}
		value := cells[col][at]
		out.WriteString(spaces(widths[col] - blocks.Width(value)))
		out.WriteString(value)
	}
	return out.String()
}

// priceWord is a model's economics in one cell: "$0.20/$0.80 M" for a priced
// model, "free" for a known zero, and NOTHING for a price nobody published.
//
// The empty answer is the important one. A router whose price depends on where
// it routes publishes "-1", an unwired catalog publishes nothing at all, and
// both used to arrive here as 0.0 — which the money rungs render "$0.00", and
// "$0.00" does not read as "unknown", it reads as free (§16 MONEY, and the
// finding behind [tokens.AppendMoney]'s sub-cent rung). Absence is the only
// honest thing to draw for a number nobody has.
func priceWord(p Price) string {
	if !p.Known {
		return ""
	}
	if p.free() {
		return freeWord
	}
	return tokens.Money(p.In) + "/" + tokens.Money(p.Out) + perMillion
}

// scoreWord is the published intelligence index, marked with its source. Zero
// is nobody's score rather than a score of zero, and renders as nothing.
func scoreWord(score float64) string {
	if score <= 0 {
		return ""
	}
	return scoreMark + " " + strconv.FormatFloat(score, 'f', 1, 64)
}

// windowWord is a model's context length as one short cell. Zero is the empty
// answer — a window nobody has read is not a window of nothing (10.2.8), and
// rendering "0" would make the cheapest-looking model the one whose catalog
// entry is simply stale. [fillReceipts] turns the emptiness into
// [tokens.GlyphMissing] where the column exists at all.
func windowWord(window int64) string {
	if window <= 0 {
		return ""
	}
	return tokens.Count(window)
}
