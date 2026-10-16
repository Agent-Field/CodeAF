package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// THE LINE COUNTS OF A CHECKOUT PAGE ARE GIT'S OWN NUMSTAT, READ FOR THE PAGE'S
// TRACKED PATHS ONLY, AND EITHER ALL ATTACHED OR NONE.
//
// Every command here is a fixed, read-only git argument list run through the
// same [appGit] (diff-index, because `git diff` rewrites the index's stat cache even with optional locks off) and the same filter blanking as the status, under the call's one
// deadline: no external diff, no textconv, no fsmonitor, no clean filter. The
// pathspec is the page's paths, so the output is bounded by the page and not by
// the checkout. Every git read of the whole call, the status twice included,
// draws on ONE byte budget ([appMeter]); a read the budget cannot hold is
// too_large, never a short answer called whole.
//
// UNTRACKED FILES HAVE NO COUNTS, ON PURPOSE. Counting them means opening a
// path the checkout's owner can swap for a pipe or a link between a stat and a
// read, and a count is not worth a read that can hang or leave the root.
//
// WHAT "THE SAME CHECKOUT" MEANS HERE, SAID EXACTLY. It is NOT a content hash.
// The counts are attached only if, between the first status read and the second,
// (1) the status text is identical, (2) the index file's stamp is identical, and
// (3) each counted path's stamp (type and permission bits, size, modification
// time in nanoseconds; or "absent") is identical. A write that preserves all of
// those between the reads is not detected, and PROTOCOL.md says so.

const (
	// appNumstatMax is the most numstat text one page may read.
	appNumstatMax = 1 << 20
)

// appLinesBetween is a test's hook for the moment between the counts and the
// second status read.
var appLinesBetween = func() {}

// appMeter is the one byte budget of a whole checkout.status call: every git
// read of it, status and numstat and attribute lookups together, spends from
// the same pool, so repeating a command cannot repeat its allowance.
type appMeter struct {
	g    appGit
	left int
}

// capped is [appGit.capped] that cannot read more than the pool has left. A read
// the pool could not hold reports overflow, exactly as one past its own limit.
func (m *appMeter) capped(dir string, limit, maxLines int, args ...string) (string, bool, *appGitFail) {
	if limit > m.left {
		limit = m.left
	}
	text, overflow, fail := m.g.capped(dir, limit, maxLines, args...)
	m.left = max(m.left-len(text), 0)
	return text, overflow, fail
}

// out is one git's whole stdout, which must fit in limit bytes and in the pool.
func (m *appMeter) out(dir string, limit int, args ...string) (string, *appGitFail) {
	text, overflow, fail := m.capped(dir, limit, 0, args...)
	if fail == nil && overflow {
		return "", &appGitFail{exit: -1, stderr: "git answered more than this call reads"}
	}
	return text, fail
}

// appStamp is a path's identity short of its content, read with Lstat so that
// asking never opens it, follows it or waits on it.
func appStamp(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "absent"
		}
		return "unreadable"
	}
	return fmt.Sprintf("%o/%d/%d", info.Mode(), info.Size(), info.ModTime().UnixNano())
}

// appLine is one entry's counts, held until every read has agreed.
type appLine struct {
	added, removed int
	binary         bool
}

