package session

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceSetTaskUsesOwnedSiblingCopies(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	library := newTestRepo(t)
	writeFile(t, filepath.Join(library, "library.txt"), "relative dependency\n")
	app := t.TempDir()
	writeFile(t, filepath.Join(app, "app.txt"), "../library/library.txt\n")
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
	})
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "read both sibling roots", WorkspaceSetRequest{
		Primary: "app", Roots: []WorkspaceRootRequest{
			{Source: app, Slot: "app", Access: WorkspaceWrite},
			{Source: library, Slot: "library", Access: WorkspaceRead},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	t.Cleanup(func() { endBeltRun(t, agent, engine) })
	var wire struct {
		Version int `json:"version"`
	}
	raw, err := os.ReadFile(agent.config.checkpointFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Version != 2 {
		t.Fatalf("workspace-set checkpoint version=%d; a V1 reader could silently resume only primary", wire.Version)
	}
	plan, _, planErr := taskcopy.ReadWorkspacePlan(cell.Cell{Root: place.Dir})
	if planErr != nil {
		t.Fatalf("checkpoint transfer projection: %v", planErr)
	}
	if plan == nil || len(plan.Groups) != 1 || len(plan.Groups[0].Entries) != 2 || !plan.Groups[0].Required {
		t.Fatal("checkpoint lost required-root closure")
	}

	engine.mu.Lock()
	spec := engine.spec
	engine.mu.Unlock()
	if !withinDir(place.Trees(), spec.Workspace) || filepath.Base(spec.Workspace) != "app" {
		t.Fatalf("primary copy = %q", spec.Workspace)
	}
	if got := readFile(t, filepath.Join(spec.Workspace, "..", "library", "library.txt")); got != "relative dependency\n" {
		t.Fatalf("sibling input = %q", got)
	}
	writeFile(t, filepath.Join(spec.Workspace, "private.txt"), "owned output\n")
	if _, err := os.Stat(filepath.Join(app, "private.txt")); !os.IsNotExist(err) {
		t.Fatalf("original changed: %v", err)
	}
	if spec.WorkspaceSet == nil || !strings.Contains(spec.WorkspaceSet.WorkerInstructions(), "library") {
		t.Fatal("worker lacks sibling routing")
	}
	row, ok := runRowOf(agent.graph(), id)
	if !ok || row.Copy == nil || row.Copy.URI == "" {
		t.Fatal("primary copy not recorded")
	}

	joined, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "cooperate on both roots", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: library, Slot: "library", Access: WorkspaceRead}, {Source: app, Slot: "app", Access: WorkspaceWrite}}})
	if err != nil {
		t.Fatalf("same declaration did not share live set: %v", err)
	}
	child, found := runRowOf(agent.graph(), joined)
	if !found || child.Parent != id {
		t.Fatal("cooperating task started independently")
	}
	if _, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "change root authority", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: library, Slot: "library", Access: WorkspaceWrite}}}); err == nil {
		t.Fatal("matching primary silently broadened secondary authority")
	}
}

