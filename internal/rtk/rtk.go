// Package rtk puts a compressor between a leaf's shell and the work model's
// context. rtk (github.com/rtk-ai/rtk, Apache-2.0) is a proxy binary that runs
// an inspection command and prints a far shorter version of what it said: a
// `ps aux` that cost 254KB of context costs 3.5KB through it, a `git diff` 41%
// less, a bare `ls -la` a fifth. Every one of those bytes is paid for again on
// every later turn of the leaf, so the saving compounds the way spilling does.
//
// Two properties are load-bearing, and everything in this package serves them.
//
// aforge stays the only thing anyone installs. rtk is never a build or install
// dependency: it is resolved at runtime, and fetched in the background if it
// is missing. While it is unavailable — offline, unsupported platform,
// download still in flight — commands run exactly as they did before. Nothing
// here may block or fail a leaf's work.
//
// A leaf's shell never changes meaning because rtk was in the middle. rtk's own
// rewriter decides what it can compress, which is the honest source of truth
// and spares us a table of command syntax; but it is broader than we want — it
// will turn `git add .` into `rtk git add .` — so the last word on eligibility
// is here, and a wrapped run that goes wrong is answered by the plain one.
//
// The head's belt deliberately does not use any of this. The head reads
// deliverables in order to answer the user in their own words; a compressed
// read would cost answer fidelity to save tokens on the one surface where the
// bytes are the point.
package rtk

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// Version is the pinned rtk release. Pinned rather than tracking latest so
	// that what a leaf sees does not change under it on someone else's release
	// schedule. To bump: read the notes at
	// https://github.com/rtk-ai/rtk/releases, set the tag here, and re-run the
	// integration test with AFORGE_TEST_RTK=1 — the one test that checks a real
	// binary still preserves exit codes.
	Version = "v0.45.0"

	// EnvBinary is the escape hatch: a path to a chosen rtk, or "off" to run
	// every command plain. It is read on every call so a running resident can
	// be taken out of the loop without a restart.
	EnvBinary = "AFORGE_RTK"

	// Off is the EnvBinary value that disables wrapping everywhere.
	Off = "off"

	// rewriteTimeout bounds the question we ask rtk before every eligible
	// command. rtk answers in single-digit milliseconds; anything near this
	// bound means something is wrong with the binary, and the answer to that is
	// to run the command plain.
	rewriteTimeout = 5 * time.Second
)

// Class is how much of a wrapped command's failure we are willing to believe.
//
// A read is cheap to repeat and its failures are the ones a model most needs
// verbatim — "no such file" has to be the shell's sentence, not a proxy's — so
// any nonzero exit is answered by running it again plain. A check earns the
// opposite treatment: a failing test suite is reporting, not malfunctioning,
// and its compressed failure is the single most valuable thing rtk produces.
// Running it twice would cost the leaf the whole suite a second time, so a
// check's exit code is taken at face value unless rtk itself came apart.
type Class int

const (
	ClassNone Class = iota
	ClassRead
	ClassCheck
)

// inspect is the eligibility table. It is a table because the property it
// encodes is not one rtk reports: running this twice changes nothing outside
// the process. rtk rewrites `git add .`, `git pull` and `pip install` just as
// readily as `ls`, and those must never be wrapped by something willing to run
// them again.
//
// The rule it follows is rtk's own purpose-built filters, minus the ones that
// write. That second qualifier matters more than it looks: rtk falls through to
// the raw command for subcommands it has no filter for and then truncates the
// result at a flat character count — a saving of 98% on `ps aux`, and an
// arbitrary cut through output nobody designed for it. A named filter knows
// what it is shortening; a fallthrough does not, so only the named ones are
// here.
//
// It is keyed on the rtk subcommand the rewrite produced rather than on raw
// command syntax, which is a much smaller and steadier surface, and the reason
// this stays a dozen lines instead of a parser. Anything absent runs plain.
var inspect = map[string]Class{
	"ls":   ClassRead,
	"tree": ClassRead,
	"read": ClassRead,
	"find": ClassRead,
	"grep": ClassRead,
	"rg":   ClassRead,
	"wc":   ClassRead,

	// Verb-gated below: rtk does not distinguish reading a repository from
	// changing one, and we must.
	"git": ClassRead,

	"go":            ClassCheck,
	"cargo":         ClassCheck,
	"pytest":        ClassCheck,
	"jest":          ClassCheck,
	"vitest":        ClassCheck,
	"golangci-lint": ClassCheck,
	"tsc":           ClassCheck,
	"ruff":          ClassCheck,
	"mypy":          ClassCheck,
}

// readOnlyGit is the whole of git that only looks. Everything else — add,
// commit, checkout, pull, stash, branch -d — is left alone, because the read
// class re-runs what fails and a second `git pull` is not a retry.
var readOnlyGit = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"blame": true, "shortlog": true,
}

