package craft

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCorrupt(repo *Repo) error {
	return os.WriteFile(filepath.Join(repo.Dir(), WorkflowDir, "broken.yaml"),
		[]byte("name: broken\nsteps: [ oh no\n"), 0o644)
}

const changelogYAML = `name: changelog
description: Read the week's commits and write the changelog the team reads on Monday.
steps:
  - id: gather
    brief: List every commit merged since the last tag, with its author and its subject.
  - id: group
    brief: Group the commits into features, fixes and chores, dropping the ones nobody outside the repo cares about.
    needs:
      - gather
  - id: write
    brief: Write the changelog entry, one line per grouped change, newest section on top.
    needs:
      - group
`

const invoiceYAML = `name: invoice
description: Build the month's invoice from tracked time and send it for approval.
steps:
  - id: collect
    brief: Collect every tracked hour for the month, per client and per project.
  - id: rate
    brief: Apply the agreed rate to each line and total it.
    needs:
      - collect
  - id: render
    brief: Render the invoice PDF and put it where the approver will see it.
    needs:
      - rate
`

func matchRepo(t *testing.T) *Repo {
	t.Helper()
	repo := openRepo(t)
	for _, file := range []string{presentationYAML, changelogYAML, invoiceYAML} {
		w := parseValid(t, file)
		if _, err := repo.Save(w, "first draft"); err != nil {
			t.Fatalf("save %s: %v", w.Name, err)
		}
	}
	return repo
}

// The whole point of keeping learned know-how: a request in the user's own
// words has to reach the craft that was distilled for it, without the user
// knowing a workflow by that name exists.
func TestMatchFindsTheCraftTheRequestIsAbout(t *testing.T) {
	repo := matchRepo(t)

	for _, probe := range []struct {
		request string
		want    string
	}{
		{"make me a presentation about the Q3 numbers", "presentation"},
		{"can you put together some slides for the board", "presentation"},
		{"write up the changelog for this week", "changelog"},
		{"I need this month's invoice for the client", "invoice"},
	} {
		found := repo.Match(probe.request, DefaultMatches)
		if len(found) == 0 {
			t.Fatalf("%q matched nothing", probe.request)
		}
		if found[0].Name != probe.want {
			t.Fatalf("%q ranked %s first (%.2f), wanted %s", probe.request, found[0].Name, found[0].Score, probe.want)
		}
		if found[0].Score < MatchFloor {
			t.Fatalf("%q scored %.2f, under the floor", probe.request, found[0].Score)
		}
		// The description comes back because it is what a caller shows. The
		// version does not: ranking never reads it, the caller picks by score
		// and name, and Load resolves the version it will actually run —
		// including an uncommitted edit, which a stamp taken here would miss.
		if found[0].Description == "" {
			t.Fatalf("%q returned a match with no description: %+v", probe.request, found[0])
		}
	}
}

func TestMatchReturnsNothingRatherThanTheNearestCraft(t *testing.T) {
	repo := matchRepo(t)
	for _, request := range []string{"hello", "", "   ", "xyzzy frobnicate"} {
		if found := repo.Match(request, DefaultMatches); len(found) != 0 {
			t.Fatalf("%q matched %s at %.2f", request, found[0].Name, found[0].Score)
		}
	}
}

func TestMatchRespectsKAndOrdersByScore(t *testing.T) {
	repo := matchRepo(t)
	found := repo.Match("presentation slides changelog invoice deck", 2)
	if len(found) != 2 {
		t.Fatalf("k=2 returned %d matches", len(found))
	}
	if found[0].Score < found[1].Score {
		t.Fatalf("matches are not best first: %.2f then %.2f", found[0].Score, found[1].Score)
	}
	if all := repo.Match("presentation slides changelog invoice deck", 0); len(all) == 0 {
		t.Fatal("the default k returned nothing")
	}
}

// A file that will not parse is not a candidate, and it does not take the rest
// of the catalogue down with it.
func TestMatchIgnoresACorruptFile(t *testing.T) {
	repo := matchRepo(t)
	if err := writeCorrupt(repo); err != nil {
		t.Fatalf("write: %v", err)
	}
	found := repo.Match("make me a presentation about the Q3 numbers", DefaultMatches)
	if len(found) == 0 || found[0].Name != "presentation" {
		t.Fatalf("a corrupt file broke retrieval: %+v", found)
	}
}
