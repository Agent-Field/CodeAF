package resident

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

const testSkillDoc = "repo-audit runs the verified repository audit"

func TestRecurringSkillPromotesOnlyAfterGreenCheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	graph := openStore(t)
	firstArtifact := writeSkillArtifact(t, "repo-audit", `#!/bin/sh
set -eu
test "$PWD" != "$CODEAF_SKILL_DIR"
test -x "$CODEAF_SKILL_DIR/run.sh"
printf '%s' "$PWD" > "$CODEAF_SKILL_DIR/CHECK_CWD"
`)
	first := recordCandidateJob(t, graph, "job-first", firstArtifact)
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if facts, err := graph.SkillFacts(store.FactCandidate, 10); err != nil || len(facts) != 1 {
		t.Fatalf("one occurrence changed status: facts=%+v err=%v", facts, err)
	}
	if _, err := os.Stat(filepath.Join(firstArtifact, "CHECK_CWD")); !os.IsNotExist(err) {
		t.Fatalf("one occurrence ran the check: %v", err)
	}

	secondArtifact := writeSkillArtifact(t, "repo-audit", `#!/bin/sh
set -eu
test "$PWD" != "$CODEAF_SKILL_DIR"
test -x "$CODEAF_SKILL_DIR/run.sh"
printf '%s' "$PWD" > "$CODEAF_SKILL_DIR/CHECK_CWD"
`)
	second := recordCandidateJob(t, graph, "job-second", secondArtifact)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	active, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil || len(active) != 1 {
		t.Fatalf("active skills = %+v err=%v", active, err)
	}
	if active[0].Seq != second.Seq {
		t.Fatalf("promoted #%d, want newest repeated candidate #%d (first #%d)", active[0].Seq, second.Seq, first.Seq)
	}
	root, err := store.SkillsRoot()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(active[0].Artifact) != root {
		t.Fatalf("installed artifact = %q, want direct child of %q", active[0].Artifact, root)
	}
	checkDir, err := os.ReadFile(filepath.Join(active[0].Artifact, "CHECK_CWD"))
	if err != nil {
		t.Fatalf("check did not execute: %v", err)
	}
	if got := string(checkDir); !strings.Contains(got, "codeaf-skill-check-") || strings.HasPrefix(got, active[0].Artifact) {
		t.Fatalf("check cwd = %q, want a clean temp directory", got)
	}
	provenance, err := os.ReadFile(filepath.Join(active[0].Artifact, "PROVENANCE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(provenance), "job-first") || !strings.Contains(string(provenance), "job-second") {
		t.Fatalf("PROVENANCE = %q, want both teaching jobs", provenance)
	}
	bin, err := store.SkillsBinDir()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, filepath.Base(active[0].Artifact))
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("skill bin entry = %v err=%v, want symlink", info, err)
	}
	all, err := graph.SkillFacts("", 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("all skills = %+v err=%v", all, err)
	}
	for _, fact := range all {
		if fact.Seq == first.Seq && fact.Status != store.FactSuperseded {
			t.Fatalf("first occurrence status = %s, want superseded", fact.Status)
		}
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	activated := 0
	for _, event := range events {
		if event.Kind == store.EventFactActivated {
			activated++
		}
	}
	if activated != 1 {
		t.Fatalf("activation events = %d, want 1", activated)
	}

	replacement, err := graph.RecordFact("job-second", "repo:audit", store.FactLesson,
		"repo-audit failed its live contract and must not be offered")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SupersedeFactWithReason(active[0].Seq, replacement.Seq, "distilled live failure"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("retired skill bin entry still exists: %v", err)
	}
	if _, err := os.Stat(active[0].Artifact); err != nil {
		t.Fatalf("retirement removed provenance-bearing artifact: %v", err)
	}
}

// H7: a skill check receives the current and legacy skill-directory spellings
// with the same value.
func TestH7SkillCheckExportsBothDirectorySpellings(t *testing.T) {
	// The old spelling is named on its own Go line so the name law can see
	// the exemption; a marker inside the script's string cannot open one.
	const legacySpelling = "AFORGE_SKILL_DIR" // legacy-name
	dir := writeSkillArtifact(t, "both-envs", "#!/bin/sh\nset -eu\n"+
		"test \"$CODEAF_SKILL_DIR\" = \"$"+legacySpelling+"\"\n"+
		"test \"$CODEAF_SKILL_DIR\" != \"\"\n")
	if err := runSkillCheck(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
}

func TestSkillCheckStripsSensitiveEnvVars(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-secret-test-value")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-secret-test-value")
	dir := writeSkillArtifact(t, "strip-secrets", "#!/bin/sh\nset -eu\n"+
		"test -z \"${ANTHROPIC_API_KEY:-}\"\n"+
		"test -z \"${SLACK_BOT_TOKEN:-}\"\n")
	if err := runSkillCheck(context.Background(), dir); err != nil {
		t.Fatalf("runSkillCheck leaked sensitive env vars: %v", err)
	}
}

func TestRecurringSkillRedCheckSupersedesCandidates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	graph := openStore(t)
	recordCandidateJob(t, graph, "job-green-source", writeSkillArtifact(t, "repo-audit", `#!/bin/sh
exit 0
`))
	recordCandidateJob(t, graph, "job-red-source", writeSkillArtifact(t, "repo-audit", `#!/bin/sh
echo deliberate-red-check >&2
exit 9
`))

	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	if active, err := graph.SkillFacts(store.FactActive, 10); err != nil || len(active) != 0 {
		t.Fatalf("red trial activated skills: %+v err=%v", active, err)
	}
	retired, err := graph.SkillFacts(store.FactSuperseded, 10)
	if err != nil || len(retired) != 2 {
		t.Fatalf("retired candidates = %+v err=%v", retired, err)
	}
	for _, fact := range retired {
		if !strings.Contains(fact.StatusNote, "exit status 9") ||
			!strings.Contains(fact.StatusNote, "deliberate-red-check") {
			t.Fatalf("candidate #%d failure evidence = %q", fact.Seq, fact.StatusNote)
		}
	}
	root, err := store.SkillsRoot()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("red trial left installed entries: %+v", entries)
	}
}

func writeSkillArtifact(t *testing.T, name, check string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"run.sh":   "#!/bin/sh\necho audited\n",
		"check.sh": check,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func recordCandidateJob(t *testing.T, graph *store.Store, id, artifact string) store.Fact {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Brief: "teach a reusable audit", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "audit this repository"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim(id, "skill-test")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%t err=%v", id, won, err)
	}
	if err := graph.Complete(claim, "audit completed"); err != nil {
		t.Fatal(err)
	}
	fact, err := graph.RecordSkillCandidate(id, "repo:audit", testSkillDoc, artifact)
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

// Digest is stable for identical directory contents and differs when a file
// changes, even if the file name is the same.
func TestContentDigestStability(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d1, err := contentDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Same contents -> same digest.
	d2, err := contentDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("same contents produced different digests: %q vs %q", d1, d2)
	}
	// Changed file content -> different digest.
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d3, err := contentDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d3 {
		t.Fatal("changed content should produce different digest")
	}
	if len(d1) != 64 {
		t.Fatalf("sha256 hex digest should be 64 chars, got %d", len(d1))
	}
	if len(d3) != 64 {
		t.Fatalf("sha256 hex digest should be 64 chars, got %d", len(d3))
	}
}
