package modelui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The economics half of the models level: what a row says about a model beyond
// its name, and what it refuses to say.
//
// Every fixture here is shaped like the live OpenRouter catalog rather than
// like a convenient list — four hundred rows, a dozen families, prices that
// span four orders of magnitude, and the three shapes that used to be one:
// a published price, a published price of zero, and no published price at all.

// pricedCatalog is a handful of rows with every receipt fact filled in.
func pricedCatalog() Catalog {
	return Catalog{
		Roles: []RoleRow{{Role: store.RoleWork, Model: "openai/gpt-oss-120b", Source: store.RoleFromGlobal}},
		Models: []ModelOption{
			{Slug: "anthropic/claude-opus-5", Name: "Claude Opus 5", Window: 1_000_000,
				Price: Price{In: 5, Out: 25, Known: true}, Intelligence: 63.1},
			{Slug: "openai/gpt-oss-120b", Name: "GPT OSS 120B", Window: 131_072,
				Price: Price{In: 0.2, Out: 0.8, Known: true}},
			{Slug: "nvidia/nemotron-3.5-lightning:free", Name: "NVIDIA: Nemotron 3.5 Lightning (free)",
				Window: 1_000_000, Price: Price{Known: true}},
			// OpenRouter's own router: it publishes "-1", which the catalog
			// reads as "nobody knows".
			{Slug: "openrouter/auto", Name: "Auto Router", Window: 2_000_000},
		},
	}
}

func modelRows(t *testing.T, p *Picker, width, height int) []string {
	t.Helper()
	rendered := lines(p, width, height)
	if len(rendered) <= p.bodyTop {
		t.Fatalf("nothing was drawn below the chrome:\n%s", strings.Join(rendered, "\n"))
	}
	return rendered[p.bodyTop:]
}

// The three receipts, in the order a choice turns on them.
func TestAModelRowCarriesItsPriceWindowAndPublishedScore(t *testing.T) {
	t.Parallel()
	p := newPicker(pricedCatalog())
	openRole(p, store.RoleWork)
	rendered := lines(p, 90, 12)

	opus, ok := find(rendered, "claude-opus-5")
	if !ok {
		t.Fatalf("no opus row in:\n%s", strings.Join(rendered, "\n"))
	}
	for _, want := range []string{"$5.00/$25.00 M", "1M", "aa 63.1"} {
		if !strings.Contains(opus, want) {
			t.Errorf("the row does not carry %q: %q", want, opus)
		}
	}
	// Sub-dollar prices keep the rungs' resolution rather than rounding into
	// "$0.00" (§16 MONEY).
	oss, _ := find(rendered, "gpt-oss-120b")
	if !strings.Contains(oss, "$0.20/$0.80 M") {
		t.Errorf("a cheap model's price is wrong: %q", oss)
	}
}

// The distinction the whole Price type exists for.
func TestAFreeModelSaysFreeAndAnUnpricedOneSaysNothing(t *testing.T) {
	t.Parallel()
	p := newPicker(pricedCatalog())
	openRole(p, store.RoleWork)
	rendered := lines(p, 90, 12)

	free, ok := find(rendered, "nemotron-3.5-lightning")
	if !ok {
		t.Fatalf("no free row in:\n%s", strings.Join(rendered, "\n"))
	}
	if !strings.Contains(free, freeWord) {
		t.Errorf("a known zero price does not read as free: %q", free)
	}

	router, _ := find(rendered, "auto")
	if strings.Contains(router, freeWord) {
		t.Errorf("a router with no published price is shown as free: %q", router)
	}
	if !strings.Contains(router, tokens.GlyphMissing) {
		t.Errorf("an absent price is not marked absent: %q", router)
	}
	// The failure this replaces, pinned everywhere on the level.
	for _, line := range rendered {
		if strings.Contains(line, "$0.00") {
			t.Errorf("a row renders a price as zero: %q", line)
		}
	}
}