// verbGated names the rtk subcommands whose first argument decides eligibility.
// rtk already refuses to rewrite `go get` and `go mod tidy`, so `go` is not
// here; git is, because rtk rewrites all of it.
var verbGated = map[string]map[string]bool{"git": readOnlyGit}

// machineFormat are the flags that say out loud that something is going to
// parse the output. rtk passes some of them through untouched and reshapes
// others — `git diff --name-only` comes back changed, `git log --pretty` comes
// back with its subjects truncated — and telling those two apart is not
// something we can do from outside. A command that asks for a machine format
// is asking for bytes, so it gets bytes.
var machineFormat = []string{
	"--porcelain", "--name-only", "--name-status", "--numstat",
	"--raw", "--format", "--pretty", "--json", "-z",
}

// nudgePrefix is rtk's own meta-channel. It writes at most one line per day to
// stderr suggesting the user install its editor hook — advice aimed at a human
// at a terminal, which a leaf's context is not. sh merges stderr into the
// result, so the line is dropped on the way past.
const nudgePrefix = "[rtk] "

// failedPrefix is what rtk prints when it could not run the command at all,
// alongside exit 127. It is the signal that the wrapping, not the command,
// is what went wrong.
const failedPrefix = "[rtk: "

// Tool is a resolved rtk binary and the memory of what it has been asked.
type Tool struct {
	Path string

	mutex     sync.Mutex
	decisions map[string]decision
}

type decision struct {
	command string
	class   Class
	// banned records a rewrite that failed on its own terms. The same shape is
	// never wrapped again, so one broken rewrite costs one fallback rather than
	// one per call for the life of the process.
	banned bool
}

// tools holds one instance per resolved path. What is worth keeping across
// leaves is the rewrite memory, which is a fact about a binary rather than
// about a workspace; where the binary is gets asked again every time, so a
// background bootstrap becomes visible the moment it lands and a changed
// AFORGE_RTK takes effect without a restart.
var (
	toolMutex sync.Mutex
	tools     = map[string]*Tool{}
)

// Available answers where rtk is, and never fetches it. Everything it does is a
// local lookup, which is nothing beside the fork the caller is about to do.
func Available() (*Tool, bool) {
	path, ok := locate()
	if !ok {
		return nil, false
	}
	toolMutex.Lock()
	defer toolMutex.Unlock()
	if existing, ok := tools[path]; ok {
		return existing, true
	}
	if len(tools) > 8 {
		tools = map[string]*Tool{}
	}
	created := &Tool{Path: path, decisions: map[string]decision{}}
	tools[path] = created
	return created, true
}

// locate walks the resolution order: an explicit choice, then the user's own
// installation, then ours.
func locate() (string, bool) {
	if chosen := strings.TrimSpace(os.Getenv(EnvBinary)); chosen != "" {
		if strings.EqualFold(chosen, Off) {
			return "", false
		}
		if executable(chosen) {
			return chosen, true
		}
		// An explicit path that is not there is a stated intention, not a
		// reason to go looking elsewhere.
		return "", false
	}
	if found, err := exec.LookPath("rtk"); err == nil {
		return found, true
	}
	managed, err := managedPath()
	if err == nil && executable(managed) {
		return managed, true
	}
	return "", false
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// BinDir is where a bootstrapped rtk lives. It is aforge's own shelf, not the
// user's ~/.local/bin, because aforge put it there and aforge maintains it.
func BinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aforge", "bin"), nil
}

func managedPath() (string, error) {
	dir, err := BinDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "rtk"), nil
}

// Wrap answers what a command should actually run as. The returned command is
// the one to execute and the class says how far its exit code can be trusted;
// ClassNone means run what was passed in, untouched.
func (t *Tool) Wrap(ctx context.Context, command string) (string, Class) {
	if t == nil || strings.TrimSpace(command) == "" {
		return command, ClassNone
	}
	if hasMachineFormat(command) {
		return command, ClassNone
	}
	if remembered, ok := t.remembered(command); ok {
		if remembered.banned || remembered.class == ClassNone {
			return command, ClassNone
		}
		return remembered.command, remembered.class
	}
	rewritten, ok := t.rewrite(ctx, command)
	verdict := decision{command: command}
	if ok && rewritten != command {
		if class := eligible(rewritten); class != ClassNone {
			verdict = decision{command: rewritten, class: class}
		}
	}
	t.remember(command, verdict)
	if verdict.class == ClassNone {
		return command, ClassNone
	}
	return verdict.command, verdict.class
}

// Ban marks a rewrite that failed on rtk's own terms, so the shape is never
// wrapped again in this process.
func (t *Tool) Ban(command string) {
	if t == nil {
		return
	}
	t.remember(command, decision{command: command, banned: true})
}

