package taskcopy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestCapturePortableSnapshotPreservesReviewedRepresentation(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "shared.txt"), "original\n")
	run(t, source, "git", "init", "-q", "-b", "main")
	run(t, source, "git", "add", ".")
	run(t, source, "git", "commit", "-qm", "original")
	write(t, filepath.Join(source, ".gitattributes"), "shared.txt text eol=lf\n")
	write(t, filepath.Join(source, "shared.txt"), "reviewed\r\n")
	if err := os.Chmod(filepath.Join(source, "shared.txt"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "empty"), 0750); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	output := filepath.Join(root, "snapshot")
	state, err := CapturePortableSnapshot(source, filepath.Join(root, "carrier"), output)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(output, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "reviewed\r\n" {
		t.Fatalf("raw bytes changed: %q", raw)
	}
	for name, want := range map[string]os.FileMode{"shared.txt": 0640, "empty": 0750} {
		info, err := os.Stat(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode=%04o want%04o", name, info.Mode().Perm(), want)
		}
	}
	if err := VerifyPortableState(output, state); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(output, "shared.txt"), "tampered\n")
	if err := VerifyPortableState(output, state); err == nil {
		t.Fatal("modified reviewed representation accepted")
	}
}

func TestMaterializePortableInputPreservesSourceRepresentation(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "shared.txt"), "input\r\n")
	write(t, filepath.Join(source, "new-input.txt"), "supported input absent from checkout\n")
	if err := os.Chmod(filepath.Join(source, "shared.txt"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "empty"), 0750); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	write(t, filepath.Join(destination, "shared.txt"), "input\n")
	write(t, filepath.Join(destination, "deleted-in-source.txt"), "stale checkout bytes\n")
	run(t, destination, "git", "init", "-q", "-b", "main")
	run(t, destination, "git", "add", ".")
	run(t, destination, "git", "commit", "-qm", "input")
	baseline, err := InspectPortableState(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = MaterializePortableInput(source, destination, baseline); err != nil {
		t.Fatal(err)
	}
	if err = VerifyPortableState(destination, baseline); err != nil {
		t.Fatal(err)
	}
}

