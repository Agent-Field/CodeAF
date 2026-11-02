package taskcopy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

func TestPlainBaselineSecretsRefusePortableComposition(t *testing.T) {
	for _, sample := range []struct {
		name, path, body string
		removed          bool
	}{
		{"dotenv", ".env", "MOCK_TOKEN=synthetic-only\n", false},
		{"private-key", "ordinary.txt", "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n-----END PRIVATE KEY-----\n", false},
		{"large-tail-key", "ordinary.dat", strings.Repeat("safe bytes\n", 200000) + "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n", false},
		{"historical-key", "removed.txt", "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n-----END PRIVATE KEY-----\n", true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			source := unsealedPlainMirrorFixture(t)
			tree := filepath.Join(liveRoot(source.cell), "1")
			path := filepath.Join(tree, sample.path)
			write(t, path, sample.body)
			run(t, tree, "git", "add", "--force", sample.path)
			run(t, tree, "git", "commit", "-qm", "synthetic baseline secret")
			if sample.removed {
				run(t, tree, "git", "rm", "-q", sample.path)
				run(t, tree, "git", "commit", "-qm", "remove synthetic secret")
			}
			before := run(t, tree, "git", "rev-parse", "HEAD")
			err := (Carry{}).ComposeWorkspace(source.cell, source.project)
			if err == nil {
				receiver := newMachineNoRepo(t)
				source.moveTo(receiver)
				_, restoreErr := restorer.Restore(receiver.cell, receiver.project)
				t.Fatalf("portable composition admitted secret-bearing full history (restore error %v)", restoreErr)
			}
			if !strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected actionable secret refusal, got %v", err)
			}
			if after := run(t, tree, "git", "rev-parse", "HEAD"); after != before {
				t.Fatal("refusal changed local history")
			}
			if sample.removed {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("removed file changed: %v", err)
				}
			} else if got, err := os.ReadFile(path); err != nil || string(got) != sample.body {
				t.Fatalf("local secret changed: %v", err)
			}
		})
	}
}

func TestColdLegacyFullHistorySecretsRefuseComposition(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "unavailable"))
	source := unsealedPlainMirrorFixture(t)
	tree := filepath.Join(liveRoot(source.cell), "1")
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(tree, "old.txt"), "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n")
	run(t, tree, "git", "add", "old.txt")
	run(t, tree, "git", "commit", "-qm", "legacy unsafe baseline")
	dest := filepath.Join(carriedRoot(source.cell), "1")
	run(t, tree, "git", "bundle", "create", filepath.Join(dest, bundleName), "HEAD")
	rec, err := readRecord(dest)
	if err != nil {
		t.Fatal(err)
	}
	rec.Head = run(t, tree, "git", "rev-parse", "HEAD")
	if err := writeRecord(dest, rec); err != nil {
		t.Fatal(err)
	}
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil {
		t.Fatal("cached live full history bypassed secret screening")
	}
	manager, _ := taskstorage.Prototype(liveRoot(source.cell))
	manager.Vault = &coldVault{}
	manager.Busy = func(string) bool { return false }
	manager.BeforeRetire = func(_ context.Context, snapshot, path string) error {
		return (Carry{}).MarkCold(source.cell, path, snapshot)
	}
	if err := manager.Park(context.Background(), tree); err != nil {
		t.Fatal(err)
	}
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil {
		t.Fatal("cached cold full history bypassed secret screening")
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("screening expanded cold copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, bundleName)); err != nil {
		t.Fatalf("refusal deleted legacy artifact: %v", err)
	}
}

func TestPortableFullHistoryScreensCommitMessages(t *testing.T) {
	source := unsealedPlainMirrorFixture(t)
	tree := filepath.Join(liveRoot(source.cell), "1")
	run(t, tree, "git", "commit", "--allow-empty", "-qm", "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n")
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil {
		t.Fatal("full history exported secret marker in commit message")
	}
}

func TestSafeFullHistoryPreservesTemplatesAndPriorArtifact(t *testing.T) {
	source := unsealedPlainMirrorFixture(t)
	tree := filepath.Join(liveRoot(source.cell), "1")
	write(t, filepath.Join(tree, ".env.example"), "MOCK_TOKEN=replace-me\n")
	run(t, tree, "git", "add", ".env.example")
	run(t, tree, "git", "commit", "-qm", "safe template")
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(carriedRoot(source.cell), "1", bundleName)
	before, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(tree, "secret.txt"), "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n")
	run(t, tree, "git", "add", "secret.txt")
	run(t, tree, "git", "commit", "-qm", "unsafe update")
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil {
		t.Fatal("unsafe update admitted")
	}
	after, err := os.ReadFile(artifact)
	if err != nil || string(before) != string(after) {
		t.Fatalf("refused update lost previous safe artifact: %v", err)
	}
	receiver := newMachineNoRepo(t)
	source.moveTo(receiver)
	if _, err := restorer.Restore(receiver.cell, receiver.project); err != nil {
		t.Fatal(err)
	}
	if body, ok := receiver.read("1", ".env.example"); !ok || body != "MOCK_TOKEN=replace-me\n" {
		t.Fatal("safe template did not restore exactly")
	}
}

