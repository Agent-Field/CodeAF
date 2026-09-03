package session

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// THE QUESTION "WHICH REPOSITORY IS THIS" IS ASKED IN ONE PLACE.
//
// `git rev-parse --show-toplevel` does not answer "is this directory a
// repository". It answers "walk up until you find one", and those are the same
// answer only when the directory is not sitting inside somebody else's tree. A
// t.TempDir() lands inside a checkout whenever GOTMPDIR or TMPDIR names one
// (Go 1.26 roots it there), and every scratch workspace under such a directory
// then grounds on the DEVELOPER'S repository: a real `task/*` branch cut, a
// real "task: Paint" committed, a real merge onto the branch they are standing
// on (#578).
//
// The refusal that stops that walk lives in [repositoryRoot] and only there. So
// a second road that asks git the same question directly would leave the
// temporary directory again with nothing in the way — the refusal is not a
// property of the question, it is a property of the one function that asks it,
// and a law is the only thing that keeps a second asker from being written by
// somebody who never read this file.

// theOneAskerOfTheGround is the function allowed to put that question to git.
// It is named rather than counted, because "there is one of them" is a fact a
// second one satisfies just as well after the first is deleted.
const theOneAskerOfTheGround = "repositoryRoot"

// theGroundQuestion is the flag that makes the question that one. It belongs to
// exactly one git command, so matching it alone is neither loose nor a guess:
// nothing else in this package can spell it and mean something different.
const theGroundQuestion = "--show-toplevel"

// TestOnlyRepositoryRootAsksGitWhereTheRepositoryIs fails when any other
// function in this package's own sources asks git to walk up out of a directory
// and name the repository it lands in, and when [repositoryRoot] stops asking —
// an exemption watching nothing is a law that has quietly retired.
func TestOnlyRepositoryRootAsksGitWhereTheRepositoryIs(t *testing.T) {
	asked := false
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		// The enclosing function is worked out from the source positions rather
		// than by walking each body, so a spelling this test did not think of —
		// the flag inside a []string built at file scope, inside a closure, inside
		// a struct literal handed to something else — is still ATTRIBUTED and
		// still reported, instead of quietly falling outside the walk.
		for _, where := range groundQuestions(file) {
			owner := enclosingFunction(file, where)
			if owner == theOneAskerOfTheGround {
				asked = true
				continue
			}
			named := owner
			if named == "" {
				named = "this file, outside any function,"
			}
			t.Errorf("%s:%d: %s asks git for `rev-parse %s`.\n"+
				"That question walks UP out of the directory it is given, so asked about a "+
				"scratch workspace that happens to sit inside a checkout it answers the "+
				"checkout — and the task grounds on the person's own repository, cuts a "+
				"branch there and commits into it (#578). There is one asker, %s, and the "+
				"refusal that stops the walk lives inside it. Ground through that, or move "+
				"the refusal with the question.",
				filepath.Base(path), fset.Position(where).Line, named, theGroundQuestion, theOneAskerOfTheGround)
		}
	})
	if !asked {
		t.Errorf("%s no longer asks git for `rev-parse %s`, so this law is watching nothing.\n"+
			"If the ground is found some other way now, say so here and check THAT road instead.",
			theOneAskerOfTheGround, theGroundQuestion)
	}
}

// groundQuestions is every place in a file where the flag is spelled as a
// string the compiler will hand to git. A comment that merely discusses it —
// task_run.go explains what the flag does inside a linked worktree — is not a
// literal and is not caught, which is right: prose about a hazard is how the
// hazard stays understood.
func groundQuestions(file *ast.File) []token.Pos {
	var found []token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		if text, err := strconv.Unquote(literal.Value); err == nil && text == theGroundQuestion {
			found = append(found, literal.Pos())
		}
		return true
	})
	return found
}

// enclosingFunction names the function a position falls inside — the
// receiver-qualified name, so `Agent.ground` and a bare `ground` are two
// different answers — and "" when it falls outside every one of them.
func enclosingFunction(file *ast.File, where token.Pos) string {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		if function.Pos() <= where && where <= function.End() {
			return functionName(function)
		}
	}
	return ""
}
