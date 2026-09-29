//go:build !windows

package fullverification

import (
	"path/filepath"
	"strings"
)

// THE PROJECT'S CHECK RUNS IN THE PROJECT'S OWN PYTHON ENVIRONMENT.
//
// A run on a machine whose Python refuses `pip install` (PEP 668, Homebrew's)
// can only get a missing test tool by making a virtual environment in the
// project, which is also what a person working on it does. A check that ran
// the bare system `python3` ignored that environment, so everything installed
// there was invisible to it: the project's tests failed on `No module named
// pytest` beside a `.venv` that had pytest in it. So the check puts the
// project's environment first, the way `source .venv/bin/activate` would.

// venvNames are the folders a project keeps its virtual environment in, in the
// order they are looked for.
var venvNames = []string{".venv", "venv"}

// ProjectVenv is the Python virtual environment for a command run in workdir
// (relative to workspace, "" for the workspace itself): one in workdir, else
// one at the top of the workspace. A folder is one when it holds pyvenv.cfg
// and bin/python, a link to the interpreter it was made from, followed. ""
// when there is none.
func ProjectVenv(workspace, workdir string) string {
	places := []string{workspace}
	if workdir = strings.TrimSpace(workdir); workdir != "" && workdir != "." {
		if !filepath.IsAbs(workdir) {
			workdir = filepath.Join(workspace, workdir)
		}
		places = []string{workdir, workspace}
	}
	for _, place := range places {
		for _, name := range venvNames {
			venv := filepath.Join(place, name)
			if fileExists(filepath.Join(venv, "pyvenv.cfg")) && fileExists(filepath.Join(venv, "bin", "python")) {
				return venv
			}
		}
	}
	return ""
}

// ActivationPreamble is the shell lines that put the virtual environment for
// a command run in workdir first on PATH, the way its activate script does,
// and "" when there is none ([ProjectVenv]).
func ActivationPreamble(workspace, workdir string) string {
	venv := ProjectVenv(workspace, workdir)
	if venv == "" {
		return ""
	}
	return "export VIRTUAL_ENV='" + strings.ReplaceAll(venv, "'", `'\''`) + "'\n" +
		"export PATH=\"$VIRTUAL_ENV/bin:$PATH\"\n" +
		"unset PYTHONHOME\n"
}

// projectPython is the interpreter the check's Python commands resolve to in
// workspace: the project's environment's when it has one, else python3.
func projectPython(workspace string) string {
	if venv := ProjectVenv(workspace, ""); venv != "" {
		return filepath.Join(venv, "bin", "python")
	}
	return "python3"
}
