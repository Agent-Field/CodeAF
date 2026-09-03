package plan

import (
	"reflect"
	"testing"
)

// The three node texts below are verbatim from a designed comparison of the
// planner arms on a brief naming three disjoint lanes over one 144 KB file
// (draws Bgl-R1a-1, Bgl-R1a-2 and Ag0-R1a-1). In each of them the spine
// answered one stage that spelled the three lanes out, the ruler called that
// stage atomic and named no parts for it, and the burden of proof refused it as
// unnamed — so the three lanes ran as one sitting against a window the material
// had already been measured past. They are kept here because they are the
// evidence: a reader who changes this file can see what it was written for.
func TestANodesOwnWordsNameItsPieces(t *testing.T) {
	for _, test := range []struct {
		name    string
		title   string
		summary string
		want    []string
	}{{
		name:  "the lanes run together in the title and labelled in the summary",
		title: "NorthDates, SouthSort, EastLowTotal",
		summary: "NorthDates: Rewrite North recorded dates to YYYY-MM-DD; " +
			"SouthSort: Sort South by quantity desc, renumber ids; " +
			"EastLowTotal: Prefix LOW items under 10, append East total",
		want: []string{"NorthDates", "SouthSort", "EastLowTotal"},
	}, {
		name:  "the part sentences end in full stops",
		title: "North_Date_Format, South_Sort_Renumber, East_Transform_Total",
		summary: "North_Date_Format: Rewrite North recorded dates to YYYY-MM-DD.; " +
			"South_Sort_Renumber: Sort South by quantity desc and renumber ids.; " +
			"East_Transform_Total: Prefix LOW and append East total line.",
		want: []string{"North_Date_Format", "South_Sort_Renumber", "East_Transform_Total"},
	}, {
		name:  "the labels are bare region names",
		title: "North, South, East",
		summary: "North: Rewrite North dates to YYYY-MM-DD only.; " +
			"South: Sort South by quantity, renumber S ids.; " +
			"East: Prefix low items, append East total line.",
		want: []string{"North", "South", "East"},
	}, {
		name:    "a labelled list wears one name and several numbers",
		title:   "Deliver L1, L2 and L3",
		summary: "L1 owns the North block, L2 the South and L3 the East.",
		want:    []string{"l1", "l2", "l3"},
	}, {
		// THE COUNTER-CASE. One subject, written the way one subject is
		// written: commas inside a sentence, a colon nowhere, and a date format
		// that looks like a list to anything counting punctuation. It stays one
		// sitting, and the whole burden of proof stays where it was for it.
		name:  "a single subject names no pieces",
		title: "Rewrite the North block dates",
		summary: "Rewrite every recorded date in the North block from DD/MM/YYYY to YYYY-MM-DD, " +
			"keeping the file header and the three block headings exactly as they are.",
		want: nil,
	}, {
		name:    "a semicolon between two ordinary clauses names no pieces",
		title:   "Read the conventions",
		summary: "Read CONVENTIONS.md first; its four rulings are settled.",
		want:    nil,
	}, {
		name:    "one labelled clause beside an unlabelled one names no pieces",
		title:   "Sort the South block",
		summary: "Sort by quantity: descending; then renumber the ids S-0001 upward.",
		want:    nil,
	}, {
		name:    "a lone mark is not a list",
		title:   "Ship v2 of the parser",
		summary: "Ship v2 of the parser and delete the v2 shim afterwards.",
		want:    nil,
	}, {
		name:    "nothing said names nothing",
		title:   "",
		summary: "",
		want:    nil,
	}} {
		t.Run(test.name, func(t *testing.T) {
			got := piecesNamed(test.title, test.summary)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("piecesNamed = %q, want %q", got, test.want)
			}
			if namesSeveralPieces(test.title, test.summary) != (len(test.want) >= 2) {
				t.Fatalf("namesSeveralPieces disagrees with piecesNamed on %q", test.title)
			}
		})
	}
}

// The law, at the two places that used to end a node's chances before the stage
// question was asked: the burden of proof, and the boundary the expansion draws
// around the stage question itself.
func TestAnAtomicNodeThatNamesItsPiecesIsStagedAndNotRefused(t *testing.T) {
	staged := &Node{
		Kind:  KindWork,
		Title: "North, South, East",
		Summary: "North: Rewrite North dates to YYYY-MM-DD only.; " +
			"South: Sort South by quantity, renumber S ids.; " +
			"East: Prefix low items, append East total line.",
		Size: SizeAtomic,
	}
	verdict := JudgeSplit(staged, Options{MaxDepth: 3})
	if !verdict.Divide {
		t.Fatalf("a node naming three pieces was refused: %q", verdict.Reason)
	}
	if !dividesInTime(staged, Options{}) {
		t.Fatal("a node naming three pieces was kept from the stage question")
	}

	kept := &Node{
		Kind:  KindWork,
		Title: "Rewrite the North block dates",
		Summary: "Rewrite every recorded date in the North block from DD/MM/YYYY to YYYY-MM-DD, " +
			"keeping the file header and the three block headings exactly as they are.",
		Size: SizeAtomic,
	}
	if verdict := JudgeSplit(kept, Options{MaxDepth: 3}); verdict.Divide {
		t.Fatal("a single-subject atomic node was admitted for division")
	} else if verdict.Reason != RefusalUnnamed {
		t.Fatalf("single-subject refusal = %q, want %q", verdict.Reason, RefusalUnnamed)
	}
	if dividesInTime(kept, Options{}) {
		t.Fatal("a single-subject atomic node was sent to the stage question")
	}
}

// The boundary the law draws through both predicates: the words are read for a
// fresh plan and never for a remainder, and a remainder's size still carries it
// through both.
func TestARemaindersListOfPiecesIsOneWorkersAssignment(t *testing.T) {
	listed := &Node{
		Kind:  KindWork,
		Title: "Fix the failing tests",
		Summary: "T1: fix test_dates.; T2: fix test_quantities.; " +
			"T3: fix test_prefixes.; T4: fix test_totals.",
		Size: SizeAtomic,
	}
	fresh := Options{MaxDepth: 3}
	remainder := Options{MaxDepth: 3, Undivided: true}

	if verdict := JudgeSplit(listed, fresh); !verdict.Divide {
		t.Fatalf("a fresh plan naming four pieces was refused: %q", verdict.Reason)
	}
	if !dividesInTime(listed, fresh) {
		t.Fatal("a fresh plan naming four pieces was kept from the stage question")
	}

	if verdict := JudgeSplit(listed, remainder); verdict.Divide {
		t.Fatal("a remainder was divided on its own list of what is left")
	} else if verdict.Reason != RefusalUnnamed {
		t.Fatalf("the remainder's refusal = %q, want %q", verdict.Reason, RefusalUnnamed)
	}
	if dividesInTime(listed, remainder) {
		t.Fatal("a remainder's list was offered the stage question")
	}

	// And the ruler's reach is untouched: the one judgment a remainder IS
	// divided on still carries the same node through both predicates.
	measured := *listed
	measured.Size = SizeOversized
	if verdict := JudgeSplit(&measured, remainder); !verdict.Divide {
		t.Fatalf("a remainder past one worker's reach was refused: %q", verdict.Reason)
	}
	if !dividesInTime(&measured, remainder) {
		t.Fatal("a remainder past one worker's reach was kept from the stage question")
	}
}
