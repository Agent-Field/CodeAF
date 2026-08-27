package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// These are the tests for Decision 26's second half: NOTHING OF OURS LIVES IN
// THE PERSON'S FOLDER. Every one of them is a pair — what a session with a
// folder does, and what a session without one still does — because the whole
// design of the seam is that the second answer never changed.

// newPlace is one session folder under a temp directory, borrowed or owned.
func newPlace(t *testing.T, owned bool) Place {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: dir, Owned: owned}
	if owned {
		place.Workspace = place.Work()
		if err := os.MkdirAll(place.Work(), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return place
}

// ── droppings ───────────────────────────────────────────────────────────────

// A job log is a dropping. With a folder it lands in logs/jobs; without one it
// lands exactly where it always did, in the workspace's dot directory.
func TestJobLogsFollowTheSessionFolder(t *testing.T) {
	workspace := t.TempDir()
	place := newPlace(t, false)

	registry := newJobRegistry(workspace, place, nil)
	job, err := registry.newJob("a build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer job.sink.close()

	if want := filepath.Join(place.Logs(), "jobs", "1.log"); job.logPath != want {
		t.Fatalf("job log = %q, want %q", job.logPath, want)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session littered the workspace: %v", err)
	}
}

func TestJobLogsKeepTheLegacyPathWithoutAFolder(t *testing.T) {
	workspace := t.TempDir()
	registry := newJobRegistry(workspace, Place{}, nil)
	job, err := registry.newJob("a build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer job.sink.close()

	if want := filepath.Join(workspace, ".aforge-v3", "jobs", "1.log"); job.logPath != want {
		t.Fatalf("job log = %q, want %q", job.logPath, want)
	}
}

// The id-claim loop is the reason two windows never share a log, and moving the
// directory must not have moved that: two registries on ONE session folder still
// take one name each.
func TestTheIDClaimSurvivesTheMoveIntoTheFolder(t *testing.T) {
	workspace := t.TempDir()
	place := newPlace(t, false)
	first, second := newJobRegistry(workspace, place, nil), newJobRegistry(workspace, place, nil)

	one, err := first.newJob("the first window's build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer one.sink.close()
	two, err := second.newJob("the second window's build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer two.sink.close()

	if one.logPath == two.logPath {
		t.Fatalf("two windows claimed one log: %s", one.logPath)
	}
	if one.id == two.id {
		t.Fatalf("two windows claimed job id %d", one.id)
	}
}

// A stub's bytes follow the folder too, and the LINE the model reads follows
// the bytes: a path out of the workspace is named absolutely, because a
// relative one would be a path nothing can open.
func TestStubBytesFollowTheSessionFolder(t *testing.T) {
	workspace := t.TempDir()
	place := newPlace(t, false)

	path, err := writeStub(place, workspace, strings.Repeat("output\n", 400))
	if err != nil {
		t.Fatalf("writeStub: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("stub path %q is relative to a workspace it is not inside", path)
	}
	if filepath.Dir(filepath.FromSlash(path)) != filepath.Join(place.Logs(), "stubs") {
		t.Fatalf("stub landed at %q, want it under %q", path, place.Logs())
	}
	if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
		t.Fatalf("the stub line names a path with nothing at it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session littered the workspace: %v", err)
	}
}

// And without a folder the stub is still named the way the model's own read tool
// takes it: relative to the workspace.
func TestStubBytesKeepTheLegacyPathWithoutAFolder(t *testing.T) {
	workspace := t.TempDir()
	path, err := writeStub(Place{}, workspace, strings.Repeat("output\n", 400))
	if err != nil {
		t.Fatalf("writeStub: %v", err)
	}
	if !strings.HasPrefix(path, ".aforge-v3/stubs/") {
		t.Fatalf("stub path = %q, want it under .aforge-v3/stubs", path)
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path))); err != nil {
		t.Fatalf("the stub line names a path with nothing at it: %v", err)
	}
}

// ── a worker's droppings, which are the family's ────────────────────────────
//
// The pair above is a SESSION with a folder and a session without one. These are
// the third case, which is neither: an agent that is not a session at all — a
// task node's worker, a fork's hand, a part's worker under either — and which
// therefore carries no Place. It borrows somebody's repository to work in, so
// "the zero Place is the legacy layout" put the harness's own litter inside that
// repository (landing.go states the measured failure).

// treeShape is every file under a directory with the digest of its bytes, so a
// test can say "this tree is untouched" rather than "the files I thought to look
// for are not there". A directory that does not exist is an empty tree, which is
// the honest reading of a workspace nothing has written into.
func treeShape(t *testing.T, dir string) map[string]string {
	t.Helper()
	shape := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir():
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		shape[filepath.ToSlash(relative)] = hex.EncodeToString(digest[:8])
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return shape
}

// loadHeavyTurns fills a live transcript with six turns of one heavy tool result
// each — the shape [Agent.stubOldOutputs] is written for (stub_test.go) — so
// that the pass has two old results to file and four recent ones to leave alone.
func loadHeavyTurns(a *Agent, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for turn := 1; turn <= 6; turn++ {
		a.messages = append(a.messages,
			textMessage("user", fmt.Sprintf("turn %d", turn)),
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: fmt.Sprintf("c%d", turn), Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: fmt.Sprintf("c%d", turn),
				Content: []ai.ContentPart{{Type: "text", Text: text}}})
	}
}

// workerWorkingIn is the production constructor's worker for a fresh node, made
// to work in the directory it is handed. It is [workerFor] (task_divide_test.go)
// with the workspace named, because the whole question here is what a worker
// leaves behind in the directory it borrowed.
func workerWorkingIn(t *testing.T, session *Agent, dir, title string) *Agent {
	t.Helper()
	graph := session.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: title, brief: "b", acceptance: "a", depth: 1})
	worker, err := session.newTaskAgent(context.Background(), dir, graph.node(id), "")
	if err != nil {
		t.Fatalf("the production constructor refused to build a worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	return worker
}

// THE MEASURED FAILURE, PINNED. A worker read a long file, the stub pass filed
// the bytes, and with no Place of its own it wrote them to
// <repo>/.aforge-v3/stubs/<digest>.txt — inside the repository it was working
// in. The bytes belong to the family: logs/stubs of the session that
// commissioned the work, and nothing at all in the borrowed tree.
func TestATaskWorkersStubsLandWithTheFamilyAndNotInTheRepository(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	place := newPlace(t, false)
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.Place = place
	})

	worker := workerWorkingIn(t, session, repo, "the whole job")
	if worker.config.Place.Dir != "" {
		t.Fatal("a worker was made into a second session; the point of this test is that it is not one")
	}
	before := treeShape(t, repo)
	loadHeavyTurns(worker, heavyOutput("SCORING SCRIPT"))
	worker.stubOldOutputs()

	entries, err := os.ReadDir(filepath.Join(place.Logs(), droppingStubs))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the family's stubs directory holds %v (%v); want the one result the worker stubbed", entries, err)
	}
	if got := treeShape(t, repo); !reflect.DeepEqual(got, before) {
		t.Fatalf("the worker changed the repository it borrowed:\n before: %v\n  after: %v", before, got)
	}
	if _, err := os.Stat(filepath.Join(repo, droppingsLegacyDir)); !os.IsNotExist(err) {
		t.Fatalf("a worker littered the person's repository: %v", err)
	}
}

