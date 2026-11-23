package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestOmitClearsStaleEngines checks the explicit -omit path for platforms
// without a native engine, retaining the README needed by go:embed.
func TestOmitClearsStaleEngines(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"furrow-linux-amd64.gz", "furrow-darwin-arm64.gz", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearStaged(dir, ""); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "README.md" {
		t.Fatalf("omit retained staged engines: %v", files)
	}
}

// TestW1WindowsSidecarFetchesBeforeBuilding checks W1 at the make target: the
// Windows sidecar fetches its own engine, and a fetch refusal stops the build
// with that message. A fake toolchain keeps this packaging check offline.
func TestW1WindowsSidecarFetchesBeforeBuilding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the cross-build recipe runs on a Unix build host")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("the sidecar packaging check needs make")
	}
	makefile, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, failFetch := range []bool{false, true} {
		t.Run(map[bool]string{false: "pinned-fetch", true: "fetch-refusal"}[failFetch], func(t *testing.T) {
			root := t.TempDir()
			log := filepath.Join(root, "calls")
			tool := filepath.Join(root, "go")
			script := `#!/bin/sh
if [ "$1" = env ]; then
  if [ "$2" = GOOS ]; then printf '%s\n' linux; else printf '%s\n' amd64; fi
  exit 0
fi
printf '%s %s/%s\n' "$*" "$GOOS" "$GOARCH" >> "$CODEAF_TEST_SIDECAR_LOG"
if [ "$1" = run ] && [ "$CODEAF_TEST_FETCH_FAIL" = 1 ]; then
  printf '%s\n' 'fixture: pinned Windows furrow download failed' >&2
  exit 23
fi
`
			if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(makePath, "--no-print-directory", "-f", makefile,
				"-o", "embed", "-o", tool, "app-sidecar", "APP_GOOS=windows", "APP_GOARCH=amd64",
				"APP_DIST="+filepath.Join(root, "dist"), "GARBLE="+tool,
				"FURROW_GOOS=linux", "FURROW_GOARCH=amd64", "BUILD_REV=fixture", "BUILD_DIRTY=false", "BUILD_AT=fixture")
			command.Dir = root
			failed := "0"
			if failFetch {
				failed = "1"
			}
			command.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
				"CODEAF_TEST_SIDECAR_LOG="+log, "CODEAF_TEST_FETCH_FAIL="+failed,
				"MAKEFLAGS=", "GOOS=", "GOARCH=")
			output, buildErr := command.CombinedOutput()
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatalf("W1 no toolchain calls: %v: %s", err, output)
			}
			lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
			if !strings.HasPrefix(lines[0], "run ./internal/furrowbin/cmd/fetch -goos=windows -goarch=amd64 ") {
				t.Fatalf("W1 must fetch the pinned Windows engine first, got:\n%s", calls)
			}
			if failFetch {
				if buildErr == nil || len(lines) != 1 || !strings.Contains(string(output), "fixture: pinned Windows furrow download failed") {
					t.Fatalf("W1 fetch refusal must stop before build: %v; calls %s; output %s", buildErr, calls, output)
				}
			} else if buildErr != nil || len(lines) != 2 || !strings.HasPrefix(lines[1], "build ") || !strings.HasSuffix(lines[1], " windows/amd64") {
				t.Fatalf("W1 must build Windows after fetching: %v; calls %s; output %s", buildErr, calls, output)
			}
		})
	}
}
