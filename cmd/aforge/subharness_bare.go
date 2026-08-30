package main

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	barepkg "github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// bareMenuEntry is the compiler-menu paragraph for the bare worker. It states
// what the worker is for in the same voice sweMenuEntry does, so a model
// choosing among workers reads one coherent menu.
const bareMenuEntry = `bare — one agent, one sitting, minimal loop — the cheapest whole-taker for small work. Give it a self-contained task that fits in a single session: a small fix, a quick investigation, a short report. It runs the same four tools as the default worker with a lighter harness, so it finishes faster and costs less on work that does not need the full contract. Do not choose it for work that needs verification against a repository's own tests, for work that spans multiple sessions, or for work whose deliverable is a code change large enough to need a pipeline.`

// bareInfo is the bare worker's registration. Its budget shape is linear's,
// SAID BY SAYING NOTHING: a registration that leaves the three deadline fields
// zero is served the generalist's shape, which is the contract stated on
// exec.SubharnessInfo. Bare is the same kind of worker — one agent taking one
// small step at a time — with a lighter prompt, and a lighter prompt does not
// buy more time; it buys fewer tokens per turn, which the per-token scaling
// already accounts for. Repeating the numbers here made bare a second author of
// linear's floor, free to drift from it the next time linear moved.
func bareInfo() exec.SubharnessInfo {
	return exec.SubharnessInfo{
		Name:    barepkg.BareSubharness,
		Purpose: strings.TrimPrefix(bareMenuEntry, barepkg.BareSubharness+" — "),
	}
}
