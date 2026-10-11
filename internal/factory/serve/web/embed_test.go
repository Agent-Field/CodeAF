package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// buildInputs are the files besides src/ whose change changes the build. The
// list and the hash recipe below are scripts/srchash.mjs's, which writes
// dist/.srchash at the end of every `npm run build`.
var buildInputs = []string{
	"package.json", "package-lock.json", "index.html", "vite.config.ts", "components.json",
	"tsconfig.json", "tsconfig.app.json", "tsconfig.node.json", "scripts/srchash.mjs",
}

func sourceHash(t *testing.T) string {
	t.Helper()
	files := append([]string(nil), buildInputs...)
	err := filepath.WalkDir("src", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, filepath.ToSlash(p))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var lines strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&lines, "%s %x\n", f, sha256.Sum256(b))
	}
	sum := sha256.Sum256([]byte(lines.String()))
	return hex.EncodeToString(sum[:])
}

// TestDistMatchesSources is the law that the committed dist/ is the build of
// the committed sources: a source edit without a rebuild would ship a
// dashboard nobody reviewed.
func TestDistMatchesSources(t *testing.T) {
	built, err := os.ReadFile(filepath.Join("dist", ".srchash"))
	if err != nil {
		t.Fatalf("dist/.srchash is missing: run make web (%v)", err)
	}
	if got, want := sourceHash(t), strings.TrimSpace(string(built)); got != want {
		t.Fatalf("dist/ was built from other sources (dist %s, src %s): run make web", want, got)
	}
}

func TestHandlerFallsBackToTheApp(t *testing.T) {
	index, err := dist.ReadFile("dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/", "/mine", "/jobs/12", "/.srchash", "/assets"} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != string(index) {
			t.Errorf("GET %s = %d, want index.html", p, rec.Code)
		}
	}
}

func TestHandlerServesBuiltAssets(t *testing.T) {
	assets, err := fs.Glob(dist, "dist/assets/*.js")
	if err != nil || len(assets) == 0 {
		t.Fatalf("no built script in dist/assets: run make web (%v)", err)
	}
	p := "/" + strings.TrimPrefix(assets[0], "dist/")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("GET %s = %d %q", p, rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("GET %s Cache-Control = %q, want immutable", p, rec.Header().Get("Cache-Control"))
	}
}
