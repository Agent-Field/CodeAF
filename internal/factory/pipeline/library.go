package pipeline

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"gopkg.in/yaml.v3"
)

// ── THE FACTORY'S PIPELINE LIBRARY ──────────────────────────────────────────
//
// The library is named pipelines a factory keeps for every repository it
// works on, with the step definitions they use: the same YAML as a
// repository's .codeaf/factory.yaml, `pipeline:` and `steps:` and nothing
// else. It lives in the factory's store (store.PipelineLibrary), not in any
// repository and not on one person's machine, so a pipeline written once runs
// on every repository the factory sees, and a routing rule or a Start can
// name it there.
//
// WHICH PIPELINE A NAME RUNS IS DECIDED IN ONE PLACE, [File.Resolve], for
// every door that names one (factory.job_start {pipeline}, a routing rule's
// then.pipeline, a job's own kind through [File.Recipe]):
//
//  1. the repository's file as this factory runs it: the factory's own copy
//     when it keeps one ([WithFactoryCopy]), else main's file;
//  2. the library ([WithLibrary]);
//  3. the built-in steps, which only ever name issue.
//
// So A REPOSITORY'S PIPELINE OF THE SAME NAME OVERRIDES THE LIBRARY'S, and a
// library issue pipeline is what a repository runs only when its own file
// does not list issue itself.
//
// A LIBRARY PIPELINE RUNS THE LIBRARY'S WORDS. Its steps are compiled from the
// library's own definitions (and the shipped steps it leaves as shipped),
// never from a repository's step of the same name, and a library step never
// changes a repository pipeline's step: the two files are read side by side,
// not merged.
//
// A FACTORY WITH NO LIBRARY RUNS EXACTLY WHAT IT RAN BEFORE: every name
// resolves to the repository's file or the built-in steps, as it always did.

// Where a resolved pipeline came from ([Resolved.From]).
const (
	FromRepoFile    = "repo"
	FromFactoryCopy = "factory_copy"
	FromLibrary     = "library"
	FromBuiltIn     = "built_in"
)

// LibraryTextMax is the most text a library holds: a repository file's cap.
const LibraryTextMax = EditTextMax

// Resolved is the pipeline a name runs: its Name, the step names it lists
// (Steps) and the stages they compile to (Stages), and From, the layer that
// supplied it. Over is every lower layer that also has a pipeline of that
// name, which this one overrides, in order (library, built_in).
type Resolved struct {
	Name   string
	From   string
	Steps  []string
	Stages []factory.Stage
	Over   []string
}

// WithLibrary is f with the factory's pipeline library beside it, for
// [File.Resolve] and [File.Recipe]. A library that names no pipeline leaves
// f exactly as it was.
func WithLibrary(f File, lib File) File {
	if len(lib.named()) == 0 {
		return f
	}
	keep := lib
	keep.library = nil
	f.library = &keep
	return f
}

// Library is the library this read was given ([WithLibrary]), and whether
// there was one.
func (f File) Library() (File, bool) {
	if f.library == nil {
		return File{}, false
	}
	return *f.library, true
}

// names says whether the file itself lists kind: any pipeline it holds but
// issue, and issue only when the file writes it (a file that does not is
// running the built-in issue steps). A Markdown recipe always writes its own.
func (f File) names(kind factory.Kind) bool {
	if _, ok := f.Pipeline[kind]; !ok {
		return false
	}
	return kind != factory.KindIssue || f.set.pipeline[kind] || f.Source == SourceMarkdown
}

// named is every pipeline the file itself lists ([File.names]), in the order
// a person reads them.
func (f File) named() []string {
	var out []string
	for _, kind := range sortedKinds(f) {
		if f.names(factory.Kind(kind)) {
			out = append(out, kind)
		}
	}
	return out
}

