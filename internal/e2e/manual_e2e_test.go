//go:build e2e

// THE MANUAL, REACHED THE WAY A PERSON REACHES IT.
//
// WHAT THIS LANE PROVES. The manual is the only thing in this program that says
// what the program is, and issue #293 measured three ways it was unreachable: a
// person had no door onto it at all, a plain question did not arrive at the page
// that answers it, and a page asked for by name arrived whole and unbounded. The
// four scenarios below put each of those on the wire — three through a live
// conversation on a real model, one through the person's own command line — and
// assert on WHAT THE TOOL WAS ASKED AND WHAT IT HANDED BACK, never on the
// model's prose alone. A model saying "the manual says five" is exactly the
// claim this file exists to check, so every scenario names the tool call, the
// page label the result carried, and the figure the code itself owns.
//
// AND ALL OF THAT IS READ OFF THE JOURNAL ON DISK, never off the event stream:
// [session.Event]'s Output is a display copy capped at four thousand bytes, and
// a scenario that graded it would be grading the surface's own truncation
// ([manualCall] says what that cost the first cut of this file).
//
//   - a plain privacy question (#293 §2): "who can see my files" opens the
//     manual, a section comes back labelled `permissions`, and the reply carries
//     something only that page says.
//   - a whole page, bounded (#293 §5): the largest page comes back CUT under the
//     read tool's own byte cap, saying so and naming its sections, and the road
//     out of the cut is travelled — a section asked for by heading, whole.
//   - truth on the wire (#293 §3): how many models codeaf runs on its own behalf
//     is a number `internal/config` owns, and the answer a person is read has to
//     be that number.
//   - the person's own door (#293 §1): `codeaf manual` with NO key in the
//     environment, which costs nothing and calls no model.
//
// WHAT IT COSTS AND HOW IT IS PINNED. Every call rides
// deepseek/deepseek-v4-flash, because [pinEveryTextModel] writes that model into
// every row that can choose one. The pin is then CHECKED rather than assumed:
// each scenario reads the machine's own usage ledger back and fails if any other
// model answered. Three short conversations on that model measure in cents.
//
//	go test -tags e2e -count=1 -timeout 30m -v -run TestManual ./internal/e2e/
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// manualAttempts is the ask and two retries. Whether a model reaches for a
	// tool at all is the one part of a turn nobody can make deterministic, so an
	// ask that missed is put again, in the same words, from a fresh conversation.
	//
	// TWO RETRIES AND NOT ONE, and the second was bought by a measurement rather
	// than by a preference. The model does not search the person's words — it
	// composes a query of its own, and the corpus is fragile to the difference.
	// `Chat().Search` was measured directly, with no model in the loop, on
	// queries this model actually wrote for the privacy scenario:
	//
	//	who can see my files in codeaf              → permissions, ranked 1st
	//	who can see my files                        → permissions, ranked 3rd of 4
	//	who can see my files privacy file access    → MISS
	//	who can see my files when I use codeaf      → MISS
	//	privacy files who can see my workspace      → MISS
	//
	// Two words the person never said drop the page out of the four the model is
	// handed. That is issue #307 and it is the retrieval seam's to fix, not this
	// lane's: a paid end-to-end scenario that went red for a defect with its own
	// free deterministic test would be a second, worse detector of something
	// already detected. Three asks make these scenarios a guard against the page
	// becoming UNREACHABLE, which is the regression they are for. A third miss is
	// the finding, and it fails with the journal under it.
	manualAttempts = 3
	// manualCap is what one scenario may spend before this lane says something
	// is wrong with the RUN rather than with the manual. A two-turn conversation
	// on a cheap model is a fraction of a cent; a quarter of a dollar is a
	// runaway worth failing on.
	manualCap = 0.25
	// manualPrivacyPage is the page whose NAME is the topic of the privacy
	// question, which is what makes that question a fair one to grade: no
	// judgement call about which page ought to answer it.
	manualPrivacyPage = "permissions"
	// manualPrivacyQuestion is that question as a person asks it IN A
	// CONVERSATION, and the last two words are the whole difference between this
	// scenario measuring the manual and measuring something else.
	//
	// #293 §2 asks it as "who can see my files", and as a retrieval probe that is
	// exactly right. On a live surface it is not one question but two, and the
	// model answers the other one: measured on this model, a third of asks were
	// answered by running `ls`, reading the folder and reporting unix modes and
	// shell access — a fair reading of "my files" when you are sitting in a
	// workspace, and an answer about the person's disk rather than about codeaf.
	// Naming the product is what a person does when they mean the product, it is
	// lifted from no heading, and it is also where the corpus is strongest:
	// `Chat().Search` ranks permissions FIRST for these words and third of four
	// without them.
	manualPrivacyQuestion = "who can see my files in codeaf?"
	// manualLargestPage is the page the bound exists for — 205 KB of it, four
	// times what the read tool will hand over. Its size is asserted rather than
	// assumed, so this scenario cannot quietly become a test of a small page.
	manualLargestPage = "tasks"
)

// ── scenario 1: a plain question about privacy ──────────────────────────────

