package resident

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// settledCache is a cache whose clock stands an hour ahead, so files written
// a moment ago are outside the racy window and the signature is trusted —
// each test owns its instance, so no package state is touched.
func settledCache(max int) *digestCache {
	return newDigestCache(max, func() time.Time { return time.Now().Add(time.Hour) })
}

func writeTreeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantDigest(t *testing.T, c *digestCache, dir string) string {
	t.Helper()
	got, err := c.digest(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := contentDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cached digest %s != contentDigest %s", got, want)
	}
	return got
}

func TestDigestCacheReadsAnUnchangedTreeOnce(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		writeTreeFile(t, filepath.Join(dir, "sub", fmt.Sprintf("f%d.txt", i)), "body")
	}
	c := settledCache(8)
	for i := 0; i < 6; i++ {
		wantDigest(t, c, dir)
	}
	if c.reads.Load() != 1 || c.hits.Load() != 5 {
		t.Fatalf("reads=%d hits=%d, want 1 and 5", c.reads.Load(), c.hits.Load())
	}
}

func TestDigestCacheSeesEditsCreatesAndDeletes(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "SKILL.md")
	writeTreeFile(t, file, "aaaa")
	c := settledCache(8)
	first := wantDigest(t, c, dir)

	// Same length, modification time put back: only ctime (and nothing a
	// size/mtime check could see) differs.
	info, _ := os.Stat(file)
	time.Sleep(20 * time.Millisecond) // past the filesystem's ctime granularity
	writeTreeFile(t, file, "bbbb")
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if second := wantDigest(t, c, dir); second == first {
		t.Fatal("edit with preserved size and mtime was not seen")
	}

	extra := filepath.Join(dir, "scripts", "run.sh")
	before := wantDigest(t, c, dir)
	writeTreeFile(t, extra, "x")
	if after := wantDigest(t, c, dir); after == before {
		t.Fatal("created file was not seen")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if after := wantDigest(t, c, dir); after != before {
		t.Fatal("deleted file was not seen")
	}
}

func TestDigestCacheNeverTrustsAFreshlyTouchedTree(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, filepath.Join(dir, "SKILL.md"), "body")
	c := newDigestCache(8, time.Now)
	for i := 0; i < 3; i++ {
		wantDigest(t, c, dir)
	}
	if c.hits.Load() != 0 || c.size() != 0 {
		t.Fatalf("racy tree cached: hits=%d size=%d", c.hits.Load(), c.size())
	}
}

func TestDigestCacheIsBounded(t *testing.T) {
	c := settledCache(3)
	var dirs []string
	for i := 0; i < 10; i++ {
		dir := t.TempDir()
		writeTreeFile(t, filepath.Join(dir, "SKILL.md"), fmt.Sprint(i))
		dirs = append(dirs, dir)
		wantDigest(t, c, dir)
		if c.size() > 3 {
			t.Fatalf("cache grew to %d", c.size())
		}
	}
	reads := c.reads.Load()
	wantDigest(t, c, dirs[9]) // newest survives
	if c.reads.Load() != reads {
		t.Fatal("most recent folder was evicted")
	}
	wantDigest(t, c, dirs[0]) // oldest was evicted, so it is read again
	if c.reads.Load() != reads+1 {
		t.Fatal("evicted folder was not re-read")
	}
}

func TestDigestCacheUnreadableTreeIsAnErrorNotAHit(t *testing.T) {
	c := settledCache(8)
	if _, err := c.digest(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing folder digested without error")
	}
	if c.size() != 0 {
		t.Fatal("an error was cached")
	}
}

func TestImportFlightCoalescesConcurrentLaunches(t *testing.T) {
	flight := newImportFlight()
	key := importKey{project: "p", home: "h"}
	var passes atomic.Int64
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	pass := func() {
		if passes.Add(1) == 1 {
			entered <- struct{}{}
			<-release
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); flight.do(key, pass) }()
	<-entered
	for i := 0; i < 31; i++ { // all arrive while the first pass is mid-scan
		wg.Add(1)
		go func() { defer wg.Done(); flight.do(key, pass) }()
	}
	// Release only once every caller has counted itself in behind the pass.
	for flight.waiting(key) < 32 {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()
	if n := passes.Load(); n != 2 {
		t.Fatalf("32 concurrent launches ran %d passes, want the running one plus one trailing", n)
	}
	if flight.idle() != 0 {
		t.Fatalf("%d entries leaked", flight.idle())
	}
	flight.do(key, pass)
	if passes.Load() != 3 {
		t.Fatal("a later, quiet launch did not get its own pass")
	}
}

func TestImportFlightKeepsWorkspacesApart(t *testing.T) {
	flight := newImportFlight()
	a, b := make(chan struct{}), make(chan struct{})
	done := make(chan struct{}, 2)
	go func() { flight.do(importKey{project: "a"}, func() { close(a); <-b }); done <- struct{}{} }()
	go func() { flight.do(importKey{project: "b"}, func() { close(b); <-a }); done <- struct{}{} }()
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("two workspaces' passes serialised behind one another")
		}
	}
	if flight.idle() != 0 {
		t.Fatal("entries leaked")
	}
}