func TestCachedFullHistoryScreensBlobTagsLiveAndCold(t *testing.T) {
	for _, cold := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "cold"}[cold], func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
			t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "unavailable"))
			source := unsealedPlainMirrorFixture(t)
			tree := filepath.Join(liveRoot(source.cell), "1")
			if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "marker.txt")
			write(t, marker, "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n")
			blob := run(t, tree, "git", "hash-object", "-w", marker)
			run(t, tree, "git", "tag", "unreferenced-secret", blob)
			artifact := filepath.Join(carriedRoot(source.cell), "1", bundleName)
			run(t, tree, "git", "bundle", "create", artifact, "HEAD", "refs/tags/unreferenced-secret")
			before, err := os.ReadFile(artifact)
			if err != nil {
				t.Fatal(err)
			}
			if cold {
				manager, _ := taskstorage.Prototype(liveRoot(source.cell))
				manager.Vault = &coldVault{}
				manager.Busy = func(string) bool { return false }
				manager.BeforeRetire = func(_ context.Context, snapshot, path string) error {
					return (Carry{}).MarkCold(source.cell, path, snapshot)
				}
				if err := manager.Park(context.Background(), tree); err != nil {
					t.Fatal(err)
				}
			}
			if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil {
				t.Fatal("tag-only secret blob escaped full-bundle screening")
			}
			after, err := os.ReadFile(artifact)
			if err != nil || string(before) != string(after) {
				t.Fatalf("screening changed cached bundle: %v", err)
			}
		})
	}
}

func TestCachedFullHistoryScreensReferenceNames(t *testing.T) {
	source := unsealedPlainMirrorFixture(t)
	tree := filepath.Join(liveRoot(source.cell), "1")
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
		t.Fatal(err)
	}
	tag := "ghp_" + strings.Repeat("A", 32)
	run(t, tree, "git", "tag", tag)
	artifact := filepath.Join(carriedRoot(source.cell), "1", bundleName)
	run(t, tree, "git", "bundle", "create", artifact, "HEAD", "refs/tags/"+tag)
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil {
		t.Fatal("secret-bearing reference name bypassed screening")
	}
}

func TestCompositionRefusesTruncatedHistoryRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fault injection")
	}
	source := unsealedPlainMirrorFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nif [ \"$3\" = cat-file ] && [ \"$4\" = --batch ]; then\n printf '%040d blob 100\\nshort' 0\n exit 0\nfi\nexec " + quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("incomplete object judged clean: %v", err)
	}
	tree := filepath.Join(liveRoot(source.cell), "1")
	if got, err := os.ReadFile(filepath.Join(tree, "README.md")); err != nil || string(got) != "plain project input\n" {
		t.Fatalf("refusal changed source: %v", err)
	}
}

func TestCachedFullHistoryRejectsNoncanonicalContainer(t *testing.T) {
	token := "ghp_" + strings.Repeat("B", 32)
	for _, kind := range []string{"custom-reference", "malformed-reference", "trailing-bytes", "duplicate-head", "prerequisite"} {
		for _, cold := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cold=%v", kind, cold), func(t *testing.T) {
				t.Setenv("CODEAF_HOME", t.TempDir())
				t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
				t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "unavailable"))
				source := unsealedPlainMirrorFixture(t)
				tree := filepath.Join(liveRoot(source.cell), "1")
				if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
					t.Fatal(err)
				}
				artifact := filepath.Join(carriedRoot(source.cell), "1", bundleName)
				raw, err := os.ReadFile(artifact)
				if err != nil {
					t.Fatal(err)
				}
				header, pack, ok := bytes.Cut(raw, []byte("\n\n"))
				if !ok {
					t.Fatal("fixture bundle lacks header")
				}
				header = append([]byte(nil), header...)
				pack = append([]byte(nil), pack...)
				head := run(t, tree, "git", "rev-parse", "HEAD")
				switch kind {
				case "custom-reference":
					header = append(header, []byte("\n"+head+" refs/custom/"+token)...)
				case "malformed-reference":
					header = append(header, []byte("\n"+head+" refs/heads/bad.."+token)...)
				case "trailing-bytes":
					pack = append(pack, []byte(token)...)
				case "duplicate-head":
					header = append(header, []byte("\n"+head+" HEAD")...)
				case "prerequisite":
					header = append(header, []byte("\n-"+head+" "+token)...)
				}
				wire := append(append(append([]byte(nil), header...), []byte("\n\n")...), pack...)
				if err := os.WriteFile(artifact, wire, 0600); err != nil {
					t.Fatal(err)
				}
				if cold {
					manager, _ := taskstorage.Prototype(liveRoot(source.cell))
					manager.Vault = &coldVault{}
					manager.Busy = func(string) bool { return false }
					manager.BeforeRetire = func(_ context.Context, snapshot, path string) error {
						return (Carry{}).MarkCold(source.cell, path, snapshot)
					}
					if err := manager.Park(context.Background(), tree); err != nil {
						t.Fatal(err)
					}
				}
				err = (Carry{}).ComposeWorkspace(source.cell, source.project)
				if err == nil {
					t.Fatal("noncanonical container accepted")
				}
				if strings.Contains(err.Error(), token) {
					t.Fatal("refusal echoed secret header or pack bytes")
				}
				after, err := os.ReadFile(artifact)
				if err != nil || !bytes.Equal(wire, after) {
					t.Fatalf("refusal changed original container: %v", err)
				}
			})
		}
	}
}