func TestWorkspaceSetReopensAllCopiesFromDurableCheckpoint(t *testing.T) {
	for _, damage := range []string{"missing", "relocated"} {
		t.Run(damage, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
			t.Setenv("CODEAF_TASK_BELT", "bash")
			app, library := t.TempDir(), newTestRepo(t)
			writeFile(t, filepath.Join(library, "library.txt"), "library input\n")
			place := Place{Dir: t.TempDir(), Workspace: app}
			config := func(c *Config) {
				c.Workspace, c.Place = app, place
				c.SessionFile = filepath.Join(place.Dir, placeTranscript)
			}
			firstEngine := newBeltRunDouble("interrupted")
			firstEngine.honoursStop = true
			registerBeltRunEngine(t, firstEngine)
			first, _ := newTestAgent(t, beltRunCompleter{text: "done"}, config)
			id, _, _, err := first.StartTaskInWorkspaceSet(context.Background(), "resume both roots", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: library, Slot: "library", Access: WorkspaceWrite}}})
			if err != nil {
				t.Fatal(err)
			}
			<-firstEngine.entered
			firstEngine.mu.Lock()
			before := firstEngine.spec
			firstEngine.mu.Unlock()
			writeFile(t, filepath.Join(before.Workspace, "unfinished.txt"), "keep primary\n")
			writeFile(t, filepath.Join(before.Workspace, "..", "library", "unfinished.txt"), "keep sibling\n")
			if done := first.cutBeltRun(); done != nil {
				<-done
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			secondEngine := newBeltRunDouble("resumed")
			secondEngine.honoursStop = true
			registerBeltRunEngine(t, secondEngine)
			second, _ := newTestAgent(t, beltRunCompleter{text: "done"}, config)
			select {
			case <-secondEngine.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("reopened Agent did not resume its workspace set")
			}
			t.Cleanup(func() { endBeltRun(t, second, secondEngine) })
			secondEngine.mu.Lock()
			after := secondEngine.spec
			secondEngine.mu.Unlock()
			if after.Workspace != before.Workspace || after.WorkspaceURI != before.WorkspaceURI {
				t.Fatal("reopen recarved primary copy")
			}
			if after.WorkspaceSet == nil || len(after.WorkspaceSet.Roots) != 2 {
				t.Fatal("checkpoint lost the required sibling")
			}
			if got := readFile(t, filepath.Join(after.Workspace, "unfinished.txt")); got != "keep primary\n" {
				t.Fatalf("primary work=%q", got)
			}
			if got := readFile(t, filepath.Join(after.Workspace, "..", "library", "unfinished.txt")); got != "keep sibling\n" {
				t.Fatalf("sibling work=%q", got)
			}
			if row, ok := runRowOf(second.graph(), id); !ok || row.WorkspaceSet == nil {
				t.Fatal("reopened row lost set metadata")
			}

			// Losing a required sibling must not silently resume the surviving primary.
			if done := second.cutBeltRun(); done != nil {
				<-done
			}
			if err := second.Close(); err != nil {
				t.Fatal(err)
			}
			var missing, copyURI string
			for _, use := range after.WorkspaceSet.Roots {
				if use.Slot == "library" {
					missing = use.Copy.Dir
					copyURI = use.Copy.URI
				}
			}
			if missing == "" {
				t.Fatal("missing sibling fixture")
			}
			if damage == "missing" {
				if err := os.RemoveAll(missing); err != nil {
					t.Fatal(err)
				}
			} else {
				dest := filepath.Join(place.Trees(), "relocated", "library")
				if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(missing, dest); err != nil {
					t.Fatal(err)
				}
				registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := registry.Bind(copyURI, dest); err != nil {
					t.Fatal(err)
				}
			}
			refusedEngine := newBeltRunDouble("must not start")
			refusedEngine.honoursStop = true
			registerBeltRunEngine(t, refusedEngine)
			refused, _ := newTestAgent(t, beltRunCompleter{text: "done"}, config)
			t.Cleanup(func() {
				if refusedEngine.didRun() {
					endBeltRun(t, refused, refusedEngine)
				}
			})
			if _, err := refused.ContinueRun(context.Background(), id); err == nil {
				t.Fatal("missing sibling was ignored")
			}
			nothingStarted(t, refused, refusedEngine)
		})
	}
}

func TestWorkspaceSetChecksHeldSecondaryBeforePreparingAnyCopy(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app, library := t.TempDir(), t.TempDir()
	holdFolder(t, library, "existing library work")
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("unexpected")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place = app, place })
	_, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "use held sibling", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: library, Slot: "library", Access: WorkspaceWrite}}})
	if err == nil {
		<-engine.entered
		endBeltRun(t, agent, engine)
		t.Fatal("held secondary root was admitted")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Fatalf("held-root refusal=%v", err)
	}
	entries, readErr := os.ReadDir(place.Trees())
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatal("copy preparation happened before all-root hold preflight")
	}
	nothingStarted(t, agent, engine)
}

func TestWorkspaceSetRefusesAnotherConversationsOwnedCopy(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	foreignPlace := Place{Dir: t.TempDir()}
	foreignCopy := filepath.Join(foreignPlace.Trees(), "1")
	if err := os.MkdirAll(foreignCopy, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, foreignPlace.Dir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(workspaceidentity.Run, foreignCopy, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: project.URI}); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: t.TempDir()}
	engine := newBeltRunDouble("unexpected")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place = foreignCopy, place })
	_, _, _, err = agent.StartTaskInWorkspaceSet(context.Background(), "copy foreign work", WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: foreignCopy, Slot: "work", Access: WorkspaceWrite}}})
	if err == nil {
		<-engine.entered
		endBeltRun(t, agent, engine)
		t.Fatal("foreign execution copy was silently treated as a new project")
	}
	nothingStarted(t, agent, engine)
}

