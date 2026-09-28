//go:build !windows

package patch

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnifiedDiffUsesRealLineAlignment(t *testing.T) {
	// An insertion must not masquerade as a replacement.
	got := GenerateTwoFilesPatch("x", "a\nb\nc\nd\ne\nf\ng\n", "a\nb\nX\nc\nd\ne\nf\ng\n")
	want := "Index: x\n===================================================================\n--- x\n+++ x\n" +
		"@@ -1,6 +1,7 @@\n a\n b\n+X\n c\n d\n e\n f\n"
	if got != want {
		t.Fatalf("insertion diff:\n%s\nwant:\n%s", got, want)
	}
	if got := GenerateTwoFilesPatch("x", "a\n", ""); got != "Index: x\n===================================================================\n--- x\n+++ x\n@@ -1,1 +0,0 @@\n-a\n" {
		t.Fatalf("deletion diff: %q", got)
	}
}

func TestTwoFilesPatchUnchangedEmptyFileIsHeaderOnly(t *testing.T) {
	// Two identical empty versions still yield a header-only patch.
	want := "Index: empty\n===================================================================\n--- empty\n+++ empty\n"
	if got := GenerateTwoFilesPatch("empty", "", ""); got != want {
		t.Fatalf("unchanged empty patch = %q, want %q", got, want)
	}
}

func TestLineDiffLargePathologicalInputIsBounded(t *testing.T) {
	// A 10k-line replacement must not allocate an old-by-new matrix; the
	// bounded linear-space path still returns a patch.
	var oldContent, newContent strings.Builder
	for index := 0; index < 10_000; index++ {
		fmt.Fprintf(&oldContent, "old-%05d\n", index)
		fmt.Fprintf(&newContent, "new-%05d\n", index)
	}
	got := GenerateTwoFilesPatch("large", oldContent.String(), newContent.String())
	if !strings.HasPrefix(got, "Index: large\n") ||
		!strings.Contains(got, "-old-00000") ||
		!strings.Contains(got, "+new-09999") {
		t.Fatalf("large replacement patch was not generated: prefix=%q len=%d", got[:min(len(got), 80)], len(got))
	}
}
