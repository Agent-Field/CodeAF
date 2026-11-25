// Package evals runs platform-authored app acceptance cases against an isolated engine.
package evals

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

//go:embed cases/*.yaml
var fixtures embed.FS

type Case struct {
	Name      string  `yaml:"name"`
	Prompt    string  `yaml:"prompt"`
	MaxRounds int     `yaml:"max_rounds"`
	NoModel   bool    `yaml:"no_model"`
	Plan      []Check `yaml:"plan"`
	Steps     []Step  `yaml:"steps"`
	Looks     []Look  `yaml:"looks"`
	Record    Record  `yaml:"record"`
}

type Record struct {
	Type  string         `yaml:"type"`
	Value map[string]any `yaml:"value"`
	Edit  map[string]any `yaml:"edit"`
}

// Steps name the public operation and capture only facts returned by the engine.
type Step struct {
	Method  string            `yaml:"method"`
	Params  map[string]any    `yaml:"params"`
	Expect  []Check           `yaml:"expect"`
	Capture map[string]string `yaml:"capture"`
	Error   string            `yaml:"error"`
}

type Check struct {
	Path   string `yaml:"path"`
	Exists *bool  `yaml:"exists"`
	Equals any    `yaml:"equals"`
	Length *int   `yaml:"length"`
}

type Usage struct {
	Calls     int      `json:"calls"`
	TokensIn  int64    `json:"tokens_in"`
	TokensOut int64    `json:"tokens_out"`
	Cost      *float64 `json:"cost_usd"`
	Unpriced  int      `json:"unpriced_records"`
}

type BuildState struct {
	App        string
	Plan       json.RawMessage
	Rounds     int
	Checked    bool
	RecipeGaps []string
}

type Result struct {
	Case        string         `json:"case"`
	Pass        bool           `json:"pass"`
	Rounds      int            `json:"rounds"`
	Usage       *Usage         `json:"usage"`
	RecipeGaps  []string       `json:"recipe_gaps,omitempty"`
	Failures    []string       `json:"failures,omitempty"`
	Adversarial []ProbeResult  `json:"adversarial,omitempty"`
	Design      []DesignResult `json:"design,omitempty"`
}

// Adapter owns the isolated engine. Build gives the oracle the planning reply
// before continuing, so a reuse offer can only be credited before the build.
type Adapter interface {
	Build(context.Context, string, bool, func(BuildState) error) (BuildState, error)
	Call(context.Context, string, any) (json.RawMessage, error)
	Usage(context.Context) (Usage, error)
}

func Builtin() ([]Case, error) {
	file, err := fixtures.Open("cases/gallery.yaml")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Load(file)
}

func Load(in io.Reader) ([]Case, error) {
	var cases []Case
	dec := yaml.NewDecoder(in)
	dec.KnownFields(true)
	if err := dec.Decode(&cases); err != nil {
		return nil, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("expected one YAML document")
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if strings.TrimSpace(c.Name) == "" || seen[c.Name] || strings.TrimSpace(c.Prompt) == "" ||
			c.MaxRounds < 1 || len(c.Steps) == 0 {
			return nil, fmt.Errorf("case %q needs a unique name, prompt, positive round budget and steps", c.Name)
		}
		seen[c.Name] = true
		for _, step := range c.Steps {
			if step.Method == "" || (step.Error != "" && (len(step.Expect) > 0 || len(step.Capture) > 0)) {
				return nil, fmt.Errorf("case %q has an invalid step", c.Name)
			}
		}
	}
	if len(cases) == 0 {
		return nil, errors.New("no app eval cases")
	}
	return cases, nil
}

func Run(ctx context.Context, app Adapter, c Case, variables ...map[string]any) (result Result) {
	result.Case = c.Name
	defer func() {
		usage, err := app.Usage(ctx)
		if err != nil {
			result.Failures = append(result.Failures, "usage: "+err.Error())
		} else {
			result.Usage = &usage
			if c.NoModel && (usage.Calls != 0 || usage.TokensIn != 0 || usage.TokensOut != 0 ||
				(usage.Cost != nil && *usage.Cost != 0)) {
				result.Failures = append(result.Failures, "deterministic app called a model")
			}
		}
		result.Pass = len(result.Failures) == 0
	}()
	var sawPlan bool
	build, err := app.Build(ctx, c.Prompt, c.NoModel, func(state BuildState) error {
		sawPlan = true
		result.Failures = append(result.Failures, checkReply(state.Plan, c.Plan)...)
		return nil
	})
	result.Rounds, result.RecipeGaps = build.Rounds, build.RecipeGaps
	if len(c.Plan) > 0 && !sawPlan {
		result.Failures = append(result.Failures, "the planning reply was missing")
	}
	if build.Rounds > c.MaxRounds {
		result.Failures = append(result.Failures, fmt.Sprintf("rounds %d exceeded budget %d", build.Rounds, c.MaxRounds))
	}
	if err != nil {
		result.Failures = append(result.Failures, "build: "+err.Error())
	}
	if err != nil || !build.Checked || build.App == "" {
		if err == nil {
			result.Failures = append(result.Failures, "build did not pass Check and publish an app")
		}
		result.Adversarial = skippedProbes("build did not publish an app")
		return result
	}
	vars := map[string]any{}
	for _, supplied := range variables {
		maps.Copy(vars, supplied)
	}
	vars["app"] = build.App
	if err := runSteps(ctx, app, c.Steps, vars); err != nil {
		result.Failures = append(result.Failures, err.Error())
	}
	if c.Record.Type != "" {
		result.Adversarial = adversarial(ctx, app, c.Record, build.App)
		for _, probe := range result.Adversarial {
			if !probe.Pass {
				result.Failures = append(result.Failures, probe.Name+": "+probe.Problem)
			}
		}
	}
	return result
}

