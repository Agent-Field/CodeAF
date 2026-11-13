package api

import "github.com/Agent-Field/codeaf/internal/factory"

// GET /v1/pipeline-library answers the factory's pipeline library: named
// pipelines and the step definitions they use, kept by the factory for every
// repository it works on (internal/factory/pipeline, library.go). Everyone
// who may read the factory may read it. PUT replaces it, for the factory's
// operator only, with the version the writer read; a library written since
// refuses the write (409) and nothing is written.

// PipelineLibraryRequest is the PUT body. Text is the whole library, the same
// YAML as a repository file's `pipeline:` and `steps:` and nothing else, ""
// for an empty one; it is REQUIRED, so a client that sent nothing cannot wipe
// the library. Expect is the version the writer read, from 1.
type PipelineLibraryRequest struct {
	Text   *string             `json:"text"`
	Expect *PipelineCopyExpect `json:"expect"`
}

// PipelineLibraryAnswer is the library as the factory keeps it: its canonical
// Text ("" when empty), its Version, who wrote it last and when, every
// pipeline it names with its steps, and whether the caller may change it.
type PipelineLibraryAnswer struct {
	Factory   Factory            `json:"factory"`
	Text      string             `json:"text"`
	Version   int                `json:"version"`
	Pipelines []PipelineChoice   `json:"pipelines"`
	Can       PipelineLibraryCan `json:"can"`
	factory.RulesChange
}

// PipelineLibraryCan is what the caller may do with the library: Change is
// replacing it, the operator's alone.
type PipelineLibraryCan struct {
	Change bool `json:"change"`
}

// PipelineChoice is one pipeline a job can run, by name, with the step names
// it lists in order and From, where the factory found it: repo (the
// repository's own file), factory_copy (the factory's copy of that file),
// library (the factory's library) or built_in (the steps codeaf ships, issue
// only). Over is every lower layer that also has a pipeline of that name,
// which this one overrides. On the library's own answer From is library.
type PipelineChoice struct {
	Name  string   `json:"name"`
	From  string   `json:"from" enum:"repo,factory_copy,library,built_in"`
	Steps []string `json:"steps"`
	Over  []string `json:"over,omitempty"`
}
