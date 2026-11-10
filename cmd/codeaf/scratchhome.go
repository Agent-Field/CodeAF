package main

import (
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// scratchRootFor names where the harness keeps its own files for a workspace.
//
// CODEAF_SCRATCH, when set, is the directory itself, verbatim — the escape
// hatch that lets CI point the recorders, `.obs/` spills and patches at its
// own temp dir rather than codeaf's. It is read through env.Value, the dynamic
// door, because the name is written verbatim here rather than declared as a
// symbol this package owns.
//
// When it is not set the answer is a per-workspace directory under codeaf's
// state root: one home per workspace NAME, so a resumed run appends to the
// recorders it already wrote, and never a sibling of the workspace itself —
// `-w` may name a person's repository, and writing next to their project would
// move the mess rather than remove it (see run.go, which used to spell this
// inline).
func scratchRootFor(root string) string {
	if dir := env.Value("CODEAF_SCRATCH"); dir != "" {
		return dir
	}
	return home.Join("scratch", filepath.Base(root))
}
