package factory

import (
	"net/url"
	"strconv"
	"strings"
)

// JobSourceFacts is where a job came from, said once for every kind of source:
// Name is the source (`github`, `jira`, `inbox:ops`, `terminal`, `chat`), Ref
// is the job's key on it in that source's own spelling (`owner/repo#147`,
// `ENG-142`, `A1`), and URL is the job's address there when the source sent
// one. A surface draws the source chip and its link from these and never
// guesses an address from a repository name.
type JobSourceFacts struct {
	Name string `json:"name"`
	Ref  string `json:"ref,omitempty"`
	URL  string `json:"url,omitempty"`
}

// SourceKey splits the SourceRef of a job filed from outside the forge into
// its source and its key there. The source part is the item's Origin, which
// is how the two halves are told apart when the source name itself holds a
// colon (`inbox:ops:A1`). ok is false for every other job.
func SourceKey(it Item) (source, id string, ok bool) {
	source = string(it.Origin)
	id, ok = strings.CutPrefix(it.SourceRef, source+":")
	return source, id, ok && source != "" && id != ""
}

// JobSource is the job's [JobSourceFacts]; ok is false for a job that says
// nothing about where it came from.
func JobSource(it Item) (JobSourceFacts, bool) {
	if source, id, ok := SourceKey(it); ok {
		return JobSourceFacts{Name: source, Ref: id, URL: strings.TrimSpace(it.URL)}, true
	}
	if it.Origin == OriginForge || it.Origin == "" && it.Num > 0 {
		ref := RepoOf(it)
		if it.Num > 0 {
			ref += "#" + strconv.Itoa(it.Num)
		}
		return JobSourceFacts{Name: string(OriginForge), Ref: ref, URL: strings.TrimSpace(it.URL)}, true
	}
	if it.Origin == "" {
		return JobSourceFacts{}, false
	}
	return JobSourceFacts{Name: string(it.Origin), URL: strings.TrimSpace(it.URL)}, true
}

// RepoURL is the address of the job's repository ([RepoOf]) on its forge, ""
// when the job names no `owner/name`. It is read off an address the forge
// itself gave for this repository — the job's pull request, then its issue —
// so a forge that is not github.com keeps its own host; only a job with
// neither falls back to github.com, the one forge the factory publishes to.
func RepoURL(it Item) string {
	repo := RepoOf(it)
	if repo == "" {
		return ""
	}
	var seen []string
	if it.PullRequest != nil {
		seen = append(seen, it.PullRequest.URL)
	}
	if it.SourceRef == "" {
		seen = append(seen, it.URL)
	}
	for _, raw := range seen {
		if base := repoBase(raw, repo); base != "" {
			return base
		}
	}
	return "https://github.com/" + repo
}

// repoBase is `scheme://host/owner/name` when raw is an http(s) address under
// that repository, "" otherwise.
func repoBase(raw, repo string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	parts := strings.SplitN(strings.Trim(u.Path, "/"), "/", 3)
	if len(parts) < 2 || !strings.EqualFold(parts[0]+"/"+parts[1], repo) {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/" + parts[0] + "/" + parts[1]
}
