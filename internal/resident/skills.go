package resident

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/skills"
	"github.com/Agent-Field/codeaf/internal/store"
)

const (
	skillCandidateScanLimit = 10_000
	skillTrialTimeout       = 10 * time.Second
	skillFailureBytes       = 400
)

// All three passes of the shelf are gated on the journal (memo.go), and for
// the same reason: each of them exists to make the disk agree with the fact
// shelf, the fact shelf only moves when something is journaled, and none of
// them was cheap. Promotion walks every candidate's parent chain back to its
// top-level job — one node read per generation, per candidate. The bin sync
// stats and readlinks the whole shelf directory. The import scan stats the
// foreign roots and reads every SKILL.md it finds. A tick that runs for a
// reason unrelated to any of them — a clock deadline, the standing ceiling —
// used to pay for them all anyway, twice a second, forever, which is what an idle
// laptop heard as a disk that never spun down.
//
// The gates are separate because the passes do not run back to back and a
// shared one would let whichever ran first suppress the others. They are in
// memory rather than durable, unlike the consolidation lane's: the
// consolidator spends a model call, so a restart buying another one is
// expensive, whereas a restart here costs one extra read of a shelf that is
// almost always empty.
//
// The import pass has one more wrinkle than its siblings: it watches the
// FOREIGN disk, which the journal cannot see at all. The journal gate is
// therefore the quiet-machine discipline and nothing more — a skill dropped
// into ~/.claude/skills while nothing is journaled is imported by the first
// pass where anything was, which on a machine in use is minutes, and on a
// machine that idle is a disk that stays quiet. That trade is the one the
// other two passes already made.

type skillRecurrence struct {
	facts []store.Fact
	jobs  map[string]bool
}

// promoteRecurringSkills is the retrospective's mechanical half. The model
// may propose a procedure after one job, but only two independent top-level
// jobs and a green executable check can make it active.
func (r *Reconciler) promoteRecurringSkills(ctx context.Context) {
	if !r.skillPromotionGate.due(r.store) {
		return
	}
	candidates, err := r.store.SkillFacts(store.FactCandidate, skillCandidateScanLimit)
	if err != nil || len(candidates) == 0 {
		return
	}
	root, err := store.SkillsRoot()
	if err != nil || os.MkdirAll(root, 0o755) != nil {
		return
	}

	groups := make(map[string]*skillRecurrence)
	for _, candidate := range candidates {
		jobID, ok := r.topLevelJobID(candidate.NodeID)
		if !ok {
			continue
		}
		key := skillMatchKey(candidate)
		group := groups[key]
		if group == nil {
			group = &skillRecurrence{jobs: make(map[string]bool)}
			groups[key] = group
		}
		group.facts = append(group.facts, candidate)
		group.jobs[jobID] = true
	}
	keys := make([]string, 0, len(groups))
	promotionOccurrences := int(r.store.Parameter(store.ParameterSkillPromotionOccurrences))
	for key, group := range groups {
		if len(group.jobs) >= promotionOccurrences {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		group := groups[key]
		selected := group.facts[0] // SkillFacts is newest first.
		jobs := make([]string, 0, len(group.jobs))
		for jobID := range group.jobs {
			jobs = append(jobs, jobID)
		}
		sort.Strings(jobs)

		installed, digest, err := installSkillTrial(ctx, root, selected, jobs)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			reason := skillFailureReason(err)
			for _, candidate := range group.facts {
				_ = r.store.SupersedeFactWithReason(candidate.Seq, 0, reason)
			}
			continue
		}
		if err := r.store.ActivateSkill(selected.Seq, installed, digest); err != nil {
			continue
		}
		r.queueLearningMoment(selected.NodeID, forgedSkillMoment(filepath.Base(installed)))
		reason := fmt.Sprintf("matched independent jobs and promoted as skill #%d", selected.Seq)
		for _, candidate := range group.facts {
			if candidate.Seq != selected.Seq {
				_ = r.store.SupersedeFactWithReason(candidate.Seq, selected.Seq, reason)
			}
		}
	}
}

func (r *Reconciler) topLevelJobID(nodeID string) (string, bool) {
	if nodeID == "" || nodeID == store.RootID {
		return "", false
	}
	node, ok, err := r.store.Node(nodeID)
	if err != nil || !ok {
		return "", false
	}
	for node.Parent != store.RootID {
		if node.Parent == "" {
			return "", false
		}
		node, ok, err = r.store.Node(node.Parent)
		if err != nil || !ok {
			return "", false
		}
	}
	return node.ID, true
}

