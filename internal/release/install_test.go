package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type fakeRelease struct {
	TagName     string      `json:"tag_name"`
	CreatedAt   string      `json:"created_at,omitempty"`
	PublishedAt any         `json:"published_at,omitempty"`
	Assets      []fakeAsset `json:"assets,omitempty"`
}

type fakeAsset struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type fakeTiming struct {
	created   string
	published any
}

type installGitHub struct {
	server            *httptest.Server
	releases          []string
	timings           map[string]fakeTiming
	badChecksum       bool
	failAPI           bool
	notFound          bool
	oldRepositoryOnly bool
	legacyAssetOnly   bool
	binary            []byte
	mu                sync.Mutex
	requests          []*http.Request
}

func newInstallGitHub(t *testing.T, releases ...string) *installGitHub {
	t.Helper()
	github := &installGitHub{releases: releases}
	github.server = httptest.NewServer(http.HandlerFunc(github.serve))
	t.Cleanup(github.server.Close)
	return github
}

func (github *installGitHub) serve(w http.ResponseWriter, request *http.Request) {
	github.mu.Lock()
	github.requests = append(github.requests, request.Clone(request.Context()))
	github.mu.Unlock()
	if github.notFound {
		http.NotFound(w, request)
		return
	}

	path := request.URL.Path
	const prefix = "/repos/Agent-Field/codeaf/"
	const legacyPrefix = "/repos/Agent-Field/aforge-v2/" // legacy-name
	apiPrefix := ""
	switch {
	case strings.HasPrefix(path, prefix) && !github.oldRepositoryOnly:
		apiPrefix = prefix
	case strings.HasPrefix(path, legacyPrefix):
		apiPrefix = legacyPrefix
	}
	if apiPrefix != "" {
		if github.failAPI {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		switch {
		case path == apiPrefix+"releases/latest":
			if len(github.releases) == 0 {
				http.NotFound(w, request)
				return
			}
			github.writeJSON(w, fakeRelease{TagName: github.releases[0]})
			return
		case path == apiPrefix+"releases":
			var releases []fakeRelease
			for _, tag := range github.releases {
				release := fakeRelease{TagName: tag}
				if timing, ok := github.timings[tag]; ok {
					release.CreatedAt = timing.created
					release.PublishedAt = timing.published
				}
				if github.timings != nil {
					release.Assets = []fakeAsset{{
						Name:      platformAsset(),
						CreatedAt: "2999-01-01T00:00:00Z",
						UpdatedAt: "2999-01-01T00:00:00Z",
					}}
				}
				releases = append(releases, release)
			}
			github.writeJSON(w, releases)
			return
		}
	}

	const downloads = "/Agent-Field/codeaf/releases/download/"
	const legacyDownloads = "/Agent-Field/aforge-v2/releases/download/" // legacy-name
	downloadPrefix := ""
	switch {
	case strings.HasPrefix(path, downloads) && !github.oldRepositoryOnly:
		downloadPrefix = downloads
	case strings.HasPrefix(path, legacyDownloads):
		downloadPrefix = legacyDownloads
	}
	if downloadPrefix != "" {
		rest := strings.TrimPrefix(path, downloadPrefix)
		tag, name, ok := strings.Cut(rest, "/")
		if !ok || !contains(github.releases, tag) {
			http.NotFound(w, request)
			return
		}
		github.writeAsset(w, tag, name)
		return
	}
	http.NotFound(w, request)
}

func (github *installGitHub) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (github *installGitHub) writeAsset(w http.ResponseWriter, tag, name string) {
	binary := fakeBinary(tag)
	if len(github.binary) != 0 {
		binary = github.binary
	}
	asset := platformAsset()
	if github.legacyAssetOnly {
		asset = legacyPlatformAsset()
	}
	switch name {
	case asset:
		_, _ = w.Write(binary)
	case "checksums.txt":
		digest := sha256.Sum256(binary)
		checksum := hex.EncodeToString(digest[:])
		if github.badChecksum {
			checksum = strings.Repeat("0", len(checksum))
		}
		fmt.Fprintf(w, "%s  %s\n", checksum, asset)
	default:
		http.NotFound(w, nil)
	}
}

func fakeBinary(tag string) []byte {
	return []byte("#!/bin/sh\nprintf 'codeaf " + tag + " · fake\\n'\n")
}

func platformAsset() string {
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	return "codeaf-" + runtime.GOOS + "-" + runtime.GOARCH + extension
}

func legacyPlatformAsset() string {
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	return "aforge-" + runtime.GOOS + "-" + runtime.GOARCH + extension // legacy-name
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type installRun struct {
	code       int
	output     string
	home       string
	installDir string
}

func runInstaller(t *testing.T, github *installGitHub, arguments []string, extraEnv ...string) installRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the installer contract is exercised through Bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	home := t.TempDir()
	installDir := filepath.Join(home, "install")
	path := minimalPath(t, true)
	command := exec.Command(bash, append([]string{filepath.Join(repositoryRoot(t), "scripts", "install.sh")}, arguments...)...)
	command.Env = append([]string{
		"HOME=" + home,
		"CODEAF_INSTALL_DIR=" + installDir,
		"CODEAF_GITHUB_API=" + github.server.URL,
		"CODEAF_GITHUB_DOWNLOAD=" + github.server.URL,
		"PATH=" + path,
		"SHELL=/bin/bash",
	}, extraEnv...)
	output, runErr := command.CombinedOutput()
	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !strings.Contains(runErr.Error(), "exit status") || !asExitError(runErr, &exitErr) {
			t.Fatalf("run installer: %v", runErr)
		}
		code = exitErr.ExitCode()
	}
	return installRun{code: code, output: string(output), home: home, installDir: installDir}
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

func minimalPath(t *testing.T, withCurl bool) string {
	t.Helper()
	dir := t.TempDir()
	commands := []string{"awk", "basename", "cat", "chmod", "cp", "dirname", "grep", "ln", "mkdir", "mktemp", "mv", "rm", "sed", "sha256sum", "shasum", "tr", "uname", "wget"}
	if withCurl {
		commands = append(commands, "curl")
	}
	for _, name := range commands {
		target, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInstallerGetsLatestStableAndFinishesWithVersion(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	run := runInstaller(t, github, nil, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("exit %d:\n%s", run.code, run.output)
	}
	// A normal run says three things and nothing else: the installed binary
	// naming itself, the notice, the line to paste. The channel, the tag and
	// the platform are --verbose's to say.
	if !strings.Contains(run.output, "installed codeaf v1.2.3 · fake") {
		t.Errorf("output does not carry the receipt:\n%s", run.output)
	}
	for _, absent := range []string{"stable v1.2.3", "codeaf: installed"} {
		if strings.Contains(run.output, absent) {
			t.Errorf("a normal run should not say %q:\n%s", absent, run.output)
		}
	}
	verbose := runInstaller(t, github, []string{"--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
	for _, want := range []string{"stable v1.2.3", runtime.GOOS + "/" + runtime.GOARCH, "codeaf: installed", "installed codeaf v1.2.3 · fake"} {
		if verbose.code != 0 || !strings.Contains(verbose.output, want) {
			t.Errorf("verbose output does not contain %q (exit %d):\n%s", want, verbose.code, verbose.output)
		}
	}
	if _, err := os.Stat(filepath.Join(run.installDir, "codeaf")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.output, "export PATH=") {
		t.Fatalf("the no-modify-path run did not print the line to paste:\n%s", run.output)
	}
	if _, err := os.Stat(filepath.Join(run.home, ".bashrc")); !os.IsNotExist(err) {
		t.Fatalf("--no-modify-path edited the shell file: %v", err)
	}
}

// H10: a 404 from the current repository makes release discovery retry the
// former repository and complete the install from there.
func TestH10InstallerFallsBackToLegacyRepository(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.oldRepositoryOnly = true
	run := runInstaller(t, github, nil, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("fallback install exit %d:\n%s", run.code, run.output)
	}
	github.mu.Lock()
	defer github.mu.Unlock()
	var current, legacy bool
	for _, request := range github.requests {
		current = current || request.URL.Path == "/repos/Agent-Field/codeaf/releases/latest"
		legacy = legacy || request.URL.Path == "/repos/Agent-Field/aforge-v2/releases/latest" // legacy-name
	}
	if !current || !legacy {
		t.Fatalf("repository requests current=%v legacy=%v", current, legacy)
	}
}

// H10: a release containing only the former asset spelling is installed under
// the current binary name after its checksum is verified.
func TestH10InstallerFallsBackToLegacyAssetName(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.legacyAssetOnly = true
	run := runInstaller(t, github, nil, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("asset fallback exit %d:\n%s", run.code, run.output)
	}
	installed, err := os.ReadFile(filepath.Join(run.installDir, "codeaf"))
	if err != nil || !strings.Contains(string(installed), "codeaf v1.2.3") {
		t.Fatalf("installed current binary = %q, %v", installed, err)
	}
}

func TestPinnedFormerReleaseKeepsAssetAndChecksumInTheFormerRepository(t *testing.T) {
	github := newInstallGitHub(t, "v0.2.0")
	github.oldRepositoryOnly = true
	github.legacyAssetOnly = true
	run := runInstaller(t, github, []string{"--version", "v0.2.0"}, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("pinned compatibility install exit %d:\n%s", run.code, run.output)
	}
	github.mu.Lock()
	defer github.mu.Unlock()
	var asset, checksum bool
	for _, request := range github.requests {
		asset = asset || strings.Contains(request.URL.Path, "/Agent-Field/aforge-v2/releases/download/v0.2.0/aforge-") // legacy-name
		checksum = checksum || request.URL.Path == "/Agent-Field/aforge-v2/releases/download/v0.2.0/checksums.txt"     // legacy-name
	}
	if !asset || !checksum {
		t.Fatalf("former repository asset=%v checksum=%v", asset, checksum)
	}
}

func TestInstallerRunsAdoptionBeforeCreatingTheStateRoot(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.binary = adoptingFakeBinary("v1.2.3")
	home := t.TempDir()
	oldState := filepath.Join(home, ".aforge") // legacy-name
	kept := filepath.Join(oldState, "v3", "projects", "p1", "note.txt")
	if err := os.MkdirAll(filepath.Dir(kept), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(home, ".codeaf", "bin")
	run := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+installDir, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("adopting install exit %d:\n%s", run.code, run.output)
	}
	adopted := filepath.Join(home, ".codeaf", "v3", "projects", "p1", "note.txt")
	if body, err := os.ReadFile(adopted); err != nil || string(body) != "kept" {
		t.Fatalf("adopted state = %q, %v", body, err)
	}
	if info, err := os.Lstat(oldState); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("former state root is not a compatibility link: %v %v", info, err)
	}
	installed := filepath.Join(installDir, "codeaf")
	command := exec.Command(installed, "doctor")
	command.Env = []string{"HOME=" + home}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), adopted) {
		t.Fatalf("installed executable does not resolve adopted state: %v\n%s", err, output)
	}
}

func TestCustomInstallOutsideTheStateRootDoesNotRunAdoption(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.binary = adoptingFakeBinary("v1.2.3")
	home := t.TempDir()
	oldState := filepath.Join(home, ".aforge") // legacy-name
	if err := os.MkdirAll(oldState, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "bin")
	run := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+outside, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("custom install exit %d:\n%s", run.code, run.output)
	}
	if info, err := os.Lstat(oldState); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("custom install moved former state: %v %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codeaf")); !os.IsNotExist(err) {
		t.Fatalf("custom install created the state root: %v", err)
	}
}

