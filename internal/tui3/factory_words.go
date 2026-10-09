package tui3

import "github.com/Agent-Field/codeaf/internal/factory"

// ── THE FACTORY'S VOCABULARY ────────────────────────────────────────────────
//
// EVERY VERB THE FLOOR AND THE ITEM PAGE NAME IS SPELLED HERE, ONCE (owner
// decision, 2026-10-08: "too many single letters, and the words do not say
// what they do"). A hint clause, a chip, a note sentence, an action line and
// the `?` sheet all read their word and their key from these constants, so a
// word changed here changes on every line that names it, and a word spelled
// anywhere else is a word that can drift. TestFactoryWordsAreTheVocabulary
// holds the old words out of the factory's sources.
//
// THE WORDS SAY WHAT THE KEY DOES, never the shape the screen draws it in:
//
//	r  run              run every stage; the run holds at each approve step
//	T  chat             a conversation about this item with the issue loaded
//	space select        tick the row to run several together
//	L  run selected     run the ticked rows (named only while one is ticked)
//	e  thinking         how hard the model thinks, the chat's Thinking dial
//	c  budget           the most this item may spend
//	1-9 stages          turn a stage on or off (named only on the `?` sheet)
//	g  open on github   the issue or pull request in the browser
//	u  refresh          read the issue again: size, cost, priority
//	p  shape steps      the manager sets the steps for this item, runs nothing
//	d  dismiss          take the row off the floor
//	s  approve          accept a landed result
//	B  request changes  another round, with notes
//	v  re-run checks    run the checks on the branch again
//	m  foreman          the floor's own chat that proposes a batch
//	U  refresh all      read every item again
//	h  handover         the four-row head
//
// WHERE THE RUN HOLDS FOR THE PERSON IS AN APPROVE STEP among the stages
// (internal/factory's approve.go), never a setting: `ask me at` is gone.

// The keys.
const (
	keyOpen               = "enter"
	keyRun                = "r"
	keyChat               = "T"
	keySelect             = "space"
	keyRunSelected        = "L"
	keyThinking           = "e"
	keyBudget             = "c"
	keyStages             = "1-9"
	keyAddStage           = "s"
	keyInWordsSet         = "w"
	keySaveRecipe         = "b"
	keyAskAuthor          = "a"
	keySync               = "g"
	keyOpenGitHub         = "g"
	keyRefresh            = "u"
	keyShapeSteps         = "p"
	keyDismiss            = "d"
	keyDiff               = "d"
	keyApprove            = "s"
	keyApproveWithChanges = "e"
	keyRequestChanges     = "B"
	keyRerunChecks        = "v"
	keyStop               = "x"
	keyPause              = "space"
	keySteer              = "S"
	keyYes                = "y"
	keyNo                 = "n"
	keyInWords            = "a"
	keyNew                = "n"
	keyRepos              = "R"
	keyRecipe             = "E"
	keyRail               = "$"
	keyForeman            = "m"
	keyRefreshAll         = "U"
	keyHandover           = "h"
	keyFilter             = "/"
	keyBacklog            = "A"
	keyDensity            = "z"
	keyOrder              = "O"
	keyRepoWalk           = "[ ]"
	keySplit              = "{ } |"
	keyScroll             = "J K"
	keyWalk               = "↑↓"
	keyBack               = "esc"
	keySheet              = "?"
)

// The words.
const (
	wordOpen               = "open"
	wordProof              = "proof"
	wordRun                = "run"
	wordChat               = "chat"
	wordSelect             = "select"
	wordRunSelected        = "run selected"
	wordThinking           = "thinking"
	wordBudget             = "budget"
	wordStages             = "stages"
	wordAddStage           = "add a stage"
	wordInWordsSet         = "set in words"
	wordSaveRecipe         = "save stages as the recipe"
	wordAskAuthor          = "ask the author"
	wordSync               = "sync to github"
	wordOpenGitHub         = "open on github"
	wordRefresh            = "refresh"
	wordShapeSteps         = "shape steps"
	wordRefreshAll         = "refresh all"
	wordDismiss            = "dismiss"
	wordDismissed          = "dismissed"
	wordDiff               = "diff"
	wordApprove            = "approve"
	wordApproveWithChanges = "approve with changes"
	wordRequestChanges     = "request changes"
	wordChangesRequested   = "changes requested"
	wordRerunChecks        = "re-run checks"
	wordStop               = "stop"
	wordPause              = "pause"
	wordResume             = "resume"
	wordSteer              = "steer"
	wordYes                = "yes"
	wordNo                 = "no"
	wordInWords            = "in words"
	wordNew                = "new"
	wordRepos              = "repos"
	wordRecipe             = "recipe"
	wordRail               = "rail"
	wordForeman            = "foreman"
	wordHandover           = "handover"
	wordFilter             = "filter"
	wordBacklog            = "backlog"
	wordDensity            = "density"
	wordOrder              = "order"
	wordRepoWalk           = "repo"
	wordSplit              = "move the split"
	wordScroll             = "scroll"
	wordWalk               = "walk"
	wordWalkStages         = "stages"
	wordFloorName          = "floor"
	wordBack               = "back"
	wordClear              = "clear"
	wordClose              = "close"
	wordSheet              = "keys"
	wordConversation       = "conversation"
	wordOf                 = "of"
	wordRows               = "rows"
	wordOn                 = "on"
	wordOff                = "off"
)