func TestImportPassUsesTheCacheAndStillTracksTheDisk(t *testing.T) {
	homeDir, projectDir := t.TempDir(), t.TempDir()
	root := filepath.Join(homeDir, ".claude", "skills")
	pdf := filepath.Join(root, "pdf")
	writeImportedSkill(t, pdf, "pdf", "Fill PDF forms")
	writeTreeFile(t, filepath.Join(pdf, "scripts", "a.py"), "print(1)")
	graph := openStore(t)
	c := settledCache(16)

	importPass(graph, projectDir, homeDir, c)
	first, ok := importedFactByArtifact(t, graph, pdf)
	if !ok {
		t.Fatal("skill not imported")
	}
	importPass(graph, projectDir, homeDir, c)
	if c.reads.Load() != 1 || c.hits.Load() != 1 {
		t.Fatalf("unchanged rescan: reads=%d hits=%d", c.reads.Load(), c.hits.Load())
	}
	if again, _ := importedFactByArtifact(t, graph, pdf); again.Seq != first.Seq {
		t.Fatal("unchanged rescan journaled a new fact")
	}

	// A bundled script edited without touching SKILL.md, size and mtime kept.
	script := filepath.Join(pdf, "scripts", "a.py")
	info, _ := os.Stat(script)
	time.Sleep(20 * time.Millisecond)
	writeTreeFile(t, script, "print(2)")
	_ = os.Chtimes(script, info.ModTime(), info.ModTime())
	importPass(graph, projectDir, homeDir, c)
	if edited, _ := importedFactByArtifact(t, graph, pdf); edited.Seq == first.Seq || edited.Digest == first.Digest {
		t.Fatal("edit to a bundled file was not re-imported")
	}

	docx := filepath.Join(root, "docx")
	writeImportedSkill(t, docx, "docx", "Edit Word files")
	importPass(graph, projectDir, homeDir, c)
	if _, ok := importedFactByArtifact(t, graph, docx); !ok {
		t.Fatal("added skill not imported")
	}

	if err := os.RemoveAll(pdf); err != nil {
		t.Fatal(err)
	}
	importPass(graph, projectDir, homeDir, c)
	if _, ok := importedFactByArtifact(t, graph, pdf); ok {
		t.Fatal("deleted skill still active")
	}
	if _, ok := importedFactByArtifact(t, graph, docx); !ok {
		t.Fatal("surviving skill lost")
	}
}

func benchShelf(b *testing.B) (homeDir, projectDir string) {
	homeDir, projectDir = b.TempDir(), b.TempDir()
	for s := 0; s < 40; s++ {
		dir := filepath.Join(homeDir, ".claude", "skills", fmt.Sprintf("skill%02d", s))
		body := "---\nname: " + filepath.Base(dir) + "\ndescription: bench\n---\n"
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
		for f := 0; f < 25; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("ref%02d.md", f)), make([]byte, 8<<10), 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
	return homeDir, projectDir
}

