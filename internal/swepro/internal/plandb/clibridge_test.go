package plandb

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Port of src/plandb/cli-bridge.test.ts. Subtest names are byte-identical to
// the bun:test `test(...)` names so coverage can be diffed 1:1.

// cbJSONOut mirrors the TS `jsonOut` helper: non-zero exit is fatal, an empty
// stdout yields `undefined` (nil here), otherwise the stdout is JSON-parsed.
func cbJSONOut(t *testing.T, r RunResult) any {
	t.Helper()
	if r.Code != 0 {
		t.Fatalf("exit %d: %s", r.Code, string(r.Stderr))
	}
	s := string(r.Stdout)
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, s)
	}
	return v
}

func cbObj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %T (%v)", v, v)
	}
	return m
}

func cbArr(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("expected JSON array, got %T (%v)", v, v)
	}
	return a
}

func cbStr(t *testing.T, v any, key string) string {
	t.Helper()
	m := cbObj(t, v)
	s, ok := m[key].(string)
	if !ok {
		t.Fatalf("field %q is not a string: %T (%v)", key, m[key], m[key])
	}
	return s
}

func cbNum(t *testing.T, v any, key string) float64 {
	t.Helper()
	m := cbObj(t, v)
	n, ok := m[key].(float64)
	if !ok {
		t.Fatalf("field %q is not a number: %T (%v)", key, m[key], m[key])
	}
	return n
}