func adoptingFakeBinary(tag string) []byte {
	return []byte("#!/bin/sh\n" +
		"if [ -z \"${CODEAF_HOME+x}\" ] && [ -z \"${AFORGE_HOME+x}\" ] && [ ! -e \"$HOME/.codeaf\" ] && [ -d \"$HOME/.aforge\" ] && [ ! -L \"$HOME/.aforge\" ]; then\n" + // legacy-name
		"  mv \"$HOME/.aforge\" \"$HOME/.codeaf\" && ln -s .codeaf \"$HOME/.aforge\"\n" + // legacy-name
		"fi\n" +
		"if [ \"${1:-}\" = doctor ]; then printf '%s\\n' \"$HOME/.codeaf/v3/projects/p1/note.txt\"; exit 0; fi\n" +
		"printf 'codeaf " + tag + " · fake\\n'\n")
}

// H10: PATH edits use the current installer marker and remain idempotent.
func TestH10InstallerWritesTheCurrentPathMarker(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	home := t.TempDir()
	run := runInstaller(t, github, nil, "HOME="+home)
	if run.code != 0 {
		t.Fatalf("install exit %d:\n%s", run.code, run.output)
	}
	body, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "# codeaf installer") != 1 {
		t.Fatalf("PATH marker = %q", body)
	}
}

