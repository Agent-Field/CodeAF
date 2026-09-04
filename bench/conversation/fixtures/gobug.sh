#!/usr/bin/env bash
# gobug.sh — the coding workload's fixture: a small Go module with one real bug
# and a test that already catches it.
#
# Deterministic and offline: standard library only, and a `go` directive old
# enough that no toolchain is fetched. The suite the harness has to satisfy is
# written BEFORE the harness runs and is checked for tampering afterwards —
# "made the tests pass" and "made the tests agree" are different outcomes and
# only a checksum tells them apart.
#
# The bug is a boundary, not a typo: tiered pricing where the top tier's
# threshold is compared with > instead of >=, so exactly-at-the-boundary orders
# are billed at the tier below. It cannot be found by reading the failing line
# alone; the fix is one character in a different function than the one the test
# names.

fixture_gobug() {
  local work="$1"
  mkdir -p "$work"
  cat > "$work/go.mod" <<'MOD'
module bench.example/pricing

go 1.21
MOD

  cat > "$work/pricing.go" <<'GO'
package pricing

// Tier is a volume band: orders of at least Min units are billed at Price per
// unit, and the highest band whose Min is reached wins.
type Tier struct {
	Min   int
	Price float64
}

// Tiers are ordered from smallest to largest band.
var Tiers = []Tier{
	{Min: 0, Price: 10.0},
	{Min: 50, Price: 8.0},
	{Min: 100, Price: 6.5},
}

// PriceFor returns the per-unit price for an order of n units.
func PriceFor(n int) float64 {
	price := Tiers[0].Price
	for _, tier := range Tiers {
		if n > tier.Min {
			price = tier.Price
		}
	}
	return price
}

// Total bills an order of n units.
func Total(n int) float64 {
	return float64(n) * PriceFor(n)
}
GO

  cat > "$work/pricing_test.go" <<'GO'
package pricing

import "testing"

func TestPriceForBands(t *testing.T) {
	cases := []struct {
		units int
		want  float64
	}{
		{1, 10.0},
		{49, 10.0},
		{50, 8.0},
		{99, 8.0},
		{100, 6.5},
		{250, 6.5},
	}
	for _, c := range cases {
		if got := PriceFor(c.units); got != c.want {
			t.Errorf("PriceFor(%d) = %v, want %v", c.units, got, c.want)
		}
	}
}

func TestTotalAtBandEdge(t *testing.T) {
	if got := Total(50); got != 400 {
		t.Errorf("Total(50) = %v, want 400", got)
	}
}
GO
}

# fixture_gobug_checksum is the guard against a harness that deletes the
# question instead of answering it.
fixture_gobug_checksum() {
  shasum -a 256 "$1/pricing_test.go" 2>/dev/null | cut -d' ' -f1
}
