package cellstore

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// ── WHAT A SCREEN SAYS, AND HOW MUCH OF IT ──────────────────────────────────
//
// The guard used to say one sentence for every path a screen newly kept out of
// the saved history. A factory job works in a fresh checkout, and the first seal
// of a repository whose tests carry fake `sk-…` keys said a dozen of them in a
// row, one per fixture, to a person who had only asked the job's manager to add
// a step. Two rules replace that:
//
//	NO NEWS IS NOT SAID   a file git tracks in the checkout, exactly as HEAD
//	                      has it, is already in the repository's history; the
//	                      sentence about it tells the person nothing they did.
//	                      THE FILE IS STILL KEPT OUT, exactly as before: this
//	                      rule drops the announcement and never the withholding.
//	ONE SCREEN, ONE NOTICE whatever is still worth saying is said once: a lead
//	                      that counts the files, then the files, each with the
//	                      rule that kept it out. A single file keeps the one
//	                      sentence it always had.
//
// A path this machine cannot read is never "no news": its permissions are
// something the person can fix, and the history git holds of it says nothing
// about whether this machine may read it. So it is grouped, never dropped.

// Notice is one thing the guard says to a person after a screen.
type Notice struct {
	// Text is the whole notice in words, for every reader that draws notices as
	// prose (the terminal, a factory item's log, stderr): the one sentence about
	// one file, or [Notice.Lead] and then one line per file.
	Text string
	// Lead is Text without its list of files, set only on a notice that names
	// more than one. A surface that draws the list itself shows this instead.
	Lead string
	// Files is every file a grouped notice names, with the rule that kept it
	// out, in the order the screen found them. It is nil on a notice about one
	// file or about none (a vault that could not be written). A surface reads the
	// files from here and never back out of Text.
	Files []keys.Finding
}

// noticeLinesSaid bounds how many files Text lists. A first seal of a large
// checkout could name hundreds, and a prose reader has no way to fold them; the
// rest are counted, and [Notice.Files] still carries every one of them.
const noticeLinesSaid = 20

// historyReadTimeout bounds the one git read a screen makes to learn which of
// its fresh findings git already holds, like every other git read in this
// package: an answer that does not come in time is no answer, and every file is
// announced as it was before the rule existed.
const historyReadTimeout = 5 * time.Second

// sealNotice is the one notice a screen owes for what it newly kept out:
// withheld is the files newly kept out because they hold a secret, unreadable
// the paths newly left out because this machine cannot read them. Nothing new
// is the zero Notice, which says nothing.
func sealNotice(withheld, unreadable []keys.Finding) Notice {
	switch {
	case len(withheld)+len(unreadable) == 0:
		return Notice{}
	case len(withheld) == 1 && len(unreadable) == 0:
		return Notice{Text: noticeFor(withheld[0])}
	case len(withheld) == 0 && len(unreadable) == 1:
		return Notice{Text: unreadableNotice(unreadable[0].Path)}
	}
	files := append(append([]keys.Finding(nil), withheld...), unreadable...)
	lead := groupLead(withheld, unreadable)
	lines := []string{lead}
	for i, f := range files {
		if i == noticeLinesSaid {
			lines = append(lines, "· and "+strconv.Itoa(len(files)-i)+" more")
			break
		}
		lines = append(lines, "· "+f.Path+" ("+f.Rule+")")
	}
	return Notice{Text: strings.Join(lines, "\n"), Lead: lead, Files: files}
}

// noticeFor is the sentence about one file newly withheld. It names the file
// and the rule, never the value.
func noticeFor(f keys.Finding) string {
	if f.Rule == keys.RuleDotenv {
		return f.Path + " is kept out of the saved history; its variables are kept locally for your tools, and the file travels through the key vault only to paired computers"
	}
	return f.Path + " looks like it holds a secret (" + f.Rule + "), so it is kept out of the saved history; the file travels through the key vault only to paired computers"
}

// unreadableNotice is the sentence about one path newly left out because this
// machine cannot read it.
func unreadableNotice(path string) string {
	return path + " cannot be read here, so it is left out of the saved history until its permissions allow it"
}

// groupLead counts what a grouped notice names and says what happens to it,
// with the SAME MEANINGS as the one-file sentences: a secret file travels
// through the key vault only to paired computers, an environment file's
// variables are also kept locally for the person's tools, and an unreadable path
// waits for its permissions.
func groupLead(withheld, unreadable []keys.Finding) string {
	var said []string
	if n := len(withheld); n > 0 {
		dotenv := 0
		for _, f := range withheld {
			if f.Rule == keys.RuleDotenv {
				dotenv++
			}
		}
		said = append(said, withheldLead(n, dotenv))
	}
	if n := len(unreadable); n > 0 {
		if n == 1 {
			said = append(said, "1 file cannot be read here, so it is left out of the saved history until its permissions allow it")
		} else {
			said = append(said, strconv.Itoa(n)+" files cannot be read here, so they are left out of the saved history until their permissions allow it")
		}
	}
	return strings.Join(said, ". ")
}

