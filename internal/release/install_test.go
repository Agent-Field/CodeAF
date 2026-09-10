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
	ID      int         `json:"id,omitempty"`
	Name    string      `json:"name,omitempty"`
	TagName string      `json:"tag_name"`
	Assets  []fakeAsset `json:"assets"`
}

type fakeAsset struct {
	ID          int          `json:"id"`
	URL         string       `json:"url"`
	NodeID      string       `json:"node_id"`
	Name        string       `json:"name"`
	Label       string       `json:"label"`
	Uploader    fakeUploader `json:"uploader"`
	ContentType string       `json:"content_type"`
	State       string       `json:"state"`
	Size        int          `json:"size"`
}

type fakeUploader struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
	Name  string `json:"name,omitempty"`
}

type installGitHub struct {
	t             *testing.T
	server        *httptest.Server
	releases      []string
	missing       map[string]bool
	badChecksum   bool
	failAPI       bool
	prettyJSON    bool
	nestedName    bool
	releaseName   bool
	omitPlatform  bool
	assetRedirect string
	mu            sync.Mutex
	requests      []*http.Request
	assetRequests []string
}

func newInstallGitHub(t *testing.T, releases ...string) *installGitHub {
	t.Helper()
	github := &installGitHub{t: t, releases: releases, missing: make(map[string]bool)}
	github.server = httptest.NewServer(http.HandlerFunc(github.serve))
	t.Cleanup(github.server.Close)
	return github
}

