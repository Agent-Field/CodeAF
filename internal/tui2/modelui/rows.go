package modelui

import (
	"github.com/Agent-Field/aforge-v2/internal/store"
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

	// right is the right-aligned column: a provenance word, or a context
	// window. A disabled row advertises none of it, for the same reason the
	// palette drops a disabled row's accelerator — a column beside a reason it
	// cannot be used is the row lying about itself.
	right string

	// disabled is why this row cannot be chosen, in the wiring's words.
	disabled string

	// result is what choosing the row emits. Nil with descend set is a role
	// row, which opens the level beneath it instead of finishing.
	result  Result
	descend bool
	role    store.ModelRole
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
			verb:    role.Word(),
			hint:    roleHint(role),
			right:   provenanceWord(binding.Source),
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
	for _, model := range c.Models {
		if model.Slug == "" {
			continue
		}
		reason := model.Disabled
		if reason == "" {
			reason = c.Disabled
		}
		item := row{
			verb:     ModelWord(model.Slug),
			hint:     model.Note,
			right:    windowWord(model.Window),
			result:   SetRole{Role: role, ModelSlug: model.Slug, Scope: scope},
			disabled: reason,
		}
		if model.Slug == binding.Model {
			// The one that is running now. ✓ is the settled glyph and it means
			// the same thing here as it does on a card: this is the state, not
			// a recommendation.
			item.glyph, item.glyphTok = tokens.GlyphSettled, tokens.Green
		}
		item.lowerVerb, item.lowerHint = lower(item.verb), lower(item.hint)
		dst = append(dst, item)
	}
	return dst
}

// windowWord is a model's context length as one short cell. Zero is
// [tokens.GlyphMissing]: a window nobody has read is not a window of nothing
// (10.2.8), and rendering "0" would make the cheapest-looking model the one
// whose catalog entry is simply stale.
func windowWord(window int64) string {
	if window <= 0 {
		return tokens.GlyphMissing
	}
	return tokens.Count(window)
}
