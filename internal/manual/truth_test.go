package manual

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE PAGES QUOTE FIGURES THE CODE OWNS, AND MARKDOWN CANNOT INTERPOLATE.
//
// Every other gate around this corpus asks whether a name is MENTIONED. None of
// them can see whether the sentence around the name is still true, which is how
// the crew grew a fifth seat while three pages went on saying four — including
// the page somebody reads on their first day, which quoted the first-run screen
// back at them with the wrong number in it. Nothing was red for it.
//
// This file is the interpolation Markdown does not have. A figure a page states
// is written here ONCE as a sentence with a hole in it, the hole is filled from
// the code that owns the figure at test time, and the page must contain the
// result. Move the constant and every page still carrying the old figure goes
// red, named, with the owner beside it — which is the whole of what a person
// fixing it needs to know.
//
// IT IS A TABLE AND NOT A TEST PER FACT. One bespoke test per number is how the
// last one of these ended up covering exactly one claim; a row is cheap enough
// that adding a figure to a page and adding it here are the same size of work.

// quotedFact is one figure the code owns and the pages repeat in prose.
type quotedFact struct {
	// fact names the figure in the words a failure should use.
	fact string
	// owner is the Go identifier the figure belongs to, named in the failure so
	// the fix is a lookup rather than a hunt.
	owner string
	// value is what that identifier says TODAY, read at test time.
	value string
	// others are the spellings value is not. A page matching one of this fact's
	// sentences around one of these is quoting a figure the code has moved past.
	// It is empty for a fact whose value is a name rather than a count: there is
	// no wrong spelling of a model id to go looking for, only a missing one.
	others []string
	// quotes is every place the fact is written out.
	quotes []quotedIn
}

// quotedIn is one such place: the chat page, and the sentence around the figure
// as a template with a single %s where the figure goes.
type quotedIn struct {
	page    string
	pattern string
}

// staleIn finds every page still carrying one of this fact's sentences around a
// figure the owner has moved past.
//
// It scans the WHOLE corpus rather than the pages listed above, because a
// sentence that wandered to a page nobody remembered to list is exactly the case
// a hand-kept list is worst at — and it is the case that shipped.
func (f quotedFact) staleIn(pages map[string]string) map[string]string {
	found := make(map[string]string)
	for _, quote := range f.quotes {
		for _, other := range f.others {
			stale := fmt.Sprintf(quote.pattern, other)
			for name, text := range pages {
				if strings.Contains(text, stale) {
					found[name] = stale
				}
			}
		}
	}
	return found
}

func TestEveryFigureAChatPageQuotesComesFromTheCodeThatOwnsIt(t *testing.T) {
	pages := flatChatPages(t)
	for _, fact := range quotedFacts(t) {
		for _, quote := range fact.quotes {
			text, ok := pages[quote.page]
			if !ok {
				t.Errorf("the chat corpus has no %s page, and %s is quoted there", quote.page, fact.fact)
				continue
			}
			if want := fmt.Sprintf(quote.pattern, fact.value); !strings.Contains(text, want) {
				t.Errorf("%s does not carry %s as %s says it: %q",
					quote.page, fact.fact, fact.owner, want)
			}
		}
		for page, stale := range fact.staleIn(pages) {
			t.Errorf("%s still says %q — %s moved %s to %q",
				page, stale, fact.owner, fact.fact, fact.value)
		}
	}
}