func TestInstallerReplacesTheFormerPathLineOnceAndStaysIdempotent(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	home := t.TempDir()
	rc := filepath.Join(home, ".bashrc")
	former := "export PATH=\"$HOME/.aforge/bin:$PATH\" # aforge installer\n" // legacy-name
	if err := os.WriteFile(rc, []byte(former), 0o600); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(home, ".codeaf", "bin")
	for attempt := 0; attempt < 3; attempt++ {
		run := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+installDir)
		if run.code != 0 {
			t.Fatalf("attempt %d exit %d:\n%s", attempt+1, run.code, run.output)
		}
	}
	body, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "# codeaf installer") != 1 || strings.Contains(string(body), "aforge") { // legacy-name
		t.Fatalf("PATH line was not repaired exactly once:\n%s", body)
	}
}

func TestInstallerSelectsTheNewestBuildOfEachChannel(t *testing.T) {
	github := newInstallGitHub(t,
		"v2.0.0", "dev-20260910-aaaaaaaaaaaa", "v2.1.0-rc.2",
		"staging-20260910-bbbbbbbbbbbb", "v2.1.0-rc.1")
	for _, test := range []struct{ channel, want string }{
		{"rc", "v2.1.0-rc.2"}, {"dev", "dev-20260910-aaaaaaaaaaaa"}, {"staging", "staging-20260910-bbbbbbbbbbbb"},
	} {
		t.Run(test.channel, func(t *testing.T) {
			arguments := []string{"--" + test.channel}
			environment := []string{"CODEAF_NO_MODIFY_PATH=1"}
			if test.channel == "rc" {
				arguments = nil
				environment = append(environment, "CHANNEL=rc")
			}
			run := runInstaller(t, github, arguments, environment...)
			if run.code != 0 || !strings.Contains(run.output, test.want) {
				t.Fatalf("exit %d, want %s:\n%s", run.code, test.want, run.output)
			}
		})
	}
}

