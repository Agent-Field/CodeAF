package session

// THE CHECKS AS THEY STOOD BEFORE ONE TASK TOUCHED ITS TREE.
//
// A task's checker reads what would ship, but an absolute red on that tree does
// not say who made it red. This file supplies the missing other half: one
// bounded reading of the commit the task was cut from, shared by every task cut
// from that same commit, and a second reading of the landing only when the first
// one found something that needs subtracting.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// checkPhotograph is one reading of a set of check commands on one tree. Red
// holds only commands that ran to an answer and answered red; unread holds the
// commands no answer can safely be drawn from.
type checkPhotograph struct {
	red      []string
	unread   []string
	failures map[string][]string
	read     bool
}

// checkGround is the pair a node's checking is judged against: what the checks
// said on the base its tree was cut from, and what they say on what would ship.
type checkGround struct {
	before checkPhotograph
	after  checkPhotograph
}

// attributable is the red the before-reading was able to assign. A command
// nobody could read on the base is evidence in neither direction, even when a
// later shell happened to get an answer from it.
func (g checkGround) attributable(after checkPhotograph) []string {
	return verify.Subtract(after.red, g.before.unread)
}

// turnedRed names only checks that were green before this work and red after
// it. The arithmetic is the session's own, so its terminal reading and a task's
// checker cannot disagree about the same pair of answers.
func (g checkGround) turnedRed() []string {
	if !g.before.read {
		return nil
	}
	old := make(map[string]bool, len(g.before.red))
	for _, command := range g.before.red {
		old[command] = true
	}
	var turned []string
	for _, command := range g.attributable(g.after) {
		_, changed := verify.NewCommandFailure(old[command], g.before.failures[command], g.after.failures[command])
		if changed {
			turned = append(turned, command)
		}
	}
	return turned
}

// alreadyRed names only checks that were red on both sides. It is the other
// half of [checkGround.turnedRed], expressed through the same subtraction the
// session uses rather than through another copy of its rule.
func (g checkGround) alreadyRed() []string {
	if !g.before.read {
		return nil
	}
	return verify.Subtract(g.attributable(g.after), g.turnedRed())
}

// readChecksOn photographs one tree with one window, and makes the same three
// discards [Agent.readBaseline] makes. A command that did not answer, changed
// the tree while answering, or lay beyond the closing window was not read; it
// never borrows green or red from the absence of an answer.
func readChecksOn(ctx context.Context, dir string, checks []string) checkPhotograph {
	ctx, done := context.WithTimeout(ctx, auditDeadline)
	defer done()

	photograph := checkPhotograph{read: true, failures: make(map[string][]string)}
	for index, check := range checks {
		if ctx.Err() != nil {
			photograph.unread = append(photograph.unread, checks[index:]...)
			break
		}
		before := treeStateNow(dir)
		run := runOneCheck(ctx, dir, check)
		if treeStateNow(dir) != before || !run.Ran {
			photograph.unread = append(photograph.unread, check)
			continue
		}
		if !run.Passed {
			photograph.red = append(photograph.red, check)
			if len(run.Failures) > 0 {
				photograph.failures[check] = append([]string(nil), run.Failures...)
			}
		}
	}
	return photograph
}

// baseCheckAnswer is the one fact the shared store keeps for one command on
// one commit. Green is the zero value because a completed reading that named
// neither red nor unread is exactly a green answer.
type baseCheckAnswer uint8

const (
	baseCheckGreen baseCheckAnswer = iota
	baseCheckRed
	baseCheckUnread
)

// baseCheckReading lets concurrent siblings share an in-flight reading as well
// as a finished one. Closing ready publishes both fields; read false means the
// checkout itself could not be made and no conclusion may be cached from it.
type baseCheckReading struct {
	ready    chan struct{}
	answer   baseCheckAnswer
	failures []string
	read     bool
}

