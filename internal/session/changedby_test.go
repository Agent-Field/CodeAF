package session

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestChangedByKnowsThePathOfAFileWriterAndNothingElse(t *testing.T) {
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = workspace })
	cases := []struct {
		name string
		tool string
		args string
		want []string
	}{
		{"relative write", "write", `{"path":"src/a.go","content":"x"}`, []string{"src/a.go"}},
		{"absolute edit inside", "edit", `{"path":"` + filepath.Join(workspace, "b.txt") + `"}`, []string{"b.txt"}},
		{"outside the workspace", "write", `{"path":"../elsewhere"}`, nil},
		{"bash may touch anything", "bash", `{"command":"rm -rf x"}`, nil},
		{"unreadable arguments", "write", `not json`, nil},
		{"no path", "write", `{}`, nil},
	}
	for _, c := range cases {
		if got := agent.changedBy(c.tool, json.RawMessage(c.args)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: changed = %v, want %v", c.name, got, c.want)
		}
	}
}
