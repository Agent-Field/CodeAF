package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useCaseInsensitiveLstat lets the guard exercise filesystem identity on a volume that ignores case.
func useCaseInsensitiveLstat(t *testing.T) {
	t.Helper()
	original := fsLstat
	fsLstat = func(path string) (os.FileInfo, error) {
		resolved, err := caseInsensitivePath(filepath.Clean(path))
		if err != nil {
			return nil, err
		}
		return os.Lstat(resolved)
	}
	t.Cleanup(func() { fsLstat = original })
}

func caseInsensitivePath(path string) (string, error) {
	parent := filepath.Dir(path)
	if parent == path || path == "." {
		return path, nil
	}
	resolved, err := caseInsensitivePath(parent)
	if err != nil {
		return "", err
	}
	name := filepath.Base(path)
	candidate := filepath.Join(resolved, name)
	if _, err = os.Lstat(candidate); !os.IsNotExist(err) {
		return candidate, err
	}
	entries, readErr := os.ReadDir(resolved)
	if readErr != nil {
		return "", readErr
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), name) {
			return filepath.Join(resolved, entry.Name()), nil
		}
	}
	return "", err
}

func groundWithCaseMetadata(t *testing.T) string {
	t.Helper()
	ground := groundWithMetadata(t)
	mixed := filepath.Join(filepath.Dir(ground), "Repo")
	if err := os.Rename(ground, mixed); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".git/objects", ".git/refs", ".git/hooks", "payload", "sub", "vendor/lib/.git"} {
		if err := os.MkdirAll(filepath.Join(mixed, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{".git/HEAD", ".git/config", "payload/HEAD"} {
		if err := os.WriteFile(filepath.Join(mixed, file), []byte("fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return mixed
}

func TestFoldsCaseFilesystemSeam(t *testing.T) {
	ground := groundWithCaseMetadata(t)
	useCaseInsensitiveLstat(t)
	for _, paths := range [][2]string{
		{filepath.Join(ground, ".git"), filepath.Join(ground, ".GIT")},
		{ground, filepath.Join(filepath.Dir(ground), "repo")},
		{filepath.Join(ground, ".git", "HEAD"), filepath.Join(filepath.Dir(ground), "repo", ".Git", "head")},
	} {
		original, err := os.Lstat(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		variant, err := fsLstat(paths[1])
		if err != nil || !os.SameFile(original, variant) {
			t.Errorf("%q should identify %q, got %v (%v)", paths[1], paths[0], variant, err)
		}
	}
}

func TestACaseVariantOfTheGroundsGitIsRefusedWhenTheVolumeIgnoresCase(t *testing.T) {
	ground := groundWithCaseMetadata(t)
	useCaseInsensitiveLstat(t)
	refused := []string{
		"rm -rf .GIT",
		"rm -rf .Git",
		"rm -rf .GIT/",
		"mv .Git x",
		"mv .GIT ../kept",
		"rm -rf .git/OBJECTS",
		"rm -rf .git/REFS/heads",
		"rm .git/head",
		"rm .git/PACKED-REFS",
		"rm -rf .git/Worktrees",
		"rm -rf .git/MODULES/x",
		"echo 'ref: refs/heads/x' > .git/head",
		"echo x > .GIT/HEAD",
		"cp payload/HEAD .GIT/HEAD",
		"cp -t .GIT payload/HEAD",
		"cp --target-directory=.GIT payload/HEAD",
		"install -t .GIT payload/HEAD",
		"rsync -a --delete /tmp/empty/ .GIT/",
		"sed -i '' 's/bare/BARE/' .GIT/config",
		"sed -i 's/bare/BARE/' .GIT/CONFIG",
		"tee .git/head",
		"truncate -s 0 .GIT/config",
		"cp -r /tmp/other/.GIT .",
		"rm -rf " + filepath.Join(ground, ".GIT"),
		"cd " + ground + " && rm -rf .GIT",
		"cd sub && rm -rf ../.GIT",
		"rmdir .Git",
		"unlink .git/head",
		"ln -sf payload/HEAD .GIT/head",
		"perl -pi -e 's/bare/BARE/' .GIT/CONFIG",
		"shred .git/head",
		"patch .GIT/config",
		"> .GIT/HEAD",
	}
	assertCaseMetadataRefusals(t, ground, refused)

	parent := filepath.Dir(ground)
	alternate := filepath.Join(parent, "repo")
	assertCaseMetadataRefusals(t, alternate, []string{
		"rm -rf " + filepath.Join(alternate, ".GIT"),
		"rm -rf " + filepath.Join(ground, ".git"),
		"rm -rf .GIT",
		"rm -rf " + filepath.Join(parent, "REPO"),
		"cp -r /tmp/other/.GIT .",
	})

	assertCaseMetadataAllowed(t, ground, []string{
		"rm -f .git/index.lock",
		"rm -f .GIT/INDEX.LOCK",
		"touch .git/index.lock",
		"printf 'x\\n' >> .GIT/info/exclude",
		"cp /tmp/pre-commit .GIT/hooks/pre-commit",
		"cp -r /tmp/other/.GIT .git/hooks",
		"cat .GIT/HEAD",
		"ls .GIT",
		"echo x > add.py",
		"echo x > ADD.PY",
		"rm -rf .github",
		"rm -rf .GITHUB",
		"rm .gitmodules",
		"rm -rf .git.bak",
		"rm -rf vendor/lib/.GIT",
		"rm -rf *",
		"mv old.py .",
		"cp /tmp/a .",
	})

	guard := taskGroundGuard{agent: &Agent{config: Config{InTask: true, Workspace: ground}}}
	ctx := context.Background()
	write := withdrawnCall("c1", "write", `{"path":".GIT/HEAD","content":"x"}`)
	if _, result, allowed := guard.PreAction(ctx, nil, nil, write); allowed || !result.isError ||
		!strings.Contains(result.text, "replacing "+filepath.Join(ground, ".git")) ||
		!strings.Contains(result.text, outsideYourCopy) {
		t.Errorf("write to .GIT/HEAD answered %q (allowed %t), want the metadata refusal", result.text, allowed)
	}
	write = withdrawnCall("c2", "write", `{"path":"add.py","content":"x"}`)
	if _, result, allowed := guard.PreAction(ctx, nil, nil, write); !allowed {
		t.Errorf("write to add.py was refused with %q, want ordinary work allowed", result.text)
	}
}

func assertCaseMetadataRefusals(t *testing.T, ground string, commands []string) {
	t.Helper()
	metadata := "replacing " + filepath.Join(ground, ".git")
	for _, command := range commands {
		why := refusedOutsideGround(ground, command)
		if !strings.Contains(why, metadata) || !strings.Contains(why, outsideYourCopy) {
			t.Errorf("%q in %q answered %q, want the metadata refusal", command, ground, why)
		}
	}
}

func assertCaseMetadataAllowed(t *testing.T, ground string, commands []string) {
	t.Helper()
	for _, command := range commands {
		if why := refusedOutsideGround(ground, command); why != "" {
			t.Errorf("%q was refused with %q, want ordinary work allowed", command, why)
		}
	}
}

func TestASeparateGitFolderInAnotherCaseIsNotProtectedOnACaseSensitiveVolume(t *testing.T) {
	ground := groundWithCaseMetadata(t)
	other := filepath.Join(ground, ".GIT")
	if err := os.Mkdir(other, 0o755); err != nil {
		if !os.IsExist(err) {
			t.Fatal(err)
		}
		t.Skip("this disk ignores case, so .GIT cannot be a separate folder")
	}
	if realVolumeFoldsCase(t, ground) {
		t.Skip("the two git spellings have the same identity")
	}
	assertCaseMetadataAllowed(t, ground, []string{
		"rm -rf .GIT",
		"echo x > .GIT/HEAD",
		"rm -rf .GIT/objects",
		"echo x > .git/head",
		"cp -r /tmp/other/.GIT .",
		"cp -r /tmp/other/.GIT .git/hooks",
	})
	assertCaseMetadataRefusals(t, ground, []string{"rm -rf .git", "rm .git/HEAD"})
}

func TestACaseVariantOfTheGroundsGitIsRefusedOnTheRealVolume(t *testing.T) {
	ground := groundWithCaseMetadata(t)
	if !realVolumeFoldsCase(t, ground) {
		t.Skip("the test volume distinguishes .git from .GIT")
	}
	assertCaseMetadataRefusals(t, ground, []string{
		"rm -rf .GIT",
		"rm -rf .Git",
		"mv .Git x",
		"rm -rf .git/OBJECTS",
		"rm .git/head",
		"echo x > .git/head",
		"sed -i '' 's/bare/BARE/' .GIT/config",
		"cp payload/HEAD .GIT/HEAD",
		"cp -t .GIT payload/HEAD",
		"rm -rf .git/REFS",
		"rm .git/PACKED-REFS",
	})
	// The folder is reached as both `Repo` and `repo`, the way the issue
	// reproduced it, and the ground may be spelled either way.
	parent := filepath.Dir(ground)
	alternate := filepath.Join(parent, "repo")
	assertCaseMetadataRefusals(t, alternate, []string{
		"rm -rf .GIT",
		"rm -rf " + filepath.Join(alternate, ".GIT"),
		"rm -rf " + filepath.Join(ground, ".git"),
		"rm -rf " + filepath.Join(parent, "REPO"),
	})
	assertCaseMetadataAllowed(t, ground, []string{
		"rm -f .GIT/INDEX.LOCK",
		"touch .git/index.lock",
		"echo x > add.py",
		"cat .GIT/HEAD",
	})
}

func realVolumeFoldsCase(t *testing.T, ground string) bool {
	t.Helper()
	original, err := os.Lstat(filepath.Join(ground, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	variant, err := os.Lstat(filepath.Join(ground, ".GIT"))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(original, variant)
}

func TestFoldsCaseProbesTheMetadataAndFallsBackToTheGround(t *testing.T) {
	ground := groundWithCaseMetadata(t)
	metadata := filepath.Join(ground, ".git")
	emptyGround := groundWithMetadata(t)
	emptyMetadata := filepath.Join(emptyGround, ".git")

	folds, known := foldsCase(ground, ".git")
	want := realVolumeFoldsCase(t, ground)
	if !known || folds != want || metadataFoldsCase(metadata) != want {
		t.Errorf("real volume wanted folds %t, got folds %t, known %t, metadata folds %t", want, folds, known, metadataFoldsCase(metadata))
	}
	if got := metadataFoldsCase(emptyMetadata); got != realVolumeFoldsCase(t, emptyGround) {
		t.Errorf("empty metadata on the real volume returned %t", got)
	}

	useCaseInsensitiveLstat(t)
	folds, known = foldsCase(metadata, "HEAD")
	if !known || !folds || !metadataFoldsCase(metadata) {
		t.Errorf("fake volume wanted folding, got folds %t, known %t, metadata folds %t", folds, known, metadataFoldsCase(metadata))
	}
	if _, known := foldsCase(emptyMetadata, "HEAD"); known {
		t.Error("missing HEAD provided an answer")
	}
	if !metadataFoldsCase(emptyMetadata) {
		t.Error("empty metadata did not use the folding ground directory")
	}
}

// Two names of one file say nothing about case. A repository whose HEAD also
// answers to a hard link called `head` sits on a disk that tells the cases apart,
// so `.git/REFS` there is a different, absent entry and stays unprotected.
func TestAHardLinkedHeadDoesNotMakeACaseSensitiveDiskLookCaseInsensitive(t *testing.T) {
	ground := groundWithCaseMetadata(t)
	if realVolumeFoldsCase(t, ground) {
		t.Skip("this disk ignores case")
	}
	if err := os.Link(filepath.Join(ground, ".git", "HEAD"), filepath.Join(ground, ".git", "head")); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	assertCaseMetadataAllowed(t, ground, []string{"rm -rf .git/REFS", "rmdir .git/REFS", "rm .git/head"})
	assertCaseMetadataRefusals(t, ground, []string{"rm .git/HEAD", "rm -rf .git/refs"})
}

// A linked worktree's `.git` is a file, and a file can have other names. Removing
// or moving one of those leaves the pointer where it is, so only a spelling of
// `.git` itself is the metadata entry.
func TestAHardLinkToAWorktreePointerIsNotTheGroundsMetadataEntry(t *testing.T) {
	ground := groundWithMetadata(t)
	pointer := filepath.Join(ground, ".git")
	if err := os.RemoveAll(pointer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pointer, []byte("gitdir: /elsewhere/.git/worktrees/w\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(pointer, filepath.Join(ground, "pointer-link")); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	useCaseInsensitiveLstat(t)
	assertCaseMetadataAllowed(t, ground, []string{"unlink pointer-link", "rm pointer-link", "mv pointer-link moved-link"})
	assertCaseMetadataRefusals(t, ground, []string{"rm .git", "rm .GIT", "mv .Git x", "echo x > .GIT"})
}
