package catalog

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/filememo"
)

// A SERVICE'S OWN PRICES, FROM ITS OWN LISTING — AND NOTHING ELSE.
//
// The default service's catalog prices every model it lists, and those are its
// own rates: they say nothing about what another service charges for the same
// model. A connection's own listing is the only evidence of ITS price, and only
// when that listing published one. These facts are read from the listing this
// install already cached for the service — nothing is fetched to answer — and
// a cache file is parsed once per change to it, so a router that asks on every
// task pays a stat.

// PriceFact is what one service's own listing said about one model's price,
// dollars per token. Known false is "it did not say": never read as free. A
// listing that published an explicit zero is a known price of zero.
type PriceFact struct {
	Known      bool
	Prompt     float64
	Completion float64
	Request    float64
	CacheRead  float64
}

// ownPrices is one cache file's price facts, with the compartment it was
// written for so a memo held for one service is never answered for another.
type ownPrices struct {
	source, base string
	facts        map[string]PriceFact
}

var ownPriceMemo = filememo.New(func(_ string, data []byte, missing bool) (ownPrices, error) {
	if missing || len(data) == 0 {
		return ownPrices{}, nil
	}
	return ownPricesOf(data), nil
})

// ownPricesOf reads a cache's rows into facts; ownership is checked by the
// caller against the compartment it asked for.
func ownPricesOf(data []byte) ownPrices {
	out := ownPrices{facts: map[string]PriceFact{}}
	cached, ok := decodeCacheAny(data)
	if !ok {
		return out
	}
	out.source, out.base = strings.TrimSpace(cached.Source), normalizeBase(cached.Base)
	for _, m := range cached.Models {
		fact := PriceFact{}
		if cached.Priced && !m.PriceUnknown {
			fact = PriceFact{Known: true, Prompt: m.PromptPrice, Completion: m.CompletionPrice, Request: m.RequestPrice, CacheRead: m.CacheReadPrice}
		}
		out.facts[strings.ToLower(m.ID)] = fact
	}
	return out
}

// OwnPrices is one service's own price facts by lower-cased model id, read
// from the listing cached for that service and base. A service with no cached
// listing answers an empty map; a model it lists without a price answers a
// fact that is not Known.
func OwnPrices(options Options) map[string]PriceFact {
	source := strings.TrimSpace(options.Source)
	base := normalizeBase(options.BaseURL)
	path := cachePath(options.Dir, source, base)
	if path == "" {
		return nil
	}
	held, _ := ownPriceMemo.Read(path)
	if held.facts == nil || held.source != source || held.base != base {
		return nil
	}
	return held.facts
}
