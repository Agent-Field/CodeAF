package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// A PROGRAM WORKS IN THE FOLDER IT WAS GIVEN. A chat opened in a plain folder
// made ~/Desktop/pong, named it as ground and said `in place`; the ladder's
// `in place` rung answered with the conversation's folder before ground was
// read, and senior-dev was handed the person's home folder. A program's folder
// is its ground, or the conversation's folder when it names none, `where` is
// not read for it, and it is that folder itself, never a copy. A ground that
// is not there yet is taken when the folder it would be made in is there, and
// made only when the run starts; one with nowhere to be made is refused.
func TestAProgramWorksInTheGroundItWasGivenAndNowhereElse(t *testing.T) {
	conversation := t.TempDir()
	ground := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = conversation
		config.Delegates = testPrograms("fake")
	})
	for _, where := range []string{"in place", "", filepath.Join(conversation, "elsewhere")} {
		stand := agent.resolveTaskGround(taskSpec{via: "fake", where: where, ground: ground, brief: "build it", deliverable: "the game", acceptance: "it runs"})
		if stand.refusal != "" || stand.ask != "" || stand.dir != canonicalPath(ground) || stand.mode != TaskModeInPlace {
			t.Fatalf("where %q: stand = %+v, want the ground %s itself", where, stand, canonicalPath(ground))
		}
	}
	stand := agent.resolveTaskGround(taskSpec{via: "fake", where: "in place", brief: "build it", deliverable: "the game", acceptance: "it runs"})
	if stand.refusal != "" || stand.dir != canonicalPath(conversation) {
		t.Fatalf("no ground: stand = %+v, want the conversation's folder %s", stand, canonicalPath(conversation))
	}
	fresh := filepath.Join(conversation, "pong")
	if stand := agent.resolveTaskGround(taskSpec{via: "fake", ground: fresh, brief: "b", deliverable: "d", acceptance: "a"}); stand.refusal != "" || stand.dir != canonicalPath(fresh) {
		t.Fatalf("a new folder whose parent is there: stand = %+v, want it taken as %s", stand, canonicalPath(fresh))
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("the card made the folder before anybody approved the work: %v", err)
	}
	if stand := agent.resolveTaskGround(taskSpec{via: "fake", ground: filepath.Join(conversation, "missing", "deeper"), brief: "b", deliverable: "d", acceptance: "a"}); !strings.Contains(stand.refusal, "not there") {
		t.Fatalf("a ground with nowhere to be made: stand = %+v, want the refusal", stand)
	}
}

// A PROGRAM IS NEVER HANDED THE HOME FOLDER, or one above it: it is not a
// project, and senior-dev on a folder with no git history snapshots all of it.
// Both doors refuse it and say what to do instead; a folder under it is fine.
func TestAProgramIsNeverHandedTheHomeFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "Desktop", "pong")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = home
		config.Delegates = testPrograms("fake")
	})
	spec := taskSpec{via: "fake", where: "in place", brief: "build pong", deliverable: "the game", acceptance: "it runs"}
	want := "fake works in one project's folder, and " + canonicalPath(home) + " is your home folder; say which folder the work is in, as ground"
	if stand := agent.resolveTaskGround(spec); stand.refusal != want {
		t.Fatalf("the conversation's folder is home: refusal = %q, want %q", stand.refusal, want)
	}
	above := spec
	above.ground = filepath.Dir(home)
	if stand := agent.resolveTaskGround(above); !strings.Contains(stand.refusal, canonicalPath(filepath.Dir(home))+" holds your home folder") {
		t.Fatalf("a ground above home: stand = %+v", stand)
	}
	under := spec
	under.ground = project
	if stand := agent.resolveTaskGround(under); stand.refusal != "" || stand.dir != canonicalPath(project) {
		t.Fatalf("a ground under home: stand = %+v, want %s", stand, canonicalPath(project))
	}
	_, _, _, err := agent.StartDelegate(context.Background(), "fake", "build pong")
	if err == nil || !strings.Contains(err.Error(), "is your home folder; open codeaf in that folder") {
		t.Fatalf("/fake typed in the home folder: err = %v", err)
	}
}

// THE RECEIPT NAMES THE FOLDER AND THE BRANCH, read off the record the run
// wrote as it started rather than off a live run a program that died at once
// has left.
func TestAProgramsReceiptNamesItsFolder(t *testing.T) {
	tree := testPrograms("fake")[0]
	repo := newTestRepo(t)
	plain := t.TempDir()
	record := &TaskCopyRecord{Dir: repo, Branch: "task/pong-abc123", Home: "work"}
	if got, want := delegateReceipt(repo, tree, record), "It is fake's: it works alone in "+repo+" itself, on a new branch task/pong-abc123; your branch work does not move, and when it ends task/pong-abc123 stays checked out there with its work. Until it ends, codeaf's own tools write nothing in "+repo+"."; got != want {
		t.Fatalf("the receipt for a repository = %q, want %q", got, want)
	}
	if got, want := delegateReceipt(plain, tree, &TaskCopyRecord{Dir: plain}), "It is fake's: it works alone in "+plain+" itself, which has no git history, so its changes are there as it makes them. Until it ends, codeaf's own tools write nothing in "+plain+"."; got != want {
		t.Fatalf("the receipt for a plain folder = %q, want %q", got, want)
	}
	got := delegateStartedReceipt(3, "Pong", "", delegateReceipt(plain, tree, nil), "")
	if !strings.HasPrefix(got, "task 3 started: Pong\nIt is fake's: it works alone in "+plain+" itself") || strings.Contains(got, "a copy of its own") || !strings.Contains(got, taskHandoffWakeSentence) {
		t.Fatalf("the started receipt = %q", got)
	}
}

