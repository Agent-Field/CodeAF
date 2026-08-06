// Package config holds the harness's process-wide defaults. It is the only
// place a model slug or an endpoint is written down, so changing the default
// model is a one-line edit rather than a search across the tree.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

const (
	// DefaultModel is the harness's default. DeepSeek V4 Flash Latest is the
	// floating alias, so a new dated snapshot is picked up without a code change.
	// 1M context, ~$0.09/M in and ~$0.18/M out, and it advertises both
	// structured_outputs and reasoning on OpenRouter.
	DefaultModel = "~deepseek/deepseek-v4-flash-latest"

	// DefaultBaseURL is OpenRouter's OpenAI-compatible endpoint.
	DefaultBaseURL = "https://openrouter.ai/api/v1"

	// DefaultSiteURL and DefaultSiteName are the OpenRouter app-attribution
	// values (HTTP-Referer and X-Title). They mirror the agentfield SDK's
	// attribution defaults so every AgentField product reports usage under
	// the same app on the OpenRouter dashboard.
	DefaultSiteURL  = "https://agentfield.ai"
	DefaultSiteName = "AgentField AI"

	DefaultTemperature = 0.2

	// DefaultMaxTokens has to cover reasoning tokens, not just the visible
	// answer. This model routinely spends more of its budget thinking than
	// writing, and a cap that only fits the answer produces an empty reply
	// rather than a short one. 16k was still not enough once the executor ran
	// with reasoning restored: a hard turn thinks past it, gets truncated, and
	// returns an empty message with no tool calls. The cap is a ceiling, not a
	// spend — room costs nothing on the turns that do not use it.
	DefaultMaxTokens = 32768

	// DefaultTimeout is generous because a reasoning pass can run for minutes on
	// a wide task even when the answer is short.
	DefaultTimeout = 300 * time.Second

	// DefaultReasoning is off — for planning calls only. Planning is structuring
	// work, not thinking work: with reasoning on, an outline that is 150 tokens
	// of answer costs 2,400 tokens of deliberation and twenty seconds of wall
	// clock, for an outline of the same shape. Set AFORGE_REASONING=low|medium|
	// high to buy it back for a plan that genuinely needs it.
	DefaultReasoning = provider.EffortOff

	// DefaultExecReasoning is off, and unlike the planning default this one was
	// settled by a controlled experiment rather than a latency argument. The
	// same real coding task, same model, same limits: reflex mode finished in
	// 6 minutes with the suite green both with and without a contract, while
	// reasoning mode was 3x slower, 4x dearer, and finished worse or not at
	// all. An earlier run had blamed reasoning-off for a 139-turn zero-file
	// disaster; the ablation proved the harness was at fault — memory decay
	// was erasing the agent's only state, and once observations survive, the
	// loop does not need a thinking pass to converge. Set
	// AFORGE_EXEC_REASONING=low|medium|high to buy deliberation back for a
	// task class that turns out to need it.
	DefaultExecReasoning = provider.EffortOff

	// DefaultSpineSamples draws the spine more than once. It is the only call
	// whose framing every later pass inherits, so an unlucky draw does not
	// degrade the graph slightly — it replaces it. The samples run at the same
	// time, so this costs no wall clock and a fraction of a cent.
	DefaultSpineSamples = 3

	// DefaultMaxDepth bounds recursion. Depth is the only cost of decomposition
	// that is genuinely serial — a whole level expands in four call-rounds
	// however wide it is — so this is the guard that actually protects latency.
	DefaultMaxDepth = 2

	// DefaultNodeBudget is the ceiling the model cannot argue with. Every other
	// stop condition is pressure applied through a prompt; this one is
	// arithmetic, and it is what guarantees the recursion terminates.
	DefaultNodeBudget = 60
)

// Config is the resolved runtime configuration.
type Config struct {
	APIKey        string
	BaseURL       string
	Model         string
	Temperature   float64
	MaxTokens     int
	Timeout       time.Duration
	SiteURL       string
	SiteName      string
	Reasoning     provider.Effort
	ExecReasoning provider.Effort
	SpineSamples  int
	MaxDepth      int
	NodeBudget    int

	// Panel is the set of models a run may route across, from AFORGE_MODELS. An
	// empty panel is the default and is the kill switch: with no panel the
	// harness builds the same single adapter it always did and no routing code
	// runs at all.
	Panel router.Panel

	// ProfileDir holds measured executor behaviour. Empty means ~/.aforge.
	ProfileDir string
}

