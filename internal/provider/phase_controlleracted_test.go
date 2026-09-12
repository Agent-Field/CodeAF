package provider

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── THE CONTROLLER-ACTED LIST, PINNED WHERE IT LIVES ────────────────────────
//
// #970: the hedge fixture closed its first-word signal on a list it kept by
// hand — `case PhaseAllSlow, PhaseBelowPace, PhaseSwitching,
// PhaseSwitchingModel:` — and that list lived nowhere else. It had already had
// to grow twice inside #952, and the failure a missing rung produces names
// nothing: a lane holds its first word until the test's deadline. The list now
// lives on the phase type itself, as [PhaseNews.ControllerActed] beside
// [PhaseNews.Waiting], and these two laws are what keep that true — that every
// phase the vocabulary declares has an answer to both questions, and that the
// clause is spelled in exactly one place in the tree.

// declaredPhases is every phase constant the vocabulary declares, read out of
// phase.go's own const block: the constant's name, and the word it carries.
//
// THE CONST BLOCK IS THE ONLY LIST, which is the whole point of #970. A table
// of phases written out by hand in a test is the hand-kept list again, one file
// further away, and it would go stale in exactly the same silence.
func declaredPhases(t *testing.T) map[string]Phase {
	t.Helper()
	_, phaseFile := waitingFile(t, "internal/provider/phase.go")
	declared := map[string]Phase{}
	for _, decl := range phaseFile.Decls {
		block, ok := decl.(*ast.GenDecl)
		if !ok || block.Tok != token.CONST {
			continue
		}
		for _, spec := range block.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// ONLY THE CONSTANTS THAT ARE PHASES: the same block would
			// otherwise hand back [PhaseWindow], which is a duration.
			named, ok := value.Type.(*ast.Ident)
			if !ok || named.Name != "Phase" {
				continue
			}
			for at, name := range value.Names {
				literal, ok := value.Values[at].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s is declared a Phase without a word of its own", name.Name)
				}
				declared[name.Name] = Phase(strings.Trim(literal.Value, `"`))
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("phase.go declares no phases at all — this law has nothing to hold")
	}
	return declared
}

// TestEveryPhaseAnswersBothQuestions is the membership of both predicates,
// stated once and checked against the constants themselves.
//
// A PHASE ADDED WITHOUT A THOUGHT FOR EITHER QUESTION FAILS HERE BY NAME. The
// table below is compared against phase.go's const block in both directions, so
// a rung written without a row fails beside the rung that was just written —
// which is exactly the failure #970 was filed about, this time loud.
//
// The two questions are different on purpose, and this table is where their
// difference is stated as fact. Waiting is what a person sits through; acted is
// what this build did about it. `first word` is a wait nobody has acted on yet,
// `trying again` is the relax ladder and not the router, and `asking` is the
// controller deciding NOT to act until a person answers — so all three wait and
// none of them has spoken, while `below pace` has spoken and is not a wait at
// all.
func TestEveryPhaseAnswersBothQuestions(t *testing.T) {
	answers := map[string]struct{ waiting, acted bool }{
		"PhaseConnecting":     {waiting: true},
		"PhaseConnectionLost": {waiting: true},
		"PhaseFirstWord":      {waiting: true},
		"PhaseThinking":       {},
		"PhaseWriting":        {},
		"PhasePaced":          {waiting: true},
		"PhasePlanPaused":     {waiting: true},
		"PhaseRetrying":       {waiting: true},
		"PhaseSwitching":      {waiting: true, acted: true},
		"PhaseAsking":         {waiting: true},
		"PhaseAllSlow":        {waiting: true, acted: true},
		"PhaseBelowPace":      {acted: true},
		"PhaseSwitchingModel": {waiting: true, acted: true},
		"PhaseRunning":        {},
		"PhaseChecking":       {},
		"PhaseTidying":        {},
		"PhaseBriefing":       {},
		"PhasePreparing":      {},
		"PhaseTakingStock":    {},
	}
	declared := declaredPhases(t)
	for name, phase := range declared {
		want, stated := answers[name]
		if !stated {
			t.Errorf("%s is declared in phase.go and answers neither question here — say whether a person waits through it and whether the controller has spoken by producing it", name)
			continue
		}
		news := PhaseNews{Phase: phase}
		if got := news.Waiting(); got != want.waiting {
			t.Errorf("%s: PhaseNews{Phase: %q}.Waiting() = %v, want %v", name, phase, got, want.waiting)
		}
		if got := news.ControllerActed(); got != want.acted {
			t.Errorf("%s: PhaseNews{Phase: %q}.ControllerActed() = %v, want %v", name, phase, got, want.acted)
		}
	}
	for name := range answers {
		if _, ok := declared[name]; !ok {
			t.Errorf("%s has an answer here and is no longer a phase — a renamed rung leaves this row holding nothing", name)
		}
	}
}

// TestTheControllerActedListLivesNowhereElse walks the checkout for a second
// copy of the case clause the hedge fixture used to keep by hand.
//
// ZERO MATCHES OUTSIDE phase.go IS THE ISSUE CLOSED. The walk reads only Go
// source — the change entry for #970 quotes the same clause as prose, and it is
// right to — and it exempts this file, which has to name the clause to look for
// it. A hit anywhere else is somebody keeping the list by hand again, and the
// failure names the file so the copy is deleted rather than tolerated.
func TestTheControllerActedListLivesNowhereElse(t *testing.T) {
	const clause = "case PhaseAllSlow, PhaseBelowPace, PhaseSwitching, PhaseSwitchingModel:"
	root := funnelRepoRoot(t)
	var copies []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// THE ONE FILE THAT MUST NAME THE CLAUSE IS THIS ONE, and phase.go is
		// where the list legitimately lives, so the law is about everywhere else.
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "internal/provider/phase.go" || rel == "internal/provider/phase_controlleracted_test.go" {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(source), clause) {
			copies = append(copies, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the checkout: %v", err)
	}
	if len(copies) != 0 {
		t.Errorf("the controller-acted case clause is spelled by hand in %s — THE LIST LIVES IN phase.go AND NOWHERE ELSE; read [PhaseNews.ControllerActed] instead of repeating it",
			strings.Join(copies, ", "))
	}
}