// maxDecisions keeps the memory from growing with a long-lived resident's
// command variety. Dropping the table wholesale rather than evicting one entry
// costs a handful of rewrites and no bookkeeping; bans are cheap to relearn,
// since relearning one costs exactly one fallback.
const maxDecisions = 512

func (t *Tool) remembered(command string) (decision, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	found, ok := t.decisions[command]
	return found, ok
}

func (t *Tool) remember(command string, verdict decision) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if len(t.decisions) >= maxDecisions {
		t.decisions = map[string]decision{}
	}
	t.decisions[command] = verdict
}

// rewrite asks rtk what it would run instead. rtk's documented contract is exit
// 0 with the rewritten line or exit 1 with nothing; v0.45 actually exits 3 on
// the rewriting branch, so the answer is read from stdout rather than from the
// status, which is true under both.
func (t *Tool) rewrite(ctx context.Context, command string) (string, bool) {
	askCtx, cancel := context.WithTimeout(ctx, rewriteTimeout)
	defer cancel()
	ask := exec.CommandContext(askCtx, t.Path, "rewrite", command)
	// The answer is cached process-wide, so it must not depend on which
	// directory happened to ask: project-local filters are switched off for the
	// question. Telemetry is refused outright — aforge does not opt a user into
	// a third party's collection on their behalf.
	ask.Env = append(os.Environ(), "RTK_NO_TOML=1", "RTK_TELEMETRY_DISABLED=1")
	var out bytes.Buffer
	ask.Stdout = &out
	if err := ask.Run(); err != nil && out.Len() == 0 {
		return "", false
	}
	rewritten := strings.TrimSpace(out.String())
	if rewritten == "" || strings.HasPrefix(rewritten, nudgePrefix) {
		return "", false
	}
	return rewritten, true
}

// eligible reads the rewritten line and decides whether we are willing to run
// it. The rule is structural: split on the shell operators rtk composes with,
// and require that every segment either opens with an rtk subcommand we accept
// or does not invoke rtk at all.
//
// That second half is what makes this safe without a parser. rtk emits the bare
// token `rtk` only at command positions, so a bare `rtk` anywhere else — the
// `sudo rtk ls` it produces for `sudo ls`, which would need rtk on root's PATH
// — fails the test and runs plain. Mis-splitting a quoted operator can only
// invent segments, and an invented segment either mentions rtk, which rejects
// the line, or does not, which changes nothing.
func eligible(rewritten string) Class {
	class := ClassNone
	for _, segment := range segments(rewritten) {
		fields := strings.Fields(segment)
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "rtk" {
			// A segment rtk left alone is only acceptable if it is genuinely
			// left alone.
			if containsToken(fields, "rtk") {
				return ClassNone
			}
			continue
		}
		if len(fields) < 2 {
			return ClassNone
		}
		subcommand := fields[1]
		found, ok := inspect[subcommand]
		if !ok {
			return ClassNone
		}
		if verbs, gated := verbGated[subcommand]; gated {
			if len(fields) < 3 || !verbs[fields[2]] {
				return ClassNone
			}
		}
		if containsToken(fields[1:], "rtk") {
			return ClassNone
		}
		// A line that mixes classes is only as trustworthy as its weakest part.
		if class == ClassNone || found == ClassRead {
			class = found
		}
	}
	return class
}

func containsToken(fields []string, token string) bool {
	for _, field := range fields {
		if field == token {
			return true
		}
	}
	return false
}

// segments splits a command line at the operators rtk chains with. It is not a
// shell parser and does not need to be: eligible only asks each piece whether
// it opens with rtk, and errs toward refusing.
func segments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", "|", "\n", ";", "\n", "&", "\n")
	return strings.Split(replacer.Replace(command), "\n")
}

func hasMachineFormat(command string) bool {
	for _, field := range strings.Fields(command) {
		field = strings.Trim(field, `"'`)
		name := field
		if index := strings.Index(field, "="); index > 0 {
			name = field[:index]
		}
		for _, flag := range machineFormat {
			if name == flag {
				return true
			}
		}
	}
	return false
}

// Failed reads a wrapped run's result for the one thing that means the wrapping
// broke rather than the command: rtk could not execute what it was handed. That
// is exit 127 — either rtk is no longer where we found it, or it fell through
// to a program that is not installed — and in both cases the plain command is
// the honest answer, whether it succeeds or reports the same absence itself.
func Failed(exitCode int, output string) bool {
	return exitCode == 127 || strings.Contains(output, failedPrefix)
}

// StripNudge removes rtk's once-a-day hook advertisement from merged output.
func StripNudge(output string) string {
	if !strings.Contains(output, nudgePrefix) {
		return output
	}
	lines := strings.Split(output, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, nudgePrefix) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
