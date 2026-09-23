//go:build !windows

package app

import "strings"

// DefaultHighModels is the pool the coder routes on when the command line
// names none: `--high` on `codeaf senior-dev run`. Each entry is a model on the
// service codeaf's model API speaks for, and senior-dev's own router picks
// among them call by call (internal/seniordev/router/adaptive); codeaf's funnel
// then serves the call the router picked.
const DefaultHighModels = "openrouter/deepseek/deepseek-v4-flash-0731,openrouter/deepseek/deepseek-v4-pro,openrouter/qwen/qwen3.6-plus,openrouter/moonshotai/kimi-k2.6,openrouter/z-ai/glm-5.1,openrouter/minimax/minimax-m2.7"

// cliArgs is what one run was asked to do, as the command line said it: the
// run command's own flags (internal/seniordev) plus the ceilings codeaf hands
// every program it carries. senior-dev's own parser, its `--format`, `--tui`
// and help were codeaf's to replace, and are gone; this is what the run itself
// reads.
type cliArgs struct {
	High     string
	Low      string
	Frontier string
	// Variant is sent as `reasoning.effort`. Empty sends no `reasoning` key,
	// so the service's own default applies.
	Variant string
	// InPlace selects the snapshot recorder: senior-dev edits the workspace
	// without requiring a repository and without writing to one.
	InPlace  bool
	MaxCost  *float64
	MaxHours *float64
}

func splitPool(raw string) []string {
	out := []string{}
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
