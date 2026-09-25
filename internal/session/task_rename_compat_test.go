package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// H5: task commits are authored with the current product name and address.
func TestH5TaskCommitIdentityUsesTheCurrentName(t *testing.T) {
	got := strings.Join(codeafGitIdentity(), " ")
	for _, want := range []string{"user.name=codeaf", "user.email=agentfield-bot@users.noreply.github.com"} {
		if !strings.Contains(got, want) {
			t.Fatalf("identity %q does not contain %q", got, want)
		}
	}
	// The author is the bot account — the same identity the Co-Authored-By
	// trailer names — and BOTH addresses codeaf once committed with stay
	// recognised as its own: a landing that forgot one would read old task work
	// as a person's intervening movement and refuse to land.
	for _, pair := range [][2]string{
		{codeafGitName, codeafGitEmail},
		{legacyBotGitName, legacyBotGitEmail},
		{legacyCodeafGitName, legacyCodeafGitEmail},
	} {
		if !taskCommitIdentity(pair[0], pair[1]) {
			t.Fatalf("taskCommitIdentity(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
	if taskCommitIdentity("person", "person@example.invalid") {
		t.Fatal("taskCommitIdentity accepted a person's identity")
	}
}

// A registered task from before the rename is resumed from its exact directory
// and lands through the ordinary cleanup road. The directory and Git's
// registration both disappear only after its commit reaches the person's tree.
func TestLegacyRegisteredWorktreeResumesLandsAndCleansUp(t *testing.T) {
	repo := canonicalPath(newTestRepo(t))
	homeBranch := currentBranch(repo)
	homeSHA := branchCommit(repo, homeBranch)
	sessionID := "legacy-session"
	legacyDir := filepath.Join(repo, filepath.FromSlash(legacyTasksDirName), sessionID, "41")
	branch := "task/former-worktree"
	mustGit(t, repo, "worktree", "add", "-b", branch, legacyDir)
	writeFile(t, filepath.Join(legacyDir, "restored.txt"), "resumed from the former path\n")

	if got, _ := taskOwnFolder(Place{}, repo, sessionID, 41); got != canonicalPath(legacyDir) {
		t.Fatalf("reload chose %q, want registered former tree %q", got, legacyDir)
	}
	agent, node := unverifiedNode(t, func(config *Config) { config.Workspace = repo })
	tree := taskTree{
		dir: legacyDir, root: repo, branch: branch, home: homeBranch, homeSha: homeSHA,
		ground: repo, mode: TaskModeWorktree,
	}
	node.setTree(tree)
	node.graph.mu.Lock()
	node.interrupted = true
	node.graph.mu.Unlock()
	resumed, ok := node.resumeTree(agent.config.Place, repo)
	if !ok || resumed.dir != canonicalPath(legacyDir) {
		t.Fatalf("resume = %q %v, want former registered tree", resumed.dir, ok)
	}
	merge, problem, _, refusal := resumed.comeHome("resume former worktree", []string{"restored.txt"}, gitSignature{})
	if merge != mergeMerged || problem != "" || refusal != refusedNothing {
		t.Fatalf("landing = %q %q %v", merge, problem, refusal)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "restored.txt")); err != nil || string(body) != "resumed from the former path\n" {
		t.Fatalf("landed file = %q, %v", body, err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("former worktree directory remains: %v", err)
	}
	listed, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, canonicalPath(legacyDir)) {
		t.Fatalf("former worktree registration remains:\n%s", listed)
	}

	lock := openGitRootLock(Place{}, repo)
	if lock == nil {
		t.Fatal("repository lock could not be opened")
	}
	defer lock.Close()
	// THE LOCK'S OWN BOUNDARY CANONICALIZES THE ROOT — two spellings of one
	// repository must never make two locks — so the expectation is spelled the
	// way this filesystem spells it: /var/… and /private/var/… are one directory
	// on a Mac, and only the resolved form is the one git and the lock share.
	wantLock := canonicalPath(filepath.Join(repo, filepath.FromSlash(legacyTasksDirName), gitRootLockName))
	if lock.Name() != wantLock {
		t.Fatalf("repository lock = %q, want pre-rename lock %q", lock.Name(), wantLock)
	}
	fresh := canonicalPath(newTestRepo(t))
	freshLock := openGitRootLock(Place{}, fresh)
	if freshLock == nil {
		t.Fatal("fresh repository lock could not be opened")
	}
	defer freshLock.Close()
	if want := canonicalPath(filepath.Join(fresh, filepath.FromSlash(tasksDirName), gitRootLockName)); freshLock.Name() != want {
		t.Fatalf("fresh repository lock = %q, want current path %q", freshLock.Name(), want)
	}
	if _, err := os.Stat(filepath.Join(fresh, filepath.FromSlash(legacyTasksDirName))); !os.IsNotExist(err) {
		t.Fatalf("fresh lock wrote former task state: %v", err)
	}
}

func TestTaskMetadataReadsCurrentFirstAndFallsBackOnlyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	formerRecord := releasedTree{Branch: "task/former", Root: "/former"}
	former, _ := json.Marshal(formerRecord)
	writeFile(t, filepath.Join(dir, legacyCodeafDroppings, releasedRecord), string(former))
	if got, ok := rememberedRelease(dir); !ok || got != formerRecord {
		t.Fatalf("former release record = %+v %v", got, ok)
	}

	currentRecord := releasedTree{Branch: "task/current", Root: "/current"}
	current, _ := json.Marshal(currentRecord)
	writeFile(t, filepath.Join(dir, codeafDroppings, releasedRecord), string(current))
	if got, ok := rememberedRelease(dir); !ok || got != currentRecord {
		t.Fatalf("current release record did not win: %+v %v", got, ok)
	}
	writeFile(t, filepath.Join(dir, codeafDroppings, releasedRecord), "not json")
	if _, ok := rememberedRelease(dir); ok {
		t.Fatal("an unusable current record silently fell back to former state")
	}
	forgetReleased(dir)
	for _, dropping := range taskDroppingNames() {
		if _, err := os.Stat(filepath.Join(dir, dropping, releasedRecord)); !os.IsNotExist(err) {
			t.Fatalf("release record remains under %s: %v", dropping, err)
		}
	}

	baselineBody, _ := json.Marshal(groundBaseline{Paths: map[string]string{"kept.txt": "digest"}})
	writeFile(t, filepath.Join(dir, legacyCodeafDroppings, groundBaselineRecord), string(baselineBody))
	if got := rememberedGroundBaseline(dir); got["kept.txt"] != "digest" {
		t.Fatalf("former ground baseline = %v", got)
	}
	writeFile(t, filepath.Join(dir, codeafDroppings, groundBaselineRecord), "not json")
	if got := rememberedGroundBaseline(dir); got != nil {
		t.Fatalf("bad current baseline fell back to former state: %v", got)
	}

	leftBody, _ := json.Marshal([]string{"loose.txt"})
	writeFile(t, filepath.Join(dir, legacyCodeafDroppings, leftBehindRecord), string(leftBody))
	if got := rememberedLeftBehind(dir); len(got) != 1 || got[0] != "loose.txt" {
		t.Fatalf("former leavings record = %v", got)
	}
	writeFile(t, filepath.Join(dir, codeafDroppings, leftBehindRecord), "not json")
	if got := rememberedLeftBehind(dir); got != nil {
		t.Fatalf("bad current leavings fell back to former state: %v", got)
	}
}

// H5: a repository advanced only by a commit bearing the former task identity
// is still recognised as machine work, while a person's next commit is not.
func TestH5LegacyTaskCommitIsStillOurs(t *testing.T) {
	repo := newTestRepo(t)
	branch := currentBranch(repo)
	recorded := branchCommit(repo, branch)
	if err := os.WriteFile(filepath.Join(repo, "machine.txt"), []byte("old task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "machine.txt")
	mustGit(t, repo, "-c", "user.name="+legacyCodeafGitName, "-c", "user.email="+legacyCodeafGitEmail,
		"commit", "-m", "task work")
	if branchMovedByPerson(repo, branch, recorded) {
		t.Fatal("legacy-authored task commit was treated as a person's movement")
	}
	mustGit(t, repo, "-c", "user.name="+legacyBotGitName, "-c", "user.email="+legacyBotGitEmail,
		"commit", "--allow-empty", "-m", "pre-bot task work")
	if branchMovedByPerson(repo, branch, recorded) {
		t.Fatal("pre-bot task commit was treated as a person's movement")
	}
	mustGit(t, repo, "-c", "user.name=person", "-c", "user.email=person@example.invalid",
		"commit", "--allow-empty", "-m", "person moved it")
	if !branchMovedByPerson(repo, branch, recorded) {
		t.Fatal("person-authored commit was treated as task machinery")
	}
}

// H5: every recogniser and staging filter accepts both repository-dropping
// spellings while the live write path stays unchanged.
func TestH5TaskDroppingRecognisersAcceptBothSpellings(t *testing.T) {
	if codeafDroppings != ".codeaf" {
		t.Fatalf("live dropping path moved to %q", codeafDroppings)
	}
	for _, name := range taskDroppingNames() {
		if !isTaskDropping(name) || !isTaskDropping(filepath.ToSlash(filepath.Join(name, "tasks", "1"))) {
			t.Fatalf("%q is not recognised as task machinery", name)
		}
		if got := stageableWork(t.TempDir(), []string{filepath.ToSlash(filepath.Join(name, "private.log"))}); len(got) != 0 {
			t.Fatalf("%q was offered to git: %v", name, got)
		}
	}
	root := filepath.Join(t.TempDir(), legacyCodeafDroppings, "tasks", "family")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if (taskTree{root: root, ground: root}).landsInThePersonsRepository() {
		t.Fatal("legacy task tree was treated as the person's repository")
	}

	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "person.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range taskDroppingNames() {
		private := filepath.Join(repo, name, "private.log")
		if err := os.MkdirAll(filepath.Dir(private), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(private, []byte("private\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := groundManifest(repo)
	if !strings.Contains(manifest, "person.txt") {
		t.Fatalf("manifest lost person work: %q", manifest)
	}
	for _, name := range taskDroppingNames() {
		if strings.Contains(manifest, name) {
			t.Fatalf("manifest exposed %s: %q", name, manifest)
		}
	}
	digests := map[string]string{}
	if !gatherDigests(repo, "", digests, &digestWalk{budget: auditRestoreEntries}) {
		t.Fatal("digest walk unexpectedly exceeded its budget")
	}
	for path := range digests {
		if isTaskDropping(path) {
			t.Fatalf("digest walk recorded private path %q", path)
		}
	}

	mirror := t.TempDir()
	if err := os.WriteFile(filepath.Join(mirror, "person.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range taskDroppingNames() {
		private := filepath.Join(mirror, name, "private.log")
		if err := os.MkdirAll(filepath.Dir(private), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(private, []byte("private\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if problem := openFamilyRepository(mirror); problem != "" {
		t.Fatalf("open family repository: %s", problem)
	}
	tracked, err := git(mirror, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tracked, "person.txt") {
		t.Fatalf("family baseline lost person work: %q", tracked)
	}
	for _, name := range taskDroppingNames() {
		if strings.Contains(tracked, name) {
			t.Fatalf("family baseline staged %s: %q", name, tracked)
		}
	}
}