// Load resolves configuration from the environment, falling back to the
// defaults above. Only the API key has no default; everything else runs
// unconfigured.
func Load() (Config, error) {
	config := Config{
		APIKey:        firstNonEmpty(os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENAI_API_KEY")),
		BaseURL:       firstNonEmpty(os.Getenv("AFORGE_BASE_URL"), DefaultBaseURL),
		Model:         firstNonEmpty(os.Getenv("AFORGE_MODEL"), DefaultModel),
		Temperature:   DefaultTemperature,
		MaxTokens:     DefaultMaxTokens,
		Timeout:       DefaultTimeout,
		SiteURL:       firstNonEmpty(os.Getenv("AFORGE_SITE_URL"), os.Getenv("AGENTFIELD_OPENROUTER_SITE_URL"), os.Getenv("OR_SITE_URL"), DefaultSiteURL),
		SiteName:      firstNonEmpty(os.Getenv("AFORGE_SITE_NAME"), os.Getenv("AGENTFIELD_OPENROUTER_APP_NAME"), os.Getenv("OR_APP_NAME"), DefaultSiteName),
		Reasoning:     DefaultReasoning,
		ExecReasoning: DefaultExecReasoning,
		SpineSamples:  DefaultSpineSamples,
		MaxDepth:      DefaultMaxDepth,
		NodeBudget:    DefaultNodeBudget,
		ProfileDir:    os.Getenv("AFORGE_PROFILE_DIR"),
	}
	if config.APIKey == "" {
		return Config{}, errors.New("OPENROUTER_API_KEY (or OPENAI_API_KEY) is required")
	}
	if raw := strings.TrimSpace(os.Getenv("AFORGE_REASONING")); raw != "" {
		effort, ok := provider.ParseEffort(raw)
		if !ok {
			return Config{}, fmt.Errorf("AFORGE_REASONING: unknown effort %q (off, low, medium, high)", raw)
		}
		config.Reasoning = effort
	}
	if raw := strings.TrimSpace(os.Getenv("AFORGE_EXEC_REASONING")); raw != "" {
		effort, ok := provider.ParseEffort(raw)
		if !ok {
			return Config{}, fmt.Errorf("AFORGE_EXEC_REASONING: unknown effort %q (off, low, medium, high)", raw)
		}
		config.ExecReasoning = effort
	}
	for _, knob := range []struct {
		name   string
		target *int
	}{
		{"AFORGE_SPINE_SAMPLES", &config.SpineSamples},
		{"AFORGE_MAX_DEPTH", &config.MaxDepth},
		{"AFORGE_NODE_BUDGET", &config.NodeBudget},
	} {
		raw := strings.TrimSpace(os.Getenv(knob.name))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return Config{}, fmt.Errorf("%s: want a non-negative integer, got %q", knob.name, raw)
		}
		*knob.target = value
	}
	panel, err := router.LoadPanel(os.Getenv("AFORGE_MODELS"))
	if err != nil {
		return Config{}, err
	}
	config.Panel = panel
	return config, nil
}

// Context stamps the run's provider knobs onto ctx: the effort the operator
// chose, and a cache key derived from the task so every call in one run asks
// for the same warm instance.
func (c Config) Context(ctx context.Context, task string) context.Context {
	ctx = provider.WithCacheKey(ctx, provider.RunCacheKey(task, c.Model))
	// Configured rather than inferred: the operator asked for this level, so it
	// travels even to a model the catalog cannot vouch for.
	return provider.WithConfiguredReasoningEffort(ctx, c.Reasoning)
}

// ExecContext layers the executor's reasoning level over a planning context.
// Planning and execution are different kinds of call — one structures, the
// other works — so the economy that makes planning fast must not travel into
// the loop, where a model with reasoning suppressed stops writing anything
// down and never converges.
func (c Config) ExecContext(ctx context.Context) context.Context {
	return provider.WithConfiguredReasoningEffort(ctx, c.ExecReasoning)
}

// Client builds what the planner and the executor call.
//
// With no panel configured this is the single adapter it has always been, built
// exactly as before — that is the kill switch, and it is the default. With a
// panel it is a router over one adapter per model, which satisfies the same
// interface, so nothing above this line changes.
func (c Config) Client() (router.Client, error) {
	if len(c.Panel.Models) == 0 {
		return provider.NewClient(c.providerConfig(c.Model))
	}
	return router.New(c.Panel, c.providerConfig(c.Model), c.ProfileDir)
}

// ClientFor builds a client for an explicitly chosen model. With no panel it is
// exactly the single-model adapter it always was. With a panel the choice is
// pinned as the router's opener, so chat keeps its runtime picker without
// bypassing observation and escalation.
func (c Config) ClientFor(model string) (router.Client, error) {
	if len(c.Panel.Models) > 0 {
		return router.NewPinned(c.Panel, c.providerConfig(model), c.ProfileDir, model)
	}
	return provider.NewClient(c.providerConfig(model))
}

func (c Config) providerConfig(model string) provider.Config {
	return provider.Config{
		APIKey:      c.APIKey,
		BaseURL:     c.BaseURL,
		Model:       model,
		Temperature: c.Temperature,
		MaxTokens:   c.MaxTokens,
		Timeout:     c.Timeout,
		SiteURL:     c.SiteURL,
		SiteName:    c.SiteName,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