// Contract: the newest published_at wins regardless of list order, created_at
// replaces a null published_at, and the same ordering applies to staging.
func TestInstallerPicksTheNewestChannelBuildWhateverTheListOrder(t *testing.T) {
	t.Run("a newer malformed tag is ignored", func(t *testing.T) {
		const (
			valid     = "dev-20260915-abcdefabcdef"
			malformed = "dev-backfill"
		)
		github := newInstallGitHub(t, valid, malformed)
		github.timings = map[string]fakeTiming{
			valid:     {published: "2026-09-15T15:00:00Z"},
			malformed: {published: "2026-09-15T16:00:00Z"},
		}
		run := runInstaller(t, github, []string{"--dev", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "dev "+valid) || strings.Contains(run.output, malformed) {
			t.Fatalf("exit %d, want valid tag %s:\n%s", run.code, valid, run.output)
		}
	})

	t.Run("a release candidate with leading zeroes is ignored", func(t *testing.T) {
		const (
			valid     = "v2.1.0-rc.1"
			malformed = "v02.1.0-rc.2"
		)
		github := newInstallGitHub(t, valid, malformed)
		github.timings = map[string]fakeTiming{
			valid:     {published: "2026-09-15T15:00:00Z"},
			malformed: {published: "2026-09-15T16:00:00Z"},
		}
		run := runInstaller(t, github, []string{"--rc", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "rc "+valid) || strings.Contains(run.output, malformed) {
			t.Fatalf("exit %d, want valid tag %s:\n%s", run.code, valid, run.output)
		}
	})

	t.Run("newest published_at", func(t *testing.T) {
		const (
			oldest = "dev-20260915-e2ae913b7d0c"
			middle = "dev-20260915-56a22c20ec53"
			newest = "dev-20260915-4b6ec83cfbfa"
		)
		github := newInstallGitHub(t, oldest, middle, newest)
		github.timings = map[string]fakeTiming{
			oldest: {published: "2026-09-15T13:43:00Z"},
			middle: {published: "2026-09-15T14:56:00Z"},
			newest: {published: "2026-09-15T15:21:00Z"},
		}
		run := runInstaller(t, github, []string{"--dev", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
		if !strings.Contains(run.output, "dev "+newest) {
			t.Errorf("output does not name newest tag %q:\n%s", newest, run.output)
		}
		for _, older := range []string{oldest, middle} {
			if strings.Contains(run.output, older) {
				t.Errorf("output contains older tag %q:\n%s", older, run.output)
			}
		}
		installed, err := os.ReadFile(filepath.Join(run.installDir, "codeaf"))
		if err != nil || !strings.Contains(string(installed), "codeaf "+newest) {
			t.Fatalf("installed binary = %q, %v", installed, err)
		}
		github.mu.Lock()
		defer github.mu.Unlock()
		for _, request := range github.requests {
			if strings.Contains(request.URL.Path, "/releases/download/"+oldest+"/") {
				t.Fatalf("installer requested the oldest release: %s", request.URL.Path)
			}
		}
	})

	t.Run("created_at replaces null published_at", func(t *testing.T) {
		const (
			older  = "dev-20260915-111111111111"
			newest = "dev-20260915-222222222222"
		)
		github := newInstallGitHub(t, older, newest)
		github.timings = map[string]fakeTiming{
			older:  {created: "2026-09-15T13:00:00Z", published: "2026-09-15T14:00:00Z"},
			newest: {created: "2026-09-15T16:00:00Z", published: json.RawMessage("null")},
		}
		run := runInstaller(t, github, []string{"--dev", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "dev "+newest) {
			t.Fatalf("exit %d, want %s:\n%s", run.code, newest, run.output)
		}
		installed, err := os.ReadFile(filepath.Join(run.installDir, "codeaf"))
		if err != nil || !strings.Contains(string(installed), "codeaf "+newest) {
			t.Fatalf("installed binary = %q, %v", installed, err)
		}
	})

	t.Run("staging uses timestamps", func(t *testing.T) {
		const (
			older  = "staging-20260915-333333333333"
			newest = "staging-20260915-444444444444"
		)
		github := newInstallGitHub(t, older, newest)
		github.timings = map[string]fakeTiming{
			older:  {published: "2026-09-15T12:00:00Z"},
			newest: {published: "2026-09-15T17:00:00Z"},
		}
		run := runInstaller(t, github, []string{"--staging", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "staging "+newest) {
			t.Fatalf("exit %d, want %s:\n%s", run.code, newest, run.output)
		}
	})
}

func TestInstallerPinsAReleaseAndNamesAMissingOne(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3", "build-legacy")
	// The channel and tag are --verbose lines; a normal run's receipt is the
	// installed binary naming itself, which the fake does with its tag.
	run := runInstaller(t, github, []string{"--version", "v1.2.3", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 || !strings.Contains(run.output, "stable v1.2.3") {
		t.Fatalf("exit %d:\n%s", run.code, run.output)
	}
	fromEnvironment := runInstaller(t, github, nil, "VERSION=v1.2.3", "CODEAF_NO_MODIFY_PATH=1")
	if fromEnvironment.code != 0 || !strings.Contains(fromEnvironment.output, "installed codeaf v1.2.3 · fake") {
		t.Fatalf("VERSION install exit %d:\n%s", fromEnvironment.code, fromEnvironment.output)
	}
	legacy := runInstaller(t, github, []string{"--version", "build-legacy", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
	wantLegacy := "codeaf: build-legacy for " + runtime.GOOS + "/" + runtime.GOARCH
	if legacy.code != 0 || !strings.Contains(legacy.output, wantLegacy) || strings.Contains(legacy.output, "codeaf: version ") {
		t.Fatalf("legacy-tag install exit %d:\n%s", legacy.code, legacy.output)
	}

	missing := runInstaller(t, github, []string{"--version", "v9.9.9"}, "CODEAF_NO_MODIFY_PATH=1")
	if missing.code != 1 || !strings.Contains(missing.output, "v9.9.9") || !strings.Contains(missing.output, "check the tag on the Releases page") {
		t.Fatalf("missing release: exit %d:\n%s", missing.code, missing.output)
	}
}

func TestDocumentedVersionPinReachesThePipedInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the installer contract is exercised through Bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	github := newInstallGitHub(t, "v1.2.3", "v9.9.9")
	home := t.TempDir()
	installDir := filepath.Join(home, "install")
	path := minimalPath(t, true)
	if err := os.Symlink(bash, filepath.Join(path, "bash")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repositoryRoot(t), "scripts", "install.sh")
	command := exec.Command(bash, "-c", `cat "$1" | VERSION=v1.2.3 bash`, "documented-pin", script)
	command.Env = []string{
		"HOME=" + home,
		"CODEAF_INSTALL_DIR=" + installDir,
		"CODEAF_GITHUB_API=" + github.server.URL,
		"CODEAF_GITHUB_DOWNLOAD=" + github.server.URL,
		"CODEAF_NO_MODIFY_PATH=1",
		"PATH=" + path,
		"SHELL=/bin/bash",
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "installed codeaf v1.2.3 · fake") || strings.Contains(string(output), "v9.9.9") {
		t.Fatalf("documented pin failed: %v\n%s", err, output)
	}
}

func TestInstallerNamesAnEmptyChannelWithoutWriting(t *testing.T) {
	for _, test := range []struct {
		name      string
		releases  []string
		arguments []string
	}{
		{name: "stable", arguments: []string{"--stable"}},
		{name: "staging", releases: []string{"v1.2.3"}, arguments: []string{"--staging"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, test.releases...)
			run := runInstaller(t, github, test.arguments)
			want := "codeaf: no " + test.name + " build has been published yet"
			if run.code != 1 || !strings.Contains(run.output, want) {
				t.Fatalf("exit %d:\n%s", run.code, run.output)
			}
			if _, err := os.Stat(run.installDir); !os.IsNotExist(err) {
				t.Fatalf("install directory was written on refusal: %v", err)
			}
		})
	}
}

func TestInstallerChecksBeforeReplacingAndCanRunTwice(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	badHome := t.TempDir()
	badDir := filepath.Join(badHome, "install")
	github.badChecksum = true
	run := runInstaller(t, github, nil, "CODEAF_INSTALL_DIR="+badDir, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 1 || !strings.Contains(run.output, "checksum") {
		t.Fatalf("checksum refusal: exit %d:\n%s", run.code, run.output)
	}
	if _, err := os.Stat(filepath.Join(badDir, "codeaf")); !os.IsNotExist(err) {
		t.Fatalf("a failed checksum left a binary: %v", err)
	}

	github.badChecksum = false
	home := t.TempDir()
	dir := filepath.Join(home, "install")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codeaf"), []byte("old running binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+dir)
	second := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+dir)
	if first.code != 0 || second.code != 0 {
		t.Fatalf("repeat exits %d and %d:\n%s\n%s", first.code, second.code, first.output, second.output)
	}
	rc, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rc), "# codeaf installer") != 1 {
		t.Fatalf("PATH edit is not idempotent:\n%s", rc)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".codeaf.tmp.*")); len(matches) != 0 {
		t.Fatalf("atomic install left temporary files: %v", matches)
	}
	installed, err := os.ReadFile(filepath.Join(dir, "codeaf"))
	if err != nil || strings.Contains(string(installed), "old running binary") {
		t.Fatalf("the old binary was not replaced: %v %q", err, installed)
	}
}

func TestInstallerSendsTheTokenOnAPICalls(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		path      string
	}{
		{name: "latest", path: "/repos/Agent-Field/codeaf/releases/latest"},
		{name: "list", arguments: []string{"--rc"}, path: "/repos/Agent-Field/codeaf/releases"},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3", "v1.3.0-rc.1")
			const token = "secret-installer-token"
			run := runInstaller(t, github, test.arguments, "GITHUB_TOKEN="+token, "CODEAF_NO_MODIFY_PATH=1")
			if run.code != 0 {
				t.Fatalf("token install exit %d:\n%s", run.code, run.output)
			}
			github.mu.Lock()
			defer github.mu.Unlock()
			for _, request := range github.requests {
				if request.URL.Path == test.path {
					if authorization := request.Header.Get("Authorization"); authorization != "Bearer "+token {
						t.Fatalf("API request Authorization = %q", authorization)
					}
					return
				}
			}
			t.Fatalf("API request %s was not made", test.path)
		})
	}
}

func TestInstallerNeverSendsAuthorizationOnDownloads(t *testing.T) {
	for _, token := range []string{"", "GITHUB_TOKEN=secret-installer-token"} {
		name := "without_token"
		if token != "" {
			name = "with_token"
		}
		t.Run(name, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			environment := []string{"CODEAF_NO_MODIFY_PATH=1"}
			if token != "" {
				environment = append(environment, token)
			}
			run := runInstaller(t, github, nil, environment...)
			if run.code != 0 {
				t.Fatalf("install exit %d:\n%s", run.code, run.output)
			}
			github.mu.Lock()
			defer github.mu.Unlock()
			downloads := 0
			for _, request := range github.requests {
				if strings.Contains(request.URL.Path, "/releases/download/") {
					downloads++
					if authorization := request.Header.Get("Authorization"); authorization != "" {
						t.Errorf("download %s has Authorization %q", request.URL.Path, authorization)
					}
				}
			}
			if downloads != 2 {
				t.Fatalf("download requests = %d, want 2", downloads)
			}
		})
	}
}

