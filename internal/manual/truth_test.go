package manual

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/ctxbudget"
	"github.com/Agent-Field/codeaf/internal/effort"
	executor "github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/standing"
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
	seats, notSeats := counted(len(crewroute.Seats))
	decay, notDecay := counted(sourceNumber(t, "../router/crew.go", "redoDecayAfter"))
	steps, notSteps := counted(sourceNumber(t, "../router/crew.go", "redoOffsetCeiling"))
	shortlist, notShortlist := counted(sourceNumber(t, "../session/taskmodel.go", "taskModelShortlist"))
	minutesWord, notMinutesWord := counted(int(standing.Interval / time.Minute))
	ceilingTimes, otherCeilingTimes := counted(int(config.CrewCheckCeilingTimes))
	taskDollars := config.CrewTaskMoney(config.CrewTaskCapDefault)
	var otherTaskDollars []string
	for amount := 1; amount <= 10; amount++ {
		word := config.CrewTaskMoney(float64(amount))
		if word != taskDollars {
			otherTaskDollars = append(otherTaskDollars, word)
		}
	}
	ceilingFloor := fmt.Sprintf("$%.2f", config.CrewCheckCeilingFloor)
	var otherCeilingFloors []string
	for cents := 1; cents <= 10; cents++ {
		word := fmt.Sprintf("$0.%02d", cents)
		if word != ceilingFloor {
			otherCeilingFloors = append(otherCeilingFloors, word)
		}
	}

	facts := []quotedFact{{
		// THE THINKING WALK HAS ONE OWNER for both surface doors and every page
		// that teaches it, so adding a rung cannot leave either door behind.
		fact: "what the picker's ctrl+t and a task's thinking control walk", owner: "config.EffortChoices",
		value:  strings.Join(config.EffortChoices, " → ") + " → " + config.EffortChoices[0],
		others: []string{"off → low → medium → high → off"},
		quotes: []quotedIn{
			{"commands", "`%s`"},
			{"models-and-cost", "`%s`"},
			{"task-controls", "`%s`"},
		},
	}, {
		// The settings row's choices are the public spelling of the ladder, so
		// the page must read them from the same list the row does.
		fact: "the words the thinking row offers", owner: "config.EffortChoices",
		value:  strings.Join(config.EffortChoices, ", "),
		quotes: []quotedIn{{"models-and-cost", "its choices are `%s`"}},
	}, {
		// THE SHIPPED DEFAULT IS THE LADDER'S OWN DEFAULT rendered in the one
		// public vocabulary, not a second word maintained by each page.
		fact: "the shipped thinking default", owner: "config.EffortWord(effort.Ship)",
		value: config.EffortWord(effort.Ship), others: []string{"low", "medium", "high", "xhigh", "max"},
		quotes: []quotedIn{
			{"models-and-cost", "**The default is `%s`.**"},
			{"models-and-cost", "The **thinking** row in `/settings` defaults to `%s`."},
			{"keys", "ships at `%s` (the provider default)"},
		},
	}, {
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
		fact: "the run supervisor's re-ask cadence", owner: "run.passInterval",
		value: strconv.Itoa(sourceDurationNumber(t, "../run/run.go", "passInterval", "Millisecond")),
		quotes: []quotedIn{
			{"how-tasks-run", "every **%s milliseconds**"},
			{"tasks", "every %s milliseconds"},
		},
	}, {
		fact: "the node road's re-ask cadence", owner: "session.taskPressurePoll",
		value: strconv.Itoa(sourceDurationNumber(t, "../session/task_pressure.go", "taskPressurePoll", "Second")),
		quotes: []quotedIn{
			{"how-tasks-run", "every **%s seconds**"},
			{"tasks", "every %s seconds"},
		},
	}, {
		fact: "how much of one skill file is read", owner: "skills.MaxSkillFileBytes",
		value:  strconv.Itoa(sourceByteLimit(t, "../skills/skills.go", "MaxSkillFileBytes") / 1024),
		quotes: []quotedIn{{"skills-from-other-tools", "at most %s KiB"}},
	}, {
		fact: "the plugin JSON cap", owner: "skills.maxPluginJSONBytes",
		value:  strconv.Itoa(sourceByteLimit(t, "../skills/regular.go", "maxPluginJSONBytes") / (1 << 20)),
		quotes: []quotedIn{{"skills-from-other-tools", "over %s MiB"}},
	}, {
		fact: "the crew's default per-task limit", owner: "config.CrewTaskCapDefault",
		value: taskDollars, others: otherTaskDollars,
		quotes: []quotedIn{
			{"models-and-cost", "**cap** — `per task %s · crew daily cap none`"},
			{"models-and-cost", "No task may cost more than its limit: **%s** unless you set another"},
			{"models-and-cost", "(`per task %s · crew daily cap none`)"},
			{"models-and-cost", "an emptied box is %s again"},
			{"models-and-cost", "this task reached its %s limit · raise it in /crew"},
			{"models-and-cost", "held under the %s.00 task limit"},
			{"models-and-cost", "| **per task** | `%s a task`"},
			{"models-and-cost", "## What may a task spend — %s a task unless you set another"},
			{"models-and-cost", "**A task carries a dollar limit of its own: %s unless you set another.**"},
			{"models-and-cost", "`per task` row — `%s a task`"},
			{"commands", "the most one task may spend — %s unless set"},
			{"commands", "the most one task may spend — %s unless set."},
			{"commands", "per task %s · crew daily cap $5.00"},
			{"running-from-the-terminal", "**Every run is held to the per-task limit**, %s unless"},
			{"running-from-the-terminal", "this task reached its %s limit · raise it in /crew"},
			{"tasks", "**Every task also has a money limit of its own: %s unless you set another**"},
		},
	}, {
		fact: "the checker's ceiling multiplier", owner: "config.CrewCheckCeilingTimes",
		value: ceilingTimes, others: otherCeilingTimes,
		quotes: []quotedIn{
			{"models-and-cost", "**A checker has a ceiling of its own on each task**: %s times its estimate"},
			{"models-and-cost", "ceiling of $0.26, %s times its estimate, before it finished"},
		},
	}, {
		fact: "the checker's ceiling floor", owner: "config.CrewCheckCeilingFloor",
		value: ceilingFloor, others: otherCeilingFloors,
		quotes: []quotedIn{{"models-and-cost", "less than %s. A check that reaches it"}},
	}, {
		// THE CREW IS THREE SEATS, and every page that counts them counts
		// them from the router's own list.
		fact: "how many seats the crew is", owner: "crewroute.Seats", value: seats, others: notSeats,
		quotes: []quotedIn{
			{"models-and-cost", "The crew is %s seats"},
			{"commands", "the %s seats a task runs on"},
			{"getting-started", "the crew is %s seats"},
		},
	}, {
		// THE TWO NUMBERS `/redo stronger` TEACHES WITH are the log reader's
		// own, so a page promising a decay the code does not keep names itself.
		fact: "how many accepted tasks decay a learned step", owner: "router.redoDecayAfter",
		value: decay, others: notDecay,
		quotes: []quotedIn{{"models-and-cost", "after %s accepted task"}},
	}, {
		fact: "how many steps a class can learn", owner: "router.redoOffsetCeiling",
		value: steps, others: notSteps,
		quotes: []quotedIn{{"models-and-cost", "at most %s steps"}},
	}, {
		// THE ONE-TASK WORDS are the router's, on the chat door and the
		// headless one alike.
		fact: "the word for the strongest crew", owner: "crewroute.EffortBest",
		value: string(crewroute.EffortBest),
		quotes: []quotedIn{
			{"tasks", "`/task --%s <brief>`"},
			{"running-from-the-terminal", "`--%s`"},
		},
	}, {
		fact: "the word for the cheapest crew", owner: "crewroute.EffortCheap",
		value: string(crewroute.EffortCheap),
		quotes: []quotedIn{
			{"tasks", "`/task --%s <brief>`"},
			{"running-from-the-terminal", "`--%s`"},
		},
	}, {
		// The mark a pinned seat wears on a headless line.
		fact: "the mark a pinned seat wears headless", owner: "crewroute.seatModel",
		value:  "(pinned)",
		quotes: []quotedIn{{"running-from-the-terminal", "checker kimi-k3 %s"}},
	}, {
		// The rule an untouched profile allows.
		fact: "the allowed rule nobody wrote", owner: "config.CrewAllowedAt",
		value:  config.CrewAllowedAt(t.TempDir()).String(),
		quotes: []quotedIn{{"models-and-cost", "An unwritten rule is `%s`"}},
	}, {
		// THE MIGRATION'S ONE LINE, as the function that writes it says it.
		fact: "the line a migrated profile is told", owner: "config.MigrateCrew",
		value:  migrationLine(t),
		quotes: []quotedIn{{"models-and-cost", "%s"}},
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
		quotes: []quotedIn{{"lanes", "The model codeaf ships with is spelled `%s`"}},
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
		fact: "how many turns one worker gets", owner: "`codeaf exec`'s --max-turns default",
		value:  strconv.Itoa(flagNumber(t, "../../cmd/codeaf/exec.go", "max-turns")),
		quotes: []quotedIn{{"adaptive-runs", "it stops itself after %s turns"}},
	}, {
		// Spelled in threes on the page for the reason the answer room above is.
		// `--budget` was renamed `--token-budget`: *budget* is a word about
		// money everywhere else in this product, so a token count wearing it
		// read as dollars.
		fact: "how many tokens one worker gets", owner: "`codeaf exec`'s --token-budget default",
		value:  grouped(flagNumber(t, "../../cmd/codeaf/exec.go", "token-budget")),
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
		value: dollarsOwed(config.DefaultPlanConsentUSD),
		quotes: []quotedIn{
			{"models-and-cost", "| **per plan** | `asks first above $%s` |"},
			// `codeaf do` on the run engine stops at the same figure unless
			// --yes-spend said otherwise, and the page that says so quotes it.
			{"worker-harness", "**$%s** out of the box"},
		},
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
	return facts
}

// migrationLine is the sentence a profile from before the crew was routed is
// told once, taken from the function that writes it: a profile holding a
// retired preset word is migrated in a temporary directory and the line it
// answers is what the page must quote.
func migrationLine(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(config.BudgetConfigPath(dir), []byte(`{"models.crew": "balanced"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	line, err := config.MigrateCrew(dir)
	if err != nil || line == "" {
		t.Fatalf("a profile with a retired preset word did not migrate: %q (%v)", line, err)
	}
	return line
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
		{"commands", "../tui3/crew.go", "crewUsage"},
		{"models-and-cost", "../tui3/crew.go", "crewUsage"},
	} {
		onScreen := sourceString(t, quoted.file, quoted.name)
		if !strings.Contains(pages[quoted.page], onScreen) {
			t.Errorf("%s does not quote %s whole. The screen says:\n\t%s",
				quoted.page, quoted.name, onScreen)
		}
	}
}

// THE COMMAND LIST AND THE MANUAL NAME THE SAME SEATS.
//
// The rows a person reads on /help and the page they read afterwards are two
// hand-written sets of sentences about one crew. Both are compared to the
// router's own seat list here rather than to each other, so the failure names
// the seat rather than only saying they differ.
func TestTheCrewCommandRowsNameTheSeatsTheRouterOwns(t *testing.T) {
	rows := strings.Join(crewCommandRows(t), "\n")
	if rows == "" {
		t.Fatal("internal/tui3/commands.go offers no /crew row")
	}
	for _, seat := range crewroute.Seats {
		if !strings.Contains(rows, string(seat)) {
			t.Errorf("no /crew row names the %s — crewroute.Seats has it:\n%s", seat, rows)
		}
	}
	pages := flatChatPages(t)
	for _, seat := range crewroute.Seats {
		if !strings.Contains(pages["models-and-cost"], "**"+string(seat)+"**") {
			t.Errorf("models-and-cost never names the %s seat in bold", seat)
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

// sourceDurationNumber checks both a cadence's figure and its unit, so a
// millisecond to second change cannot leave a numerically equal page green.
func sourceDurationNumber(t *testing.T, path, name, unit string) int {
	t.Helper()
	match := regexp.MustCompile(`(?m)^\s*(?:const\s+)?` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)\s*\*\s*time\.([A-Za-z]+)`).FindStringSubmatch(sourceText(t, path))
	if match == nil || match[2] != unit {
		t.Fatalf("%s no longer holds %s in time.%s", path, name, unit)
	}
	n, _ := strconv.Atoi(match[1])
	return n
}

// sourceByteLimit resolves the small arithmetic expression that owns a file
// cap, including its multiplier; reading only its first digit would miss drift.
func sourceByteLimit(t *testing.T, path, name string) int {
	t.Helper()
	match := regexp.MustCompile(`(?m)^\s*(?:const\s+)?` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)\s*(\*|<<)\s*(\d+)`).FindStringSubmatch(sourceText(t, path))
	if match == nil {
		t.Fatalf("%s no longer holds a byte limit called %s", path, name)
	}
	left, _ := strconv.Atoi(match[1])
	right, _ := strconv.Atoi(match[3])
	if match[2] == "<<" {
		return left << right
	}
	return left * right
}

// flagNumber is the default of one command-line flag, read out of the file that
// declares it. It exists beside [sourceNumber] because a figure is not always a
// named constant: a command's walls are often written as literals in the flag
// declaration itself — `flags.Int("turns", 200, …)` — and that literal is both
// what the command's own help prints and what a page quoting it must agree with.
//
// IT READS EVERY SHAPE A NUMERIC FLAG IS DECLARED IN, and that is the whole
// reason this comment is longer than the function. A flag that moves to
// cmd/codeaf's [newCountFlag] — the value that refuses a bad count with a
// sentence about the flag instead of [strconv]'s `parse error` — keeps exactly
// the same default and stops being `flags.Int` while it does it. A reader that
// knew only the one shape went RED with a message about a flag that had not
// changed, and the day somebody "fixed" that by exempting the flag, every
// figure declared that way would have gone invisible to this gate — silently,
// which is the failure mode this whole file exists to prevent. `exec`'s two
// walls and `logs --tail` are all declared that way now.
//
// AND A DEFAULT MAY BE A NAME RATHER THAN A NUMBER. A door that reads its
// default from the constant that owns it is this repository's one-source-of-truth
// rule working, so the name is resolved through [flagDefaultConstants] rather
// than treated as a missing flag. A shape nobody has taught it is still a hard
// failure, and that is deliberate.
func flagNumber(t *testing.T, path, name string) int {
	t.Helper()
	quoted := regexp.QuoteMeta(name)
	// A default is a literal or the NAME of one. The trailing group is the same
	// group in every shape, so one loop reads them all and one reader decides
	// afterwards which kind it got.
	const number, named = `(\d+)`, `([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)`
	var shapes []string
	for _, default_ := range []string{number, named} {
		shapes = append(shapes,
			// flags.Int("max-turns", 200, …) — and Int(…, exec.DefaultLeafTokens, …)
			`Int\(\s*"`+quoted+`"\s*,\s*`+default_,
			// newCountFlag(flags, "max-turns", 200, …) — and the same, named
			`newCountFlag\([^,]+,\s*"`+quoted+`"\s*,\s*`+default_,
		)
	}
	for _, shape := range shapes {
		match := regexp.MustCompile(shape).FindStringSubmatch(sourceText(t, path))
		if match == nil {
			continue
		}
		if value, err := strconv.Atoi(match[1]); err == nil {
			return value
		}
		value, ok := flagDefaultConstants[match[1]]
		if !ok {
			t.Fatalf("--%s's default is %s, which this gate cannot resolve to a number.\n"+
				"Add it to flagDefaultConstants in this file, importing the package that owns it, "+
				"so the figure the manual quotes goes on being checked against the door", name, match[1])
		}
		return value
	}
	t.Fatalf("%s no longer declares a --%s flag with a number for its default, in any shape this reads "+
		"(flags.Int, newCountFlag, either with a literal or with a named constant) — teach it the new "+
		"shape rather than dropping the flag, or the figure the manual quotes stops being checked at all",
		path, name)
	return 0
}

// flagDefaultConstants is every Go constant a door may name where a flag default
// goes, spelled the way the door writes it.
//
// A DOOR THAT READS ITS DEFAULT FROM A CONSTANT IS THE REPOSITORY'S OWN RULE
// WORKING, not a shape to exempt. `codeaf exec --token-budget` used to spell
// 150000 itself, and so did `codeaf run` and the chat surface, so a
// recalibration had to move four numbers together and a door could print a
// figure the loop no longer used. They read [executor.DefaultLeafTokens] now.
// That fix took the literal this gate was reading away, and the gate's own
// failure said what to do about it: teach it the new shape. Resolving the name
// HERE, against the constant this package links to, is what keeps the manual's
// sentence answerable to the door — the alternative, reading the constant's own
// file by regex, would go quiet the day somebody moved the declaration.
var flagDefaultConstants = map[string]int{
	"exec.DefaultLeafTokens": executor.DefaultLeafTokens,
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
