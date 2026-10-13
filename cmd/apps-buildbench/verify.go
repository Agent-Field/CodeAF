package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// verdict is the bench's judgement of one set of files. Issues fail the run;
// Notes are quality gaps that do not.
type verdict struct {
	Issues []string
	Notes  []string
}

func (v verdict) pass() bool { return len(v.Issues) == 0 }

// verify runs schema validation, a decode, and the bench's own binding and
// quality checks (internal/apps/check is not built yet).
func verify(files spec.Files, kit *spec.Kit) verdict {
	var v verdict
	for _, issue := range spec.Validate(files) {
		v.Issues = append(v.Issues, issue.String())
	}
	if !v.pass() {
		return v
	}
	bundle, err := spec.Decode(files)
	if err != nil {
		v.Issues = append(v.Issues, "decode: "+err.Error())
		return v
	}
	c := newChecker(bundle, kit)
	c.run()
	v.Issues, v.Notes = c.issues, c.notes
	return v
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func report(issues []string) string { return strings.Join(issues, "\n") }

func fileIssue(file, format string, args ...any) string {
	return file + ": " + fmt.Sprintf(format, args...)
}
