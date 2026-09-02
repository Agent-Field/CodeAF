package manual

import (
	"strings"
	"testing"
)

// QUESTIONS PHRASED BY SOMEONE WHO HAS NOT READ THE HEADINGS.
//
// This file is deliberately not the probe list in chat_test.go. That list is a
// ledger of everything the pages already answer, and it grew alongside them:
// its questions were written by the people writing the headings, so it measures
// the questions the pages were written for. It was green on 700+ probes while
// the twenty-five below reached their page first eleven times.
//
// So these are asked the other way round. Each one is a plain sentence about a
// topic, paired with the page whose NAME is that topic — no judgement calls, no
// phrasing lifted from a heading, nobody checking afterwards whether the page
// happens to use those words.
//
// THE LAW OF THIS FILE: a miss is fixed by making the page reachable, never by
// rewriting the question. Rewriting a question here turns the file into a
// second copy of the probe list, which is exactly the failure it exists to
// catch. Fix it in a `## ` heading written in the asker's own words, or in the
// scoring in corpus.go — and then check both floors below, because a heading
// that rescues one question can bury another.
var plainQuestions = []struct{ ask, want string }{
	{"how do I share a file with it", "attaching-files"},
	{"how do I check on it later", "keeping-an-eye"},
	{"what is aforge", "starting-aforge,what-i-can-do"},
	{"how do I get started", "getting-started"},
	{"what does it remember", "what-i-remember"},
	{"how do I set a spending limit", "models-and-cost,commands"},
	{"how do I keep it working after I close the lid", "staying-on-that-machine,keeping-an-eye"},
	{"who can see my files", "permissions"},
	{"how do I let it run things without asking", "permissions"},
	{"how do I split a job into parts", "tasks"},
	{"where does the finished work end up", "how-tasks-run"},
	{"how do I pick a different model", "models-and-cost,commands"},
	{"can I run it on another computer", "running-on-another-machine"},
	{"what do I do when it loses connection", "when-the-connection-drops"},
	{"how do I save a way of working and reuse it", "saved-shapes-of-work,saved-programs"},
	{"how do I make a rule it always follows", "standing-orders"},
	{"how do I choose which folder it works in", "choosing-a-folder"},
	{"what do all the keys do", "keys"},
	{"how do I see everything at once", "home"},
	{"the screen is blank", "empty-screen"},
	{"how do I undo something", "sessions-and-rewind"},
	{"it keeps summarizing the conversation", "compacting-over-and-over"},
	{"can it make a video", "making-pictures-audio-and-video"},
	{"how do I connect my mail account", "accounts"},
	{"what is on this task page", "reading-a-task-page"},
}

// heldOutQuestions were written cold, before a line of this change existed, and
// were not looked at again until it was finished. NOTHING IS EVER TUNED AGAINST
// THEM. Their whole worth is that no heading was written with them in view, so
// they measure what a stranger's first question actually meets; the moment one
// of them is answered by writing its words into a page, it stops measuring
// anything and this file is worse than it was.
//
// Their floor is therefore low, and is simply what they measured — a number to
// hold, not a number to chase. The gap between it and the floor above is the
// honest size of the difference between a question the pages were prepared for
// and a question they were not.
var heldOutQuestions = []struct{ ask, want string }{
	{"can I stop it from touching anything outside one folder", "permissions"},
	{"how much is this costing me", "models-and-cost"},
	{"how do I send it a photo", "attaching-files"},
	{"is there a list of shortcuts", "keys"},
	{"what happened while I was away", "keeping-an-eye"},
	{"why did it forget what we were talking about", "compacting-over-and-over,what-i-remember"},
	{"how do I open something on the other machine", "opening-files-from-that-machine"},
	{"I want it to always write in british english", "standing-orders"},
	{"can I go back to how it was before", "sessions-and-rewind"},
	{"where did it put the files it wrote", "places,how-tasks-run"},
	{"nothing is on the screen", "empty-screen"},
	{"how do I run several things at the same time", "tasks,how-tasks-run"},
	{"how do I use it from my phone", "asking-from-home"},
	{"what do I type to see the commands", "commands"},
	{"wifi went down did I lose everything", "when-the-connection-drops"},
	{"how do I hook up my calendar", "accounts"},
	{"does it work on a server I ssh into", "running-on-another-machine"},
	{"can it draw me a picture", "making-pictures-audio-and-video"},
	{"how do I make it repeat the same routine each time", "saved-programs,saved-shapes-of-work"},
	{"what is the first thing I should do after installing", "getting-started,starting-aforge"},
	{"will it keep going if I shut my laptop", "staying-on-that-machine"},
	{"how do I tell it which project to work on", "choosing-a-folder"},
}

// reach ranks one question and reports whether a wanted page came first and
// whether it came at all, within the sections the belt tool actually hands the
// model. It logs every row so a failure is read as a table rather than as one
// name, because a change to the scoring moves the whole table at once.
func reach(t *testing.T, set []struct{ ask, want string }) (first, within int) {
	t.Helper()
	for _, question := range set {
		found := Chat().Search(question.ask, DefaultResults)
		pages := make([]string, 0, len(found))
		for _, section := range found {
			pages = append(pages, section.Page)
		}
		wanted := strings.Split(question.want, ",")
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
		t.Logf("%s %-52s want=%-38s got=%v", mark, question.ask, question.want, pages)
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
	first, within := reach(t, plainQuestions)
	t.Logf("plain: first %d/%d · within top %d: %d/%d",
		first, len(plainQuestions), DefaultResults, within, len(plainQuestions))
	if first < firstFloor {
		t.Errorf("the right page came first %d times of %d; the floor is %d — write the asker's words into a `## ` heading on the page that answers them", first, len(plainQuestions), firstFloor)
	}
	if within < withinFloor {
		t.Errorf("the right page was among the %d sections handed to the model %d times of %d; the floor is %d", DefaultResults, within, len(plainQuestions), withinFloor)
	}
}

// Read the comment on heldOutQuestions before touching this. Fixing a failure
// here by editing a page for the question that failed defeats the only reason
// the set exists.
func TestHeldOutQuestionsReachTheirPage(t *testing.T) {
	const (
		firstFloor  = 7
		withinFloor = 17
	)
	first, within := reach(t, heldOutQuestions)
	t.Logf("held out: first %d/%d · within top %d: %d/%d",
		first, len(heldOutQuestions), DefaultResults, within, len(heldOutQuestions))
	if first < firstFloor {
		t.Errorf("a cold question landed its page first %d times of %d; the floor is %d — fix the scoring, not these questions", first, len(heldOutQuestions), firstFloor)
	}
	if within < withinFloor {
		t.Errorf("a cold question reached its page at all %d times of %d; the floor is %d — fix the scoring, not these questions", within, len(heldOutQuestions), withinFloor)
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