// quotedFacts is the hand-written half of the table. The generated half — the
// crew's own three by five — is appended by [crewFacts].
func quotedFacts(t *testing.T) []quotedFact {
	t.Helper()
	seats, notSeats := counted(len(config.ModelTiers))
	presets, notPresets := counted(len(config.CrewPresets))
	// The chooser reads one seat more than the crew has: the model you talk to,
	// which it shows and deliberately cannot move.
	chooser, notChooser := counted(len(config.ModelTiers) + 1)
	shortlist, notShortlist := counted(sourceNumber(t, "../session/taskmodel.go", "taskModelShortlist"))
	minutesWord, notMinutesWord := counted(int(standing.Interval / time.Minute))

	facts := []quotedFact{{
		// THE STOP'S BOUND IS A NAMED CONSTANT AND THE PAGES QUOTE IT, so a
		// ruling that moves the bound moves the manual in the same change
		// rather than leaving two pages promising a window that is gone.
		fact: "how long a stop waits before it detaches", owner: "tui3.stopGrace",
		value: strconv.Itoa(sourceNumber(t, "../tui3/app.go", "stopGrace")),
		quotes: []quotedIn{
			{"keys", "bounded at %s seconds"},
			{"screen", "bounded at %s seconds"},
		},
	}, {
		fact: "how many models the crew is", owner: "config.ModelTiers", value: seats, others: notSeats,
		quotes: []quotedIn{
			{"commands", "the %s models aforge uses on its own behalf"},
			{"commands", "The other %s — reflex, small work"},
			{"commands", "picking one puts all %s back"},
			{"commands", "sets the %s and confirms"},
			{"commands", "each of the %s classes funds"},
			{"commands", "of the %s classes are **select** rows"},
			{"getting-started", "the %s models aforge uses on its own behalf"},
			{"getting-started", "The crew is the %s class rows"},
			{"screen", "the preset the %s models aforge"},
			{"permissions", "one of the %s crew classes"},
			{"permissions", "set all %s at once"},
			{"models-and-cost", "worked out from the %s"},
			{"what-i-remember", "the cheapest of the %s crew classes"},
		},
	}, {
		fact: "how many crew presets there are", owner: "config.CrewPresets", value: presets, others: notPresets,
		quotes: []quotedIn{
			{"commands", "Then the %s presets"},
			{"getting-started", "draws the %s presets"},
			{"models-and-cost", "opens all %s as a chooser"},
			{"models-and-cost", "same near-free models in all %s"},
		},
	}, {
		fact: "how many seats the crew chooser reads", owner: "config.ModelTiers and the seat you talk to",
		value: chooser, others: notChooser,
		quotes: []quotedIn{
			{"commands", "aforge runs **%s model seats**"},
			{"commands", "the %s-seat reading"},
			{"commands", "reads all %s and sets the"},
		},
	}, {
		fact: "how long a shortlist of models is", owner: "session.taskModelShortlist",
		value: shortlist, others: notShortlist,
		quotes: []quotedIn{{"tasks", "matching more than %s ids is refused"}},
	}, {
		// The shipped default is a FLOATING ALIAS and the lanes page explains
		// what that costs a reader — so the page spells the id out, and a
		// default that moved would leave it explaining a name nobody is on.
		// There is no wrong spelling of a model id to hunt for, only a missing
		// one, so this row carries no others (see [quotedFact.others]).
		fact: "the shipped default model", owner: "config.DefaultModel",
		value:  config.DefaultModel,
		quotes: []quotedIn{{"lanes", "The model aforge ships with is spelled `%s`"}},
	}, {
		fact: "the shipped context fill", owner: "ctxbudget.DefaultFillPercent",
		value:  strconv.Itoa(ctxbudget.DefaultFillPercent),
		quotes: []quotedIn{{"compacting-over-and-over", "`%s%% of 1M (pinned)`"}},
	}, {
		// The answer room is spelled in threes on the page, which is how a
		// person reads a number and not how Go writes one, so the grouping is
		// done here rather than being a second spelling nobody would maintain.
		fact: "the answer room", owner: "ctxbudget.DefaultCompletionReserveTokens",
		value:  grouped(ctxbudget.DefaultCompletionReserveTokens),
		quotes: []quotedIn{{"compacting-over-and-over", "`answer room` setting, %s tokens"}},
	}, {
		fact: "the default task proposal window", owner: "config.DefaultTaskAutoApprove",
		value: strconv.Itoa(config.DefaultTaskAutoApprove), others: []string{"5"},
		quotes: []quotedIn{{"tasks", "The default window is %s seconds."}},
	}, {
		// `exec`'s two walls. They are the figures a person running one worker
		// without the screen plans a campaign around, and the page states both,
		// so both are answerable to the line the command's own help prints them
		// from.
		fact: "how many turns one worker gets", owner: "`aforge exec`'s --max-turns default",
		value:  strconv.Itoa(flagNumber(t, "../../cmd/aforge/exec.go", "max-turns")),
		quotes: []quotedIn{{"adaptive-runs", "it stops itself after %s turns"}},
	}, {
		// Spelled in threes on the page for the reason the answer room above is.
		// `--budget` was renamed `--token-budget`: *budget* is a word about
		// money everywhere else in this product, so a token count wearing it
		// read as dollars.
		fact: "how many tokens one worker gets", owner: "`aforge exec`'s --token-budget default",
		value:  grouped(flagNumber(t, "../../cmd/aforge/exec.go", "token-budget")),
		quotes: []quotedIn{{"adaptive-runs", "when the run has spent %s tokens"}},
	}, {
		// THE SPENDING TABLE IS SIX ROWS OF SHIPPED FIGURES, and four of them
		// are dollars the code owns. A page telling somebody what they are
		// allowed to spend is the last place a stale number is harmless: it is
		// read by a person deciding whether to raise a limit, and the figure
		// they are deciding about is the one on the page. The dollar sign lives
		// in the pattern rather than in the value, because it belongs to the
		// sentence and not to the constant.
		fact: "the day's shipped limit", owner: "config.DefaultDailyBudgetUSD",
		value: dollarsOwed(config.DefaultDailyBudgetUSD),
		quotes: []quotedIn{
			{"models-and-cost", "| **per day** | `$%s` |"},
			{"models-and-cost", "`$3.42 of $%s · resets at midnight`"},
		},
	}, {
		fact: "the figure a plan asks above", owner: "config.DefaultPlanConsentUSD",
		value:  dollarsOwed(config.DefaultPlanConsentUSD),
		quotes: []quotedIn{{"models-and-cost", "| **per plan** | `asks first above $%s` |"}},
	}, {
		fact: "what one firing may spend", owner: "standing.DefaultPerRunUSD",
		value:  dollarsOwed(standing.DefaultPerRunUSD),
		quotes: []quotedIn{{"models-and-cost", "| **per standing run** | `$%s a firing` |"}},
	}, {
		fact: "what practice may take of the day", owner: "config.DefaultPracticeBudgetUSD",
		value:  dollarsOwed(config.DefaultPracticeBudgetUSD),
		quotes: []quotedIn{{"models-and-cost", "| **practice** | `$%s of the day` |"}},
	}, {
		// The pass, which the page states in words because a person asking how
		// often their watch checks is not asking for a duration.
		fact: "how often everything standing is checked", owner: "standing.Interval",
		value: minutesWord, others: notMinutesWord,
		quotes: []quotedIn{{"keeping-an-eye", "Everything standing is checked every %s minutes."}},
	}, {
		// THE ↻ LINE'S WHY IS THE CODE'S WORD FOR THE BOUND, NOT THE PAGE'S.
		// That line said only how many turns it picked up, and the page showed
		// it that way; a reader learned what had ended the attempt from the ⏳
		// above it, which is adjacency and not a promise. The line now carries
		// the bound, read off the release reason, and the bound is named in
		// exactly one place in the code — so a page still showing the old shape,
		// or the old word for the bound, names itself here.
		fact: "what a leaf that spent its budget ran out of", owner: "exec.RanOutSubject(exec.StopBudget)",
		value: executor.RanOutSubject(executor.StopBudget),
		others: []string{
			executor.RanOutSubject(executor.StopTurnCap),
			executor.RanOutSubject(executor.StopDeadline),
			"its tokens",
		},
		quotes: []quotedIn{
			{"adaptive-runs", "picked up again from 9 recorded turns — it was still working when it ran out of %s"},
		},
	}}
	return append(facts, crewFacts(t)...)
}

