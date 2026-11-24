package executor

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestFenceAllowWordsAreDockerOffAndAbsolutePaths(t *testing.T) {
	got := ParseFenceAllow("/home/p", "docker, ~/.cache/ms-playwright", "/opt/tools\nrelative/path", "")
	if !got.Docker || got.Off {
		t.Fatalf("docker/off read wrong: %+v", got)
	}
	want := []string{"/home/p/.cache/ms-playwright", "/opt/tools"}
	if strings.Join(got.Paths, " ") != strings.Join(want, " ") {
		t.Fatalf("paths %q, want %q", got.Paths, want)
	}
	if bad := FenceAllowRejects("/home/p", "docker, relative/path"); len(bad) != 1 || bad[0] != "relative/path" {
		t.Fatalf("rejects %q", bad)
	}
	if !ParseFenceAllow("", "off").Off {
		t.Fatal("off is not off")
	}
}

func TestATurnedOffFenceIsNoFence(t *testing.T) {
	f, err := NewFence(FenceOptions{Root: t.TempDir(), StateDir: t.TempDir(), Allow: FenceAllow{Off: true}})
	if err != nil || f != nil {
		t.Fatalf("fence %v, err %v", f, err)
	}
	if FencedSeat(nil) != Host {
		t.Fatal("no fence is not the host seat")
	}
}

// fenceFixture is a home with keys, a codeaf home with secret and ordinary
// files, and a worktree whose git folder lives in another checkout.
func fenceFixture(t *testing.T, allow FenceAllow) (*Fence, string, string, string) {
	t.Helper()
	base := t.TempDir()
	home, codeaf := filepath.Join(base, "home"), filepath.Join(base, "home", ".codeaf")
	main, work := filepath.Join(base, "main"), filepath.Join(base, "work")
	common := filepath.Join(main, ".git")
	gitdir := filepath.Join(common, "worktrees", "work")
	for _, d := range []string{filepath.Join(home, ".ssh"), codeaf, filepath.Join(common, "hooks"), gitdir, work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(codeaf, "config.json"):      "{}",
		filepath.Join(codeaf, "credentials.json"): "{}",
		filepath.Join(codeaf, "vault.enc"):        "x",
		filepath.Join(codeaf, "chat.log"):         "x",
		filepath.Join(work, ".git"):               "gitdir: " + gitdir + "\n",
		filepath.Join(gitdir, "commondir"):        "../..\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := NewFence(FenceOptions{Root: work, Home: home, CodeafHome: codeaf, StateDir: filepath.Join(base, "state"), Allow: allow})
	if err != nil {
		t.Fatal(err)
	}
	return f, home, main, work
}

func canon(t *testing.T, p string) string {
	t.Helper()
	c, err := canonicalPath(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAFenceHidesKeysAndCodeafSecretsAndLetsAWorktreeCommit(t *testing.T) {
	f, home, main, _ := fenceFixture(t, FenceAllow{})
	for _, want := range []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".aws"), filepath.Join(home, ".config", "gh"),
		filepath.Join(home, ".codeaf", "config.json"), filepath.Join(home, ".codeaf", "credentials.json"), filepath.Join(home, ".codeaf", "vault.enc")} {
		if !contains(f.Hidden, canon(t, want)) {
			t.Errorf("%s is not hidden: %q", want, f.Hidden)
		}
	}
	if contains(f.Hidden, canon(t, filepath.Join(home, ".codeaf", "chat.log"))) {
		t.Error("an ordinary codeaf file is hidden")
	}
	if !contains(f.Writable, canon(t, filepath.Join(main, ".git"))) {
		t.Errorf("the repository's git folder is not writable: %q", f.Writable)
	}
	for _, ro := range []string{filepath.Join(main, ".git", "hooks"), filepath.Join(main, ".git", "config")} {
		if !contains(f.ReadOnly, canon(t, ro)) {
			t.Errorf("%s is not read-only: %q", ro, f.ReadOnly)
		}
	}
	if !contains(f.Sockets, "/var/run/docker.sock") {
		t.Errorf("the docker socket is not refused: %q", f.Sockets)
	}
	if RepositoryOf(f.Root) != canon(t, main) {
		t.Errorf("a worktree's repository is %q, want %q", RepositoryOf(f.Root), main)
	}
	for _, kv := range []string{"GOCACHE=" + filepath.Join(f.Cache, "go-build"), "TMPDIR=" + f.Tmp, "npm_config_cache=" + filepath.Join(f.Cache, "npm"), "DOCKER_HOST=unix://" + f.Blank} {
		if !contains(f.Env, kv) {
			t.Errorf("environment lacks %s", kv)
		}
	}
}

func TestAWideningUnhidesAndAllowsDocker(t *testing.T) {
	_, home, _, _ := fenceFixture(t, FenceAllow{})
	f, _, _, _ := fenceFixture(t, FenceAllow{Docker: true})
	if len(f.Sockets) != 0 || contains(f.Env, "DOCKER_HOST=unix://"+f.Blank) {
		t.Fatalf("docker allowed but refused: %q", f.Sockets)
	}
	opened, err := NewFence(FenceOptions{Root: t.TempDir(), Home: home, StateDir: t.TempDir(), Allow: FenceAllow{Paths: []string{filepath.Join(home, ".ssh")}}})
	if err != nil {
		t.Fatal(err)
	}
	if contains(opened.Hidden, canon(t, filepath.Join(home, ".ssh"))) {
		t.Fatal("an allowed folder is still hidden")
	}
}

func TestTheFileToolsKeepToTheFence(t *testing.T) {
	f, home, main, work := fenceFixture(t, FenceAllow{})
	for path, write := range map[string]bool{filepath.Join(work, "a.go"): true, filepath.Join(f.Tmp, "x"): true, filepath.Join(home, "notes"): false} {
		if !f.Allows(path, write) {
			t.Errorf("%s (write %v) refused", path, write)
		}
	}
	refused := map[string]bool{filepath.Join(home, ".ssh", "id_rsa"): false, filepath.Join(home, "outside"): true,
		filepath.Join(main, ".git", "hooks", "pre-commit"): true, filepath.Join(work, ".codeaf", "config.json"): true,
		filepath.Join(home, ".codeaf", "config.json"): false}
	// macOS keeps its system temporary folders writable (darwinTemp), and a
	// test's own temporary folder is one of them.
	if runtime.GOOS == "darwin" && within(canon(t, home), "/private/var/folders") {
		delete(refused, filepath.Join(home, "outside"))
	}
	for path, write := range refused {
		if f.Allows(path, write) {
			t.Errorf("%s (write %v) allowed", path, write)
		}
	}
}

func TestAvoidingGrantsEverythingBesideTheAvoidedPath(t *testing.T) {
	root := canon(t, t.TempDir())
	for _, d := range []string{"a/keep", "a/secret/inner", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "a", "secret"), filepath.Join(root, "a", "link")); err != nil {
		t.Fatal(err)
	}
	var got []string
	for p := range avoiding(root, []string{filepath.Join(root, "a", "secret")}) {
		rel, _ := filepath.Rel(root, p)
		got = append(got, rel)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != "a/file a/keep b" {
		t.Fatalf("grants %q", got)
	}
}

// A folder on the way to the avoided path that can be passed through but not
// listed must not end the walk: the grants BELOW it are still the chat's own
// files, and losing them made a hosted chat's commands unable to read its own
// git settings (P1 item 0, found on the first run of the hosted smoke).
func TestAvoidingKeepsGrantingBelowAFolderItCannotList(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lists every folder")
	}
	root := canon(t, t.TempDir())
	for _, d := range []string{"people/p/chat/home/keep", "people/p/chat/home/secret"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "people", "p", "chat", "home", "settings"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "people"), 0o711); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "people"), 0o700) })
	var got []string
	for p := range avoiding(root, []string{filepath.Join(root, "people", "p", "chat", "home", "secret")}) {
		rel, _ := filepath.Rel(root, p)
		got = append(got, rel)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != "people/p/chat/home/keep people/p/chat/home/settings" {
		t.Fatalf("grants %q", got)
	}
}