func TestInstallerDoesNotPrintTheTokenWithVerboseOutput(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	const token = "secret-installer-token"
	run := runInstaller(t, github, nil, "GITHUB_TOKEN="+token, "VERBOSE=1", "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 || strings.Contains(run.output, token) {
		t.Fatalf("verbose install exit %d or leaked token:\n%s", run.code, run.output)
	}
}

func TestInstallerHelpUnknownFlagsAndAPIFailures(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	help := runInstaller(t, github, []string{"--help"})
	if help.code != 0 || !strings.Contains(help.output, "Channels:") || !strings.Contains(help.output, "Environment:") {
		t.Fatalf("help exit %d:\n%s", help.code, help.output)
	}
	unknown := runInstaller(t, github, []string{"--unknown"})
	if unknown.code != 2 || !strings.Contains(unknown.output, "Usage:") {
		t.Fatalf("unknown exit %d:\n%s", unknown.code, unknown.output)
	}
	github.failAPI = true
	failure := runInstaller(t, github, nil)
	if failure.code != 1 || !strings.Contains(failure.output, "GITHUB_TOKEN") || !strings.Contains(failure.output, "VERSION=") {
		t.Fatalf("API failure exit %d:\n%s", failure.code, failure.output)
	}
}

// Contract: an API refusal names rate limiting and an unreadable repository,
// while retaining both available recovery actions.
func TestInstallerNamesBothReasonsWhenTheAPIRefuses(t *testing.T) {
	for _, test := range []struct {
		name     string
		prepare  func(*installGitHub)
		wantLine bool
	}{
		{name: "rate limit", prepare: func(github *installGitHub) { github.failAPI = true }, wantLine: true},
		{name: "private repository", prepare: func(github *installGitHub) { github.notFound = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, "dev-20260915-aaaaaaaaaaaa")
			test.prepare(github)
			run := runInstaller(t, github, []string{"--dev"})
			if run.code != 1 {
				t.Fatalf("exit %d, want 1:\n%s", run.code, run.output)
			}
			for _, want := range []string{"a rate limit", "a repository you cannot read", "VERSION=", "GITHUB_TOKEN"} {
				if !strings.Contains(run.output, want) {
					t.Errorf("output does not contain %q:\n%s", want, run.output)
				}
			}
			if test.wantLine {
				const want = "GitHub's API could not be reached or refused (a rate limit, or a repository you cannot read?); pin VERSION=<tag>, or export GITHUB_TOKEN"
				if !strings.Contains(run.output, want) {
					t.Errorf("output does not contain pinned refusal:\n%s", run.output)
				}
			}
		})
	}
}

