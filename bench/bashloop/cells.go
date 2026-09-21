package main

// cells.go — the six briefs and their fixtures.
//
// The cells are the contract (DESIGN.md, "The comparison"): six shapes of
// work, each with its own fixture repo under fixtures/ and its own grader in
// grade.go. The briefs are written the way a person writes them — the same
// words go to both arms, verbatim, and neither arm is told anything about the
// experiment it is taking part in.

// cell is one shape the comparison measures.
type cell struct {
	id      string // c1..c6, the id the CSV and the table quote
	name    string // one line a person reads
	fixture string // directory under fixtures/, seeded fresh per invocation
	brief   string // the whole instruction, verbatim
}

// allCells is the grid's cell list, in the order the plan runs them.
func allCells() []cell {
	return []cell{
		cellC1, cellC2, cellC3, cellC4, cellC5, cellC6,
	}
}

// cellC1 — small fix. One failing suite in a fixture repo; graded by that
// suite going green.
var cellC1 = cell{
	id:      "c1",
	name:    "small fix",
	fixture: "c1-small-fix",
	brief: `The test suite in this repository is failing. Run the tests, find out why,
and fix the code so the whole suite passes. The tests describe the behavior
that is wanted — fix the code, not the tests. When the suite is green, report
what was wrong in a sentence or two.`,
}

// cellC2 — feature. A new module and its tests; graded by suite green and a
// sane diff.
var cellC2 = cell{
	id:      "c2",
	name:    "feature",
	fixture: "c2-feature",
	brief: `This module needs a new package called histogram. Give it a Histogram type
that starts empty and grows, with Add(value float64) to record a value,
Buckets() returning the buckets as a slice — each bucket carrying Count, From
and To — and a String() that draws a small ASCII bar chart, one line per
bucket. Give the package its own tests, leave the existing stats package as
it is, and make sure the whole module's tests pass when you're done.`,
}

// cellC3 — multi-file refactor. Reads-heavy work, the read-idiom tax; graded
// by suite green and the public API unchanged.
var cellC3 = cell{
	id:      "c3",
	name:    "multi-file refactor",
	fixture: "c3-refactor",
	brief: `The tax math in this module has been pasted into three places: items.go,
orders.go and report.go each carry their own copy of the same calculation.
Pull it into one shared function in a new file called tax.go, and have all
three call sites use it. The public surface of this module must stay exactly
as it is — nothing exported may be renamed, removed, or added — and the test
suite must still pass, unchanged, when you're done.`,
}

// cellC4 — report. Research and synthesis into REPORT.md; graded by presence
// and section coverage.
var cellC4 = cell{
	id:      "c4",
	name:    "report",
	fixture: "c4-report",
	brief: `Read every document in the docs/ folder of this repository and write a
REPORT.md at the top level summarizing what they say. Give it four sections:
Summary, Findings, Risks, and Recommendations. The Findings section should
cover every document, with the concrete numbers from each one; the
Recommendations section should be a numbered list with at least three items.
Keep the report grounded in what the documents actually say.`,
}

// cellC5 — wide job. A brief that forces the work to be handed out; graded by
// children landed and the integrated result.
var cellC5 = cell{
	id:      "c5",
	name:    "wide job",
	fixture: "c5-wide",
	brief: `Four packages in this module — parse, render, notify and archive — each
have their own failing tests. They share no code with each other. Get every
package's tests passing.

These are four independent jobs: hand the parts out so they can move at the
same time rather than doing them one after another yourself, then fold the
results back together. When all four are done, write INTEGRATION.md at the
top of the repository with one line per package saying what was wrong and
what fixed it, and make sure the whole module's tests pass.`,
}

// cellC6 — kept-tool. A brief needing a generated image through the belt;
// graded by the image existing and being referenced.
var cellC6 = cell{
	id:      "c6",
	name:    "kept-tool image",
	fixture: "c6-image",
	brief: `Read DESIGN.md in this repository and write ARCHITECTURE.md at the top
level: how the checkout flow works, component by component, in your own
words. Include a diagram of the components and how they connect — generate
the diagram as an image file, save it at docs/architecture.png inside this
repository, and reference it from ARCHITECTURE.md so a reader of the
document sees it where it belongs in the text.`,
}