// Resolve is THE ONE ANSWER to which pipeline a name runs on this read: the
// repository's file, else the library, else the built-in steps (see the
// package's library note). ok is false for a name no layer has, or whose list
// compiles to nothing a runner can run.
func (f File) Resolve(name string) (Resolved, bool) {
	kind := factory.Kind(name)
	lib := f.library
	var over []string
	if lib != nil && lib.names(kind) {
		over = append(over, FromLibrary)
	}
	if kind == factory.KindIssue {
		over = append(over, FromBuiltIn)
	}
	var res Resolved
	switch {
	case f.names(kind):
		from := FromRepoFile
		if f.Copy != nil {
			from = FromFactoryCopy
		}
		res = Resolved{Name: name, From: from, Steps: slices.Clone(f.Pipeline[kind]), Stages: f.stages(f.Pipeline[kind]), Over: over}
	case lib != nil && lib.names(kind):
		res = Resolved{Name: name, From: FromLibrary, Steps: slices.Clone(lib.Pipeline[kind]), Stages: lib.stages(lib.Pipeline[kind]), Over: over[1:]}
	case kind == factory.KindIssue:
		// The built-in list, with whatever words the file gives its steps:
		// what a repository with no issue pipeline of its own always ran.
		res = Resolved{Name: name, From: FromBuiltIn, Steps: slices.Clone(f.Pipeline[kind]), Stages: f.stages(f.Pipeline[kind])}
	default:
		return Resolved{}, false
	}
	if len(res.Over) == 0 {
		res.Over = nil
	}
	return res, len(res.Stages) > 0
}

// Named is the stages the pipeline called name runs ([File.Resolve]), nil when
// no layer has one: what factory.job_start {pipeline} and a routing rule's
// then.pipeline run.
func (f File) Named(name string) []factory.Stage {
	res, ok := f.Resolve(name)
	if !ok {
		return nil
	}
	return res.Stages
}

// Pipelines is every pipeline a job on this read can run, one per name, each
// as [File.Resolve] answers it: issue first, then pr and ci, then every other
// name alphabetically. A name some layer has but whose list runs nothing is
// left out.
func (f File) Pipelines() []Resolved {
	var out []Resolved
	for _, name := range f.pipelineNames() {
		if res, ok := f.Resolve(name); ok {
			out = append(out, res)
		}
	}
	return out
}

// pipelineNames is every name the file holds and every name the library
// lists, in the order a person reads them.
func (f File) pipelineNames() []string {
	all := File{Pipeline: map[factory.Kind][]string{}}
	for kind := range f.Pipeline {
		all.Pipeline[kind] = nil
	}
	if f.library != nil {
		for _, kind := range f.library.named() {
			all.Pipeline[factory.Kind(kind)] = nil
		}
	}
	return sortedKinds(all)
}

// PipelineFile is the file whose step definitions the pipeline called name
// compiles from: the library for a name the library supplies, else f.
func (f File) PipelineFile(name string) File {
	if res, ok := f.Resolve(name); ok && res.From == FromLibrary {
		return *f.library
	}
	return f
}

// ParseLibrary reads a library's text: "" is an empty library. A library is
// held to more than a repository file ([CheckLibrary]) when it is saved, so
// a stored one that no longer reads cleanly is an error, never half a library.
func ParseLibrary(text string) (File, error) {
	if text == "" {
		return Defaults(), nil
	}
	if _, refused := CheckLibrary(text); refused != "" {
		return Defaults(), fmt.Errorf("the factory's pipeline library cannot be read: %s", refused)
	}
	return Parse(text)
}

