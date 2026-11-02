package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A JOB'S AREA IS WHERE MOST OF ITS FILES ARE: a declared area when the
// repository names its areas, otherwise the top-level directory (two segments
// under a container such as internal/), and nothing when no file says.
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
		{"a container directory's area is its first two segments", nil, []string{
			"internal/factory/area.go", "internal/factory/api/area.go", "internal/factory/serve/serve.go",
			"cmd/codeaf/appfactory.go", "cmd/codeaf/app.go", "docs/design/app-v3/PROTOCOL.md"}, "internal/factory/"},
		{"cmd is a container too", nil, []string{"cmd/codeaf/a.go", "cmd/codeaf/b.go", "internal/factory/c.go"}, "cmd/codeaf/"},
		{"every container is two segments deep", nil, []string{"src/a/x", "pkg/b/x", "lib/c/x", "app/d/x", "apps/e/x", "packages/f/x", "crates/g/x"}, "app/d/"},
		{"a file directly inside a container is the container", nil, []string{"internal/doc.go"}, "internal/"},
		{"a non-container stays one segment", nil, []string{"web/src/a.ts", "web/lib/b.ts"}, "web/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := factory.JobArea(c.areas, c.paths); got != c.want {
				t.Fatalf("JobArea(%q, %q) = %q, want %q", c.areas, c.paths, got, c.want)
			}
		})
	}
}
