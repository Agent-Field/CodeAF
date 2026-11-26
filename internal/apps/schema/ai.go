package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	if step.Output["type"] != "object" || step.Output["additionalProperties"] != false {
		return errors.New("a proposed change needs an object output with additionalProperties set to false")
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
