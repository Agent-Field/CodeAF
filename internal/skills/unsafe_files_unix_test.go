//go:build unix

package skills

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRegularOpenerRefusesNonRegularTargets(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(fifo, filepath.Join(root, "linked-pipe")); err != nil {
		t.Fatal(err)
	}
	paths := []string{fifo, filepath.Join(root, "linked-pipe"), root}
	if _, err := os.Stat("/dev/zero"); err == nil {
		paths = append(paths, "/dev/zero")
	}
	if listener, err := net.Listen("unix", filepath.Join(root, "socket")); err == nil {
		defer listener.Close()
		paths = append(paths, filepath.Join(root, "socket"))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			answer := make(chan error, 1)
			go func() { _, err := OpenRegular(path); answer <- err }()
			select {
			case err := <-answer:
				if !errors.Is(err, ErrNotRegular) {
					t.Fatalf("OpenRegular(%q) = %v", path, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("OpenRegular blocked on a non-regular file")
			}
		})
	}
	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("ordinary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(regular, filepath.Join(root, "linked-regular")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{regular, filepath.Join(root, "linked-regular")} {
		data, err := ReadWholeRegular(path, 8)
		if err != nil || string(data) != "ordinary" {
			t.Fatalf("ReadWholeRegular(%q) = %q, %v", path, data, err)
		}
		if _, err := ReadWholeRegular(path, 7); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("over cap = %v", err)
		}
		data, err = ReadRegularHead(path, 3)
		if err != nil || string(data) != "ord" {
			t.Fatalf("head = %q, %v", data, err)
		}
	}
}

func TestDiscoverReadsLinkedOrdinarySkill(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	source := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(source, []byte("---\nname: linked\ndescription: linked ordinary file\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(project, ".agents", "skills", "linked")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(folder, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	found := discoverBeforeDeadline(t, project, home)
	if skill, ok := byDir(found, filepath.Join("skills", "linked")); !ok || skill.Name != "linked" {
		t.Fatalf("linked regular SKILL.md was not loaded: %+v", found)
	}
}

func TestOversizedPluginJSONIsAbsent(t *testing.T) {
	root := pluginFixture(t)
	project, home := filepath.Join(root, "elsewhere"), filepath.Join(root, "home")
	registry := filepath.Join(home, installedPluginsFile)
	large := []byte(`{"plugins":{},"padding":"` + strings.Repeat("x", maxPluginJSONBytes) + `"}`)
	if err := os.WriteFile(registry, large, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(byName(discoverBeforeDeadline(t, project, home), "tidy-commits")) != 0 {
		t.Fatal("oversized registry was read")
	}
	if _, err := ReadWholeRegular(registry, maxPluginJSONBytes); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized JSON = %v", err)
	}
}

func TestDiscoverSkipsUnsafePluginFiles(t *testing.T) {
	for _, name := range []string{
		"home/.claude/settings.json",
		"home/.claude/plugins/installed_plugins.json",
		"home/.claude/plugins/known_marketplaces.json",
		"home/.claude/plugins/cache/market/tidy/1.0.0/.claude-plugin/plugin.json",
		"market-clone/split/.claude-plugin/marketplace.json",
	} {
		t.Run(name, func(t *testing.T) {
			root := pluginFixture(t)
			path := filepath.Join(root, name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Skip(err)
			}
			_ = discoverBeforeDeadline(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
		})
	}
}

func TestOversizedSettingsCannotOverrideEnabledPluginMap(t *testing.T) {
	root := pluginFixture(t)
	project, home := filepath.Join(root, "elsewhere"), filepath.Join(root, "home")
	path := filepath.Join(project, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"enabledPlugins":{"tidy@market":false},"padding":"` + strings.Repeat("x", maxPluginJSONBytes) + `"}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(byName(discoverBeforeDeadline(t, project, home), "tidy-commits")) != 1 {
		t.Fatal("oversized settings overrode the home enabled plugin")
	}
}

func discoverBeforeDeadline(t *testing.T, project, home string) []Skill {
	t.Helper()
	answer := make(chan []Skill, 1)
	go func() {
		found, _ := Discover(Options{ProjectDir: project, HomeDir: home})
		answer <- found
	}()
	select {
	case found := <-answer:
		return found
	case <-time.After(2 * time.Second):
		t.Fatal("Discover blocked for two seconds reading a non-regular file")
		return nil
	}
}

func TestDiscoverSkipsPipesAndDevicesWithAReason(t *testing.T) {
	for _, kind := range []string{"fifo", "linked fifo", "device"} {
		t.Run(kind, func(t *testing.T) {
			project, home := t.TempDir(), t.TempDir()
			folder := filepath.Join(project, ".agents", "skills", "unsafe")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(folder, "SKILL.md")
			switch kind {
			case "fifo":
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Skip(err)
				}
			case "linked fifo":
				target := filepath.Join(t.TempDir(), "pipe")
				if err := syscall.Mkfifo(target, 0o600); err != nil {
					t.Skip(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "device":
				if _, err := os.Stat("/dev/zero"); err != nil {
					t.Skip(err)
				}
				if err := os.Symlink("/dev/zero", path); err != nil {
					t.Fatal(err)
				}
			}
			found := discoverBeforeDeadline(t, project, home)
			skill, ok := byDir(found, filepath.Join("skills", "unsafe"))
			if !ok || !strings.Contains(skill.Warning, "not a regular file") || skill.Name != "" {
				t.Fatalf("unsafe skill should be skipped with a reason: %+v", found)
			}
		})
	}
}

func TestDiscoverSkipsNonRegularClaudeSettings(t *testing.T) {
	for _, name := range []string{"settings.json", "settings.local.json"} {
		for _, kind := range []string{"fifo", "device"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				root := pluginFixture(t)
				project := filepath.Join(root, "elsewhere")
				if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(project, ".claude", name)
				if kind == "fifo" {
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						t.Skip(err)
					}
				} else {
					if _, err := os.Stat("/dev/zero"); err != nil {
						t.Skip(err)
					}
					if err := os.Symlink("/dev/zero", path); err != nil {
						t.Fatal(err)
					}
				}
				found := discoverBeforeDeadline(t, project, filepath.Join(root, "home"))
				if len(byName(found, "tidy-commits")) != 1 {
					t.Fatalf("unsafe project settings should be absent, preserving the home setting: %+v", found)
				}
			})
		}
	}
}