// TestManualAnswersAPlainPrivacyQuestion is #293 §2 on the wire. "Who can see my
// files" was one of the two questions the issue measured missing the permissions
// page entirely, and it is the pair where a wrong answer costs the person
// something real.
//
// TWO CLAIMS, IN ORDER. The manual was opened, and what came back carried a
// section labelled `permissions` — that is the issue's own criterion, read off
// the journal rather than off the model's account of itself. Then the reply is
// held to carrying something only the manual says ([readOffTheManual]), which is
// the difference between an answer read off a page and a fluent one.
//
// THE SECOND CLAIM IS ABOUT THE SECTIONS IT WAS HANDED AND NOT ABOUT THE
// PERMISSIONS ONE ALONE, and that was measured rather than chosen. This question
// returns four sections and more than one of them honestly answers it: a run
// whose reply quoted "nothing outside those two crosses" — the
// opening-files-from-that-machine section's own words, on no other page — was
// read off the manual by any fair reading, and failing it would have been
// grading which of four sections the search itself put in front of the model.
// What the manual owes the person is that the page was reached and the answer
// came off a page; which page the model then wrote from is the search's doing.
// The evidence is looked for in the permissions section FIRST, so a log line
// says when the answer came from the page whose name is the topic.
//
// WHAT A RED HERE MEANS, because there are three places it can come from and the
// failure line says which: the model never called the tool at all; it rewrote the
// question into a query of its own and that query did not reach the page; or it
// reached the page and answered past it.
func TestManualAnswersAPlainPrivacyQuestion(t *testing.T) {
	w := newManualWorld(t)
	runManualScenario(t, w, "plain privacy question", manualPrivacyQuestion,
		func(p *probe, out turn, calls []manualCall) {
			if len(calls) == 0 {
				p.missf("the model answered without opening the manual at all; the tools it called were %v", out.names())
				return
			}
			labelled := labelledSections(calls)
			body, found := sectionsFrom(labelled, manualPrivacyPage)
			if !found {
				p.missf("no manual result came from the %s page; what came back was labelled %v (#293 §2: this is the question that missed it)",
					manualPrivacyPage, labelsOf(labelled))
				return
			}
			t.Logf("  the manual answered from %v", labelsOf(labelled))
			evidence, page, grounded := readOffTheManual(out.Reply, labelled)
			if !grounded {
				p.missf("the reply carries nothing only the manual says, so it cannot be told from an invented one.\n  the distinctive words of the %s section it was handed: %v\n  the reply was: %s",
					manualPrivacyPage, sample(distinctiveWordsIn(body, manualPrivacyPage), 20), out.Reply)
				return
			}
			t.Logf("  the reply is read off the manual and not composed: from %s, it carries %s", page, evidence)
		})
}

// ── scenario 2: a whole page, bounded ───────────────────────────────────────

// TestManualBoundsAWholePageAndOpensItsSections is #293 §5. A page asked for by
// name used to arrive whole, and the largest is four times what the read tool
// will hand over — one lookup could spend a conversation's whole context. The
// bound is asserted against [bare.ResultByteCap] itself rather than a number
// written here, because two numbers for one bargain drift apart; and the cut is
// only half the claim, so the road out of it is travelled too.
func TestManualBoundsAWholePageAndOpensItsSections(t *testing.T) {
	whole, found := manual.Chat().Page(manualLargestPage)
	if !found {
		t.Fatalf("there is no manual page named %s", manualLargestPage)
	}
	if len(whole) <= bare.ResultByteCap {
		t.Fatalf("%s is %d bytes, inside the %d-byte cap: this scenario needs a page that a whole read cannot hold",
			manualLargestPage, len(whole), bare.ResultByteCap)
	}
	headings := headingsOf(manualLargestPage)
	t.Logf("%s is %d bytes in %d sections; the cap on one result is %d",
		manualLargestPage, len(whole), len(headings), bare.ResultByteCap)

	w := newManualWorld(t)
	runManualScenario(t, w, "a whole page, bounded",
		"show me the whole manual page about "+manualLargestPage,
		func(p *probe, out turn, calls []manualCall) {
			read, found := pageRead(calls, manualLargestPage)
			if !found {
				p.missf("no manual call asked for the %s page by name; the calls were %s", manualLargestPage, describeCalls(calls))
				return
			}

			// THE CUT ITSELF. Three claims, and the third is the one the issue
			// is about: a result that says it was cut but is still the whole
			// page would satisfy a reader and flood the conversation anyway.
			if !strings.Contains(read.Output, manualCutMark) {
				p.missf("the %s page came back with no cut notice in it (%d bytes)", manualLargestPage, len(read.Output))
			}
			if len(read.Output) > bare.ResultByteCap {
				p.missf("the %s page came back at %d bytes, past the %d-byte cap a tool result is held to",
					manualLargestPage, len(read.Output), bare.ResultByteCap)
			}
			named := headingsNamedIn(read.Output, headings)
			if len(named) < manualHeadingsWanted {
				p.missf("the cut named %d of the page's own headings, want at least %d — the rest of the page is only addressable if the notice says what to ask for: %v",
					len(named), manualHeadingsWanted, sample(named, 5))
			}
			t.Logf("  the %s page came back at %d bytes of %d, naming %d of its %d headings",
				manualLargestPage, len(read.Output), len(whole), len(named), len(headings))

			// THE ROAD OUT OF THE CUT. Either the model took it, or it told the
			// person the page is long and named the parts they can ask for.
			// Both are a person left able to reach the rest, which is the whole
			// of what the bound owes them, so either settles this.
			if asked, found := sectionRead(calls, manualLargestPage); found {
				checkSectionCameBackWhole(p, t, asked)
				return
			}
			if !saysItWasCut(out.Reply) || !offersTheRest(out.Reply, headings) {
				p.missf("the page was cut and nothing followed: no `section` call, and the reply neither says the page did not all arrive nor points at the parts the rest is in.\n  the reply was: %s", out.Reply)
				return
			}
			t.Logf("  no section call; the reply says the page was cut and points at its sections")
		})
}

// manualHeadingsWanted is how many of a page's own headings a cut has to name
// before the rest of the page counts as addressable. One would be a signpost
// pointing at a single room; three is the shortest list that reads as a list.
const manualHeadingsWanted = 3

// manualCutMark is the opening of the notice a cut result carries. It is matched
// on its stem rather than on the whole sentence because the byte figures inside
// it are the run's own and the wording after them is the bound lane's to change.
const manualCutMark = "[Cut: "

