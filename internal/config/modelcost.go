package config

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

// ModelCostHint is what the provider tables already know about one model's
// price, said in the fewest true words — the dim receipt beside a model row
// (13: every row carries its own money, and a row that cannot know renders
// nothing rather than a zero).
//
// Two tables answer, in this order, because they answer different questions.
// The operator's panel ([router.Spec.Role]) is a claim a human wrote down —
// base, mid, top — and a human's own word about their own panel outranks a
// number scraped from a price list. Failing that the catalog's completion price
// is a real number: output tokens are what a conversation actually spends, and
// one price beside a row is a hint where two would be a table.
//
// Neither table is a capability check and neither is a filter. An unpriced
// model is the offline case — the catalog fetch is deliberately soft — and the
// honest rendering of "nobody told us" is silence.
func ModelCostHint(panel router.Panel, models *catalog.Catalog, slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	if word := panelTierWord(panel, slug); word != "" {
		return word
	}
	if models == nil {
		return ""
	}
	model, ok := models.Model(slug)
	if !ok {
		return ""
	}
	perMillion := model.CompletionPrice * 1_000_000
	if perMillion <= 0 {
		return ""
	}
	return "$" + trimZeros(strconv.FormatFloat(perMillion, 'f', 2, 64)) + "/M out"
}

// panelTierWord finds the operator's own word for this model. Slug comparison
// tolerates OpenRouter's floating-alias tilde for the same reason
// router.Catalog.Entry does: the panel and the configured slot are written by
// different hands and one of them usually carries it.
func panelTierWord(panel router.Panel, slug string) string {
	// A thinking level comes off for the same reason the tilde does: it is how a
	// tier row says how hard to ask, not which model, and a panel spec is
	// written without one (roles.SplitEffort, and catalog's own normalizeID).
	bare, _ := roles.SplitEffort(slug)
	want := strings.TrimPrefix(bare, "~")
	for _, spec := range panel.Models {
		if strings.TrimPrefix(strings.TrimSpace(spec.Slug), "~") != want {
			continue
		}
		return strings.ToLower(strings.TrimSpace(spec.Role))
	}
	return ""
}

// trimZeros drops a trailing .00 and a trailing 0, so a price reads as $3/M
// rather than $3.00/M and $1.5 rather than $1.50. Money is rendered small
// everywhere else in the product for the same reason.
func trimZeros(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	return strings.TrimSuffix(value, ".")
}
