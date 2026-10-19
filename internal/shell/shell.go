// Package shell chooses a bash that shares the engine's filesystem and OS.
//
// On 2026-10-19 the native Windows desktop resolved bash to
// C:\Windows\System32\bash.exe, WSL's launcher. It parsed the command line a
// second time in Linux: $PATH in the plandb prefix expanded to WSL's PATH,
// whose Program Files (x86) entry made every task command fail with "syntax
// error near unexpected token '('". A chat's for n in …; do gh pr view $n; done
// also reached gh with an empty $n. Choosing a real bash fixes both failures
// and keeps commands beside the files the engine reads and writes.
//
// NEVER THE WSL LAUNCHER. A PATH THAT BASH READS IS IN BASH'S FORM, A VALUE A
// WINDOWS PROGRAM READS KEEPS ITS OWN. UNIX ARGV IS UNTOUCHED.
package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/env"
)

// NoBash is shared by every Windows command door so both remedies stay visible.
const NoBash = "codeaf runs commands in Git for Windows' bash, and none was found: install Git for Windows, or set CODEAF_BASH to a bash.exe. WSL's bash.exe is not used."

// MissingBash and WSLBash distinguish an explicit choice that cannot work from
// discovery finding nothing, so the remedy never contradicts the person's choice.
const MissingBash = "CODEAF_BASH names %s, which does not exist. Set it to a Git for Windows or MSYS2 bash.exe, or unset it and codeaf will look for Git Bash."
const WSLBash = "CODEAF_BASH names %s, which is WSL's launcher and runs commands in a different system. Set it to a Git for Windows or MSYS2 bash.exe, or unset it and codeaf will look for Git Bash."

// PathConversionFact keeps the command advice beside the selected shell fact;
// native programs receive Git Bash's usual argument conversion unless told otherwise.
const PathConversionFact = "- Git Bash rewrites arguments starting with / into Windows paths for Windows programs: write API routes without the leading slash (gh api repos/…), or prefix that command with MSYS_NO_PATHCONV=1.\n"

// Lookup makes resolution independent of the host so Windows rules can be proved
// without starting a process or depending on this machine's installed programs.
type Lookup struct {
	Env      func(string) string
	Exists   func(string) bool
	LookPath func(string) (string, error)
	// Abs binds an explicit relative file to this process's filesystem instead of
	// letting exec search PATH again. Pure fixtures may omit it for absolute paths.
	Abs func(string) (string, error)
}

