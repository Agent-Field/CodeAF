#!/usr/bin/env bash
# promoprice — the Go fixture the bughunt and feature cells share.
#
# A nine-file pricing service with one deliberate property: the price of a SKU
# depends on the customer's Tier, and the memo cache in cache/cache.go keys on
# the SKU alone. Whoever asks second for the same SKU on a different tier is
# served the first tier's price.
#
# The bug is *distant* from its symptom on purpose. The failing test lives in
# server/handler_test.go — two HTTP-ish requests, gold then silver — and the
# cause is a one-line key three packages away. The passing test in
# pricing/price_test.go builds a fresh cache per case, so it never crosses tiers
# and never sees it: a suite that is green on the wrong code is what makes this
# a search rather than a lookup.
#
# Everything is stdlib. `go test ./...` needs no network, and the go directive is
# deliberately old so no toolchain download is triggered on a metered machine.
#
# Usage: promoprice.sh <dir> <mode>      mode = bug | clean
#   bug    plants the tier-blind cache key and ships the failing handler test
#   clean  the same tree, correct key, suite green — the feature cell's base
set -euo pipefail

DIR="${1:?usage: promoprice.sh <dir> <bug|clean>}"
MODE="${2:-bug}"

case "$MODE" in
  bug|clean) ;;
  *) echo "promoprice: mode must be bug or clean, got $MODE" >&2; exit 1 ;;
esac

rm -rf "$DIR"
mkdir -p "$DIR/catalog" "$DIR/pricing" "$DIR/cache" "$DIR/internal/round" "$DIR/server" "$DIR/cmd/promoprice"

cat > "$DIR/go.mod" <<'EOF'
module promoprice

go 1.21
EOF

cat > "$DIR/catalog/catalog.go" <<'EOF'
// Package catalog is the list price of every SKU, in cents.
package catalog

import "fmt"

var base = map[string]int{
	"widget-1": 1000,
	"widget-2": 2500,
	"gizmo-9":  4999,
}

// BaseCents is the list price before any tier discount or promotion.
func BaseCents(sku string) (int, error) {
	cents, ok := base[sku]
	if !ok {
		return 0, fmt.Errorf("catalog: no such sku %q", sku)
	}
	return cents, nil
}

// SKUs is every sku the catalog knows, for callers that enumerate.
func SKUs() []string { return []string{"widget-1", "widget-2", "gizmo-9"} }
EOF

cat > "$DIR/pricing/tier.go" <<'EOF'
package pricing

import "fmt"

// Tier is what the customer has earned. It scales the list price down.
type Tier string

const (
	TierStandard Tier = "standard"
	TierSilver   Tier = "silver"
	TierGold     Tier = "gold"
)

var multipliers = map[Tier]float64{
	TierStandard: 1.00,
	TierSilver:   0.90,
	TierGold:     0.75,
}

// Multiplier is the fraction of list price this tier pays.
func Multiplier(tier Tier) (float64, error) {
	value, ok := multipliers[tier]
	if !ok {
		return 0, fmt.Errorf("pricing: unknown tier %q", tier)
	}
	return value, nil
}

// Tiers is every tier, in increasing order of discount.
func Tiers() []Tier { return []Tier{TierStandard, TierSilver, TierGold} }

// ParseTier turns request text into a Tier.
func ParseTier(text string) (Tier, error) {
	tier := Tier(text)
	if _, err := Multiplier(tier); err != nil {
		return "", err
	}
	return tier, nil
}
EOF

cat > "$DIR/pricing/promo.go" <<'EOF'
package pricing

// A promotion is a flat cents-off applied after the tier multiplier.
var promos = map[string]int{
	"widget-2": 100,
}

// PromoCents is the flat discount for a sku, or zero when it is not on promo.
func PromoCents(sku string) int { return promos[sku] }
EOF

cat > "$DIR/internal/round/round.go" <<'EOF'
// Package round holds the one money rule: never invent a fraction of a cent,
// and never let a discount take a price below zero.
package round

import "math"

// Cents rounds a computed price to whole cents, half away from zero, and
// clamps at zero.
func Cents(value float64) int {
	if value <= 0 {
		return 0
	}
	return int(math.Round(value))
}
EOF

if [ "$MODE" = "bug" ]; then
  cat > "$DIR/cache/cache.go" <<'EOF'
