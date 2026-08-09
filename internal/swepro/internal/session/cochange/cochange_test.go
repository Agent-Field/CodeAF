package cochange

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

// Translation of src/session/co-change.test.ts — verbatim describe/test names,
// same assertion semantics (toBeCloseTo(x, 8) → |diff| < 0.5e-8).

func kptr(f float64) *float64 { return &f }

func noOpts() BuildCoChangeOptions { return BuildCoChangeOptions{} }

func assertCommits(t *testing.T, got, want []CoChangeCommit) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commits mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func assertCloseTo(t *testing.T, got, want float64, digits int) {
	t.Helper()
	tol := math.Pow(10, -float64(digits)) / 2
	if math.Abs(got-want) >= tol {
		t.Fatalf("expected %v to be close to %v (%d digits)", got, want, digits)
	}
}

func TestParseGitLog(t *testing.T) {
	t.Run("parses hash + files separated by blank lines", func(t *testing.T) {
		raw := strings.Join([]string{
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"src/a.ts",
			"src/b.ts",
			"",
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"src/c.ts",
			"",
		}, "\n")
		assertCommits(t, ParseGitLog(raw), []CoChangeCommit{
			{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Files: []string{"src/a.ts", "src/b.ts"}},
			{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Files: []string{"src/c.ts"}},
		})
	})

	t.Run("merge commit with no files yields empty files array", func(t *testing.T) {
		raw := strings.Join([]string{
			"cccccccccccccccccccccccccccccccccccccccc",
			"",
			"dddddddddddddddddddddddddddddddddddddddd",
			"src/only.ts",
			"",
		}, "\n")
		commits := ParseGitLog(raw)
		assertCommits(t, commits, []CoChangeCommit{
			{Hash: "cccccccccccccccccccccccccccccccccccccccc", Files: []string{}},
			{Hash: "dddddddddddddddddddddddddddddddddddddddd", Files: []string{"src/only.ts"}},
		})
	})

	t.Run("rename lines keep the destination path", func(t *testing.T) {
		raw := strings.Join([]string{
			"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			"src/old.ts => src/new.ts",
			"pkg/{foo => bar}/mod.ts",
			"",
		}, "\n")
		assertCommits(t, ParseGitLog(raw), []CoChangeCommit{
			{
				Hash:  "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				Files: []string{"src/new.ts", "pkg/bar/mod.ts"},
			},
		})
	})

	t.Run("trailing newline and short hashes are accepted", func(t *testing.T) {
		raw := "abc1234\nsrc/x.ts\n"
		assertCommits(t, ParseGitLog(raw), []CoChangeCommit{
			{Hash: "abc1234", Files: []string{"src/x.ts"}},
		})
	})

	t.Run("dedups files within a commit", func(t *testing.T) {
		raw := "ffff111\nsrc/a.ts\nsrc/a.ts\nsrc/b.ts\n"
		got := ParseGitLog(raw)[0].Files
		if !reflect.DeepEqual(got, []string{"src/a.ts", "src/b.ts"}) {
			t.Fatalf("files mismatch: %#v", got)
		}
	})
}

func TestBuildCoChangeGraph(t *testing.T) {
	t.Run("skips bulk commits above MAX_COMMIT_FILES", func(t *testing.T) {
		bulkFiles := make([]string, MaxCommitFiles+1)
		for i := range bulkFiles {
			bulkFiles[i] = fmt.Sprintf("f%d.ts", i)
		}
		commits := []CoChangeCommit{
			{Hash: "1", Files: bulkFiles},
			{Hash: "2", Files: []string{"a.ts", "b.ts"}},
			{Hash: "3", Files: []string{"a.ts", "c.ts"}},
		}
		g := BuildCoChangeGraph(commits, noOpts())
		if v, _ := g.Totals.Get("a.ts"); v != 2 {
			t.Fatalf("totals a.ts = %v, want 2", v)
		}
		if v, _ := g.Totals.Get("b.ts"); v != 1 {
			t.Fatalf("totals b.ts = %v, want 1", v)
		}
		if g.Totals.Has("f0.ts") {
			t.Fatalf("totals f0.ts should be undefined")
		}
		// a-b co-occur once; a-c once; bulk pairs absent
		assertCloseTo(t, Coupling(g, "a.ts", "b.ts"), 1.0/(2+1-1), 8)
		assertCloseTo(t, Coupling(g, "a.ts", "c.ts"), 1.0/(2+1-1), 8)
	})

	t.Run("respects maxCommitFiles override", func(t *testing.T) {
		g := BuildCoChangeGraph(
			[]CoChangeCommit{
				{Hash: "1", Files: []string{"a.ts", "b.ts", "c.ts"}},
				{Hash: "2", Files: []string{"a.ts", "b.ts"}},
			},
			BuildCoChangeOptions{MaxCommitFiles: kptr(2)},
		)
		if v, _ := g.Totals.Get("a.ts"); v != 1 {
			t.Fatalf("totals a.ts = %v, want 1", v)
		}
		if g.Totals.Has("c.ts") {
			t.Fatalf("totals c.ts should be undefined")
		}
	})

	t.Run("skips empty-file commits", func(t *testing.T) {
		g := BuildCoChangeGraph([]CoChangeCommit{
			{Hash: "m", Files: []string{}},
			{Hash: "1", Files: []string{"a.ts", "b.ts"}},
		}, noOpts())
		if v, _ := g.Totals.Get("a.ts"); v != 1 {
			t.Fatalf("totals a.ts = %v, want 1", v)
		}
	})
}

