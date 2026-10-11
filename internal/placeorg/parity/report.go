package parity

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// WriteReport writes the comparison the parity lane owes. The numbers are the
// run's; a later run replaces the file.
func WriteReport(path string, r Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Places parity\n\n")
	fmt.Fprintf(&b, "Go organiser against the owner's saved embeddings (%d chats). Cluster ids are arbitrary, so the score is the adjusted Rand index. Names are a model's and are not compared. The saved reference is the single-anchor run; this port also scores an end anchor when a chat has one, and a chat earns every home at or above the floor and within the delta, with no cap.\n\n", r.Chats)
	fmt.Fprintf(&b, "Wall: %s. Space: `%s`.\n\n", r.Wall.Round(time.Millisecond), r.Space)
	if r.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n\n", r.Summary)
	}
	b.WriteString("## Levels\n\n")
	b.WriteString("| Level | ARI | Floor |\n| --- | --- | --- |\n")
	for _, name := range LevelNames {
		fmt.Fprintf(&b, "| %s | %.3f | %.2f |\n", name, r.ARI[name], Floor)
	}
	fmt.Fprintf(&b, "\n## Invariants\n\n%d of %d passed.\n\n", r.Passed, len(r.Invariants))
	for _, inv := range r.Invariants {
		mark := "pass"
		if !inv.OK {
			mark = "fail"
			if inv.Why != "" {
				mark += ": " + inv.Why
			}
		}
		fmt.Fprintf(&b, "- %s — %s\n", inv.Name, mark)
	}
	b.WriteString("\n## Op list diff\n\n")
	b.WriteString("Counts are kinds. The reference list is the lines its run printed, including lines that are not tree operations. The Go list is the plan.\n\n")
	b.WriteString("| Kind | Reference | Go |\n| --- | --- | --- |\n")
	keys := map[string]struct{}{}
	for k := range r.RefOps {
		keys[k] = struct{}{}
	}
	for k := range r.OpCounts {
		keys[k] = struct{}{}
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", k, r.RefOps[k], r.OpCounts[k])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
