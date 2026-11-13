package github

// pr_files.go reads the file list of a pull request the factory follows, for
// every job that has one: a run built here, a run another service built, a
// pull request delivered from anywhere. The merge card reads it to say what a
// merge puts on the base branch, above all what it removes, and nothing here
// knows a pipeline, a step or a path by its name.

import (
	"context"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// pullFileLister is the read a client has of a pull request's whole file list
// with each file's status. A client without it leaves the list unknown.
type pullFileLister interface {
	PullFileList(ctx context.Context, owner, repo string, number, most int) ([]forge.PullFile, error)
}

// prFilesKept bounds the per-process cache. One row per pull request is small,
// but a factory that runs for months follows thousands of merged ones, and the
// job's own stored list is what carries a quiet one across (see [Source.readPRFiles]).
const prFilesKept = 1024

// prFilesRow is one pull request's list as it was read on one head.
type prFilesRow struct {
	head  string
	files []factory.ChangedFile
	total int
}

// readPRFiles fills an open pull request's file list for the head it points
// at now. A LIST IS READ ONCE PER HEAD: the job's stored list for the same head
// answers first, then this process's cache, and only a head nobody has read
// costs requests. A failed read leaves the list unknown, never an older
// head's; it is not a read problem, because a missing list does not make any
// other fact about the pull request less true, and the merge card already
// says plainly when it cannot see the files.
func (s *Source) readPRFiles(ctx context.Context, owner, repo string, p forge.Pull, prev *factory.PullRequest, out *factory.PullRequest) {
	if p.HeadSHA == "" {
		return
	}
	if keepPRFiles(prev, out) {
		return
	}
	key := prKey(owner, repo, p.Number)
	s.mu.Lock()
	row, ok := s.prFiles[key]
	s.mu.Unlock()
	if !ok || row.head != p.HeadSHA {
		lister, can := s.api.(pullFileLister)
		if !can {
			return
		}
		got, err := lister.PullFileList(ctx, owner, repo, p.Number, factory.PRFilesMost)
		if err != nil {
			return
		}
		row = prFilesRow{head: p.HeadSHA, files: prFilesOf(got), total: max(p.Files, len(got))}
		s.mu.Lock()
		if s.prFiles == nil {
			s.prFiles = map[string]prFilesRow{}
		}
		if _, known := s.prFiles[key]; !known && len(s.prFiles) >= prFilesKept {
			for k := range s.prFiles {
				delete(s.prFiles, k)
				break
			}
		}
		s.prFiles[key] = row
		s.mu.Unlock()
	}
	if len(row.files) == 0 && row.total == 0 {
		// AN EMPTY CHANGE IS A FACT ONLY WHEN BOTH OF GITHUB'S ANSWERS SAY
		// SO: the file list read on this head is empty, and the pull request's
		// own counts (changed files, lines added and removed) are all zero.
		// Either alone stays unknown, so a list GitHub has not filled yet
		// never reads as a pull request that changes nothing.
		if p.Files == 0 && p.Additions == 0 && p.Deletions == 0 {
			out.Files, out.FilesTotal, out.FilesHead = nil, 0, row.head
		}
		return
	}
	out.Files, out.FilesTotal, out.FilesHead = append([]factory.ChangedFile(nil), row.files...), row.total, row.head
}

// keepPRFiles carries prev's list onto out when it was read on out's head,
// and says whether it did. A list for any other head is dropped, not kept.
func keepPRFiles(prev *factory.PullRequest, out *factory.PullRequest) bool {
	if prev == nil || out.HeadSHA == "" || prev.FilesHead != out.HeadSHA {
		return false
	}
	out.Files, out.FilesTotal, out.FilesHead = append([]factory.ChangedFile(nil), prev.Files...), prev.FilesTotal, prev.FilesHead
	return true
}

func prKey(owner, repo string, number int) string {
	return owner + "/" + repo + "#" + strconv.Itoa(number)
}

// prFilesOf is GitHub's list in the factory's words. GitHub's "changed" is a
// file whose mode alone changed, which is a modification; every other status
// keeps GitHub's own word, so a client that has no word for one shows it as
// it came rather than guessing.
func prFilesOf(in []forge.PullFile) []factory.ChangedFile {
	out := make([]factory.ChangedFile, 0, len(in))
	for _, f := range in {
		status := f.Status
		if status == "changed" || status == "" {
			status = "modified"
		}
		out = append(out, factory.ChangedFile{Path: f.Path, Status: status, Added: f.Additions, Removed: f.Deletions, PreviousPath: f.Previous})
	}
	return out
}
