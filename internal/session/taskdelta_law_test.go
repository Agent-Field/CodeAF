package session

// taskdelta_law_test.go — THE LAW OF THE ONE DISK WRITE THIS READING OWES.
//
// `a.mu` is the lock a steer, a cut, a named interruption and every drain take
// before a request leaves. A stat, a marshal and a file write performed inside
// it is milliseconds on a person's path for a lookup file nobody is reading yet,
// and it is the exact shape #876 lifted out of the meta stamp (placemeta.go's
// [Agent.stampUserLocked]). The delta reading owes the same kind of file, and it
// must owe it the same way: decided on the path, written behind it.
//
// A RUNTIME TEST CANNOT HOLD THIS. [offpath.Write.Owe] starts the write at once
// on a goroutine of its own, so "the file is not there yet" is a race with the
// performer rather than a property — a fixture asserting it would be green on a
// busy machine and red on a quiet one, which is the shape of flake this change
// is about. What IS a property is where the call SITS, and the tree can see it.
//
// IT READS THE TREE ITSELF, so it runs on the laws gate of every pull request
// (scripts/laws.sh finds it by this import).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestTheToldStampIsOwedAndNeverWrittenOnThePath holds every call of [NoteTold]
// in this package to sitting inside the closure handed to an `Owe`.
func TestTheToldStampIsOwedAndNeverWrittenOnThePath(t *testing.T) {
	set := token.NewFileSet()
	files := parsePackage(t, set)
	seen := 0
	for _, file := range files {
		calls, complaints := owedStampLaw(set, file)
		seen += calls
		for _, complaint := range complaints {
			t.Error(complaint)
		}
	}
	if seen == 0 {
		t.Fatal("no call of NoteTold found in the package; the law is reading the wrong tree")
	}
}

// owedStampLaw answers how many stamp writes this file makes and what is wrong
// with the ones that are not owed.
func owedStampLaw(set *token.FileSet, file *ast.File) (int, []string) {
	owed := owedClosures(file)
	calls := 0
	var complaints []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !namesFunction(call.Fun, "NoteTold") {
			return true
		}
		calls++
		for _, span := range owed {
			if call.Pos() > span[0] && call.End() < span[1] {
				return true
			}
		}
		complaints = append(complaints, fmt.Sprintf(
			"%s: the told stamp is not owed to [Agent.toldStamp] here — either it is written straight "+
				"onto the path, under the lock every request goes through (#876), or it is owed to "+
				"another writer, whose patch replaces this one and drops the stamp. Hand it to "+
				"a.toldStamp().Owe and let [Agent.SettleWrites] land it.",
			set.Position(call.Pos())))
		return true
	})
	return calls, complaints
}

// owedClosures is the span of every function literal handed to THIS STAMP'S OWN
// writer's `Owe`, which is the one place in this package the told stamp may be
// performed.
//
// THE RECEIVER IS HALF THE LAW. A [stampWriter] coalesces by REPLACING its
// patch, so a told stamp owed to the meta writer is a told stamp the next meta
// stamp drops on the floor — a write that is off the path and also never
// happens. Accepting any `Owe` would let that through while looking exactly as
// green as the honest shape.
func owedClosures(file *ast.File) [][2]token.Pos {
	var spans [][2]token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !namesFunction(call.Fun, "Owe") || !owedToTheToldStamp(call.Fun) {
			return true
		}
		for _, argument := range call.Args {
			if literal, ok := argument.(*ast.FuncLit); ok {
				spans = append(spans, [2]token.Pos{literal.Pos(), literal.End()})
			}
		}
		return true
	})
	return spans
}

// owedToTheToldStamp reports whether this `Owe` is the told stamp's own —
// `<something>.toldStamp().Owe(…)`.
func owedToTheToldStamp(fun ast.Expr) bool {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := selector.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	return namesFunction(receiver.Fun, "toldStamp")
}

// namesFunction reports whether this callee is the named function, however it is
// reached: a bare name, or a selector on something.
func namesFunction(fun ast.Expr, name string) bool {
	switch callee := fun.(type) {
	case *ast.Ident:
		return callee.Name == name
	case *ast.SelectorExpr:
		return callee.Sel.Name == name
	}
	return false
}

// AND THE LAW BITES, and is satisfiable. Both shapes are planted, because a law
// that names nothing and a law nobody can satisfy fail in the same way: somebody
// deletes them.
func TestTheOwedStampLawNamesAWriteOnThePathAndPassesTheOwedOne(t *testing.T) {
	for _, plant := range []struct {
		what     string
		source   string
		complain bool
	}{{
		what: "a stamp written straight onto the path, under the lock",
		source: `package session
func (a *Agent) refreshElsewhere() {
	a.mu.Lock()
	NoteTold(dir, now)
	a.mu.Unlock()
}`,
		complain: true,
	}, {
		what: "a stamp owed to another writer, whose next patch replaces it",
		source: `package session
func (a *Agent) refreshElsewhere() {
	a.mu.Lock()
	a.metaStamp().Owe(func() { NoteTold(dir, now) })
	a.mu.Unlock()
}`,
		complain: true,
	}, {
		what: "the stamp owed under the lock to its own writer and performed behind it",
		source: `package session
func (a *Agent) refreshElsewhere() {
	a.mu.Lock()
	a.toldStamp().Owe(func() { NoteTold(dir, now) })
	a.mu.Unlock()
}`,
	}} {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, "planted.go", plant.source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing the plant for %s: %v", plant.what, err)
		}
		calls, complaints := owedStampLaw(set, file)
		if calls != 1 {
			t.Fatalf("the law found %d stamp writes in %s, want one", calls, plant.what)
		}
		switch {
		case plant.complain && len(complaints) == 0:
			t.Errorf("the law said nothing about %s, which is the one shape it is for", plant.what)
		case !plant.complain && len(complaints) > 0:
			t.Errorf("the law complained about %s:\n%s", plant.what, strings.Join(complaints, "\n"))
		}
	}
}
