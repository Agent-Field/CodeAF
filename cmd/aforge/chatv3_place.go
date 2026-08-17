package main

// chatv3_place.go is the door's half of Decision 26: the two things the launch
// assembly has to DO about a session folder, as opposed to the arithmetic
// internal/session's place.go already does about one.
//
// Both are here rather than in chatv3.go because both are decisions about the
// person's disk — where their deliverables are indexed, and what an owned
// workspace is when it is first made — and a launch function that also held
// them would be a function nobody could read the launch out of.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// artifactsIndexPath is the deliverables index for this machine:
// ~/.aforge/v3/artifacts.jsonl, resolved through internal/home so AFORGE_HOME
// moves it with everything else (Decision 26 — one home, one seam).
//
// It is one function rather than a literal at the two call sites, because the
// SESSION writes rows for the pictures it paints and the SURFACE writes rows
// for the conversations it exports, and two spellings of one path would be two
// indexes with half a person's work in each.
//
// The launch assembly sets every carrier from here, beside where it sets
// Config.Place: session.Config.ArtifactsIndex and tui3.Options.ArtifactsIndex
// (chatv3.go), and the --host surface's own (chatv3_host.go).
func artifactsIndexPath() string {
	return home.Join("v3", session.ArtifactsIndexName)
}

// prepareOwnedWorkspace makes an owned session's work/ directory and, silently,
// a git repository out of it.
//
// WHY A REPOSITORY NOBODY ASKED FOR. An owned session is one opened nowhere in
// particular — $HOME, a temp directory, a launcher — and until now that was the
// one kind of session that ran with none of the machinery a repository buys:
// task nodes got no worktrees, so they ran in place with no isolation, no
// auditor and no merge (Decision 19), and every document the agent touched was
// edited with no way back. The repository is what turns those on, and the
// person never has to know it is there: they did not choose a project, so there
// is nothing here that was theirs to be surprised by.
//
// THE INITIAL COMMIT IS THE POINT, not a formality. A repository with no HEAD
// has no commit to branch from, and `git worktree add` against it fails — so a
// session that had been init-ed but never committed would look isolated and
// behave exactly like one that was not. The commit is empty because there is
// nothing in the directory yet, which is precisely the state worth recording:
// "this is where the session started".
//
// FAILING IS NOT FATAL, and that is the whole of the error handling. git may be
// missing, a sandbox may refuse the write, a hook may be broken — and none of
// those is a reason to refuse somebody a conversation. What the session loses
// is isolation, which it never had in this mode anyway; what it must not do is
// PRETEND to have it, so the loss is one line on stderr and the caller carries
// on (Decision 26 — pretending to isolate is worse than not isolating).
//
// The one error it does return is the directory: a workspace that could not be
// created is not a workspace, and a session pointed at a path that is not there
// fails at its first tool call instead of at its launch.
func prepareOwnedWorkspace(place session.Place) error {
	work := strings.TrimSpace(place.Work())
	if work == "" {
		// Not an owned session, or not a session with a folder. Either way there
		// is no workspace of ours to make, and nothing here to say about it.
		return nil
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		return fmt.Errorf("could not make this session's workspace: %w", err)
	}
	if _, err := os.Stat(filepath.Join(work, ".git")); err == nil {
		// A resumed owned session already has its repository and its first
		// commit. Re-initializing would be harmless and re-committing would not:
		// an empty commit on top of the person's own work is a line in their
		// history that says nothing happened.
		return nil
	}
	if err := initOwnedRepository(work); err != nil {
		fmt.Fprintln(os.Stderr, "this session runs without undo history: "+err.Error())
	}
	return nil
}

// initOwnedRepository is the git half, and it is deliberately three commands
// that all have to work: an init, an empty commit, and the HEAD that proves it.
//
// The identity is passed PER COMMAND rather than configured, exactly as
// commitTaskWork does in internal/session: a machine with no git identity still
// commits, and the person's own config is never touched by a session opening.
func initOwnedRepository(work string) error {
	if _, err := gitIn(work, "init", "--quiet"); err != nil {
		return err
	}
	if _, err := gitIn(work,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit", "--allow-empty", "--no-verify", "--quiet", "-m", "session opened",
	); err != nil {
		return err
	}
	// The HEAD is READ BACK rather than assumed. Everything above can report
	// success on a machine where a template hook or a stale config left no
	// commit behind, and the whole reason for the commit is that a worktree can
	// branch from it — so the check is the same question the worktree will ask.
	if _, err := gitIn(work, "rev-parse", "--verify", "HEAD"); err != nil {
		return err
	}
	return nil
}

// gitIn runs one plumbing command in a directory. There is no context: every
// call here is local and answers immediately or not at all, and the environment
// is pinned for the reason internal/session's git helper pins it — a pager or
// an editor would hang a launch on a terminal it is about to hand to the
// surface.
func gitIn(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	out, err := command.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %s", strings.Join(args, " "), gitReason(string(out), err))
	}
	return string(out), nil
}

// gitReason is the sentence the log line carries: git's own first line when it
// said anything, and the process error when it said nothing — which is what a
// machine with no git at all looks like from here.
func gitReason(out string, err error) string {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}
