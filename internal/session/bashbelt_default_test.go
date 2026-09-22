package session

import "testing"

// THIS FILE IS THE ONLY THING IN THE PACKAGE THAT TESTS THE DEFAULT.
//
// hermetic_test.go's TestMain pins CODEAF_TASK_BELT to the older belt for the
// whole suite, because this package's tests were written against that engine
// and named it by saying nothing. That pin is correct and it has a cost: with
// it in place, nothing in the package would notice if the default went back to
// the older belt, because every test would keep passing and the suite's green
// would be computed over a road no person runs.
//
// So the truth table is asserted here, by name, with the variable set for each
// row through t.Setenv — which outranks the package pin and is restored on the
// way out. An empty value is a row, and a deliberate one: an exported but blank
// variable must read as an unset one, which is the default, which is bash.
func TestTheBeltIsBashUnlessOneOfThreeWordsSaysOtherwise(t *testing.T) {
	for _, row := range []struct {
		value string
		want  bool
		why   string
	}{
		{"", true, "unset or blank is the default, and the default is the harness"},
		{"bash", true, "the word that used to be the way in still names the harness"},
		{"node", false, "the older belt, named"},
		{"legacy", false, "the older belt, under its other name"},
		{"off", false, "the older belt, for a person who reads the switch as a switch"},
		{"NODE", false, "the words are matched without case, as a person types them"},
		{" node ", false, "surrounding space is a person's typing, not a different word"},
		{"nodes", true, "an unrecognised word leaves a person on the belt they were promised"},
		{"true", true, "a word that means nothing here does not move anybody"},
		{"no", true, "and neither does one that looks like it should"},
	} {
		t.Run(row.value, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", row.value)
			if got := bashBeltAsked(); got != row.want {
				t.Fatalf("CODEAF_TASK_BELT=%q: belt on = %v, want %v — %s", row.value, got, row.want, row.why)
			}
			// The exported door and the package's own reader are two halves of
			// one fact, and the whole point of the door is that they cannot
			// disagree. A change that flipped one and not the other would leave
			// a run and the landing that judges it on different belts.
			if BashBeltAsked() != bashBeltAsked() {
				t.Fatalf("CODEAF_TASK_BELT=%q: the exported door and the package reader disagree", row.value)
			}
		})
	}
}

// The suite's own pin is a fact worth asserting, because it is what makes every
// other test in this package a test of the older engine. If it is ever dropped,
// a hundred and forty tests change what they are testing without one line of
// them changing, which is exactly what happened when the default moved.
func TestThePackageSuiteRunsOnTheOlderBelt(t *testing.T) {
	if bashBeltAsked() {
		t.Fatal("this package's TestMain no longer pins the older belt: every test here is now testing the harness road it was not written for")
	}
}