func (github *installGitHub) serve(w http.ResponseWriter, request *http.Request) {
	github.mu.Lock()
	github.requests = append(github.requests, request.Clone(request.Context()))
	github.mu.Unlock()

	path := request.URL.Path
	const prefix = "/repos/Agent-Field/aforge-v2/"
	if strings.HasPrefix(path, prefix) {
		if github.failAPI {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		switch {
		case path == prefix+"releases/latest":
			if len(github.releases) == 0 {
				http.NotFound(w, request)
				return
			}
			github.writeRelease(w, github.releases[0])
			return
		case path == prefix+"releases":
			var releases []fakeRelease
			for _, tag := range github.releases {
				releases = append(releases, github.release(tag))
			}
			github.writeJSON(w, releases)
			return
		case strings.HasPrefix(path, prefix+"releases/tags/"):
			tag := strings.TrimPrefix(path, prefix+"releases/tags/")
			if github.missing[tag] || !contains(github.releases, tag) {
				http.NotFound(w, request)
				return
			}
			github.writeRelease(w, tag)
			return
		case strings.HasPrefix(path, prefix+"releases/assets/"):
			id := strings.TrimPrefix(path, prefix+"releases/assets/")
			github.mu.Lock()
			github.assetRequests = append(github.assetRequests, id)
			github.mu.Unlock()
			name, tag := github.assetForID(id)
			if name == "" {
				http.NotFound(w, request)
				return
			}
			if github.assetRedirect != "" {
				http.Redirect(w, request, github.assetRedirect+"/"+name+"?tag="+tag, http.StatusFound)
				return
			}
			github.writeAsset(w, tag, name)
			return
		}
	}

	const downloads = "/Agent-Field/aforge-v2/releases/download/"
	if strings.HasPrefix(path, downloads) {
		rest := strings.TrimPrefix(path, downloads)
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

func (github *installGitHub) release(tag string) fakeRelease {
	asset := platformAsset()
	base := releaseID(tag) * 10
	assets := make([]fakeAsset, 0, 2)
	if !github.omitPlatform {
		assets = append(assets, github.asset(base+1, asset, len(fakeBinary(tag))))
	}
	assets = append(assets, github.asset(base+2, "checksums.txt", 80))
	release := fakeRelease{TagName: tag, Assets: assets}
	if github.releaseName {
		release.ID = base + 9
		release.Name = asset
	}
	return release
}

func (github *installGitHub) asset(id int, name string, size int) fakeAsset {
	uploader := fakeUploader{Login: "release-bot", ID: 7}
	if github.nestedName {
		uploader.Name = name
	}
	return fakeAsset{
		ID:          id,
		URL:         fmt.Sprintf("%s/repos/Agent-Field/aforge-v2/releases/assets/%d", github.server.URL, id),
		NodeID:      fmt.Sprintf("RA_%d", id),
		Name:        name,
		Label:       "",
		Uploader:    uploader,
		ContentType: "application/octet-stream",
		State:       "uploaded",
		Size:        size,
	}
}

func (github *installGitHub) writeRelease(w http.ResponseWriter, tag string) {
	github.writeJSON(w, github.release(tag))
}

func (github *installGitHub) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	if github.prettyJSON {
		encoder.SetIndent("", "  ")
	}
	_ = encoder.Encode(value)
}

func (github *installGitHub) assetForID(id string) (string, string) {
	for _, tag := range github.releases {
		for _, asset := range github.release(tag).Assets {
			if fmt.Sprint(asset.ID) == id {
				return asset.Name, tag
			}
		}
	}
	return "", ""
}

func (github *installGitHub) writeAsset(w http.ResponseWriter, tag, name string) {
	binary := fakeBinary(tag)
	switch name {
	case platformAsset():
		_, _ = w.Write(binary)
	case "checksums.txt":
		digest := sha256.Sum256(binary)
		checksum := hex.EncodeToString(digest[:])
		if github.badChecksum {
			checksum = strings.Repeat("0", len(checksum))
		}
		fmt.Fprintf(w, "%s  %s\n", checksum, platformAsset())
	default:
		http.NotFound(w, nil)
	}
}

func releaseID(tag string) int {
	value := 1
	for _, char := range tag {
		value += int(char)
	}
	return value
}

func fakeBinary(tag string) []byte {
	return []byte("#!/bin/sh\nprintf 'aforge " + tag + " · fake\\n'\n")
}

func platformAsset() string {
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	return "aforge-" + runtime.GOOS + "-" + runtime.GOARCH + extension
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
		"AFORGE_INSTALL_DIR=" + installDir,
		"AFORGE_GITHUB_API=" + github.server.URL,
		"AFORGE_GITHUB_DOWNLOAD=" + github.server.URL,
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
	commands := []string{"awk", "basename", "cat", "chmod", "cp", "dirname", "grep", "mkdir", "mktemp", "mv", "rm", "sed", "sha256sum", "shasum", "tr", "uname", "wget"}
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
	run := runInstaller(t, github, nil, "AFORGE_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("exit %d:\n%s", run.code, run.output)
	}
	for _, want := range []string{"stable v1.2.3", runtime.GOOS + "/" + runtime.GOARCH, "aforge v1.2.3 · fake"} {
		if !strings.Contains(run.output, want) {
			t.Errorf("output does not contain %q:\n%s", want, run.output)
		}
	}
	if _, err := os.Stat(filepath.Join(run.installDir, "aforge")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.output, "export PATH=") {
		t.Fatalf("the no-modify-path run did not print the line to paste:\n%s", run.output)
	}
	if _, err := os.Stat(filepath.Join(run.home, ".bashrc")); !os.IsNotExist(err) {
		t.Fatalf("--no-modify-path edited the shell file: %v", err)
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
			environment := []string{"AFORGE_NO_MODIFY_PATH=1"}
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

func TestInstallerPinsAReleaseAndNamesAMissingOne(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3", "build-legacy")
	run := runInstaller(t, github, []string{"--version", "v1.2.3"}, "AFORGE_NO_MODIFY_PATH=1")
	if run.code != 0 || !strings.Contains(run.output, "stable v1.2.3") {
		t.Fatalf("exit %d:\n%s", run.code, run.output)
	}
	fromEnvironment := runInstaller(t, github, nil, "VERSION=v1.2.3", "AFORGE_NO_MODIFY_PATH=1")
	if fromEnvironment.code != 0 || !strings.Contains(fromEnvironment.output, "stable v1.2.3") {
		t.Fatalf("VERSION install exit %d:\n%s", fromEnvironment.code, fromEnvironment.output)
	}
	legacy := runInstaller(t, github, []string{"--version", "build-legacy"}, "AFORGE_NO_MODIFY_PATH=1")
	wantLegacy := "aforge: build-legacy for " + runtime.GOOS + "/" + runtime.GOARCH
	if legacy.code != 0 || !strings.Contains(legacy.output, wantLegacy) || strings.Contains(legacy.output, "aforge: version ") {
		t.Fatalf("legacy-tag install exit %d:\n%s", legacy.code, legacy.output)
	}

	missing := runInstaller(t, github, []string{"--version", "v9.9.9"}, "AFORGE_NO_MODIFY_PATH=1")
	if missing.code != 1 || !strings.Contains(missing.output, "v9.9.9") || !strings.Contains(missing.output, "/releases") {
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
		"AFORGE_INSTALL_DIR=" + installDir,
		"AFORGE_GITHUB_API=" + github.server.URL,
		"AFORGE_GITHUB_DOWNLOAD=" + github.server.URL,
		"AFORGE_NO_MODIFY_PATH=1",
		"PATH=" + path,
		"SHELL=/bin/bash",
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "stable v1.2.3") || strings.Contains(string(output), "v9.9.9") {
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
			want := "aforge: no " + test.name + " build has been published yet"
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
	run := runInstaller(t, github, nil, "AFORGE_INSTALL_DIR="+badDir, "AFORGE_NO_MODIFY_PATH=1")
	if run.code != 1 || !strings.Contains(run.output, "checksum") {
		t.Fatalf("checksum refusal: exit %d:\n%s", run.code, run.output)
	}
	if _, err := os.Stat(filepath.Join(badDir, "aforge")); !os.IsNotExist(err) {
		t.Fatalf("a failed checksum left a binary: %v", err)
	}

	github.badChecksum = false
	home := t.TempDir()
	dir := filepath.Join(home, "install")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aforge"), []byte("old running binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := runInstaller(t, github, nil, "HOME="+home, "AFORGE_INSTALL_DIR="+dir)
	second := runInstaller(t, github, nil, "HOME="+home, "AFORGE_INSTALL_DIR="+dir)
	if first.code != 0 || second.code != 0 {
		t.Fatalf("repeat exits %d and %d:\n%s\n%s", first.code, second.code, first.output, second.output)
	}
	rc, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rc), "# aforge installer") != 1 {
		t.Fatalf("PATH edit is not idempotent:\n%s", rc)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".aforge.tmp.*")); len(matches) != 0 {
		t.Fatalf("atomic install left temporary files: %v", matches)
	}
	installed, err := os.ReadFile(filepath.Join(dir, "aforge"))
	if err != nil || strings.Contains(string(installed), "old running binary") {
		t.Fatalf("the old binary was not replaced: %v %q", err, installed)
	}
}

func TestInstallerUsesAuthenticatedAssetRoutesWithoutLeakingTheToken(t *testing.T) {
	for _, pretty := range []bool{false, true} {
		name := "compact"
		if pretty {
			name = "pretty"
		}
		t.Run(name, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			github.prettyJSON = pretty
			const token = "secret-installer-token"
			run := runInstaller(t, github, nil, "GITHUB_TOKEN="+token, "VERBOSE=1", "AFORGE_NO_MODIFY_PATH=1")
			if run.code != 0 || strings.Contains(run.output, token) {
				t.Fatalf("authenticated install exit %d or leaked token:\n%s", run.code, run.output)
			}
			github.mu.Lock()
			defer github.mu.Unlock()
			if len(github.assetRequests) != 2 {
				t.Fatalf("asset API requests = %v", github.assetRequests)
			}
			for _, request := range github.requests {
				if strings.Contains(request.URL.Path, "/repos/") && request.Header.Get("Authorization") != "Bearer "+token {
					t.Errorf("API request %s has Authorization %q", request.URL.Path, request.Header.Get("Authorization"))
				}
				if strings.Contains(request.URL.Path, "/releases/assets/") && request.Header.Get("Accept") != "application/octet-stream" {
					t.Errorf("asset request Accept = %q", request.Header.Get("Accept"))
				}
			}
		})
	}
}

func TestInstallerFindsOnlyDirectAssetsDespiteNestedMatchingNames(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.nestedName = true
	run := runInstaller(t, github, nil, "GITHUB_TOKEN=secret-installer-token", "AFORGE_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("nested-name install exit %d:\n%s", run.code, run.output)
	}
	github.mu.Lock()
	defer github.mu.Unlock()
	if len(github.assetRequests) != 2 || contains(github.assetRequests, "7") {
		t.Fatalf("nested uploader IDs were treated as assets: %v", github.assetRequests)
	}
}

func TestInstallerDoesNotTreatAReleaseTitleAsAnAsset(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.releaseName = true
	github.omitPlatform = true
	run := runInstaller(t, github, nil, "GITHUB_TOKEN=secret-installer-token", "AFORGE_NO_MODIFY_PATH=1")
	want := "release v1.2.3 has no " + platformAsset() + " asset"
	if run.code != 1 || !strings.Contains(run.output, want) {
		t.Fatalf("release-title collision exit %d:\n%s", run.code, run.output)
	}
	github.mu.Lock()
	defer github.mu.Unlock()
	if len(github.assetRequests) != 0 {
		t.Fatalf("a release ID was requested as an asset: %v", github.assetRequests)
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

func TestInstallerUsesWgetWhenCurlIsAbsent(t *testing.T) {
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget is not installed")
	}
	github := newInstallGitHub(t, "v1.2.3")
	run := runInstaller(t, github, nil, "PATH="+minimalPath(t, false), "AFORGE_NO_MODIFY_PATH=1")
	if run.code != 0 || !strings.Contains(run.output, "aforge v1.2.3 · fake") {
		t.Fatalf("wget install exit %d:\n%s", run.code, run.output)
	}
	missing := runInstaller(t, github, []string{"--version", "v9.9.9"}, "PATH="+minimalPath(t, false), "AFORGE_NO_MODIFY_PATH=1")
	if missing.code != 1 || !strings.Contains(missing.output, "v9.9.9") || !strings.Contains(missing.output, "/releases") {
		t.Fatalf("wget missing release exit %d:\n%s", missing.code, missing.output)
	}
}

func TestInstallerWgetDropsAuthorizationBeforeFollowingAssetRedirect(t *testing.T) {
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget is not installed")
	}
	github := newInstallGitHub(t, "v1.2.3")
	var mu sync.Mutex
	var redirectedAuthorization []string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		redirectedAuthorization = append(redirectedAuthorization, request.Header.Get("Authorization"))
		mu.Unlock()
		github.writeAsset(w, request.URL.Query().Get("tag"), strings.TrimPrefix(request.URL.Path, "/"))
	}))
	t.Cleanup(target.Close)
	github.assetRedirect = target.URL
	run := runInstaller(t, github, nil,
		"PATH="+minimalPath(t, false),
		"GITHUB_TOKEN=secret-installer-token",
		"AFORGE_NO_MODIFY_PATH=1",
	)
	if run.code != 0 {
		t.Fatalf("redirected wget install exit %d:\n%s", run.code, run.output)
	}
	github.mu.Lock()
	assetAPIRequests := 0
	for _, request := range github.requests {
		if strings.Contains(request.URL.Path, "/releases/assets/") {
			assetAPIRequests++
			if request.Header.Get("Authorization") != "Bearer secret-installer-token" {
				t.Errorf("asset API request has Authorization %q", request.Header.Get("Authorization"))
			}
		}
	}
	github.mu.Unlock()
	if assetAPIRequests != 2 {
		t.Fatalf("asset API requests = %d, want 2", assetAPIRequests)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(redirectedAuthorization) != 2 {
		t.Fatalf("redirect target requests = %d, want 2", len(redirectedAuthorization))
	}
	for _, authorization := range redirectedAuthorization {
		if authorization != "" {
			t.Fatalf("redirect target received Authorization %q", authorization)
		}
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
	run := runInstaller(t, github, nil, "PATH="+path, "AFORGE_NO_MODIFY_PATH=1")
	if run.code != 1 || !strings.Contains(run.output, "unsupported platform: plan9") {
		t.Fatalf("unsupported install exit %d:\n%s", run.code, run.output)
	}
}

func TestInstallerKeepsTheWebsiteChannelSeam(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\nCHANNEL=\"${CHANNEL:-stable}\"\n") {
		t.Fatal("the website-rewritten CHANNEL line is missing")
	}
}
