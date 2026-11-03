package session

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

// WorkstreamSourceReadMax lets the app bound reads before JSON encoding.
const WorkstreamSourceReadMax = workstreamReadMax

type WorkstreamSourceProposal struct {
	Kind   string              `json:"kind"`
	Path   *string             `json:"path,omitempty"`
	Repo   *string             `json:"repo,omitempty"`
	Number *int                `json:"number,omitempty"`
	URL    *string             `json:"url,omitempty"`
	Ref    *workstream.KeptRef `json:"ref,omitempty"`
	Title  *string             `json:"title,omitempty"`
	Why    string              `json:"why"`
}

type WorkstreamSourceProposed struct {
	ID      string
	Already bool
}

func (p WorkstreamSourceProposal) SourceAdd() workstream.SourceAdd {
	return workstream.SourceAdd{Kind: p.Kind, Path: p.Path, Repo: p.Repo, Number: p.Number, URL: p.URL, Ref: p.Ref, Title: p.Title}
}

var workstreamProposeSourceSchema = fmt.Sprintf(`{"type":"object","properties":{"kind":{"type":"string","enum":["file","repo","issue","link","kept"]},"path":{"type":"string","minLength":1,"maxLength":%d},"repo":{"type":"string"},"number":{"type":"integer","minimum":1,"maximum":%d},"url":{"type":"string","minLength":1,"maxLength":%d},"ref":{"type":"object","properties":{"kind":{"type":"string","enum":["record","page","message","factory_job"]},"id":{"type":"string","minLength":1,"maxLength":%d},"chat_id":{"type":"string","minLength":1,"maxLength":%d},"factory":{"type":"string","minLength":1,"maxLength":%d}},"required":["kind","id"],"additionalProperties":false},"title":{"type":"string","minLength":1,"maxLength":%d},"why":{"type":"string","minLength":1,"maxLength":%d}},"required":["kind","why"],"additionalProperties":false}`, workstream.SourcePathMax, workstream.SourceIssueMax, workstream.SourceURLMax, workstream.SourceRefMax, workstream.SourceRefMax, workstream.SourceRefMax, workstream.SourceTitleMax, workstream.SourceWhyMax)

func workstreamSourceTool(door WorkstreamDoor) bare.Tool {
	return workstreamTool("propose_source", "Propose a source with a reason. The person keeps or drops it; jobs cannot read a proposal.", workstreamProposeSourceSchema, func(ctx context.Context, ask WorkstreamSourceProposal) (string, error) {
		if err := workstream.ValidateSourceWhy(ask.Why); err != nil {
			return "", &toolArgumentError{repair: err.Error()}
		}
		if err := workstream.ValidateSourceAdd(ask.SourceAdd()); err != nil {
			return "", &toolArgumentError{repair: err.Error()}
		}
		out, err := door.ProposeSource(ctx, ask)
		if err != nil {
			return "", err
		}
		if out.Already {
			return "Source " + out.ID + " is already present. The person keeps or drops it.", nil
		}
		return "Proposed source " + out.ID + ". The person keeps or drops it.", nil
	})
}