// checkSectionCameBackWhole holds the follow-up to the claim the cut made: the
// section it pointed at comes back, and comes back UNCUT and identical to what
// the corpus holds. A section that arrived trimmed would make the notice an
// invitation to a second truncation.
func checkSectionCameBackWhole(p *probe, t *testing.T, asked manualCall) {
	t.Logf("  the model followed the cut with section=%q", asked.Section)
	if strings.Contains(asked.Output, manualCutMark) {
		p.missf("the section %q came back cut; a section is the smallest thing the manual can be asked for and is the road out of the page's own cut", asked.Section)
		return
	}
	section, found := manual.Chat().Section(manualLargestPage, asked.Section)
	if !found {
		// The model invented a heading. That is a model's whim rather than a
		// finding about the manual, and the retry above is what it gets — but
		// the refusal it was handed still has to name what does exist.
		if !strings.Contains(asked.Output, "sections are") {
			p.missf("the model asked for a section %q the %s page does not have, and what came back does not say what it does have: %s",
				asked.Section, manualLargestPage, shorten(asked.Output, 300))
			return
		}
		p.missf("the model asked for a section %q the %s page does not have", asked.Section, manualLargestPage)
		return
	}
	if strings.TrimSpace(asked.Output) != strings.TrimSpace(section.Body) {
		p.missf("the section %q came back at %d bytes; the corpus holds %d — a section is returned whole or not at all",
			asked.Section, len(asked.Output), len(section.Body))
		return
	}
	t.Logf("  it came back whole: %d bytes, byte for byte what the corpus holds", len(asked.Output))
}

// saysItWasCut and offersTheRest are the second road out of a cut, and they are
// two questions rather than one because the cut owes the person two things: to
// know that what they were shown is a part, and to know how to ask for the rest.
// A reply with only the first leaves them stuck; a reply with only the second is
// offering to re-read something they think they already have.
//
// BOTH ARE READ GENEROUSLY, and deliberately. What is being graded is whether
// the person was told, not which of the ordinary words for it the model reached
// for — a measured reply says "it was cut at the section list because it's 207KB
// … want me to read a specific section?", which names no heading and is a
// complete answer. Naming one still counts, so a model that lists them passes on
// the stronger evidence.
func saysItWasCut(reply string) bool {
	return mentionsAny(reply, "cut", "long", "large", "truncat", "not the whole", "part of")
}

func offersTheRest(reply string, headings []string) bool {
	return len(headingsNamedIn(reply, headings)) > 0 || mentionsAny(reply, "section", "part", "heading", "rest of")
}

// mentionsAny is the one reading behind both, so the two cannot come to disagree
// about what counts as being told something.
func mentionsAny(text string, words ...string) bool {
	low := strings.ToLower(text)
	for _, word := range words {
		if strings.Contains(low, word) {
			return true
		}
	}
	return false
}

// ── scenario 3: truth on the wire ───────────────────────────────────────────

// TestManualQuotesTheCrewsRealSize is #293 §3, which is the failure class this
// whole lane exists for: a page that has stopped being true reads exactly like
// one that is, and the person asked precisely because they could not check. The
// count is derived from [crewroute.Seats], the router's own list of the seats a
// task runs on, so the day a seat is added or removed this test moves with the
// code and the manual is what turns red.
func TestManualQuotesTheCrewsRealSize(t *testing.T) {
	seats := len(crewroute.Seats)
	if seats == 0 {
		t.Fatalf("crewroute.Seats is empty")
	}
	t.Logf("internal/crewroute owns the figure: %d seats (%v)", seats, crewroute.Seats)

	w := newManualWorld(t)
	runManualScenario(t, w, "how many models the crew is",
		"how many models is a task's crew?",
		func(p *probe, out turn, calls []manualCall) {
			if len(calls) == 0 {
				p.missf("the model answered a question about codeaf out of memory: it called %v and never opened the manual", out.names())
				return
			}
			// NAMING THE SEATS IS SAYING HOW MANY OF THEM THERE ARE, and it is
			// the more checkable of the two: #293 §3's own worked example is two
			// pages that listed some seat names and omitted the one that pays
			// most of a task's bill, which a count alone would not have caught.
			// The words are the router's, not this file's.
			if named := seatsNamedIn(out.Reply, crewSeatLabels()); len(named) == seats {
				t.Logf("  the reply names all %d crew seats the router owns: %v", seats, named)
				return
			}
			said := countsClaimedAbout(out.Reply, crewNouns)
			if len(said) == 0 {
				p.missf("the reply names no number of models and no crew seat, so a person asking how many gets no answer.\n  the reply was: %s", out.Reply)
				return
			}
			t.Logf("  the reply claims %v", said)
			if !claimsCount(said, seats) {
				// THE EXACT FAILURE #293 §3 IS ABOUT. What the model was handed
				// is printed with it, because the whole question an autopsy asks
				// here is whether the page lied or the model did.
				p.missf("the reply never says %d: what it claims is %v.\n  the reply was: %s\n  what the manual handed it was:\n%s",
					seats, said, out.Reply, indent(manualResults(calls), "    "))
				return
			}
			t.Logf("  the reply says %d, which is what internal/crewroute says", seats)
		})
}

// crewNouns are the words a claim about the crew's size lands on. A number that
// is not next to one of these is a number about something else — how many
// rows the settings sheet has, how many pages the manual has — and grading it would be
// grading a sentence nobody asked about.
var crewNouns = []string{"model", "models", "seat", "seats", "tier", "tiers", "crew"}

// crewSeatLabels is the word each crew seat is KNOWN BY — "worker", "planner",
// "checker" — read off the router's own list. They are the router's because a
// seat's name is the router's to change, and a test that spelled them here
// would be the second copy that #293 §3 is about.
func crewSeatLabels() []string {
	labels := make([]string, 0, len(crewroute.Seats))
	for _, seat := range crewroute.Seats {
		labels = append(labels, string(seat))
	}
	return labels
}

// seatsNamedIn is which of those words a reply carries.
func seatsNamedIn(reply string, labels []string) []string {
	low := strings.ToLower(reply)
	var named []string
	for _, label := range labels {
		if strings.Contains(low, strings.ToLower(label)) {
			named = append(named, label)
		}
	}
	return named
}

