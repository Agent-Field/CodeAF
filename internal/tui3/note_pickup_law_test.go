package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE NOTE'S PICKUP SENTENCE IS SPELLED ONCE (contract 2a and 2b). The chat's
// receipt for a note on a run's row and the room's line under the same stored
// note are one promise, and two string literals of it would each pass their own
// test on the day one of them changed. So the sources themselves are read: in
// this package and the session's, exactly one literal says it, the room's
// constant is that one, and the manual quotes it exactly.
func TestTheNotePickupSentenceIsSpelledOnce(t *testing.T) {
	count := 0
	for _, dir := range []string{".", "../session"} {
		packages, err := parser.ParseDir(token.NewFileSet(), dir, func(info os.FileInfo) bool {
			return !strings.HasSuffix(info.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range packages {
			for _, file := range pkg.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					literal, ok := node.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						return true
					}
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Errorf("unquote %s: %v", literal.Value, err)
					}
					if strings.Contains(value, "reads a note at its next step") {
						count++
					}
					return true
				})
			}
		}
	}
	if count != 1 {
		t.Fatalf("note pickup sentence has %d Go definitions, want one", count)
	}
	if taskPlanPickupWord != session.RunNotePickupWord {
		t.Fatal("the room's note pickup words differ from the chat receipt")
	}
	if !manual.Chat().Mentions(session.RunNotePickupWord) {
		t.Fatal("the manual does not quote the note pickup sentence")
	}
}
