package crewroute

import (
	"fmt"
	"strconv"
	"strings"
)

// WHICH MODELS THE CREW MAY BE PICKED FROM — ONE RULE, WRITTEN ONCE.
//
// The /crew panel says what is allowed and the router picks inside it, and
// this is the rule's grammar and its one reading. It is a RULE rather than a
// list wherever it can be, because a rule keeps meaning what it meant when the
// catalog moves: `open` admits tomorrow's open-weight release the day it is
// listed, and a list of today's ids would need somebody to notice it.
//
//	all                       everything a connected provider can reach (the default)
//	open                      open-weight models only
//	≤1/5                      at most $1 per million tokens in and $5 out (`<=1/5` too)
//	glm-5.3-flash, kimi-k3    exactly these models — an id, a model's own name
//	                          after its vendor, or a vendor or provider name
//
// Any of the four may be followed by `+x` and `-x`, read left to right, the
// last word about a model winning: `open -deepseek` is every open model but
// DeepSeek's, `≤1/5 +moonshotai/kimi-k3` is the price rule with one exception
// let in. A `+x` or `-x` naming a PROVIDER — `-openrouter` — takes that
// provider's routes away rather than models, so a model it serves stays
// allowed on any other route that reaches it.

// Base is the first word of a rule: what is allowed before any `+x` or `-x`.
type Base string

const (
	BaseAll   Base = "all"
	BaseOpen  Base = "open"
	BasePrice Base = "price"
	BaseList  Base = "list"
)

// DefaultAllowed is the rule a profile that never answered reads.
const DefaultAllowed = "all"

// Allowed is a parsed rule.
type Allowed struct {
	Base Base
	// MaxIn and MaxOut are the price rule's two ceilings, dollars per million
	// tokens. Zero on every other base.
	MaxIn  float64
	MaxOut float64
	// Members are an explicit list's words.
	Members []string
	// Mods are the `+x` / `-x` words, in the order they were written.
	Mods []Mod
}

// Mod is one `+x` or `-x`.
type Mod struct {
	Add  bool
	Word string
}

// ParseAllowed reads a rule. An empty rule is [DefaultAllowed]. A rule that
// does not parse is refused with the reason, because a typo that silently
// allowed everything would be a setting somebody thinks is protecting them.
func ParseAllowed(raw string) (Allowed, error) {
	words := splitRule(raw)
	if len(words) == 0 {
		return Allowed{Base: BaseAll}, nil
	}
	var rule Allowed
	first := strings.ToLower(words[0])
	rest := words
	switch {
	case first == string(BaseAll):
		rule.Base, rest = BaseAll, words[1:]
	case first == string(BaseOpen):
		rule.Base, rest = BaseOpen, words[1:]
	case strings.HasPrefix(first, "≤") || strings.HasPrefix(first, "<="):
		in, out, err := parsePriceRule(first)
		if err != nil {
			return Allowed{}, err
		}
		rule.Base, rule.MaxIn, rule.MaxOut, rest = BasePrice, in, out, words[1:]
	case strings.HasPrefix(first, "+") || strings.HasPrefix(first, "-"):
		// A rule that opens with a modifier modifies everything: `-openrouter`
		// alone is all, minus one provider.
		rule.Base = BaseAll
	default:
		rule.Base = BaseList
	}
	for _, word := range rest {
		switch {
		case strings.HasPrefix(word, "+") && len(word) > 1:
			rule.Mods = append(rule.Mods, Mod{Add: true, Word: strings.ToLower(word[1:])})
		case strings.HasPrefix(word, "-") && len(word) > 1:
			rule.Mods = append(rule.Mods, Mod{Add: false, Word: strings.ToLower(word[1:])})
		case rule.Base == BaseList:
			rule.Members = append(rule.Members, strings.ToLower(word))
		default:
			return Allowed{}, fmt.Errorf("%q: after %s, name a model or provider with + or -", word, rule.Base)
		}
	}
	return rule, nil
}

