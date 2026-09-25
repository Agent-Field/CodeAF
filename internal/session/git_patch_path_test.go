package session

import "testing"

// Contract 1c: the patch reader must use git's destination path for both
// quoted and bare headers, including names with a destination prefix inside.
func TestPatchSectionPathReadsRealName(t *testing.T) {
	cases := []struct{ name, patch, want string }{
		{"embedded destination", "diff --git a/x b/plandb.db b/x b/plandb.db\nindex 1..2\n", "x b/plandb.db"},
		{"octal", "diff --git \"a/odd name \\303\\251'q.txt\" \"b/odd name \\303\\251'q.txt\"\nindex 1..2\n", "odd name é'q.txt"},
		{"quote", "diff --git \"a/has\\\"quote.txt\" \"b/has\\\"quote.txt\"\nindex 1..2\n", "has\"quote.txt"},
		{"newline", "diff --git \"a/new\\nline.txt\" \"b/new\\nline.txt\"\nindex 1..2\n", "new\nline.txt"},
		{"rename", "diff --git a/old.txt b/new.txt\nsimilarity index 100%\nrename from old.txt\nrename to new.txt\n", "new.txt"},
		{"deletion", "diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\n--- a/gone.txt\n+++ /dev/null\n", "gone.txt"},
		{"older engine", "diff --git a/old.txt b/new.txt\n--- a/old.txt\n+++ b/new.txt\n", "new.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PatchSectionPath(tc.patch); got != tc.want {
				t.Fatalf("PatchSectionPath = %q, want %q", got, tc.want)
			}
		})
	}
}