func skillMatchKey(fact store.Fact) string {
	scope := strings.ToLower(strings.TrimSpace(fact.Scope))
	doc := strings.Join(strings.Fields(strings.ToLower(fact.Body)), " ")
	return scope + "\x00" + doc
}

func installSkillTrial(ctx context.Context, root string, candidate store.Fact, jobs []string) (string, string, error) {
	rawSource := strings.TrimSpace(candidate.Artifact)
	if !filepath.IsAbs(rawSource) {
		return "", "", fmt.Errorf("candidate artifact %q is not absolute", rawSource)
	}
	source, err := filepath.Abs(rawSource)
	if err != nil {
		return "", "", fmt.Errorf("resolve candidate artifact: %w", err)
	}
	if pathsOverlap(source, root) {
		return "", "", fmt.Errorf("candidate artifact %q overlaps the skill shelf", source)
	}
	staging, err := os.MkdirTemp(root, ".candidate-")
	if err != nil {
		return "", "", fmt.Errorf("create skill staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	if err := copySkillDirectory(source, staging); err != nil {
		return "", "", fmt.Errorf("prepare skill trial: %w", err)
	}
	provenance := strings.Join(jobs, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(staging, "PROVENANCE"), []byte(provenance), 0o644); err != nil {
		return "", "", fmt.Errorf("write skill provenance: %w", err)
	}
	if err := runSkillCheck(ctx, staging); err != nil {
		return "", "", err
	}
	if _, err := skillExecutable(staging); err != nil {
		return "", "", fmt.Errorf("check.sh removed the skill executable: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "PROVENANCE"), []byte(provenance), 0o644); err != nil {
		return "", "", fmt.Errorf("rewrite skill provenance: %w", err)
	}

	slug := skillSlug(filepath.Base(source))
	target := filepath.Join(root, slug)
	if _, err := os.Lstat(target); err == nil {
		slug += "-" + strconv.FormatInt(candidate.Seq, 10)
		target = filepath.Join(root, slug)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("install skill: inspect target: %w", err)
	}
	if _, err := os.Lstat(target); err == nil {
		return "", "", fmt.Errorf("install skill: target %q already exists", target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("install skill: inspect target: %w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		return "", "", fmt.Errorf("install skill: %w", err)
	}

	digest, err := contentDigest(target)
	if err != nil {
		return "", "", fmt.Errorf("install skill: compute digest: %w", err)
	}
	return target, digest, nil
}

// contentDigest returns a sha256 digest of all regular files under dir,
// sorted by relative path. Symlinks are refused — installSkillTrial rejects
// them earlier, and this read ensures the digest covers only what the trial
// copied.
func contentDigest(dir string) (string, error) {
	entries := make([]string, 0)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		entries = append(entries, relative)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)

	h := sha256.New()
	for _, relative := range entries {
		path := filepath.Join(dir, relative)
		// Write the relative path as a prefix so two directories with
		// different file structures but the same content after concatenation
		// produce different digests.
		if _, err := io.WriteString(h, relative+"\x00"); err != nil {
			return "", err
		}
		f, err := skills.OpenRegular(path)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func copySkillDirectory(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("artifact %q is not a real directory", source)
	}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact contains symlink %q", relative)
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact contains non-regular file %q", relative)
		}
		return copySkillFile(path, destination, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	check := filepath.Join(target, "check.sh")
	if !regularExecutable(check) {
		return fmt.Errorf("artifact needs executable check.sh")
	}
	if _, err := skillExecutable(target); err != nil {
		return err
	}
	return nil
}

func copySkillFile(source, target string, mode fs.FileMode) error {
	reader, err := skills.OpenRegular(source)
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, reader)
	closeErr := writer.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func runSkillCheck(ctx context.Context, skillDir string) error {
	clean, err := os.MkdirTemp("", "codeaf-skill-check-")
	if err != nil {
		return fmt.Errorf("create clean check directory: %w", err)
	}
	defer os.RemoveAll(clean)

	trialCtx, cancel := context.WithTimeout(ctx, skillTrialTimeout)
	defer cancel()
	cmd := exec.CommandContext(trialCtx, filepath.Join(skillDir, "check.sh"))
	cmd.Dir = clean
	cmd.Env = safeSkillCheckEnv(skillDir)
	cmd.WaitDelay = time.Second
	output, runErr := cmd.CombinedOutput()
	if trialCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("check.sh timed out after %s: %s", skillTrialTimeout, boundedSkillOutput(output))
	}
	if runErr != nil {
		return fmt.Errorf("check.sh failed: %v: %s", runErr, boundedSkillOutput(output))
	}
	return nil
}

func boundedSkillOutput(output []byte) string {
	text := strings.TrimSpace(string(output))
	if len(text) <= skillFailureBytes {
		return text
	}
	return clipBlock(text, skillFailureBytes)
}

func safeSkillCheckEnv(skillDir string) []string {
	safeKeys := map[string]bool{
		"PATH":        true,
		"HOME":        true,
		"TMPDIR":      true,
		"USER":        true,
		"LOGNAME":     true,
		"SHELL":       true,
		"LANG":        true,
		"LC_ALL":      true,
		"TERM":        true,
		"GOROOT":      true,
		"GOPATH":      true,
		"CARGO_HOME":  true,
		"RUSTUP_HOME": true,
	}
	var envs []string
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := parts[0]
		upper := strings.ToUpper(k)
		if strings.Contains(upper, "KEY") ||
			strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "AUTH") ||
			strings.Contains(upper, "PASSWORD") ||
			strings.Contains(upper, "CREDENTIAL") ||
			strings.HasPrefix(upper, "ANTHROPIC_") ||
			strings.HasPrefix(upper, "OPENAI_") ||
			strings.HasPrefix(upper, "GEMINI_") ||
			strings.HasPrefix(upper, "DEEPSEEK_") ||
			strings.HasPrefix(upper, "SLACK_") ||
			strings.HasPrefix(upper, "GITHUB_") ||
			strings.HasPrefix(upper, "AWS_") {
			continue
		}
		if safeKeys[k] || strings.HasPrefix(k, "LC_") {
			envs = append(envs, kv)
		}
	}
	const skillDirEnv = "CODEAF_SKILL_DIR"
	envs = append(envs, skillDirEnv+"="+skillDir, env.Legacy(skillDirEnv)+"="+skillDir)
	return envs
}

func skillFailureReason(err error) string {
	reason := "skill trial failed: " + strings.TrimSpace(err.Error())
	return clipFactBody(reason)
}

func skillSlug(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var slug strings.Builder
	dash := false
	for _, char := range name {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			slug.WriteRune(char)
			dash = false
		default:
			if slug.Len() > 0 && !dash {
				slug.WriteByte('-')
				dash = true
			}
		}
		if slug.Len() >= 48 {
			break
		}
	}
	result := strings.Trim(slug.String(), "-")
	if result == "" {
		return "skill"
	}
	return result
}

