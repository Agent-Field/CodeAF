package head

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// Unix parity contract 2: padding inside quoted pointers remains unrecognized,
// so it cannot consume a result slot ahead of a valid report.
func TestUnixParityResultPointers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix slash path compatibility")
	}
	node := store.Node{FoldPointers: []string{`" /padded.txt"`, "/report.txt"}}
	if got := collectResultFiles(node, 1); !reflect.DeepEqual(got, []string{"/report.txt"}) {
		t.Fatalf("result pointers = %q", got)
	}
}