// A hand is the same story with no worktree at all: it works in the CALLER'S own
// workspace by construction (fork.go), so a hand that filed its own droppings
// against that workspace wrote into the person's repository on every fork the
// conversation itself ran.
func TestAForkHandsStubsLandWithTheSessionAndNotInTheRepository(t *testing.T) {
	place := newPlace(t, false)
	agent, repo := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = place
	})
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	seed, system := agent.forkSeed()
	hand, err := agent.newHandAgent(forkPart{Role: "one", Scope: []string{"a"}}, seed, system, &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatalf("newHandAgent: %v", err)
	}
	t.Cleanup(func() { _ = hand.Close() })

	before := treeShape(t, repo)
	loadHeavyTurns(hand, heavyOutput("A LONG READ"))
	hand.stubOldOutputs()

	entries, err := os.ReadDir(filepath.Join(place.Logs(), droppingStubs))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the session's stubs directory holds %v (%v); want the one result the hand stubbed", entries, err)
	}
	if got := treeShape(t, repo); !reflect.DeepEqual(got, before) {
		t.Fatalf("a hand changed the workspace it shares with the caller:\n before: %v\n  after: %v", before, got)
	}
}

// A job log is the other dropping and it takes the same road. A worker running a
// background build wrote its log under the repository it was building in, on a
// timer, for as long as the job lived.
func TestATaskWorkersJobLogLandsWithTheFamilyAndNotInTheRepository(t *testing.T) {
	repo := t.TempDir()
	place := newPlace(t, false)
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.Place = place
	})

	worker := workerWorkingIn(t, session, repo, "the whole job")
	before := treeShape(t, repo)
	job, err := worker.jobs.newJob("a build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer job.sink.close()

	if want := filepath.Join(place.Logs(), droppingJobs, "1.log"); job.logPath != want {
		t.Fatalf("a worker's job log is at %q, want %q", job.logPath, want)
	}
	if got := treeShape(t, repo); !reflect.DeepEqual(got, before) {
		t.Fatalf("the worker changed the repository it borrowed:\n before: %v\n  after: %v", before, got)
	}
}

