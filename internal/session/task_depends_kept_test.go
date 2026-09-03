package session

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func keptDependency(t *testing.T, place Place, repo, session string, id uint64, title, path, content string) (string, string) {
	t.Helper()
	tree, err := prepareTaskTree(place, repo, session, id, title)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, path), content)
	merge, detail := tree.comeHome(title, []string{path})
	if merge != mergeKept {
		t.Fatalf("dependency landing = %q (%s), want kept", merge, detail)
	}
	return tree.branch, tree.dir
}

func graphNodeWithKeptDependencies(t *testing.T, agent *Agent, branches ...string) *TaskNode {
	t.Helper()
	graph := agent.graph()
	graph.mu.Lock()
	defer graph.mu.Unlock()
	ids := make([]uint64, 0, len(branches))
	for index, branch := range branches {
		id := uint64(index + 1)
		ids = append(ids, id)
		graph.nodes[id] = &TaskNode{
			graph: graph, id: id, done: make(chan struct{}), state: TaskDone,
			spec: taskSpec{title: "kept dependency"}, branch: branch, merge: mergeKept,
		}
		graph.order = append(graph.order, id)
	}
	id := uint64(len(branches) + 1)
	node := &TaskNode{
		graph: graph, id: id, dependsOn: ids, done: make(chan struct{}), state: TaskRunning,
		spec:   taskSpec{title: "continue from kept work", dependsOn: ids},
		Ground: agent.config.Workspace, Mode: TaskModeWorktree,
	}
	graph.nodes[id] = node
	graph.order = append(graph.order, id)
	return node
}

// C11: one kept dependency is the dependent task's starting commit, so the
// worker sees its file before making any call of its own.
func TestC11ADependentStartsFromOneKeptBranch(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	place := Place{Dir: t.TempDir(), Workspace: repo}
	branch, _ := keptDependency(t, place, repo, "one", 1, "write alpha", "alpha.txt", "alpha\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace, config.Place = repo, place
	})
	node := graphNodeWithKeptDependencies(t, agent, branch)
	tree, ok := agent.openTaskWorld(context.Background(), node, io.Discard)
	if !ok {
		t.Fatalf("dependent did not open: %s", node.notice().Report)
	}
	if got := readFile(t, filepath.Join(tree.dir, "alpha.txt")); got != "alpha\n" {
		t.Fatalf("dependent starts with %q", got)
	}
	if tree.home != "main" {
		t.Fatalf("dependent home = %q, want main", tree.home)
	}
	tree.releaseKept()
}

// C11: every further kept dependency is merged into the dependent's new copy
// before its worker starts.
func TestC11ADependentStartsWithTwoKeptBranchesCombined(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	place := Place{Dir: t.TempDir(), Workspace: repo}
	alpha, _ := keptDependency(t, place, repo, "alpha", 1, "write alpha", "alpha.txt", "alpha\n")
	beta, _ := keptDependency(t, place, repo, "beta", 2, "write beta", "beta.txt", "beta\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace, config.Place = repo, place
	})
	node := graphNodeWithKeptDependencies(t, agent, alpha, beta)
	tree, ok := agent.openTaskWorld(context.Background(), node, io.Discard)
	if !ok {
		t.Fatalf("dependent did not open: %s", node.notice().Report)
	}
	for _, file := range []string{"alpha.txt", "beta.txt"} {
		if got := readFile(t, filepath.Join(tree.dir, file)); got != strings.TrimSuffix(file, ".txt")+"\n" {
			t.Fatalf("%s in dependent = %q", file, got)
		}
	}
	tree.releaseKept()
}

// C11: conflicting kept dependencies stop the task before a worker starts,
// name both branches, and leave no registered working copy behind.
func TestC11ConflictingKeptDependenciesFailBeforeTheWorkerStarts(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	place := Place{Dir: t.TempDir(), Workspace: repo}
	alpha, _ := keptDependency(t, place, repo, "alpha", 1, "write alpha", "shared.txt", "alpha\n")
	beta, _ := keptDependency(t, place, repo, "beta", 2, "write beta", "shared.txt", "beta\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace, config.Place = repo, place
	})
	node := graphNodeWithKeptDependencies(t, agent, alpha, beta)
	_, ok := agent.openTaskWorld(context.Background(), node, io.Discard)
	if ok {
		t.Fatal("a worker was allowed to start on conflicting dependency branches")
	}
	want := "could not prepare a working copy: the branches of the work this depends on do not merge cleanly (" +
		alpha + ", " + beta + ") — merge them yourself first"
	if got := node.notice().Report; got != want {
		t.Fatalf("report = %q, want %q", got, want)
	}
	dir := filepath.Join(place.Trees(), "3")
	if list := gitOut(t, repo, "worktree", "list"); strings.Contains(list, dir) {
		t.Fatalf("the failed dependent stayed registered:\n%s", list)
	}
}
