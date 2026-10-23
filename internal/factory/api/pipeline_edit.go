package api

import "github.com/Agent-Field/codeaf/internal/factory/pipeline"

// GET /v1/repos/{owner}/{repo}/pipeline/edit-source answers the repository's
// pipeline file exactly as the DEFAULT BRANCH holds it at one pinned commit, read
// with the caller's own GitHub credential, so a person can edit it and open a
// pull request (POST .../pipeline/open-pr) from the very bytes they saw. It reads
// nothing from the host's checkout or personal file, takes no query, and writes
// nothing to GitHub or to disk.

// Pipeline edit-source states. Only "file" is editable.
const (
	PipelineEditFile     = "file"      // .codeaf/factory.yaml exists and reads without a problem
	PipelineEditMissing  = "missing"   // the default branch has no such file; nothing is invented in its place
	PipelineEditInvalid  = "invalid"   // it exists and does not read cleanly; Problems say why
	PipelineEditTooLarge = "too_large" // it is larger than a pull request can carry
)

// PipelineEditSource is the pinned file. Path is the one fixed file; BaseSHA is
// the 40-hex default-branch commit it was read at, and is what a pull request
// must pin as expected_base_sha. Text is the raw file, unchanged, for the states
// file and invalid. Everything after Editable is the parse of Text and is empty
// unless Editable.
type PipelineEditSource struct {
	Factory       Factory `json:"factory"`
	Repo          string  `json:"repo"`
	Path          string  `json:"path"`
	DefaultBranch string  `json:"default_branch"`
	BaseSHA       string  `json:"base_sha"`
	// CanPush is GitHub's answer for the caller's credential. A pull request
	// needs it; reading does not.
	CanPush bool `json:"can_push"`
	// Archived is GitHub's flag for the repository and is ALWAYS written, false
	// included, so a reader can tell "not archived" from "not said". An archived
	// repository can still answer can_push true and the pull request route refuses
	// it, so can_push alone is not "may open one".
	Archived bool `json:"archived"`
	// RuntimeFields is written in EVERY 200 state, false included: it is this
	// factory's own statement that its parser reads a step's kind, when, effort
	// and fanout and its runner obeys them. A factory that predates the words
	// cannot write it, so a reader treats absence as "does not say", never as
	// "probably fine".
	RuntimeFields bool     `json:"runtime_fields"`
	State         string   `json:"state" enum:"file,missing,invalid,too_large"`
	Editable      bool     `json:"editable"`
	Text          string   `json:"text,omitempty"`
	Problems      []string `json:"problems,omitempty"`

	// DefaultBudget is the file's. BudgetInFile is false when the file does not
	// state one and the number is the shipped default.
	DefaultBudget float64            `json:"default_budget,omitempty"`
	BudgetInFile  bool               `json:"budget_in_file,omitempty"`
	Pipelines     []PipelineEditKind `json:"pipelines,omitempty"`
	Steps         []PipelineEditStep `json:"steps,omitempty"`
	Fixed         []string           `json:"fixed,omitempty"`
}

// PipelineEditKind is one pipeline, its steps in order. InFile is false for a
// kind the file does not list, which runs the shipped steps.
type PipelineEditKind struct {
	Kind   string   `json:"kind"`
	Steps  []string `json:"steps"`
	InFile bool     `json:"in_file"`
}

// PipelineEditStep is one step definition. A Gate (approve) is a person and has
// no prompt. Locked steps are refused by the preview. InFile is false for a
// step whose wording is a shipped default the file does not write.
type PipelineEditStep struct {
	Name   string `json:"name"`
	Gate   bool   `json:"gate,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	Until  string `json:"until,omitempty"`
	Max    int    `json:"max,omitempty"`
	// Kind is check, gate or post; empty is a chat. When, Effort and Fanout are
	// the runtime words the file wrote on the step, empty when it wrote none.
	Kind   string `json:"kind,omitempty"`
	When   string `json:"when,omitempty"`
	Effort string `json:"effort,omitempty"`
	Fanout string `json:"fanout,omitempty"`
	Locked bool   `json:"locked"`
	InFile bool   `json:"in_file"`
}

// Edit-source failure stages, in the order of the route's sequence.
const (
	PipelineEditStageCredential = "credential"
	PipelineEditStageRequest    = "request"
	PipelineEditStageRepository = "repository"
	PipelineEditStageBase       = "base"
	PipelineEditStageFile       = "file"
)

// PipelineEditSourceFailure is the route's non-2xx body. This route only reads,
// so it wrote nothing; it says nothing about what anyone else may have written
// to the repository meanwhile.
type PipelineEditSourceFailure struct {
	Factory      Factory `json:"factory"`
	Error        string  `json:"error"`
	Reason       string  `json:"reason,omitempty"`
	Stage        string  `json:"stage"`
	GitHubStatus int     `json:"github_status,omitempty"`
	RequestID    string  `json:"request_id"`
}

// SetParsed fills the fields that are the parse of Text from what the pipeline
// package read, so the factory that answers the route and the engine that checks
// the answer build them with the same code.
func (s *PipelineEditSource) SetParsed(src pipeline.Source) {
	s.DefaultBudget, s.BudgetInFile = src.Budget, src.BudgetInFile
	s.Fixed = append([]string(nil), src.Fixed...)
	s.Pipelines, s.Steps = nil, nil
	for _, k := range src.Kinds {
		if k.Kind == "pr" || k.Kind == "ci" {
			continue
		}
		s.Pipelines = append(s.Pipelines, PipelineEditKind{Kind: k.Kind, Steps: append([]string{}, k.Steps...), InFile: k.InFile})
	}
	for _, st := range src.Steps {
		s.Steps = append(s.Steps, PipelineEditStep{Name: st.Name, Gate: st.Gate, Prompt: st.Prompt, Until: st.Until,
			Max: st.Max, Kind: st.Kind, When: st.When, Effort: st.Effort, Fanout: st.Fanout, Locked: st.Locked, InFile: st.InFile})
	}
}