func TestCanonicalFullHistoryScreensUnreferencedPackBlob(t *testing.T) {
	for _, cold := range []bool{false, true} {
		t.Run(fmt.Sprintf("cold=%v", cold), func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
			t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "unavailable"))
			source := unsealedPlainMirrorFixture(t)
			tree := filepath.Join(liveRoot(source.cell), "1")
			if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "marker.txt")
			write(t, marker, "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-ONLY\n")
			blob := run(t, tree, "git", "hash-object", "-w", marker)
			var objects strings.Builder
			for _, entry := range strings.Split(run(t, tree, "git", "rev-list", "--objects", "HEAD"), "\n") {
				objects.WriteString(strings.Fields(entry)[0] + "\n")
			}
			objects.WriteString(blob + "\n")
			command := exec.Command("git", "-C", tree, "pack-objects", "--stdout")
			command.Stdin = strings.NewReader(objects.String())
			pack, err := command.Output()
			if err != nil {
				t.Fatal("cannot create mock pack")
			}
			artifact := filepath.Join(carriedRoot(source.cell), "1", bundleName)
			raw, err := os.ReadFile(artifact)
			if err != nil {
				t.Fatal(err)
			}
			header, _, ok := bytes.Cut(raw, []byte("\n\n"))
			if !ok {
				t.Fatal("fixture header missing")
			}
			wire := append(append(append([]byte(nil), header...), []byte("\n\n")...), pack...)
			if err := os.WriteFile(artifact, wire, 0600); err != nil {
				t.Fatal(err)
			}
			if cold {
				manager, _ := taskstorage.Prototype(liveRoot(source.cell))
				manager.Vault = &coldVault{}
				manager.Busy = func(string) bool { return false }
				manager.BeforeRetire = func(_ context.Context, snapshot, path string) error {
					return (Carry{}).MarkCold(source.cell, path, snapshot)
				}
				if err := manager.Park(context.Background(), tree); err != nil {
					t.Fatal(err)
				}
			}
			if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err == nil || !strings.Contains(err.Error(), "secret material") {
				t.Fatalf("unreferenced pack blob escaped object scan: %v", err)
			}
			after, err := os.ReadFile(artifact)
			if err != nil || !bytes.Equal(wire, after) {
				t.Fatalf("screening modified original bundle: %v", err)
			}
		})
	}
}

func TestFullHistoryCompositionAcceptsCanonicalObjectFormats(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			source := newMachineNoRepo(t)
			tree := filepath.Join(liveRoot(source.cell), "1")
			write(t, filepath.Join(tree, "safe.txt"), "mock safe bytes\n")
			command := exec.Command("git", "-C", tree, "init", "-q", "--object-format="+format)
			if err := command.Run(); err != nil {
				t.Skip("installed Git lacks requested object format")
			}
			run(t, tree, "git", "add", ".")
			run(t, tree, "git", "commit", "-qm", "mock baseline")
			for range 2 {
				if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestHistoryInspectionNeverEchoesGitDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fault injection")
	}
	source := unsealedPlainMirrorFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	token := "ghp_" + strings.Repeat("C", 32)
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nif [ \"$3\" = index-pack ]; then\n printf '%s\\n' " + quote(token) + " >&2\n exit 1\nfi\nexec " + quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	err = (Carry{}).ComposeWorkspace(source.cell, source.project)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatal("inspection diagnostics were accepted or echoed")
	}
}