// splitRule reads a rule's words: spaces and commas both separate them.
func splitRule(raw string) []string {
	return strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}

// parsePriceRule reads `≤1/5` or `<=1/5`: two ceilings, dollars per million.
func parsePriceRule(word string) (in, out float64, err error) {
	body := strings.TrimPrefix(strings.TrimPrefix(word, "≤"), "<=")
	body = strings.ReplaceAll(body, "$", "")
	parts := strings.Split(body, "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%q: a price rule is ≤<in>/<out> dollars per million tokens, like ≤1/5", word)
	}
	in, errIn := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	out, errOut := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errIn != nil || errOut != nil || in < 0 || out < 0 {
		return 0, 0, fmt.Errorf("%q: a price rule is ≤<in>/<out> dollars per million tokens, like ≤1/5", word)
	}
	return in, out, nil
}

// String writes the rule back in its canonical spelling, which is what the
// panel shows and what is stored.
func (a Allowed) String() string {
	var words []string
	switch a.Base {
	case BaseOpen:
		words = append(words, string(BaseOpen))
	case BasePrice:
		words = append(words, "≤"+trimFloat(a.MaxIn)+"/"+trimFloat(a.MaxOut))
	case BaseList:
		words = append(words, strings.Join(a.Members, ", "))
	default:
		if len(a.Mods) == 0 {
			return string(BaseAll)
		}
		words = append(words, string(BaseAll))
	}
	for _, mod := range a.Mods {
		sign := "-"
		if mod.Add {
			sign = "+"
		}
		words = append(words, sign+mod.Word)
	}
	return strings.Join(words, " ")
}

// trimFloat spells a ceiling without trailing zeros.
func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// With is the rule with one more `+x` or `-x` on its end — what `/crew models
// +x` writes. A word the rule already carries with the same sign is not
// written twice, and the same word with the other sign is replaced, so the
// rule stays the shortest spelling of what the person meant.
func (a Allowed) With(add bool, word string) Allowed {
	word = strings.ToLower(strings.TrimSpace(word))
	out := a
	out.Mods = nil
	for _, mod := range a.Mods {
		if mod.Word != word {
			out.Mods = append(out.Mods, mod)
		}
	}
	out.Mods = append(out.Mods, Mod{Add: add, Word: word})
	return out
}

// AdmitsModel says whether the rule allows a model, before routes are asked.
func (a Allowed) AdmitsModel(m Model) bool {
	admitted := false
	switch a.Base {
	case BaseAll, "":
		admitted = true
	case BaseOpen:
		admitted = m.Open
	case BasePrice:
		// Prices are per token in the catalog; the rule is per million.
		admitted = m.PromptPrice*1e6 <= a.MaxIn+1e-9 && m.CompletionPrice*1e6 <= a.MaxOut+1e-9
	case BaseList:
		for _, member := range a.Members {
			if matchesModel(member, m.ID) {
				admitted = true
				break
			}
		}
	}
	for _, mod := range a.Mods {
		if matchesModel(mod.Word, m.ID) {
			admitted = mod.Add
		}
	}
	return admitted
}

// AdmitsRoute says whether the rule allows a provider's routes. Only a `-x`
// naming the provider takes them away; a later `+x` gives them back.
func (a Allowed) AdmitsRoute(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	admitted := true
	for _, mod := range a.Mods {
		if mod.Word == provider {
			admitted = mod.Add
		}
	}
	return admitted
}

// NamesModel says whether the rule speaks about this model by name — a list
// member or a modifier matching it. It is how a pin outside a price or open
// rule can still be let in by name.
func (a Allowed) NamesModel(id string) bool {
	for _, member := range a.Members {
		if matchesModel(member, id) {
			return true
		}
	}
	for _, mod := range a.Mods {
		if mod.Add && matchesModel(mod.Word, id) {
			return true
		}
	}
	return false
}