// windowsClean uses Windows separators even when the resolver is tested on Unix.
// Cleaning with slash semantics first also removes dot components on either OS.
func windowsClean(name string) string {
	slash := strings.ReplaceAll(name, `\`, "/")
	volume := ""
	switch {
	case len(slash) >= 2 && slash[1] == ':':
		volume, slash = slash[:2], slash[2:]
	case strings.HasPrefix(slash, "//"):
		// A UNC server and share are a volume: parent components cannot escape it.
		if serverEnd := strings.IndexByte(slash[2:], '/'); serverEnd >= 0 {
			shareStart := serverEnd + 3
			shareEnd := len(slash)
			if end := strings.IndexByte(slash[shareStart:], '/'); end >= 0 {
				shareEnd = shareStart + end
			}
			volume, slash = slash[:shareEnd], slash[shareEnd:]
			if slash == "" {
				return strings.ReplaceAll(volume, "/", `\`)
			}
		}
	}
	return strings.ReplaceAll(volume+path.Clean(slash), "/", `\`)
}

// IsWSL rejects the system launchers and application aliases: neither executes
// commands in the same OS as the engine's native file tools.
func IsWSL(name, systemRoot string) bool {
	name = strings.ToLower(windowsClean(name))
	if strings.Contains(`\`+name, `\windowsapps\`) {
		return true
	}
	root := strings.ToLower(windowsClean(systemRoot))
	if systemRoot == "" {
		return false
	}
	for _, dir := range []string{"system32", "syswow64", "sysnative"} {
		if name == root+`\`+dir+`\bash.exe` {
			return true
		}
	}
	return false
}

// ResolveWindows follows the explicit choice, Git installation, standard roots,
// then PATH order. An unusable explicit choice is a refusal, never a suggestion.
func ResolveWindows(l Lookup) (string, error) {
	root := l.Env("SystemRoot")
	usable := func(name string) bool { return !IsWSL(name, root) && l.Exists(name) }
	if explicit := l.Env("CODEAF_BASH"); explicit != "" {
		candidate := explicit
		var err error
		if l.Abs != nil {
			candidate, err = l.Abs(explicit)
		}
		if IsWSL(candidate, root) {
			return "", fmt.Errorf(WSLBash, explicit)
		}
		if err != nil || !l.Exists(candidate) {
			return "", fmt.Errorf(MissingBash, explicit)
		}
		return candidate, nil
	}
	gitBash := func(root string) string {
		for _, sub := range []string{`bin\bash.exe`, `usr\bin\bash.exe`} {
			candidate := windowsClean(root + `\` + sub)
			if usable(candidate) {
				return candidate
			}
		}
		return ""
	}
	if git, err := l.LookPath("git.exe"); err == nil {
		git = windowsClean(git)
		lower := strings.ToLower(git)
		for _, suffix := range []string{`\mingw64\bin\git.exe`, `\cmd\git.exe`, `\bin\git.exe`} {
			if strings.HasSuffix(lower, suffix) {
				if bash := gitBash(git[:len(git)-len(suffix)]); bash != "" {
					return bash, nil
				}
				break
			}
		}
	}
	for _, install := range []struct{ env, sub string }{
		{"ProgramW6432", "Git"}, {"ProgramFiles", "Git"}, {"ProgramFiles(x86)", "Git"},
		{"LOCALAPPDATA", `Programs\Git`}, {"USERPROFILE", `scoop\apps\git\current`},
	} {
		if base := l.Env(install.env); base != "" {
			if bash := gitBash(base + `\` + install.sub); bash != "" {
				return bash, nil
			}
		}
	}
	if bash, err := l.LookPath("bash.exe"); err == nil && usable(bash) {
		return bash, nil
	}
	return "", errors.New(NoBash)
}

var bashCache struct {
	sync.Mutex
	key   string
	ready bool
	bash  string
	err   error
}

// Bash preserves the literal Unix argv[0]. Windows discovery is cached by its
// inputs, so changing an override in a test resets the answer without repeated
// filesystem probes on every command in a running session.
func Bash() (string, error) {
	if runtime.GOOS != "windows" {
		return "bash", nil
	}
	names := []string{"CODEAF_BASH", "PATH", "PATHEXT", "SystemRoot", "ProgramW6432", "ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA", "USERPROFILE"}
	values := make(map[string]string, len(names))
	var key strings.Builder
	for _, name := range names {
		values[name] = env.Value(name)
		key.WriteString(values[name])
		key.WriteByte(0)
	}
	bashCache.Lock()
	defer bashCache.Unlock()
	if !bashCache.ready || bashCache.key != key.String() {
		bashCache.bash, bashCache.err = ResolveWindows(Lookup{
			Env:      func(name string) string { return values[name] },
			Exists:   func(name string) bool { info, err := os.Stat(name); return err == nil && info.Mode().IsRegular() },
			LookPath: exec.LookPath,
			Abs:      filepath.Abs,
		})
		bashCache.key, bashCache.ready = key.String(), true
	}
	return bashCache.bash, bashCache.err
}

// Preferred preserves the Unix /bin/bash, PATH bash, sh preference used by the
// streaming and short-job doors, while Windows always uses the shared resolver.
func Preferred() (string, []string, error) {
	if runtime.GOOS == "windows" {
		bash, err := Bash()
		return bash, []string{"-c"}, err
	}
	return preferred(Lookup{
		Exists:   func(name string) bool { _, err := os.Stat(name); return err == nil },
		LookPath: exec.LookPath,
	})
}

func preferred(l Lookup) (string, []string, error) {
	if l.Exists("/bin/bash") {
		return "/bin/bash", []string{"-c"}, nil
	}
	if bash, err := l.LookPath("bash"); err == nil {
		return bash, []string{"-c"}, nil
	}
	return "sh", []string{"-c"}, nil
}

// Reading preserves verify's Unix PATH lookup, including its absence semantics.
func Reading() (string, error) {
	if runtime.GOOS == "windows" {
		return Bash()
	}
	return exec.LookPath("bash")
}

// ListPath converts only paths bash interprets itself. Values passed to native
// programs, such as PLANDB_DB, must keep their native Windows spelling.
func ListPath(native string) string { return listPath(runtime.GOOS, native) }

func listPath(goos, native string) string {
	if goos != "windows" {
		return native
	}
	if strings.HasPrefix(native, "/") {
		return native
	}
	slash := strings.ReplaceAll(native, `\`, "/")
	if len(slash) >= 3 && slash[1] == ':' && slash[2] == '/' && ((slash[0] >= 'A' && slash[0] <= 'Z') || (slash[0] >= 'a' && slash[0] <= 'z')) {
		return "/" + strings.ToLower(slash[:1]) + slash[2:]
	}
	return slash
}

// PathExport leaves Unix prefix bytes intact and puts a converted literal in a
// Windows PATH list. The native shelf variable remains available to programs.
func PathExport(variable, native string) string {
	if runtime.GOOS != "windows" {
		return "export PATH=\"${" + variable + ":?}:$PATH\"\n"
	}
	return "export PATH='" + strings.ReplaceAll(ListPath(native), "'", "'\\''") + "':$PATH\n"
}

// PromptFact is absent on Unix so existing prompt bytes stay unchanged.
func PromptFact() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	bash, err := Bash()
	if err != nil {
		return "- Shell: " + err.Error() + "\n"
	}
	return "- Shell: Git Bash (" + bash + "); Windows drives are /c/…, /d/…\n" + PathConversionFact
}