// CodeError carries a typed refusal so a transport failure can never satisfy it.
type CodeError string

func (e CodeError) Error() string { return string(e) }

func runSteps(ctx context.Context, app Adapter, steps []Step, vars map[string]any) error {
	for _, step := range steps {
		params, err := substitute(step.Params, vars)
		if err != nil {
			return fmt.Errorf("%s: %w", step.Method, err)
		}
		body, err := app.Call(ctx, step.Method, params)
		if err != nil || step.Error != "" {
			if step.Error != "" && errors.Is(err, CodeError(step.Error)) {
				continue
			}
			if err != nil {
				return fmt.Errorf("%s: %w", step.Method, err)
			}
			return fmt.Errorf("%s: expected error %q", step.Method, step.Error)
		}
		checks := slices.Clone(step.Expect)
		for i := range checks {
			checks[i].Equals, err = substitute(checks[i].Equals, vars)
			if err != nil {
				return fmt.Errorf("%s: %w", step.Method, err)
			}
		}
		if problems := checkReply(body, checks); len(problems) > 0 {
			return fmt.Errorf("%s: %s", step.Method, strings.Join(problems, ", "))
		}
		for _, key := range slices.Sorted(maps.Keys(step.Capture)) {
			value, ok := resolve(decode(body), step.Capture[key])
			if !ok {
				return fmt.Errorf("%s: could not capture %q at %s", step.Method, key, step.Capture[key])
			}
			vars[key] = value
		}
	}
	return nil
}

func substitute(value any, vars map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		if name, ok := strings.CutPrefix(v, "$"); ok {
			value, ok := vars[name]
			if !ok {
				return nil, fmt.Errorf("unbound variable %q", name)
			}
			return value, nil
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, value := range v {
			replacement, err := substitute(value, vars)
			if err != nil {
				return nil, err
			}
			out[k] = replacement
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, value := range v {
			replacement, err := substitute(value, vars)
			if err != nil {
				return nil, err
			}
			out[i] = replacement
		}
		return out, nil
	}
	return value, nil
}

func checkReply(body json.RawMessage, checks []Check) []string {
	if len(checks) == 0 {
		return nil
	}
	if !json.Valid(body) {
		return []string{"invalid JSON observation"}
	}
	tree := decode(body)
	var failures []string
	for _, c := range checks {
		got, ok := resolve(tree, c.Path)
		switch {
		case c.Exists != nil && *c.Exists != ok:
			failures = append(failures, fmt.Sprintf("%s existence should be %t", c.Path, *c.Exists))
		case c.Equals != nil && (!ok || !reflect.DeepEqual(got, normalize(c.Equals))):
			failures = append(failures, fmt.Sprintf("%s should equal %v, got %v", c.Path, c.Equals, got))
		case c.Length != nil && (!ok || listLength(got) != *c.Length):
			failures = append(failures, fmt.Sprintf("%s should have %d items, got %d", c.Path, *c.Length, listLength(got)))
		}
	}
	return failures
}

func listLength(value any) int {
	if items, ok := value.([]any); ok {
		return len(items)
	}
	return -1
}

func normalize(in any) any {
	raw, _ := json.Marshal(in)
	return decode(raw)
}

func decode(raw []byte) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

func resolve(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	if !strings.HasPrefix(path, "/") {
		return nil, false
	}
	for _, segment := range strings.Split(path[1:], "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			item, ok := v[key]
			if !ok {
				return nil, false
			}
			value = item
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(v) {
				return nil, false
			}
			value = v[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func WriteReport(report, table io.Writer, results []Result) error {
	enc := json.NewEncoder(report)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		return err
	}
	out := tabwriter.NewWriter(table, 0, 4, 2, ' ', 0)
	fmt.Fprintln(out, "CASE\tRESULT\tROUNDS\tTOKENS\tUSD\tRECIPE GAPS")
	for _, r := range results {
		status, tokens, price := "PASS", "—", "—"
		if !r.Pass {
			status = "FAIL"
		}
		if r.Usage != nil {
			tokens = strconv.FormatInt(r.Usage.TokensIn+r.Usage.TokensOut, 10)
			if r.Usage.Cost != nil {
				price = fmt.Sprintf("%.6f", *r.Usage.Cost)
				if r.Usage.Unpriced > 0 {
					price += " partial"
				}
			}
		}
		fmt.Fprintf(out, "%s\t%s\t%d\t%s\t%s\t%d\n", r.Case, status, r.Rounds, tokens, price, len(r.RecipeGaps))
	}
	return out.Flush()
}
