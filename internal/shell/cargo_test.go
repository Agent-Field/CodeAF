package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCargoDiscoveryUsesInstalledToolchainWithoutChangingExplicitSelection(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	for _, mode := range []string{"default home", "custom cargo home", "explicit path wins", "missing installation", "custom home is authoritative"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home with spaces")
			install := func(dir, result string) {
				t.Helper()
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "cargo"), []byte("#!/bin/sh\necho "+result+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			cargoHome, path, want := "", "/usr/bin:/bin", "default"
			if mode != "missing installation" {
				install(filepath.Join(home, ".cargo", "bin"), "default")
			}
			switch mode {
			case "custom cargo home":
				cargoHome = filepath.Join(root, "custom toolchain")
				install(filepath.Join(cargoHome, "bin"), "custom")
				want = "custom"
			case "explicit path wins":
				dir := filepath.Join(root, "chosen")
				install(dir, "chosen")
				path, want = dir+":"+path, "chosen"
			case "custom home is authoritative":
				cargoHome = filepath.Join(root, "missing custom home")
			}
			c := exec.Command(bash, "-c", CargoPathSetup()+"command cargo --version")
			c.Env = append(os.Environ(), "HOME="+home, "CARGO_HOME="+cargoHome, "PATH="+path)
			out, err := c.CombinedOutput()
			if mode == "missing installation" || mode == "custom home is authoritative" {
				if err == nil {
					t.Fatal("uninstalled toolchain unexpectedly ran")
				}
				return
			}
			if err != nil || strings.TrimSpace(string(out)) != want {
				t.Fatalf("discovery output=%q, error=%v, want=%q", out, err, want)
			}
		})
	}
}