// countClaim is one "<number> <noun>" a reply made, kept with the words it was
// said in so a failure quotes the reply rather than a parse of it.
type countClaim struct {
	said  string
	count int
}

// countsClaimedAbout finds every number a reply attached to one of the nouns.
// The window is one clause wide — a number and its noun separated by more than
// a few words are not the same claim — and "one" is skipped because "one model
// per class" is a sentence about the shape and not about the size.
//
// EVERY CLAIM IS COLLECTED AND THE RIGHT ONE IS LOOKED FOR AMONG THEM, rather
// than every claim being required to be the right one. A measured reply reads
// "Five. codeaf runs six model seats total: the one you talk to, and five it
// uses on its own behalf" — which answers the question exactly and counts the
// talk model in a second, true sentence. Failing that would be grading arithmetic
// the person did not ask for. What #293 §3 is about is a reply in which the
// figure the code owns never appears at all.
func countsClaimedAbout(reply string, nouns []string) []countClaim {
	number := `(two|three|four|five|six|seven|eight|nine|ten|\d{1,2})`
	noun := `(?:` + strings.Join(nouns, "|") + `)`
	gap := `[^.;:\n]{0,40}?`
	var out []countClaim
	// BOTH ORDERS, because a reply writes the count on either side of the thing
	// it counts: "five models" and "a crew of five" are one claim said twice.
	for _, shape := range []string{`(?i)\b` + number + `\b` + gap + `\b` + noun + `\b`,
		`(?i)\b` + noun + `\b` + gap + `\b` + number + `\b`} {
		for _, found := range regexp.MustCompile(shape).FindAllStringSubmatch(reply, -1) {
			count, ok := numberWord(found[1])
			if !ok {
				continue
			}
			out = append(out, countClaim{said: strings.TrimSpace(found[0]), count: count})
		}
	}
	return out
}

// numberWords is the small end of the counting numbers, which is the whole
// range a claim about a crew of seats can honestly fall in.
var numberWords = map[string]int{
	"two": 2, "three": 3, "four": 4, "five": 5,
	"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
}

// claimsCount answers whether any of a reply's claims is the figure the code
// owns.
func claimsCount(said []countClaim, want int) bool {
	for _, claim := range said {
		if claim.count == want {
			return true
		}
	}
	return false
}

func numberWord(word string) (int, bool) {
	if count, found := numberWords[strings.ToLower(word)]; found {
		return count, true
	}
	var count int
	if _, err := fmt.Sscanf(word, "%d", &count); err != nil || count < 2 || count > 10 {
		return 0, false
	}
	return count, true
}

// ── scenario 4: the person's own door ───────────────────────────────────────

// TestManualOpensWithNoKeyAndNoModel is #293 §1, and it is the only scenario
// here that spends nothing and calls nothing: that is the point of it. The
// questions people ask most are the ones they ask BEFORE there is a key to make
// a model call with, so the command line is driven with the key stripped out of
// the environment and a home directory that holds no profile at all.
func TestManualOpensWithNoKeyAndNoModel(t *testing.T) {
	codeaf := binary(t)
	home := t.TempDir()
	pages := manual.Chat().Pages()

	t.Run("the list", func(t *testing.T) {
		out, code := runManualCommand(t, codeaf, home)
		if code != 0 {
			t.Errorf("`codeaf manual` exited %d with no key; the list is what a person with nothing set up reads first:\n%s", code, out)
		}
		for _, page := range pages {
			if !strings.Contains(out, page) {
				t.Errorf("the list does not name the page %q", page)
			}
		}
		t.Logf("`codeaf manual` listed all %d pages, exit 0", len(pages))
	})

	t.Run("a page by name", func(t *testing.T) {
		want, found := manual.Chat().Page(manualPrivacyPage)
		if !found {
			t.Fatalf("there is no manual page named %s", manualPrivacyPage)
		}
		out, code := runManualCommand(t, codeaf, home, manualPrivacyPage)
		if code != 0 {
			t.Fatalf("`codeaf manual %s` exited %d:\n%s", manualPrivacyPage, code, shorten(out, 400))
		}
		// VERBATIM, and that is the whole claim of the person's door: a person
		// reading the manual here is reading the manual, not a retelling.
		if strings.TrimSpace(out) != strings.TrimSpace(want) {
			t.Errorf("`codeaf manual %s` printed %d bytes where the page is %d; the page is printed as it is written or not at all",
				manualPrivacyPage, len(strings.TrimSpace(out)), len(strings.TrimSpace(want)))
		}
		t.Logf("`codeaf manual %s` printed the page byte for byte, exit 0", manualPrivacyPage)
	})

	t.Run("a question in the person's own words", func(t *testing.T) {
		out, code := runManualCommand(t, codeaf, home, "who can see my files")
		if code != 0 {
			t.Fatalf("`codeaf manual \"who can see my files\"` exited %d:\n%s", code, shorten(out, 400))
		}
		// THE PERSON'S DOOR, NOT THE MODEL'S. `codeaf manual "…"` prints
		// [manual.RenderWhole], whose labels are Markdown headings
		// (`## permissions · …`). The belt tool still uses bracketed labels
		// ([manual.Render]); a test that looked for `[permissions · ` here
		// reported `[]` on a correct answer the day the person's door grew its
		// own spelling.
		label := manual.PersonSectionOpen(manualPrivacyPage)
		if !strings.Contains(out, label) {
			t.Errorf("nothing in the answer is labelled %s, so a person cannot trace a sentence back to the page that authorized it; what came back was labelled %v",
				label, labelsOf(renderedSections(out)))
		}
		t.Logf("the answer carries %s labels, exit 0", label)
	})

	t.Run("a page that does not exist", func(t *testing.T) {
		out, code := runManualCommand(t, codeaf, home, "no-such-page")
		if code == 0 {
			t.Errorf("`codeaf manual no-such-page` exited 0; a page asked for BY NAME and missing really did fail:\n%s", shorten(out, 400))
		}
		for _, page := range pages {
			if !strings.Contains(out, page) {
				t.Errorf("the refusal does not name the page %q that does exist", page)
			}
		}
		t.Logf("`codeaf manual no-such-page` exited %d and named all %d real pages", code, len(pages))
	})
}

