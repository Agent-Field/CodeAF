package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// CHECKOUT.STATUS IS ONE READ-ONLY `git status`, BOUNDED IN TIME, OUTPUT AND NUMBER.
//
// It runs through the same [appGit] files.diff uses, so it inherits that
// runner's laws: no optional locks, no fsmonitor, one deadline for every
// process, output counted as it arrives and the process killed past the bound.
// What `git status` adds is one thing files.diff never meets: to tell whether a
// file whose stat data changed really differs, git runs the file's clean
// filter, and a filter is a program named by the repository's configuration.
// So every configured filter driver is blanked on the command line first (the
// driver names are read with `git config --get-regexp`, which runs nothing),
// and submodules are compared by recorded commit only (--ignore-submodules=dirty)
// so a submodule's own configuration is never consulted. Nothing is written to
// the repository, the index included.

// appCheckoutOutMax is the most status text one read keeps; a test lowers it.
var appCheckoutOutMax = appwire.CheckoutOutMax

// appCheckoutTotalMax is the most git output one whole call reads, every command
// together; a test lowers it. It is smaller than two full status reads on
// purpose: a status too big to be read twice cannot have its counts confirmed.
var appCheckoutTotalMax = appwire.CheckoutTotalMax

// appStatusSlots holds one token per status being read.
var appStatusSlots = make(chan struct{}, appwire.CheckoutJobs)

