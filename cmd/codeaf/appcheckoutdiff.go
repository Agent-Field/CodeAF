package main

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// CHECKOUT.DIFF AND CHECKOUT.LOCATE: ONE ROW OF THE STATUS, FOUND BY ITS PATH.
//
// Both read ONE status snapshot ([appCheckoutSnapshot]) and match the request's
// path byte for byte against that snapshot's rows. The path a client sends is
// therefore never a place to open, only a name to look up, and a path that is
// not a changed row of this checkout is not_found whatever else it is. The same
// 3 s clock, 6 MiB pool and two admission slots as checkout.status cover the
// snapshot and everything read after it.

// appCheckoutRow is the one row a request named, with the snapshot it came from.
type appCheckoutRow struct {
	snap      *appSnapshot
	entry     appwire.CheckoutEntry
	workspace string
}

// appCheckoutRowFor finds the row. Callers hold a slot and own the context.
func appCheckoutRowFor(ctx context.Context, workspace string, q appwire.CheckoutPathQuery) (*appCheckoutRow, error) {
	snap, err := appCheckoutSnapshot(ctx, workspace)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return nil, appwire.Errorf(appwire.CodeNotFound, "this agent's workspace is not a git checkout")
	}
	if q.Revision != "" && q.Revision != snap.out.Revision {
		return nil, &appwire.Error{
			Code:    appwire.CodeConflict,
			Message: "the checkout has changed since that row was shown (now at revision " + snap.out.Revision + "); read the status again",
			Detail:  appwire.ConflictDetail{Reason: appwire.ConflictStaleRevision},
		}
	}
	// A PATH THAT NAMES MORE THAN ONE ROW NAMES NONE. The rows are counted, never
	// the first taken: `git rm --cached f` leaves a staged deletion and an
	// untracked `f` under one path, and a click on the untracked file must not be
	// answered with the deletion.
	var found []appwire.CheckoutEntry
	for _, entry := range snap.entries {
		if !entry.PathUnreadable && entry.Path == q.Path {
			found = append(found, entry)
		}
	}
	if snap.overflow {
		// A status cut by its bound cannot show that no second row for the path
		// lies past the cut (untracked rows come last), so no path is confirmed.
		return nil, appwire.Errorf(appwire.CodeTooLarge, "the status is longer than the engine reads, so that path cannot be confirmed")
	}
	switch len(found) {
	case 0:
		return nil, appwire.Errorf(appwire.CodeNotFound, "that path is not a changed path in the current status")
	case 1:
		return &appCheckoutRow{snap: snap, entry: found[0], workspace: workspace}, nil
	}
	return nil, &appwire.Error{
		Code:    appwire.CodeConflict,
		Message: "more than one row of the status has that path, so none is chosen for you",
		Detail:  appwire.ConflictDetail{Reason: appwire.ConflictAmbiguousRow, Path: q.Path},
	}
}

