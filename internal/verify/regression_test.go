package verify

// Which of two red checks this work is answerable for, read from happy-dom's
// own sequence.

import (
	"strings"
	"testing"
)

// happyDomBaseline is the roster the nemotron n1 run photographed before it
// started: four checks, all green, in the one file its reading was scoped to.
var happyDomBaseline = []string{
	"IntersectionObserver observe",
	"IntersectionObserver unobserve",
	"IntersectionObserver disconnect",
	"IntersectionObserver takeRecords",
}

// happyDomAfter is that same file after the run rewrote it: thirty-three checks,
// twenty-nine of them written by the run. The command never changed, so nothing
// was widened and nothing looked out of place.
func happyDomAfter(red int) (reported, failing []string) {
	reported = append(reported, happyDomBaseline...)
	for index := 1; index <= 29; index++ {
		name := "IntersectionObserver initial observation queuing " + itoa(index)
		reported = append(reported, name)
		if index <= red {
			failing = append(failing, name)
		}
	}
	return reported, failing
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// A REGRESSION IS A CHECK THAT WAS NAMED GREEN AT THE BASELINE AND IS RED NOW.
//
// happy-dom's nemotron n1 gate failed the delivery with `This work broke checks
// that were passing before it: IntersectionObserver initial observation queuing
// Queues an entry for each newly observed target, …` while the grader scored the
// same tree 9 of 9. Every reading of that job ran the identical command —
// `vitest run --reporter=json test/intersection-observer/IntersectionObserver.test.ts`
// — so nothing had been widened and no rule fired; the run had simply REWRITTEN
// the file the reading was scoped to, from 4 checks to 33, and eighteen of the
// ones it wrote were red.
//
// Subtracting the two failing lists is an approximation that holds only while
// both readings run the same set of checks, and a run writes checks. The
// baseline's ROSTER is the authority.
func TestOnlyACheckGreenAtTheBaselineCanHaveRegressed(t *testing.T) {
	reported, failing := happyDomAfter(18)
	reading := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Reported: happyDomBaseline},
		After:  Result{Reported: reported, Failing: failing},
	}
	if broke := reading.Regressed(); len(broke) > 0 {
		t.Errorf("the work was convicted of breaking %d checks it wrote itself: %v",
			len(broke), broke)
	}
	// They are a finding, in words that are true of them.
	own := reading.OwnFailing()
	if len(own) != 18 {
		t.Fatalf("the run's own red checks were counted as %d, want 18: %v", len(own), own)
	}
	for _, name := range own {
		if !strings.Contains(name, "initial observation queuing") {
			t.Errorf("a baseline check was filed as one this work wrote: %q", name)
		}
	}

	// And a genuine regression is still one: a name the baseline reported green
	// and the finished tree reports red.
	reported, failing = happyDomAfter(18)
	failing = append(failing, "IntersectionObserver unobserve")
	real := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Reported: happyDomBaseline},
		After:  Result{Reported: reported, Failing: failing},
	}
	broke := real.Regressed()
	if len(broke) != 1 || broke[0] != "IntersectionObserver unobserve" {
		t.Errorf("Regressed() = %v, want exactly the one baseline check that went red", broke)
	}
	if own := real.OwnFailing(); len(own) != 18 {
		t.Errorf("the baseline's own red check was filed as this work's: %v", own)
	}

	// A check the baseline reported RED is nobody's regression and nobody's own.
	pre := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{
			Reported: happyDomBaseline,
			Failing:  []string{"IntersectionObserver takeRecords"},
		},
		After: Result{
			Reported: happyDomBaseline,
			Failing:  []string{"IntersectionObserver takeRecords"},
		},
	}
	if broke := pre.Regressed(); len(broke) > 0 {
		t.Errorf("a check the repository arrived with red was scored as broken: %v", broke)
	}
	if own := pre.OwnFailing(); len(own) > 0 {
		t.Errorf("a check that existed at the baseline was filed as this work's: %v", own)
	}
}

// WHERE THE BASELINE NAMED NO GREEN CHECK THE QUESTION CANNOT BE ASKED, and the
// old subtraction stands.
//
// A ROSTER THAT IS ALL RED IS NOT A ROSTER. Plenty of runners print their
// failures and nothing else, and against those the reported list IS the failure
// list: it carries no evidence that any check passed, so it cannot tell a check
// that was green and is missing from a check that never existed. Reading it as a
// roster would file every regression in every such project as a test the run
// wrote itself — the exact opposite of the mistake this rule exists for.
func TestARunnerThatNamesOnlyItsFailuresIsReadAsItAlwaysWas(t *testing.T) {
	for _, probe := range []struct {
		name   string
		before Result
	}{
		{name: "no roster at all", before: Result{
			Failing: []string{"tests/test_env.py::test_needs_root"}}},
		{name: "a roster that is all red", before: Result{
			Reported: []string{"tests/test_env.py::test_needs_root"},
			Failing:  []string{"tests/test_env.py::test_needs_root"}}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			reading := Reading{
				Taken: true, AfterTaken: true,
				Before: probe.before,
				After: Result{Failing: []string{
					"tests/test_env.py::test_needs_root",
					"tests/test_igel.py::test_results_path",
				}},
			}
			broke := reading.Regressed()
			if len(broke) != 1 || broke[0] != "tests/test_igel.py::test_results_path" {
				t.Errorf("Regressed() = %v, want the one check this work turned red", broke)
			}
			if own := reading.OwnFailing(); len(own) > 0 {
				t.Errorf("failures were filed as the run's own off a roster that "+
					"names no passing check: %v", own)
			}
		})
	}
}
