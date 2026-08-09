package specidentifiers

import (
	"strings"
	"testing"
)

func TestJavaScriptWhitespaceAndLineSeparators(t *testing.T) {
	got := ExtractSpecIdentifiers("Use `\u2028WidgetName\u2029` and \"Auto\u2028Table\".")
	if len(got) != 2 || got[0].Value != "WidgetName" || got[1].Value != "Auto\u2028Table" {
		t.Fatalf("unexpected identifiers: %#v", got)
	}
}

func TestMarkdownBulletIsRequirement(t *testing.T) {
	got := ClassifyLineContexts("- use `WidgetName`")
	if len(got) != 1 || got[0].Evidence {
		t.Fatalf("markdown bullet classified as evidence: %#v", got)
	}
}

// TestSpecNamedFileIsSatisfiedByChangingIt pins the go-humanize regression.
//
// A planner issue named its target file the way issue files do:
//
//	## Files
//	- **Modify**: `ordinals.go` — two modulus operands changed
//
// looksLikePath requires a "/", so a repo-root basename is classified KindCode
// and checked by content — the filename had to appear INSIDE the changed
// files' text. A Go source file does not contain its own name, so the gate
// reported `ordinals.go` missing while ordinals.go was the only file the fix
// touched, and kept re-dispatching a correct, green tree.
func TestSpecNamedFileIsSatisfiedByChangingIt(t *testing.T) {
	changed := []string{"ordinals.go", "ordinals_test.go"}
	// Real content of the fixed file: it never mentions its own filename.
	content := "package humanize\n\nimport \"strconv\"\n\n" +
		"func Ordinal(x int) string {\n\tsuffix := \"th\"\n" +
		"\tswitch x % 10 {\n\tcase 2:\n\t\tif x%100 != 12 {\n" +
		"\t\t\tsuffix = \"nd\"\n\t\t}\n\t}\n\treturn strconv.Itoa(x) + suffix\n}\n"
	result := CheckIdentifiersInTree(CheckIdentifiersInput{
		Identifiers: []SpecIdentifier{
			{Value: "ordinals.go", Kind: KindCode, Source: "- **Modify**: `ordinals.go`"},
			{Value: "ordinals_test.go", Kind: KindCode, Source: "- `ordinals_test.go`: existing test suite"},
			{Value: "Ordinal", Kind: KindCode, Source: "Implements: `func Ordinal(x int) string`"},
		},
		SearchText: content, ChangedFiles: &changed,
	})
	if len(result.Missing) != 0 {
		t.Fatalf("missing = %#v, want none: a file the spec names and the fix changed is satisfied", result.Missing)
	}
	if len(result.Present) != 3 {
		t.Fatalf("present = %#v, want all three", result.Present)
	}
}

// TestSpecNamedFileStillMissingWhenUnchanged keeps the gate honest: the
// fallback only credits files that were actually changed, so a coder that
// edited something else is still caught.
func TestSpecNamedFileStillMissingWhenUnchanged(t *testing.T) {
	changed := []string{"bytes.go"}
	result := CheckIdentifiersInTree(CheckIdentifiersInput{
		Identifiers: []SpecIdentifier{
			{Value: "ordinals.go", Kind: KindCode, Source: "- **Modify**: `ordinals.go`"},
			{Value: "TeensException", Kind: KindCode, Source: "Exports: `TeensException`"},
		},
		SearchText:   "package humanize\n\nfunc Bytes(s uint64) string { return \"\" }\n",
		ChangedFiles: &changed,
	})
	if len(result.Missing) != 2 {
		t.Fatalf("missing = %#v, want both (wrong file touched, name absent)", result.Missing)
	}
}

// TestChangedFileFallbackDoesNotShadowContentMatching: a selector like
// `strconv.Itoa` is shaped like a basename but is a code name. It is satisfied
// by appearing in the content, and is NOT credited by the file list.
func TestChangedFileFallbackDoesNotShadowContentMatching(t *testing.T) {
	changed := []string{"ordinals.go"}
	result := CheckIdentifiersInTree(CheckIdentifiersInput{
		Identifiers: []SpecIdentifier{
			{Value: "strconv.Itoa", Kind: KindCode, Source: "Consumes: `strconv.Itoa`"},
			{Value: "strings.Builder", Kind: KindCode, Source: "Consumes: `strings.Builder`"},
		},
		SearchText:   "return strconv.Itoa(x) + suffix",
		ChangedFiles: &changed,
	})
	if len(result.Present) != 1 || result.Present[0].Value != "strconv.Itoa" {
		t.Fatalf("present = %#v, want only strconv.Itoa", result.Present)
	}
	if len(result.Missing) != 1 || result.Missing[0].Value != "strings.Builder" {
		t.Fatalf("missing = %#v, want strings.Builder", result.Missing)
	}
}

// TestQuotedExpressionIsNotationNotAnIdentifier is the go-humanize regression.
//
// A planner issue wrote its acceptance checks as shell commands. The
// single-quoted grep ARGUMENT is a multi-token phrase, so the quoted-phrase
// branch — which exists for multi-word display names — enforced the code
// expression as an identifier and the gate re-dispatched a green tree.
func TestQuotedExpressionIsNotationNotAnIdentifier(t *testing.T) {
	issue := "- [ ] `grep -n 'x%100 != 12' ordinals.go` exits 0 and matches line 16\n" +
		"- [ ] `grep -n 'x%100 != 13' ordinals.go` exits 0 and matches line 20\n"
	for _, id := range ExtractSpecIdentifiers(issue) {
		if strings.Contains(id.Value, "!=") {
			t.Errorf("expression %q became an identifier obligation (%s)", id.Value, id.Kind)
		}
	}
}

// TestQuotedDisplayNamesSurvive: the branch's real purpose is intact — a
// multi-word NAME carries no operators and stays enforceable.
func TestQuotedDisplayNamesSurvive(t *testing.T) {
	got := ExtractSpecIdentifiers(`The heading must read "Auto Table of Contents" exactly.`)
	found := false
	for _, id := range got {
		if id.Value == "Auto Table of Contents" {
			found = true
		}
	}
	if !found {
		t.Fatalf("display name lost: %#v", got)
	}
}

// TestLooksLikeExpressionBoundary pins the crisp rule: operators mean notation,
// ordinary prose names do not.
func TestLooksLikeExpressionBoundary(t *testing.T) {
	for _, expr := range []string{
		"x%100 != 12", "a == b", "count >= 3", "x < y", "flag && other",
		"lhs || rhs", "total - 1", "n * 2",
	} {
		if !looksLikeExpression(expr) {
			t.Errorf("%q must be treated as notation", expr)
		}
	}
	for _, name := range []string{
		"Auto Table of Contents", "Sign In", "Dark Mode Toggle",
		"user profile page", "Save and Exit",
	} {
		if looksLikeExpression(name) {
			t.Errorf("%q is a display name, not notation", name)
		}
	}
}

// TestMarkersKeepTheirPunctuation: markers are classified before the phrase
// branch and must be unaffected by the expression rule.
func TestMarkersKeepTheirPunctuation(t *testing.T) {
	got := ExtractSpecIdentifiers("Preserve `<!-- TOC -->` in the output.")
	found := false
	for _, id := range got {
		if id.Value == "<!-- TOC -->" && id.Kind == KindMarker {
			found = true
		}
	}
	if !found {
		t.Fatalf("marker lost: %#v", got)
	}
}
