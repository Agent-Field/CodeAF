package pipeline

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// CopyKeeper is where a factory keeps its own copies of repositories' pipeline
// files ([factory.PipelineCopy]): its store.
type CopyKeeper interface {
	PipelineCopy(repo string) (*factory.PipelineCopy, int, error)
	ChangePipelineCopy(repo string, change func(cur *factory.PipelineCopy, version int) (*factory.PipelineCopy, bool, error)) (*factory.PipelineCopy, int, error)
}

// WithFactoryCopy is one repository's read as the factory runs it: main is
// the read of its main branch's file (with the personal file merged as
// personal names it, and mainErr its error), and keep is the factory's store.
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
// the copy said. The read is then main's, and the log says so.
//
// A COPY THAT CANNOT BE READ REFUSES THE READ like the factory's rules do
// ([ErrHouseRulesUnread]): a job must never quietly run main's steps when a
// person replaced them.
func WithFactoryCopy(main File, mainErr error, keep CopyKeeper, repo, personal string) (File, error) {
	if keep == nil || strings.TrimSpace(repo) == "" {
		return main, mainErr
	}
	c, _, err := keep.PipelineCopy(repo)
	if err != nil {
		return main, fmt.Errorf("%w: its copy of %s: %v", ErrHouseRulesUnread, RepoFile, err)
	}
	if c == nil {
		return main, mainErr
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
			return main, mainErr
		}
	}
	f, err := CopyFile(*c)
	f, err = MergePersonal(f, personal, err)
	f.Copy = c.Clone()
	factory.NoteRecipeSource(repo, factory.FromFactoryCopy)
	return f, err
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

// MainSays is whether main, a read of the main branch, is a clean file that
// is the same as text ([SameFile]).
func MainSays(main File, text string) bool {
	return main.From == factory.FromMainBranch && main.Source == SourceFile && len(main.Problems) == 0 && SameFile(main.Text, text)
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
	if err != nil || len(f.Problems) > 0 {
		return nil, false
	}
	data, err := Encode(f)
	return data, err == nil
}
