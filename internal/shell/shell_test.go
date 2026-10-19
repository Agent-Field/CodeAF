package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// R3 distinguishes the two explicit-choice refusals without claiming discovery
// failed, and preserves the person's path exactly in either answer.
func TestWindowsOverrideRefusalSentences(t *testing.T) {
	for _, tc := range []struct{ path, format string }{
		{`C:\missing é\bash.exe`, MissingBash},
		{`C:\Windows\System32\bash.exe`, WSLBash},
	} {
		lookup := fakeLookup(map[string]string{"CODEAF_BASH": tc.path, "SystemRoot": `C:\Windows`}, nil)
		if got, err := ResolveWindows(lookup); got != "" || err == nil || err.Error() != fmt.Sprintf(tc.format, tc.path) {
			t.Fatalf("override %q: %q, %v", tc.path, got, err)
		}
	}
}

func fakeLookup(environment, programs map[string]string, files ...string) Lookup {
	return Lookup{
		Env: func(name string) string { return environment[name] },
		Exists: func(name string) bool {
			for _, file := range files {
				if strings.EqualFold(windowsClean(name), windowsClean(file)) {
					return true
				}
			}
			return false
		},
		LookPath: func(name string) (string, error) {
			if found := programs[name]; found != "" {
				return found, nil
			}
			return "", errors.New("not found")
		},
	}
}

