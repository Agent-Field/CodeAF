package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A JOB'S AREA IS WHERE MOST OF ITS FILES ARE: a declared area when the
// repository names its areas, otherwise the top-level directory, and nothing
// when no file says.
func TestJobAreaIsWhereMostOfTheJobsFilesAre(t *testing.T) {
	cases := []struct {
		name  string
		areas []string
		paths []string
		want  string
	}{
		{"no files", nil, nil, ""},
		{"top-level directory", nil, []string{"cli/main.go", "cli/flags.go", "docs/cli.md"}, "cli/"},
		{"a tie goes to the lexically first directory", nil, []string{"web/a.ts", "api/b.go"}, "api/"},
		{"files at the root are no area", nil, []string{"README.md", "go.mod"}, ""},
		{"root files do not outvote a directory", nil, []string{"README.md", "go.mod", "cli/main.go"}, "cli/"},
		{"a declared area wins", []string{"tui", "webhooks"}, []string{"webhooks/x.go", "webhooks/y.go", "tui/z.go"}, "webhooks"},
		{"a declared area with a slash", []string{"webhooks/"}, []string{"webhooks/x.go"}, "webhooks/"},
		{"a declared area matches whole segments only", []string{"webhooks"}, []string{"webhooks2/x.go", "webhooks2/y.go"}, "webhooks2/"},
		{"a declared tie goes to the first declared", []string{"tui", "spend"}, []string{"spend/a.go", "tui/b.go"}, "tui"},
		{"a nested declared area", []string{"internal/tui3"}, []string{"internal/tui3/a.go", "internal/session/b.go", "internal/session/c.go"}, "internal/tui3"},
		{"no declared area matches falls back to directories", []string{"tui"}, []string{"cmd/x.go"}, "cmd/"},
		{"a file named like the area is not under it", []string{"Makefile"}, []string{"Makefile"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := factory.JobArea(c.areas, c.paths); got != c.want {
				t.Fatalf("JobArea(%q, %q) = %q, want %q", c.areas, c.paths, got, c.want)
			}
		})
	}
}