// Package cache memoises computed prices so a hot SKU is priced once.
package cache

import "sync"

// Key is the cache identity of a priced request.
func Key(sku string) string {
	return sku
}

// Cache is a tiny concurrent memo table.
type Cache struct {
	mu      sync.Mutex
	entries map[string]int
	hits    int
	misses  int
}

// New builds an empty cache.
func New() *Cache { return &Cache{entries: map[string]int{}} }

// Get returns a memoised price, if one was stored under this key.
func (c *Cache) Get(key string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.entries[key]
	if ok {
		c.hits++
	} else {
		c.misses++
	}
	return value, ok
}

// Put memoises a price under this key.
func (c *Cache) Put(key string, cents int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cents
}

// Stats reports hits and misses, for the handler's debug line.
func (c *Cache) Stats() (hits, misses int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}
EOF
else
  cat > "$DIR/cache/cache.go" <<'EOF'
// Package cache memoises computed prices so a hot SKU is priced once.
package cache

import "sync"

// Key is the cache identity of a priced request. Every dimension that moves
// the price has to appear here or two different prices collide on one entry.
func Key(sku, tier string) string {
	return sku + "|" + tier
}

// Cache is a tiny concurrent memo table.
type Cache struct {
	mu      sync.Mutex
	entries map[string]int
	hits    int
	misses  int
}

// New builds an empty cache.
func New() *Cache { return &Cache{entries: map[string]int{}} }

// Get returns a memoised price, if one was stored under this key.
func (c *Cache) Get(key string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.entries[key]
	if ok {
		c.hits++
	} else {
		c.misses++
	}
	return value, ok
}

// Put memoises a price under this key.
func (c *Cache) Put(key string, cents int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cents
}

// Stats reports hits and misses, for the handler's debug line.
func (c *Cache) Stats() (hits, misses int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}
EOF
fi

if [ "$MODE" = "bug" ]; then
  KEYCALL='cache.Key(sku)'
else
  KEYCALL='cache.Key(sku, string(tier))'
fi

cat > "$DIR/pricing/price.go" <<EOF
// Package pricing turns a sku and a customer tier into a price in cents.
package pricing

import (
	"promoprice/cache"
	"promoprice/catalog"
	"promoprice/internal/round"
)

// Pricer prices SKUs, memoising what it has already computed.
type Pricer struct {
	memo *cache.Cache
}

// NewPricer builds a pricer with an empty memo.
func NewPricer() *Pricer { return &Pricer{memo: cache.New()} }

// PriceCents is the price this tier pays for this sku, after the tier
// multiplier and any flat promotion, rounded to whole cents.
func (p *Pricer) PriceCents(sku string, tier Tier) (int, error) {
	key := $KEYCALL
	if cents, ok := p.memo.Get(key); ok {
		return cents, nil
	}
	base, err := catalog.BaseCents(sku)
	if err != nil {
		return 0, err
	}
	multiplier, err := Multiplier(tier)
	if err != nil {
		return 0, err
	}
	cents := round.Cents(float64(base)*multiplier - float64(PromoCents(sku)))
	p.memo.Put(key, cents)
	return cents, nil
}

// Stats exposes the memo's hit and miss counts.
func (p *Pricer) Stats() (hits, misses int) { return p.memo.Stats() }
EOF

cat > "$DIR/server/handler.go" <<'EOF'
// Package server is the request-shaped entry point onto the pricer. It is a
// plain struct rather than net/http so the suite needs no ports.
package server

import (
	"fmt"

	"promoprice/pricing"
)

// Request is one quote request.
type Request struct {
	SKU  string
	Tier string
}

// Response is one quote.
type Response struct {
	SKU   string
	Tier  string
	Cents int
}

// Server answers quote requests, sharing one pricer across them.
type Server struct {
	pricer *pricing.Pricer
}

// New builds a server with a fresh pricer.
func New() *Server { return &Server{pricer: pricing.NewPricer()} }

// Quote prices one request.
func (s *Server) Quote(request Request) (Response, error) {
	tier, err := pricing.ParseTier(request.Tier)
	if err != nil {
		return Response{}, err
	}
	cents, err := s.pricer.PriceCents(request.SKU, tier)
	if err != nil {
		return Response{}, err
	}
	return Response{SKU: request.SKU, Tier: request.Tier, Cents: cents}, nil
}