// CheckoutStatus answers checkout.status for the agent's workspace.
func (b *appBackend) CheckoutStatus(q appwire.CheckoutQuery) (appwire.CheckoutStatus, error) {
	a, err := b.find(q.Agent)
	if err != nil {
		return appwire.CheckoutStatus{}, err
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	if strings.TrimSpace(workspace) == "" {
		return appwire.CheckoutStatus{Entries: []appwire.CheckoutEntry{}}, nil
	}
	select {
	case appStatusSlots <- struct{}{}:
		defer func() { <-appStatusSlots }()
	default:
		return appwire.CheckoutStatus{}, appwire.Errorf(appwire.CodeBusy, "checkout.status is already being read; try again")
	}
	ctx, cancel := context.WithTimeout(context.Background(), appwire.CheckoutSeconds*time.Second)
	defer cancel()
	return appCheckout(ctx, appCanonical(workspace), q)
}

// appSnapshot is one whole read of a checkout's status: where it is, the
// filter blanks every later git of the call carries, the parsed header and rows,
// and the revision that names exactly this text. checkout.status cuts a page
// from it; checkout.diff and checkout.locate find one row in it. The byte pool
// and the clock are the call's, so everything read after it spends from them.
type appSnapshot struct {
	g        *appMeter
	dir, top string
	gitDir   string
	blank    []string
	named    map[string]bool
	out      appwire.CheckoutStatus
	entries  []appwire.CheckoutEntry
	text     string
	overflow bool
	read     func() (string, bool, *appGitFail)
}

// appCheckoutSnapshot reads dir's status once. A folder that is not a checkout
// answers a nil snapshot and no error.
func appCheckoutSnapshot(ctx context.Context, dir string) (*appSnapshot, error) {
	g := &appMeter{g: appGit{ctx: ctx}, left: appCheckoutTotalMax}
	roots, fail := g.out(dir, 8<<10, "rev-parse", "--absolute-git-dir", "--show-toplevel")
	if fail != nil {
		if fail.notRepo() {
			return nil, nil
		}
		return nil, appCheckoutFailure(fail)
	}
	gitDir, top, _ := strings.Cut(strings.TrimSuffix(roots, "\n"), "\n")

	// The driver names are read from the person's configuration and only named
	// back to git as empty commands; nothing configured is run.
	blank, named := []string{}, map[string]bool{}
	if text, fail := g.out(dir, 64<<10, "config", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`); fail != nil {
		if fail.exit != 1 { // 1 is git's "no such key", the ordinary case
			return nil, appCheckoutFailure(fail)
		}
	} else {
		for _, key := range strings.Split(text, "\n") {
			if key = strings.TrimSpace(key); key != "" {
				// A name git's -c would split at its "=" cannot be blanked, and a
				// filter left standing is a program that could run: refuse.
				if strings.Contains(key, "=") {
					return nil, appwire.Errorf(appwire.CodeEngine, "a configured filter name cannot be set aside safely")
				}
				// EVERY DRIVER GETS ALL FOUR SETTINGS, whichever one the key named.
				// A driver marked required (git-lfs sets it) makes git fail the whole
				// status when its emptied command does nothing, so required goes
				// false beside the blanks.
				driver := key[:strings.LastIndexByte(key, '.')]
				if !named[driver] {
					named[driver] = true
					blank = append(blank, "-c", driver+".clean=", "-c", driver+".smudge=",
						"-c", driver+".process=", "-c", driver+".required=false")
				}
			}
		}
	}

	args := append(append([]string{}, blank...), "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--ignore-submodules=dirty")
	readStatus := func() (string, bool, *appGitFail) {
		text, overflow, fail := g.capped(dir, appCheckoutOutMax, 0, args...)
		if fail == nil && overflow {
			// What was kept ends mid-record; only whole records are read. The final
			// terminator goes too, so the parser never sees an empty record where a
			// rename's original path was cut away and calls it a rename from nothing.
			if cut := strings.LastIndexByte(text, 0); cut >= 0 {
				text = text[:cut]
			} else {
				text = ""
			}
		}
		return text, overflow, fail
	}
	text, overflow, fail := readStatus()
	if fail != nil {
		if fail.notRepo() {
			return nil, nil
		}
		return nil, appCheckoutFailure(fail)
	}
	out, entries := appParseStatus(text)
	out.Repo, out.Operation = true, appOperation(gitDir)
	sum := sha256.Sum256([]byte(text))
	out.Revision = hex.EncodeToString(sum[:16])
	if overflow {
		out.Incomplete = true
	} else {
		counts := appCountStatus(entries)
		out.Counts = &counts
	}
	return &appSnapshot{g: g, dir: dir, top: top, gitDir: gitDir, blank: blank, named: named,
		out: out, entries: entries, text: text, overflow: overflow, read: readStatus}, nil
}

// appCheckout reads dir's status and cuts the page the query asks for.
func appCheckout(ctx context.Context, dir string, q appwire.CheckoutQuery) (appwire.CheckoutStatus, error) {
	empty := appwire.CheckoutStatus{Entries: []appwire.CheckoutEntry{}}
	snap, err := appCheckoutSnapshot(ctx, dir)
	if err != nil {
		return empty, err
	}
	if snap == nil {
		return empty, nil
	}
	g, top, gitDir, blank, named := snap.g, snap.top, snap.gitDir, snap.blank, snap.named
	out, entries, text, overflow, readStatus := snap.out, snap.entries, snap.text, snap.overflow, snap.read

	start := 0
	if q.Cursor != "" {
		rev, offset, ok := strings.Cut(q.Cursor, ":")
		n, err := strconv.Atoi(offset)
		if !ok || err != nil || n < 0 || n > len(entries) || rev != out.Revision {
			return empty, appwire.Errorf(appwire.CodeBadParams, "that cursor is no longer valid; read the status again from the start")
		}
		start = n
	}
	end := min(start+q.Limit, len(entries))
	out.Entries = entries[start:end]
	// A status cut by the bound has no Next past the part read: its revision
	// cannot name what was never read, so a cursor into it would be a promise.
	if end < len(entries) {
		out.Next = out.Revision + ":" + strconv.Itoa(end)
	}
	if reason := appPageLines(g, dir, top, gitDir, blank, named, &out, overflow, readStatus, text); reason != "" {
		out.LinesUnavailable = reason
	}
	return out, nil
}

// appCheckoutFailure is a git failure as the wire says it: a clock that ran out
// is busy, and any other refusal is the engine's, with git's own first line.
func appCheckoutFailure(fail *appGitFail) error {
	switch {
	case fail.timedOut:
		return appwire.Errorf(appwire.CodeBusy, "git did not answer in time; try again")
	case fail.missing:
		return appwire.Errorf(appwire.CodeEngine, "there is no git program here")
	}
	first, _, _ := strings.Cut(strings.TrimSpace(fail.stderr), "\n")
	if first == "" {
		first = "git could not read the checkout"
	}
	return appwire.Errorf(appwire.CodeEngine, "%s", first)
}

// appParseStatus reads `git status --porcelain=v2 -z --branch` text. Header
// records fill the branch facts; every other record is one entry. A record it
// does not know is skipped, never guessed at.
func appParseStatus(text string) (appwire.CheckoutStatus, []appwire.CheckoutEntry) {
	out := appwire.CheckoutStatus{}
	entries := []appwire.CheckoutEntry{}
	records := strings.Split(text, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		switch {
		case rec == "":
		case strings.HasPrefix(rec, "# branch.oid "):
			if oid := strings.TrimPrefix(rec, "# branch.oid "); oid == "(initial)" {
				out.Unborn = true
			} else {
				out.Head = oid
			}
		case strings.HasPrefix(rec, "# branch.head "):
			if name := strings.TrimPrefix(rec, "# branch.head "); name == "(detached)" {
				out.Detached = true
			} else {
				out.Branch = name
			}
		case strings.HasPrefix(rec, "# branch.upstream "):
			out.Upstream = strings.TrimPrefix(rec, "# branch.upstream ")
		case strings.HasPrefix(rec, "# branch.ab "):
			var ahead, behind int
			if f := strings.Fields(strings.TrimPrefix(rec, "# branch.ab ")); len(f) == 2 {
				a, errA := strconv.Atoi(strings.TrimPrefix(f[0], "+"))
				d, errD := strconv.Atoi(strings.TrimPrefix(f[1], "-"))
				if errA == nil && errD == nil {
					ahead, behind = a, d
					out.Ahead, out.Behind = &ahead, &behind
				}
			}
		case strings.HasPrefix(rec, "# "):
		case rec[0] == '1':
			if f := strings.SplitN(rec, " ", 9); len(f) == 9 && len(f[1]) == 2 {
				entries = append(entries, appStatusEntry(appwire.CheckoutChanged, f[1], f[2], f[8], ""))
			}
		case rec[0] == '2':
			f := strings.SplitN(rec, " ", 10)
			if len(f) != 10 || len(f[1]) != 2 || i+1 >= len(records) {
				continue
			}
			i++ // the original path is the next record
			kind := appwire.CheckoutRenamed
			if strings.HasPrefix(f[8], "C") {
				kind = appwire.CheckoutCopied
			}
			entries = append(entries, appStatusEntry(kind, f[1], f[2], f[9], records[i]))
		case rec[0] == 'u':
			if f := strings.SplitN(rec, " ", 11); len(f) == 11 && len(f[1]) == 2 {
				entry := appStatusEntry(appwire.CheckoutConflicted, "..", f[2], f[10], "")
				entry.Conflict = appConflictWords[f[1]]
				entries = append(entries, entry)
			}
		case rec[0] == '?' && len(rec) > 2:
			entries = append(entries, appStatusEntry(appwire.CheckoutUntracked, "..", "N...", rec[2:], ""))
		}
	}
	return out, entries
}

var appStatusWords = map[byte]string{
	'M': "modified", 'A': "added", 'D': "deleted", 'T': "typechange", 'R': "renamed", 'C': "copied",
}

var appConflictWords = map[string]string{
	"DD": "both_deleted", "AU": "added_by_us", "UD": "deleted_by_them", "UA": "added_by_them",
	"DU": "deleted_by_us", "AA": "both_added", "UU": "both_modified",
}

// appStatusEntry is one record's entry. A path that is not valid UTF-8 carries
// no path at all.
func appStatusEntry(kind, xy, sub, path, old string) appwire.CheckoutEntry {
	entry := appwire.CheckoutEntry{
		Kind: kind, Index: appStatusWords[xy[0]], Worktree: appStatusWords[xy[1]],
		Submodule: strings.HasPrefix(sub, "S"),
	}
	if utf8.ValidString(path) && utf8.ValidString(old) {
		entry.Path, entry.OldPath = path, old
	} else {
		entry.PathUnreadable = true
	}
	return entry
}

// appCountStatus counts a whole, complete status.
func appCountStatus(entries []appwire.CheckoutEntry) appwire.CheckoutCounts {
	var c appwire.CheckoutCounts
	for _, e := range entries {
		switch e.Kind {
		case appwire.CheckoutUntracked:
			c.Untracked++
		case appwire.CheckoutConflicted:
			c.Conflicted++
		default:
			if e.Index != "" {
				c.Staged++
			}
			if e.Worktree != "" {
				c.Unstaged++
			}
		}
	}
	return c
}

// appOperation is the operation git left marks of in its own directory, found
// by stat alone. The order is the order git itself would be most confusing in:
// a rebase hides a cherry-pick it is in the middle of.
func appOperation(gitDir string) string {
	for _, probe := range []struct{ name, op string }{
		{"rebase-merge", appwire.CheckoutOpRebase},
		{"rebase-apply", appwire.CheckoutOpRebase},
		{"MERGE_HEAD", appwire.CheckoutOpMerge},
		{"CHERRY_PICK_HEAD", appwire.CheckoutOpCherryPick},
		{"REVERT_HEAD", appwire.CheckoutOpRevert},
		{"BISECT_LOG", appwire.CheckoutOpBisect},
	} {
		if _, err := os.Lstat(filepath.Join(gitDir, probe.name)); err == nil {
			return probe.op
		}
	}
	return ""
}

var _ appwire.CheckoutReader = (*appBackend)(nil)
