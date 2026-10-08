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
//	r  run              run every stage; the run stops where `ask me at` says
//	t  ask me at        where the run stops to ask you: plan, pull request, never
//	T  chat             a conversation about this item with the issue loaded
//	space select        tick the row to run several together
//	L  run selected     run the ticked rows (named only while one is ticked)
//	e  thinking         how hard the model thinks, the chat's Thinking dial
//	c  budget           the most this item may spend
//	1-9 stages          turn a stage on or off (named only on the `?` sheet)
//	g  open on github   the issue or pull request in the browser
//	u  refresh          read the issue again: size, cost, priority
//	d  dismiss          take the row off the floor
//	s  approve          accept a landed result
//	B  request changes  another round, with notes
//	v  re-run checks    run the checks on the branch again
//	m  foreman          the floor's own chat that proposes a batch
//	U  refresh all      read every item again
//	h  handover         the four-row head
//
// THE RECIPE FILE KEEPS ITS OWN GRAMMAR. `.codeaf/factory.md` still says
// `gate plan`, `gate ship`, `gate none` and `effort strong`, and the item's
// stored gate is still [factory.GatePlan], [factory.GateShip] or
// [factory.GateNone]: only the screen's words changed ([factoryGateWord]).

// The keys.
const (
	keyOpen               = "enter"
	keyRun                = "r"
	keyAskAt              = "t"
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
	wordAskAt              = "ask me at"
	wordGatePlan           = "plan"
	wordGatePR             = "pull request"
	wordGateNever          = "never"
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
	wordFacetRun      = "run"
	wordFacetLog      = "log"
	wordFacetResult   = "result"
	wordFacetSettings = "settings"
	// wordFloorCrumb is the first crumb of the item page's trail, the floor.
	wordFloorCrumb = "Factory"
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

// factoryGateWord is the screen's word for where the run stops to ask: the
// stored gate is the recipe file's word (`plan`, `ship`, `none`), and the
// screen says it the way a person would (`plan`, `pull request`, `never`).
func factoryGateWord(g factory.Gate) string {
	switch g {
	case factory.GatePlan:
		return wordGatePlan
	case factory.GateShip:
		return wordGatePR
	case factory.GateNone:
		return wordGateNever
	}
	return string(g)
}

// factoryAskAtWords is the gate as one phrase, `ask me at plan`, and nothing
// for an item with no gate (the emptiness law).
func factoryAskAtWords(g factory.Gate) string {
	if g == "" {
		return ""
	}
	return wordAskAt + " " + factoryGateWord(g)
}