// baseCheckClaim is one cache entry this caller is responsible for filling.
// Keeping the pointer beside the command lets a failed checkout wake precisely
// its waiters before the key is made available for a later attempt.
type baseCheckClaim struct {
	command string
	entry   *baseCheckReading
}

// Completed entries deliberately live for the process rather than expiring
// underneath a later sibling. There is one entry per commit and check command,
// the commands are bounded by [auditCheckCount] per source, and so the store is
// bounded by the commits this process actually checks work against.
var sharedBaseChecks = struct {
	sync.Mutex
	byKey map[string]*baseCheckReading
}{byKey: make(map[string]*baseCheckReading)}

// checkBase names the repository and ref whose commit is the untouched side of
// a task's landing. lockRoot is kept separately because a branch in a fork is
// checked out from that fork while still sharing the ground repository's lock.
type checkBase struct {
	repository string
	lockRoot   string
	ref        string
	sha        string
}

// checkBaseFor resolves the immutable world captured when the task was cut.
// A WORKER MAY COMMIT, so neither its branch tip nor the current ground HEAD
// is a before-reading. Older records without a captured commit supply no
// baseline instead of attributing the worker's own failures to earlier work.
func checkBaseFor(tree taskTree) (checkBase, bool) {
	base := checkBase{repository: tree.branchHolder(), lockRoot: tree.root}
	base.ref = strings.TrimSpace(tree.checkBase)
	if base.ref == "" {
		base.ref = strings.TrimSpace(tree.base)
	}
	if base.ref == "" {
		base.ref = strings.TrimSpace(tree.seal)
	}
	if base.ref == "" || base.repository == "" {
		return checkBase{}, false
	}
	if base.lockRoot == "" {
		base.lockRoot = base.repository
	}
	out, err := git(base.repository, "rev-parse", "--verify", base.ref+"^{commit}")
	if err != nil || strings.TrimSpace(out) == "" {
		return checkBase{}, false
	}
	base.sha = strings.TrimSpace(out)
	return base, true
}

// claimBaseChecks returns every entry in the door's order and claims only the
// absent ones for this caller. The commit and command together are the identity:
// two worktrees cut from one commit are the same tree for this question.
func claimBaseChecks(sha string, checks []string) ([]*baseCheckReading, []baseCheckClaim) {
	sharedBaseChecks.Lock()
	defer sharedBaseChecks.Unlock()

	entries := make([]*baseCheckReading, 0, len(checks))
	var claims []baseCheckClaim
	for _, command := range checks {
		key := sha + "\x00" + command
		entry := sharedBaseChecks.byKey[key]
		if entry == nil {
			entry = &baseCheckReading{ready: make(chan struct{})}
			sharedBaseChecks.byKey[key] = entry
			claims = append(claims, baseCheckClaim{command: command, entry: entry})
		}
		entries = append(entries, entry)
	}
	return entries, claims
}

// publishBaseChecks completes this caller's claims. A checkout failure is not
// remembered as a reading: its waiters are released into silence, and a later
// task may try the cheap checkout again instead of inheriting a transient miss.
func publishBaseChecks(sha string, claims []baseCheckClaim, photograph checkPhotograph) {
	sharedBaseChecks.Lock()
	defer sharedBaseChecks.Unlock()

	for _, claim := range claims {
		claim.entry.read = photograph.read
		claim.entry.answer = answerForCheck(claim.command, photograph)
		claim.entry.failures = append([]string(nil), photograph.failures[claim.command]...)
		if !photograph.read {
			key := sha + "\x00" + claim.command
			if sharedBaseChecks.byKey[key] == claim.entry {
				delete(sharedBaseChecks.byKey, key)
			}
		}
		close(claim.entry.ready)
	}
}

// answerForCheck folds one set reading down to the per-command fact the cache
// keeps. Door commands are unique, so membership is the complete answer.
func answerForCheck(command string, photograph checkPhotograph) baseCheckAnswer {
	for _, unread := range photograph.unread {
		if unread == command {
			return baseCheckUnread
		}
	}
	for _, red := range photograph.red {
		if red == command {
			return baseCheckRed
		}
	}
	return baseCheckGreen
}