// crewFacts is the crew table as the pages copy it out by hand: the chooser's
// line for each preset, and the models page's row for each seat.
//
// IT PINS THE MAPPING AND NOT ONLY THE IDS. A fact per id would go green on a
// page that had frugal's worker and max's worker the wrong way round, because
// both ids are on the page somewhere. So a fact is a whole LINE — every seat of
// one preset in order, or one seat across all three presets in order — built
// from [config.CrewModels] with the seat words the settings registry owns. Point
// a preset at a better model and the page still printing the old table names
// itself.
func crewFacts(t *testing.T) []quotedFact {
	t.Helper()
	seats := seatLabels(t)
	crews := make(map[string]map[string]string, len(config.CrewPresets))
	facts := make([]quotedFact, 0, 2*len(config.CrewPresets)+len(config.ModelTiers)+1)
	for _, preset := range config.CrewPresets {
		crews[preset], _ = config.CrewModels(preset)
		line := make([]string, 0, len(config.ModelTiers))
		for _, tier := range config.ModelTiers {
			line = append(line, seats[tier]+" "+crews[preset][tier])
		}
		facts = append(facts,
			quotedFact{
				fact: preset + "'s own line", owner: "config.CrewLine(" + strconv.Quote(preset) + ")",
				value:  config.CrewLine(preset),
				quotes: []quotedIn{{"commands", "%s"}, {"models-and-cost", "%s"}},
			},
			quotedFact{
				fact: preset + "'s row in the crew chooser", owner: "config.CrewModels(" + strconv.Quote(preset) + ")",
				value:  strings.Join(line, " · "),
				quotes: []quotedIn{{"commands", "%s"}},
			})
	}
	for _, tier := range config.ModelTiers {
		row := make([]string, 0, len(config.CrewPresets))
		for _, preset := range config.CrewPresets {
			row = append(row, "`"+baseModel(crews[preset][tier])+"`")
		}
		facts = append(facts, quotedFact{
			fact: "the " + tier + " seat across the presets", owner: "config.CrewModels",
			value:  "| " + seats[tier] + " | " + strings.Join(row, " | ") + " |",
			quotes: []quotedIn{{"models-and-cost", "%s"}},
		})
	}
	_, defaultMastermindEffort := roles.SplitEffort(config.DefaultMastermindModel)
	return append(facts, quotedFact{
		fact: "the shipped mastermind's thinking level", owner: "config.DefaultMastermindModel",
		value: defaultMastermindEffort,
		quotes: []quotedIn{{
			"models-and-cost", "shipped **mastermind** carries `:%s`",
		}},
	}, quotedFact{
		fact: "the line /crew max confirms with", owner: "config.CrewSummary after config.ApplyCrew",
		value:  crewConfirmLine(t, config.CrewMax),
		quotes: []quotedIn{{"commands", "%s"}, {"models-and-cost", "%s"}},
	}, quotedFact{
		// THE SAME THREE SEATS WITHOUT THE CONFIRM LINE'S OWN PREFIX, which is
		// how /status prints them and how both pages print /status back at the
		// reader. It is a second row rather than a second pattern on the one
		// above because the prefix differs — `crew → ` where the command
		// confirms, a padded label where the reading is listed — and the four
		// copies of this line drifted onto a worker the max crew has not used
		// since the presets moved, with nothing red for it.
		fact: "max's three classes as /status lists them", owner: "config.CrewClasses under the max crew",
		value:  strings.TrimPrefix(crewConfirmLine(t, config.CrewMax), "crew → "),
		quotes: []quotedIn{{"commands", "crew %s"}, {"models-and-cost", "crew %s"}},
	})
}