// workspacePath is the row's path relative to the workspace folder, "" outside it.
func (r *appCheckoutRow) workspacePath() string {
	rel, err := filepath.Rel(r.workspace, filepath.Join(r.snap.top, filepath.FromSlash(r.entry.Path)))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// open is the absolute path to offer for Open and Reveal, "" when it cannot be
// proved reachable now. It is a fact, never a grant.
func (r *appCheckoutRow) open() string {
	if !utf8.ValidString(r.entry.Path) || !appReachable(r.snap.top, r.entry.Path) {
		return ""
	}
	return filepath.Join(r.snap.top, filepath.FromSlash(r.entry.Path))
}

// withCheckoutSlot runs fn under an admission slot and one 3 s clock.
func (b *appBackend) withCheckoutSlot(agent, method string, fn func(ctx context.Context, workspace string) error) error {
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	if strings.TrimSpace(workspace) == "" {
		return appwire.Errorf(appwire.CodeNotFound, "this agent has no workspace")
	}
	select {
	case appStatusSlots <- struct{}{}:
		defer func() { <-appStatusSlots }()
	default:
		return appwire.Errorf(appwire.CodeBusy, "%s is already being read; try again", method)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appwire.CheckoutSeconds*time.Second)
	defer cancel()
	return fn(ctx, appCanonical(workspace))
}

// appCheckoutLocate is checkout.locate for a workspace, under the caller's clock.
func appCheckoutLocate(ctx context.Context, workspace string, q appwire.CheckoutPathQuery) (appwire.CheckoutLocation, error) {
	row, err := appCheckoutRowFor(ctx, workspace, q)
	if err != nil {
		return appwire.CheckoutLocation{}, err
	}
	return appwire.CheckoutLocation{Entry: row.entry, Revision: row.snap.out.Revision, Repo: row.snap.top,
		WorkspacePath: row.workspacePath(), Open: row.open()}, nil
}

// appCheckoutDiff is checkout.diff for a workspace, under the caller's clock.
func appCheckoutDiff(ctx context.Context, workspace string, q appwire.CheckoutPathQuery) (appwire.CheckoutDiffResult, error) {
	row, err := appCheckoutRowFor(ctx, workspace, q)
	if err != nil {
		return appwire.CheckoutDiffResult{}, err
	}
	return appwire.CheckoutDiffResult{Entry: row.entry, Revision: row.snap.out.Revision, Repo: row.snap.top,
		WorkspacePath: row.workspacePath(), Diff: appCheckoutDiffRow(row), Open: row.open()}, nil
}

// CheckoutLocate answers checkout.locate.
func (b *appBackend) CheckoutLocate(q appwire.CheckoutPathQuery) (out appwire.CheckoutLocation, err error) {
	err = b.withCheckoutSlot(q.Agent, "checkout.locate", func(ctx context.Context, workspace string) (err error) {
		out, err = appCheckoutLocate(ctx, workspace, q)
		return err
	})
	return out, err
}

// CheckoutDiff answers checkout.diff.
func (b *appBackend) CheckoutDiff(q appwire.CheckoutPathQuery) (out appwire.CheckoutDiffResult, err error) {
	err = b.withCheckoutSlot(q.Agent, "checkout.diff", func(ctx context.Context, workspace string) (err error) {
		out, err = appCheckoutDiff(ctx, workspace, q)
		return err
	})
	return out, err
}

// appCheckoutDiffRow is the diff of one row against the snapshot's base, from
// fixed read-only git run at the repository root. It is diff-index and never
// diff: `git diff` rewrites the index's stat cache even with optional locks off.
func appCheckoutDiffRow(row *appCheckoutRow) (out appwire.FileDiff) {
	snap, entry := row.snap, row.entry
	top := snap.top
	out = appwire.FileDiff{Path: entry.Path, Resolved: filepath.Join(top, filepath.FromSlash(entry.Path)),
		Repo: top, Outside: row.workspacePath() == ""}
	// AN UNAVAILABLE DIFF CARRIES NO CONTENT OR COUNT FACT, however far the
	// comparison got: hunks, counts and the change class computed before a later
	// read failed were never confirmed, and showing them would be the unverified
	// diff this answer exists to refuse. The row facts (path, repo, old path,
	// base, commit) stay.
	scrub := func(d appwire.FileDiff) appwire.FileDiff {
		d.Text, d.Lines, d.Truncated, d.Binary, d.Add, d.Del, d.Change = "", 0, false, false, nil, nil, ""
		return d
	}
	unavailable := func(reason, detail string) appwire.FileDiff {
		out.State, out.Reason, out.Detail = appwire.DiffUnavailable, reason, detail
		return scrub(out)
	}
	if entry.Submodule || (entry.Kind != appwire.CheckoutChanged && entry.Kind != appwire.CheckoutRenamed && entry.Kind != appwire.CheckoutUntracked) {
		kind := entry.Kind
		if entry.Submodule {
			kind = "submodule"
		}
		return unavailable(appwire.DiffReasonUnsupportedRow, kind)
	}
	g := snap.g
	fail := func(f *appGitFail) appwire.FileDiff { return scrub(f.answer(out)) }

	// A file a configured filter governs is never diffed: its bytes are not what
	// the repository stores, and the filter is never run.
	paths := []string{entry.Path}
	if entry.Kind == appwire.CheckoutRenamed {
		if entry.OldPath == "" {
			return unavailable(appwire.DiffReasonUnsupportedRow, "renamed without its original path")
		}
		paths = []string{entry.OldPath, entry.Path}
	}
	if len(snap.named) > 0 {
		attrs, f := g.out(top, 4<<10, append([]string{"check-attr", "-z", "filter", "--"}, paths...)...)
		if f != nil {
			return fail(f)
		}
		parts := strings.Split(attrs, "\x00")
		for j := 0; j+2 < len(parts); j += 3 {
			if snap.named["filter."+parts[j+2]] {
				return unavailable(appwire.DiffReasonFilter, parts[j+2])
			}
		}
	}

	if entry.Kind == appwire.CheckoutUntracked {
		// THE READ DRAWS ON THE CALL'S POOL: at most what is left of it, and the
		// bytes actually read are charged, so the call's total stays the one
		// bound. A file that does not fit what is left is too_large, said to be
		// the budget, never a partial text called whole.
		limit := min(int64(appwire.FileReadMax), int64(g.left))
		data, reason := appReadContained(top, entry.Path, limit)
		g.left -= len(data)
		switch reason {
		case "too_large":
			out.State, out.Base = appwire.DiffUntracked, appwire.DiffBaseNone
			out.Reason = appwire.DiffReasonTooLarge
			if limit < appwire.FileReadMax {
				out.Detail = "the byte budget of this call is spent"
			}
			return out
		case "":
		default:
			return unavailable(appwire.DiffReasonUnreadable, "the file could not be opened safely")
		}
		return appCheckoutAdded(out, data)
	}

	base := snap.out.Head
	out.Base = appwire.DiffBaseHead
	if base == "" {
		if !snap.out.Unborn {
			return unavailable(appwire.DiffReasonGitFailed, "the status named no base")
		}
		empty, f := g.out(top, 256, "hash-object", "-t", "tree", "/dev/null")
		if f != nil {
			return fail(f)
		}
		base = strings.TrimSpace(empty)
		out.Base = appwire.DiffBaseNone
	}
	out.Commit = base

	inBase := ""
	for _, p := range paths {
		tree, f := g.out(top, 64<<10, "ls-tree", "-z", "-l", base, "--", p)
		if f != nil {
			return fail(f)
		}
		if p == paths[0] {
			inBase = tree
		}
		if appTreeTooLarge(tree) {
			return unavailable(appwire.DiffReasonTooLarge, "")
		}
	}

	before := appRowStamps(row, paths)
	args := append(append([]string{}, snap.blank...), "diff-index", "--no-color", "--no-ext-diff", "--no-textconv",
		"--ignore-submodules=dirty")
	if entry.Kind == appwire.CheckoutRenamed {
		args = append(args, "-M")
		out.OldPath = entry.OldPath
	} else {
		args = append(args, "--no-renames")
	}
	numstat, f := g.out(top, 64<<10, append(append(append([]string{}, args...), "--numstat", "-z", base, "--"), paths...)...)
	if f != nil {
		return fail(f)
	}
	add, del, binary, found := appNumstat(numstat)
	if found {
		out.State = appwire.DiffChanged
		switch {
		case entry.Kind == appwire.CheckoutRenamed:
			out.Change = "renamed"
		case inBase == "":
			out.Change = "added"
		case entry.Worktree == "deleted" || entry.Index == "deleted":
			// The ROW says deleted, whatever is on disk now: an ignored file whose
			// index entry was removed is still there and still a deletion.
			out.Change = "deleted"
		default:
			out.Change = "modified"
		}
		if binary {
			out.Binary = true
		} else {
			out.Add, out.Del = &add, &del
			appDiffMid()
			hunks, overflow, f := g.capped(top, appwire.FileDiffMax+16<<10, appwire.FileDiffLines+256,
				append(append(append([]string{}, args...), "-p", "--unified=3", base, "--"), paths...)...)
			if f != nil {
				return fail(f)
			}
			out.Text, out.Lines, out.Truncated = appHunks(hunks)
			out.Truncated = out.Truncated || overflow
			if !utf8.ValidString(out.Text) {
				out.Binary, out.Text, out.Lines, out.Truncated, out.Add, out.Del = true, "", 0, false, nil, nil
			}
		}
	} else {
		// The row is changed against the index or the work tree, and net of both
		// the work tree is the base's content: said as it is, not as an empty diff.
		out.State = appwire.DiffUnchanged
	}

	// The checkout must still be the one the row came from: the status text and
	// the stamps of the index and of the paths read, as the line counts check.
	appDiffBetween()
	again, over, f := snap.read()
	switch {
	case f != nil:
		return fail(f)
	case over:
		return unavailable(appwire.DiffReasonTooLarge, "the status could not be read again within the byte budget")
	case again != snap.text || appRowStamps(row, paths) != before:
		return unavailable(appwire.DiffReasonChanged, "the checkout changed during the comparison")
	}
	return out
}

// appDiffMid is a test's hook between the counts and the hunks.
var appDiffMid = func() {}

// appDiffBetween is a test's hook for the moment between the comparison and the
// status read that confirms it.
var appDiffBetween = func() {}

// appRowStamps is the index's stamp and each path's, as one comparable string.
func appRowStamps(row *appCheckoutRow, paths []string) string {
	var b strings.Builder
	b.WriteString(appStamp(filepath.Join(row.snap.gitDir, "index")))
	for _, p := range paths {
		b.WriteString("\x00" + appStamp(filepath.Join(row.snap.top, filepath.FromSlash(p))))
	}
	return b.String()
}

// appCheckoutAdded answers an untracked file as wholly added, from bytes already
// read safely. It is [appUntracked]'s text, built from memory.
func appCheckoutAdded(out appwire.FileDiff, data []byte) appwire.FileDiff {
	out.State, out.Base = appwire.DiffUntracked, appwire.DiffBaseNone
	for _, b := range data {
		if b == 0 {
			out.Binary = true
			return out
		}
	}
	if !utf8.Valid(data) {
		out.Binary = true
		return out
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	add, del := len(lines), 0
	out.Add, out.Del = &add, &del
	var text strings.Builder
	if add > 0 {
		text.WriteString("@@ -0,0 +1," + strconv.Itoa(add) + " @@\n")
	}
	for _, line := range lines {
		text.WriteString("+")
		text.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			text.WriteString("\n\\ No newline at end of file\n")
		}
	}
	out.Text, out.Lines, out.Truncated = appHunks(text.String())
	return out
}

var _ appwire.CheckoutDiffer = (*appBackend)(nil)