// runManualCommand drives the built binary's manual door with NO key anywhere
// near it and a home directory holding nothing, so a pass is a pass for somebody
// who has not set codeaf up yet.
func runManualCommand(t *testing.T, codeaf, home string, args ...string) (string, int) {
	t.Helper()
	run := exec.Command(codeaf, append([]string{"manual"}, args...)...)
	run.Env = append(withoutKeys(os.Environ()), "CODEAF_HOME="+home, "CODEAF_PROFILE_DIR=")
	out, err := run.CombinedOutput()
	code := 0
	var exit *exec.ExitError
	if err != nil {
		if !asExitError(err, &exit) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	return string(out), code
}

// withoutKeys strips every credential this command must not need. The list is
// the provider keys a launch reads; anything left in would let a pass depend on
// the machine it ran on.
func withoutKeys(env []string) []string {
	stripped := make([]string, 0, len(env))
	for _, row := range env {
		name := row
		if at := strings.IndexByte(row, '='); at >= 0 {
			name = row[:at]
		}
		if strings.HasSuffix(name, "_API_KEY") || name == "CODEAF_HOME" || name == "CODEAF_PROFILE_DIR" {
			continue
		}
		stripped = append(stripped, row)
	}
	return stripped
}

func asExitError(err error, out **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*out = exit
	}
	return ok
}

// ── the throwaway machine, and one scenario's run ───────────────────────────

// newManualWorld is [newWorld] with every text row pinned, so a run of this file
// is one model's behaviour rather than a profile's.
func newManualWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	pinEveryTextModel(t)
	return w
}

// aPlainWorkspace is the ground a scenario is asked on: an ordinary empty folder
// with an ordinary name.
//
// THE NAME IS THE WHOLE OF WHY THIS FUNCTION EXISTS. [testing.T] names its temp
// directory after the test function, and a conversation opened straight on one
// was measured reading the path back to the person: "the directory name
// TestManualAnswersAPlainPrivacyQuestion… suggests this is inside an automated
// test runner. That test process can see everything here." A scenario whose own
// name tells the model it is being tested is measuring the harness.
//
// AND IT IS EMPTY, WHICH WAS ALSO MEASURED RATHER THAN ASSUMED. Ordinary working
// material was put in it first, on the reasoning that nobody asks who can see
// their files while standing in an empty folder. It made the scenario worse, and
// instructively so: the model read every file and then answered about THOSE
// files — unix modes, shell access, `chmod 600` — instead of about codeaf. The
// question is about the product, the material is a prompt to answer about the
// material, and this lane is not the place to discover which.
func aPlainWorkspace(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "quarterly-report")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make a workspace: %v", err)
	}
	return dir
}

// manualConfig is the departure this lane makes from the ambient one: nothing
// here is about standing orders, so the seam is off and there is no card for
// anybody to press. The person's own approval rules already allow `manual`.
func manualConfig(cfg *session.Config) {
	cfg.Standing = nil
	cfg.AskConsent = false
}

// probe collects what one attempt missed, so a scenario reports every fault of a
// turn at once rather than dying on the first.
type probe struct{ missed []string }

func (p *probe) missf(format string, args ...any) {
	p.missed = append(p.missed, fmt.Sprintf(format, args...))
}

// runManualScenario puts one question to a fresh conversation, grades the turn,
// and reports the wall time, the spend and which models answered. A first miss
// is a model's whim and gets the same words a second time; a second miss is the
// finding and fails with the journal under it.
func runManualScenario(t *testing.T, w *world, name, question string, check func(*probe, turn, []manualCall)) {
	t.Helper()
	for attempt := 1; attempt <= manualAttempts; attempt++ {
		started := time.Now()
		agent, place := w.open(aPlainWorkspace(t), manualConfig)
		out := w.say(agent, question, answerYes)
		wall := time.Since(started)
		usd, models := ledgerSince(t, started)

		found := &probe{}
		if out.Err != nil {
			found.missf("the turn ended in an error: %v", out.Err)
		}
		check(found, out, manualCalls(t, place))

		t.Logf("SCENARIO %s · attempt %d · wall %s · cost $%.6f · models %s · calls %v",
			name, attempt, wall.Round(time.Second), usd, strings.Join(models, ","), out.names())
		// THE PIN IS CHECKED AND NEVER ASSUMED. A second id in the ledger means
		// a row this file does not know about chose a model, and every figure
		// above would be about a mixture.
		for _, model := range models {
			if model != e2eModel {
				t.Errorf("a call rode %q; every model row in this run is pinned at %q", model, e2eModel)
			}
		}
		if usd > manualCap {
			t.Errorf("%s spent $%.4f, past the $%.2f a two-turn conversation costs on %s",
				name, usd, manualCap, e2eModel)
		}
		if len(found.missed) == 0 {
			return
		}
		if attempt == manualAttempts {
			for _, miss := range found.missed {
				t.Error(miss)
			}
			logManualJournal(t, agent, manualCalls(t, place))
			return
		}
		t.Logf("attempt %d missed — asking the same words once more, from a new conversation:\n  %s",
			attempt, strings.Join(found.missed, "\n  "))
	}
}