// Format renders a quote the way the CLI prints it.
func Format(response Response) string {
	return fmt.Sprintf("%s %s %d", response.SKU, response.Tier, response.Cents)
}
EOF

cat > "$DIR/cmd/promoprice/main.go" <<'EOF'
// Command promoprice quotes one sku at one tier.
package main

import (
	"flag"
	"fmt"
	"os"

	"promoprice/server"
)

func main() {
	sku := flag.String("sku", "widget-1", "the sku to quote")
	tier := flag.String("tier", "standard", "the customer tier")
	flag.Parse()

	response, err := server.New().Quote(server.Request{SKU: *sku, Tier: *tier})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(server.Format(response))
}
EOF

# The suite that is already green, and stays green. Each case builds its own
# pricer, so no case ever crosses tiers on one memo — which is exactly why this
# file does not catch the planted bug.
cat > "$DIR/pricing/price_test.go" <<'EOF'
package pricing

import "testing"

func TestPriceCentsPerTier(t *testing.T) {
	cases := []struct {
		sku  string
		tier Tier
		want int
	}{
		{"widget-1", TierStandard, 1000},
		{"widget-1", TierSilver, 900},
		{"widget-1", TierGold, 750},
		{"widget-2", TierStandard, 2400},
		{"gizmo-9", TierGold, 3749},
	}
	for _, testCase := range cases {
		pricer := NewPricer()
		got, err := pricer.PriceCents(testCase.sku, testCase.tier)
		if err != nil {
			t.Fatalf("%s/%s: %v", testCase.sku, testCase.tier, err)
		}
		if got != testCase.want {
			t.Errorf("%s/%s = %d, want %d", testCase.sku, testCase.tier, got, testCase.want)
		}
	}
}

func TestUnknownSKUAndTier(t *testing.T) {
	pricer := NewPricer()
	if _, err := pricer.PriceCents("nope", TierGold); err == nil {
		t.Error("unknown sku priced without error")
	}
	if _, err := pricer.PriceCents("widget-1", Tier("platinum")); err == nil {
		t.Error("unknown tier priced without error")
	}
}

func TestMemoIsUsed(t *testing.T) {
	pricer := NewPricer()
	for i := 0; i < 3; i++ {
		if _, err := pricer.PriceCents("widget-1", TierGold); err != nil {
			t.Fatal(err)
		}
	}
	hits, misses := pricer.Stats()
	if misses != 1 || hits < 2 {
		t.Errorf("memo hits=%d misses=%d, want one miss and repeat hits", hits, misses)
	}
}
EOF

# The suite that fails on the bug fixture and passes on the clean one: one
# server, one sku, two tiers, in that order.
cat > "$DIR/server/handler_test.go" <<'EOF'
package server

import "testing"

func TestQuoteAcrossTiersOnOneServer(t *testing.T) {
	srv := New()
	gold, err := srv.Quote(Request{SKU: "widget-1", Tier: "gold"})
	if err != nil {
		t.Fatal(err)
	}
	if gold.Cents != 750 {
		t.Fatalf("gold widget-1 = %d, want 750", gold.Cents)
	}
	silver, err := srv.Quote(Request{SKU: "widget-1", Tier: "silver"})
	if err != nil {
		t.Fatal(err)
	}
	if silver.Cents != 900 {
		t.Errorf("silver widget-1 = %d, want 900 (a gold quote came first on the same server)", silver.Cents)
	}
}

func TestQuoteEveryTierIsDistinct(t *testing.T) {
	srv := New()
	seen := map[int]string{}
	for _, tier := range []string{"standard", "silver", "gold"} {
		response, err := srv.Quote(Request{SKU: "gizmo-9", Tier: tier})
		if err != nil {
			t.Fatal(err)
		}
		if other, clash := seen[response.Cents]; clash {
			t.Errorf("tier %s priced %d, same as tier %s", tier, response.Cents, other)
		}
		seen[response.Cents] = tier
	}
}

func TestFormat(t *testing.T) {
	got := Format(Response{SKU: "widget-1", Tier: "gold", Cents: 750})
	if got != "widget-1 gold 750" {
		t.Errorf("Format = %q", got)
	}
}
EOF

echo "$DIR"
