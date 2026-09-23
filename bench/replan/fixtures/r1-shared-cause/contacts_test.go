package contacts

import (
	"reflect"
	"testing"
)

func TestReport1SameEmailIgnoresStraySpaces(t *testing.T) {
	if !SameEmail(" ada@example.com", "Ada@Example.com ") {
		t.Fatal("two spellings of one address were treated as two people")
	}
	if SameEmail("ada@example.com", "grace@example.com") {
		t.Fatal("two different addresses were treated as one")
	}
}

func TestReport2DedupFoldsSpacingAndCase(t *testing.T) {
	got := Dedup([]string{"Ada Lovelace", "ada  lovelace", " Grace Hopper", "grace hopper", "Alan Turing"})
	want := []string{"Ada Lovelace", " Grace Hopper", "Alan Turing"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dedup = %q, want %q", got, want)
	}
}

func TestReport3LookupIgnoresStraySpaces(t *testing.T) {
	book := NewBook(map[string]string{"Alan Turing": "555-0100"})
	for _, name := range []string{"alan turing ", "Alan  Turing", " ALAN TURING"} {
		phone, ok := book.Lookup(name)
		if !ok || phone != "555-0100" {
			t.Errorf("Lookup(%q) = %q, %v; want 555-0100", name, phone, ok)
		}
	}
	if _, ok := book.Lookup("Grace Hopper"); ok {
		t.Error("found a name that is not in the book")
	}
}

func TestReport4SlugCollapsesSpaces(t *testing.T) {
	for title, want := range map[string]string{
		"  Hello   World ": "hello-world",
		"Hello World":      "hello-world",
		"One":              "one",
	} {
		if got := Slug(title); got != want {
			t.Errorf("Slug(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestReport5TagCountsFoldSpacingAndCase(t *testing.T) {
	got := CountTags([]string{"Go", " go", "GO ", "rust"})
	want := map[string]int{"go": 3, "rust": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CountTags = %v, want %v", got, want)
	}
}

func TestReport6InitialsKeepTheLastName(t *testing.T) {
	for name, want := range map[string]string{
		"grace brewster hopper": "GBH",
		"Ada Lovelace":          "AL",
		"plato":                 "P",
	} {
		if got := Initials(name); got != want {
			t.Errorf("Initials(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReport7PhoneGroupsThreeThreeFour(t *testing.T) {
	if got := FormatPhone("5551234567"); got != "555-123-4567" {
		t.Fatalf("FormatPhone = %q, want 555-123-4567", got)
	}
	if got := FormatPhone("12345"); got != "12345" {
		t.Fatalf("FormatPhone changed a short number: %q", got)
	}
}