// The item page's rows, one per facet of the item (factory_item.go): what a
// person reads on its left column and on the `?` sheet.
const (
	wordFacetIssue    = "issue"
	wordFacetManager  = "manager"
	wordFacetSteps    = "steps"
	wordFacetLog      = "log"
	wordFacetResult   = "result"
	wordFacetSettings = "settings"
	// wordFloorCrumb is the first crumb of the item page's trail, the floor.
	wordFloorCrumb = "Factory"
)

// The item page's top bar (factory_bar.go): its one control, which says what
// a press does where the run stands, and what a step held for a person says
// on the left column.
const (
	keyControl   = "space"
	wordContinue = "continue"
	// wordSendBack is `n` at an approve step: the run goes back to the step
	// before it.
	wordSendBack      = "send back"
	wordWaitingForYou = "waiting for you"
	wordTypeHere      = "type here"
)

// The timeline's words: the run's story in the middle of the item page
// (factory_timeline.go).
const (
	wordManager      = "manager"
	wordYou          = "you"
	wordStep         = "step"
	wordSteps        = "steps"
	wordSaid         = "said"
	wordSkipped      = "skipped"
	wordMoreAbove    = "more above"
	wordRunsIt       = "runs it"
	wordOpensTheChat = "opens the conversation"
	// The story is the manager's conversation: the line while a turn runs on
	// it, `manager is thinking`, and the tail of a reply cut short, `… ▸ T
	// for the whole chat`.
	wordIsThinking      = "is thinking"
	wordForTheWholeChat = "for the whole chat"
	// The manager's box (factory_timeline.go): what it says unfocused, what
	// its empty typing row says, what `enter` on the manager row does, and
	// the way out of it, which keeps the words typed.
	wordEnterOrClickToTalk = "enter or click to talk"
	wordToTheManager       = "to the manager"
	wordSayIt              = "say it"
	wordTalk               = "talk"
	wordSend               = "send"
	wordBackToKeys         = "back to keys"
)

// A stage's loop (owner decision, 2026-10-08): the item page's rail says it
// in a cell or two (factory_item.go), and the story's open head says it whole,
// `round 1 of 2 · until clean · per finding`, with the stage's ask and the
// reason it was set dim under it (factory_timeline.go).
const (
	wordRound    = "round"
	wordUntil    = "until"
	wordAskLabel = "ask:"
	wordWhyLabel = "why:"
)

// The runner's words about the manager's shaping. They are the runner's
// lines in the run's stream, spelled here so the vocabulary and the e2e words
// table (internal/e2e/tuiwords_test.go) name them once. (The shaping question
// `run these stages?` went with `ask me at`: an approve step holds instead.)
const (
	// wordManagerSet leads the Adapted line the manager's edit writes.
	wordManagerSet = "manager set"
	// wordRecipeStands is the stream line after `n`, or after the manager
	// did not answer (`the manager did not answer · the recipe stands`).
	wordRecipeStands = "the recipe stands"
	// wordStageIsOneWord is the runner's refusal of a stage name that is not
	// one word.
	wordStageIsOneWord = "a stage is one word"
	// wordNineStagesAlready is the runner's refusal to add a tenth stage.
	wordNineStagesAlready = "the run has nine stages already"
)

// The words of a stage the recipe fixes (factory_stagefixed.go): the refusal
// every key on the floor says for it, after the stage's name.
const (
	wordFixedByRecipe       = "is fixed by the recipe"
	wordChangeTheRecipeFile = "change " + factory.RecipeFile + " to change it"
)

// The first offer of a recipe file (factory_recipeoffer.go): a repository with
// no `.codeaf/factory.md` whose first item lands is asked once whether to
// write one, at the bottom of the right column where the habit offer stands.
const (
	wordNoRecipe       = "no recipe in"
	wordYet            = "yet"
	wordWriteRecipeAsk = "write the recipe into the repo so the team shares it?"
	wordWriteIt        = "write it"
	wordNotNow         = "not now"
	wordRecipeWritten  = "recipe written"
)

// The `?` sheet's group names, in the order the sheet draws them.
const (
	wordGroupDo   = "do"
	wordGroupSet  = "set"
	wordGroupAlso = "also"
	wordGroupMove = "move"
)

// factoryStripMost is the most clauses the peek's strip and the item page's
// action line name: the row's verbs, never the whole keyboard (`?` has that).
const factoryStripMost = 5

// factoryHintClause is one clause of a key line: the key, a space, the word.
func factoryHintClause(key, word string) string { return key + " " + word }
