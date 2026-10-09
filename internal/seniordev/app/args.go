//go:build !windows

package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
)

// DefaultHighModels is the pool the coder routes on when the command line
// names none: `--high` on `codeaf senior-dev run`. Each entry is a model on the
// service codeaf's model API speaks for, and senior-dev's own router picks
// among them call by call (internal/seniordev/router/adaptive); codeaf's funnel
// then serves the call the router picked.
const DefaultHighModels = "openrouter/deepseek/deepseek-v4-flash-0731,openrouter/deepseek/deepseek-v4-pro,openrouter/qwen/qwen3.6-plus,openrouter/moonshotai/kimi-k2.6,openrouter/z-ai/glm-5.1,openrouter/minimax/minimax-m2.7"

// DefaultVariant is the reasoning effort every coder call asks for when the
// command line names none: `--variant` on `codeaf senior-dev run`. senior-dev
// is handed the long, many-sided work, and a coder left to each model's own
// default thought as little as the model's makers chose for a chat. It is a
// rung of codeaf's one ladder (internal/effort), and the summaries a long run
// writes of its own history send none ([turnCall.Summary]).
const DefaultVariant = string(effort.High)

// ParseVariant reads a `--variant` word on codeaf's one ladder
// (internal/effort): a rung is sent as it is spelled, and `none`, `auto`,
// `off` or nothing send no `reasoning` at all, so the model thinks however it
// thinks. A word that is neither is refused rather than sent for a provider
// to reject call after call.
func ParseVariant(word string) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(word), "none") {
		return "", true
	}
	rung, ok := effort.Parse(word)
	return rung.String(), ok
}

// VariantWords is every word [ParseVariant] takes, for the sentence that
// refuses one it does not.
func VariantWords() string {
	words := make([]string, 0, len(effort.Rungs)+1)
	for _, rung := range effort.Rungs {
		words = append(words, rung.String())
	}
	return strings.Join(append(words, "none"), ", ")
}

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
	// InPlace forces the snapshot recorder: senior-dev edits the workspace
	// without writing to any repository around it. Without it, the snapshot
	// recorder is still chosen wherever there is no git history to use.
	InPlace bool
	// VerifyBuild and VerifyTest are the build and test the person named as
	// the project's own check, each in place of discovery for its kind; empty
	// leaves that kind to discovery (fullverification/declared.go).
	VerifyBuild string
	VerifyTest  string
	// StateDir is where the session store lives, resolved through its links;
	// empty keeps it in the workspace's own .senior-dev ([stateDirectory]).
	StateDir string
	MaxCost  *float64
	MaxHours *float64
}

// CrewModel is a crew seat's model as a pool entry: the id filed under the
// service codeaf's model API speaks for, which is how every pool entry is
// spelled ([DefaultHighModels]). An id already filed there is left alone.
func CrewModel(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.HasPrefix(id, orclient.Service+"/") {
		return id
	}
	return orclient.Service + "/" + id
}

// crewPools keeps, of pools a conversation's crew filled, only the models the
// catalog can size — a call on one it cannot is a call senior-dev refuses to
// make — and says which it dropped. A --high with nothing left routes on
// [DefaultHighModels], because a crew of models this catalog does not know is
// no reason to stop a run codeaf already started; an empty --low or
// --frontier falls back to --high, as it always does.
//
// IT IS ONLY FOR A CREW. A person who types --high at a shell meant those
// models, and is told plainly when one cannot be served; a crew was chosen for
// the conversation, and a program that cannot use one of its seats uses its
// own list rather than failing an hour of work.
func crewPools(args cliArgs, known func(string) bool, notes io.Writer) cliArgs {
	keep := func(raw string) string {
		var kept []string
		for _, ref := range splitPool(raw) {
			if known(ref) {
				kept = append(kept, ref)
				continue
			}
			_, _ = fmt.Fprintf(notes, "[senior-dev] the crew's %s is not in the model catalog; it is left out of this run\n", ref)
		}
		return strings.Join(kept, ",")
	}
	args.High, args.Low, args.Frontier = keep(args.High), keep(args.Low), keep(args.Frontier)
	if args.High == "" {
		_, _ = fmt.Fprintf(notes, "[senior-dev] none of the crew's models can be sized; routing on senior-dev's own list\n")
		args.High = DefaultHighModels
	}
	return args
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