// A column no row can fill is not a column of dashes; it is not there.
func TestAReceiptColumnNobodyCanFillDoesNotExist(t *testing.T) {
	t.Parallel()
	c := Catalog{Models: []ModelOption{
		{Slug: "a/one", Window: 128_000},
		{Slug: "b/two", Window: 32_000},
	}}
	p := newPicker(c)
	openRole(p, store.RoleWork)
	for _, line := range modelRows(t, p, 80, 8) {
		if strings.Contains(line, tokens.GlyphMissing) {
			t.Errorf("an absent column was drawn as a mark: %q", line)
		}
	}
	// The window, which every row does know, is still there.
	if row, _ := find(lines(p, 80, 8), "one"); !strings.Contains(row, "128K") {
		t.Errorf("the one column every row can fill went missing: %q", row)
	}
}

// §16 ALIGNMENT: the receipts are a table, so the sub-columns share x-positions
// down the list even where the values do not share a width.
func TestTheReceiptSubColumnsShareOneColumnPerSurface(t *testing.T) {
	t.Parallel()
	p := newPicker(pricedCatalog())
	openRole(p, store.RoleWork)
	rendered := modelRows(t, p, 90, 12)

	edge, unit, rows := -1, -1, 0
	for _, line := range rendered {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows++
		// Every model row ends at the same column, because every row's block is
		// the same width and is right-aligned against the frame.
		if width := blocks.Width(line); edge < 0 {
			edge = width
		} else if width != edge {
			t.Fatalf("rows end at columns %d and %d:\n%s", edge, width, strings.Join(rendered, "\n"))
		}
		// And the price sub-column is right-aligned INSIDE the block, so
		// "$5.00/$25.00 M" and "$0.20/$0.80 M" end together though they do not
		// start together.
		at := strings.Index(line, perMillion)
		if at < 0 {
			continue
		}
		if unit < 0 {
			unit = blocks.Width(line[:at])
		} else if got := blocks.Width(line[:at]); got != unit {
			t.Fatalf("the price column ends at %d on one row and %d on another:\n%s",
				unit, got, strings.Join(rendered, "\n"))
		}
	}
	if rows < 3 || unit < 0 {
		t.Fatalf("too few rows to compare (%d rows, price column at %d):\n%s",
			rows, unit, strings.Join(rendered, "\n"))
	}
}

// The block gives up a column at a time, cheapest fact last: the score goes
// before the window and the window goes before the price, because price is what
// the decision turns on.
func TestTheReceiptDropsItsColumnsFromTheRight(t *testing.T) {
	t.Parallel()
	p := newPicker(pricedCatalog())
	openRole(p, store.RoleWork)

	var sawAll, sawNoScore, sawPriceOnly bool
	for w := 30; w <= 100; w++ {
		row, ok := find(lines(p, w, 12), "claude-opus-5")
		if !ok {
			continue
		}
		price := strings.Contains(row, "$5.00/$25.00")
		window := strings.Contains(row, "1M")
		score := strings.Contains(row, scoreMark)
		switch {
		case score:
			if !price || !window {
				t.Fatalf("at width %d the score outlived a receipt it ranks below: %q", w, row)
			}
			sawAll = true
		case window:
			if !price {
				t.Fatalf("at width %d the window outlived the price: %q", w, row)
			}
			sawNoScore = true
		case price:
			sawPriceOnly = true
		}
	}
	if !sawAll || !sawNoScore || !sawPriceOnly {
		t.Errorf("the ladder never used all three steps: all=%v no-score=%v price-only=%v",
			sawAll, sawNoScore, sawPriceOnly)
	}
}

// -- the full catalog ----------------------------------------------------------