// crewConfirmLine is the sentence a person reads after /crew, taken from the
// function that builds it rather than rebuilt here: a gate that spelled the line
// itself would go on passing through a change to the words or the order, which
// is the drift it exists to catch.
func crewConfirmLine(t *testing.T, preset string) string {
	t.Helper()
	dir := t.TempDir()
	if err := config.ApplyCrew(dir, preset); err != nil {
		t.Fatalf("the %s crew would not apply: %v", preset, err)
	}
	return config.CrewSummary(dir)
}

// seatLabels is the word each crew seat wears on a settings surface, read from
// the registry that owns it. The chooser and the models page both print these
// beside the ids, so a gate on a line of ids has to spell them the same way.
func seatLabels(t *testing.T) map[string]string {
	t.Helper()
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()})
	labels := make(map[string]string, len(config.ModelTiers))
	for _, tier := range config.ModelTiers {
		row, ok := registry.Row(seatKey(tier))
		if !ok {
			// A seat with no settings row is worth saying on its own, but it must
			// not stop the pages being read: the run that adds a seat is exactly
			// the run that needs the list of pages now behind.
			t.Errorf("the settings registry has no %s row for the %s seat", seatKey(tier), tier)
			labels[tier] = tier
			continue
		}
		labels[tier] = row.Label
	}
	return labels
}

