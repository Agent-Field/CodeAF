package manual

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual/asked"
)

// THE QUERIES A MODEL ACTUALLY SENT — AND THE ONES THAT STILL MISS.
//
// The first `model` string in each group below was READ OFF THE WIRE —
// `deepseek/deepseek-v4-flash`, asked the `person` string beside it in a live
// conversation, called the `manual` tool with these words of its own (#307, and
// #309's own table). The rest are stand-ins. The wire's other two rewrites —
// "who can see my files privacy file access", "privacy files who can see my
// workspace" — reach the permissions page on their own since the section was
// reworded to carry the privacy and file-access terms, so they measure the
// corpus now rather than this file, and the rows carry phrasings of the same
// shape that still miss. Each one is checked twice: that it misses the page
// ALONE, so a passing row is the mechanism working rather than the corpus
// having been kind, and that it reaches the page once the person's sentence is
// read with it.
//
// A row that stops missing on its own is a row that has stopped measuring
// anything, and this file says so out loud rather than passing quietly.
var paraphrases = []struct{ person, model, want string }{
	// The scenario's own question, as internal/e2e asks it. The first row is
	// the wire's own rewrite that still misses; the two under it are the
	// stand-ins the header comment explains.
	{"who can see my files in aforge", "who can see my files when I use aforge", "permissions"},
	{"who can see my files in aforge", "who can view my files in aforge", "permissions"},
	{"who can see my files in aforge", "visibility of files in the workspace", "permissions"},
	// And the same rewrites against #293's bare wording, which reaches the page
	// second of four on its own — the thinnest margin there is, and both still
	// come back.
	{"who can see my files", "who can view my files in aforge", "permissions"},
	{"who can see my files", "visibility of files in the workspace", "permissions"},
}

// TestAParaphraseReachesThePageThePersonsWordsReach is #307. The model does not
// search what it was asked; it composes a query, and on a corpus this small two
// words nobody said drop the page out of the four the model is handed.
func TestAParaphraseReachesThePageThePersonsWordsReach(t *testing.T) {
	for _, row := range paraphrases {
		if reaches(Chat().Search(row.model, DefaultResults), row.want) {
			t.Errorf("%q now reaches %s on its own, so this row no longer measures the fix — replace it with a rewrite that still misses, or take it out",
				row.model, row.want)
			continue
		}
		found := Chat().SearchBoth(row.model, row.person, DefaultResults)
		if !reaches(found, row.want) {
			t.Errorf("the model asked %q while the person had asked %q, and the %s page still did not come back; what did was %v",
				row.model, row.person, row.want, pagesOf(found))
			continue
		}
		t.Logf("%-45q + %-34q → %v", row.model, row.person, pagesOf(found))
	}
}

// TestAQueryThatAlreadyReachesThePageStillDoes is the other half, and it is the
// half a mechanism like this fails at: a rewrite that was already right must not
// be spoiled by reading a second question beside it.
func TestAQueryThatAlreadyReachesThePageStillDoes(t *testing.T) {
	const (
		person = "who can see my files"
		// The one rewrite of the three in #307 that reached the page by itself.
		model = "who can see my files privacy"
		want  = "permissions"
	)
	alone := Chat().Search(model, DefaultResults)
	if !reaches(alone, want) {
		t.Fatalf("%q no longer reaches %s alone, so there is nothing here to keep: it came back %v", model, want, pagesOf(alone))
	}
	if found := Chat().SearchBoth(model, person, DefaultResults); !reaches(found, want) {
		t.Errorf("%q reached %s alone and lost it once %q was read beside it: %v", model, want, person, pagesOf(found))
	}
}

// TestOneQuestionAskedTwiceIsTheSameLookup holds the identity every caller
// depends on: the person's words ARE the query on the manual's own command line
// and in `/manual`, and a lookup that changed because the same sentence was
// handed over twice would be a second ranking nobody asked for.
func TestOneQuestionAskedTwiceIsTheSameLookup(t *testing.T) {
	for _, question := range asked.Plain {
		alone, twice := Chat().Search(question.Ask, DefaultResults), Chat().SearchBoth(question.Ask, question.Ask, DefaultResults)
		if strings.Join(labelsOf(alone), "|") != strings.Join(labelsOf(twice), "|") {
			t.Errorf("%q answered %v alone and %v asked of itself", question.Ask, labelsOf(alone), labelsOf(twice))
		}
	}
}

