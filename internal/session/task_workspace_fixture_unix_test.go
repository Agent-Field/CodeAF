//go:build linux || darwin

package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// macOS inherits directory groups and can attach provenance to otherwise plain fixture files.
func normalizeWorkspaceApplyFixtureMetadata(t *testing.T, path string) {
	t.Helper()
	if err := os.Lchown(path, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("set Apply fixture ownership for %s: %v", path, err)
	}
	for _, name := range workspaceApplyFixtureAttributes(t, path) {
		if err := unix.Lremovexattr(path, name); err != nil {
			t.Fatalf("remove extended attribute %q from Apply fixture %s: %v", name, path, err)
		}
	}
	if names := workspaceApplyFixtureAttributes(t, path); len(names) != 0 {
		t.Fatalf("extended attribute %q remains on Apply fixture %s after removal", names[0], path)
	}
}

func workspaceApplyFixtureAttributes(t *testing.T, path string) []string {
	t.Helper()
	size, err := unix.Llistxattr(path, nil)
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		t.Fatalf("list extended attributes on Apply fixture %s: %v", path, err)
	}
	if size == 0 {
		return nil
	}
	buffer := make([]byte, size)
	n, err := unix.Llistxattr(path, buffer)
	if err != nil {
		t.Fatalf("read extended attributes on Apply fixture %s: %v", path, err)
	}
	return strings.Split(strings.TrimSuffix(string(buffer[:n]), "\x00"), "\x00")
}

func TestTaskWorkspaceReviewRefusesInheritedGroupUntilFixtureIsNormalized(t *testing.T) {
	// SPEC-4 pins macOS directory-group inheritance on Linux with a setgid fixture.
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	secondary := -1
	for _, group := range groups {
		if group != os.Getgid() {
			secondary = group
			break
		}
	}
	if secondary < 0 {
		t.Skip("no supplementary group differs from the process primary group for the setgid inheritance fixture")
	}
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, false)
	root := workspaceApplyFixtureDir(t)
	if err := os.Chown(root, os.Getuid(), secondary); err != nil {
		t.Fatalf("set inherited-group fixture to supplementary group %d: %v", secondary, err)
	}
	if err := os.Chmod(root, 0750|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inherited.txt")
	writeFile(t, path, "inherited group\n")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if stat := info.Sys().(*syscall.Stat_t); stat.Gid != uint32(secondary) {
		t.Fatalf("setgid fixture file group = %d, want inherited group %d", stat.Gid, secondary)
	}
	private := filepath.Join(copyDir, "inherited.txt")
	if err := os.Rename(path, private); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ReviewTaskWorkspaceRoot(t.Context(), id, "work"); err == nil || !strings.Contains(err.Error(), "portable apply cannot preserve a different owner or group") {
		t.Fatalf("Review did not refuse inherited file ownership: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "inherited.txt")); !os.IsNotExist(err) {
		t.Fatalf("refused Review changed the original: %v", err)
	}
	prepareWorkspaceApplyFixture(t, private)
	if _, err := agent.ReviewTaskWorkspaceRoot(t.Context(), id, "work"); err != nil {
		t.Fatalf("normalized fixture still refused Review: %v", err)
	}
}

func TestTaskWorkspaceReviewAcceptsFixtureAfterRemovingExtendedAttributes(t *testing.T) {
	// macOS provenance must be removed from success fixtures without relaxing Review's metadata refusal.
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, false)
	attribute := "user.codeaf.fixture"
	if runtime.GOOS == "darwin" {
		attribute = "com.codeaf.fixture"
	}
	for _, root := range []string{source, copyDir} {
		if err := unix.Lsetxattr(filepath.Join(root, "shared.txt"), attribute, []byte("fixture metadata"), 0); err != nil {
			t.Fatalf("set extended attribute %q on fixture %s: %v", attribute, root, err)
		}
	}
	if _, err := agent.ReviewTaskWorkspaceRoot(t.Context(), id, "work"); err == nil || !strings.Contains(err.Error(), "portable apply cannot preserve extended attributes or access-control metadata") {
		t.Fatalf("Review did not refuse fixture attributes: %v", err)
	}
	prepareWorkspaceApplyFixture(t, source, copyDir)
	if _, err := agent.ReviewTaskWorkspaceRoot(t.Context(), id, "work"); err != nil {
		t.Fatalf("metadata-free fixture still refused Review: %v", err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(t.Context(), id, "work"); err != nil {
		t.Fatalf("metadata-free fixture still refused Apply: %v", err)
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "private result\n" {
		t.Fatalf("metadata-free Apply changed reviewed bytes: %q", got)
	}
}