// seatKey is one seat's settings row, which is also the word the pages print.
func seatKey(tier string) string { return "models.tiers." + tier }

// EVERY SEAT LIST NAMES EVERY SEAT.
//
// Two pages tell a person which settings rows the crew is, by writing the rows
// out. A list like that has no number in it to go wrong — it goes wrong by being
// short, which reads as complete, and the seat it lost was the one that pays
// most of a task's bill. So the list is checked against the seats themselves:
// any page that names one row of the crew must name all of them.
func TestEverySeatListInTheChatManualNamesEverySeat(t *testing.T) {
	for name, text := range flatChatPages(t) {
		if !strings.Contains(text, "models.tiers.") {
			continue
		}
		for _, tier := range config.ModelTiers {
			if !strings.Contains(text, seatKey(tier)) {
				t.Errorf("%s writes the crew's settings rows out and leaves %s off — config.ModelTiers has %d seats",
					name, seatKey(tier), len(config.ModelTiers))
			}
		}
	}
}

// A PAGE QUOTING A SCREEN QUOTES IT, WORD FOR WORD.
//
// Several pages print a shipped sentence back at the reader, which is the right
// thing to do — somebody comparing the page to the screen in front of them
// should find the same words there. Two of them shipped paraphrased instead, and
// both paraphrases carried the wrong number: the page said four where the screen
// said five. So the sentence is read out of the Go source that owns it and the
// page must contain it whole. internal/tui3 cannot be imported here — it imports
// this package — so its source is read as text, which is what the gates around
// this corpus already do to the packages they sit underneath.
func TestEveryPageQuotingAShippedSentenceQuotesItWhole(t *testing.T) {
	pages := flatChatPages(t)
	for _, quoted := range []struct{ page, file, name string }{
		{"getting-started", "../tui3/onboarding.go", "controlLimitWord"},
		{"getting-started", "../tui3/onboarding.go", "controlModelWord"},
		{"getting-started", "../tui3/onboarding.go", "controlCrewWord"},
		{"commands", "../tui3/crew.go", "crewScopeLine"},
		{"commands", "../tui3/crew.go", "crewPinLine"},
		{"commands", "../tui3/crew.go", "crewCustomLine"},
		{"models-and-cost", "../tui3/crew.go", "crewScopeLine"},
	} {
		onScreen := sourceString(t, quoted.file, quoted.name)
		if !strings.Contains(pages[quoted.page], onScreen) {
			t.Errorf("%s does not quote %s whole. The screen says:\n\t%s",
				quoted.page, quoted.name, onScreen)
		}
	}
}

