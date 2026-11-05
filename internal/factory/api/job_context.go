package api

import "github.com/Agent-Field/codeaf/internal/factory"

// JobContextRequest replaces attachments before the job's first run; [] clears them.
type JobContextRequest struct {
	Context []factory.JobContext `json:"context"`
	Expect  *Expect              `json:"expect"`
}

// JobContext names an attachment on a job read; only worker admission carries Markdown.
type JobContext struct {
	Title  string `json:"title"`
	Source string `json:"source,omitempty"`
	// Markdown crosses only to the worker that must brief the plan.
	Markdown string `json:"markdown,omitempty"`
}

func fromJobContext(c factory.JobContext) JobContext {
	return JobContext{Title: c.Title, Source: c.Source}
}

func toJobContext(c JobContext) factory.JobContext {
	return factory.JobContext{Title: c.Title, Source: c.Source, Markdown: c.Markdown}
}
