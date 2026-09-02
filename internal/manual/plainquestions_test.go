package manual

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual/asked"
)

// THE FLOORS ON QUESTIONS A STRANGER ASKS.
//
// The questions themselves, and the law about never rewriting one to make it
// pass, are in internal/manual/asked — a package of their own because
// internal/e2e measures the SAME twenty-five and the same held-out set through
// a live model, and two copies of the list would let the free number and the
// paid one stop being about the same thing.

// reach ranks one question and reports whether a wanted page came first and
// whether it came at all, within the sections the belt tool actually hands the
// model. It logs every row so a failure is read as a table rather than as one
// name, because a change to the scoring moves the whole table at once.
func reach(t *testing.T, set []asked.Question) (first, within int) {
	t.Helper()
	for _, question := range set {
		found := Chat().Search(question.Ask, DefaultResults)
		pages := make([]string, 0, len(found))
		for _, section := range found {
			pages = append(pages, section.Page)
		}
		wanted := strings.Split(question.Want, ",")
		hit := func(depth int) bool {
			for i, page := range pages {
				if i >= depth {
					return false
				}
				for _, want := range wanted {
					if page == want {
						return true
					}
				}
			}
			return false
		}
		mark := "MISS"
		switch {
		case hit(1):
			first, within = first+1, within+1
			mark = "1st "
		case hit(len(pages)):
			within++
			mark = "top4"
		}
		t.Logf("%s %-52s want=%-38s got=%v", mark, question.Ask, question.Want, pages)
	}
	return first, within
}

// The floor is 20 and 25 because that is what #293 asked for; the measurement
// when this landed was 21 and 25, so there is one first place of slack and none
// at all on reaching the page. CI fails on a drop because a page that stops
// being reachable is a page the chat talks over the top of, and nothing else in
// the build notices.
func TestPlainQuestionsReachThePageThatAnswersThem(t *testing.T) {
	const (
		firstFloor  = 20
		withinFloor = 25
	)
	first, within := reach(t, asked.Plain)
	t.Logf("plain: first %d/%d · within top %d: %d/%d",
		first, len(asked.Plain), DefaultResults, within, len(asked.Plain))
	if first < firstFloor {
		t.Errorf("the right page came first %d times of %d; the floor is %d — write the asker's words into a `## ` heading on the page that answers them", first, len(asked.Plain), firstFloor)
	}
	if within < withinFloor {
		t.Errorf("the right page was among the %d sections handed to the model %d times of %d; the floor is %d", DefaultResults, within, len(asked.Plain), withinFloor)
	}
}

// Read the comment on [asked.HeldOut] before touching this. Fixing a failure
// here by editing a page for the question that failed defeats the only reason
// the set exists.
func TestHeldOutQuestionsReachTheirPage(t *testing.T) {
	const (
		firstFloor  = 7
		withinFloor = 17
	)
	first, within := reach(t, asked.HeldOut)
	t.Logf("held out: first %d/%d · within top %d: %d/%d",
		first, len(asked.HeldOut), DefaultResults, within, len(asked.HeldOut))
	if first < firstFloor {
		t.Errorf("a cold question landed its page first %d times of %d; the floor is %d — fix the scoring, not these questions", first, len(asked.HeldOut), firstFloor)
	}
	if within < withinFloor {
		t.Errorf("a cold question reached its page at all %d times of %d; the floor is %d — fix the scoring, not these questions", within, len(asked.HeldOut), withinFloor)
	}
}

// The four questions the issue singles out, asserted one at a time so a failure
// names the question rather than a count. Two of them are the ones where a
// wrong answer costs the person something real, and the other two are the most
// basic thing anybody asks about a program they have just met.
func TestTheQuestionsWhereAWrongPageCostsSomething(t *testing.T) {
	page := func(query string, n int) string {
		found := Chat().Search(query, DefaultResults)
		if n >= len(found) {
			return ""
		}
		return found[n].Page
	}
	reaches := func(query, want string) bool {
		for _, section := range Chat().Search(query, DefaultResults) {
			if section.Page == want {
				return true
			}
		}
		return false
	}

	for _, query := range []string{"who can see my files", "how do I let it run things without asking"} {
		if !reaches(query, "permissions") {
			t.Errorf("%q must reach the permissions page; it reached none of it", query)
		}
	}
	if first := page("what is aforge", 0); first != "what-i-can-do" && first != "starting-aforge" {
		t.Errorf(`"what is aforge" should answer from what-i-can-do or starting-aforge; it answered from %q`, first)
	}
	// "program" is the collision: a saved program is a shape of work, and the
	// person asking this means writing software. The general-purpose answer has
	// to win, or the product tells someone it is a thing it is not.
	if first := page("is this only for programming", 0); first != "what-i-can-do" {
		t.Errorf(`"is this only for programming" should answer from what-i-can-do; it answered from %q`, first)
	}
}
