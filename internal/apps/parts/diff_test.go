package parts

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func TestLineDiff(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		want           []bridge.DiffLine
	}{
		{"empty", "", "", []bridge.DiffLine{}},
		{"same", "a\nb", "a\nb", []bridge.DiffLine{{Op: " ", Text: "a"}, {Op: " ", Text: "b"}}},
		{"add", "a", "a\nb", []bridge.DiffLine{{Op: " ", Text: "a"}, {Op: "+", Text: "b"}}},
		{"remove", "a\nb", "b", []bridge.DiffLine{{Op: "-", Text: "a"}, {Op: " ", Text: "b"}}},
		{"replace", "a\nb", "a\nc", []bridge.DiffLine{{Op: " ", Text: "a"}, {Op: "-", Text: "b"}, {Op: "+", Text: "c"}}},
		{"trailing newline", "a\n", "a", []bridge.DiffLine{{Op: " ", Text: "a"}, {Op: "-", Text: ""}}},
		{"all new", "", "a", []bridge.DiffLine{{Op: "+", Text: "a"}}},
		{"all removed", "a", "", []bridge.DiffLine{{Op: "-", Text: "a"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := lineDiff(tt.from, tt.to); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestDiffRepeatedLinesPreserveBothBodies(t *testing.T) {
	texts := []string{"", "a", "a\na", "b\na\nb", "a\nb\na", "a\n", "\nb", "a\nb\na\nb"}
	for _, from := range texts {
		for _, to := range texts {
			var before, after []string
			for _, line := range lineDiff(from, to) {
				if line.Op != "+" {
					before = append(before, line.Text)
				}
				if line.Op != "-" {
					after = append(after, line.Text)
				}
			}
			if !reflect.DeepEqual(before, splitLines(from)) || !reflect.DeepEqual(after, splitLines(to)) {
				t.Fatalf("diff lost lines: %q → %q: %v %v", from, to, before, after)
			}
		}
	}
}
