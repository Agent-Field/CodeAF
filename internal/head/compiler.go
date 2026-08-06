package head

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// compilerSystemPrompt applies assume-and-declare at the boundary between a
// user's durable intent and planning. Ambiguity becomes a visible, revisable
// receipt instead of a synchronous question that stalls the graph.
const compilerSystemPrompt = `You are the intent compiler for an asynchronous task graph. Apply ASSUME-AND-DECLARE.

Turn the user's verbatim instruction and the current graph context into a complete execution brief. Return exactly one JSON object with this shape and no text outside it:
{"goal":"...","deliverable":"...","budget":"...","assumptions":["..."]}

Rules:
- State a clear goal that names the final deliverable, what success means, and the evidence standard that will prove it.
- Name that same concrete final deliverable separately in deliverable.
- Give a sensible free-text budget, including a currency amount when cost is otherwise unspecified.
- Fill every missing decision with a practical default: scope, audience, format, quality bar, evidence, timing, tools, and constraints whenever the user did not settle them.
- List every default you supplied explicitly in assumptions. Assumptions are revisable receipts, not hidden guesses.
- Never ask a question back and never leave a placeholder such as TBD, unknown, or ask user.
- The user's words are the authority. Do not narrow or replace them with an inferred request.
- End goal with a line beginning "Verbatim request:" followed by the user's instruction exactly as supplied.

Be precise enough for downstream planning, but do not design the task graph yourself.`

// Brief is the complete, assumption-bearing intent handed to planning. Budget
// stays free text because the graph does not impose a money type on callers.
type Brief struct {
	Goal        string   `json:"goal"`
	Assumptions []string `json:"assumptions"`
	Deliverable string   `json:"deliverable"`
	Budget      string   `json:"budget"`
}

// Compiler converts verbatim user intent into a planning brief without asking
// the user to resolve unspecified details first.
type Compiler struct {
	client Client
}

// NewCompiler returns an intent compiler backed by client.
func NewCompiler(client Client) *Compiler {
	return &Compiler{client: client}
}

// Compile applies assume-and-declare once. The exact instruction is appended
// deterministically to Goal even if a provider ignores that prompt rule, so no
// downstream transformation can silently lose the user's words.
func (c *Compiler) Compile(ctx context.Context, instruction string, graphContext string) (Brief, error) {
	if c == nil || c.client == nil {
		return Brief{}, errors.New("compile intent: nil client")
	}
	user := "Current graph context:\n" + graphContext +
		"\n\nUser instruction (verbatim; preserve exactly):\n" + instruction
	response, err := c.client.CompleteWithMessages(ctx, []ai.Message{
		textMessage("system", compilerSystemPrompt),
		textMessage("user", user),
	}, ai.WithMaxTokens(1000))
	if err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	if response == nil {
		return Brief{}, errors.New("compile intent: provider returned a nil response")
	}

	var brief Brief
	if err := decodeJSONObject(response.Text(), &brief); err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	if err := validateBrief(brief); err != nil {
		return Brief{}, fmt.Errorf("compile intent: %w", err)
	}
	brief.Goal = anchorGoal(brief.Goal, instruction)
	return brief, nil
}

func validateBrief(brief Brief) error {
	if strings.TrimSpace(brief.Goal) == "" {
		return errors.New("empty goal")
	}
	if strings.TrimSpace(brief.Deliverable) == "" {
		return errors.New("empty deliverable")
	}
	if strings.TrimSpace(brief.Budget) == "" {
		return errors.New("empty budget")
	}
	if brief.Assumptions == nil {
		return errors.New("missing assumptions")
	}
	for _, assumption := range brief.Assumptions {
		if strings.TrimSpace(assumption) == "" {
			return errors.New("empty assumption")
		}
	}
	return nil
}

func anchorGoal(goal, instruction string) string {
	goal = strings.TrimSpace(goal)
	// A compiler that followed the prompt already carries the anchor inline;
	// appending a second copy would double the user's words in every receipt.
	if strings.Contains(goal, "Verbatim request:") && strings.Contains(goal, instruction) {
		return goal
	}
	return goal + "\n\nVerbatim request:\n" + instruction
}