func TestPortableSnapshotCarriesImmutableCreationBaseline(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "shared.txt"), "original\n")
	run(t, source, "git", "init", "-q", "-b", "main")
	run(t, source, "git", "add", ".")
	run(t, source, "git", "commit", "-qm", "original")
	baseline, err := InspectPortableState(source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := WritePortableBaseline(source, baseline)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, "shared.txt"), "reviewed\n")
	root := t.TempDir()
	output := filepath.Join(root, "snapshot")
	if _, err = CapturePortableSnapshot(source, filepath.Join(root, "carrier"), output); err != nil {
		t.Fatal(err)
	}
	restored, got, err := ReadPortableBaseline(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != digest || restored.Entries["shared.txt"] != baseline.Entries["shared.txt"] {
		t.Fatal("historical creation baseline changed in portable snapshot")
	}
	changed, err := InspectPortableState(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = WritePortableBaseline(source, changed); err == nil {
		t.Fatal("creation baseline was overwritten")
	}
}

func TestInspectPortableStateScopesIgnoredDependencies(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "cache/\n")
	write(t, filepath.Join(root, "input.txt"), "input\n")
	run(t, root, "git", "init", "-q", "-b", "main")
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-qm", "input")
	write(t, filepath.Join(root, "cache", "collision.txt"), "unrelated cache\n")
	state, err := InspectPortableState(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Entries["cache/collision.txt"]; ok {
		t.Fatal("ignored dependency was included in portable census")
	}
	selected, err := InspectPortablePaths(root, []string{"cache/collision.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Entries["cache/collision.txt"].Kind != "file" {
		t.Fatal("explicit ignored destination collision was hidden")
	}
}

func TestInspectPortableStateDoesNotRunGlobalFSMonitor(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "input.txt"), "input\n")
	settings := t.TempDir()
	marker := filepath.Join(settings, "fsmonitor-ran")
	hook := filepath.Join(settings, "fsmonitor-hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf ran > '"+marker+"'\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(settings, "global.gitconfig")
	run(t, settings, "git", "config", "--file", config, "core.fsmonitor", hook)
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	if _, err := InspectPortableState(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("synthetic portable index executed user's global fsmonitor")
	}
}

func TestWorkspaceCarrierPreservesAndVerifiesCreationBaseline(t *testing.T) {
	c, plan := workspaceFixture(t)
	tree := filepath.Join(liveRoot(c), "7", "main")
	baseline, err := InspectPortableState(tree)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := WritePortableBaseline(tree, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err = (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := cell.Cell{ID: "receiver", Root: filepath.Join(t.TempDir(), "chat")}
	if err = os.MkdirAll(filepath.Join(receiver.Root, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	run(t, c.Root, "cp", "-R", workspaceCarrierRoot(c), workspaceCarrierRoot(receiver))
	writeWorkspaceCheckpoint(t, receiver, plan)
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepared.Promote(); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(liveRoot(receiver), "7", "main")
	_, got, err := ReadPortableBaseline(restored)
	if err != nil || got != digest {
		t.Fatalf("creation baseline lost: %s %v", got, err)
	}
	if err = os.WriteFile(filepath.Join(restored, PortableBaselinePath), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = prepared.Acknowledge(); err == nil {
		t.Fatal("tampered historical metadata admitted after portable restore")
	}
}

func TestInspectPortableStateIgnoresInheritedGitRouting(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_CONFIG"} {
		t.Run(name, func(t *testing.T) {
			foreign := t.TempDir()
			write(t, filepath.Join(foreign, "foreign.txt"), "foreign\n")
			run(t, foreign, "git", "init", "-q", "-b", "main")
			run(t, foreign, "git", "add", ".")
			run(t, foreign, "git", "commit", "-qm", "foreign")
			run(t, foreign, "git", "config", "core.fsmonitor", "original-monitor")
			config := filepath.Join(foreign, ".git", "config")
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			write(t, filepath.Join(target, "input.txt"), "intended input\n")
			value := filepath.Join(foreign, ".git")
			if name == "GIT_CONFIG" {
				value = config
			}
			t.Setenv(name, value)
			state, err := InspectPortableState(target)
			if err != nil {
				t.Fatal(err)
			}
			if state.Entries["input.txt"].Kind != "file" {
				t.Fatal("inherited routing hid intended source input")
			}
			after, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("read-only portable inspection changed a foreign Git config")
			}
		})
	}
}

func TestPortableSnapshotRecordsOnlyExplicitlyWithheldOmissions(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "input.txt"), "input\n")
	run(t, source, "git", "init", "-q", "-b", "main")
	run(t, source, "git", "add", ".")
	run(t, source, "git", "commit", "-qm", "input")
	write(t, filepath.Join(source, ".env"), "FIXTURE_VALUE=private-fixture-only\n")
	root := t.TempDir()
	output := filepath.Join(root, "snapshot")
	state, err := CapturePortableSnapshot(source, filepath.Join(root, "carrier"), output)
	if err != nil {
		t.Fatal(err)
	}
	if state.Withheld[".env"].Kind != "file" {
		t.Fatal("omission has no bounded withholding disposition")
	}
	if _, exists := state.Entries[".env"]; exists {
		t.Fatal("withheld payload represented as applied output")
	}
	if _, err = os.Stat(filepath.Join(output, ".env")); !os.IsNotExist(err) {
		t.Fatal("withheld payload copied into portable snapshot")
	}
	if err = VerifyPortableState(output, state); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(source, ".env")); err != nil || string(got) != "FIXTURE_VALUE=private-fixture-only\n" {
		t.Fatal("original private input changed")
	}
}

func TestWorkspaceCarrierRestoresChangedAndNewDanglingLinks(t *testing.T) {
	c, plan := workspaceFixture(t)
	tree := filepath.Join(liveRoot(c), "7", "main")
	if err := os.Symlink("tracked.txt", filepath.Join(tree, "pointer")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tracked.txt", filepath.Join(tree, "clean-link")); err != nil {
		t.Fatal(err)
	}
	run(t, tree, "git", "add", "pointer", "clean-link")
	run(t, tree, "git", "commit", "-qm", "link input")
	if err := os.Remove(filepath.Join(tree, "pointer")); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"pointer": "missing existing target", "new-link": "../missing new target"} {
		if err := os.Symlink(target, filepath.Join(tree, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	if export := os.Getenv("CODEAF_TEST_EXPORT_LINK_CARRIER"); export != "" {
		exported := cell.Cell{ID: "receiver", Root: filepath.Join(export, "chat")}
		if err := os.MkdirAll(filepath.Join(exported.Root, cell.StateDir), 0700); err != nil {
			t.Fatal(err)
		}
		run(t, c.Root, "cp", "-R", workspaceCarrierRoot(c), workspaceCarrierRoot(exported))
		writeWorkspaceCheckpoint(t, exported, plan)
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(export, "plan.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	receiver := cell.Cell{ID: "receiver", Root: filepath.Join(t.TempDir(), "chat")}
	if err := os.MkdirAll(filepath.Join(receiver.Root, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	run(t, c.Root, "cp", "-R", workspaceCarrierRoot(c), workspaceCarrierRoot(receiver))
	writeWorkspaceCheckpoint(t, receiver, plan)
	manifestPath := filepath.Join(workspaceCarrierRoot(receiver), "manifest.json")
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, unsupported := range []string{"1", "99"} {
		altered := strings.Replace(string(manifestRaw), `"directory_version":2`, `"directory_version":`+unsupported, 1)
		if altered == string(manifestRaw) {
			t.Fatal("typed-link carrier omitted mandatory capability version")
		}
		if err = os.WriteFile(manifestPath, []byte(altered), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = (Carry{}).PrepareWorkspace(receiver, plan); err == nil {
			t.Fatalf("unsupported typed-link version %s admitted", unsupported)
		}
		if _, err = os.Stat(filepath.Join(liveRoot(receiver), "7", "main")); !os.IsNotExist(err) {
			t.Fatal("unsupported carrier was promoted")
		}
	}
	if err = os.WriteFile(manifestPath, manifestRaw, 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepared.Promote(); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(liveRoot(receiver), "7", "main")
	for name, target := range map[string]string{"pointer": "missing existing target", "new-link": "../missing new target", "clean-link": "tracked.txt"} {
		got, err := os.Readlink(filepath.Join(restored, name))
		if err != nil || got != target {
			t.Fatalf("link %s got %q want %q: %v", name, got, target, err)
		}
	}
	if err = prepared.Acknowledge(); err != nil {
		t.Fatal(err)
	}
}

// This external-fixture test runs the exact Linux-produced carrier on Darwin.
func TestWorkspaceCarrierImportsLinuxLinkFixture(t *testing.T) {
	fixture := os.Getenv("CODEAF_TEST_IMPORT_LINK_CARRIER")
	if fixture == "" {
		t.Skip("external Linux-produced carrier not supplied")
	}
	raw, err := os.ReadFile(filepath.Join(fixture, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan WorkspacePlan
	if err = json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	receiver := cell.Cell{ID: "receiver", Root: filepath.Join(fixture, "chat")}
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepared.Promote(); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(liveRoot(receiver), "7", "main")
	for name, target := range map[string]string{"pointer": "missing existing target", "new-link": "../missing new target", "clean-link": "tracked.txt"} {
		got, err := os.Readlink(filepath.Join(restored, name))
		if err != nil || got != target {
			t.Fatalf("imported link %s=%q: %v", name, got, err)
		}
		info, err := os.Lstat(filepath.Join(restored, name))
		if err != nil || info.Mode().Perm() != 0777 {
			t.Fatalf("Linux link mode lost: %v %v", info, err)
		}
	}
	if err = prepared.Acknowledge(); err != nil {
		t.Fatal(err)
	}
}
