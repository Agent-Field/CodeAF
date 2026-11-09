package cellstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// capturePlanOp asks for the engine's complete read scope before the guard
// reads content. Only the engine knows when a delta must become a full capture.
type capturePlanOp struct {
	Changed []string `json:"changed"`
}

func (capturePlanOp) Verb() string { return "capture-plan" }
func (o capturePlanOp) Args() []string {
	args := []string{"--json", "capture-plan"}
	for _, p := range o.Changed {
		args = append(args, "--changed", p)
	}
	return args
}

type capturePlan struct {
	Token string   `json:"token"`
	Full  bool     `json:"full"`
	Paths []string `json:"paths"`
}

// plannedSnapshot NEVER falls back to an unplanned capture. A changed policy,
// concurrent editor or background writer invalidates the token; every retry
// obtains a new scope and screens it again (C9/U2).
func (e Engine) plannedSnapshot(ctx context.Context, c cell.Cell, receipt string, changed []string, calls []Executed) (captureReply, error) {
	tree := e.tree(c)
	hints := e.Guard.index.hints(captureKey(tree, c.ID), tree, changed)
	// Rebinding can preserve the timeline's old path index. The caller knows
	// that its physical folder changed, so its first plan there must be full.
	if before, err := os.ReadFile(filepath.Join(e.LocalDir(c), "engine-tree")); err == nil && len(before) > 0 && string(before) != tree {
		hints = nil
	}
	if needsLinkScope(tree, hints) {
		hints = nil
	}
	policy, err := readPolicy(tree, e.policyDir(c))
	if err != nil {
		return captureReply{}, err
	}
	if err = policy.write(e.policyDir(c)); err != nil {
		return captureReply{}, err
	}
	for attempt := 0; attempt < maxPlanAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return captureReply{}, err
		}
		raw, err := e.do(ctx, c, e.cellDir(c), capturePlanOp{Changed: changedPaths(hints)})
		if err != nil {
			// A plan cannot enumerate an unreadable directory. Repair only that
			// eligibility metadata, then ask for a fresh token before screening bytes.
			text := strings.ToLower(err.Error())
			if strings.Contains(text, "permission denied") || strings.Contains(text, "access is denied") {
				repaired, repairErr := e.Guard.repairUnreadable(c, tree, e.policyDir(c))
				if repairErr != nil {
					return captureReply{}, repairErr
				}
				if repaired {
					continue
				}
			}
			return captureReply{}, fmt.Errorf("plan capture: %w", err)
		}
		var plan capturePlan
		if err = json.Unmarshal(raw, &plan); err != nil || plan.Token == "" || plan.Paths == nil {
			return captureReply{}, fmt.Errorf("plan capture: engine did not return a token and paths")
		}
		before, _ := os.ReadFile(filepath.Join(e.policyDir(c), policyName))
		screened, err := e.Guard.screenPlan(c, tree, e.policyDir(c), plan)
		if err != nil {
			return captureReply{}, err
		}
		if err = noteLeftOut(c, tree, screened, calls); err != nil {
			return captureReply{}, err
		}
		after, _ := os.ReadFile(filepath.Join(e.policyDir(c), policyName))
		if string(before) != string(after) {
			continue
		}
		// Inventory changes are composed content too. Their stamps are covered by
		// the token, so the engine refuses and re-plans if these writes changed it.
		markEngine(c, e)
		out, err := e.do(ctx, c, e.cellDir(c), sealOp{Turn: receipt, Plan: plan.Token})
		if err != nil {
			if strings.Contains(err.Error(), "re-plan") || strings.Contains(err.Error(), "capture plan") {
				// A delta may have expanded internally (for example, a hardlink).
				// A fresh full scope covers that fallback rather than repeating the hint.
				hints = nil
				continue
			}
			return captureReply{}, fmt.Errorf("seal: snapshot: %w", err)
		}
		var reply captureReply
		if err = json.Unmarshal(out, &reply); err != nil || reply.Snapshot == "" {
			return reply, fmt.Errorf("seal: engine did not return a snapshot id")
		}
		// An earlier group may have synced while this capture was still in flight.
		// The completed snapshot must remain owed before its local turn can become durable (C28/P5).
		markEngine(c, e)
		e.Guard.index.noteDirectories(captureKey(tree, c.ID), plan.Full, screened.directories)
		return reply, nil
	}
	return captureReply{}, fmt.Errorf("files kept changing while saving this chat; try again")
}

// maxPlanAttempts bounds contention with an editor or a long-running writer.
// Exhausting it surfaces a save failure and retains the recorder's WAL.
const maxPlanAttempts = 8

