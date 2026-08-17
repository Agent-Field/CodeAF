package config

import (
	"strings"
	"testing"
)

// THE SHAPES A CARD OFFERS.
//
// bashshapes.go derives them; these tests are the two things a caller may rely
// on — what the offer looks like for the lines people actually run, and the
// refusals that keep an unpressable or unfirable rule off the list.

// The offer, for three real lines. The command's own words lead, the program
// alone is second, and the line itself is always last.
func TestTheShapesOfferedForARealCommandLine(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"git status --short", []string{"git status*", "git *", "git status --short"}},
		{"npm test", []string{"npm test*", "npm *", "npm test"}},
		{"docker compose up -d", []string{"docker compose up*", "docker *", "docker compose up -d"}},
		// A flag right after the program name leaves no run of words to widen,
		// so there are two shapes rather than three.
		{"ls -la", []string{"ls *", "ls -la"}},
		{`grep -rn "wave" internal/`, []string{"grep *", `grep -rn "wave" internal/`}},
	} {
		got := BashShapes(tc.line)
		if len(got) != len(tc.want) {
			t.Fatalf("%q offers %q, want %q", tc.line, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%q offers %q, want %q", tc.line, got, tc.want)
			}
		}
	}
}

// THE LINE IS ALWAYS THE LAST ENTRY, which is what lets the card call that one
// "just this line" and mean it.
func TestTheLineItselfIsAlwaysTheLastShape(t *testing.T) {
	for _, line := range []string{
		"ls", "git status --short", "npm test", "make -j8 build", "cargo test --all",
	} {
		shapes := BashShapes(line)
		if len(shapes) == 0 {
			t.Fatalf("%q derived nothing", line)
		}
		if shapes[len(shapes)-1] != line {
			t.Fatalf("%q ends its offer with %q", line, shapes[len(shapes)-1])
		}
	}
}

// NEVER MORE THAN THREE. A person reads the offer across one line and picks
// once.
func TestNoLineOffersMoreThanThreeShapes(t *testing.T) {
	for _, line := range []string{
		"git status --short", "docker compose up -d --build --remove-orphans",
		"kubectl get pods -n prod -o wide", "ls",
	} {
		if shapes := BashShapes(line); len(shapes) > bashShapeCount {
			t.Fatalf("%q offers %d shapes: %q", line, len(shapes), shapes)
		}
	}
}

// A BARE WILDCARD IS NEVER ON THE LIST, and neither is a shape with no word in
// it at all. Either would be an approval for every command there is, offered
// under a keystroke somebody presses in a hurry.
func TestNoShapeIsABareWildcard(t *testing.T) {
	for _, line := range []string{
		"git status --short", "ls -la", "./configure --prefix=/usr", "make", "npm test",
	} {
		for _, shape := range BashShapes(line) {
			if strings.Trim(shape, "* ") == "" {
				t.Fatalf("%q offered %q", line, shape)
			}
			if len(literalWords(shape)) == 0 {
				t.Fatalf("%q offered %q, which names no command", line, shape)
			}
		}
	}
}

// SUDO IS NOT A COMMAND. The program-alone shape of a sudo line would say the
// person approved running things as root, which is the opposite of what they
// were asked, so it is left off and the words under it are offered instead.
func TestTheProgramShapeOfASudoLineIsNotOffered(t *testing.T) {
	shapes := BashShapes("sudo apt-get update")
	for _, shape := range shapes {
		if shape == "sudo *" || shape == "doas *" {
			t.Fatalf("the offer widens to elevation itself: %q", shapes)
		}
	}
	if len(shapes) != 2 || shapes[0] != "sudo apt-get update*" {
		t.Fatalf("the offer is %q, want the command's own words and then the line", shapes)
	}
}

// A COMPOUND LINE DERIVES NOTHING. An allow rule cannot fire for one, so every
// shape drawn off it would be a choice between three rules that do nothing.
func TestACompoundLineOffersNoShapesAtAll(t *testing.T) {
	for _, line := range []string{
		"cd /tmp && rm -rf build",
		"git status | head",
		"echo hi; echo there",
		"echo $(whoami)",
	} {
		if shapes := BashShapes(line); shapes != nil {
			t.Fatalf("%q offered %q", line, shapes)
		}
	}
}

// EVERY SHAPE HAS TO BE ONE AN ALLOW RULE COULD FIRE FOR, asked of the matcher
// itself rather than believed.
func TestEveryShapeIsOneAnAllowCouldFireFor(t *testing.T) {
	for _, line := range []string{
		"git status --short", "npm test", `git commit -m "one, two"`, "ls *.go",
		"sudo apt-get update", "make -j8",
	} {
		for _, shape := range BashShapes(line) {
			if !bashShapeHolds(shape) {
				t.Fatalf("%q offered %q, which cannot be written down", line, shape)
			}
		}
	}
}

// A control byte in the line — a model's own text reached this card — is never
// a shape. A rule nobody can read back in the settings sheet is not a rule.
func TestALineCarryingAnEscapeDerivesNothing(t *testing.T) {
	if shapes := BashShapes("git status\x1b[1A --short"); shapes != nil {
		t.Fatalf("an escape was offered as a shape: %q", shapes)
	}
	if bashShapeHolds("git \x07status*") {
		t.Fatal("a shape carrying a control byte was accepted")
	}
}

// And the write refuses the same shapes the deriver refuses, so a caller that
// came by some other road cannot put one in the row.
func TestTheWriteRefusesAShapeThatNamesNoCommand(t *testing.T) {
	dir := profileWith(t, nil)
	for _, shape := range []string{"*", "* *", "sudo *", "doas *"} {
		if err := RememberBashApproval(dir, shape); err == nil {
			t.Fatalf("%q was written into the row", shape)
		}
	}
	if got := BashApprovalsAt(dir); got != "" {
		t.Fatalf("the row reads %q after four refusals", got)
	}
}

// The chosen shape is what lands, and the row still reads back as what was
// written: the card banks a glob and the settings sheet shows a glob.
func TestAChosenShapeIsWhatTheRowKeeps(t *testing.T) {
	dir := profileWith(t, nil)
	if err := RememberBashApproval(dir, "git status*"); err != nil {
		t.Fatalf("the write failed: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "allow git status*" {
		t.Fatalf("the row reads %q", got)
	}
	rules, err := ParseBashApprovals(BashApprovalsAt(dir))
	if err != nil || len(rules) != 1 || rules[0].Match != "git status*" {
		t.Fatalf("the row read back as %v (%v)", rules, err)
	}
	// And the shape it banked answers the next command of that shape, which is
	// the whole point of banking one: a second press writes nothing.
	if err := RememberBashApproval(dir, "git status --porcelain"); err != nil {
		t.Fatalf("the second press reported an error: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "allow git status*" {
		t.Fatalf("the row grew a second rule under one that already allows: %q", got)
	}
}

// FormatBashApprovals still round-trips a row of globs.
func TestARowOfShapesRoundTrips(t *testing.T) {
	rules := []BashRule{
		{Match: "git status*", Action: "allow"},
		{Match: `git commit -m "a, b"*`, Action: "allow"},
		{Match: "rm -rf *", Action: "deny"},
	}
	read, err := ParseBashApprovals(FormatBashApprovals(rules))
	if err != nil {
		t.Fatalf("the row does not parse back: %v", err)
	}
	if len(read) != len(rules) {
		t.Fatalf("the row read back as %v", read)
	}
	for i := range read {
		if read[i] != rules[i] {
			t.Fatalf("entry %d read back as %+v, want %+v", i, read[i], rules[i])
		}
	}
}