// logManualJournal is the autopsy a failure leaves behind: every tool call of
// the turn with its arguments and what came back, then the conversation as a
// reader of the transcript would see it. It is printed only on the failing
// attempt, because a passing run should not cost anybody a scroll.
func logManualJournal(t *testing.T, agent *session.Agent, calls []manualCall) {
	t.Helper()
	var journal strings.Builder
	for _, one := range calls {
		fmt.Fprintf(&journal, "\n  CALL manual %s (%d bytes back)\n    → %s", shorten(one.Args, 400), len(one.Output), shorten(one.Output, 2000))
	}
	if journal.Len() == 0 {
		journal.WriteString("\n  (no tool call at all)")
	}
	t.Logf("THE TURN'S JOURNAL:%s", journal.String())
	var entries strings.Builder
	for _, entry := range agent.Transcript() {
		fmt.Fprintf(&entries, "\n  [%s] %s", entry.Role, shorten(entry.Text, 400))
	}
	t.Logf("THE TRANSCRIPT:%s", entries.String())
}

// ── reading what the tool was asked and what it answered ────────────────────

// manualCall is one `manual` call as the JOURNAL holds it: the arguments the
// model sent, decoded, and the result that answered them WHOLE.
//
// IT IS READ OFF THE FILE AND NOT OFF THE EVENT STREAM, and that is the
// difference between this scenario measuring the bound and measuring nothing.
// [session.Event]'s Output is a DISPLAY COPY capped at four thousand bytes
// (loop.go's outputLimit), and so is a replayed transcript row's — the model
// reads the full text off the journal and only the journal. A first cut of this
// file asserted on the event and watched a 207 KB page arrive as 4022 bytes with
// no cut notice in it, which is what a display cap looks like from the outside
// and says nothing at all about what the tool returned.
type manualCall struct {
	Args    string
	Output  string
	Query   string `json:"query"`
	Page    string `json:"page"`
	Section string `json:"section"`
}

// journalLine is one record of transcript.jsonl, in the two shapes that matter
// here: an assistant message carrying tool calls, and the tool message that
// answers one of them by id. It is re-declared rather than imported because the
// record is unexported — which is the point of an end-to-end lane: what a reader
// outside the engine can see is the file.
type journalLine struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"toolCallId"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"toolCalls"`
}

// manualCalls is every `manual` call of a conversation, in the order the model
// made them, each with the result it was actually handed. Arguments that will
// not decode are kept rather than dropped: a call with garbled arguments is a
// fact about the turn, and a scenario that silently lost it would report "no
// call" for something that happened.
func manualCalls(t *testing.T, place session.Place) []manualCall {
	t.Helper()
	raw, err := os.ReadFile(place.Transcript())
	if err != nil {
		t.Fatalf("read the journal at %s: %v", place.Transcript(), err)
	}
	var order []manualCall
	byID := map[string]int{}
	results := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var one journalLine
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			continue
		}
		if one.Role == "tool" && one.ToolCallID != "" {
			results[one.ToolCallID] = one.Content
			continue
		}
		for _, called := range one.ToolCalls {
			if called.Function.Name != "manual" {
				continue
			}
			asked := manualCall{Args: called.Function.Arguments}
			_ = json.Unmarshal([]byte(called.Function.Arguments), &asked)
			byID[called.ID] = len(order)
			order = append(order, asked)
		}
	}
	for id, at := range byID {
		order[at].Output = results[id]
	}
	return order
}

// pageRead is the call that asked for a page BY NAME and not for one of its
// sections — the unbounded read #293 §5 is about.
func pageRead(calls []manualCall, page string) (manualCall, bool) {
	for _, one := range calls {
		if pageName(one.Page) == page && strings.TrimSpace(one.Section) == "" {
			return one, true
		}
	}
	return manualCall{}, false
}

// sectionRead is the call that took the road the cut offered.
func sectionRead(calls []manualCall, page string) (manualCall, bool) {
	for _, one := range calls {
		if pageName(one.Page) == page && strings.TrimSpace(one.Section) != "" {
			return one, true
		}
	}
	return manualCall{}, false
}

// pageName is how a page is asked for versus how it is filed: a model writes
// "tasks.md" as often as "tasks", and both name the same page.
func pageName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), ".md"))
}

// describeCalls is what a missing call's failure prints: which of the three
// shapes each call took, so an autopsy can tell a search from a page read.
func describeCalls(calls []manualCall) string {
	if len(calls) == 0 {
		return "(none)"
	}
	shapes := make([]string, 0, len(calls))
	for _, one := range calls {
		shapes = append(shapes, fmt.Sprintf("{query:%q page:%q section:%q}", one.Query, one.Page, one.Section))
	}
	return strings.Join(shapes, " ")
}

// manualResults is everything the manual handed the model this turn, for the
// one failure whose whole question is whether the page lied or the model did.
func manualResults(calls []manualCall) string {
	blocks := make([]string, 0, len(calls))
	for _, one := range calls {
		blocks = append(blocks, one.Output)
	}
	return strings.Join(blocks, "\n\n")
}

// labelled is one rendered section as the model was handed it: the page and
// heading it came from, and the text under them.
type labelled struct {
	Page  string
	Title string
	Body  string
}

// sectionLabel matches the label [manual.Render] writes over every section for
// the model. personSectionLabel is the Markdown heading [manual.RenderWhole]
// writes for a person on the command line. Both are anchored to the start of a
// line, because a body may well contain brackets, hashes and a middle dot of
// its own.
var (
	sectionLabel       = regexp.MustCompile(`(?m)^\[([a-z0-9][a-z0-9\-]*) · (.+)\]$`)
	personSectionLabel = regexp.MustCompile(`(?m)^## ([a-z0-9][a-z0-9\-]*) · (.+)$`)
)

