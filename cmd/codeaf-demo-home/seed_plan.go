package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

const (
	demoPlanRoot = "demo-auth"
	demoAgent    = "demo-seeder"
)

// writePlan seeds the belt's task store through the same API a live run uses.
func writePlan(projects map[string]*demoProject, ids map[string]string) error {
	project := projects[firstProjectName]
	chat := ids[roomTalkTitle]
	if project == nil || chat == "" {
		return fmt.Errorf("seed plan: missing project %q or conversation %q", firstProjectName, roomTalkTitle)
	}
	storeDir := filepath.Join(project.dir, ".codeaf")
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return fmt.Errorf("seed plan directory: %w", err)
	}
	plan, err := plandb.Open(filepath.Join(storeDir, "plandb.db"), firstProjectName, demoPlanRoot, "rewrite the auth flow", "", chat)
	if err != nil {
		return fmt.Errorf("seed plan: %w", err)
	}
	defer plan.Close()

	const (
		read       = "demo-read"
		handler    = "demo-handler"
		middleware = "demo-middleware"
		tests      = "demo-tests"
		fixtures   = "demo-fixtures"
		manual     = "demo-manual"
		check      = "demo-read-check"
		poem       = "demo-poem"
		chapter1   = "demo-chapter-1"
		chapter2   = "demo-chapter-2"
		chapter3   = "demo-chapter-3"
		poemCheck  = "demo-poem-check"
		index      = "demo-index"
		indexChild = "demo-index-child"
	)

	_, err = plan.AddMany([]plandb.TaskSpec{
		{ID: read, Title: "read the current flow", ParentID: demoPlanRoot},
		{ID: handler, Title: "write the handler", ParentID: demoPlanRoot},
		{ID: middleware, Title: "write the middleware", ParentID: demoPlanRoot},
		{ID: tests, Title: "write the tests", ParentID: demoPlanRoot, Dependencies: []plandb.Dependency{{TaskID: handler}}},
		{ID: fixtures, Title: "write the fixtures", ParentID: tests, Dependencies: []plandb.Dependency{{TaskID: tests, Kind: plandb.DepSuggests}}},
		{ID: manual, Title: "update the manual", ParentID: demoPlanRoot, Dependencies: []plandb.Dependency{{TaskID: tests}}},
		{ID: check, Title: "check: read the current flow", ParentID: demoPlanRoot, Role: plandb.RoleCheck},
		{ID: poem, Title: "five chapter poem", ParentID: demoPlanRoot},
		{ID: chapter1, Title: "write chapter one", ParentID: poem},
		{ID: chapter2, Title: "write chapter two", ParentID: poem},
		{ID: chapter3, Title: "write chapter three", ParentID: poem},
		{ID: poemCheck, Title: "check: five chapter poem", ParentID: poem, Role: plandb.RoleCheck},
	})
	if err != nil {
		return fmt.Errorf("seed plan tasks: %w", err)
	}
	for _, done := range []struct{ id, result string }{
		{read, "the current flow is summarised"},
		{check, "holds: the flow is read and summarised"},
		{chapter1, "chapter one written"}, {chapter2, "chapter two written"},
		{chapter3, "chapter three written"}, {poemCheck, "holds: all five chapters scan"},
	} {
		if _, err := plan.Claim(done.id, demoAgent); err != nil {
			return fmt.Errorf("claim %s: %w", done.id, err)
		}
		if _, err := plan.Done(done.id, demoAgent, done.result, nil, nil); err != nil {
			return fmt.Errorf("finish %s: %w", done.id, err)
		}
	}
	for _, running := range []string{handler, middleware} {
		if _, err := plan.Claim(running, demoAgent); err != nil {
			return fmt.Errorf("run %s: %w", running, err)
		}
	}
	if _, err := plan.AddMany([]plandb.TaskSpec{{ID: index, Title: "index the poems", ParentID: demoPlanRoot}}); err != nil {
		return fmt.Errorf("seed index run: %w", err)
	}
	if _, err := plan.Claim(index, demoAgent); err != nil {
		return fmt.Errorf("run index: %w", err)
	}
	if _, err := plan.AddMany([]plandb.TaskSpec{{ID: indexChild, Title: "read the poem index", ParentID: index}}); err != nil {
		return fmt.Errorf("seed index child: %w", err)
	}
	if _, err := plan.Claim(indexChild, demoAgent); err != nil {
		return fmt.Errorf("run index child: %w", err)
	}
	if _, err := plan.Fail(indexChild, demoAgent, "the index source is unavailable"); err != nil {
		return fmt.Errorf("fail index child: %w", err)
	}
	if _, err := plan.Done(index, demoAgent, "indexed the available poems", nil, nil); err != nil {
		return fmt.Errorf("finish index: %w", err)
	}

	if err := plan.SetLive(handler, 12, "$ go test ./internal/auth/..."); err != nil {
		return err
	}
	if err := plan.SetLive(middleware, 4, "$ cat > internal/auth/mw.go <<'EOF'"); err != nil {
		return err
	}
	if _, err := plan.AddPersonNote(handler, "keep the middleware order"); err != nil {
		return err
	}
	if err := plan.AddSpend(demoPlanRoot, "glm-5.3-flash", plandb.RoleWork, 0.08, 1800, 240); err != nil {
		return err
	}
	if err := plan.AddSpend(demoPlanRoot, "glm-5.3", plandb.RolePlan, 0.04, 900, 120); err != nil {
		return err
	}
	if err := plan.AddSpend(read, "glm-5.3-flash", plandb.RoleWork, 0.03, 600, 90); err != nil {
		return err
	}
	if err := plan.AddSpend(handler, "glm-5.3-flash", plandb.RoleWork, 0.12, 2400, 360); err != nil {
		return err
	}

	counts := map[string]int{demoPlanRoot: 1, read: 6, handler: 12, middleware: 4, tests: 1, fixtures: 1, manual: 1, check: 1, poem: 1, chapter1: 1, chapter2: 1, chapter3: 1, poemCheck: 1, index: 1, indexChild: 1}
	for taskID, count := range counts {
		if err := writeDemoTrajectory(storeDir, taskID, count); err != nil {
			return err
		}
	}
	return nil
}

func writeDemoTrajectory(storeDir, taskID string, count int) error {
	dir := plandb.TaskDir(storeDir, taskID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("trajectory directory %s: %w", taskID, err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "trajectory.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("trajectory %s: %w", taskID, err)
	}
	writer := bufio.NewWriter(file)
	for step := 1; step <= count; step++ {
		line := struct {
			Kind        string `json:"kind"`
			Step        int    `json:"step"`
			Command     string `json:"command"`
			Observation string `json:"observation"`
		}{"step", step, fmt.Sprintf("$ demo step %02d", step), "completed"}
		if err := json.NewEncoder(writer).Encode(line); err != nil {
			_ = file.Close()
			return fmt.Errorf("trajectory %s: %w", taskID, err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