// bigCatalog is the shape of the real thing: three hundred models across twelve
// families, priced across four orders of magnitude, a third of them scored, and
// one row per shape that has to survive being one of three hundred.
func bigCatalog(n int) Catalog {
	families := []string{
		"ai21", "alibaba", "anthropic", "cohere", "deepseek", "google",
		"meta", "mistralai", "nvidia", "openai", "qwen", "x-ai",
	}
	models := make([]ModelOption, 0, n)
	for i := 0; i < n; i++ {
		fam := families[i%len(families)]
		opt := ModelOption{
			Slug:   fmt.Sprintf("%s/model-%03d", fam, i),
			Name:   fmt.Sprintf("%s: Model %03d", fam, i),
			Window: int64(4096 * (1 + i%64)),
			Price:  Price{In: float64(i%40) * 0.25, Out: float64(i%40) * 1.5, Known: true},
		}
		if i%3 == 0 {
			opt.Intelligence = 20 + float64(i%45)
		}
		if i%37 == 0 {
			// A row the provider published nothing about but a name.
			opt.Price, opt.Window, opt.Intelligence = Price{}, 0, 0
		}
		models = append(models, opt)
	}
	return Catalog{
		Roles:  []RoleRow{{Role: store.RoleWork, Model: models[n-1].Slug, Source: store.RoleFromGlobal}},
		Models: models,
	}
}

// The complaint this whole surface was rebuilt for: the picker must hold the
// catalog the provider publishes, and say so when it cannot show all of it.
func TestTheWholeCatalogIsHeldAndTheHiddenCountIsHonest(t *testing.T) {
	t.Parallel()
	p := newPicker(bigCatalog(300))
	openRole(p, store.RoleWork)
	if p.Total() != 300 {
		t.Fatalf("the level holds %d rows, want all 300", p.Total())
	}
	if p.Count() != 300 {
		t.Fatalf("%d rows survive an empty filter, want all 300", p.Count())
	}

	const height = 14
	rendered := lines(p, 90, height)
	body := rendered[p.bodyTop:]
	last := strings.TrimSpace(body[len(body)-1])
	shown := len(body) - 1
	want := strconv.Itoa(300-shown) + " more"
	if last != want {
		t.Fatalf("the clipped list ends with %q, want %q\n%s", last, want, strings.Join(rendered, "\n"))
	}

	// A list that fits says nothing about hiding, because nothing is hidden.
	p.Reset()
	small := newPicker(bigCatalog(4))
	openRole(small, store.RoleWork)
	for _, line := range lines(small, 90, 20) {
		if strings.Contains(line, " more") {
			t.Errorf("a list that fits claims rows are hidden: %q", line)
		}
	}
}

func TestFilteringNarrowsOverTheWordTheIdAndTheProvidersName(t *testing.T) {
	t.Parallel()
	c := bigCatalog(300)
	c.Models = append(c.Models, ModelOption{
		Slug: "anthropic/claude-opus-5", Name: "Claude Opus 5",
		Window: 1_000_000, Price: Price{In: 5, Out: 25, Known: true},
	})
	p := newPicker(c)
	openRole(p, store.RoleWork)
	total := p.Count()

	// The vendor prefix — the one part of the id the row's word drops.
	typeText(p, "anthropic")
	if p.Count() == 0 || p.Count() >= total {
		t.Fatalf("%q narrowed %d rows to %d", "anthropic", total, p.Count())
	}
	for _, i := range p.hits {
		if !strings.Contains(p.rows[i].lowerSlug, "anthropic") {
			t.Fatalf("a row that is not anthropic's survived: %q", p.rows[i].verb)
		}
	}

	// The provider's display name, which is never drawn.
	p.Key(ctrlKey('u'))
	typeText(p, "Claude Opus")
	if p.Count() != 1 {
		t.Fatalf("%d rows survive %q, want the one", p.Count(), "Claude Opus")
	}
	// Terms in any order, across fields, with a space no field contains.
	p.Key(ctrlKey('u'))
	typeText(p, "opus anthropic")
	if p.Count() != 1 {
		t.Fatalf("%d rows survive two terms, want the one", p.Count())
	}
	res, ok := p.Selected()
	if !ok || res.(SetRole).ModelSlug != "anthropic/claude-opus-5" {
		t.Fatalf("the survivor is %v", res)
	}

	// Over-filtering names the query back rather than looking broken.
	p.Key(ctrlKey('u'))
	typeText(p, "zzz")
	if _, ok := find(lines(p, 80, 10), "no model matches zzz"); !ok {
		t.Error("an over-filtered catalog does not name its query")
	}
}

