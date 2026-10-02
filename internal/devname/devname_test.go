package devname

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanTrimsAndKeepsOneToThirtyTwoCharacters(t *testing.T) {
	cases := map[string]string{
		"  desk  ":                "desk",
		"a":                       "a",
		strings.Repeat("é", Most): strings.Repeat("é", Most),
		"my laptop (work)":        "my laptop (work)",
	}
	for typed, want := range cases {
		if got, err := Clean(typed); err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", typed, got, err, want)
		}
	}
}

func TestCleanRefusesWhatCannotBeANameWithTheReason(t *testing.T) {
	cases := map[string]error{
		"":                          ErrEmpty,
		" \t ":                      ErrEmpty,
		strings.Repeat("x", Most+1): ErrTooLong,
		"two\nlines":                ErrControl,
		"esc\x1b[31mred":            ErrControl,
		"c1\u009bcontrol":           ErrControl,
	}
	for typed, want := range cases {
		if _, err := Clean(typed); !errors.Is(err, want) {
			t.Errorf("Clean(%q) error = %v; want %v", typed, err, want)
		}
	}
}

func TestNameIsTheHostNameUntilAPersonChoosesOne(t *testing.T) {
	dir := t.TempDir()
	if got := Name(dir); got != Default() {
		t.Fatalf("unnamed computer is %q; want the host name %q", got, Default())
	}
	if _, err := Set(dir, "  studio "); err != nil {
		t.Fatal(err)
	}
	if got := Name(dir); got != "studio" {
		t.Fatalf("named computer is %q; want studio", got)
	}
}

func TestARefusedNameChangesNothing(t *testing.T) {
	dir := t.TempDir()
	if _, err := Set(dir, "studio"); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(dir, "bad\x00"); err == nil {
		t.Fatal("a name with a control character was kept")
	}
	if got := Name(dir); got != "studio" {
		t.Fatalf("name after a refused rename is %q; want studio", got)
	}
}

func TestDefaultCutsTheDomainOff(t *testing.T) {
	if strings.Contains(Default(), ".") && !strings.HasPrefix(Default(), ".") {
		t.Fatalf("default %q still carries a domain", Default())
	}
}

func TestShownCutsTheLocalSuffixAMacHostNameCarries(t *testing.T) {
	cases := map[string]string{
		"mac.local":     "mac",
		"Mac.LOCAL":     "Mac",
		"desk":          "desk",
		"my.laptop":     "my.laptop",
		".local":        ".local",
		"a.local.local": "a.local",
	}
	for name, want := range cases {
		if got := Shown(name); got != want {
			t.Errorf("Shown(%q) = %q; want %q", name, got, want)
		}
	}
}

func TestNameIsShownThroughTheSameRuleForAChosenName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("studio.local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Name(dir); got != "studio" {
		t.Fatalf("Name = %q; a chosen name reads like every other label", got)
	}
}