func regularExecutable(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func skillExecutable(skillDir string) (string, error) {
	run := filepath.Join(skillDir, "run.sh")
	if regularExecutable(run) {
		return run, nil
	}
	var executable string
	err := filepath.WalkDir(skillDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || executable != "" {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, relErr := filepath.Rel(skillDir, path)
		if relErr != nil {
			return relErr
		}
		if relative == "check.sh" || relative == "PROVENANCE" {
			return nil
		}
		if regularExecutable(path) {
			executable = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if executable == "" {
		return "", fmt.Errorf("artifact needs run.sh or another executable")
	}
	return executable, nil
}

func pathsOverlap(first, second string) bool {
	within := func(parent, child string) bool {
		relative, err := filepath.Rel(parent, child)
		return err == nil && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(os.PathSeparator))
	}
	return within(first, second) || within(second, first)
}

// syncSkillBins makes the active fact view true on disk and retires only bin
// symlinks this forge owns. Installed directories remain as provenance-bearing
// evidence after retirement; they simply stop being offered on PATH.
func (r *Reconciler) syncSkillBins() {
	if !r.skillBinGate.due(r.store) {
		return
	}
	facts, err := r.store.SkillFacts("", skillCandidateScanLimit)
	if err != nil {
		return
	}
	root, err := store.SkillsRoot()
	if err != nil {
		return
	}
	bin := filepath.Join(root, "bin")
	active := make(map[string]bool)
	installed := make(map[string]bool)
	for _, fact := range facts {
		if _, ok := installedSkillSlug(root, fact.Artifact); !ok {
			continue
		}
		installed[fact.Artifact] = true
		if fact.Status == store.FactActive {
			active[fact.Artifact] = true
		}
	}
	if len(active) > 0 {
		if err := os.MkdirAll(bin, 0o755); err != nil {
			return
		}
	}
	for artifact := range active {
		ensureSkillBinLink(root, bin, artifact)
	}
	for artifact := range installed {
		if !active[artifact] {
			removeSkillBinLink(root, bin, artifact)
		}
	}
}

func installedSkillSlug(root, artifact string) (string, bool) {
	if artifact == "" {
		return "", false
	}
	relative, err := filepath.Rel(root, artifact)
	if err != nil || relative == "." || relative == ".." || relative == "bin" ||
		filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) ||
		strings.Contains(relative, string(os.PathSeparator)) {
		return "", false
	}
	return relative, true
}

func ensureSkillBinLink(root, bin, artifact string) {
	slug, ok := installedSkillSlug(root, artifact)
	if !ok {
		return
	}
	executable, err := skillExecutable(artifact)
	if err != nil {
		return
	}
	link := filepath.Join(bin, slug)
	if target, err := os.Readlink(link); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(bin, target)
		}
		if filepath.Clean(target) == filepath.Clean(executable) {
			return
		}
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		return
	}
	_ = os.Symlink(executable, link)
}