func TestTheCatalogIsGroupedByFamilyAndCheapestFirstWithin(t *testing.T) {
	t.Parallel()
	c := Catalog{Models: []ModelOption{
		{Slug: "openai/dear", Price: Price{In: 10, Out: 30, Known: true}},
		{Slug: "anthropic/dear", Price: Price{In: 5, Out: 25, Known: true}},
		{Slug: "openai/unpriced"},
		{Slug: "openai/cheap", Price: Price{In: 0.2, Out: 0.8, Known: true}},
		{Slug: "anthropic/cheap", Price: Price{In: 1, Out: 5, Known: true}},
	}}
	p := newPicker(c)
	openRole(p, store.RoleWork)
	var order []string
	for _, i := range p.hits {
		order = append(order, p.rows[i].result.(SetRole).ModelSlug)
	}
	want := []string{"anthropic/cheap", "anthropic/dear", "openai/cheap", "openai/dear", "openai/unpriced"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v\nwant  %v", order, want)
	}
}

// A reader arrives from a chip showing what they are on, and the question they
// arrived with is "what else" — asked from where they are.
func TestOpeningARoleLandsOnTheModelItIsRunning(t *testing.T) {
	t.Parallel()
	c := bigCatalog(300)
	bound := c.Roles[0].Model
	p := newPicker(c)
	openRole(p, store.RoleWork)

	res, ok := p.Selected()
	if !ok {
		t.Fatal("nothing is selected on a three-hundred-row level")
	}
	if got := res.(SetRole).ModelSlug; got != bound {
		t.Fatalf("the cursor landed on %q, want the bound %q", got, bound)
	}
	// And the ✓ is on screen, which is the only way anyone ever sees it.
	rendered := lines(p, 90, 14)
	if _, ok := find(rendered, tokens.GlyphSettled); !ok {
		t.Errorf("the bound model is off screen:\n%s", strings.Join(rendered, "\n"))
	}
}

// The production bar, at catalog scale: every line fits, nothing panics, at
// every width and height a terminal can produce.
func TestWidthSweepWithTheWholeCatalog(t *testing.T) {
	t.Parallel()
	c := bigCatalog(300)
	c.Models = append(c.Models,
		ModelOption{Slug: "彼ら/日本語のモデル", Name: "wide runes", Window: 1_000_000,
			Price: Price{In: 999.5, Out: 4999.5, Known: true}, Intelligence: 100},
		ModelOption{Slug: "a-vendor/an-extremely-long-model-name-nobody-would-ship:high"},
	)
	p := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	p.SetCatalog(c)
	openRole(p, store.RoleWork)
	for _, query := range []string{"", "model", "a 0"} {
		p.setQuery(query)
		for w := 1; w <= 120; w++ {
			for _, h := range []int{1, 2, 3, 5, 12, 40} {
				out := p.Render(w, h)
				rows := strings.Split(out, "\n")
				if out == "" {
					rows = nil
				}
				if len(rows) > h {
					t.Fatalf("query %q at %dx%d rendered %d lines", query, w, h, len(rows))
				}
				for i, line := range rows {
					if got := blocks.Width(line); got > w {
						t.Fatalf("query %q at %dx%d line %d is %d cells: %q", query, w, h, i, got, line)
					}
				}
			}
		}
	}
}

// Three hundred rows must not cost three hundred rows' worth of work per frame.
func BenchmarkRenderTheWholeCatalog(b *testing.B) {
	p := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	p.SetCatalog(bigCatalog(300))
	p.cursor = 0
	p.Key(namedKey('\r'))
	b.ReportAllocs()
	for b.Loop() {
		p.Render(90, 24)
	}
}