// matchesModel reads one rule word against one model id: the whole id, the
// model's own name after its vendor, or the vendor itself. The comparison is
// on lineage, so `deepseek/deepseek-v4-flash` names the dated build too.
func matchesModel(word, id string) bool {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return false
	}
	full := Lineage(id)
	if Lineage(word) == full {
		return true
	}
	vendor, tail := full, full
	if slash := strings.Index(full, "/"); slash >= 0 {
		vendor, tail = full[:slash], full[slash+1:]
	}
	return word == vendor || Lineage(word) == tail
}

// ── THE PANEL'S EDITS, EACH THE SHORTEST RULE THAT SAYS IT ──
//
// The /crew panel edits the rule a toggle at a time — a model ticked on or off
// in its checklist, a provider taken away, the base walked from `all` to
// `open` — and every one of those writes the rule back through the same one
// writer the typed form uses. What these add is the arithmetic of saying the
// change in as few words as possible, because the rule is SHOWN: a checklist
// that left `+x -x +x` behind after three presses would be a panel writing a
// rule nobody could read back.

// Custom says whether the rule is more than one of the three plain bases: an
// explicit list, or a base with exceptions on it. It is the fourth answer the
// panel's models row walks between, and the one its checklist edits.
func (a Allowed) Custom() bool {
	return a.Base == BaseList || len(a.Mods) > 0
}

// Rebased is the rule on a new base with nothing carried over — the `‹ open ›`
// the panel's row steps to is `open`, and not `open` with whatever exceptions
// the rule it stepped from happened to have. The price ceilings are taken for
// [BasePrice] and ignored for every other base.
func (a Allowed) Rebased(base Base, maxIn, maxOut float64) Allowed {
	out := Allowed{Base: base}
	if base == BasePrice {
		out.MaxIn, out.MaxOut = maxIn, maxOut
	}
	return out
}

// Without is the rule with every `+x` or `-x` naming word taken off, which
// hands that word's verdict back to the base.
func (a Allowed) Without(word string) Allowed {
	word = strings.ToLower(strings.TrimSpace(word))
	out := a
	out.Mods = nil
	for _, mod := range a.Mods {
		if mod.Word != word {
			out.Mods = append(out.Mods, mod)
		}
	}
	return out
}

// Toggled is the rule with one model's verdict flipped to admit, in the
// shortest spelling: an explicit list gains or loses a member, and any other
// base loses the exception it already had for the model when that alone gives
// the wanted verdict, or gains one when it does not. It is what a tick in the
// panel's checklist writes.
//
// AN EXPLICIT LIST MAY NOT BE EMPTIED. `a, b` less both is not a rule the
// grammar can spell — an empty rule reads as `all`, which is the opposite of
// what unticking the last model means — so the second answer is false and the
// rule comes back as it was.
func (a Allowed) Toggled(m Model, admit bool) (Allowed, bool) {
	word := strings.ToLower(strings.TrimSpace(m.ID))
	if a.Base == BaseList {
		out := a.Without(word)
		var members []string
		for _, member := range a.Members {
			if !matchesModel(member, m.ID) {
				members = append(members, member)
			}
		}
		if admit {
			members = append(members, word)
		}
		if len(members) == 0 {
			return a, false
		}
		out.Members = members
		if out.AdmitsModel(m) != admit {
			// A vendor-wide exception outranks the member: say this model by name
			// after it, which is the last word and therefore the verdict.
			out = out.With(admit, word)
		}
		return out, true
	}
	out := a.Without(word)
	if out.AdmitsModel(m) != admit {
		out = out.With(admit, word)
	}
	return out, true
}

// RouteToggled is the rule with one provider's routes given back or taken
// away, in the shortest spelling, the way [Allowed.Toggled] says a model.
func (a Allowed) RouteToggled(provider string, admit bool) Allowed {
	out := a.Without(provider)
	if out.AdmitsRoute(provider) != admit {
		out = out.With(admit, provider)
	}
	return out
}
