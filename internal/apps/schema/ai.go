package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// CompileAIOutput gives Check and the run the same local output contract.
func CompileAIOutput(output map[string]any) (*jsonschema.Schema, error) {
	if len(output) == 0 {
		return nil, errors.New("declare an output schema")
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	const location = "https://app.invalid/output.json"
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	compiler.LoadURL = func(string) (io.ReadCloser, error) {
		return nil, errors.New("AI output schemas cannot load external references")
	}
	if err := compiler.AddResource(location, bytes.NewReader(raw)); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

// CheckAIProposal confines a draft action to one reviewed effect on its
// declared subject. There is no later step that can apply the model output.
func (r Run) CheckAIProposal(step *AIStep) error {
	if step.Propose == "" {
		return nil
	}
	param, ok := strings.CutPrefix(step.Propose, "$params.")
	if !ok || r.Params[param].Type != "ref" || r.Params[param].To == "" {
		return errors.New("an AI proposal needs a declared record reference")
	}
	if len(r.Steps) != 1 || r.Steps[0].AI != step {
		return errors.New("a proposed change must be the only step in its run")
	}
	props, _ := step.Output["properties"].(map[string]any)
	if step.Output["type"] != "object" || len(props) == 0 || step.Output["additionalProperties"] != false || step.Output["patternProperties"] != nil {
		return errors.New("a proposed change needs an object output with named properties and additionalProperties set to false")
	}
	return nil
}

// AIFieldOutput limits automatic fill to scalars understood by the writer.
func AIFieldOutput(field Field) map[string]any {
	if field.Required || field.Masked || field.Computed != "" || field.Sequence != nil {
		return nil
	}
	switch field.Type {
	case "text", "longtext":
		return map[string]any{"type": "string"}
	case "enum":
		return map[string]any{"type": "string", "enum": field.Values}
	case "number", "money":
		return map[string]any{"type": "number"}
	case "bool":
		return map[string]any{"type": "boolean"}
	default:
		return nil
	}
}

// Document retrieval is an effect. Run completions only get declared
// inputs, so an explicit instruction to fetch a location cannot work.
var promptFetchesDocument = regexp.MustCompile(`(?im)(?:^|[.!?]\s+|\band\s+)(?:please\s+)?(?:read|fetch|download|open|retrieve|browse|visit|load)\b[^.!?\n]{0,120}(?:https?://|\{\{[^}\n]*(?:url|link)\s*\}\})`)

func (c *checker) checkPromptSource(file, at, prompt string) {
	if promptFetchesDocument.MatchString(strings.TrimSpace(prompt)) {
		c.add(file, at, "run_source_unavailable", "This step cannot fetch a URL. Read the document through a source first, then give the step the retrieved text or an intake attachment.")
	}
}

func (c *checker) checkAIStep(file, at string, run Run, step *AIStep) {
	if run.Budget == nil || run.Budget.PerRun <= 0 {
		c.add(file, at+"/ai", CodeNotAllowed, "An AI step needs a positive per-run budget.")
	}
	if err := run.CheckAIProposal(step); err != nil {
		c.add(file, at+"/ai/propose", CodeStepKind, "%v", err)
	}
	shape, err := CompileAIOutput(step.Output)
	if err != nil {
		c.add(file, at+"/ai/output", CodeStepKind, "The AI output schema is invalid: %v", err)
		return
	}
	if step.Example == nil {
		c.add(file, at+"/ai/example", CodeStepKind, "Give Check an example response to use without a model.")
		return
	}
	if err := shape.Validate(step.Example); err != nil {
		c.add(file, at+"/ai/example", CodeStepKind, "The AI example does not match its output schema.")
	}
	if step.Propose == "" {
		return
	}
	param, _ := strings.CutPrefix(step.Propose, "$params.")
	typ := run.Params[param].To
	if !c.canWrite(typ) {
		c.add(file, at+"/ai/propose", CodeNotAllowed, "The proposal needs write access to %q.", typ)
	}
	fields, _ := c.typeFields(typ)
	life, managed := c.lifecycle(typ)
	props, _ := step.Output["properties"].(map[string]any)
	for _, name := range sortedKeys(props) {
		field, ok := fields[name]
		if !ok || field.Masked || field.Computed != "" || field.Sequence != nil || managed && life.Field == name {
			c.add(file, at+"/ai/output/properties/"+name, CodeNotAllowed, "The proposal cannot set %s.%s.", typ, name)
		}
	}
}
