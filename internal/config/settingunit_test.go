package config

import (
	"regexp"
	"strings"
	"testing"
)

// bareNumber is a reading a person cannot decide anything from: a run of digits
// with nothing on it saying what they count.
var bareNumber = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// EVERY NUMBER ON THIS SURFACE SAYS WHAT IT COUNTS.
//
// `ssh reuse 300` is three hundred WHAT — seconds, connections, kilobytes? A
// developer scanning a settings tab could not decide any of eleven rows without
// moving the cursor onto each one and reading the paragraph under it, and a
// setting nobody can decide is a setting nobody will touch. The unit belongs to
// the row, beside its default, so it is stated once and every surface that draws
// a number gets it ([Setting.Unit], [Setting.Reading]).
//
// The law is on the READING and not on the field, because a row may answer it
// two ways: by declaring a unit, or by a reader that already spells one — a
// duration writes `20m`, a dollar row writes `$5`, [formatPercent] writes `60%`.
// A row whose own LABEL names what is counted says [UnitInLabel] rather than
// nothing, so a row that was thought about and a row nobody has looked at are
// different states.
func TestEverySettingSaysWhatItsNumberMeans(t *testing.T) {
	dir := t.TempDir()
	for _, row := range registry(t, dir).Rows() {
		reading := row.Reading()
		if !bareNumber.MatchString(reading) {
			continue
		}
		if row.Unit == UnitInLabel {
			// The label already names what is counted — `tasks at once  3`, and
			// `task repair rounds  1 round` would be the row saying `rounds`
			// twice. The row was looked at, which is what the sentinel records.
			continue
		}
		if row.Unit != "" {
			t.Fatalf("row %q declares Unit %q and still draws %q — Reading did not use it",
				row.Key, row.Unit, reading)
		}
		t.Fatalf("row %q draws %q, which is %s what?\n"+
			"  drawn: %s  %s\n"+
			"  want:  %s  %s<unit>   — give it a Unit in the registry, or UnitInLabel if the label already says it",
			row.Key, reading, reading, row.Label, reading, row.Label, reading)
	}
}

// AND EVERY ROW SAYS WHAT IT DECIDES. The panel shows exactly one description —
// the row the cursor is on — so a row with no sentence is a row that answers
// nothing at any width. The sentence lives beside the default for the reason the
// unit does: it is the row's own account of itself and not a caption a surface
// wrote about it.
func TestEverySettingSaysWhatItDecides(t *testing.T) {
	dir := t.TempDir()
	for _, row := range registry(t, dir).Rows() {
		if strings.TrimSpace(row.Hint) == "" {
			t.Fatalf("row %q (%q) carries no sentence saying what it decides", row.Key, row.Label)
		}
	}
}

// A SYMBOL IS PART OF THE NUMBER AND A WORD IS NOT. `300s` and `60%` read as one
// token, the way `20m` on the duration rows beside them already does; `1536 MB`
// and `65536 tok` are two words and are drawn as two. And a unit that is a noun
// reads at one as well as at three.
func TestAUnitIsWrittenAgainstItsNumberOnlyWhenItIsASymbol(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  Setting
		want string
	}{
		{"seconds attach", Setting{Unit: "s", read: func() string { return "300" }}, "300s"},
		{"percent attaches", Setting{Unit: "%", read: func() string { return "60" }}, "60%"},
		{"words stand off", Setting{Unit: "tok", read: func() string { return "65536" }}, "65536 tok"},
		{"megabytes stand off", Setting{Unit: "MB", read: func() string { return "1536" }}, "1536 MB"},
		{
			"a noun reads at one",
			Setting{Unit: "clean firings", UnitOne: "clean firing", read: func() string { return "1" }},
			"1 clean firing",
		},
		{
			"and at three",
			Setting{Unit: "clean firings", UnitOne: "clean firing", read: func() string { return "3" }},
			"3 clean firings",
		},
		{
			"an off word takes no unit",
			Setting{Unit: "tasks", EmptyLabel: "no limit", read: func() string { return "" }},
			"no limit",
		},
		{
			"a label that already says it adds nothing",
			Setting{Unit: UnitInLabel, Label: "task repair rounds", read: func() string { return "1" }},
			"1",
		},
	} {
		if got := tc.row.Reading(); got != tc.want {
			t.Fatalf("%s: the row draws %q, want %q", tc.name, got, tc.want)
		}
	}
}

// AND A ROW TAKES ITS OWN UNIT BACK. A row that draws `300s` and then refuses
// `300s` is a row arguing with itself: the suffix is this registry's word and
// the person is handing it back.
func TestASettingTakesBackTheUnitItDrew(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	row, ok := rows.Row(KeySSHControlPersist)
	if !ok {
		t.Fatal("the ssh reuse row is not in the registry")
	}
	if got := row.Reading(); got != "300s" {
		t.Fatalf("ssh reuse draws %q, want %q", got, "300s")
	}
	if err := row.Apply("120s"); err != nil {
		t.Fatalf("the row drew 300s and refused 120s: %v", err)
	}
	again, _ := registry(t, dir).Row(KeySSHControlPersist)
	if got := again.Reading(); got != "120s" {
		t.Fatalf("after saving 120s the row draws %q, want %q", got, "120s")
	}
	if err := again.Apply("90"); err != nil {
		t.Fatalf("the row refused a bare figure: %v", err)
	}
	again, _ = registry(t, dir).Row(KeySSHControlPersist)
	if got := again.Reading(); got != "90s" {
		t.Fatalf("after saving a bare 90 the row draws %q, want %q", got, "90s")
	}
}