func TestInstallerUsesWgetWhenCurlIsAbsent(t *testing.T) {
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget is not installed")
	}
	github := newInstallGitHub(t, "v1.2.3")
	run := runInstaller(t, github, nil, "PATH="+minimalPath(t, false), "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 || !strings.Contains(run.output, "codeaf v1.2.3 · fake") {
		t.Fatalf("wget install exit %d:\n%s", run.code, run.output)
	}
	missing := runInstaller(t, github, []string{"--version", "v9.9.9"}, "PATH="+minimalPath(t, false), "CODEAF_NO_MODIFY_PATH=1")
	if missing.code != 1 || !strings.Contains(missing.output, "v9.9.9") || !strings.Contains(missing.output, "check the tag on the Releases page") {
		t.Fatalf("wget missing release exit %d:\n%s", missing.code, missing.output)
	}
}

func TestInstallerNamesAnUnsupportedPlatform(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	path := minimalPath(t, true)
	uname := filepath.Join(path, "uname")
	if err := os.Remove(uname); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uname, []byte("#!/bin/sh\nprintf 'plan9\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := runInstaller(t, github, nil, "PATH="+path, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 1 || !strings.Contains(run.output, "unsupported platform: plan9") {
		t.Fatalf("unsupported install exit %d:\n%s", run.code, run.output)
	}
}

