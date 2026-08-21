package tui3

import "testing"

// ONE FILE IS NOT ONE FILES. parseHomeRepo is the whole of the repository band,
// and the reading it gets wrong most often is the one a person sees most often.
func TestTheRepoBandCountsOneFileSingular(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "one changed file",
			raw:  "# branch.head main\n1 .M N... 100644 100644 100644 abc abc README.md\n",
			want: "main · 1 file dirty",
		},
		{
			name: "two changed files",
			raw:  "# branch.head main\n1 .M N... 100644 100644 100644 abc abc README.md\n? chat.log\n",
			want: "main · 2 files dirty",
		},
		{
			name: "a clean repository says only its branch",
			raw:  "# branch.head main\n",
			want: "main",
		},
		{
			name: "every clause at once",
			raw:  "# branch.head feature/home\n# branch.ab +1 -3\n1 .M N... 100644 100644 100644 a a x\n? y\n",
			want: "feature/home · 2 files dirty · ahead 1 · behind 3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseHomeRepo(tc.raw); got != tc.want {
				t.Fatalf("the repo band reads %q, wanted %q", got, tc.want)
			}
		})
	}
}
