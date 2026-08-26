package tui3

import "testing"

// ONE FILE IS NOT ONE FILES. parseHomeRepo is the whole of the repository band,
// and the reading it gets wrong most often is the one a person sees most often.
func TestTheRepoBandCountsOneFileSingular(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raw    string
		want   string
		branch string
	}{
		{
			name:   "one changed file",
			raw:    "# branch.head main\n1 .M N... 100644 100644 100644 abc abc README.md\n",
			want:   "main · 1 file dirty",
			branch: "main",
		},
		{
			name:   "two changed files",
			raw:    "# branch.head main\n1 .M N... 100644 100644 100644 abc abc README.md\n? chat.log\n",
			want:   "main · 2 files dirty",
			branch: "main",
		},
		{
			name:   "a clean repository says only its branch",
			raw:    "# branch.head main\n",
			want:   "main",
			branch: "main",
		},
		{
			name:   "every clause at once",
			raw:    "# branch.head feature/home\n# branch.ab +1 -3\n1 .M N... 100644 100644 100644 a a x\n? y\n",
			want:   "feature/home · 2 files dirty · ahead 1 · behind 3",
			branch: "feature/home",
		},
		{
			// A DETACHED HEAD IS NOT A BRANCH, and the composer layer says where a
			// task will run out of exactly this field — so a tree with no branch
			// draws the project and nothing after it (composerlayer.go).
			name:   "a detached head names no branch",
			raw:    "# branch.head (detached)\n? y\n",
			want:   "1 file dirty",
			branch: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, branch := parseHomeRepo(tc.raw)
			if got != tc.want {
				t.Fatalf("the repo band reads %q, wanted %q", got, tc.want)
			}
			if branch != tc.branch {
				t.Fatalf("the branch reads %q, wanted %q", branch, tc.branch)
			}
		})
	}
}