func TestWorkspaceSetStopKeepsEachRootWithoutLanding(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app, library := t.TempDir(), newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("interrupted")
	engine.honoursStop = true
	engine.early = func(workspace string) {
		writeFile(t, filepath.Join(workspace, "unfinished.txt"), "app work\n")
		writeFile(t, filepath.Join(workspace, "..", "library", "unfinished.txt"), "library work\n")
	}
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
	})
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "work on both roots", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite, Landing: app}, {Source: library, Slot: "library", Access: WorkspaceWrite, Landing: library}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	agent.beltMu.Lock()
	over := agent.beltRun.over
	agent.beltMu.Unlock()
	if _, err := agent.Cancel("task:" + strconv.FormatUint(id, 10)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-over:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not settle")
	}
	row, ok := runRowOf(agent.graph(), id)
	if !ok || !row.Stopped || row.WorkspaceSet == nil {
		t.Fatal("stopped set receipt missing")
	}
	for _, use := range row.WorkspaceSet.Roots {
		if use.Outcome != mergeKept {
			t.Errorf("root %s stop outcome=%q", use.Slot, use.Outcome)
		}
		if _, err := os.Stat(filepath.Join(use.Copy.Dir, "unfinished.txt")); err != nil {
			t.Fatalf("private work lost: %v", err)
		}
	}
	for _, source := range []string{app, library} {
		if _, err := os.Stat(filepath.Join(source, "unfinished.txt")); !os.IsNotExist(err) {
			t.Fatalf("stopped work landed in source: %v", err)
		}
	}
}

func TestWorkspaceSetRejectsNonportableSlotsWithoutRenaming(t *testing.T) {
	for _, slot := range []string{"APP", "café", "con", "trailing."} {
		t.Run(slot, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
			t.Setenv("CODEAF_TASK_BELT", "bash")
			app, other := t.TempDir(), t.TempDir()
			place := Place{Dir: t.TempDir()}
			engine := newBeltRunDouble("unexpected")
			registerBeltRunEngine(t, engine)
			agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place = app, place })
			_, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "portable sibling layout", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: other, Slot: slot, Access: WorkspaceRead}}})
			if err == nil {
				<-engine.entered
				endBeltRun(t, agent, engine)
				t.Fatal("unsupported or colliding slot was admitted")
			}
			entries, readErr := os.ReadDir(place.Trees())
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatal(readErr)
			}
			if len(entries) > 0 {
				t.Fatal("unsupported layout created private copies")
			}
			nothingStarted(t, agent, engine)
		})
	}
}

func TestWorkspaceSetQueuedCellReopensEveryDeclaredRootWithoutCarving(t *testing.T) {
	littleMemoryHost(t)
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app, library := t.TempDir(), t.TempDir()
	c, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: c.Root, Workspace: app}
	if err := SaveMeta(place.Dir, Meta{Workspace: app}); err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	open := func() *Agent {
		agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
			config.Workspace, config.Place, config.SessionFile = app, place, place.Transcript()
			config.ProfileDir = profile
			config.TaskMaxLoad = 0
			config.TaskMinFreeMB = 1 << 40
		})
		return agent
	}
	engine := newBeltRunDouble("must remain queued")
	registerBeltRunEngine(t, engine)
	first := open()
	id, _, _, err := first.StartTaskInWorkspaceSet(context.Background(), "queued two-root work", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: library, Slot: "library", Access: WorkspaceRead}}})
	if err != nil {
		t.Fatal(err)
	}
	first.beltMu.Lock()
	old := first.beltRun
	first.beltMu.Unlock()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.over:
	case <-time.After(5 * time.Second):
		t.Fatal("queued driver did not close")
	}
	second := open()
	row, ok := runRowOf(second.graph(), id)
	if !ok || row.PendingRun == nil || row.PendingRun.WorkspaceSet == nil {
		t.Fatal("queued root declaration did not reopen")
	}
	sources := map[string]string{}
	for _, root := range row.PendingRun.WorkspaceSet.Roots {
		sources[root.Slot] = root.Source
	}
	if sources["app"] != canonicalPath(app) || sources["library"] != canonicalPath(library) {
		t.Fatalf("queued root bindings lost across sealed checkpoint: %#v", sources)
	}
	if engine.didRun() {
		t.Fatal("machine-held task started a worker")
	}
	entries, err := os.ReadDir(place.Trees())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Fatal("queued reopen carved a workspace before admission")
	}
}