// BenchmarkContentDigestEveryFolder is what every launch paid before the cache:
// every byte of every folder read.
func BenchmarkContentDigestEveryFolder(b *testing.B) {
	homeDir, _ := benchShelf(b)
	folders, _ := filepath.Glob(filepath.Join(homeDir, ".claude", "skills", "*"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, dir := range folders {
			if _, err := contentDigest(dir); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkImportUnchangedShelf(b *testing.B) {
	homeDir, projectDir := benchShelf(b)
	graph, err := store.Open(filepath.Join(b.TempDir(), "g.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer graph.Close()
	c := settledCache(64)
	importPass(graph, projectDir, homeDir, c) // import and warm
	b.ReportAllocs()
	b.ResetTimer()
	reads := c.reads.Load()
	for i := 0; i < b.N; i++ {
		importPass(graph, projectDir, homeDir, c)
	}
	b.ReportMetric(float64(c.reads.Load()-reads)/float64(b.N), "folder-reads/op")
	b.ReportMetric(float64(1040), "files-in-shelf") // 40 folders x 26 files; no whole-folder rehashes when warm
}

// BenchmarkImportOldShapeShelf is the whole pre-cache pass over the same
// fixture: discovery that sizes every folder, a full content digest of each,
// then the shelf comparison. It is the honest denominator for
// BenchmarkImportUnchangedShelf; both are wall time on this machine's page
// cache, not a statement about any other OS.
func BenchmarkImportOldShapeShelf(b *testing.B) {
	homeDir, projectDir := benchShelf(b)
	graph, err := store.Open(filepath.Join(b.TempDir(), "g.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer graph.Close()
	runImportPass(graph, projectDir, homeDir, contentDigest, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runImportPass(graph, projectDir, homeDir, contentDigest, false)
	}
}

// Many workspaces over one shared skills root must read its bytes once.
func TestDigestCacheCoalescesConcurrentMissesOnOneFolder(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		writeTreeFile(t, filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), "body")
	}
	c := settledCache(8)
	const callers = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			wantDigest(t, c, dir)
		}()
	}
	close(start)
	wg.Wait()
	// wantDigest itself calls contentDigest directly, which is not counted.
	if c.reads.Load() != 1 || c.hits.Load() != callers-1 {
		t.Fatalf("reads=%d hits=%d, want 1 and %d", c.reads.Load(), c.hits.Load(), callers-1)
	}
	if len(c.reading) != 0 {
		t.Fatal("in-flight reads leaked")
	}
}

// One shelf, two workspaces: neither may retire the other's project skill, and
// concurrent passes may not leave a duplicate behind.
func TestImportPassKeepsAnotherWorkspacesProjectSkills(t *testing.T) {
	homeDir := t.TempDir()
	projA, projB := t.TempDir(), t.TempDir()
	shared := filepath.Join(homeDir, ".claude", "skills", "pdf")
	skillA := filepath.Join(projA, ".claude", "skills", "alpha")
	skillB := filepath.Join(projB, ".claude", "skills", "beta")
	writeImportedSkill(t, shared, "pdf", "PDF")
	writeImportedSkill(t, skillA, "alpha", "A only")
	writeImportedSkill(t, skillB, "beta", "B only")
	graph := openStore(t)
	c := settledCache(16)

	importPass(graph, projA, homeDir, c)
	importPass(graph, projB, homeDir, c)
	for _, dir := range []string{shared, skillA, skillB} {
		if _, ok := importedFactByArtifact(t, graph, dir); !ok {
			t.Fatalf("%s not active after both workspaces imported", dir)
		}
	}

	// B's pass cannot see A's folder go; A's own pass can, and does.
	if err := os.RemoveAll(skillA); err != nil {
		t.Fatal(err)
	}
	importPass(graph, projB, homeDir, c)
	if _, ok := importedFactByArtifact(t, graph, skillA); !ok {
		t.Fatal("workspace B retired workspace A's project skill")
	}
	importPass(graph, projA, homeDir, c)
	if _, ok := importedFactByArtifact(t, graph, skillA); ok {
		t.Fatal("workspace A did not retire its own deleted skill")
	}
	if _, ok := importedFactByArtifact(t, graph, skillB); !ok {
		t.Fatal("workspace A retired workspace B's project skill")
	}
}

func TestConcurrentWorkspacePassesLeaveOneFactPerFolder(t *testing.T) {
	homeDir := t.TempDir()
	shared := filepath.Join(homeDir, ".claude", "skills", "pdf")
	writeImportedSkill(t, shared, "pdf", "PDF")
	graph := openStore(t)
	c := settledCache(16)
	var projects []string
	for i := 0; i < 6; i++ {
		proj := t.TempDir()
		writeImportedSkill(t, filepath.Join(proj, ".claude", "skills", "own"), "own", "mine")
		projects = append(projects, proj)
	}
	var wg sync.WaitGroup
	for round := 0; round < 3; round++ {
		for _, proj := range projects {
			wg.Add(1)
			go func() { defer wg.Done(); reconcileImportedSkillsWith(graph, proj, homeDir, c) }()
		}
	}
	wg.Wait()
	active, err := graph.SkillFacts(store.FactActive, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, f := range active {
		seen[f.Artifact]++
	}
	if len(seen) != 7 {
		t.Fatalf("active folders = %d, want 7: %v", len(seen), seen)
	}
	for dir, n := range seen {
		if n != 1 {
			t.Fatalf("%s has %d active facts", dir, n)
		}
	}
	if importPasses.idle() != 0 {
		t.Fatal("gates leaked")
	}
}