// TestASectionBothQuestionsReturnedComesFirst is the ranking rule itself. Two
// questions about the same thing agreeing on a section is stronger evidence than
// either one's number — and the numbers are not comparable across questions
// anyway, since a longer question scores every section higher — so agreement is
// settled before any score is looked at.
func TestASectionBothQuestionsReturnedComesFirst(t *testing.T) {
	for _, row := range paraphrases {
		mine := setOfLabels(labelsOf(Chat().Search(row.model, DefaultResults)))
		both := map[string]bool{}
		for _, label := range labelsOf(Chat().Search(row.person, DefaultResults)) {
			if mine[label] {
				both[label] = true
			}
		}
		if len(both) == 0 {
			t.Logf("%q and %q returned nothing in common; nothing to order here", row.model, row.person)
			continue
		}
		alone := ""
		for _, label := range labelsOf(Chat().SearchBoth(row.model, row.person, DefaultResults)) {
			switch {
			case !both[label]:
				alone = label
			case alone != "":
				t.Errorf("%q + %q: both questions returned %q and it came back behind %q, which only one of them returned",
					row.model, row.person, label, alone)
			}
		}
	}
}

// TestAPastedDocumentIsNotReadAsAQuestion holds [theirWordsCap]. A message that
// long is material, not a question, and the lookup falls back to exactly what it
// did before this file existed rather than being diluted by a document.
func TestAPastedDocumentIsNotReadAsAQuestion(t *testing.T) {
	const model = "who can view my files in aforge"
	paste := "who can see my files — here is the file, sorry it is long:\n" +
		strings.Repeat("the quarterly revenue figures for the northern region and the southern region\n", 200)
	if len(paste) <= theirWordsCap {
		t.Fatalf("the paste is %d bytes, inside the %d-byte cap: this test needs one past it", len(paste), theirWordsCap)
	}
	alone, pasted := labelsOf(Chat().Search(model, DefaultResults)), labelsOf(Chat().SearchBoth(model, paste, DefaultResults))
	if strings.Join(alone, "|") != strings.Join(pasted, "|") {
		t.Errorf("a %d-byte paste moved the lookup: %v alone, %v with the paste read beside it", len(paste), alone, pasted)
	}
}

// TestEveryQuestionThePagesWereWrittenForIsInsideTheCap keeps [theirWordsCap]'s
// own claim true. The cap is generous because a question is short; a set of
// questions that grew past it would have turned the mechanism off for exactly
// the asks it was built for, silently.
func TestEveryQuestionThePagesWereWrittenForIsInsideTheCap(t *testing.T) {
	longest := 0
	for _, set := range [][]asked.Question{asked.Plain, asked.HeldOut} {
		for _, question := range set {
			if len(question.Ask) > longest {
				longest = len(question.Ask)
			}
			if theirQuestion(question.Ask) == "" {
				t.Errorf("%q is %d bytes and would not be read as a question at all", question.Ask, len(question.Ask))
			}
		}
	}
	t.Logf("the longest question either set asks is %d bytes; the cap is %d", longest, theirWordsCap)
}

// ── small readers ───────────────────────────────────────────────────────────

func reaches(found []Section, page string) bool {
	for _, section := range found {
		if section.Page == page {
			return true
		}
	}
	return false
}

func pagesOf(found []Section) []string {
	out := make([]string, 0, len(found))
	for _, section := range found {
		out = append(out, section.Page)
	}
	return out
}

// labelsOf names a section the way a reader of a rendered result names one:
// its page and its heading, which together are its identity in the corpus.
func labelsOf(found []Section) []string {
	out := make([]string, 0, len(found))
	for _, section := range found {
		out = append(out, section.Page+" · "+section.Title)
	}
	return out
}

func setOfLabels(labels []string) map[string]bool {
	out := make(map[string]bool, len(labels))
	for _, label := range labels {
		out[label] = true
	}
	return out
}