func TestWorkspaceSetPreparationFailureKeepsDurableIncompleteOwnership(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app, other := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(app, "first.txt"), "first root bytes\n")
	denied := filepath.Join(other, "denied.txt")
	writeFile(t, denied, "second root input\n")
	if err := os.Chmod(denied, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(denied, 0600) })
	if _, err := os.ReadFile(denied); err == nil {
		t.Skip("this platform cannot enforce the synthetic read denial")
	}
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("must not start")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = app, place, place.Transcript() })
	_, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "prepare both roots", WorkspaceSetRequest{Primary: "a", Roots: []WorkspaceRootRequest{{Source: app, Slot: "a", Access: WorkspaceWrite}, {Source: other, Slot: "z", Access: WorkspaceWrite}}})
	if err == nil {
		<-engine.entered
		endBeltRun(t, agent, engine)
		t.Fatal("unreadable second root started a worker")
	}
	raw, readErr := os.ReadFile(agent.config.checkpointFile())
	if readErr != nil {
		t.Fatalf("prepared first root has no durable incomplete receipt: %v", readErr)
	}
	var document struct {
		Version int         `json:"version"`
		Runs    []runRecord `json:"runs"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 2 || len(document.Runs) != 1 || document.Runs[0].State != TaskFailed || document.Runs[0].WorkspaceSet == nil {
		t.Fatal("incomplete preparation is not recorded as failed V2 work")
	}
	row := document.Runs[0]
	if len(row.WorkspaceSet.Roots) != 2 {
		t.Fatal("incomplete receipt lost the required root declaration")
	}
	first := row.WorkspaceSet.Roots[0].Copy
	if first == nil || first.URI == "" {
		t.Fatal("prepared first copy lost its ownership")
	}
	if got := readFile(t, filepath.Join(first.Dir, "first.txt")); got != "first root bytes\n" {
		t.Fatalf("prepared bytes=%q", got)
	}
	if _, err := agent.ContinueRun(context.Background(), row.ID); err == nil {
		t.Fatal("incomplete set resumed through ordinary continuation")
	}
	nothingStarted(t, agent, engine)
	// Simulate a crash before the task checkpoint could name the prepared copy.
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(agent.config.checkpointFile()); err != nil {
		t.Fatal(err)
	}
	reopened, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = app, place, place.Transcript() })
	kept, ok := runRowOf(reopened.graph(), row.ID)
	if !ok || kept.State != TaskFailed || kept.WorkspaceSet == nil || kept.WorkspaceSet.Roots[0].Copy == nil || kept.WorkspaceSet.Roots[0].Copy.URI != first.URI {
		t.Fatal("preparation journal did not recover incomplete ownership")
	}
	if _, err := reopened.ContinueRun(context.Background(), row.ID); err == nil {
		t.Fatal("journal-only incomplete preparation resumed")
	}
	nothingStarted(t, reopened, engine)
}

func TestWorkspaceSetModelProposalUsesOwnedSiblingCopies(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app, library := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(library, "input.txt"), "sibling input\n")
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	args, err := json.Marshal(map[string]any{
		"title": "Use both roots", "summary": "Build in private sibling copies", "brief": "Read ../library/input.txt and write only private output", "deliverable": "private output", "acceptance": "both roots available",
		"workspace_set": WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: library, Slot: "library", Access: WorkspaceRead}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	completer := &scriptedCompleter{steps: []step{toolStep("propose_task", string(args)), finalText("handed off")}}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
		c.AskConsent = false
		c.TaskAutoApproveSeconds = 0
	})
	events, err := agent.Submit(context.Background(), "use the app and library in private sibling copies")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	select {
	case <-engine.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("model proposal never reached workspace-set worker")
	}
	t.Cleanup(func() { endBeltRun(t, agent, engine) })
	engine.mu.Lock()
	spec := engine.spec
	engine.mu.Unlock()
	if spec.WorkspaceSet == nil || len(spec.WorkspaceSet.Roots) != 2 {
		t.Fatal("model task lost declared roots")
	}
	if got := readFile(t, filepath.Join(spec.Workspace, "..", "library", "input.txt")); got != "sibling input\n" {
		t.Fatal(got)
	}
}

func TestWorkspaceSetModelCannotAuthorizeLanding(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app := t.TempDir()
	args, _ := json.Marshal(map[string]any{"title": "private task", "summary": "private task", "brief": "write a file", "deliverable": "output", "acceptance": "output exists", "workspace_set": WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite, Landing: app}}}})
	completer := &scriptedCompleter{steps: []step{toolStep("propose_task", string(args)), finalText("refused")}}
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Workspace = app
		c.Place = Place{Dir: t.TempDir(), Workspace: app}
		c.AskConsent = false
		c.TaskAutoApproveSeconds = 0
	})
	events, err := agent.Submit(context.Background(), "use a private copy")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	engine.mu.Lock()
	started := engine.spec.Workspace != ""
	engine.mu.Unlock()
	if started {
		t.Fatal("model granted itself borrowed-source landing authority")
	}
	if agent.graph().node(1) != nil {
		t.Fatal("unsafe proposal was admitted")
	}
}

func TestWorkspaceSetCheckpointWriterWaitsForCaptureFence(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app := t.TempDir()
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
	})
	release, err := taskcopy.LockWorkspaceCheckpoint(context.Background(), cell.Cell{Root: place.Dir})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	arrived := make(chan struct{}, 1)
	agent.graph().store.duringWrite = func() {
		select {
		case arrived <- struct{}{}:
		default:
		}
	}
	done := make(chan error, 1)
	go func() {
		_, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "private work", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}}})
		done <- err
	}()
	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("writer never reached checkpoint boundary")
	}
	select {
	case err := <-done:
		t.Fatalf("writer escaped held capture fence: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not continue after capture released fence")
	}
	<-engine.entered
	endBeltRun(t, agent, engine)
}

func TestWorkspaceSetLandingRefusalKeepsPrivateWrites(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app := t.TempDir()
	writeFile(t, filepath.Join(app, "base.txt"), "baseline\n")
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("done")
	engine.landing = RunLanding{Changed: []string{"private.txt"}, Refused: "verification refused landing"}
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
	})
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "produce private output", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite, Landing: app}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	engine.mu.Lock()
	dir := engine.spec.Workspace
	engine.mu.Unlock()
	writeFile(t, filepath.Join(dir, "private.txt"), "unverified output\n")
	endBeltRun(t, agent, engine)
	if _, err := os.Stat(filepath.Join(app, "private.txt")); !os.IsNotExist(err) {
		t.Fatal("refused landing changed borrowed source")
	}
	row, ok := runRowOf(agent.graph(), id)
	if !ok || row.WorkspaceSet.Roots[0].Outcome != mergeAborted {
		t.Fatal("landing refusal reported as successful private retention")
	}
}

func TestWorkspaceSetUnchangedRootCompletesWithoutLandingFailure(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app := t.TempDir()
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("done")
	engine.landing = RunLanding{Refused: runNothingToLand}
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
	})
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "read only", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceRead}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	endBeltRun(t, agent, engine)
	row, _ := runRowOf(agent.graph(), id)
	if row.WorkspaceSet.Roots[0].Outcome == mergeAborted {
		t.Fatal("unchanged read root treated as landing failure")
	}
}

// The live path creates its mirror's repository after the native carve.
// Baselines must come from that admission, not from fixture-seeded task rows.
func TestPlainWorkspaceSetPersistsItsPreWorkerGitBaselines(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app, library := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(app, "input.txt"), "original app\n")
	writeFile(t, filepath.Join(library, "input.txt"), "original library\n")
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
	})
	_, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "produce outputs in two plain roots", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: library, Slot: "library", Access: WorkspaceWrite}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	t.Cleanup(func() { endBeltRun(t, agent, engine) })
	engine.mu.Lock()
	set := engine.spec.WorkspaceSet.Clone()
	engine.mu.Unlock()
	selections := []taskcopy.WorkspaceResultSelection{}
	for _, root := range set.Roots {
		copy := root.Copy
		if copy == nil || copy.CheckBase == "" {
			t.Fatalf("fresh plain root %s has no pre-worker Git baseline", root.Slot)
		}
		if head, err := git(copy.Dir, "rev-parse", "HEAD"); err != nil || strings.TrimSpace(head) != copy.CheckBase {
			t.Fatalf("root %s did not capture its admission HEAD: %v", root.Slot, err)
		}
		selections = append(selections, taskcopy.WorkspaceResultSelection{CopyURI: copy.URI, BaseCommit: copy.CheckBase})
		commitIn(t, copy.Dir, "completed.txt")
	}
	raw, err := os.ReadFile(place.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Runs []struct {
			WorkspaceSet *WorkspaceSet `json:"workspaceSet"`
		} `json:"runs"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Runs) != 1 || document.Runs[0].WorkspaceSet == nil {
		t.Fatal("actual task checkpoint lost workspace set")
	}
	for i, root := range document.Runs[0].WorkspaceSet.Roots {
		if root.Copy.CheckBase != selections[i].BaseCommit {
			t.Fatal("checkpoint replaced the pre-worker baseline after work")
		}
	}
	c := cell.Cell{Root: place.Dir}
	plan, _, err := taskcopy.ReadWorkspacePlan(c)
	if err != nil || plan == nil {
		t.Fatalf("actual task transfer projection: %v", err)
	}
	if err = (taskcopy.Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	outputs, err := taskcopy.RestoreCompletedWorkspaceResults(c, *plan, filepath.Join(t.TempDir(), "results"), selections)
	if err != nil {
		t.Fatalf("actual admitted baselines cannot preserve completed results: %v", err)
	}
	for _, selection := range selections {
		dir := outputs[selection.CopyURI]
		if got := readFile(t, filepath.Join(dir, "completed.txt")); got != "completed.txt\n" {
			t.Fatalf("completed result %q", got)
		}
		if _, err := os.Stat(filepath.Join(dir, "input.txt")); !os.IsNotExist(err) {
			t.Fatal("unchanged input was duplicated as a completed output")
		}
	}
}

