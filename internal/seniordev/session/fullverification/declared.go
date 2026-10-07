//go:build !windows

package fullverification

import "strings"

// THE PERSON WHO STARTED THE RUN MAY NAME THE PROJECT'S OWN CHECK.
//
// Discovery reads the workspace, and some projects keep the command that
// proves them somewhere it never looks: a fuzz target whose harness lives
// beside the checkout rather than in it, a benchmark rig with a validation
// script of its own, a build driven by a wrapper the README does not name. The
// floor demands a build and a test from every accountable project, so such a
// project failed verification for want of a command, with the command that
// would have proved it sitting one directory away.
//
// So `codeaf senior-dev run` takes `--verify-build` and `--verify-test`, and a
// command named there replaces discovery for its kind. It is still run by
// senior-dev itself, on the frozen tree, under the same strict preamble and
// ceiling as anything discovered; only WHERE the command came from changes.
//
// IT IS THE PERSON'S WORD, NEVER THE MODEL'S. The flags are read from the
// command line that started the run, which the model working the brief cannot
// write to; a check the working model could choose for itself is a check it
// could choose to pass. A declared kind is demanded like a discovered one, so
// naming a test never quietly excuses a missing build.

// Declared is the build and test commands a person named on the command line.
// An empty field leaves that kind to discovery.
type Declared struct {
	Build string
	Test  string
}

// The sources a declared entrypoint carries, spelled as the flags a person
// typed, so every line that reports the command says where it came from.
const (
	DeclaredBuildSource = "--verify-build"
	DeclaredTestSource  = "--verify-test"
)

// Declare returns the plan with each command the person named in place of
// whatever discovery chose for that kind. A declared kind is expected, so the
// floor holds the run to it exactly as it would a discovered one.
func (plan Plan) Declare(declared Declared) Plan {
	build := strings.TrimSpace(declared.Build)
	test := strings.TrimSpace(declared.Test)
	if build == "" && test == "" {
		return plan
	}
	chosen := map[EntrypointKind]Entrypoint{}
	for _, entrypoint := range plan.Entrypoints {
		chosen[entrypoint.Kind] = entrypoint
	}
	if build != "" {
		chosen[KindBuild] = Entrypoint{Kind: KindBuild, Command: build, Source: DeclaredBuildSource}
		plan.BuildExpected = true
	}
	if test != "" {
		chosen[KindTest] = Entrypoint{Kind: KindTest, Command: test, Source: DeclaredTestSource}
		plan.TestExpected = true
	}
	plan.Entrypoints = []Entrypoint{}
	for _, kind := range []EntrypointKind{KindBuild, KindTest} {
		if entrypoint, ok := chosen[kind]; ok {
			plan.Entrypoints = append(plan.Entrypoints, entrypoint)
		}
	}
	return plan
}

// IsDeclared reports whether an entrypoint came from the command line rather
// than from discovery.
func (entrypoint Entrypoint) IsDeclared() bool {
	return entrypoint.Source == DeclaredBuildSource || entrypoint.Source == DeclaredTestSource
}