func TestInstallerKeepsTheWebsiteChannelSeam(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const seams = "CHANNEL=\"${CHANNEL:-stable}\"\nINSTALL_NAME=\"${CODEAF_INSTALL_NAME:-codeaf}\""
	if strings.Count(string(raw), seams) != 1 || strings.Count(string(raw), "INSTALL_NAME=\"${CODEAF_INSTALL_NAME:-codeaf}\"") != 1 {
		t.Fatal("the website-rewritten CHANNEL and INSTALL_NAME lines are not adjacent and unique")
	}
}

// V1: --name and CODEAF_INSTALL_NAME install the selected dev build under the
// requested file, leave codeaf untouched, and reject every invalid name before writing.
func TestV1InstallerName(t *testing.T) {
	const tag = "dev-20260921-bbbbbbbbbbbb"
	github := newInstallGitHub(t, tag)

	t.Run("flag", func(t *testing.T) {
		dir := t.TempDir()
		codeaf := filepath.Join(dir, "codeaf")
		original := []byte("stable stays here")
		if err := os.WriteFile(codeaf, original, 0o755); err != nil {
			t.Fatal(err)
		}
		run := runInstaller(t, github, []string{"--name", "devaf", "--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
		devaf := filepath.Join(dir, "devaf")
		if _, err := os.Stat(devaf); err != nil {
			t.Fatal(err)
		}
		kept, err := os.ReadFile(codeaf)
		if err != nil || string(kept) != string(original) {
			t.Fatalf("codeaf = %q, %v", kept, err)
		}
		// A normal run's receipt names the COMMAND the person will type first,
		// then the installed file naming its build; the last thing said is the
		// line to paste, and the path is --verbose's to say. A devaf install that
		// said "installed codeaf" sent the person to type a command this install
		// never wrote (the fresh-install check of 2026-09-25).
		if !strings.Contains(run.output, "installed devaf · codeaf "+tag+" · fake") {
			t.Fatalf("output does not carry the receipt naming devaf:\n%s", run.output)
		}
		for _, line := range strings.Split(run.output, "\n") {
			if strings.HasPrefix(line, "installed codeaf") {
				t.Fatalf("a devaf install's receipt says %q:\n%s", line, run.output)
			}
		}
		if got := strings.Split(strings.TrimSpace(run.output), "\n"); got[len(got)-1] != `export PATH="`+dir+`:$PATH"` {
			t.Fatalf("last line = %q:\n%s", got[len(got)-1], run.output)
		}
		verbose := runInstaller(t, github, []string{"--name", "devaf", "--dev", "--verbose"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_NO_MODIFY_PATH=1")
		if verbose.code != 0 || !strings.Contains(verbose.output, "codeaf: installed "+devaf) {
			t.Fatalf("verbose output does not name %s (exit %d):\n%s", devaf, verbose.code, verbose.output)
		}
	})

	t.Run("environment and flag precedence", func(t *testing.T) {
		dir := t.TempDir()
		fromEnv := runInstaller(t, github, []string{"--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_INSTALL_NAME=devaf", "CODEAF_NO_MODIFY_PATH=1")
		if fromEnv.code != 0 {
			t.Fatalf("environment exit %d:\n%s", fromEnv.code, fromEnv.output)
		}
		if _, err := os.Stat(filepath.Join(dir, "devaf")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fromEnv.output, "installed devaf · codeaf "+tag+" · fake") {
			t.Fatalf("an install named from the environment does not name devaf in its receipt:\n%s", fromEnv.output)
		}
		flag := runInstaller(t, github, []string{"--name", "mine", "--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_INSTALL_NAME=ignored", "CODEAF_NO_MODIFY_PATH=1")
		if flag.code != 0 {
			t.Fatalf("flag exit %d:\n%s", flag.code, flag.output)
		}
		if _, err := os.Stat(filepath.Join(dir, "mine")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "ignored")); !os.IsNotExist(err) {
			t.Fatalf("environment overrode flag: %v", err)
		}
	})

	for _, invalid := range []string{"../x", "-x", ""} {
		t.Run("invalid "+invalid, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "install")
			run := runInstaller(t, github, []string{"--name", invalid, "--dev"},
				"CODEAF_INSTALL_DIR="+dir, "CODEAF_NO_MODIFY_PATH=1")
			if run.code != 2 || !strings.Contains(run.output, "must match ^[A-Za-z0-9][A-Za-z0-9._-]*$") {
				t.Fatalf("exit %d:\n%s", run.code, run.output)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("invalid name wrote install directory: %v", err)
			}
		})
	}

	t.Run("invalid from the environment", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "install")
		run := runInstaller(t, github, []string{"--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_INSTALL_NAME=../x", "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 2 || !strings.Contains(run.output, "must match ^[A-Za-z0-9][A-Za-z0-9._-]*$") {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("invalid environment name wrote install directory: %v", err)
		}
	})

	t.Run("no word after the flag", func(t *testing.T) {
		run := runInstaller(t, github, []string{"--name"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 2 || !strings.Contains(run.output, "--name needs a word") {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
	})
}

// V2: The website name seam is the one exact line beneath CHANNEL, and the
// installer's help names both ways to choose it.
func TestV2InstallerNameSeamAndHelp(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const nameLine = "INSTALL_NAME=\"${CODEAF_INSTALL_NAME:-codeaf}\""
	if strings.Count(string(raw), nameLine) != 1 || !strings.Contains(string(raw), "CHANNEL=\"${CHANNEL:-stable}\"\n"+nameLine+"\n") {
		t.Fatal("the installer name seam is not unique and directly below CHANNEL")
	}
	github := newInstallGitHub(t, "v1.2.3")
	help := runInstaller(t, github, []string{"--help"})
	if help.code != 0 || !strings.Contains(help.output, "--name WORD") || !strings.Contains(help.output, "CODEAF_INSTALL_NAME") {
		t.Fatalf("help exit %d:\n%s", help.code, help.output)
	}
}
