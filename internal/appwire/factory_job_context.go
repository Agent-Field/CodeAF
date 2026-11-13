package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FeatureFactoryJobContext announces the separate versioned attachment write.
const FeatureFactoryJobContext = "factory.job_context"

// JobActContextSet names the action in a successful attachment receipt.
const JobActContextSet = "context_set"

// FactoryJobContext is reference material the plan reads, outside the job text.
type FactoryJobContext struct {
	// Title names the attachment, trimmed to 1–200 characters.
	Title string `json:"title"`
	// Markdown is the complete reference text, separate from the description.
	Markdown string `json:"markdown"`
	// Source is an optional label, path or URL of at most 500 characters.
	Source string `json:"source,omitempty"`
}

// FactoryJobContextSetParams replaces one job's attachments; [] clears them.
type FactoryJobContextSetParams struct {
	Factory string              `json:"factory"`
	Job     int                 `json:"job"`
	Expect  *api.Expect         `json:"expect"`
	Context []FactoryJobContext `json:"context"`
}

// FactoryJobContextSetter carries one write, never re-sent on an uncertain outcome.
// The Factory decides whether the caller may edit and whether the job has begun.
type FactoryJobContextSetter interface {
	FactoryJobContextSet(context.Context, FactoryJobContextSetParams) (FactoryJobActed, error)
}

func withFactoryJobContextFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobContextSetter); ok {
		hello = withFeature(hello, FeatureFactoryJobContext)
	}
	return hello
}

func init() {
	methods["factory.job_context_set"] = shaped[FactoryJobContextSetParams, FactoryJobActed](setJobContext)
}

func setJobContext(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	s, ok := e.(FactoryJobContextSetter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.job_context_set")
	}
	var p FactoryJobContextSetParams
	if err := decodeStrict("factory.job_context_set", raw, &p); err != nil {
		return nil, err
	}
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return nil, err
	}
	if err := checkJobContext(p.Context); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryJobContextSet(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func checkJobContext(attachments []FactoryJobContext) error {
	if attachments == nil {
		return Errorf(CodeBadParams, "context is required: send [] to clear attachments")
	}
	if len(attachments) > MaxFactoryJobContextItems {
		return Errorf(CodeBadParams, "context takes at most %d items", MaxFactoryJobContextItems)
	}
	total := 0
	for i := range attachments {
		c := &attachments[i]
		c.Title = strings.TrimSpace(c.Title)
		if n := utf8.RuneCountInString(c.Title); n < 1 || n > MaxFactoryJobContextTitle {
			return Errorf(CodeBadParams, "context title must be 1–%d characters after trimming", MaxFactoryJobContextTitle)
		}
		if utf8.RuneCountInString(c.Source) > MaxFactoryJobContextSource {
			return Errorf(CodeBadParams, "context source must be at most %d characters", MaxFactoryJobContextSource)
		}
		if strings.TrimSpace(c.Markdown) == "" {
			return Errorf(CodeBadParams, "context markdown must be non-empty")
		}
		total += utf8.RuneCountInString(c.Markdown)
	}
	if total > MaxFactoryJobContext {
		return Errorf(CodeBadParams, "context markdown must total at most %d characters", MaxFactoryJobContext)
	}
	return nil
}
