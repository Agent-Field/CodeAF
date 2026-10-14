package prompt

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const queueScreen = `{"screen":"queue","title":"Queue","nav":{"part":"Nav"},"body":{"part":"Text","props":{"text":"hi"}}}`

func TestParseFilesForNormalizesWrongShapes(t *testing.T) {
	tests := []struct {
		name, reply string
		want        []string
		path        string
	}{
		{"bare screen by id", queueScreen, []string{"screens/home.json", "screens/queue.json"}, "screens/queue.json"},
		{"bare screen only one asked", strings.Replace(queueScreen, `"queue"`, `"approvals"`, 1), []string{"screens/queue.json"}, "screens/queue.json"},
		{"bare screen without id", `{"title":"Queue","body":{"part":"Text"}}`, []string{"screens/queue.json"}, "screens/queue.json"},
		{"fenced bare screen", "Here it is:\n```json\n" + queueScreen + "\n```", []string{"screens/queue.json"}, "screens/queue.json"},
		{"files list", `{"files":[{"path":"screens/queue.json","content":` + queueScreen + `}]}`, nil, "screens/queue.json"},
		{"files list name", `{"files":[{"name":"agent.md","contents":"hello"}]}`, nil, "agent.md"},
		{"files object", `{"files":{"screens/queue.json":` + queueScreen + `}}`, nil, "screens/queue.json"},
		{"folder nesting", `{"screens":{"queue.json":` + queueScreen + `}}`, nil, "screens/queue.json"},
		{"folder nesting with slash", `{"screens/":{"queue.json":` + queueScreen + `},"agent.md":"x"}`, nil, "screens/queue.json"},
		{"one wrapper too deep", `{"app":{"screens/queue.json":` + queueScreen + `}}`, nil, "screens/queue.json"},
		{"wrapper around folders", `{"output":{"screens":{"queue.json":` + queueScreen + `}}}`, nil, "screens/queue.json"},
		{"content wrapped", `{"screens/queue.json":{"content":` + queueScreen + `}}`, nil, "screens/queue.json"},
		{"yaml content wrapped", `{"runs/chase.yaml":{"content":"run: chase\nsteps: []\n"}}`, nil, "runs/chase.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := ParseFilesFor(tt.reply, tt.want)
			if err != nil {
				t.Fatal(err)
			}
			if len(files[tt.path]) == 0 {
				t.Fatalf("files=%v, want %s", files, tt.path)
			}
		})
	}
}

func TestParseFilesForReportsAmbiguousShapes(t *testing.T) {
	tests := []struct {
		name, reply string
		want, keys  []string
	}{
		{"bare screen two asked no id", `{"title":"Queue","body":{"part":"Text"}}`,
			[]string{"screens/home.json", "screens/queue.json"}, []string{"body", "title"}},
		{"bare screen no screen asked", queueScreen, []string{"app.json", "agent.md"}, []string{"body", "nav", "screen", "title"}},
		{"bare screen unknown want", queueScreen, nil, []string{"body", "nav", "screen", "title"}},
		{"list with bad path", `{"files":[{"path":"notes.txt","content":"x"}]}`, nil, []string{"files"}},
		{"two wrappers", `{"a":{"app.json":{}},"b":{"agent.md":"x"}}`, nil, []string{"a", "b"}},
		{"inline app", `{"records":{},"budget":{}}`, []string{"app.json"}, []string{"budget", "records"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFilesFor(tt.reply, tt.want)
			var shape *ShapeError
			if !errors.As(err, &shape) || !reflect.DeepEqual(shape.Keys, tt.keys) {
				t.Fatalf("err=%v, want shape error with keys %v", err, tt.keys)
			}
			for _, p := range tt.want {
				if !strings.Contains(err.Error(), p) {
					t.Fatalf("hint %q does not name %s", err, p)
				}
			}
			if strings.HasPrefix(err.Error(), "prompt:") {
				t.Fatalf("hint carries the package prefix: %q", err)
			}
		})
	}
}

func TestContentUnwrapKeepsRealFiles(t *testing.T) {
	files, err := ParseFiles(`{"app.json":{"content":"x","id":"a"}}`)
	if err != nil || !strings.Contains(string(files["app.json"]), `"content": "x"`) {
		t.Fatalf("files=%v err=%v", files, err)
	}
}
