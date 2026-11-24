package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAFolderIsToldFromARepositoryByItsPath(t *testing.T) {
	for repo, want := range map[string]bool{"/home/me/proj": true, "/srv": true, "acme/api": false, "api": false, "": false} {
		if got := factory.IsFolderRepo(repo); got != want {
			t.Errorf("IsFolderRepo(%q) = %v, want %v", repo, got, want)
		}
	}
	// A folder is never off, even one at the root whose path has one slash.
	sw := factory.NewRepoSwitch([]string{"acme/api"}, true)
	if sw.Off("/srv") || sw.Off("/home/me/proj") || !sw.Off("acme/web") {
		t.Fatal("the repository switch turned a folder off, or stopped turning a repository off")
	}
}

func TestAFolderOriginNamesItsGitHubRepository(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:Acme/API.git":        "acme/api",
		"https://github.com/acme/api":        "acme/api",
		"https://gitlab.com/acme/api.git":    "",
		"/home/me/elsewhere.git":             "",
		"ssh://git@github.com/acme/api.git/": "acme/api",
	} {
		if got := factory.GitHubRemoteName(remote); got != want {
			t.Errorf("GitHubRemoteName(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestOpenAPullRequestIsOfferedOnlyWhereItCanWork(t *testing.T) {
	done := factory.Item{Repo: "/home/me/proj", State: factory.StateShipped, HeadCommit: "abc",
		Stream: &factory.Stream{Log: []factory.LogLine{{Text: "branch: factory/1-x"}}}}
	if !factory.FolderPROffered(done, "acme/api") {
		t.Fatal("a finished folder job with a GitHub remote was not offered a pull request")
	}
	if factory.FolderPROffered(done, "") {
		t.Fatal("a folder with no GitHub remote was offered a pull request")
	}
	opened := done
	opened.FolderPR = &factory.PublishedPR{Number: 3, URL: "https://github.com/acme/api/pull/3"}
	onRepo := done
	onRepo.Repo = "acme/api"
	running := done
	running.State = factory.StateRunning
	for name, it := range map[string]factory.Item{"opened": opened, "a repository": onRepo, "running": running} {
		if factory.FolderPROffered(it, "acme/api") {
			t.Errorf("%s was offered a pull request", name)
		}
	}
}
