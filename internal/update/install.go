package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallOptions describes one in-place replacement.
type InstallOptions struct {
	Client  *Client
	Release Release
	Target  string
	GOOS    string
	GOARCH  string
}

// InstallResult is the release and executable path that are ready to restart.
type InstallResult struct {
	Release Release
	Path    string
}

// Install downloads, checks, and atomically replaces one executable.
func Install(ctx context.Context, options InstallOptions) (InstallResult, error) {
	if options.Client == nil {
		return InstallResult{}, errors.New("no release client is available")
	}
	target := strings.TrimSpace(options.Target)
	if target == "" {
		return InstallResult{}, errors.New("the running executable path is empty")
	}
	goos, goarch := options.GOOS, options.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (goarch != "amd64" && goarch != "arm64") {
		return InstallResult{}, fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}
	extension := ""
	if goos == "windows" {
		extension = ".exe"
	}
	asset := "codeaf-" + goos + "-" + goarch + extension

	release := options.Release
	if release.Tag == "" {
		return InstallResult{}, errors.New("the release tag is empty")
	}
	if release.Repository == "" {
		release.Repository = primaryRepository
	}
	var (
		body      []byte
		checksums []byte
		err       error
	)
	for index, repository := range installRepositories(release.Repository) {
		release.Repository = repository
		body, asset, checksums, err = options.Client.downloadRelease(ctx, release, asset, goos, goarch, extension)
		if err == nil {
			break
		}
		if !isStatus(err, http.StatusNotFound) || index == 1 {
			return InstallResult{}, err
		}
	}
	if err != nil {
		return InstallResult{}, err
	}
	expected, ok := checksumFor(checksums, asset)
	if !ok {
		return InstallResult{}, fmt.Errorf("checksums.txt has no checksum for %s", asset)
	}
	actualBytes := sha256.Sum256(body)
	actual := hex.EncodeToString(actualBytes[:])
	if !strings.EqualFold(expected, actual) {
		return InstallResult{}, fmt.Errorf("the checksum for %s did not match", asset)
	}

	temporaryFile, err := os.CreateTemp(filepath.Dir(target), fmt.Sprintf(".codeaf.tmp.%d.", os.Getpid()))
	if err != nil {
		return InstallResult{}, fmt.Errorf("cannot replace %s: %w; install a release with: %s", target, err, CurlCommand)
	}
	temporary := temporaryFile.Name()
	defer os.Remove(temporary)
	_, writeErr := temporaryFile.Write(body)
	closeErr := temporaryFile.Close()
	if writeErr != nil {
		return InstallResult{}, fmt.Errorf("write %s: %w", temporary, writeErr)
	}
	if closeErr != nil {
		return InstallResult{}, fmt.Errorf("close %s: %w", temporary, closeErr)
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("make %s executable: %w", temporary, err)
	}
	if err := replaceExecutable(temporary, target); err != nil {
		return InstallResult{}, fmt.Errorf("cannot replace %s: %w; install a release with: %s", target, err, CurlCommand)
	}
	return InstallResult{Release: release, Path: target}, nil
}

func installRepositories(first string) []string {
	if first == legacyRepository {
		return []string{legacyRepository}
	}
	return []string{primaryRepository, legacyRepository} // legacy-name
}

func (c *Client) downloadRelease(ctx context.Context, release Release, asset, goos, goarch, extension string) ([]byte, string, []byte, error) {
	body, err := c.get(ctx, c.assetURL(release, asset), "application/octet-stream", false)
	if isStatus(err, http.StatusNotFound) {
		legacyAsset := "aforge-" + goos + "-" + goarch + extension // legacy-name
		body, err = c.get(ctx, c.assetURL(release, legacyAsset), "application/octet-stream", false)
		if err == nil {
			asset = legacyAsset
		}
	}
	if err != nil {
		return nil, asset, nil, err
	}
	checksums, err := c.get(ctx, c.assetURL(release, "checksums.txt"), "application/octet-stream", false)
	if err != nil {
		return nil, asset, nil, err
	}
	return body, asset, checksums, nil
}

func checksumFor(raw []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		candidate := strings.TrimPrefix(fields[1], "*")
		if candidate == name {
			if decoded, err := hex.DecodeString(fields[0]); err == nil && len(decoded) == sha256.Size {
				return strings.ToLower(fields[0]), true
			}
		}
	}
	return "", false
}

// ExecutableTarget resolves the running binary before an installer replaces it.
func ExecutableTarget(executable func() (string, error)) (string, error) {
	if executable == nil {
		executable = os.Executable
	}
	path, err := executable()
	if err != nil {
		return "", fmt.Errorf("find the running codeaf: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	return path, nil
}