// awaitBaseChecks assembles the shared per-command answers back into one
// photograph in the door's order. A window that closes while a sibling is
// reading leaves the commands it did not reach unread for this node.
func awaitBaseChecks(ctx context.Context, checks []string, entries []*baseCheckReading) checkPhotograph {
	photograph := checkPhotograph{read: true, failures: make(map[string][]string)}
	for index, entry := range entries {
		select {
		case <-entry.ready:
		case <-ctx.Done():
			photograph.unread = append(photograph.unread, checks[index:]...)
			return photograph
		}
		if !entry.read {
			return checkPhotograph{}
		}
		switch entry.answer {
		case baseCheckRed:
			photograph.red = append(photograph.red, checks[index])
			if len(entry.failures) > 0 {
				photograph.failures[checks[index]] = append([]string(nil), entry.failures...)
			}
		case baseCheckUnread:
			photograph.unread = append(photograph.unread, checks[index])
		}
	}
	return photograph
}

// baseChecksFor is the before-reading: the door's checks run on the commit the
// node's tree was cut from, once, and every sibling cut from it shares the
// result. Only commands absent from the store need a checkout or a process.
func (a *Agent) baseChecksFor(ctx context.Context, tree taskTree, checks []string) checkPhotograph {
	base, ok := checkBaseFor(tree)
	if !ok {
		return checkPhotograph{}
	}
	entries, claims := claimBaseChecks(base.sha, checks)
	if len(claims) > 0 {
		// A READING THAT DIES IS UNREAD, NEVER GREEN. Publishing is deferred so
		// one panicking check cannot leave every sibling waiting forever on the
		// claims this call opened.
		photograph := checkPhotograph{read: true, unread: make([]string, len(claims))}
		for index, claim := range claims {
			photograph.unread[index] = claim.command
		}
		func() {
			defer func() { publishBaseChecks(base.sha, claims, photograph) }()
			defer guard.Recover("task base checks")
			photograph = readClaimedBaseChecks(ctx, tree.place, base, claims)
		}()
	}
	return awaitBaseChecks(ctx, checks, entries)
}

// readClaimedBaseChecks makes the one detached checkout needed for every cache
// miss in this call. The work's own checkout is never used: it can already hold
// the change whose responsibility is being decided.
func readClaimedBaseChecks(ctx context.Context, place Place, base checkBase, claims []baseCheckClaim) checkPhotograph {
	commands := make([]string, 0, len(claims))
	for _, claim := range claims {
		commands = append(commands, claim.command)
	}
	if ctx.Err() != nil {
		// A CLOSED WINDOW HAS READ NONE OF THESE COMMANDS. Answer that directly
		// rather than hand the person's own repository to a command runner whose
		// separate context check happens to keep it safe today.
		return checkPhotograph{read: true, unread: commands}
	}
	holder, err := os.MkdirTemp("", "aforge-check-base-")
	if err != nil {
		return checkPhotograph{}
	}
	dir := filepath.Join(holder, "base")
	drop, problem := detachedWorktree(place, base.lockRoot, base.repository, dir, base.sha)
	if problem != "" {
		_ = os.RemoveAll(holder)
		return checkPhotograph{}
	}
	defer func() {
		drop()
		_ = os.RemoveAll(holder)
	}()
	return readChecksOn(ctx, dir, commands)
}

