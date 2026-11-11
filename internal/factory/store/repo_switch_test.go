package store

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The switch exists once the list does: a floor that never kept one has
// nothing off, and a list saved empty has every repository off. A name that
// is not owner/name is never off, and case does not matter.
func TestTheWatchedListIsTheRepoSwitch(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sw, err := st.RepoSwitch()
	if err != nil {
		t.Fatal(err)
	}
	if sw.Off("acme/api") || sw.Watched("acme/api") {
		t.Fatal("a floor with no list switched a repository off")
	}
	if err := st.SetRepos([]string{"Acme/API"}); err != nil {
		t.Fatal(err)
	}
	if sw, err = st.RepoSwitch(); err != nil {
		t.Fatal(err)
	}
	if sw.Off("acme/api") || !sw.Watched("acme/api") || !sw.Off("acme/web") || sw.Off("web") {
		t.Fatalf("switch with one repository on = %+v", sw)
	}
	if !sw.OffJob(factory.Item{Repo: "web", Product: "acme"}) || sw.OffJob(factory.Item{Repo: "api", Product: "acme"}) {
		t.Fatal("a job's repository was not read as owner/name")
	}
	if err := st.SetRepos(nil); err != nil {
		t.Fatal(err)
	}
	if sw, err = st.RepoSwitch(); err != nil {
		t.Fatal(err)
	}
	if !sw.Off("acme/api") {
		t.Fatal("an empty list left a repository on")
	}
}

// The chat's door makes no job on a repository that is off.
func TestAddRefusesARepoThatIsOff(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "new work"}); err == nil || err.Error() != factory.RepoOffWords("acme/api") {
		t.Fatalf("add on an off repository = %v", err)
	}
	if items, _ := st.List(); len(items) != 0 {
		t.Fatalf("a job was made: %+v", items)
	}
	if _, err := st.Add(context.Background(), factory.Item{Repo: "acme/web", Title: "new work"}); err != nil {
		t.Fatal(err)
	}
}
