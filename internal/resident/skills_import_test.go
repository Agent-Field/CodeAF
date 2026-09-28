package resident

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/skills"
	"github.com/Agent-Field/codeaf/internal/store"
)

// writeImportedSkill lays down one foreign skill folder exactly as another
// harness would have installed it: a directory whose SKILL.md frontmatter
// names it, and nothing else required.
func writeImportedSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func importedFactByArtifact(t *testing.T, graph *store.Store, artifact string) (store.Fact, bool) {
	t.Helper()
	facts, err := graph.SkillFacts(store.FactActive, 100)
	if err != nil {
		t.Fatalf("read active skills: %v", err)
	}
	for _, fact := range facts {
		if filepath.Clean(fact.Artifact) == filepath.Clean(artifact) {
			return fact, true
		}
	}
	return store.Fact{}, false
}

// The whole point of the pass: a person's existing Claude Code skill and an
// existing project skill both become ACTIVE facts whose artifact is the
// ORIGINAL directory — no copy, no check.sh, no executable — and a second run
// over an unchanged disk creates nothing.
func TestImportSyncRegistersForeignSkillsInPlace(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	pdfDir := filepath.Join(homeDir, ".claude", "skills", "pdf")
	reportDir := filepath.Join(projectDir, ".claude", "skills", "report")
	writeImportedSkill(t, pdfDir, "pdf", "Fill, flatten and redact PDF forms")
	writeImportedSkill(t, reportDir, "report", "Drafts the weekly project report")

	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	reconciler.reconcileImportedSkills(projectDir, homeDir)
	reconciler.reconcileImportedSkills(projectDir, homeDir)

	active, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil || len(active) != 2 {
		t.Fatalf("active imported skills = %+v err = %v, want exactly two", active, err)
	}
	all, err := graph.SkillFacts("", 100)
	if err != nil || len(all) != 2 {
		t.Fatalf("the second run created something: all skill facts = %+v err = %v", all, err)
	}

	pdf, ok := importedFactByArtifact(t, graph, pdfDir)
	if !ok {
		t.Fatalf("no active fact for the original pdf directory %s", pdfDir)
	}
	if pdf.Trust != "imported-provisional" {
		t.Errorf("pdf Trust = %q, want imported-provisional", pdf.Trust)
	}
	if pdf.Body != "Fill, flatten and redact PDF forms" {
		t.Errorf("pdf Body = %q, want the frontmatter description", pdf.Body)
	}
	if pdf.Scope != "harness:claude" {
		t.Errorf("pdf Scope = %q, want harness:claude", pdf.Scope)
	}

	report, ok := importedFactByArtifact(t, graph, reportDir)
	if !ok {
		t.Fatalf("no active fact for the original report directory %s", reportDir)
	}
	if report.Trust != "imported-provisional" {
		t.Errorf("report Trust = %q, want imported-provisional", report.Trust)
	}
	if report.Body != "Drafts the weekly project report" {
		t.Errorf("report Body = %q, want the frontmatter description", report.Body)
	}
	if want := "repo:" + strings.ToLower(projectDir); report.Scope != want {
		t.Errorf("report Scope = %q, want %q", report.Scope, want)
	}
}

// The wiring itself: a Tick takes the project from the working directory and
// the home from the CODEAF_HOME override, exactly as a disposable run needs.
func TestImportSyncWiredIntoTick(t *testing.T) {
	overrideHome := t.TempDir()
	t.Setenv(home.EnvVar, overrideHome)
	projectDir := t.TempDir()
	pdfDir := filepath.Join(overrideHome, ".claude", "skills", "pdf")
	reportDir := filepath.Join(projectDir, ".claude", "skills", "report")
	writeImportedSkill(t, pdfDir, "pdf", "Fill, flatten and redact PDF forms")
	writeImportedSkill(t, reportDir, "report", "Drafts the weekly project report")
	t.Chdir(projectDir)

	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A second Tick over the unchanged disk must create nothing — the second
	// sync is the idempotency the whole pass is built on, and a quiet tick
	// honors it by not running at all.
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	all, err := graph.SkillFacts("", 100)
	if err != nil || len(all) != 2 {
		t.Fatalf("two ticks left %d skill facts, want exactly the two imports: %+v err = %v", len(all), all, err)
	}

	for _, dir := range []string{pdfDir, reportDir} {
		fact, ok := importedFactByArtifact(t, graph, dir)
		if !ok {
			t.Fatalf("Tick imported nothing for %s", dir)
		}
		if fact.Trust != "imported-provisional" {
			t.Errorf("fact for %s has Trust %q, want imported-provisional", dir, fact.Trust)
		}
	}
}