// THE CARD NAMES THE PROJECT. A program's card said `where:` and the path its
// copy would have under codeaf's state; it says the folder itself, and that it
// gets a branch of its own there when the folder is a repository.
func TestAProgramsCardNamesTheProject(t *testing.T) {
	repo, plain := newTestRepo(t), t.TempDir()
	config := Config{Workspace: t.TempDir(), Delegates: testPrograms("fake")}
	if got := taskCardWhere(config, 1, taskSpec{via: "fake", ground: repo}); got != repo+", on a branch of its own" {
		t.Fatalf("a repository's card says where: %q", got)
	}
	if got := taskCardWhere(config, 1, taskSpec{via: "fake", ground: plain}); got != plain {
		t.Fatalf("a plain folder's card says where: %q", got)
	}
}

// A PROGRAM WORKS WITH THE MODELS THE PERSON ASKED FOR. The card showed the
// model a proposal named and the run was handed the crew's; now one word or
// several (comma-separated) resolve to the models the run is handed, a word
// that names no model is refused, and a proposal naming none is handed the
// crew rather than the default a task's card shows.
func TestAProgramWorksWithTheModelsThePersonAskedFor(t *testing.T) {
	router := modelsource.DefaultSource("https://openrouter.ai/api/v1")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.TaskModels = func() []string {
			return []string{"moonshotai/kimi-k2.6", "z-ai/glm-5.1", "z-ai/glm-5.3-flash", "deepseek/deepseek-v4-pro"}
		}
		config.Sources = modelsource.NewSet(modelsource.Connected{Source: router, Key: "sk-or-v1-routerkey0000000000", Address: router.Address})
	})
	choice := agent.resolveProgramModels("kimi-k2.6, deepseek-v4-pro")
	if choice.problem != "" || choice.model != "moonshotai/kimi-k2.6,deepseek/deepseek-v4-pro" {
		t.Fatalf("two words = %+v", choice)
	}
	if got := programAsked(taskSpec{modelWord: "kimi-k2.6, deepseek-v4-pro", model: choice.model}); strings.Join(got, " ") != "moonshotai/kimi-k2.6 deepseek/deepseek-v4-pro" {
		t.Fatalf("asked = %q", got)
	}
	if choice := agent.resolveProgramModels("kimi-k2.6, nosuchmodel"); choice.problem == "" {
		t.Fatalf("a word naming no model was not refused: %+v", choice)
	}
	if choice := agent.resolveProgramModels("glm, kimi-k2.6"); choice.problem == "" {
		t.Fatalf("a word naming several models in a list was not refused: %+v", choice)
	}
	if got := programAsked(taskSpec{model: "z-ai/glm-5.3-flash"}); got != nil {
		t.Fatalf("a proposal naming no model asked for %q", got)
	}
	if got := delegateStartedReceipt(2, "Invaders", "moonshotai/kimi-k2.6", "It is fake's.", ""); !strings.HasPrefix(got, "task 2 started on moonshotai/kimi-k2.6: Invaders\n") {
		t.Fatalf("receipt = %q", got)
	}
}

// A MODEL NO CONNECTED SERVICE SERVES IS REFUSED BY NAME, before a card. It was
// handed to the program and every call on it was answered on the crew's
// working seat instead: the person asked for one model and got another.
func TestAProgramIsNotHandedAModelNoServiceServes(t *testing.T) {
	router := modelsource.DefaultSource("https://openrouter.ai/api/v1")
	proxy := modelsource.Source{ID: modelsource.CustomID, Written: "mybox", Name: "mybox", Address: "http://127.0.0.1:9000/v1"}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.TaskModels = func() []string { return []string{"moonshotai/kimi-k2.6", "mybox/qwen3-coder"} }
		config.Sources = modelsource.NewSet(
			modelsource.Connected{Source: router, Address: router.Address},
			modelsource.Connected{Source: proxy, Key: "local", Address: proxy.Address},
		)
	})
	if choice := agent.resolveProgramModels("kimi-k2.6"); !strings.Contains(choice.problem, "none of the model services connected here can serve moonshotai/kimi-k2.6") {
		t.Fatalf("an unserved model = %+v", choice)
	}
	if choice := agent.resolveProgramModels("qwen3-coder, kimi-k2.6"); !strings.Contains(choice.problem, "none of the model services connected here can serve moonshotai/kimi-k2.6") {
		t.Fatalf("an unserved model in a list = %+v", choice)
	}
	if choice := agent.resolveProgramModels("qwen3-coder"); choice.problem != "" || choice.model != "mybox/qwen3-coder" {
		t.Fatalf("a served model = %+v", choice)
	}
	// A model spelled with a connected service's prefix is taken as written,
	// though the catalog lists nothing of that service.
	if choice := agent.resolveProgramModels("mybox/some-local-model"); choice.problem != "" || choice.model != "mybox/some-local-model" {
		t.Fatalf("a model named with its service = %+v", choice)
	}
	if choice := agent.resolveProgramModels("openrouter/moonshotai/kimi-k2.6"); !strings.Contains(choice.problem, "serve openrouter/moonshotai/kimi-k2.6") {
		t.Fatalf("a keyless service's model named with its prefix = %+v", choice)
	}
	if choice := agent.resolveProgramModels(""); choice.problem != "" {
		t.Fatalf("no model named was refused: %+v", choice)
	}
}
