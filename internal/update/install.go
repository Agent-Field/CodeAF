package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	shownAsset := asset
	body, err := c.downloadAsset(ctx, c.assetURL(release, asset), shownAsset)
	if isStatus(err, http.StatusNotFound) {
		legacyAsset := "aforge-" + goos + "-" + goarch + extension // legacy-name
		body, err = c.downloadAsset(ctx, c.assetURL(release, legacyAsset), shownAsset)
		if err == nil {
			asset = legacyAsset
		}
	}
	if err != nil {
		return nil, asset, nil, err
	}
	checksums, err := c.get(ctx, c.assetURL(release, "checksums.txt"), "application/octet-stream", false, "checksums.txt")
	if err != nil {
		return nil, asset, nil, err
	}
	return body, asset, checksums, nil
}

type downloadRead struct {
	bytes []byte
	err   error
}

func (c *Client) downloadAsset(ctx context.Context, rawURL, asset string) ([]byte, error) {
	ceiling := c.downloadCeiling()
	downloadContext, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()

	request, err := http.NewRequestWithContext(downloadContext, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%s could not start arriving", asset)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", c.userAgent())
	response, err := c.requestClient().Do(request)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, fmt.Errorf("downloading %s was stopped", asset)
		case errors.Is(downloadContext.Err(), context.DeadlineExceeded):
			return nil, downloadCeilingError(asset, ceiling)
		case responseHeaderTimedOut(err):
			return nil, fmt.Errorf("%s did not start arriving within %s", asset, spellDuration(apiRequestTimeout))
		default:
			return nil, fmt.Errorf("%s could not start arriving", asset)
		}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &statusError{code: response.StatusCode, resource: asset}
	}
	return readDownloadBody(ctx, downloadContext, response.Body, asset, c.stallWindow(), ceiling)
}

func responseHeaderTimedOut(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout awaiting response headers")
}

// readDownloadBody watches successful reads rather than wall time. A slow link
// that keeps delivering bytes is healthy; one silent read must not hold the
// installer forever even though the HTTP client has no whole-body timeout.
func readDownloadBody(parent, download context.Context, body io.Reader, asset string, stall, ceiling time.Duration) ([]byte, error) {
	reads := make(chan downloadRead, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			count, err := body.Read(buffer)
			var copied []byte
			if count > 0 {
				copied = append([]byte(nil), buffer[:count]...)
			}
			select {
			case reads <- downloadRead{bytes: copied, err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	timer := time.NewTimer(stall)
	defer timer.Stop()
	var downloaded []byte
	for {
		select {
		case <-download.Done():
			if parent.Err() != nil {
				return nil, fmt.Errorf("downloading %s was stopped", asset)
			}
			return nil, downloadCeilingError(asset, ceiling)
		case <-timer.C:
			return nil, downloadStallError(asset, stall)
		case read := <-reads:
			if len(read.bytes) > 0 {
				downloaded = append(downloaded, read.bytes...)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(stall)
			}
			if read.err == nil {
				continue
			}
			if errors.Is(read.err, io.EOF) {
				return downloaded, nil
			}
			if parent.Err() != nil {
				return nil, fmt.Errorf("downloading %s was stopped", asset)
			}
			if errors.Is(download.Err(), context.DeadlineExceeded) {
				return nil, downloadCeilingError(asset, ceiling)
			}
			return nil, fmt.Errorf("downloading %s stopped before it finished", asset)
		}
	}
}

func downloadStallError(asset string, window time.Duration) error {
	return fmt.Errorf("downloading %s stalled — no bytes for %s", asset, spellDuration(window))
}

func downloadCeilingError(asset string, ceiling time.Duration) error {
	return fmt.Errorf("downloading %s took longer than %s", asset, spellDuration(ceiling))
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
