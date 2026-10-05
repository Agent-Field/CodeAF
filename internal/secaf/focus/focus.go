// Package focus is what an audit of the changes tells its hunters: which files
// changed, which sit near them, and the change itself.
//
// sec-af's PR mode filtered findings to the changed files only AFTER every
// hunter had scanned the whole repository, so an audit of a ten-line change
// cost a whole audit. Here the scan step is told the change up front and
// reports only what the change touches or makes reachable; the filter after
// the hunt stays, as the backstop it was.
//
// IT RIDES THE CONTEXT, because the hunters are reached through sec-af's own
// tree of calls between reasoners, which carry the context in process and
// nothing else that the algorithm does not already read. An audit of the whole
// repository carries none, and its hunters' prompts are byte for byte the
// prompts sec-af wrote.
package focus

import (
	"context"
	"strings"
)

// Focus is the change an audit is about.
type Focus struct {
	// Base is how the change's starting point is said: `origin/dev`.
	Base string
	// Changed is the files the change touched; Nearby the files that name
	// one of them.
	Changed, Nearby []string
	// Diff is the change itself, already cut to a size a prompt holds.
	Diff string
}

type key struct{}

// With is ctx carrying f.
func With(ctx context.Context, f Focus) context.Context { return context.WithValue(ctx, key{}, f) }

// From is the focus ctx carries, and false for an audit of the whole
// repository.
func From(ctx context.Context) (Focus, bool) {
	f, ok := ctx.Value(key{}).(Focus)
	return f, ok && len(f.Changed) > 0
}

// Section is the words appended to a hunter's scan prompt.
func (f Focus) Section() string {
	var b strings.Builder
	b.WriteString("\n\nCHANGE UNDER AUDIT:\n")
	b.WriteString("This audit covers a change to the repository, not the whole of it. Report only locations in the changed files, ")
	b.WriteString("or in the files near them where the change makes the code reachable or exploitable. Code elsewhere is context: ")
	b.WriteString("read it to trace where data comes from and goes, but do not report it.\n")
	b.WriteString("\nChanged files")
	if f.Base != "" {
		b.WriteString(" (since " + f.Base + ")")
	}
	b.WriteString(":\n")
	for _, file := range f.Changed {
		b.WriteString("- " + file + "\n")
	}
	if len(f.Nearby) > 0 {
		b.WriteString("\nFiles near the change (they name a changed file):\n")
		for _, file := range f.Nearby {
			b.WriteString("- " + file + "\n")
		}
	}
	if diff := strings.TrimSpace(f.Diff); diff != "" {
		b.WriteString("\nThe change (git diff):\n")
		b.WriteString(diff)
		b.WriteString("\n")
	}
	return b.String()
}