// THE COMMAND LIST AND THE MANUAL COUNT THE SAME SEATS.
//
// The row a person reads on /help and the page they read afterwards are two
// hand-written sentences about one table, and the row is the one nothing was
// checking. Both are compared to config here rather than to each other, so the
// failure names the figure rather than only saying they differ.
func TestTheCrewCommandRowCountsTheSeatsConfigOwns(t *testing.T) {
	seats, notSeats := counted(len(config.ModelTiers))
	rows := strings.Join(crewCommandRows(t), "\n")
	if rows == "" {
		t.Fatal("internal/tui3/commands.go offers no /crew row")
	}
	// The two sentences the rows say the count in, as [quotedIn] writes them:
	// a hole where the figure goes, filled from config rather than from either
	// sentence. A reword that loses one of them fails here too, loudly, which is
	// the right answer for a row that has stopped saying how many seats there are.
	for _, pattern := range []string{"the %s models aforge uses", "set the %s to"} {
		if want := fmt.Sprintf(pattern, seats); !strings.Contains(rows, want) {
			t.Errorf("no /crew row says %q — config.ModelTiers has %d seats:\n%s",
				want, len(config.ModelTiers), rows)
		}
		for _, other := range notSeats {
			if stale := fmt.Sprintf(pattern, other); strings.Contains(rows, stale) {
				t.Errorf("a /crew row still says %q — config.ModelTiers has %d seats, spelled %q",
					stale, len(config.ModelTiers), seats)
			}
		}
	}
}

// crewCommandRows is every /crew row in the command list, read as text.
var crewRowLine = regexp.MustCompile(`(?m)^.*\{name: "crew".*$`)

func crewCommandRows(t *testing.T) []string {
	t.Helper()
	return crewRowLine.FindAllString(sourceText(t, "../tui3/commands.go"), -1)
}

// THE FLOOR AND THE PAGE ABOUT IT AGREE, INCLUDING ABOUT ITS SIZE.
//
// internal/approval names the calls a blanket allow cannot switch off, and the
// permissions page tells a person the same names and says how many there are. A
// fourth entry in that table without its sentence here would leave the page
// telling somebody a message goes out silently when it does not — the one
// mistake a page about permissions must never make. The names come from the
// table itself ([approval.ActsInThePersonsNameTools]) rather than being repeated
// here, because a list repeated in a test is the same list going stale twice.
func TestThePermissionsPageNamesEveryToolTheFloorHolds(t *testing.T) {
	page := flatChatPages(t)["permissions"]
	tools := approval.ActsInThePersonsNameTools()
	if len(tools) == 0 {
		t.Fatal("internal/approval names no call that acts in the person's name")
	}
	for _, tool := range tools {
		if !approval.AlwaysAsks(tool, nil) {
			t.Errorf("%s is in approval's table and the floor does not hold it", tool)
		}
		if !strings.Contains(page, "`"+tool+"`") {
			t.Errorf("the permissions page does not name %s, which the floor always asks about", tool)
		}
	}
	if count, _ := counted(len(tools)); !strings.Contains(page, "exactly "+count+" tools") {
		t.Errorf("the permissions page does not say the floor is exactly %s tools — internal/approval names %d",
			count, len(tools))
	}
}

// EVERY WORD THE SKETCH IS TAUGHT IS A WORD THE PAGE EXPLAINS.
//
// The long-answer road turns on one line a second model writes, and the ask that
// asks for it names the tokens it will accept — `(done)` for nothing left,
// `(waiting)` for nothing left but waiting on work already handed out. A token
// the ask teaches and the page does not is a person reading a word on a card
// that the manual has no account of, and a token that is renamed leaves the page
// explaining a word nothing says any more. So the list is read out of the ask
// itself rather than written here: a fourth token cannot arrive undocumented.
func TestEverySketchTokenTheAskTeachesIsOnTheTasksPage(t *testing.T) {
	ask := sourceString(t, "../session/checkpoint.go", "checkpointSketchAsk")
	tokens := regexp.MustCompile(`'(\([a-z]+\))'`).FindAllStringSubmatch(ask, -1)
	if len(tokens) == 0 {
		t.Fatal("the checkpoint's sketch ask names no token at all")
	}
	page := flatChatPages(t)["tasks"]
	for _, token := range tokens {
		if !strings.Contains(page, "`"+token[1]+"`") {
			t.Errorf("the tasks page does not explain %s, which session.checkpointSketchAsk teaches the reader to answer",
				token[1])
		}
	}
}

