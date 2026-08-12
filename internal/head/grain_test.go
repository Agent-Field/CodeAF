package head

import (
	"strings"
	"testing"
)

// The compiler's width rule debited only lateness: an extra worker was free to
// organise and the person waited for the longest chain. Both halves are true
// and together they say nothing about the bill, which is context — every part
// re-pays whatever it must be told before it can start. W6 puts the price back
// in the same sentence that grants the width.
func TestTheCompilerWidthRulePricesContextAndNotOnlyLateness(t *testing.T) {
	for _, want := range []string{
		"the time the person waits is the longest chain, never the total",
		"appetite is paid in context",
		"re-pays whatever it must be told before it can start",
		"the same answer bought a second time",
	} {
		if !strings.Contains(compilerSystemPrompt, want) {
			t.Errorf("the width rule is missing %q", want)
		}
	}
}

// Width follows the grain of the material. The compiler is the first place that
// can get this wrong and the most expensive one, because what it decides here
// is what every later pass is laying out. The rule must say where pieces come
// from (the enumeration already in the material), forbid the coarse halving
// that hands each piece the whole set, and carry the counterweight for material
// that enumerates nothing.
func TestTheCompilerReadsWidthOffTheMaterialsGrain(t *testing.T) {
	for _, want := range []string{
		"Where the material already enumerates its units",
		"a piece takes some of those units and says which, never all of them",
		"halving an enumerated set into two pieces that each carry the whole set",
		"where nothing is enumerated there is no split to read off it",
	} {
		if !strings.Contains(compilerSystemPrompt, want) {
			t.Errorf("the compiler prompt is missing the grain rule: %q", want)
		}
	}
}

// The bundle field is where the incident's damage was actually done: one
// request came back as two parts that were the same request twice, so two
// workers each did the whole job and a synthesis was invented to join two
// copies of one answer. The rule now makes sameness the disqualifier and sends
// unit-splitting to the pass that can do it.
func TestTheBundleFieldRejectsTwoPartsThatWouldReturnTheSameThing(t *testing.T) {
	for _, want := range []string{
		"Each part must be a DIFFERENT answer",
		"if two of them would return the same thing, they were never two requests",
		"One request that happens to cover many units is one answer and belongs in parts []",
	} {
		if !strings.Contains(compilerSystemPrompt, want) {
			t.Errorf("the parts rule is missing %q", want)
		}
	}
}

// House law, scoped to what W6 wrote: the guidance is general or it is not
// guidance. No authored example, no domain, no invented figure — everything
// evidential comes from the measured lines the menu carries.
func TestTheW6CompilerRulesNameNoDomainAndInventNoFigures(t *testing.T) {
	for _, rule := range []string{
		"- WIDTH IS FREE TO COORDINATE",
		"- Where the material already enumerates its units",
	} {
		start := strings.Index(compilerSystemPrompt, rule)
		if start < 0 {
			t.Fatalf("rule %q is not in the prompt", rule)
		}
		line := compilerSystemPrompt[start:]
		if end := strings.Index(line, "\n"); end > 0 {
			line = line[:end]
		}
		lower := strings.ToLower(line)
		for _, forbidden := range []string{"for example", "e.g.", "such as", "$", "%"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%q reaches for an example or a figure: %q", rule, forbidden)
			}
		}
	}
}