func TestCLIBridgePlanDBArgvRouting(t *testing.T) {
	t.Run("init + add + list", func(t *testing.T) {
		ResetPlanDBForTesting()

		cbJSONOut(t, RunPlanDB([]string{"plandb", "init", "demo"}))
		added := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "first task", "--json", "--project", "demo", "--kind", "code"}))
		if got := cbStr(t, added, "title"); got != "first task" {
			t.Errorf("added.title = %q, want %q", got, "first task")
		}
		if got := cbStr(t, added, "kind"); got != "code" {
			t.Errorf("added.kind = %q, want %q", got, "code")
		}
		if got := cbStr(t, added, "status"); got != "ready" {
			t.Errorf("added.status = %q, want %q", got, "ready")
		}
		list := cbArr(t, cbJSONOut(t, RunPlanDB([]string{"plandb", "list", "--json", "--project", "demo"})))
		if len(list) != 1 {
			t.Fatalf("list length = %d, want 1", len(list))
		}
		if got, want := cbStr(t, list[0], "id"), cbStr(t, added, "id"); got != want {
			t.Errorf("list[0].id = %q, want %q", got, want)
		}
	})

	t.Run("claim via 'task claim' subcommand", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		task := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "x", "--json", "--project", "demo"}))
		claimed := cbJSONOut(t, RunPlanDB([]string{"plandb", "task", "claim", cbStr(t, task, "id"), "--agent", "worker-1", "--json"}))
		if got := cbStr(t, claimed, "agent_id"); got != "worker-1" {
			t.Errorf("claimed.agent_id = %q, want %q", got, "worker-1")
		}
		if got := cbStr(t, claimed, "status"); got != "claimed" {
			t.Errorf("claimed.status = %q, want %q", got, "claimed")
		}
	})

	t.Run("task fail / task cancel via subcommand", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		task := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "x", "--json", "--project", "demo"}))
		RunPlanDB([]string{"plandb", "task", "claim", cbStr(t, task, "id"), "--agent", "w", "--json"})
		failed := cbJSONOut(t, RunPlanDB([]string{"plandb", "task", "fail", cbStr(t, task, "id"), "--error", "boom"}))
		if got := cbStr(t, failed, "status"); got != "failed" {
			t.Errorf("failed.status = %q, want %q", got, "failed")
		}
		if got := cbStr(t, failed, "error"); got != "boom" {
			t.Errorf("failed.error = %q, want %q", got, "boom")
		}

		t2 := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "y", "--json", "--project", "demo"}))
		cx := cbJSONOut(t, RunPlanDB([]string{"plandb", "task", "cancel", cbStr(t, t2, "id")}))
		if got := cbStr(t, cx, "status"); got != "cancelled" {
			t.Errorf("cx.status = %q, want %q", got, "cancelled")
		}
	})

	t.Run("list --parent filters to direct children only", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		p := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "parent", "--json", "--project", "demo"}))
		pid := cbStr(t, p, "id")
		a := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "child1", "--json", "--project", "demo", "--parent", pid}))
		b := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "child2", "--json", "--project", "demo", "--parent", pid}))
		cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "unrelated", "--json", "--project", "demo"}))
		kids := cbArr(t, cbJSONOut(t, RunPlanDB([]string{"plandb", "list", "--parent", pid, "--json"})))

		gotIDs := make([]string, 0, len(kids))
		for _, k := range kids {
			gotIDs = append(gotIDs, cbStr(t, k, "id"))
		}
		sort.Strings(gotIDs)
		wantIDs := []string{cbStr(t, a, "id"), cbStr(t, b, "id")}
		sort.Strings(wantIDs)
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Errorf("kids ids = %v, want %v", gotIDs, wantIDs)
		}
	})

	t.Run("done via 'done' command (no task subcommand)", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		task := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "x", "--json", "--project", "demo"}))
		RunPlanDB([]string{"plandb", "task", "claim", cbStr(t, task, "id"), "--agent", "w", "--json"})
		done := cbJSONOut(t, RunPlanDB([]string{"plandb", "done", cbStr(t, task, "id"), "--json", "--result", "all good"}))
		if got := cbStr(t, done, "status"); got != "done" {
			t.Errorf("done.status = %q, want %q", got, "done")
		}
		if got := cbStr(t, done, "result"); got != "all good" {
			t.Errorf("done.result = %q, want %q", got, "all good")
		}
	})

	t.Run("done blocked by open child", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		parent := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "parent", "--json", "--project", "demo"}))
		cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "child", "--json", "--project", "demo", "--parent", cbStr(t, parent, "id")}))
		r := RunPlanDB([]string{"plandb", "done", cbStr(t, parent, "id"), "--json"})
		if r.Code == 0 {
			t.Errorf("exit code = 0, want non-zero")
		}
		if !regexp.MustCompile(`(?i)descendant`).MatchString(string(r.Stderr)) {
			t.Errorf("stderr = %q, want match /descendant/i", string(r.Stderr))
		}
	})

	t.Run("status returns counts", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		RunPlanDB([]string{"plandb", "add", "a", "--json", "--project", "demo"})
		RunPlanDB([]string{"plandb", "add", "b", "--json", "--project", "demo"})
		out := cbJSONOut(t, RunPlanDB([]string{"plandb", "status", "--json", "--full", "--project", "demo"}))
		status := cbObj(t, out)["status"]
		if got := cbNum(t, status, "total"); got != 2 {
			t.Errorf("status.total = %v, want 2", got)
		}
		if got := cbNum(t, status, "ready"); got != 2 {
			t.Errorf("status.ready = %v, want 2", got)
		}
	})

	t.Run("amend prepends to description", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		task := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "x", "--json", "--project", "demo", "--description", "original"}))
		amended := cbJSONOut(t, RunPlanDB([]string{"plandb", "task", "amend", cbStr(t, task, "id"), "--prepend", "NOTE: extra", "--json"}))
		// TS: amended.description?.startsWith("NOTE: extra") === true — an
		// absent/null description would make the optional chain undefined and
		// fail the assertion, so a non-string here is a failure.
		desc, ok := cbObj(t, amended)["description"].(string)
		if !ok || !strings.HasPrefix(desc, "NOTE: extra") {
			t.Errorf("amended.description = %v, want prefix %q", cbObj(t, amended)["description"], "NOTE: extra")
		}
	})

	t.Run("split parallel + sequential", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		p := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "p", "--json", "--project", "demo"}))
		parallel := cbArr(t, cbJSONOut(t, RunPlanDB([]string{"plandb", "split", cbStr(t, p, "id"), "--into", "A, B, C", "--json"})))
		if len(parallel) != 3 {
			t.Fatalf("parallel length = %d, want 3", len(parallel))
		}
		every := true
		for _, task := range parallel {
			if cbStr(t, task, "status") != "ready" {
				every = false
			}
		}
		if !every {
			t.Errorf("parallel.every(status === \"ready\") = false, want true")
		}
	})

	t.Run("context + contexts + search", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		RunPlanDB([]string{"plandb", "context", "tokio runtime explained", "--project", "demo", "--kind", "discovery", "--json"})
		ctxs := cbArr(t, cbJSONOut(t, RunPlanDB([]string{"plandb", "contexts", "--project", "demo", "--json"})))
		if len(ctxs) != 1 {
			t.Fatalf("contexts length = %d, want 1", len(ctxs))
		}
		hits := cbArr(t, cbJSONOut(t, RunPlanDB([]string{"plandb", "search", "tokio", "--project", "demo", "--json"})))
		if !(len(hits) > 0) {
			t.Errorf("search hits length = %d, want > 0", len(hits))
		}
	})

	t.Run("dep flag parsing", func(t *testing.T) {
		ResetPlanDBForTesting()

		RunPlanDB([]string{"plandb", "init", "demo"})
		a := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "a", "--json", "--project", "demo"}))
		b := cbJSONOut(t, RunPlanDB([]string{"plandb", "add", "b", "--json", "--project", "demo", "--dep", cbStr(t, a, "id") + ":feeds_into"}))
		if got := cbStr(t, b, "status"); got != "pending" {
			t.Errorf("b.status = %q, want %q", got, "pending")
		}
	})

	t.Run("unknown op returns nonzero exit", func(t *testing.T) {
		ResetPlanDBForTesting()

		r := RunPlanDB([]string{"plandb", "frobnicate"})
		if r.Code == 0 {
			t.Errorf("exit code = 0, want non-zero")
		}
	})
}
