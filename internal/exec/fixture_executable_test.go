package exec

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"

	commandshell "github.com/Agent-Field/codeaf/internal/shell"
)

// K6 uses the same scripted tools on both platforms. Native Windows needs a
// real executable entry point; its helper dispatches the unchanged script to
// Git Bash, while Unix retains the executable script itself.
func fixtureExecutable(t *testing.T, path string, source []byte, mode os.FileMode) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(path, source, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path += ".exe"
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if err := os.WriteFile(path+".script", source, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// K6 keeps make-based assertions on hosts with make. Git for Windows supplies
// Bash, but does not promise this separate program; native shell tests do not
// depend on it.
func requireFixtureMake(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	bash, err := commandshell.Bash()
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(bash, "-c", "command -v make").Run(); err != nil {
		t.Skip("make is not installed in the native Git Bash environment")
	}
}