// renderedSections reads a tool result — or a person's command-line answer —
// back into the sections it was built from. This is the assertion the whole
// lane rests on: the labels are how a person — and this test — traces a
// sentence to the page that authorized it. Both spellings are accepted because
// the belt tool and `codeaf manual "…"` share the corpus and disagree only on
// the label shape ([manual.PersonSectionOpen], [manual.ModelSectionOpen]).
func renderedSections(text string) []labelled {
	type hit struct {
		start, labelEnd, pageLo, pageHi, titleLo, titleHi int
	}
	var hits []hit
	for _, re := range []*regexp.Regexp{sectionLabel, personSectionLabel} {
		for _, span := range re.FindAllStringSubmatchIndex(text, -1) {
			hits = append(hits, hit{
				start: span[0], labelEnd: span[1],
				pageLo: span[2], pageHi: span[3],
				titleLo: span[4], titleHi: span[5],
			})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].start < hits[j].start })
	found := make([]labelled, 0, len(hits))
	for at, one := range hits {
		end := len(text)
		if at+1 < len(hits) {
			end = hits[at+1].start
		}
		found = append(found, labelled{
			Page:  text[one.pageLo:one.pageHi],
			Title: text[one.titleLo:one.titleHi],
			Body:  strings.TrimSpace(text[one.labelEnd:end]),
		})
	}
	return found
}

// labelledSections is every section every manual call of a turn handed back.
func labelledSections(calls []manualCall) []labelled {
	var found []labelled
	for _, one := range calls {
		found = append(found, renderedSections(one.Output)...)
	}
	return found
}

// sectionsFrom is the text of everything that came from one page, and whether
// anything did.
func sectionsFrom(sections []labelled, page string) (string, bool) {
	var blocks []string
	for _, one := range sections {
		if one.Page == page {
			blocks = append(blocks, one.Title+"\n"+one.Body)
		}
	}
	return strings.Join(blocks, "\n\n"), len(blocks) > 0
}

// labelsOf is the page · heading pairs a result carried, for a failure line.
func labelsOf(sections []labelled) []string {
	out := make([]string, 0, len(sections))
	for _, one := range sections {
		out = append(out, one.Page+" · "+shorten(one.Title, 50))
	}
	return out
}

// ── is the reply read off the page, or composed? ────────────────────────────

// groundedIn answers whether a reply carries something ONLY the named page says,
// and what that something was. A fluent answer about codeaf is indistinguishable
// from a remembered one — that is the sentence internal/manual's own header opens
// with — so a scenario that graded the reply for sounding right would be grading
// the exact thing the manual exists to stop anybody trusting.
//
// WHAT COUNTS AS EVIDENCE, AND WHY IT IS THREE THINGS AND NOT ONE. It was
// measured on this model rather than guessed at: over four real answers to this
// question, a run of the page's own words reached the reply twice, and the page's
// own quoted names reached it three times. A model does not quote a page it read,
// it retells it, so grading on a quotation alone would fail a good answer half
// the time and say nothing about the manual. Each of the three below is a thing
// the reply could not have arrived at except from this page, and any one settles
// it:
//
//   - A WORD no other page of the manual uses. The strongest and the rarest.
//   - A PHRASE — [manualPhraseWords] words in a row — that appears in what this
//     page handed over and nowhere else in the corpus. A retelling keeps the
//     order of a list even when it rewrites the sentence around it.
//   - The NAMES the page quotes, [manualNamesWanted] of them. A page that lists
//     `read`, `grep`, `find`, `ls` as the tools allowed without asking is
//     answered by a reply that names them, whatever prose it wraps them in, and a
//     model that never opened the page names a different set or none.
//
// readOffTheManual is [groundedIn] over everything one turn was handed: it tries
// the page whose name is the topic first, then every other page the search
// returned, and answers with the evidence and the page it came from.
func readOffTheManual(reply string, handed []labelled) (string, string, bool) {
	for _, page := range pagesReturned(handed) {
		body, found := sectionsFrom(handed, page)
		if !found {
			continue
		}
		if evidence, grounded := groundedIn(reply, body, page); grounded {
			return evidence, page, true
		}
	}
	return "", "", false
}

// pagesReturned is the pages a turn's manual results came from, the topic's own
// page first and the rest in the order the search ranked them, each once.
func pagesReturned(handed []labelled) []string {
	seen := map[string]bool{manualPrivacyPage: true}
	order := []string{manualPrivacyPage}
	for _, one := range handed {
		if seen[one.Page] {
			continue
		}
		seen[one.Page] = true
		order = append(order, one.Page)
	}
	return order
}

func groundedIn(reply, handed, page string) (string, bool) {
	said := plainWords(reply)
	if word, found := distinctiveWord(said, handed, page); found {
		return "the word " + strconv.Quote(word) + ", which no other page uses", true
	}
	if phrase, found := distinctivePhrase(said, handed, page); found {
		return "the phrase " + strconv.Quote(phrase) + ", which appears on no other page", true
	}
	if names, found := quotedNames(said, handed); found {
		return "the names it quotes — " + strings.Join(names, ", "), true
	}
	return "", false
}

// distinctiveWord is the first word of what the page handed over that the reply
// used and no other page of the manual contains.
func distinctiveWord(said []string, handed, page string) (string, bool) {
	using := setOf(said)
	for _, word := range distinctiveWordsIn(handed, page) {
		if using[word] {
			return word, true
		}
	}
	return "", false
}

// manualPhraseWords is how long a run has to be before its order is evidence
// rather than coincidence. Three content words in the page's own order, found
// nowhere else in a 36-page corpus, is a phrase somebody read.
const manualPhraseWords = 3

// distinctivePhrase is the first run of the handed text, of that length, that
// belongs to this page alone and reached the reply.
func distinctivePhrase(said []string, handed, page string) (string, bool) {
	elsewhere := phrasesElsewhere(page)
	inReply := runsOf(said, manualPhraseWords)
	for _, run := range orderedRunsOf(plainWords(handed), manualPhraseWords) {
		if !elsewhere[run] && inReply[run] {
			return run, true
		}
	}
	return "", false
}

// manualNamesWanted is how many of a page's quoted names a reply has to carry.
// One could be a coincidence of vocabulary — every page says `read` somewhere —
// but two of the same page's list, in a reply to the question that page answers,
// is that list arriving.
const manualNamesWanted = 2

