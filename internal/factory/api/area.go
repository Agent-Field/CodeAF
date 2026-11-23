package api

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// AreaIn says a job's area ([factory.JobArea]) from its own change, in the
// areas its repository declares among repos. It is the one rule for area:
// [FromItem] says it with no declarations, [FromSnapshot] with the floor's,
// and the factory's server says it again for every job it sends (serve's
// withVersion) with the declarations of its last floor read, so a verb's job
// and the queue never name different areas. Only a
// run's change says which files a job touched: a pull request's own files sit
// on pull-request items, which are never jobs ([factory.IssueJobs]).
func AreaIn(j *Job, repos []factory.Repo) {
	j.Area = ""
	if j.Change == nil {
		return
	}
	paths := make([]string, len(j.Change.Files))
	for i, f := range j.Change.Files {
		paths[i] = f.Path
	}
	j.Area = factory.JobArea(declaredAreas(repos, j.Repo), paths)
}

func declaredAreas(repos []factory.Repo, name string) []string {
	for _, r := range repos {
		if strings.EqualFold(r.Name, name) {
			return r.Areas
		}
	}
	return nil
}
