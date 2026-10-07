package delegate

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestSeniorDevCeilingsAreFiniteAndCapExplicitLimits(t *testing.T) {
	for _, tc := range []struct {
		in, want Ceilings
	}{
		{Ceilings{}, Ceilings{CostUSD: DefaultSeniorDevCostUSD, Hours: DefaultSeniorDevHours}},
		{Ceilings{CostUSD: 1, Hours: 0.5}, Ceilings{CostUSD: 1, Hours: 0.5}},
		{Ceilings{CostUSD: 30, Hours: 9}, Ceilings{CostUSD: DefaultSeniorDevCostUSD, Hours: DefaultSeniorDevHours}},
	} {
		if got := tc.in.SeniorDev(); got != tc.want {
			t.Fatalf("%+v became %+v, want %+v", tc.in, got, tc.want)
		}
	}
	if got := (Ceilings{}).SeniorDev().Summary(); got != "up to $10.00 and 3h" {
		t.Fatalf("default ceiling summary = %q", got)
	}
	if got := (Ceilings{CostUSD: 1, Hours: 0.5}).SeniorDev().Summary(); !strings.Contains(got, "$1.00") || !strings.Contains(got, "30m") {
		t.Fatalf("smaller ceiling summary = %q", got)
	}
}

func TestSeniorDevShellFlagsReplaceDefaults(t *testing.T) {
	for _, tc := range []struct {
		in, want Ceilings
	}{
		{Ceilings{}, Ceilings{CostUSD: DefaultSeniorDevCostUSD, Hours: DefaultSeniorDevHours}},
		{Ceilings{CostUSD: 30, Hours: 9}, Ceilings{CostUSD: 30, Hours: 9}},
	} {
		if got := tc.in.SeniorDevDefaults(); got != tc.want {
			t.Fatalf("shell ceiling %+v became %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestSeniorDevShellParserSetsFiniteDefaultsAndHonorsFlags(t *testing.T) {
	program := testProgram(nil)
	program.Name, program.Unattended = "senior-dev", SeniorDevCeilings
	for _, tc := range []struct {
		line []string
		want Ceilings
	}{
		{[]string{"repair"}, Ceilings{CostUSD: DefaultSeniorDevCostUSD, Hours: DefaultSeniorDevHours}},
		{[]string{"--max-cost", "30", "--max-hours", "9", "repair"}, Ceilings{CostUSD: 30, Hours: 9}},
	} {
		inv, err := Parse(program, tc.line, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		if inv.Ceilings != tc.want {
			t.Fatalf("%q: ceiling %+v, want %+v", tc.line, inv.Ceilings, tc.want)
		}
	}
}

func TestSeniorDevRejectsNonFiniteCeilings(t *testing.T) {
	program := testProgram(nil)
	program.Name, program.Unattended = "senior-dev", SeniorDevCeilings
	for _, line := range [][]string{
		{"--max-cost", "NaN", "repair"},
		{"--max-hours", "+Inf", "repair"},
	} {
		if _, err := Parse(program, line, &bytes.Buffer{}); err == nil {
			t.Fatalf("non-finite ceiling %q was accepted", line)
		}
	}
	if got := (Ceilings{CostUSD: math.NaN(), Hours: math.Inf(1)}).SeniorDev(); got.CostUSD != DefaultSeniorDevCostUSD || got.Hours != DefaultSeniorDevHours {
		t.Fatalf("non-finite conversation limit produced %+v", got)
	}
}

// A program that names no ceilings of its own is held to none: the
// conversation's remaining limits pass through as they are, and a shell line
// with no flags runs unbounded, as every program but senior-dev did before the
// field existed. One that names its own is held to those and not to
// senior-dev's.
func TestUnattendedCeilingsAreTheProgramsOwn(t *testing.T) {
	left := Ceilings{CostUSD: 30, Hours: 9}
	if got := left.CappedBy(Ceilings{}); got != left {
		t.Fatalf("a program with no ceilings capped %+v to %+v", left, got)
	}
	own := Ceilings{CostUSD: 5, Hours: 2}
	if got := left.CappedBy(own); got != own {
		t.Fatalf("%+v capped by %+v became %+v", left, own, got)
	}
	if got := (Ceilings{}).CappedBy(own); got != own {
		t.Fatalf("an unlimited conversation ran under %+v, want %+v", got, own)
	}
	if got := (Ceilings{CostUSD: 1}).CappedBy(Ceilings{Hours: 2}); got != (Ceilings{CostUSD: 1, Hours: 2}) {
		t.Fatalf("a program with only a wall ceiling gave %+v", got)
	}
	program := testProgram(nil)
	inv, err := Parse(program, []string{"repair"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Ceilings.IsZero() {
		t.Fatalf("a shell run of a program with no ceilings got %+v", inv.Ceilings)
	}
	program.Unattended = own
	if inv, err = Parse(program, []string{"repair"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if inv.Ceilings != own {
		t.Fatalf("a shell run got %+v, want the program's own %+v", inv.Ceilings, own)
	}
}

// A BARE RUN OF A PROGRAM WITH A DEFAULT BRIEF RUNS THAT BRIEF, and the line a
// host hands its child carries it, so the child parses back the same
// invocation. A program with none still has nothing to run.
func TestABareRunTakesTheProgramsDefaultBrief(t *testing.T) {
	program := testProgram(nil)
	program.DefaultBrief = "the whole repository"
	inv, err := Parse(program, []string{"--max-cost", "2"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Brief() != "the whole repository" {
		t.Fatalf("a bare run's brief = %q", inv.Brief())
	}
	again, err := Parse(program, inv.Line, &bytes.Buffer{})
	if err != nil || again.Brief() != inv.Brief() || again.Ceilings != inv.Ceilings {
		t.Fatalf("the line %q read back as %+v, %v", inv.Line, again, err)
	}
	if typed, _ := Parse(program, []string{"changes"}, &bytes.Buffer{}); typed.Brief() != "changes" {
		t.Fatalf("a typed brief became %q", typed.Brief())
	}
	program.DefaultBrief = ""
	if bare, _ := Parse(program, nil, &bytes.Buffer{}); bare.Brief() != "" {
		t.Fatalf("a program with no default brief ran %q", bare.Brief())
	}
}

// No ceiling is said as no words, and one ceiling as that one alone.
func TestACeilingThatIsNotSetIsNotSaid(t *testing.T) {
	for _, tc := range []struct {
		in   Ceilings
		want string
	}{
		{Ceilings{}, ""},
		{Ceilings{CostUSD: 5}, "up to $5.00"},
		{Ceilings{Hours: 2}, "up to 2h"},
		{Ceilings{CostUSD: 5, Hours: 2}, "up to $5.00 and 2h"},
	} {
		if got := tc.in.Summary(); got != tc.want {
			t.Errorf("%+v says %q, want %q", tc.in, got, tc.want)
		}
	}
}