// ONE LOOKUP IS ONE SIZE, WHEREVER IT IS ASKED FOR.
//
// [DefaultResults] and [SectionBodyCap] are this package's own account of how
// much of the manual one question is answered from, and the belt restates the
// first of them as a constant of its own rather than importing it. Two numbers
// for one bargain drift, and the drift is invisible: the tool would simply hand
// the model more or less than the pages were written to be read in.
func TestOneLookupIsTheSizeThisPackageSaysItIs(t *testing.T) {
	if belt := sourceNumber(t, "../session/tools_manual.go", "manualSections"); belt != DefaultResults {
		t.Errorf("the belt answers a question from %d sections and manual.DefaultResults is %d", belt, DefaultResults)
	}
	long := Section{Page: "p", Title: "t", Body: strings.Repeat("x", SectionBodyCap+10)}
	rendered := Render([]Section{long})
	if !strings.HasSuffix(rendered, "…") || len(rendered) > SectionBodyCap+64 {
		t.Errorf("a section over SectionBodyCap (%d) rendered as %d bytes, not cut and marked", SectionBodyCap, len(rendered))
	}
}

// ── reading a figure off the code that owns it ──────────────────────────────

// blockquoteMark is a Markdown quote marker at the head of a line.
var blockquoteMark = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)

// flatChatPages is every chat page with its line breaks taken out.
//
// A sentence in a page is wrapped wherever the column ran out, so a gate reading
// the raw text would be a gate on where somebody's editor broke the line. Quote
// markers go with the wrapping for the same reason: a page quoting a screen's
// own words back at the reader has to be checkable against that string.
func flatChatPages(t *testing.T) map[string]string {
	t.Helper()
	names := Chat().Pages()
	pages := make(map[string]string, len(names))
	for _, name := range names {
		text, ok := Chat().Page(name)
		if !ok {
			t.Fatalf("the chat corpus lists %s and cannot open it", name)
		}
		pages[name] = flatten(blockquoteMark.ReplaceAllString(text, ""))
	}
	return pages
}

// flatten is one run of whitespace, one space.
func flatten(text string) string { return strings.Join(strings.Fields(text), " ") }

// sourceText is a Go file this package sits underneath, read as data. It is how
// a gate reaches a figure owned by a package that imports this one.
func sourceText(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	return string(text)
}