func TestFallbackWorkspaceSetPreservesUserDirectoryMetadata(t *testing.T) {
	testWorkspaceSetDirectoryMetadata(t, false)
}
func TestRealNativeWorkspaceSetPreservesUserDirectoryMetadata(t *testing.T) {
	testWorkspaceSetDirectoryMetadata(t, true)
}
func testWorkspaceSetDirectoryMetadata(t *testing.T, native bool) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	wantRung := GroundRungCopy
	if native {
		isolatedRetirementFurrow(t)
		wantRung = GroundRungUniverse
	} else {
		installRefusingFurrow(t)
	}
	app, library := t.TempDir(), t.TempDir()
	for _, root := range []string{app, library} {
		if err := os.MkdirAll(filepath.Join(root, "user-dir", "empty"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(root, "user-dir"), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "user-dir", "private.txt"), []byte("preserve user metadata\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	place := Place{Dir: t.TempDir(), Workspace: app}
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = app, place
		c.SessionFile = filepath.Join(place.Dir, placeTranscript)
	})
	_, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "inspect original directory metadata", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite}, {Source: library, Slot: "library", Access: WorkspaceWrite}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	t.Cleanup(func() { endBeltRun(t, agent, engine) })
	engine.mu.Lock()
	set := engine.spec.WorkspaceSet.Clone()
	engine.mu.Unlock()
	for _, root := range set.Roots {
		if root.Copy.Rung != wantRung {
			t.Fatalf("%s admission used %s, required %s", root.Slot, root.Copy.Rung, wantRung)
		}
		for rel, want := range map[string]os.FileMode{"user-dir": 0750, "user-dir/empty": 0700, "user-dir/private.txt": 0640} {
			info, err := os.Lstat(filepath.Join(root.Copy.Dir, rel))
			if err != nil {
				t.Errorf("%s %s missing: %v", root.Slot, rel, err)
				continue
			}
			if got := info.Mode().Perm(); got != want {
				t.Errorf("%s %s mode=%04o want=%04o", root.Slot, rel, got, want)
			}
		}
	}
}