// An edited skill is a changed skill: the old fact retires pointing at its
// replacement, and the replacement keeps the same original directory.
func TestImportSyncSupersedesWhenSkillChangesOnDisk(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	pdfDir := filepath.Join(homeDir, ".claude", "skills", "pdf")
	writeImportedSkill(t, pdfDir, "pdf", "Fill and flatten PDF forms")

	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	reconciler.reconcileImportedSkills(projectDir, homeDir)
	first, ok := importedFactByArtifact(t, graph, pdfDir)
	if !ok {
		t.Fatal("first pass imported nothing")
	}

	writeImportedSkill(t, pdfDir, "pdf", "Fill, flatten and redact PDF forms")
	reconciler.reconcileImportedSkills(projectDir, homeDir)

	second, ok := importedFactByArtifact(t, graph, pdfDir)
	if !ok {
		t.Fatal("second pass lost the skill")
	}
	if second.Seq == first.Seq {
		t.Fatalf("the changed skill was never re-recorded: #%d", second.Seq)
	}
	if second.Body != "Fill, flatten and redact PDF forms" {
		t.Errorf("Body = %q, want the new description", second.Body)
	}
	superseded, err := graph.SkillFacts(store.FactSuperseded, 10)
	if err != nil || len(superseded) != 1 {
		t.Fatalf("superseded skills = %+v err = %v, want exactly the old fact", superseded, err)
	}
	if superseded[0].Seq != first.Seq {
		t.Errorf("superseded #%d, want the original #%d", superseded[0].Seq, first.Seq)
	}
}

// A deleted folder retires its fact with the reason a person needs: the file
// to go looking for.
func TestImportSyncSupersedesWhenFolderIsGone(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	pdfDir := filepath.Join(homeDir, ".claude", "skills", "pdf")
	writeImportedSkill(t, pdfDir, "pdf", "Fill, flatten and redact PDF forms")

	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	reconciler.reconcileImportedSkills(projectDir, homeDir)
	if _, ok := importedFactByArtifact(t, graph, pdfDir); !ok {
		t.Fatal("first pass imported nothing")
	}
	if err := os.RemoveAll(pdfDir); err != nil {
		t.Fatal(err)
	}
	reconciler.reconcileImportedSkills(projectDir, homeDir)

	active, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil || len(active) != 0 {
		t.Fatalf("active imported skills after deletion = %+v err = %v, want none", active, err)
	}
	superseded, err := graph.SkillFacts(store.FactSuperseded, 10)
	if err != nil || len(superseded) != 1 {
		t.Fatalf("superseded skills = %+v err = %v, want exactly one", superseded, err)
	}
	if !strings.Contains(superseded[0].StatusNote, "SKILL.md") {
		t.Errorf("StatusNote = %q, want the missing file named", superseded[0].StatusNote)
	}
}