func removeSkillBinLink(root, bin, artifact string) {
	slug, ok := installedSkillSlug(root, artifact)
	if !ok {
		return
	}
	link := filepath.Join(bin, slug)
	target, err := os.Readlink(link)
	if err != nil {
		return
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(bin, target)
	}
	relative, err := filepath.Rel(artifact, filepath.Clean(target))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return
	}
	_ = os.Remove(link)
}

// importedSkillTrust is the tier every foreign skill is registered under. It
// is the fence the whole import pass is built on: the sync touches ONLY facts
// carrying exactly this tier, so a forged or authored skill — anything the
// forge itself taught or a person wrote — is never superseded, rewritten or
// otherwise disturbed by a folder it never heard of changing on disk.
const importedSkillTrust = "imported-provisional"

// importForeignSkills is the shelf's third pass: it registers skills a person
// already has for another harness — Claude Code, Codex, any agentskills.io
// reader — from where those harnesses keep them, in place, with no copy and
// no reinstall.
func (r *Reconciler) importForeignSkills() {
	if !r.skillImportGate.due(r.store) {
		return
	}
	// The project directory is the working directory, derived exactly the way
	// the rest of the tree derives a surface's own ground: the head's
	// workspaceRoot and the errand surface's errandWorkspace both fall back to
	// it, so the resident reads the same directory and invents no new source.
	projectDir, err := os.Getwd()
	if err != nil {
		return
	}
	// The home directory is the one door internal/home owns: CODEAF_HOME
	// moves it wholesale, and a test binary that named no home of its own is
	// handed the quarantine rather than the home of whoever ran it, so the
	// scan never imports a real person's skills into a throwaway store.
	homeDir, err := home.Login()
	if err != nil {
		return
	}
	r.reconcileImportedSkills(projectDir, homeDir)
}

// reconcileImportedSkills is the reconciler's own door into the import pass:
// the gate and the working directory are the resident's, and the store-bound
// work is shared with the v3 chat door, which runs the same pass on every
// launch because it claims no residency of its own.
func (r *Reconciler) reconcileImportedSkills(projectDir, homeDir string) {
	ReconcileImportedSkills(r.store, projectDir, homeDir)
}

