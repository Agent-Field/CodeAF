package tui3

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ZERO SPEND DRAWS NOTHING. The emptiness law on the chip is the same as on
// every other money figure: unused is absent, never `$0.00`.
func TestSpendChipFieldEmptiness(t *testing.T) {
	if got := spendChipField(session.ModelSpend{}); got.known() {
		t.Fatalf("a zero row drew %q", got.full)
	}
	if got := spendChipField(session.ModelSpend{Model: "x", USD: 0, Tokens: 100}); got.known() {
		t.Fatalf("a zero-USD row drew %q", got.full)
	}
	if got := spendChipField(session.ModelSpend{Model: "x", USD: 1.25, Tokens: 1000}); !got.known() {
		t.Fatal("a priced row answered an empty chip")
	}
}

// WITH AN EMPTY BOX, THE BILL LEADS THE LIST. promoteUsed puts the dearest
// spender first and leaves an unused model behind it — habit before catalog.
func TestPromoteUsedOrdersBySpend(t *testing.T) {
	models := []Model{
		{ID: "cheap/model"},
		{ID: "dear/model"},
	}
	p := picker{}
	p.start(models, "cheap/model")
	p.spent = map[string]session.ModelSpend{
		spendModelKey("dear/model"): {Model: "dear/model", USD: 12.5, Tokens: 2_000_000},
	}
	p.promoteUsed()
	if len(p.hits) != 2 {
		t.Fatalf("hits = %v, want both models", p.hits)
	}
	if got := p.all[p.hits[0]].ID; got != "dear/model" {
		t.Fatalf("promoteUsed led with %q, want dear/model", got)
	}
	if got := p.all[p.hits[1]].ID; got != "cheap/model" {
		t.Fatalf("the unused model is %q after the spender, want cheap/model", got)
	}
}

// `used` KEEPS ONLY SPENDERS. The term is a filter, not a sort: models with no
// fortnight bill drop out entirely.
func TestUsedTermKeepsOnlySpenders(t *testing.T) {
	term, ok := parseLaneTerm("used")
	if !ok || term.kind != termUsed {
		t.Fatalf("parseLaneTerm(used) = %#v, %v", term, ok)
	}
	models := []Model{
		{ID: "spent/one"},
		{ID: "never/run"},
		{ID: "spent/two"},
	}
	p := picker{}
	p.start(models, "never/run")
	p.spent = map[string]session.ModelSpend{
		spendModelKey("spent/one"): {Model: "spent/one", USD: 3, Tokens: 100},
		spendModelKey("spent/two"): {Model: "spent/two", USD: 9, Tokens: 400},
	}
	p.filter.setText("used")
	p.rank()
	got := make([]string, 0, len(p.hits))
	for _, at := range p.hits {
		got = append(got, p.all[at].ID)
	}
	if len(got) != 2 {
		t.Fatalf("used kept %v, want only the two spenders", got)
	}
	for _, id := range got {
		if id == "never/run" {
			t.Fatalf("used left an unused model on the list: %v", got)
		}
	}
}

// AFTER PROMOTION THE FIRST USED ROW CARRIES THE SECTION HEADING. The empty-box
// list names the stretch `used lately` so habit is not mistaken for catalog order.
func TestUsedSectionHeading(t *testing.T) {
	models := []Model{
		{ID: "catalog/a"},
		{ID: "catalog/b"},
	}
	p := picker{}
	p.start(models, "catalog/a")
	p.spent = map[string]session.ModelSpend{
		spendModelKey("catalog/b"): {Model: "catalog/b", USD: 4.2, Tokens: 50_000},
	}
	p.rank()
	if len(p.list) == 0 {
		t.Fatal("the list is empty after rank")
	}
	if got := p.usedSectionBefore(0); got != pickerUsedSectionWord {
		t.Fatalf("usedSectionBefore(0) = %q, want %q", got, pickerUsedSectionWord)
	}
	if got := p.groupBefore(0); got != pickerUsedSectionWord {
		t.Fatalf("groupBefore(0) = %q, want %q", got, pickerUsedSectionWord)
	}
	if got := p.sectionLabel(pickerUsedSectionWord); got != pickerUsedSectionWord+" · 1" {
		t.Fatalf("sectionLabel(used) = %q, want a count of one", got)
	}
	if got := p.sectionLabel(pickerAllSectionWord); got != pickerAllSectionWord+" · 1" {
		t.Fatalf("sectionLabel(all) = %q, want a count of one", got)
	}
}

// `/model used` ON A QUIET MACHINE SAYS HOW TO LEAVE, not a bare "no model matches".
func TestUsedFilterEmptyCopy(t *testing.T) {
	p := picker{}
	p.start([]Model{{ID: "a/model"}}, "a/model")
	p.filter.setText("used")
	p.rank()
	if got := p.emptyLine(); got != noModelsUsedLately {
		t.Fatalf("emptyLine = %q, want %q", got, noModelsUsedLately)
	}
	// AND A TYPED FILTER THAT MATCHES NOTHING KEEPS THE ORDINARY LINE once spenders exist.
	p.spent = map[string]session.ModelSpend{
		spendModelKey("a/model"): {Model: "a/model", USD: 1, Tokens: 10},
	}
	p.filter.setText("usedzzzz")
	p.rank()
	if got := p.emptyLine(); got == noModelsUsedLately {
		t.Fatalf("a misspelled filter used the quiet-machine copy: %q", got)
	}
}