func TestTheSeatbeltFenceProfileRefusesWhatTheFenceRefuses(t *testing.T) {
	f, home, main, work := fenceFixture(t, FenceAllow{})
	profile := fenceProfile(f)
	allowAt := strings.Index(profile, "(allow file-write*")
	hideAt := strings.Index(profile, "(deny file-read* file-write*")
	roAt := strings.Index(profile, "(deny file-write*")
	if allowAt < 0 || hideAt < allowAt || roAt < allowAt {
		t.Fatalf("a refusal is not after the write grant it narrows:\n%s", profile)
	}
	for _, want := range []string{
		quote(canon(t, work)), quote(f.Tmp), quote(f.Cache), `"/private/var/folders"`,
		quote(canon(t, filepath.Join(home, ".ssh"))), quote(canon(t, filepath.Join(main, ".git", "hooks"))),
		`(remote unix-socket (path-literal "/var/run/docker.sock"))`, `(global-name "com.apple.SecurityServer")`,
		"(allow network*)",
	} {
		if !strings.Contains(profile, want) {
			t.Errorf("profile lacks %s", want)
		}
	}
	if strings.Contains(profile, "(deny network*)") {
		t.Error("the fence turned the network off")
	}
}

func TestTheRefusalIsOneSentenceAddedOnce(t *testing.T) {
	out := WithFenceLine("touch: cannot touch '/home/p/x': Permission denied")
	if !strings.HasSuffix(out, FenceLine) || strings.Count(WithFenceLine(out), FenceLine) != 1 {
		t.Fatalf("got %q", out)
	}
	if WithFenceLine("build failed: syntax error") != "build failed: syntax error" {
		t.Fatal("an ordinary failure was dressed as a refusal")
	}
	for _, banned := range []string{"auditor", "verdict", "verified", "refuted", "sandbox", "landlock", "seatbelt"} {
		if strings.Contains(strings.ToLower(FenceLine+FencePartialLine+FenceDegradedLine), banned) {
			t.Errorf("a person-facing line says %q", banned)
		}
	}
}

// A run whose state is in one codeaf folder can still be started by a person
// whose own codeaf folder holds the command line's keys; that folder's secrets
// are hidden from the run too, and its ordinary files are not.
func TestAFenceHidesAnotherCodeafHomesSecretsToo(t *testing.T) {
	base := t.TempDir()
	home, mine, theirs := filepath.Join(base, "home"), filepath.Join(base, "app-state"), filepath.Join(base, "home", ".codeaf")
	work := filepath.Join(base, "work")
	for _, d := range []string{home, mine, theirs, work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(mine, "config.json"), filepath.Join(theirs, "config.json"),
		filepath.Join(theirs, "credentials.json"), filepath.Join(theirs, "chat.log"),
	} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := NewFence(FenceOptions{Root: work, Home: home, CodeafHome: mine, OtherCodeafHomes: []string{theirs, ""}, StateDir: filepath.Join(base, "state")})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.Join(mine, "config.json"), filepath.Join(theirs, "config.json"), filepath.Join(theirs, "credentials.json")} {
		if !contains(f.Hidden, canon(t, want)) {
			t.Errorf("%s is not hidden: %q", want, f.Hidden)
		}
	}
	if contains(f.Hidden, canon(t, filepath.Join(theirs, "chat.log"))) {
		t.Error("an ordinary file in the other codeaf folder is hidden")
	}
}
