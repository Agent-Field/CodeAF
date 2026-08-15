package rtk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	releaseBase = "https://github.com/rtk-ai/rtk/releases/download"

	// bootstrapTimeout bounds the whole fetch. A resident that cannot finish a
	// 8MB download in two minutes is on a link where compressing shell output
	// is not the day's problem.
	bootstrapTimeout = 2 * time.Minute

	// maxArchiveBytes refuses an archive far larger than any rtk release, so a
	// wrong or hostile URL cannot fill a user's disk in the background.
	maxArchiveBytes = 64 << 20
)

// targets maps Go's platform names onto the release asset triples. Taken from
// rtk's install.sh — note that linux/arm64 ships gnu while linux/amd64 ships
// musl, which is rtk's choice, not a typo. A platform absent here has no
// release, and the resident simply never wraps anything.
var targets = map[string]string{
	"darwin/arm64": "aarch64-apple-darwin",
	"darwin/amd64": "x86_64-apple-darwin",
	"linux/amd64":  "x86_64-unknown-linux-musl",
	"linux/arm64":  "aarch64-unknown-linux-gnu",
}

// spoken keeps the bootstrap to one line for the life of the process. A helper
// that quietly saves tokens has nothing to say when it works and nothing to
// gain by saying the same thing twice when it does not.
var spoken sync.Once

// Bootstrap fetches the pinned rtk if there is none, and is safe to call from a
// goroutine at startup and to ignore. It never returns an error because there
// is no caller for whom rtk's absence is a failure: every path through here
// ends with commands running exactly as they always have.
func Bootstrap(ctx context.Context) {
	if _, ok := Available(); ok {
		return
	}
	if strings.TrimSpace(os.Getenv(EnvBinary)) != "" {
		// Someone named a binary, or turned this off. Either way the choice has
		// been made and downloading over it would be presumptuous.
		return
	}
	if err := fetch(ctx); err != nil {
		spoken.Do(func() {
			log.Printf("note: shell output will not be compressed — %v", err)
		})
		return
	}
	spoken.Do(func() {
		log.Printf("note: installed rtk %s for compressed shell output", Version)
	})
}

func fetch(ctx context.Context) error {
	target, ok := targets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return fmt.Errorf("rtk has no release for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	dir, err := BinDir()
	if err != nil {
		return fmt.Errorf("resolve the aforge bin directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer cancel()

	asset := "rtk-" + target + ".tar.gz"
	archive, err := download(fetchCtx, releaseBase+"/"+Version+"/"+asset)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	sums, err := download(fetchCtx, releaseBase+"/"+Version+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	if err := verify(archive, sums, asset); err != nil {
		return err
	}
	binary, err := extract(archive)
	if err != nil {
		return err
	}
	return install(dir, binary)
}

func download(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxArchiveBytes))
}

// verify refuses anything the release did not vouch for. An unverified binary
// is not a smaller context window, it is an executable of unknown provenance in
// the user's home directory.
func verify(archive, sums []byte, asset string) error {
	sum := sha256.Sum256(archive)
	actual := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != asset {
			continue
		}
		if fields[0] != actual {
			return fmt.Errorf("checksum mismatch for %s", asset)
		}
		return nil
	}
	return fmt.Errorf("no checksum published for %s", asset)
}

// extract pulls the one file we came for. Naming it exactly rather than walking
// the archive out to disk is what makes a path-traversal entry uninteresting:
// there is nowhere for it to go.
func extract(archive []byte) ([]byte, error) {
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open the rtk archive: %w", err)
	}
	defer func() { _ = unzipped.Close() }()
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read the rtk archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "rtk" || strings.Contains(header.Name, "..") {
			continue
		}
		return io.ReadAll(io.LimitReader(reader, maxArchiveBytes))
	}
	return nil, fmt.Errorf("the rtk archive contained no rtk binary")
}

// install lands the binary atomically. A half-written file that is executable
// is worse than no file at all, because it would resolve.
func install(dir string, binary []byte) error {
	staged, err := os.CreateTemp(dir, ".rtk-*")
	if err != nil {
		return fmt.Errorf("stage rtk: %w", err)
	}
	name := staged.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := staged.Write(binary); err != nil {
		_ = staged.Close()
		return fmt.Errorf("write rtk: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("write rtk: %w", err)
	}
	if err := os.Chmod(name, 0o700); err != nil {
		return fmt.Errorf("make rtk executable: %w", err)
	}
	if err := os.Rename(name, filepath.Join(dir, "rtk")); err != nil {
		return fmt.Errorf("install rtk: %w", err)
	}
	return nil
}
