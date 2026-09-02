package manual

import "testing"

// fakePages is a corpus of pages held in memory, for the invariants that are
// about how a page is indexed rather than about what any real page says.
type fakePages map[string]string

func (pages fakePages) Glob(string) ([]string, error) {
	names := make([]string, 0, len(pages))
	for name := range pages {
		names = append(names, name)
	}
	return names, nil
}

func (pages fakePages) ReadFile(name string) ([]byte, error) {
	return []byte(pages[name]), nil
}

// A page's `# ` title counts in every one of its sections, so it would be easy
// for it to count in document frequency once per section too — and then cutting
// one long section in half would move the IDF of the title's words for every
// question asked of the corpus, which is a page's shape deciding what its
// vocabulary is worth. It is counted once per page instead, and this pins that:
// the same page written as one section and as two indexes identically.
func TestSplittingASectionDoesNotMoveTheTitlesWeight(t *testing.T) {
	const title = "# The empty screen\n\n"
	whole := newCorpus(fakePages{
		"chat/empty-screen.md": title + "## What you see\n\nfirst half\n\nsecond half\n",
		"chat/other.md":        "# Something else\n\n## A heading\n\nthe screen is elsewhere\n",
	}, "chat/*.md")
	split := newCorpus(fakePages{
		"chat/empty-screen.md": title + "## What you see\n\nfirst half\n\n## And also\n\nsecond half\n",
		"chat/other.md":        "# Something else\n\n## A heading\n\nthe screen is elsewhere\n",
	}, "chat/*.md")
	whole.load()
	split.load()

	for _, word := range whole.pageTitle["empty-screen"] {
		if whole.documents[word] != split.documents[word] {
			t.Errorf("splitting a section moved %q from %d documents to %d; a page title is counted once per page",
				word, whole.documents[word], split.documents[word])
		}
	}
	if len(whole.pageTitle["empty-screen"]) == 0 {
		t.Fatal("the page title was not indexed at all")
	}
}

// The title is one statement about the page however many times it says a word.
func TestAPageTitleIsIndexedOnceForEachWordItUses(t *testing.T) {
	corpus := newCorpus(fakePages{
		"chat/keys.md": "# Keys, keys and more keys\n\n## A heading\n\nbody\n",
	}, "chat/*.md")
	corpus.load()
	if got := corpus.pageTitle["keys"]; len(got) != 2 || got[0] != "key" || got[1] != "more" {
		t.Fatalf("page title indexed as %v, want each word once", got)
	}
}