// quotedNames is the backticked names of what the page handed over, and which of
// them the reply carries. Only bare identifiers count: a backticked SENTENCE is
// prose, and a path or a flag would be matched by the phrase reading above.
func quotedNames(said []string, handed string) ([]string, bool) {
	using := setOf(said)
	seen := map[string]bool{}
	var carried []string
	for _, found := range backtickedName.FindAllStringSubmatch(handed, -1) {
		name := strings.ToLower(found[1])
		if seen[name] || !using[name] {
			continue
		}
		seen[name] = true
		carried = append(carried, name)
	}
	return carried, len(carried) >= manualNamesWanted
}

// backtickedName matches one bare identifier between backticks — a tool name, a
// setting word — and nothing longer.
var backtickedName = regexp.MustCompile("`([A-Za-z][A-Za-z0-9_]{2,})`")

// distinctiveWordsIn is every word of what the model was handed that appears on
// no other page of the manual — the page's own vocabulary, in reading order so a
// failure prints something a person can scan.
func distinctiveWordsIn(handed, page string) []string {
	elsewhere := wordsElsewhere(page)
	seen := map[string]bool{}
	var found []string
	for _, word := range tokenize(handed) {
		if elsewhere[word] || seen[word] {
			continue
		}
		seen[word] = true
		found = append(found, word)
	}
	return found
}

// wordsElsewhere is the vocabulary of the whole manual EXCEPT one page, and
// phrasesElsewhere is its runs of [manualPhraseWords] words. Both are built once
// per page and kept: a scenario asks for them on every attempt and the corpus is
// a megabyte and a half.
func wordsElsewhere(page string) map[string]bool {
	return elsewhere(wordsCache, page, func(text string) []string { return tokenize(text) })
}

func phrasesElsewhere(page string) map[string]bool {
	return elsewhere(phrasesCache, page, func(text string) []string {
		return orderedRunsOf(plainWords(text), manualPhraseWords)
	})
}

// elsewhere is the one reading behind both: everything the OTHER pages contain,
// under whichever unit is asked for.
func elsewhere(cache map[string]map[string]bool, page string, unitsOf func(string) []string) map[string]bool {
	if found, cached := cache[page]; cached {
		return found
	}
	found := map[string]bool{}
	for _, other := range manual.Chat().Pages() {
		if other == page {
			continue
		}
		text, ok := manual.Chat().Page(other)
		if !ok {
			continue
		}
		for _, unit := range unitsOf(text) {
			found[unit] = true
		}
	}
	cache[page] = found
	return found
}

var (
	wordsCache   = map[string]map[string]bool{}
	phrasesCache = map[string]map[string]bool{}
)

// wordToken is what counts as a word for the vocabulary reading: it starts with
// a letter and is long enough to be a term rather than glue. Dots, dashes and
// underscores are inside it on purpose — `tools.approvalMode` and `--yolo` are
// exactly the things a grounded answer quotes, and splitting them would throw
// away the strongest evidence there is.
var wordToken = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_.\-]{3,}`)

// tokenize is that reading, lowercased and stripped of the punctuation a
// sentence leaves stuck to its last word.
func tokenize(text string) []string {
	found := wordToken.FindAllString(text, -1)
	out := make([]string, 0, len(found))
	for _, word := range found {
		word = strings.ToLower(strings.Trim(word, ".-_"))
		if len(word) >= 4 {
			out = append(out, word)
		}
	}
	return out
}

// plainWordToken is a run of letters and digits and nothing else. It is the
// reading the PHRASE side uses, because the manual is markdown and a reply is
// markdown of the model's own choosing: the same eight words are "**Reads of
// this machine** — `read`, `grep`, `find`, `ls`." on the page and "(`read`,
// `grep`, `find`, `ls`)" in the reply. Grading the asterisks would be grading
// which of the two wrote the emphasis, which is not what a quotation is.
var plainWordToken = regexp.MustCompile(`[a-z0-9]+`)

// plainWords is one text as its bare words, lowercased and in order.
func plainWords(text string) []string {
	return plainWordToken.FindAllString(strings.ToLower(text), -1)
}

// orderedRunsOf is every run of n words, in order and with repeats, which is
// what the handed side needs so a failure can name the FIRST phrase that would
// have counted. runsOf is the same thing as a set, for the side being searched.
func orderedRunsOf(words []string, n int) []string {
	if len(words) < n {
		return nil
	}
	out := make([]string, 0, len(words)-n+1)
	for at := 0; at+n <= len(words); at++ {
		out = append(out, strings.Join(words[at:at+n], " "))
	}
	return out
}

func runsOf(words []string, n int) map[string]bool {
	return setOf(orderedRunsOf(words, n))
}

func setOf(items []string) map[string]bool {
	found := make(map[string]bool, len(items))
	for _, item := range items {
		found[item] = true
	}
	return found
}

// ── a page's own headings ───────────────────────────────────────────────────

// headingsOf is one page's section titles, which is the whole of what a cut page
// has to offer: the names of the parts it can be asked for by.
func headingsOf(page string) []string {
	sections := manual.Chat().PageSections(page)
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		if title := strings.TrimSpace(section.Title); title != "" {
			out = append(out, title)
		}
	}
	return out
}

// headingsNamedIn is which of a page's headings a text names. The comparison is
// on the heading whole, because half a heading is not a name anybody can ask by.
func headingsNamedIn(text string, headings []string) []string {
	low := strings.ToLower(text)
	var found []string
	for _, heading := range headings {
		if strings.Contains(low, strings.ToLower(heading)) {
			found = append(found, heading)
		}
	}
	return found
}

// ── small readers ───────────────────────────────────────────────────────────

// sample is the first few of a list, with the tail counted rather than printed,
// so a failure line stays a line.
func sample(items []string, limit int) string {
	if len(items) <= limit {
		return fmt.Sprintf("%v", items)
	}
	return fmt.Sprintf("%v (… and %d more)", items[:limit], len(items)-limit)
}

// indent puts a block under a failure line without it reading as a new one.
func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for at, line := range lines {
		lines[at] = prefix + line
	}
	return strings.Join(lines, "\n")
}