// C2-C4 prove every resolution tier without depending on a Windows host.
func TestWindowsResolutionRules(t *testing.T) {
	const git = `C:\Program Files\Git`
	const bin = git + `\bin\bash.exe`
	const usr = git + `\usr\bin\bash.exe`
	const other = `D:\msys64\usr\bin\bash.exe`
	const wsl = `C:\Windows\System32\bash.exe`
	tests := []struct {
		name          string
		env, programs map[string]string
		files         []string
		want, refusal string
	}{
		{name: "explicit wins", env: map[string]string{"CODEAF_BASH": other, "ProgramFiles": `C:\Program Files`}, programs: map[string]string{"git.exe": git + `\cmd\git.exe`, "bash.exe": wsl}, files: []string{other, bin, wsl}, want: other},
		{name: "missing explicit refuses", env: map[string]string{"CODEAF_BASH": other}, programs: map[string]string{"bash.exe": bin}, files: []string{bin}, refusal: other},
		{name: "WSL explicit refuses", env: map[string]string{"CODEAF_BASH": wsl, "SystemRoot": `C:\Windows`}, programs: map[string]string{"git.exe": git + `\cmd\git.exe`}, files: []string{bin, wsl}, refusal: wsl},
		{name: "git cmd", programs: map[string]string{"git.exe": git + `\cmd\git.exe`, "bash.exe": other}, files: []string{bin, usr, other}, want: bin},
		{name: "git bin", programs: map[string]string{"git.exe": git + `\bin\git.exe`}, files: []string{bin}, want: bin},
		{name: "git mingw64", programs: map[string]string{"git.exe": git + `\mingw64\bin\git.exe`}, files: []string{bin}, want: bin},
		{name: "git mixed slash case", programs: map[string]string{"git.exe": `C:/Program Files/Git/MINGW64/bin/git.exe`}, files: []string{bin}, want: bin},
		{name: "git usr fallback", programs: map[string]string{"git.exe": git + `\cmd\git.exe`}, files: []string{usr}, want: usr},
		{name: "git beats roots", env: map[string]string{"ProgramW6432": `D:\Programs`}, programs: map[string]string{"git.exe": git + `\cmd\git.exe`}, files: []string{bin, `D:\Programs\Git\bin\bash.exe`}, want: bin},
		{name: "missing git bash falls through", env: map[string]string{"ProgramFiles": `C:\Program Files`}, programs: map[string]string{"git.exe": `D:\Git\cmd\git.exe`}, files: []string{bin}, want: bin},
		{name: "unrelated git path is not a root", programs: map[string]string{"git.exe": `D:\tools\git.exe`, "bash.exe": other}, files: []string{`D:\bin\bash.exe`, other}, want: other},
		{name: "root beats PATH WSL", env: map[string]string{"SystemRoot": `C:\Windows`, "ProgramFiles": `C:\Program Files`}, programs: map[string]string{"bash.exe": wsl}, files: []string{bin, wsl}, want: bin},
		{name: "PATH MSYS", programs: map[string]string{"bash.exe": other}, files: []string{other}, want: other},
		{name: "PATH WSL refuses", env: map[string]string{"SystemRoot": `C:\Windows`}, programs: map[string]string{"bash.exe": wsl}, files: []string{wsl}, refusal: NoBash},
		{name: "PATH alias refuses", programs: map[string]string{"bash.exe": `C:\Users\x\AppData\Local\WindowsApps\bash.exe`}, files: []string{`C:\Users\x\AppData\Local\WindowsApps\bash.exe`}, refusal: NoBash},
		{name: "missing PATH file", programs: map[string]string{"bash.exe": other}, refusal: NoBash},
		{name: "none", refusal: NoBash},
	}
	// C3 also proves the complete standard-root ordering, including usr fallback.
	roots := []struct{ env, base, sub string }{
		{"ProgramW6432", `C:\W6432`, "Git"}, {"ProgramFiles", `C:\PF`, "Git"}, {"ProgramFiles(x86)", `C:\PF86`, "Git"},
		{"LOCALAPPDATA", `C:\Local`, `Programs\Git`}, {"USERPROFILE", `C:\Users\me`, `scoop\apps\git\current`},
	}
	for i, root := range roots {
		environment := map[string]string{}
		files := []string{other}
		for _, r := range roots[i:] {
			environment[r.env] = r.base
			files = append(files, r.base+`\`+r.sub+`\usr\bin\bash.exe`, r.base+`\`+r.sub+`\bin\bash.exe`)
		}
		tests = append(tests, struct {
			name          string
			env, programs map[string]string
			files         []string
			want, refusal string
		}{name: root.env, env: environment, programs: map[string]string{"bash.exe": other}, files: files, want: root.base + `\` + root.sub + `\bin\bash.exe`})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveWindows(fakeLookup(tc.env, tc.programs, tc.files...))
			if tc.refusal != "" {
				if got != "" || err == nil || !strings.Contains(err.Error(), tc.refusal) {
					t.Fatalf("got %q, %v; want refusal %q", got, err, tc.refusal)
				}
				if tc.refusal == NoBash && err.Error() != NoBash {
					t.Fatalf("unshared refusal: %v", err)
				}
				if tc.env["CODEAF_BASH"] != "" && !strings.Contains(err.Error(), "CODEAF_BASH") {
					t.Fatal(err)
				}
			} else if err != nil || !strings.EqualFold(got, tc.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// C2 covers cleaned, case-insensitive system launcher and WindowsApps paths.
func TestWindowsWSLPredicate(t *testing.T) {
	for _, name := range []string{
		`C:\Windows\System32\bash.exe`, `C:\WINDOWS\SysWOW64\BASH.EXE`, `c:/windows/Sysnative/bash.exe/`,
		`C:/Windows/temp/../System32/./bash.exe`, `C:/../Windows/System32/bash.exe`, `\\server\WindowsApps\..\bash.exe`, `C:\Users\x\WindowsApps\bash.exe`, `D:/WINDOWSAPPS/alias/bash.exe`, `WindowsApps/bash.exe`,
	} {
		if !IsWSL(name, `c:\Windows\`) {
			t.Errorf("accepted WSL %q", name)
		}
	}
	for _, name := range []string{`C:\Git\bin\bash.exe`, `C:\Windows\System32\Git\bash.exe`, `D:\Windows\System32\bash.exe`, `C:\WindowsAppsExtra\bash.exe`, `/usr/bin/bash`} {
		if IsWSL(name, `C:\Windows`) {
			t.Errorf("rejected bash %q", name)
		}
	}
}

// D6 and C6-C7 pin drive, UNC and POSIX forms without spawning a converter.
func TestWindowsListPath(t *testing.T) {
	for _, tc := range []struct{ native, want string }{
		{`C:\a\b`, `/c/a/b`}, {`D:/a/b`, `/d/a/b`}, {`z:\Program Files (x86)\héllo ✓`, `/z/Program Files (x86)/héllo ✓`},
		{`\\server\share\x`, `//server/share/x`}, {`/c/a/b`, `/c/a/b`}, {`//server/share/x`, `//server/share/x`}, {`/usr/bin`, `/usr/bin`}, {"", ""},
	} {
		if got := listPath("windows", tc.native, filepath.Abs); got != tc.want {
			t.Errorf("%q => %q, want %q", tc.native, got, tc.want)
		}
		for _, goos := range []string{"linux", "darwin"} {
			if got := listPath(goos, tc.native, filepath.Abs); got != tc.native {
				t.Errorf("%s changed %q to %q", goos, tc.native, got)
			}
		}
	}
}

// C5 pins all Unix preferences, including missing /bin/bash, with fake lookups.
func TestUnixShellArgv(t *testing.T) {
	for _, tc := range []struct {
		absolute, onPath bool
		want             string
	}{
		{true, true, "/bin/bash"}, {true, false, "/bin/bash"}, {false, true, "/fake/bin/bash"}, {false, false, "sh"},
	} {
		programs := map[string]string{}
		if tc.onPath {
			programs["bash"] = "/fake/bin/bash"
		}
		var files []string
		if tc.absolute {
			files = []string{"/bin/bash"}
		}
		got, args, err := preferred(fakeLookup(nil, programs, files...))
		if got != tc.want || !reflect.DeepEqual(args, []string{"-c"}) || err != nil {
			t.Fatalf("got %q %q %v", got, args, err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	if got, err := Bash(); got != "bash" || err != nil {
		t.Fatalf("literal Unix bash changed: %q %v", got, err)
	}
	if fact := PromptFact(); fact != "" {
		t.Fatalf("Unix footer changed: %q", fact)
	}
	if got := PathExport("CODEAF_SKILLS_BIN", `C:\shelf`); got != "export PATH=\"${CODEAF_SKILLS_BIN:?}:$PATH\"\n" {
		t.Fatalf("Unix shelf changed: %q", got)
	}
}

// C5 proves verify still resolves bash from PATH, rather than /bin/bash or sh.
func TestUnixReadingUsesPathLookup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix lookup contract")
	}
	dir := t.TempDir()
	bash := filepath.Join(dir, "bash")
	if err := os.WriteFile(bash, []byte("unused"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got, err := Reading(); got != bash || err != nil {
		t.Fatalf("reading shell: %q %v", got, err)
	}
	if err := os.Remove(bash); err != nil {
		t.Fatal(err)
	}
	if got, err := Reading(); got != "" || err == nil {
		t.Fatalf("missing bash fell through: %q %v", got, err)
	}
}

// C2-C4 prove an explicit relative file cannot be re-searched on PATH and reach
// WSL, and that an explicit relative path into a system folder is also refused.
func TestWindowsExplicitRelativePath(t *testing.T) {
	const candidate = `C:\work\bash.exe`
	lookup := fakeLookup(map[string]string{"CODEAF_BASH": "bash.exe", "SystemRoot": `C:\Windows`}, map[string]string{"bash.exe": `C:\Windows\System32\bash.exe`}, candidate, `C:\Windows\System32\bash.exe`)
	lookup.Abs = func(name string) (string, error) { return candidate, nil }
	if got, err := ResolveWindows(lookup); got != candidate || err != nil {
		t.Fatalf("explicit relative file: %q %v", got, err)
	}
	lookup.Abs = func(name string) (string, error) { return `C:\Windows\System32\bash.exe`, nil }
	if got, err := ResolveWindows(lookup); got != "" || err == nil || err.Error() != fmt.Sprintf(WSLBash, "bash.exe") {
		t.Fatalf("relative WSL: %q %v", got, err)
	}
}
