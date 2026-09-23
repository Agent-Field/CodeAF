package main

// cells.go — the four briefs, each with a structure the brief does not say.
//
// Every cell is a small Go module under fixtures/, seeded fresh per
// invocation, and each one hides one thing a planner could only learn by
// reading the code or by running the tests. The briefs are written the way a
// person writes them; neither arm is told anything about the experiment.

import (
	"fmt"
	"sort"
	"strings"
)

// cell is one shape the baseline measures.
type cell struct {
	id      string // r1..r4, the id the CSV and the table quote
	name    string // the cell's directory name and a person's handle for it
	fixture string // directory under fixtures/
	hidden  string // what the brief does not say, for the design doc and the dry run
	brief   string // the whole instruction, verbatim
}

// allCells is the grid's cell list, in the order the plan runs them.
func allCells() []cell {
	return []cell{cellR1, cellR2, cellR3, cellR4}
}

// cellR1 — seven "separate" bug reports, five of which are one bug in one
// function (canonical, in canon.go). Graded on the whole suite; measured on
// how many tasks edited canon.go and how many symptom files were patched
// locally instead.
var cellR1 = cell{
	id:      "r1",
	name:    "r1-shared-cause",
	fixture: "r1-shared-cause",
	hidden:  "reports 1-5 are one bug: canonical() in canon.go lowercases but does not trim or collapse spaces; 6 and 7 are independent",
	brief: `Seven bug reports came in this week from different people using the contacts
package. Fix all seven. Each report has a failing test in contacts_test.go
named after it; the tests describe the wanted behavior, so fix the code and
leave the tests exactly as they are.

1. Two email addresses that differ only by a stray space are treated as
   different people.
2. Dedup keeps "Ada Lovelace" and "ada  lovelace" as two separate names.
3. Looking up "alan turing " in the phone book finds nothing, though
   "Alan Turing" is in it.
4. Slug turns "  Hello   World " into "--hello---world-" instead of
   "hello-world".
5. Tag counts list " go" and "Go" as two different tags.
6. Initials drops the last name: "grace brewster hopper" gives "GB".
7. Phone numbers are grouped wrong: "5551234567" comes out as "555-1234-567".`,
}

// cellR2 — a migration whose width is only visible in the code: six packages
// call the deprecated legacy.Fetch, each with its own retry count and its own
// reading of a 404, and the brief names none of them. Graded on the suite,
// the tests untouched and legacy gone; measured on parallelism and wall.
var cellR2 = cell{
	id:      "r2",
	name:    "r2-probe-then-fanout",
	fixture: "r2-probe-then-fanout",
	hidden:  "six independent call sites (billing, catalog, inventory, mailer, reviews, shipping); retries n become Attempts n+1 and a 404 becomes client.ErrNotFound at each one",
	brief: `The legacy package in this module is deprecated. Move every caller of it over
to the client package, and then delete the legacy package entirely. Nothing a
caller does may change: the existing tests must still pass, and they must not
be edited.`,
}

// cellR3 — the obvious change breaks a package the brief never mentions:
// export writes CSV through money.Format, and the bank's format has no
// separators and no parentheses. It shows only when the whole suite runs.
// Graded on the suite; measured on failed test runs and repeated edits.
var cellR3 = cell{
	id:      "r3",
	name:    "r3-wrong-first-approach",
	fixture: "r3-wrong-first-approach",
	hidden:  "export.CSV also formats through money.Format and its test wants the plain signed form, so changing Format alone turns export red",
	brief: `Amounts in this module should read the way our accountants write them:
thousands separators, and negative amounts in parentheses instead of a minus
sign. 1234.56 becomes 1,234.56 and -1234.56 becomes (1,234.56). The money
package's Format function is where amounts are formatted, and its tests
already describe the new behavior. Make the change, and leave every test as
it is.`,
}

// cellR4 — the control: a one-function fix where no plan can help, so any
// planning machinery shows up as pure overhead.
var cellR4 = cell{
	id:      "r4",
	name:    "r4-control",
	fixture: "r4-control",
	hidden:  "nothing: one function, one failing test",
	brief: `The test suite in this repository is failing. Run the tests, find out why,
and fix the code so the whole suite passes. The tests describe the behavior
that is wanted, so fix the code, not the tests.`,
}

// r1SymptomFiles are the files whose functions show r1's shared bug. The one
// right fix is in canon.go; a change to any of these is a symptom patched
// where it showed rather than where it came from.
var r1SymptomFiles = []string{"email.go", "dedup.go", "lookup.go", "slug.go", "tags.go"}

// chooseCells filters the cell list by id or by name.
func chooseCells(names string) ([]cell, error) {
	all := allCells()
	names = strings.TrimSpace(names)
	if names == "" {
		return all, nil
	}
	var chosen []cell
	seen := map[string]bool{}
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		c, ok := findCell(all, name)
		if !ok {
			ids := make([]string, 0, len(all))
			for _, known := range all {
				ids = append(ids, known.id)
			}
			sort.Strings(ids)
			return nil, fmt.Errorf("no cell %q (have %s)", name, strings.Join(ids, " "))
		}
		seen[name] = true
		chosen = append(chosen, c)
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("-cells named nothing")
	}
	return chosen, nil
}

// findCell answers one cell by id or by name.
func findCell(cells []cell, word string) (cell, bool) {
	for _, c := range cells {
		if c.id == word || c.name == word {
			return c, true
		}
	}
	return cell{}, false
}
