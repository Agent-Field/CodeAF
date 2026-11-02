package api

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// jobArea is the area of the job's own change ([factory.JobArea]) against the
// areas its repository declares, nil for none. Only a run's change says which
// files a job touched: a pull request's own files sit on pull-request items,
// which are never jobs ([factory.IssueJobs]).
func jobArea(it factory.Item, areas []string) string {
	if it.Change == nil {
		return ""
	}
	paths := make([]string, len(it.Change.Files))
	for i, f := range it.Change.Files {
		paths[i] = f.Path
	}
	return factory.JobArea(areas, paths)
}

// InRepoAreas says a job's area again in the areas its repository declares,
// for a response that has the repositories at hand; [FromItem] alone knows
// none, and a repository that declares none leaves the job as it was.
func InRepoAreas(j *Job, it factory.Item, repos []factory.Repo) {
	name := factory.RepoOf(it)
	for _, r := range repos {
		if len(r.Areas) > 0 && strings.EqualFold(r.Name, name) {
			j.Area = jobArea(it, r.Areas)
			return
		}
	}
}