// CheckLibrary is text as the factory keeps it, or why it does not take it.
//
// A LIBRARY IS `pipeline:` AND `steps:` ONLY: start rules, sizes, budgets,
// rules, fixed, adapt and merge belong to a repository (or to the factory's
// own rules and spending) and are refused rather than kept where nothing
// reads them. It must read with no problem, list no pipeline named pr or ci,
// and run every step it lists ([runtimeDrops]). The answer is the canonical
// encoding ([Encode]), which writes only what the text wrote: a shipped step
// it names but leaves as shipped is not written out. "" (or a text that
// writes nothing) is the empty library, kept as "".
func CheckLibrary(text string) ([]byte, string) {
	switch {
	case strings.TrimSpace(text) == "":
		return nil, ""
	case len(text) > LibraryTextMax:
		return nil, fmt.Sprintf("the library is at most %d bytes", LibraryTextMax)
	case !utf8.ValidString(text):
		return nil, "the library must be valid UTF-8"
	}
	root, err := decodeRoot(text)
	if err != nil {
		if strings.Contains(err.Error(), "is a map of") {
			return nil, "the library is a map of pipeline and steps"
		}
		return nil, err.Error()
	}
	if root != nil {
		if root.Kind != yaml.MappingNode {
			return nil, "the library is a map of pipeline and steps"
		}
		for i := 0; i+1 < len(root.Content); i += 2 {
			switch key := root.Content[i].Value; key {
			case "pipeline", "steps":
			default:
				return nil, fmt.Sprintf("line %d: %s belongs to a repository's file; the library keeps pipeline and steps only", root.Content[i].Line, key)
			}
		}
	}
	f, err := Parse(text)
	if err != nil {
		return nil, err.Error()
	}
	if len(f.Problems) > 0 {
		return nil, f.Problems[0].Error()
	}
	if why := libraryRefusal(f); why != "" {
		return nil, why
	}
	return EncodeLibrary(f)
}

// libraryRefusal is why a parsed library is not one the factory keeps, or "".
func libraryRefusal(f File) string {
	for _, kind := range f.named() {
		if kind == string(factory.KindPR) || kind == string(factory.KindCI) {
			return ReservedPipeline(kind)
		}
	}
	if drops := runtimeDrops(f); len(drops) > 0 {
		return drops[0]
	}
	return ""
}

// EncodeLibrary writes a library the way [Encode] writes a file, "" for one
// that writes nothing.
func EncodeLibrary(f File) ([]byte, string) {
	data, err := Encode(f)
	if err != nil {
		return nil, err.Error()
	}
	if t := bytes.TrimSpace(data); len(t) == 0 || string(t) == "{}" {
		return nil, ""
	}
	if len(data) > LibraryTextMax {
		return nil, fmt.Sprintf("the library would be larger than %d bytes", LibraryTextMax)
	}
	return data, ""
}

// LibraryOp says whether an edit verb can change a library: the verbs that
// touch only `pipeline:` and `steps:`.
func LibraryOp(verb string) bool {
	switch verb {
	case OpSetStep, OpAddStep, OpRemoveStep, OpMoveStep, OpRenameStep, OpSetApprover,
		OpAddPipeline, OpRemovePipeline, OpRenamePipeline:
		return true
	}
	return false
}

// LibraryEdited is the library edits make of base ("" for an empty library):
// [ApplyEdits] for the verbs a library takes ([LibraryOp]), answered as the
// library the factory would keep. Text is "" for an edit that leaves the
// library empty.
func LibraryEdited(base string, ops []EditOp) (Edited, error) {
	for i, op := range ops {
		if !LibraryOp(op.Op) {
			return Edited{}, refuseEdit(EditBadOp, i, "%s changes a repository's file; the library keeps pipelines and steps only", op.Op)
		}
	}
	if base != "" {
		if _, refused := CheckLibrary(base); refused != "" {
			return Edited{}, refuseEdit(EditBaseInvalid, -1, "%s", refused)
		}
	}
	ed, err := ApplyEdits(base, ops)
	if err != nil {
		return ed, err
	}
	if !ed.Changed {
		return ed, nil
	}
	data, refused := CheckLibrary(ed.Text)
	if refused != "" {
		return Edited{}, refuseEdit(EditNotRepresentable, -1, "%s", refused)
	}
	ed.Text = string(data)
	return ed, nil
}