// withheldLead is the clause for n files withheld, dotenv of them environment
// files.
func withheldLead(n, dotenv int) string {
	count := strconv.Itoa(n)
	switch {
	case dotenv == n && n == 1:
		return "1 environment file is kept out of the saved history; its variables are kept locally for your tools, and the file travels through the key vault only to paired computers"
	case dotenv == n:
		return count + " environment files are kept out of the saved history; their variables are kept locally for your tools, and the files travel through the key vault only to paired computers"
	case n == 1:
		return "1 file looks like it holds a secret, so it is kept out of the saved history; it travels through the key vault only to paired computers"
	}
	lead := count + " files look like they hold secrets, so they are kept out of the saved history; they travel through the key vault only to paired computers"
	switch dotenv {
	case 0:
		return lead
	case 1:
		return lead + ", and the variables of the environment file among them are kept locally for your tools"
	}
	return lead + ", and the variables of the " + strconv.Itoa(dotenv) + " environment files among them are kept locally for your tools"
}

// newlyFound is found less every path before already holds, each path once, in
// the order found.
func newlyFound(found []keys.Finding, already func(string) bool) []keys.Finding {
	seen := map[string]bool{}
	var out []keys.Finding
	for _, f := range found {
		if !seen[f.Path] && !already(f.Path) {
			seen[f.Path] = true
			out = append(out, f)
		}
	}
	return out
}

// withoutHistory is found less every file git already holds in tree exactly as
// it is there ([inHistoryUnchanged]).
func withoutHistory(tree string, found []keys.Finding) []keys.Finding {
	if len(found) == 0 {
		return found
	}
	known := inHistoryUnchanged(tree, pathsOf(found))
	if len(known) == 0 {
		return found
	}
	var out []keys.Finding
	for _, f := range found {
		if !known[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

// inHistoryUnchanged answers which of paths git holds at HEAD and finds unchanged
// in tree, in the index and on disk. It is ONE BOUNDED READ PER SCREEN, never
// one per file: two git commands under one deadline, each handed every path at
// once, and it is made only when a screen has something new to say.
//
// IT ANSWERS ONLY WHAT GIT CONFIRMS. A file counts as known when HEAD's tree
// names it (a positive answer, so a nested repository, an untracked file or an
// ignored one is never mistaken for history) and the diff against HEAD does not
// (so an edited file, or one added to the index but never committed, is news).
// A tree that is not a git checkout, a repository with no commit yet, a git that
// fails or does not answer in time: every one of them answers nothing, and the
// files are announced exactly as before.
//
// Paths are literal (`--literal-pathspecs`), so a file named with a glob's
// characters matches only itself, and both commands report them relative to
// tree, which may sit below the repository's root. Neither takes a lock: a read
// beside a tool's own git command must never fail it.
func inHistoryUnchanged(tree string, paths []string) map[string]bool {
	if len(paths) == 0 || !hasGitMarker(tree) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), historyReadTimeout)
	defer cancel()
	spec := paths
	if len(spec) > maxChangedArgs {
		spec = []string{"."}
	}
	held, ok := gitNames(ctx, tree, append([]string{"ls-tree", "-r", "-z", "--name-only", "HEAD", "--"}, spec...))
	if !ok || len(held) == 0 {
		return nil
	}
	changed, ok := gitNames(ctx, tree, append([]string{"diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--name-only", "-z", "--relative", "HEAD", "--"}, spec...))
	if !ok {
		return nil
	}
	known := map[string]bool{}
	for _, path := range paths {
		if held[path] && !changed[path] {
			known[path] = true
		}
	}
	return known
}

// gitNames runs one read-only git command in tree and answers the NUL-separated
// names it printed; false when git failed, which the caller reads as no answer.
func gitNames(ctx context.Context, tree string, args []string) (map[string]bool, bool) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "--literal-pathspecs"}, args...)...) //codeaf:plumbing A notice skips files git already holds unchanged.
	cmd.Dir = tree
	raw, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	names := map[string]bool{}
	for _, name := range strings.Split(string(raw), "\x00") {
		if name != "" {
			names[name] = true
		}
	}
	return names, true
}
