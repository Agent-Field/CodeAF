package calc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-calc.ts from
// the real src/session/overflow.ts + src/session/session.ts (and, for the two
// bundled normalisations §3.5 pins, verbatim copies of the dist sources). The
// gate is BYTE equality between jscompat.Stringify(goResult) and the
// JSON.stringify the TS run recorded.

type fixtureLine struct {
	Name      string            `json:"name"`
	Fn        string            `json:"fn"`
	Env       map[string]string `json:"env"`
	ModuleEnv map[string]string `json:"module_env"`
	ArgsJSON  string            `json:"args_json"`
	OutJSON   string            `json:"out_json"`
}

// num decodes a fixture number: a JSON number, or one of the string sentinels
// JSON cannot carry natively (see gen-calc.ts's encodeArgs).
type num float64

func (n *num) UnmarshalJSON(raw []byte) error {
	if len(raw) > 0 && raw[0] == '"' {
		var sentinel string
		if err := json.Unmarshal(raw, &sentinel); err != nil {
			return err
		}
		switch sentinel {
		case "NaN":
			*n = num(math.NaN())
		case "Infinity":
			*n = num(math.Inf(1))
		case "-Infinity":
			*n = num(math.Inf(-1))
		case "-0":
			*n = num(math.Copysign(0, -1))
		default:
			*n = num(jscompat.ToNumber(sentinel))
		}
		return nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*n = num(value)
	return nil
}

func f64(n *num) *float64 {
	if n == nil {
		return nil
	}
	value := float64(*n)
	return &value
}

// ── the JSON-transportable specs mirrored from tools/fixtures/gen-calc.ts ──

type limitSpec struct {
	Context num  `json:"context"`
	Input   *num `json:"input"`
	Output  num  `json:"output"`
}

type cacheCostSpec struct {
	Read  num `json:"read"`
	Write num `json:"write"`
}

type over200KSpec struct {
	Cache  *cacheCostSpec `json:"cache"`
	Input  num            `json:"input"`
	Output num            `json:"output"`
}

type costSpec struct {
	Input                num            `json:"input"`
	Output               num            `json:"output"`
	Cache                *cacheCostSpec `json:"cache"`
	ExperimentalOver200K *over200KSpec  `json:"experimentalOver200K"`
}

type modelSpec struct {
	Limit limitSpec `json:"limit"`
	Cost  *costSpec `json:"cost"`
}

type compactionSpec struct {
	Auto                 *bool `json:"auto"`
	Prune                *bool `json:"prune"`
	TailTurns            *num  `json:"tail_turns"`
	PreserveRecentTokens *num  `json:"preserve_recent_tokens"`
	Reserved             *num  `json:"reserved"`
}

type cfgSpec struct {
	Compaction *compactionSpec `json:"compaction"`
}

type tokensSpec struct {
	Total     *num `json:"total"`
	Input     num  `json:"input"`
	Output    num  `json:"output"`
	Reasoning num  `json:"reasoning"`
	Cache     struct {
		Read  num `json:"read"`
		Write num `json:"write"`
	} `json:"cache"`
}

type usageSpec struct {
	InputTokens       *num `json:"inputTokens"`
	InputTokenDetails *struct {
		NoCacheTokens    *num `json:"noCacheTokens"`
		CacheReadTokens  *num `json:"cacheReadTokens"`
		CacheWriteTokens *num `json:"cacheWriteTokens"`
	} `json:"inputTokenDetails"`
	OutputTokens       *num `json:"outputTokens"`
	OutputTokenDetails *struct {
		TextTokens      *num `json:"textTokens"`
		ReasoningTokens *num `json:"reasoningTokens"`
	} `json:"outputTokenDetails"`
	TotalTokens       *num `json:"totalTokens"`
	ReasoningTokens   *num `json:"reasoningTokens"`
	CachedInputTokens *num `json:"cachedInputTokens"`

	// The raw-OpenRouter shape (computeTokenUsage cases).
	PromptTokens        *num `json:"prompt_tokens"`
	CompletionTokens    *num `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens     *num `json:"cached_tokens"`
		CacheWriteTokens *num `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens *num `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// v3Spec is decoded separately because `inputTokens` is a number in
// LanguageModelUsage but an object in LanguageModelV3Usage.
type v3Spec struct {
	InputTokens struct {
		Total      *num `json:"total"`
		NoCache    *num `json:"noCache"`
		CacheRead  *num `json:"cacheRead"`
		CacheWrite *num `json:"cacheWrite"`
	} `json:"inputTokens"`
	OutputTokens struct {
		Total     *num `json:"total"`
		Text      *num `json:"text"`
		Reasoning *num `json:"reasoning"`
	} `json:"outputTokens"`
	Raw json.RawMessage `json:"raw"`
}

type argsSpec struct {
	Cfg      cfgSpec                   `json:"cfg"`
	Model    modelSpec                 `json:"model"`
	Tokens   tokensSpec                `json:"tokens"`
	Drift    *num                      `json:"drift"`
	Agent    *string                   `json:"agent"`
	Usage    json.RawMessage           `json:"usage"`
	Metadata map[string]map[string]any `json:"metadata"`
	Rates    *costSpec                 `json:"rates"`
	Value    *string                   `json:"value"`
}

// ── spec → port types ─────────────────────────────────────────────────────

func buildCfg(spec cfgSpec) Config {
	if spec.Compaction == nil {
		return Config{}
	}
	return Config{Compaction: &CompactionConfig{
		Auto:                 spec.Compaction.Auto,
		Prune:                spec.Compaction.Prune,
		TailTurns:            f64(spec.Compaction.TailTurns),
		PreserveRecentTokens: f64(spec.Compaction.PreserveRecentTokens),
		Reserved:             f64(spec.Compaction.Reserved),
	}}
}

func buildCache(spec *cacheCostSpec) *CacheCost {
	if spec == nil {
		return nil
	}
	return &CacheCost{Read: float64(spec.Read), Write: float64(spec.Write)}
}

func buildModel(spec modelSpec) Model {
	model := Model{Limit: ModelLimit{
		Context: float64(spec.Limit.Context),
		Input:   f64(spec.Limit.Input),
		Output:  float64(spec.Limit.Output),
	}}
	if spec.Cost != nil {
		model.Cost = buildCost(spec.Cost)
	}
	return model
}

func buildCost(spec *costSpec) *ModelCost {
	cost := &ModelCost{
		Input:  float64(spec.Input),
		Output: float64(spec.Output),
		Cache:  buildCache(spec.Cache),
	}
	if spec.ExperimentalOver200K != nil {
		cost.ExperimentalOver200K = &Over200KCost{
			Cache:  buildCache(spec.ExperimentalOver200K.Cache),
			Input:  float64(spec.ExperimentalOver200K.Input),
			Output: float64(spec.ExperimentalOver200K.Output),
		}
	}
	return cost
}

func buildTokens(spec tokensSpec) Tokens {
	return Tokens{
		Total:     f64(spec.Total),
		Input:     float64(spec.Input),
		Output:    float64(spec.Output),
		Reasoning: float64(spec.Reasoning),
		Cache:     TokenCache{Read: float64(spec.Cache.Read), Write: float64(spec.Cache.Write)},
	}
}

func buildUsageTokens(spec tokensSpec) UsageTokens {
	return UsageTokens{
		Total:     f64(spec.Total),
		Input:     float64(spec.Input),
		Output:    float64(spec.Output),
		Reasoning: float64(spec.Reasoning),
		Cache:     UsageCache{Write: float64(spec.Cache.Write), Read: float64(spec.Cache.Read)},
	}
}

func buildLanguageModelUsage(t *testing.T, raw json.RawMessage) LanguageModelUsage {
	t.Helper()
	var spec usageSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	usage := LanguageModelUsage{
		InputTokens:       f64(spec.InputTokens),
		OutputTokens:      f64(spec.OutputTokens),
		TotalTokens:       f64(spec.TotalTokens),
		ReasoningTokens:   f64(spec.ReasoningTokens),
		CachedInputTokens: f64(spec.CachedInputTokens),
	}
	if spec.InputTokenDetails != nil {
		usage.InputTokenDetails = &InputTokenDetails{
			NoCacheTokens:    f64(spec.InputTokenDetails.NoCacheTokens),
			CacheReadTokens:  f64(spec.InputTokenDetails.CacheReadTokens),
			CacheWriteTokens: f64(spec.InputTokenDetails.CacheWriteTokens),
		}
	}
	if spec.OutputTokenDetails != nil {
		usage.OutputTokenDetails = &OutputTokenDetails{
			TextTokens:      f64(spec.OutputTokenDetails.TextTokens),
			ReasoningTokens: f64(spec.OutputTokenDetails.ReasoningTokens),
		}
	}
	return usage
}

func buildOpenRouterUsage(t *testing.T, raw json.RawMessage) *OpenRouterUsage {
	t.Helper()
	var spec usageSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode raw usage: %v", err)
	}
	usage := &OpenRouterUsage{
		PromptTokens:     f64(spec.PromptTokens),
		CompletionTokens: f64(spec.CompletionTokens),
	}
	if spec.PromptTokensDetails != nil {
		usage.PromptTokensDetails = &OpenRouterPromptTokensDetails{
			CachedTokens:     f64(spec.PromptTokensDetails.CachedTokens),
			CacheWriteTokens: f64(spec.PromptTokensDetails.CacheWriteTokens),
		}
	}
	if spec.CompletionTokensDetails != nil {
		usage.CompletionTokensDetails = &OpenRouterCompletionTokensDetails{
			ReasoningTokens: f64(spec.CompletionTokensDetails.ReasoningTokens),
		}
	}
	// The opaque wire object carries through verbatim; args_json holds the exact
	// bytes JSON.stringify produced for it.
	usage.Raw = append(json.RawMessage(nil), raw...)
	return usage
}

func buildV3Usage(t *testing.T, raw json.RawMessage) LanguageModelV3Usage {
	t.Helper()
	var spec v3Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode v3 usage: %v", err)
	}
	return LanguageModelV3Usage{
		InputTokens: LanguageModelV3InputTokens{
			Total:      f64(spec.InputTokens.Total),
			NoCache:    f64(spec.InputTokens.NoCache),
			CacheRead:  f64(spec.InputTokens.CacheRead),
			CacheWrite: f64(spec.InputTokens.CacheWrite),
		},
		OutputTokens: LanguageModelV3OutputTokens{
			Total:     f64(spec.OutputTokens.Total),
			Text:      f64(spec.OutputTokens.Text),
			Reasoning: f64(spec.OutputTokens.Reasoning),
		},
		Raw: spec.Raw,
	}
}

func TestFixtureParity(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	cases := 0
	byFn := map[string]int{}
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		cases++
		byFn[fixture.Fn]++
		t.Run(fixture.Name, func(t *testing.T) {
			restoreEnv := SetEnvForTesting(fixture.Env)
			defer restoreEnv()
			restoreModule := SetModuleEnvForTesting(fixture.ModuleEnv)
			defer restoreModule()

			var spec argsSpec
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &spec); err != nil {
				t.Fatalf("decode args_json: %v", err)
			}

			var result any
			switch fixture.Fn {
			case "usable":
				result = jscompat.JSNumber(Usable(UsableInput{Cfg: buildCfg(spec.Cfg), Model: buildModel(spec.Model)}))
			case "shouldScanDrift":
				result = ShouldScanDrift(ScanDriftInput{
					Cfg:    buildCfg(spec.Cfg),
					Tokens: buildTokens(spec.Tokens),
					Model:  buildModel(spec.Model),
				})
			case "isOverflow":
				result = IsOverflow(OverflowInput{
					Cfg:    buildCfg(spec.Cfg),
					Tokens: buildTokens(spec.Tokens),
					Model:  buildModel(spec.Model),
					Drift:  f64(spec.Drift),
					Agent:  spec.Agent,
				})
			case "getUsage":
				result = GetUsage(GetUsageInput{
					Model:    buildModel(spec.Model),
					Usage:    buildLanguageModelUsage(t, spec.Usage),
					Metadata: spec.Metadata,
				})
			case "computeTokenUsage":
				result = ComputeTokenUsage(buildOpenRouterUsage(t, spec.Usage))
			case "asLanguageModelUsage":
				result = AsLanguageModelUsage(buildV3Usage(t, spec.Usage))
			case "costDecimalString":
				rates := costRates{}
				if spec.Rates != nil {
					if spec.Rates.ExperimentalOver200K != nil {
						t.Fatalf("costDecimalString rates never carry experimentalOver200K")
					}
					rates = baseRates(buildCost(spec.Rates))
				}
				result = costDecimal(buildUsageTokens(spec.Tokens), rates).decString()
			case "decString":
				if spec.Value == nil {
					t.Fatalf("decString case without a value")
				}
				result = decParse(*spec.Value).decString()
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}

			encoded, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify go result: %v", err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Errorf("byte parity failure\n  fn:   %s\n  env:  %v\n  mod:  %v\n  args: %s\n  want: %s\n  got:  %s",
					fixture.Fn, fixture.Env, fixture.ModuleEnv, fixture.ArgsJSON, fixture.OutJSON, encoded)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	// Every ported function must be represented, and the file must not have
	// silently shrunk.
	for _, fn := range []string{
		"usable", "shouldScanDrift", "isOverflow",
		"getUsage", "computeTokenUsage", "asLanguageModelUsage", "costDecimalString", "decString",
	} {
		if byFn[fn] == 0 {
			t.Fatalf("no fixture cases for %q", fn)
		}
	}
	if cases < 3000 {
		t.Fatalf("expected at least 3000 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases %v", cases, byFn)
}