// sourceNumber is one `name = 123` constant out of such a file.
func sourceNumber(t *testing.T, path, name string) int {
	t.Helper()
	match := regexp.MustCompile(`(?m)^\s*(?:const\s+)?` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)`).FindStringSubmatch(sourceText(t, path))
	if match == nil {
		t.Fatalf("%s no longer holds a number called %s", path, name)
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("%s = %q is not a number", name, match[1])
	}
	return value
}

// flagNumber is the default of one command-line flag, read out of the file that
// declares it. It exists beside [sourceNumber] because a figure is not always a
// named constant: a command's walls are often written as literals in the flag
// declaration itself — `flags.Int("turns", 200, …)` — and that literal is both
// what the command's own help prints and what a page quoting it must agree with.
//
// IT READS EVERY SHAPE A NUMERIC FLAG IS DECLARED IN, and that is the whole
// reason this comment is longer than the function. A flag that moves to
// cmd/aforge's [newCountFlag] — the value that refuses a bad count with a
// sentence about the flag instead of [strconv]'s `parse error` — keeps exactly
// the same default and stops being `flags.Int` while it does it. A reader that
// knew only the one shape went RED with a message about a flag that had not
// changed, and the day somebody "fixed" that by exempting the flag, every
// figure declared that way would have gone invisible to this gate — silently,
// which is the failure mode this whole file exists to prevent. `exec`'s two
// walls and `logs --tail` are all declared that way now. A shape nobody
// has taught it is still a hard failure, and that is deliberate.
func flagNumber(t *testing.T, path, name string) int {
	t.Helper()
	quoted := regexp.QuoteMeta(name)
	shapes := []string{
		// flags.Int("max-turns", 200, …)
		`Int\(\s*"` + quoted + `"\s*,\s*(\d+)`,
		// newCountFlag(flags, "max-turns", 200, …)
		`newCountFlag\([^,]+,\s*"` + quoted + `"\s*,\s*(\d+)`,
	}
	for _, shape := range shapes {
		if match := regexp.MustCompile(shape).FindStringSubmatch(sourceText(t, path)); match != nil {
			value, err := strconv.Atoi(match[1])
			if err != nil {
				t.Fatalf("--%s's default = %q is not a number", name, match[1])
			}
			return value
		}
	}
	t.Fatalf("%s no longer declares a --%s flag with a number for its default, in any shape this reads "+
		"(flags.Int, newCountFlag) — teach it the new shape rather than dropping the flag, "+
		"or the figure the manual quotes stops being checked at all", path, name)
	return 0
}

// sourceString is one `name = "…" + "…"` string constant out of such a file,
// joined back into the sentence it is on screen.
func sourceString(t *testing.T, path, name string) string {
	t.Helper()
	match := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(name) + `\s*=\s*((?:\s*"[^"]*"\s*\+)*\s*"[^"]*")`).
		FindStringSubmatch(sourceText(t, path))
	if match == nil {
		t.Fatalf("%s no longer holds a string called %s", path, name)
	}
	var built strings.Builder
	for _, piece := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(match[1], -1) {
		built.WriteString(piece[1])
	}
	return flatten(built.String())
}

// countWords is how a page spells a count. Prose writes a small number as a
// word, so a gate on a figure in prose has to be able to write it that way too.
var countWords = []string{"zero", "one", "two", "three", "four", "five", "six",
	"seven", "eight", "nine", "ten", "eleven", "twelve"}

// counted answers the word for n and every word it is not. The second half is
// what turns a moved constant into a named failure on the page still saying the
// figure it used to be.
func counted(n int) (string, []string) {
	if n < 0 || n >= len(countWords) {
		return strconv.Itoa(n), nil
	}
	others := make([]string, 0, len(countWords)-1)
	for word := range countWords {
		if word != n {
			others = append(others, countWords[word])
		}
	}
	return countWords[n], others
}

// dollarsOwed is a shipped limit as a page writes it: the digits alone, with the
// dollar sign left to the sentence around it. Every one of these is a whole
// number of dollars and a page that printed `$500.00` at somebody would be a
// page inventing a precision the setting does not have, so the trailing zeroes
// go rather than being formatted back in.
func dollarsOwed(usd float64) string {
	return strconv.FormatFloat(usd, 'f', -1, 64)
}

// grouped writes a number in threes, the way a page does: 65,536.
func grouped(n int) string {
	digits := strconv.Itoa(n)
	var out strings.Builder
	for at, digit := range digits {
		if at > 0 && (len(digits)-at)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return out.String()
}

// baseModel is a model id without its vendor prefix, which is the spelling the
// tables on the models page use. It is config.shortModel's rule, minus the
// empty-value word that surface needs and a table does not.
func baseModel(id string) string {
	if at := strings.LastIndex(id, "/"); at >= 0 {
		return id[at+1:]
	}
	return id
}