// screenPlan reads individual workspace candidates, never recursively walks a
// directory returned in a plan. Git history and harness-owned state retain
// their C9 exemption. Already withheld paths are rechecked so cleaning a file
// makes it eligible for a subsequent, freshly screened plan.
func (g Guard) screenPlan(c cell.Cell, tree, dir string, plan capturePlan) (Screened, error) {
	policy, err := readPolicy(tree, dir)
	if err != nil {
		return Screened{}, err
	}
	var paths []string
	directories := map[string]fileStamp{}
	for _, physical := range plan.Paths {
		rel, err := planRelative(tree, physical)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			stateRel, stateErr := planRelative(stateDir(c), physical)
			if stateErr != nil || stateRel == ".." || strings.HasPrefix(stateRel, ".."+string(filepath.Separator)) {
				return Screened{}, fmt.Errorf("plan capture: candidate is outside the workspace and chat state")
			}
			continue
		}
		rel = filepath.ToSlash(rel)
		// History is exempt from content screening, but its directory identities
		// and control files must still widen later tool hints when Git changes.
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if info, err := os.Lstat(physical); err == nil && (info.IsDir() || gitControlPath(rel)) {
				_ = os.SameFile(info, info)
				directories[rel] = fileStamp{At: info.ModTime(), Info: info}
			}
		}
		if !policy.isLeftOut(rel) {
			paths = append(paths, rel)
		}
	}
	// Git-ignored individual files still feed the vault, while ignored
	// directories remain unopened. This list is metadata, not a guard tree walk.
	ignored, err := ignoredFiles(tree)
	if err != nil {
		return Screened{}, err
	}
	paths = union(paths, ignored)
	left := g.leftOut(tree, paths, knownLocks(c), policy)
	policy = policy.leaving(left.paths())
	again := union(policy.withheld, policy.unreadable)
	observe := func(rel string, entry fs.DirEntry) {
		if !entry.IsDir() {
			return
		}
		if info, err := entry.Info(); err == nil {
			_ = os.SameFile(info, info)
			directories[rel] = fileStamp{At: info.ModTime(), Info: info}
		}
	}
	if info, err := os.Lstat(tree); err == nil {
		_ = os.SameFile(info, info)
		directories["."] = fileStamp{At: info.ModTime(), Info: info}
	}
	scan := keys.Scanner{Ledger: g.Ledger, Visit: observe, Skip: func(p string) bool { return policy.isLeftOut(p) || policy.isRebuilt(p) || policy.isHeld(p) }}
	found := scan.Candidates(tree, union(paths, again))
	// Known unreadable directories are outside the plan until their permissions
	// recover. Probe only their listing, never their children; clearing the rule
	// forces a new full plan whose newly eligible files are screened (C9/U2).
	for _, rel := range policy.unreadable {
		abs := filepath.Join(tree, filepath.FromSlash(rel))
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			continue
		}
		f, err := os.Open(abs)
		if err == nil {
			_, err = f.Readdirnames(1)
			_ = f.Close()
		}
		if err != nil && !errors.Is(err, io.EOF) {
			found = append(found, keys.Finding{Path: rel, Rule: keys.RuleUnreadable})
		}
	}
	secrets, unreadable := splitUnreadable(found)
	next := policy.with(pathsOf(secrets)).unreadableAre(pathsOf(unreadable))
	if err = next.write(dir); err != nil {
		return Screened{}, err
	}
	g.announce(policy, secrets)
	g.announceUnreadable(policy, unreadable)
	g.vault(c, tree, secrets)
	left.Apart = next.apart()
	left.directories = directories
	return left, nil
}

func ignoredFiles(tree string) ([]string, error) {
	if !hasGitMarker(tree) {
		return nil, nil
	}
	cmd := exec.Command("git", "--no-optional-locks", "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z") //codeaf:plumbing Ignored individual secrets still belong in the vault.
	cmd.Dir = tree
	raw, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && strings.Contains(string(exit.Stderr), "not a git repository") {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", ignoredReadFailure, err)
	}
	var paths []string
	var directories []string
	for _, path := range strings.Split(string(raw), "\x00") {
		if strings.HasSuffix(path, "/") {
			directories = append(directories, path)
			paths = append(paths, strings.TrimSuffix(path, "/"))
		} else if path != "" {
			paths = append(paths, path)
		}
	}
	// Git collapses config/ when only config/.env.local is ignored. Ask for its
	// individual names unless the directory itself is ignored (D2).
	if len(directories) > 0 {
		dirs, err := ignoredDirectories(tree)
		if err != nil {
			return nil, err
		}
		for _, dir := range directories {
			if inRules(dirs, strings.TrimSuffix(dir, "/")) {
				continue
			}
			cmd := exec.Command("git", "--no-optional-locks", "ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--", dir) //codeaf:plumbing Collapsed names do not hide individual vault files.
			cmd.Dir = tree
			raw, err := cmd.Output()
			if err != nil {
				return nil, err
			}
			for _, name := range strings.Split(string(raw), "\x00") {
				if name != "" {
					paths = append(paths, name)
				}
			}
		}
	}
	return paths, nil
}

// repairUnreadable is a policy-preparation fallback only after an engine plan
// fails on permissions. It visits directory metadata, never working-file bytes;
// the successful replacement plan remains the sole content-screening scope.
func (g Guard) repairUnreadable(c cell.Cell, tree, dir string) (bool, error) {
	policy, err := readPolicy(tree, dir)
	if err != nil {
		return false, err
	}
	policy.ignoredDirs, err = ignoredDirectories(tree)
	if err != nil {
		return false, err
	}
	var found []string
	_ = filepath.WalkDir(tree, func(physical string, entry fs.DirEntry, problem error) error {
		rel, err := filepath.Rel(tree, physical)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if problem != nil {
			if errors.Is(problem, os.ErrPermission) {
				found = append(found, rel)
			}
			return nil
		}
		if entry.IsDir() && (policy.isLeftOut(rel) || policy.isRebuilt(rel) || policy.isUnreadable(rel) || policy.isHeld(rel)) {
			return filepath.SkipDir
		}
		return nil
	})
	if len(found) == 0 {
		return false, nil
	}
	next := policy.unreadableAre(union(policy.unreadable, found))
	if err := next.write(dir); err != nil {
		return false, err
	}
	var notices []keys.Finding
	for _, path := range found {
		notices = append(notices, keys.Finding{Path: path, Rule: keys.RuleUnreadable})
	}
	g.announceUnreadable(policy, notices)
	return true, nil
}

// gitControlPath includes in-place edits to the files that direct history and
// Git's own ignore policy. Object/ref directory membership covers ordinary
// atomic Git updates without another per-seal repository walk.
func gitControlPath(path string) bool {
	switch path {
	case ".git", ".git/HEAD", ".git/index", ".git/config", ".git/packed-refs", ".git/shallow", ".git/info/exclude":
		return true
	}
	return false
}