// AND THE LEGACY RUNG IS STILL THERE FOR THE ONE CALLER IT WAS MEANT FOR. A
// worker of a session that truly has no folder has nowhere else to write, so it
// writes exactly where it always did — this is the behaviour the fix above must
// not have taken away.
func TestAWorkerOfAFolderlessSessionKeepsTheLegacyPath(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	repo := t.TempDir()
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
	})
	if session.config.Place.Logs() != "" {
		t.Fatal("this session has a folder, so it is not the case under test")
	}

	worker := workerWorkingIn(t, session, repo, "the whole job")
	loadHeavyTurns(worker, heavyOutput("A LONG READ"))
	worker.stubOldOutputs()

	entries, err := os.ReadDir(filepath.Join(repo, droppingsLegacyDir, droppingStubs))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the legacy stubs directory holds %v (%v); want the one result the worker stubbed", entries, err)
	}
}

// ── the two structural halves of the law ────────────────────────────────────

// packageSources is every non-test file of this package, parsed. The structural
// checks below read the package's own text because what they hold is a rule
// about CONSTRUCTION: a worker kind added next year is one nobody will think to
// write a droppings test for, and these two fail the moment it exists.
func packageSources(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[path] = parsed
	}
	if len(files) < 50 {
		t.Fatalf("the scan found %d source files, so it has broken rather than passed", len(files))
	}
	return fset, files
}

// sourceOf is one expression as it is written, for a failure message that names
// what somebody actually typed.
func sourceOf(fset *token.FileSet, node ast.Node) string {
	var out strings.Builder
	if err := printer.Fprint(&out, fset, node); err != nil {
		return "?"
	}
	return out.String()
}

// droppingWriters are the three functions that put a dropping on disk, and which
// argument of each is the folder it lands in.
var droppingWriters = map[string]int{
	"writeStub":      0, // stub.go: the bytes of a result taken out of context
	"newJobRegistry": 1, // jobs.go: every background job's log
	"newChatJournal": 3, // chatlog.go: an over-long message spilled beside the thread
}

// NOBODY ASKS A .Place FOR A DROPPINGS HOME. The two are the same answer for a
// conversation and different answers for every worker, which is exactly why the
// wrong one passed review: it is correct in the case anybody tests by hand.
// [Config.droppingsPlace] is the question with the family in it, and this holds
// the three writers to asking it.
func TestEveryDroppingWriterAsksForTheDroppingsHomeAndNotForAPlace(t *testing.T) {
	fset, files := packageSources(t)
	var wrong []string
	seen := 0
	for path, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			at, watched := droppingWriters[name.Name]
			if !watched || at >= len(call.Args) {
				return true
			}
			seen++
			if written := sourceOf(fset, call.Args[at]); strings.HasSuffix(written, ".Place") {
				wrong = append(wrong, fmt.Sprintf("%s:%d: %s(%s)",
					path, fset.Position(call.Pos()).Line, name.Name, written))
			}
			return true
		})
	}
	if seen < len(droppingWriters) {
		t.Fatalf("the scan found %d dropping writers, so it has broken rather than passed", seen)
	}
	if len(wrong) > 0 {
		sort.Strings(wrong)
		t.Fatalf("a dropping was filed against a session's Place rather than the family's droppings home:\n\t%s\n"+
			"Ask Config.droppingsPlace() — a worker carries no Place, so a Place here means the borrowed workspace (landing.go).",
			strings.Join(wrong, "\n\t"))
	}
}

// AND EVERY AGENT THIS PACKAGE BUILDS SAYS WHERE ITS LITTER GOES. A worker is
// built by a Config literal that copies about thirty fields from its parent, and
// the failure this whole law is about was one of them missing — so a literal that
// names neither a folder of its own (Place) nor its family's (droppings) is a new
// kind of worker quietly writing into somebody's repository again.
func TestEveryAgentThisPackageBuildsSaysWhereItsDroppingsGo(t *testing.T) {
	fset, files := packageSources(t)
	var silent []string
	seen := 0
	for path, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || name.Name != "newAgent" || len(call.Args) == 0 {
				return true
			}
			// A caller handing over a whole Config — [New], which forwards the
			// door's own — is not a construction site: the door's Place is the
			// session's and the question does not arise.
			literal, ok := call.Args[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			seen++
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := pair.Key.(*ast.Ident); ok && (key.Name == "droppings" || key.Name == "Place") {
					return true
				}
			}
			silent = append(silent, fmt.Sprintf("%s:%d", path, fset.Position(call.Pos()).Line))
			return true
		})
	}
	if seen < 4 {
		t.Fatalf("the scan found %d agent constructions, so it has broken rather than passed", seen)
	}
	if len(silent) > 0 {
		sort.Strings(silent)
		t.Fatalf("an agent is built with no answer to where its droppings land:\n\t%s\n"+
			"Set droppings: to the family's Place (Agent.familyPlace for a node, Config.droppingsPlace() otherwise), "+
			"or Place: if this agent really is a session of its own (landing.go).",
			strings.Join(silent, "\n\t"))
	}
}

