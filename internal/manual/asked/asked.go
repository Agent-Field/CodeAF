// Package asked is the questions a STRANGER asks, and the page each one is
// about. It is test material and nothing in the shipped program imports it.
//
// It is a package of its own because two lanes have to measure the same
// questions or neither number means anything. internal/manual ranks them with
// no model in the loop, which is free and exact; internal/e2e puts them to a
// live model through the surface a person uses, which is what the model
// actually does with them (#307). A second copy of the list would let the two
// drift, and the day they drifted the comparison between them would quietly
// stop being a comparison.
package asked

// Question is one plain sentence about a topic and the page whose NAME is that
// topic. Want carries every page that fairly answers it, comma separated, and a
// question is reached when any of them comes back.
type Question struct{ Ask, Want string }

// Plain is TWENTY-FIVE QUESTIONS PHRASED BY SOMEONE WHO HAS NOT READ THE
// HEADINGS.
//
// This is deliberately not the probe list in internal/manual's chat_test.go.
// That list is a ledger of everything the pages already answer, and it grew
// alongside them: its questions were written by the people writing the
// headings, so it measures the questions the pages were written for. It was
// green on 700+ probes while the twenty-five below reached their page first
// eleven times.
//
// So these are asked the other way round. Each one is a plain sentence about a
// topic, paired with the page whose NAME is that topic — no judgement calls, no
// phrasing lifted from a heading, nobody checking afterwards whether the page
// happens to use those words.
//
// THE LAW OF THIS LIST: a miss is fixed by making the page reachable, never by
// rewriting the question. Rewriting a question here turns the list into a
// second copy of the probe list, which is exactly the failure it exists to
// catch. Fix it in a `## ` heading written in the asker's own words, or in the
// scoring in corpus.go — and then check both floors in plainquestions_test.go,
// because a heading that rescues one question can bury another.
var Plain = []Question{
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

// HeldOut were written cold, before a line of the ranking change that produced
// them existed, and were not looked at again until it was finished. NOTHING IS
// EVER TUNED AGAINST THEM. Their whole worth is that no heading was written
// with them in view, so they measure what a stranger's first question actually
// meets; the moment one of them is answered by writing its words into a page,
// it stops measuring anything and this list is worse than it was.
//
// Their floor is therefore low, and is simply what they measured — a number to
// hold, not a number to chase. The gap between it and the floor above is the
// honest size of the difference between a question the pages were prepared for
// and a question they were not.
var HeldOut = []Question{
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