// appPageLines attaches counts to out.Entries and answers "" or the reason none
// were attached. first is the status text the page was cut from.
func appPageLines(g *appMeter, dir, top, gitDir string, blank []string, named map[string]bool, out *appwire.CheckoutStatus,
	overflow bool, reread func() (string, bool, *appGitFail), first string) string {
	var tracked, renamed []int
	for i, e := range out.Entries {
		if e.Submodule || e.PathUnreadable || e.Path == "" {
			continue
		}
		switch e.Kind {
		case appwire.CheckoutChanged:
			tracked = append(tracked, i)
		case appwire.CheckoutRenamed:
			if e.OldPath != "" {
				renamed = append(renamed, i)
			}
		}
	}
	if len(tracked)+len(renamed) == 0 {
		return ""
	}
	if overflow {
		return appwire.LinesIncomplete
	}
	fail := func(f *appGitFail) string {
		if f.timedOut {
			return appwire.LinesTimeout
		}
		return appwire.LinesGitFailed
	}
	got := map[int]appLine{}
	stamps := func() string {
		var b strings.Builder
		b.WriteString(appStamp(filepath.Join(gitDir, "index")))
		for _, list := range [][]int{tracked, renamed} {
			for _, i := range list {
				b.WriteString("\x00" + appStamp(filepath.Join(top, filepath.FromSlash(out.Entries[i].Path))))
			}
		}
		return b.String()
	}
	before := stamps()

	// A path whose filter attribute names a driver we blanked would be counted
	// from bytes the filter was meant to change: it stays unknown.
	skip := map[int]bool{}
	if len(named) > 0 {
		args := []string{"check-attr", "-z", "filter", "--"}
		at := map[string]int{}
		for _, list := range [][]int{tracked, renamed} {
			for _, i := range list {
				args = append(args, out.Entries[i].Path)
				at[out.Entries[i].Path] = i
			}
		}
		text, f := g.out(dir, appNumstatMax, args...)
		if f != nil {
			return fail(f)
		}
		parts := strings.Split(text, "\x00")
		for j := 0; j+2 < len(parts); j += 3 {
			if i, ok := at[parts[j]]; ok && named["filter."+parts[j+2]] {
				skip[i] = true
			}
		}
	}

	base := out.Head
	if base == "" {
		if !out.Unborn {
			return appwire.LinesGitFailed
		}
		empty, f := g.out(dir, 256, "hash-object", "-t", "tree", "/dev/null")
		if f != nil {
			return fail(f)
		}
		base = strings.TrimSpace(empty)
	}
	diff := func(renames bool, paths []string) ([]string, string) {
		args := append(append([]string{}, blank...), "diff-index", "--numstat", "-z", "--no-ext-diff", "--no-textconv",
			"--ignore-submodules=dirty", "--no-color")
		if renames {
			args = append(args, "-M")
		} else {
			args = append(args, "--no-renames")
		}
		args = append(append(args, base, "--"), paths...)
		text, overflow, f := g.capped(dir, appNumstatMax, 0, args...)
		switch {
		case f != nil:
			return nil, fail(f)
		case overflow:
			return nil, appwire.LinesTooLarge
		}
		return strings.Split(strings.TrimSuffix(text, "\x00"), "\x00"), ""
	}

	var plain []string
	for _, i := range tracked {
		if !skip[i] {
			plain = append(plain, out.Entries[i].Path)
		}
	}
	if len(plain) > 0 {
		records, reason := diff(false, plain)
		if reason != "" {
			return reason
		}
		rows := map[string]appLine{}
		for _, rec := range records {
			if rec == "" {
				continue
			}
			f := strings.SplitN(rec, "\t", 3)
			if len(f) != 3 || f[2] == "" {
				return appwire.LinesGitFailed // a rename row cannot appear under --no-renames
			}
			rows[f[2]] = appNumstatRow(f[0], f[1])
		}
		for _, i := range tracked {
			if skip[i] {
				continue
			}
			// A complete diff that lists no row for a path says the work tree is
			// the base's content there: staged and unstaged edits that cancel.
			got[i] = rows[out.Entries[i].Path]
		}
	}

	var pairs []string
	for _, i := range renamed {
		if !skip[i] {
			pairs = append(pairs, out.Entries[i].OldPath, out.Entries[i].Path)
		}
	}
	if len(pairs) > 0 {
		records, reason := diff(true, pairs)
		if reason != "" {
			return reason
		}
		type pair struct{ old, new string }
		rows := map[pair]appLine{}
		for j := 0; j < len(records); j++ {
			f := strings.SplitN(records[j], "\t", 3)
			switch {
			case records[j] == "":
			case len(f) != 3:
				return appwire.LinesGitFailed
			case f[2] != "":
				// A row that is not a rename pairs nothing.
			case j+2 < len(records):
				rows[pair{records[j+1], records[j+2]}] = appNumstatRow(f[0], f[1])
				j += 2
			default:
				return appwire.LinesGitFailed // a rename cut off from its paths
			}
		}
		for _, i := range renamed {
			if row, ok := rows[pair{out.Entries[i].OldPath, out.Entries[i].Path}]; ok && !skip[i] {
				got[i] = row
			}
		}
	}

	appLinesBetween()
	again, over, f := reread()
	switch {
	case f != nil:
		return fail(f)
	case over:
		return appwire.LinesTooLarge
	case again != first || stamps() != before:
		return appwire.LinesChanged
	}
	for i, row := range got {
		e := &out.Entries[i]
		if row.binary {
			e.Binary = true
			continue
		}
		added, removed := row.added, row.removed
		e.Added, e.Removed = &added, &removed
	}
	return ""
}

// appNumstatRow reads numstat's two columns; "-" in both is binary.
func appNumstatRow(added, removed string) appLine {
	if added == "-" && removed == "-" {
		return appLine{binary: true}
	}
	a, errA := strconv.Atoi(added)
	r, errR := strconv.Atoi(removed)
	if errA != nil || errR != nil {
		return appLine{binary: true}
	}
	return appLine{added: a, removed: r}
}