// ── deliverables ────────────────────────────────────────────────────────────

// A BORROWED session paints into its own artifacts/ and never into the
// repository it was opened in — that is the whole of "the person's repo is
// borrowed, never littered".
func TestAPaintedPictureLandsInArtifactsForABorrowedSession(t *testing.T) {
	picture := pngOfSize(t, 8, 6)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	place := newPlace(t, false)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")

	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		config.Place = place
		config.ArtifactsIndex = index
	})
	if result, isError := runTool(t, agent, "generate_image", `{"prompt":"Sunset over the Harbour"}`); isError {
		t.Fatalf("generate_image failed: %s", result)
	}

	entries, err := os.ReadDir(place.Artifacts())
	if err != nil || len(entries) != 1 {
		t.Fatalf("artifacts directory = %v, %v; want one picture", entries, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session painted into the person's repository: %v", err)
	}

	// And the row: a deliverable nobody can find again is not a deliverable.
	rows := ReadArtifacts(index)
	if len(rows) != 1 {
		t.Fatalf("artifact rows = %d, want 1", len(rows))
	}
	if rows[0].Kind != "image" {
		t.Fatalf("row kind = %q, want image", rows[0].Kind)
	}
	if rows[0].Title != "sunset over the harbour" {
		t.Fatalf("row title = %q, want the prompt's own words", rows[0].Title)
	}
	if rows[0].Path != filepath.Join(place.Artifacts(), entries[0].Name()) {
		t.Fatalf("row path = %q, want the file that was written", rows[0].Path)
	}
}

// An OWNED session's workspace IS ours, so a picture lands in it like any other
// file the work produced — no dot directory, no artifacts/ detour.
func TestAPaintedPictureLandsInTheWorkspaceForAnOwnedSession(t *testing.T) {
	picture := pngOfSize(t, 8, 6)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	place := newPlace(t, true)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = place.Work()
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		config.Place = place
		config.ArtifactsIndex = index
	})
	if result, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour"}`); isError {
		t.Fatalf("generate_image failed: %s", result)
	}

	entries, err := os.ReadDir(place.Work())
	if err != nil {
		t.Fatalf("read the owned workspace: %v", err)
	}
	if len(entries) != 1 || entries[0].IsDir() {
		t.Fatalf("owned workspace holds %v, want one picture in it", entries)
	}
	if !strings.HasSuffix(entries[0].Name(), "-a-harbour.png") {
		t.Fatalf("generated name %q lost the timestamp-slug shape", entries[0].Name())
	}
	if _, err := os.Stat(place.Artifacts()); !os.IsNotExist(err) {
		t.Fatalf("an owned session made an artifacts directory it has no use for: %v", err)
	}
	if rows := ReadArtifacts(index); len(rows) != 1 {
		t.Fatalf("artifact rows = %d, want 1", len(rows))
	}
}

// A session with no folder keeps the dot directory it always had, and records
// nothing when nobody gave it an index — a test and a headless --once both.
func TestAPaintedPictureKeepsTheLegacyPathWithoutAFolder(t *testing.T) {
	picture := pngOfSize(t, 8, 6)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	agent, workspace := newPainterAgent(t, painter, "paint/model")

	if result, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour"}`); isError {
		t.Fatalf("generate_image failed: %s", result)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, ".aforge-v3", "images"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("legacy image directory = %v, %v; want one picture", entries, err)
	}
}

// PlaceSession reads the id off the folder's own name, and answers NOTHING for
// a session that has no folder rather than inventing one.
func TestPlaceSessionNamesTheFolderAndNothingElse(t *testing.T) {
	place := newPlace(t, false)
	if got, want := PlaceSession(place), filepath.Base(place.Dir); got != want {
		t.Fatalf("session id = %q, want %q", got, want)
	}
	if got := PlaceSession(Place{}); got != "" {
		t.Fatalf("a session with no folder named itself %q", got)
	}
}
