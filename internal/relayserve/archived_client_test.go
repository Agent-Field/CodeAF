package relayserve_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// R25/R29 execute an archived pre-conditional HTTP client unchanged, with only
// its unrelated types supplied by a bridge. Its blind force must be refused,
// ordinary save/release/revoke must work, and large archived bytes must match.
func TestR25ArchivedPreconditionalClient(t *testing.T) {
	data := filepath.Join("testdata", "preconditional")
	raw, err := os.ReadFile(filepath.Join(data, "archive.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SHA map[string]string `json:"sha256"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		t.Fatal("archive manifest")
	}
	dir := t.TempDir()
	if err = os.Mkdir(filepath.Join(dir, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, want := range manifest.SHA {
		b, err := os.ReadFile(filepath.Join(data, name+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(b)
		if hex.EncodeToString(hash[:]) != want {
			t.Fatal("archived client changed")
		}
		if err = os.WriteFile(filepath.Join(dir, "directory", name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, one := range []struct{ src, dst string }{{"bridge.go.txt", "directory/bridge.go"}, {"main.go.txt", "main.go"}} {
		b, err := os.ReadFile(filepath.Join(data, one.src))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, one.dst), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Carry the repository's pruned module graph, checksums, and local raft
	// replacement into the probe module so -mod=readonly resolves every
	// package from the populated module cache without querying GOPROXY=off
	// for pruned transitive go.mod files, and disable VCS stamping outside
	// the checkout.
	rootMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	rootSum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	mod := strings.Replace(string(rootMod), "module github.com/Agent-Field/codeaf\n", "module github.com/Agent-Field/codeaf/internal/archiveprobe\n", 1)
	mod = strings.Replace(mod, "replace go.etcd.io/raft/v3 => ./third_party/raft", "replace go.etcd.io/raft/v3 => "+filepath.Join(root, "third_party", "raft"), 1)
	mod += "\nrequire github.com/Agent-Field/codeaf v0.0.0\n\nreplace github.com/Agent-Field/codeaf => " + root + "\n"
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "go.sum"), rootSum, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "client")
	build := exec.Command("go", "build", "-buildvcs=false", "-mod=readonly", "-p=1", "-o", binary, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("archived client build: %v\n%s", err, out)
	}
	largePath, err := filepath.Abs("../raftdirectory/testdata/v1-large-requests.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(largePath)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Now   int64
		Cases []struct {
			Name, Body string
			Headers    map[string]string
		}
	}
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("large archive")
	}
	var mu sync.Mutex
	matched := map[string]bool{}
	blind := 0
	svc := relayserve.New(relayserve.Config{Store: t.TempDir(), Now: func() time.Time { return time.UnixMilli(f.Now) }})
	defer svc.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/acquire") {
			blind++
			if string(body) != `{"force":true}` {
				t.Errorf("archived blind body changed: %s", body)
			}
		}
		for _, c := range f.Cases {
			if r.Method == "POST" && r.URL.Path == "/v1/dir/cells/"+c.Name {
				if string(body) != c.Body {
					t.Errorf("archived client did not emit frozen %s bytes", c.Name)
				}
				for k, v := range c.Headers {
					if r.Header.Get(k) != v {
						t.Errorf("archived %s proof changed: %s", c.Name, k)
					}
				}
				matched[c.Name] = true
			}
		}
		svc.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	smallPath, err := filepath.Abs("../raftdirectory/testdata/v1-requests.json")
	if err != nil {
		t.Fatal(err)
	}
	run := exec.Command(binary, srv.URL, smallPath, largePath)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("archived client: %v\n%s", err, out)
	}
	if len(matched) != len(f.Cases) || blind != 2 {
		t.Fatal("archived contract calls did not all run")
	}
}