// detachedWorktree is the one road every temporary checkout takes. The caller
// chooses its directory and ref; the add, forced removal, prune and repository
// lock stay one operation so the clean restore and the before-reading cannot
// drift into different git machinery.
func detachedWorktree(place Place, lockRoot, repository, dir, ref string) (func(), string) {
	remove := func() {
		unlock := lockGitRoot(place, lockRoot)
		_, _ = git(repository, "worktree", "remove", "--force", dir)
		_, _ = git(repository, "worktree", "prune")
		unlock()
		_ = os.RemoveAll(dir)
	}
	// THE PRE-CLEAN CAN ONLY REMOVE AN EARLIER COPY OF THIS SAME RESTORE.
	// [restoreFromBranch] names the node's own `-check` path; [restoreFromGround]
	// and [readClaimedBaseChecks] name children of temporary directories they
	// have just made, so nothing belonging to another caller can be at any of
	// the three paths.
	remove()
	unlock := lockGitRoot(place, lockRoot)
	out, err := git(repository, "worktree", "add", "--detach", dir, ref)
	unlock()
	if err != nil {
		_ = os.RemoveAll(dir)
		problem := firstLine(out)
		if problem == "" {
			problem = err.Error()
		}
		return func() {}, problem
	}
	return remove, ""
}

// checkGroundFor reads what the door's checks said before this work and, only
// when something was already red, what they say on the clean tree that would
// ship. Both readings share one window; what that window misses stays unread.
func (a *Agent) checkGroundFor(ctx context.Context, tree taskTree, ground auditGround, door auditDoor, log io.Writer) checkGround {
	if len(door.checks) == 0 || !ground.restored {
		return checkGround{}
	}
	ctx, done := context.WithTimeout(ctx, auditDeadline)
	defer done()

	checks := checkGround{before: a.baseChecksFor(ctx, tree, door.checks)}
	if len(checks.before.red) > 0 {
		checks.after = readChecksOn(ctx, ground.dir, door.checks)
	}
	fmt.Fprintf(log, "check: %s\n", checkGroundLog(checks))
	return checks
}

// checkGroundLog is one compact account for the job record. It names absence
// rather than turning an untaken reading into a clean one, and it stays out of
// the report a person reads.
func checkGroundLog(checks checkGround) string {
	if !checks.before.read {
		return "the checks' base could not be read"
	}
	var parts []string
	if len(checks.before.red) > 0 {
		parts = append(parts, "base red: "+strings.Join(checks.before.red, ", "))
	}
	if len(checks.before.unread) > 0 {
		parts = append(parts, "base unread: "+strings.Join(checks.before.unread, ", "))
	}
	if len(parts) == 0 {
		parts = append(parts, "every base check passed")
	}
	if checks.after.read {
		if len(checks.after.red) > 0 {
			parts = append(parts, "landing red: "+strings.Join(checks.after.red, ", "))
		}
		if len(checks.after.unread) > 0 {
			parts = append(parts, "landing unread: "+strings.Join(checks.after.unread, ", "))
		}
		if len(checks.after.red) == 0 && len(checks.after.unread) == 0 {
			parts = append(parts, "every landing check passed")
		}
	}
	return strings.Join(parts, " · ")
}

// checkGroundBlock is the before-and-after fact handed to the checker. It says
// nothing when nobody got a complete enough before-reading to assign a red;
// an unread command is never smuggled into either side by a general sentence.
func checkGroundBlock(checks checkGround) string {
	if !checks.before.read {
		return ""
	}
	already, turned := checks.alreadyRed(), checks.turnedRed()
	if len(already) == 0 && len(turned) == 0 {
		if len(checks.before.red) == 0 && len(checks.before.unread) == 0 {
			return "\nWHAT THE CHECKS SAID BEFORE THIS WORK:\nEvery check was passing before this work began, " +
				"so any red you find is this work's.\n"
		}
		return ""
	}
	var out strings.Builder
	out.WriteString("\nWHAT THE CHECKS SAID BEFORE THIS WORK:\n")
	for _, command := range already {
		fmt.Fprintf(&out, "- `%s` was red before this work and remains red. This comparison found no newly named failure; it is not proof that the requested behavior works.\n", command)
	}
	if len(already) > 0 {
		out.WriteString("Judge the requested acceptance from the work and its evidence; unparsed red output remains uncertain.\n")
	}
	for _, command := range turned {
		fmt.Fprintf(&out, "- `%s`: this work turned it red. That is a finding.\n", command)
	}
	return out.String()
}
