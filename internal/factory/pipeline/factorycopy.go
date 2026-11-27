package pipeline

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	noPipelineFileNote = "This repository has no " + RepoFile + " on its main branch, so this job runs the built-in steps."
	noIssueListNote    = RepoFile + " has no pipeline: issue: list, so this job runs the built-in steps."
)

// DecideJobPipeline records the file used when a door decides from a clean
// read. A door that only copies an existing decision uses Item.DecidePipeline
// because it has no new read to explain.
func (f File) DecideJobPipeline(it *factory.Item, now time.Time, pick string) {
	if it.PipelineIsDecided() {
		return
	}
	it.DecidePipeline(f.Copy, now)
	if !factory.IsIssueJob(*it) || it.RunOn != "" || pick != "" && pick != string(factory.KindIssue) {
		return
	}
	var note string
	switch f.From {
	case factory.FromDefault:
		note = noPipelineFileNote
	case factory.FromMainBranch, factory.FromWorkingTree, factory.FromFactoryCopy:
		note = noIssueListNote
	default:
		return
	}
	res, ok := f.Resolve(string(factory.KindIssue))
	if !ok || res.From != FromBuiltIn {
		return
	}
	// A routing rule can have chosen other steps while the job was in
	// Backlog. Those steps are kept when Start does not pick a pipeline.
	if pick == "" && it.Route != "" && !sameSteps(it.Stages, res.Stages) {
		return
	}
	// Bring back forgets the decision but keeps the job's activity.
	if !slices.ContainsFunc(it.Activity, func(ev factory.Event) bool {
		return ev.What == noPipelineFileNote || ev.What == noIssueListNote
	}) {
		it.Note(now, note)
	}
}

// CopyKeeper is where a factory keeps its own copies of repositories' pipeline
// files ([factory.PipelineCopy]): its store.
type CopyKeeper interface {
	PipelineCopy(repo string) (*factory.PipelineCopy, int, error)
	ChangePipelineCopy(repo string, change func(cur *factory.PipelineCopy, version int) (*factory.PipelineCopy, bool, error)) (*factory.PipelineCopy, int, error)
}

// WithFactoryCopy is one repository's read as the factory runs it: main is
// the read of its main branch's file BEFORE the personal file is merged (and
// mainErr its error), personal names the personal file, and keep is the
// factory's store. Whichever file is used is tightened by the personal file
// here ([MergePersonal]).
//
// PRECEDENCE: THE FACTORY'S COPY, ELSE MAIN'S FILE, ELSE THE BUILT-IN STEPS.
// When the factory keeps a copy for repo, the answer is that file, with From
// [factory.FromFactoryCopy] and Copy saying who saved it and when, tightened
// by the same personal file; main's read error no longer matters, because
// nothing of main's is used. The factory's own rules go on top afterwards
// ([WithHouseRules]), as they do on main's file.
//
// THE COPY CLEARS ITSELF when main's file, read cleanly from the main branch,
// is the same file ([SameFile]): its pull request merged, and main says what
// the copy said. The read is then main's, and the log says so. Main is judged
// on its own text, never with the personal file merged in, so a problem in
// the personal file cannot keep a merged copy standing.
//
// A COPY THAT CANNOT BE READ REFUSES THE READ like the factory's rules do
// ([ErrHouseRulesUnread]): a job must never quietly run main's steps when a
// person replaced them.
func WithFactoryCopy(main File, mainErr error, keep CopyKeeper, repo, personal string) (File, error) {
	if keep == nil || strings.TrimSpace(repo) == "" {
		return MergePersonal(main, personal, mainErr)
	}
	c, _, err := keep.PipelineCopy(repo)
	if err != nil {
		f, _ := MergePersonal(main, personal, mainErr)
		return f, fmt.Errorf("%w: its copy of %s: %v", ErrHouseRulesUnread, RepoFile, err)
	}
	if c == nil {
		return MergePersonal(main, personal, mainErr)
	}
	if mainErr == nil && MainSays(main, c.Text) {
		cleared, _, err := keep.ChangePipelineCopy(repo, func(cur *factory.PipelineCopy, _ int) (*factory.PipelineCopy, bool, error) {
			if cur == nil || !cur.Same(c) {
				return cur, false, nil
			}
			return nil, true, nil
		})
		if err == nil && cleared == nil {
			factory.NoteRecipeSource(repo, factory.FromMainBranch)
			return MergePersonal(main, personal, mainErr)
		}
	}
	f, err := CopyFile(*c)
	f, err = MergePersonal(f, personal, err)
	f.Copy = c.Clone()
	factory.NoteRecipeSource(repo, factory.FromFactoryCopy)
	return f, err
}

// JobFile is THE ONE READ of the file one job is bounded by, for every door
// that starts it, edits its steps, briefs its manager or checks its rules,
// posts and merges. A JOB'S FILE IS DECIDED ONCE ([factory.Item.DecidePipeline])
// and read here the same way by all of them:
//
//   - THE COPY THE JOB CARRIES ([factory.Item.PipelineCopy]), tightened by
//     the personal file as every read is;
//   - else, for a job decided on main's file, MAIN'S FILE ALONE (read with
//     withCopy false), never a copy the factory saved since;
//   - else, for a job not decided yet, the factory's read now (withCopy
//     true: its own copy, else main's file, else the built-in steps), which
//     its first launch then decides.
//
// read is the job's repository read with the personal file and before the
// factory's own rules. A copy off a reader's job list carries who saved it
// and when but not the file (api.FromItem); it is not one to read, and the
// factory's read now (withCopy true) is read instead.
func JobFile(it factory.Item, personal string, read func(withCopy bool) (File, error)) (File, error) {
	if c := it.PipelineCopy; c != nil && strings.TrimSpace(c.Text) != "" {
		f, err := CopyFile(*c)
		return MergePersonal(f, personal, err)
	}
	return read(!it.PipelineIsDecided() || it.PipelineCopy != nil)
}

// CopyFile is the file a factory copy reads as: Source file, From
// [factory.FromFactoryCopy], Copy set. A copy that no longer parses (it was
// checked when it was saved) is the defaults with its problem and the error.
func CopyFile(c factory.PipelineCopy) (File, error) {
	f, err := Parse(c.Text)
	f.Source = SourceFile
	f.From = factory.FromFactoryCopy
	f.Copy = c.Clone()
	return f, err
}

// MainSays is whether main, a read of the main branch before any personal
// file is merged, is a clean file that is the same as text ([SameFile]).
func MainSays(main File, text string) bool {
	_, unread := main.firstReadProblem()
	return main.From == factory.FromMainBranch && main.Source == SourceFile && !unread && SameFile(main.Text, text)
}

// SameFile says whether two pipeline files are the same file: both read
// cleanly and write the same bytes the way every writer here writes them
// ([Encode]), so a merge that kept the content but not the layout or comments
// is still the same file.
func SameFile(a, b string) bool {
	if a == b {
		return strings.TrimSpace(a) != ""
	}
	ea, ok := canonical(a)
	if !ok {
		return false
	}
	eb, ok := canonical(b)
	return ok && bytes.Equal(ea, eb)
}

func canonical(text string) ([]byte, bool) {
	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	f, err := Parse(text)
	if _, unread := f.firstReadProblem(); err != nil || unread {
		return nil, false
	}
	data, err := Encode(f)
	return data, err == nil
}