// The trust tier is a fence: authored and forged facts pointing at a
// discovered directory are recorded beside, never superseded, rewritten or
// otherwise touched.
func TestImportSyncLeavesAuthoredAndForgedFactsAlone(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	pdfDir := filepath.Join(homeDir, ".claude", "skills", "pdf")
	writeImportedSkill(t, pdfDir, "pdf", "Fill, flatten and redact PDF forms")

	graph := openStore(t)
	authored, err := graph.RecordSkillCandidate(store.RootID, "repo:pdf", "authored pdf doc", pdfDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ActivateSkill(authored.Seq, pdfDir, "authored-digest"); err != nil {
		t.Fatal(err)
	}
	forged, err := graph.RecordSkillCandidateFrom(store.FactWriterOther, store.RootID,
		"repo:pdf", "forged pdf doc", pdfDir, "forged")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ActivateSkill(forged.Seq, pdfDir, "forged-digest"); err != nil {
		t.Fatal(err)
	}

	reconciler := New(graph, nil, nil)
	reconciler.reconcileImportedSkills(projectDir, homeDir)

	if fact, ok := importedFactByArtifact(t, graph, pdfDir); !ok {
		t.Fatal("the imported fact was not recorded")
	} else if fact.Trust != "imported-provisional" {
		t.Errorf("imported fact Trust = %q", fact.Trust)
	}
	if fact, found, err := graph.FactBySeq(authored.Seq); err != nil || !found || fact.Status != store.FactActive ||
		fact.Digest != "authored-digest" || fact.Body != "authored pdf doc" {
		t.Errorf("the authored fact was disturbed: %+v found = %t err = %v", fact, found, err)
	}
	if fact, found, err := graph.FactBySeq(forged.Seq); err != nil || !found || fact.Status != store.FactActive ||
		fact.Digest != "forged-digest" || fact.Body != "forged pdf doc" {
		t.Errorf("the forged fact was disturbed: %+v found = %t err = %v", fact, found, err)
	}
	superseded, err := graph.SkillFacts(store.FactSuperseded, 10)
	if err != nil || len(superseded) != 0 {
		t.Fatalf("something was superseded: %+v err = %v", superseded, err)
	}
}

// A skill whose name does not match its folder loads with a warning in the
// discovery, and a loaded skill is an imported skill: the warning is for the
// person to see, not a reason to refuse the folder.
func TestImportSyncImportsSkillWhoseNameMismatchesFolder(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	pdfDir := filepath.Join(homeDir, ".claude", "skills", "pdf")
	writeImportedSkill(t, pdfDir, "different-name", "Fill, flatten and redact PDF forms")

	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	reconciler.reconcileImportedSkills(projectDir, homeDir)

	fact, ok := importedFactByArtifact(t, graph, pdfDir)
	if !ok {
		t.Fatal("the mismatched-name skill was not imported")
	}
	if fact.Body != "Fill, flatten and redact PDF forms" {
		t.Errorf("Body = %q, want the frontmatter description", fact.Body)
	}
}

// The scan reads the .codeaf/skills root too, but only folders holding a
// SKILL.md: the promoted command folders the forge itself installed there
// stay invisible to it.
func TestImportSyncIgnoresPromotedCommandFolders(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	promoted := filepath.Join(homeDir, ".codeaf", "skills", "repo-audit")
	for name, body := range map[string]string{
		"run.sh":   "#!/bin/sh\necho audited\n",
		"check.sh": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.MkdirAll(promoted, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(promoted, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pdfDir := filepath.Join(homeDir, ".claude", "skills", "pdf")
	writeImportedSkill(t, pdfDir, "pdf", "Fill, flatten and redact PDF forms")

	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	reconciler.reconcileImportedSkills(projectDir, homeDir)

	active, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil || len(active) != 1 {
		t.Fatalf("active skills = %+v err = %v, want only the pdf skill", active, err)
	}
	if active[0].Artifact != pdfDir {
		t.Errorf("the promoted command folder was imported: %q", active[0].Artifact)
	}
}

// A skill folder that is a link reaches the shelf under the link's own name,
// and an edit made at the folder the link names is read on the next pass: the
// digest is taken through the link, not of it.
func TestImportSyncReadsLinkedSkillFoldersThroughTheLink(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	shared := filepath.Join(homeDir, "shared", "pdf-kit")
	writeImportedSkill(t, shared, "pdf", "Fill and flatten PDF forms")
	link := filepath.Join(homeDir, ".claude", "skills", "pdf")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}

	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	reconciler.reconcileImportedSkills(projectDir, homeDir)
	first, ok := importedFactByArtifact(t, graph, link)
	if !ok {
		t.Fatal("the linked skill folder was not imported")
	}
	if first.SkillName() != "pdf" {
		t.Errorf("SkillName = %q, want the link's own name", first.SkillName())
	}

	writeImportedSkill(t, shared, "pdf", "Fill, flatten and redact PDF forms")
	reconciler.reconcileImportedSkills(projectDir, homeDir)
	second, ok := importedFactByArtifact(t, graph, link)
	if !ok {
		t.Fatal("the second pass lost the linked skill")
	}
	if second.Seq == first.Seq || second.Body != "Fill, flatten and redact PDF forms" {
		t.Errorf("the edit behind the link was never read: #%d %q after #%d", second.Seq, second.Body, first.Seq)
	}
}

// The two deeper sources are named by the harness they belong to, the way the
// six skills folders always were: a Claude Code plugin's skill is a Claude
// Code skill, and Codex's bundled one is a Codex skill.
func TestImportedSkillScopeNamesTheHarnessForDeeperRoots(t *testing.T) {
	for root, want := range map[string]string{
		".claude/skills":        "harness:claude",
		".claude/plugins":       "harness:claude",
		".codex/skills/.system": "harness:codex",
		".agents/skills":        "harness:agents",
	} {
		skill := skills.Skill{Scope: skills.ScopeUser, Root: root}
		if got := importedSkillScope(skill, "/work/app"); got != want {
			t.Errorf("importedSkillScope(%q) = %q, want %q", root, got, want)
		}
	}
}