func TestCouplingJaccard(t *testing.T) {
	// Hand-built: commits
	//   1: a,b
	//   2: a,b
	//   3: a,c
	//   4: b
	// totals: a=3, b=3, c=1
	// pair(a,b)=2 → Jaccard = 2/(3+3-2)=2/4=0.5
	// pair(a,c)=1 → Jaccard = 1/(3+1-1)=1/3
	// pair(b,c)=0 → 0
	graph := BuildCoChangeGraph([]CoChangeCommit{
		{Hash: "1", Files: []string{"a.ts", "b.ts"}},
		{Hash: "2", Files: []string{"a.ts", "b.ts"}},
		{Hash: "3", Files: []string{"a.ts", "c.ts"}},
		{Hash: "4", Files: []string{"b.ts"}},
	}, noOpts())

	t.Run("hand-computed Jaccard scores", func(t *testing.T) {
		assertCloseTo(t, Coupling(graph, "a.ts", "b.ts"), 0.5, 8)
		assertCloseTo(t, Coupling(graph, "a.ts", "c.ts"), 1.0/3.0, 8)
		if got := Coupling(graph, "b.ts", "c.ts"); got != 0 {
			t.Fatalf("coupling(b,c) = %v, want 0", got)
		}
	})

	t.Run("symmetric and self/absent edge cases", func(t *testing.T) {
		if Coupling(graph, "b.ts", "a.ts") != Coupling(graph, "a.ts", "b.ts") {
			t.Fatalf("coupling is not symmetric")
		}
		if got := Coupling(graph, "a.ts", "a.ts"); got != 1 {
			t.Fatalf("coupling(a,a) = %v, want 1", got)
		}
		if got := Coupling(graph, "a.ts", "missing.ts"); got != 0 {
			t.Fatalf("coupling(a,missing) = %v, want 0", got)
		}
	})
}

func TestRelatedFiles(t *testing.T) {
	// a couples with b (0.5) and c (~0.333); d never co-occurs with a
	// Add e with same score as c for tie-break: commits making pair(a,e)=1, totals e=1 → 1/3
	graph := BuildCoChangeGraph([]CoChangeCommit{
		{Hash: "1", Files: []string{"a.ts", "b.ts"}},
		{Hash: "2", Files: []string{"a.ts", "b.ts"}},
		{Hash: "3", Files: []string{"a.ts", "c.ts"}},
		{Hash: "4", Files: []string{"b.ts"}},
		{Hash: "5", Files: []string{"a.ts", "e.ts"}},
		{Hash: "6", Files: []string{"d.ts", "z.ts"}},
	}, noOpts())

	t.Run("top-k ordered by score desc, path asc on ties", func(t *testing.T) {
		// After commit 5: totals a=4, b=3, c=1, e=1
		// pair(a,b)=2 → 2/(4+3-2)=2/5=0.4
		// pair(a,c)=1 → 1/(4+1-1)=1/4=0.25
		// pair(a,e)=1 → 1/4=0.25  → tie c vs e → c.ts before e.ts
		top := RelatedFiles(graph, "a.ts", kptr(3))
		paths := make([]string, len(top))
		for i, r := range top {
			paths[i] = r.Path
		}
		if !reflect.DeepEqual(paths, []string{"b.ts", "c.ts", "e.ts"}) {
			t.Fatalf("paths = %#v", paths)
		}
		assertCloseTo(t, float64(top[0].Score), 0.4, 8)
		assertCloseTo(t, float64(top[1].Score), 0.25, 8)
		assertCloseTo(t, float64(top[2].Score), 0.25, 8)
	})

	t.Run("k=1 and unknown path", func(t *testing.T) {
		if got := RelatedFiles(graph, "a.ts", kptr(1)); len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got := RelatedFiles(graph, "missing.ts", kptr(5)); len(got) != 0 {
			t.Fatalf("expected [], got %#v", got)
		}
		if got := RelatedFiles(graph, "a.ts", kptr(0)); len(got) != 0 {
			t.Fatalf("expected [], got %#v", got)
		}
	})
}