// ReconcileImportedSkills makes the fact shelf agree with the foreign roots:
// every discovered skill that is not shadowed gets one active fact whose
// artifact is the ORIGINAL directory, and every previously imported fact
// whose folder went away or stopped being readable is superseded with the
// reason why. Both directories come in as arguments and the store is the
// caller's, so the same pass serves the resident reconciler's gated tick and
// a chat door that runs it once per launch — and it is idempotent: a second
// run over an unchanged disk journals nothing.
//
// The pass never fails loudly. A folder that cannot be digested, a fact that
// cannot be recorded: each is skipped and picked up by the next pass, because
// half-imported is a state the next pass repairs and a failed pass is one
// nothing repairs.
func ReconcileImportedSkills(st *store.Store, projectDir, homeDir string) {
	discovered, err := skills.Discover(skills.Options{ProjectDir: projectDir, HomeDir: homeDir})
	if err != nil {
		return
	}
	active, err := st.SkillFacts(store.FactActive, skillCandidateScanLimit)
	if err != nil {
		return
	}
	// Only facts this pass itself recorded are its business. The map is
	// keyed by the original directory because that is the skill's identity
	// across runs — names, scopes and docs may change, the folder is what the
	// person deleted or edited. SkillFacts is newest first, so the first fact
	// seen for a directory is the one to keep; a second one can only exist
	// when a crash landed between one import's activation and the supersede it
	// was about to journal, and it retires here so the shelf keeps its
	// one-active-fact-per-folder shape.
	imported := make(map[string]store.Fact)
	for _, fact := range active {
		if fact.Trust != importedSkillTrust {
			continue
		}
		dir := filepath.Clean(strings.TrimSpace(fact.Artifact))
		if dir == "" {
			continue
		}
		if existing, seen := imported[dir]; seen {
			_ = st.SupersedeFactWithReason(fact.Seq, existing.Seq, "duplicate import record")
			continue
		}
		imported[dir] = fact
	}

	// alive is every directory this scan still endorses — shadowed ones
	// included, because a folder another root outranks has not gone away, and
	// superseding a live folder because it lost a naming contest would retire
	// a working skill for a cosmetic reason. A skill that LOADED endorses its
	// folder even when it carries a soft warning — a name that does not match
	// its folder is still a working skill, per the spec's client guide — while
	// a skipped one (no name, no description, unparseable) endorses nothing.
	alive := make(map[string]bool)
	for _, skill := range discovered {
		if skill.Name == "" || skill.Description == "" {
			continue
		}
		dir := filepath.Clean(skill.Dir)
		alive[dir] = true
		if skill.Shadowed {
			continue
		}
		// A skill folder reached through a link is digested at the folder the
		// link names. The fact keeps the link as its artifact, because the
		// link's name is the skill's name; but the walk below does not descend
		// through a link at its root, so digesting the link itself would hash
		// nothing and an edited skill would never be read again.
		digestDir := dir
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			digestDir = resolved
		}
		digest, err := contentDigest(digestDir)
		if err != nil {
			continue
		}
		if existing, ok := imported[dir]; ok && existing.Digest == digest {
			continue
		}
		candidate, err := st.RecordSkillCandidateFrom(store.FactWriterOther, store.RootID,
			importedSkillScope(skill, projectDir), clipFactBody(skill.Description), dir, importedSkillTrust)
		if err != nil {
			continue
		}
		if err := st.ActivateSkill(candidate.Seq, dir, digest); err != nil {
			continue
		}
		if existing, ok := imported[dir]; ok {
			_ = st.SupersedeFactWithReason(existing.Seq, candidate.Seq,
				"imported skill changed on disk")
		}
	}

	// What the disk no longer endorses must retire: a deleted folder and a
	// folder whose SKILL.md stopped parsing read the same from here, and the
	// reason names the file because that is the thing a person goes looking
	// for. A shadowed folder stays alive, so it never reaches this arm.
	gone := make([]string, 0, len(imported))
	for dir := range imported {
		if !alive[dir] {
			gone = append(gone, dir)
		}
	}
	sort.Strings(gone)
	for _, dir := range gone {
		_ = st.SupersedeFactWithReason(imported[dir].Seq, 0,
			"skill folder no longer holds a readable SKILL.md")
	}
}

// importedSkillScope names where a discovered skill came from, in the tree's
// kind:value convention (notebook.go builds "repo:"+dir and "tool:"+word the
// same way). The rule is deterministic and read off the discovery, never the
// clock: a project skill is scoped to its project directory, so the catalog's
// scorer surfaces it exactly when the work is in that directory; a user skill
// is scoped to the harness folder it was read from, which names its source
// without naming any one machine's paths.
//
// THE HARNESS IS THE ROOT'S FIRST FOLDER, which is the same answer as before
// for the six skills folders and the right one for the two roots that sit
// deeper: a skill out of a Claude Code plugin is a Claude Code skill, and one
// out of Codex's bundled folder is a Codex skill.
func importedSkillScope(skill skills.Skill, projectDir string) string {
	if skill.Scope == skills.ScopeProject {
		return "repo:" + projectDir
	}
	harness, _, _ := strings.Cut(strings.TrimPrefix(filepath.ToSlash(skill.Root), "."), "/")
	return "harness:" + harness
}
