package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// THE SETTINGS PANEL: /settings, or ctrl+, — the one fullscreen thing this
// surface draws.
//
// It is omp's INTERACTIONS sheet over aforge's own registry, and the whole of
// what this file adds to that registry is a UI SKIN: which tab a row belongs
// under, what it is called there, the one line it says about itself, and which
// widget answers it. NOT ONE SETTING IS DECLARED HERE. Every row comes from
// [config.Settings] — the same rows the v2 sheet renders, written through the
// same writers — because a second place to declare a knob is a second place for
// a knob to disagree with itself.
//
// Four rules hold the design together:
//
//   - THE MAP IS TOTAL over the registry. A row nobody placed would be a row
//     nobody could reach, so chrome_test.go fails the build when a registry key
//     has no [settingMeta]. A new setting lands as one registry row plus one
//     line in [settingUI], which is the same trade omp makes.
//   - The panel is MODAL and fullscreen, and it is the only thing on this
//     surface that is. A settings sheet is not something you read the
//     conversation past, and the alternative — a bottom-anchored list of
//     twenty-eight rows — would have taken the frame anyway while pretending
//     not to.
//   - Every write goes through [config.Setting.Apply], which validates in plain
//     language and persists to the GLOBAL profile. The project layer
//     (<workspace>/.openaf/config.json) is deliberately not writable from here:
//     it is a file a repository commits, and a panel that edited it would be
//     this surface committing to somebody's repository on their behalf.
//   - A refusal is SHOWN, never swallowed. A pinned row, a seam the door did
//     not wire, a number outside its band — each answers in the registry's own
//     words on the foot line.
//
// The two-space indent, the dim values and the single accent are styles.go's
// palette and nothing new: the panel is a list, and this surface already knows
// what a list looks like (palette.go's overlayRow draws every row here).

// The tabs, in the order docs/CHAT-V3.md Decision 6 names them.
const (
	// tabSession is this conversation: what it may run, what it may spend, and
	// which models answer the small calls it makes for itself.
	tabSession = "Session"
	// tabContext is what a model carries — the context law, whole.
	tabContext = "Context"
	// tabWorkspace is what aforge may do and spend while it works for you.
	tabWorkspace = "Workspace"
	// tabDisplay is how the surface draws itself and what it remembers of your
	// typing.
	tabDisplay = "Display"
	// tabProviders is which model answers what.
	tabProviders = "Providers"
)

// And the sixth, which is not a reading of the registry at all: the accounts
// this profile has connected and what each of them may do (connectcaps.go). It
// is last because the five before it are one object read five ways, and a
// person walking the bar meets the knobs before their accounts.
var settingTabs = []string{tabSession, tabContext, tabWorkspace, tabDisplay, tabProviders, tabConnections}

// settingWidget is how a row is ANSWERED, which is not quite how it reads.
// The registry's [config.SettingKind] says what a value is; this says what the
// keyboard does to it.
type settingWidget uint8

const (
	// widgetText opens the one-line submenu: enter saves, empty clears, esc
	// cancels. It is the default because most rows are a word or a number.
	widgetText settingWidget = iota
	// widgetToggle flips in place on enter or space. Booleans only.
	widgetToggle
	// widgetCycle walks a short enum in place, in the registry's own order.
	widgetCycle
	// widgetSelect opens THE MODEL PICKER — the same component /model opens
	// (palette.go), filter box, ranking, and rows carrying window, price and
	// arena score. A slot row is a model choice, and a model choice is a thing
	// this surface already knows how to ask; a second, plainer list would be a
	// worse way to answer the same question in the same product.
	widgetSelect
)

// settingMeta is the UI half of a registry row: where it is shown, what it is
// called there, the one line under it, and the widget that answers it.
//
// label and about may be empty, and then the registry's own Label and the first
// sentence of its Hint are used. That is not laziness — the registry's words are
// already the product's words (internal/config, rule 14), and repeating them
// here would be a second copy to keep in step. The overrides exist for the rows
// whose registry hint is three sentences long: a panel row gets ONE line.
// Masking is deliberately NOT one of these fields. A credential row masks
// itself — [config.Setting] carries Secret and its reader already hands back
// dots and a tail — so this panel never sees the key at all, which is the only
// arrangement in which it cannot leak one. What the flag buys HERE is the edit
// box: it opens on the row's displayed value (the mask), and the registry's
// writer treats an unchanged mask as "no change", so enter on a row somebody
// only looked at does not overwrite their key with a row of bullets.
type settingMeta struct {
	tab    string
	label  string
	about  string
	widget settingWidget
}

// settingUI is the skin: registry key → where it lives and how it is answered.
//
// The tabs are a reading of the rows and not of the categories: internal/config
// groups by what a row IS (models, spending, practice, interface); a person
// opening this panel is looking for what a row is ABOUT. "session ceiling" is a
// dollar figure and it lives under Session, because the question it answers is
// "what may THIS conversation do".
var settingUI = map[string]settingMeta{
	// ── Session ─────────────────────────────────────────────────────────────
	config.KeyToolApprovalMode: {
		tab: tabSession, label: "ask before running", widget: widgetCycle,
		about: "what happens when the model asks to run a tool. Dangerous shell " +
			"commands are asked about whichever way this is set.",
	},
	config.KeyToolApprovals: {
		tab: tabSession, label: "tool exceptions", widget: widgetText,
		about: "exceptions to the answer above, one per tool: read:allow, bash:prompt.",
	},
	// And under the tool exceptions, the exceptions for the one tool a per-tool
	// answer cannot really answer. It is where "always, this command" on an
	// approval question lands, so it is also where a person comes to take one
	// back: this is the row the card's receipt sends them to.
	config.KeyBashApprovals: {
		tab: tabSession, label: "shell command rules", widget: widgetText,
		about: "answers for single shell commands, first match wins: " +
			"allow git status*, deny rm -rf *.",
	},
	// It sits directly under the two rows it modifies, because that is what it
	// is: not a fourth approval mode but a filter in front of the one above —
	// it can only spare you a question, never answer one those rows refuse.
	config.KeyGuardian: {
		tab: tabSession, label: "guardian", widget: widgetCycle,
		about: "asks a small model first whether a call is plainly safe, so you " +
			"are only asked about the rest.",
	},
	// And directly under those three, because it is the last thing that can
	// happen to the question they raise: the clock that answers it when nobody
	// does. It is a countdown toward NO — the row above can spare you a
	// question, this one refuses on your behalf — which is why it sits here and
	// not beside the task countdown it otherwise looks like.
	config.KeyConsentTimeout: {
		tab: tabSession, label: "approval countdown", widget: widgetText,
		about: "seconds an approval question waits before it answers no for you. " +
			"Any key stops the clock; 0 turns it off.",
	},
	// THE MACHINERY IS NOT THE SURFACE'S TO NAME, and a settings row is as much
	// the surface as a card is. The key is the engine's ([config.KeyTaskAudit])
	// and it keeps its name; what a person reads is what the switch DOES to their
	// work — the task's own claim that it is finished is taken on trust, or it is
	// not — because that, and not the shape of the apparatus behind it, is the
	// thing they are being asked to decide.
	config.KeyTaskAudit: {
		tab: tabSession, label: "check task work", widget: widgetCycle,
		about: "each task's work is checked over before it merges. " +
			"Off merges on the task's own word.",
	},
	config.KeyMemoryConsolidation: {
		tab: tabSession, label: "memory consolidation", widget: widgetCycle,
		about: "memory quietly tidies itself when the session sits idle. " +
			"Off, it only ever grows.",
	},
	// The countdown sits under the gate rows for the same reason the guardian
	// does: it is not a fourth approval mode but the OTHER clock in the room —
	// how long a proposed task waits for you before it starts on its own.
	config.KeyTaskAutoApprove: {
		tab: tabSession, label: "task countdown", widget: widgetText,
		about: "seconds a proposed task waits for you before it starts. " +
			"0 waits for your answer instead.",
	},
	// And under the countdown, what happens at the OTHER end of a task: how many
	// times work that came back with something missing is sent back to finish it.
	config.KeyTaskRepairRounds: {
		tab: tabSession, label: "task repair rounds", widget: widgetText,
		about: "times a task that came back with something missing is sent back to " +
			"finish it before it lands as incomplete. 0 lets the first gap end it.",
	},
	// And the three that say how much of it happens at once: the number you may
	// name, and the two readings of the machine that hold the next one back
	// whatever you named.
	config.KeyTaskParallel: {
		tab: tabSession, label: "tasks at once", widget: widgetText,
		about: "how many tasks may run at the same time. Blank is no limit — the machine " +
			"and the provider are the real ceilings.",
	},
	config.KeyTaskMaxLoad: {
		tab: tabSession, label: "busy machine", widget: widgetText,
		about: "the load per core at which new tasks wait instead of starting. Running " +
			"tasks are never touched. 0 stops watching.",
	},
	config.KeyTaskMinFreeMB: {
		tab: tabSession, label: "memory floor", widget: widgetText,
		about: "MB of memory that must be free before another task starts. 0 stops " +
			"watching.",
	},
	// And beside it, the other thing that is true of every task you hand off:
	// whose hands it goes into. It is answered by the PICKER, like the two tier
	// rows below it and for the same reason — a row that asks "which model" and
	// offers a blank line is asking a person to be the catalog.
	config.KeyTaskModel: {
		tab: tabSession, label: "task model", widget: widgetSelect,
		about: "the model a task runs on when you have not asked for another. " +
			"Blank runs it on the model you are talking to.",
	},
	// THE TWO TIER ROWS ARE MODEL CHOICES AND ARE ANSWERED AS ONE. They were a
	// text box for four waves, which meant the only way to name the cheap model
	// was to type its id from memory — in a panel that already knows every id,
	// what each one holds, what it costs and how it scores. A row that asks
	// "which model" and offers a blank line is asking a person to be the
	// catalog.
	config.KeyTierLowModel: {
		tab: tabSession, label: "small work", widget: widgetSelect,
		about: "the cheap model for the short things aforge writes for itself — " +
			"session names, labels.",
	},
	config.KeyTierHighModel: {
		tab: tabSession, label: "careful work", widget: widgetSelect,
		about: "the capable model for the small things that must not be wrong — " +
			"the summary a compaction keeps.",
	},
	config.KeyModelRoles: {
		tab: tabSession, label: "pinned roles", widget: widgetText,
		about: "exceptions to the two rows above, one per role: title:openai/gpt-5-mini.",
	},
	// It is a TEXT box and not a select, unlike the two tier rows above it,
	// because the answer is an ORDER rather than a choice: a picker that returns
	// one id cannot express "this one, then that one", and a fallback list of one
	// is most of what makes this row worth having.
	config.KeyModelFallbacks: {
		tab: tabSession, label: "fallback models", widget: widgetText,
		about: "where a conversation goes when no endpoint will take the request: " +
			"slugs, comma-separated, first tried first. Blank picks the nearest one.",
	},
	config.KeySpendRail: {
		tab: tabSession, label: "session ceiling", widget: widgetText,
		about: "what one conversation may spend before it stops starting turns. " +
			"0 removes the ceiling; the turn in flight always finishes.",
	},

	// ── Context ─────────────────────────────────────────────────────────────
	//
	// The four rows of the context law, which is where compaction is actually
	// configured: fill decides WHEN a conversation is compacted, and the other
	// three decide what a call carries when it is.
	config.KeyContextFill: {
		tab: tabContext, label: "compact at", widget: widgetText,
		about: "how much of the model's window aforge fills before it compacts, " +
			"as a percent. The rest stays as thinking and answer room.",
	},
	config.KeyCompletionReserve: {
		tab: tabContext, label: "answer room", widget: widgetText,
		about: "tokens every call keeps free for its answer and its reasoning.",
	},
	config.KeyWorkingSet: {
		tab: tabContext, label: "working set", widget: widgetText,
		about: "the most material kept quoted in front of a worker at once, " +
			"however large the model's window is.",
	},
	// THE UNIT IS A MULTIPLE AND THE PANEL SAYS SO. The row reads 250 by
	// default, and one line of "as a percent" over that number reads as a
	// percentage OF something — of a window, of a budget — which makes 250 look
	// like a mistake or like tokens mislabelled. It is neither: the registry's
	// figure is cumulative re-sends of the whole context expressed in hundredths,
	// so 100 is once and 250 is two and a half times over, and it is floored at
	// 100 rather than capped at 100 (internal/config's writeContextReuse: below
	// one whole context it is a refusal, not a governor). The label carries the
	// worked example, because the number a person sees is 250 and the sentence
	// under it has one job — making that number mean something.
	config.KeyContextReuse: {
		tab: tabContext, label: "context reuse", widget: widgetText,
		about: "how many times over one piece of work may re-send its whole " +
			"context before aforge tells it to land: 100 is once, 250 is two and " +
			"a half times. At least 100.",
	},
	// Search is a context row for the reason the four above it are: it decides
	// what a model can put IN its context that it did not already have.
	config.KeySearchProvider: {
		tab: tabContext, label: "searching", widget: widgetCycle,
		about: "where a web search goes. auto uses the best back end your keys " +
			"reach and falls back to one that needs none.",
	},
	config.KeyExaKey: {
		tab: tabContext, label: "exa key", widget: widgetText,
		about: "an exa.ai key, which buys better results and page fetches than " +
			"the free back end. Optional.",
	},
	config.KeyJinaKey: {
		tab: tabContext, label: "jina key", widget: widgetText,
		about: "a jina.ai key. It buys nothing but headroom: page fetches already " +
			"work unauthenticated.",
	},

	// ── Workspace ───────────────────────────────────────────────────────────
	//
	// What aforge may spend and do while it works for you, and what it does with
	// its own time when you are not here.
	config.KeyDailyBudget: {
		tab: tabWorkspace, label: "daily budget", widget: widgetText,
		about: "what aforge may spend on your work in a day. 0 removes the rail.",
	},
	config.KeyPlanConsent: {
		tab: tabWorkspace, label: "ask before spending", widget: widgetText,
		about: "above this estimate a planned job quotes its price and waits for " +
			"your go-ahead. 0 never asks.",
	},
	config.KeyPracticeBudget: {
		tab: tabWorkspace, label: "practice budget", widget: widgetText,
		about: "the slice of the day reserved for aforge practicing on itself.",
	},
	config.KeyPracticeIdle: {
		tab: tabWorkspace, label: "quiet before practice", widget: widgetText,
		about: "how long the room stays quiet before aforge starts practicing.",
	},
	config.KeyBriefAfter: {
		tab: tabWorkspace, label: "arrival brief after", widget: widgetText,
		about: "how long you have to be away before aforge greets you with a " +
			"summary. 0 always briefs.",
	},
	config.KeyTenureAfter: {
		tab: tabWorkspace, label: "tenure after", widget: widgetText,
		about: "how many clean firings a standing charter needs before it earns tenure.",
	},
	config.KeyAttribution: {
		tab: tabWorkspace, label: "attribution", widget: widgetToggle,
		about: "signs the commits and PRs aforge writes for you — one trailer, " +
			"one footer line.",
	},
	// The two rows a Google connection is signed with. They belong on this tab
	// and not under Providers because they are not about which model answers
	// what: they are about what aforge may REACH on your behalf, which is the
	// question this tab already holds.
	//
	// Neither of them is where a person connects an account — /connect is, and it
	// asks nothing but a keypress. These are for somebody signing in through
	// their own Google project rather than the one aforge ships with, which is a
	// setting and not a step.
	config.KeyGoogleOAuthClient: {
		tab: tabWorkspace, label: "google sign-in id", widget: widgetText,
		about: "identifies aforge to Google when you connect an account. Blank " +
			"uses the one aforge ships with.",
	},
	config.KeyGoogleOAuthSecret: {
		tab: tabWorkspace, label: "google sign-in secret", widget: widgetText,
		about: "the secret that goes with the id above. It is kept masked once saved.",
	},

	// ── Display ─────────────────────────────────────────────────────────────
	config.KeyHistoryEnabled: {
		tab: tabDisplay, label: "input history", widget: widgetToggle,
		about: "remembers the messages you send, so the up arrow walks them back " +
			"in a later session.",
	},
	config.KeyDraftPersist: {
		tab: tabDisplay, label: "keep drafts", widget: widgetToggle,
		about: "keeps the half-typed message in the box across a restart, per directory.",
	},
	config.KeyNerdFont: {
		tab: tabDisplay, label: "nerd font", widget: widgetToggle,
		about: "draws the chrome with Nerd Font icons. Turn it off if icons show as boxes.",
	},
	config.KeyLinearMode: {
		tab: tabDisplay, label: "linear mode", widget: widgetToggle,
		about: "single column, no motion, no spinners — the accessible rendering.",
	},
	config.KeyRailState: {
		tab: tabDisplay, label: "sidebar", widget: widgetCycle,
		about: "how much of the right rail stands beside the chat: the full " +
			"column, a one-column handle, or nothing.",
	},
	config.KeySplitPct: {
		tab: tabDisplay, label: "chat width", widget: widgetText,
		about: "the chat pane's share of the frame while the task rail is open.",
	},

	// ── Providers ───────────────────────────────────────────────────────────
	//
	// The model slots themselves are added by [init] from [config.ModelSlots],
	// so a sixth role or a sixth modality reaches this panel without anybody
	// editing this file — the same contract internal/config's own sheet keeps.
	//
	// EVERY ONE OF THEM ASKS ITS OWN QUESTION ([filterFor]). Five of these rows
	// are not conversations at all — drawing, speaking, composing, filming, and
	// the one that hears you — and the looking row wants a model that can SEE.
	// Answering all seven with the chat law, which is what this panel did, does
	// not give a media slot a list that is merely too wide: it gives it the exact
	// complement of the rows that could answer it.
	config.KeyMouse: {
		tab: tabDisplay, label: "mouse", widget: widgetCycle,
		about: "on gives hover and click; off gives the terminal's own text selection back.",
	},
	config.KeyTimestamps: {
		tab: tabDisplay, label: "timestamps", widget: widgetCycle,
		about: "footers puts a receipt under each finished turn; separators only marks the gaps.",
	},
	config.KeyVisionModel: {
		tab: tabProviders, label: "looking", widget: widgetSelect,
		about: "the model that looks at images. Blank picks one that can see.",
	},
	config.KeyDocumentEngine: {
		tab: tabProviders, label: "reading", widget: widgetCycle,
		about: "which rung reads your documents. auto walks local, then free, then paid OCR.",
	},
	// It belongs on this tab and not under Session because it is a question
	// about WHERE a request goes, not about what this conversation may do: one
	// model id is served by many endpoints, and this is which of their
	// differences the session pays attention to.
	config.KeyRouting: {
		tab: tabProviders, label: "routing", widget: widgetCycle,
		about: "one model is served by many endpoints. latency asks for the fastest and " +
			"demotes one that keeps being slow; price asks for the cheapest; off asks for nothing.",
	},
}

func init() {
	for _, slot := range config.ModelSlots() {
		settingUI[config.ModelSettingKey(slot.Slot)] = settingMeta{
			tab: tabProviders, label: slot.Label, widget: widgetSelect,
		}
	}
}

// settingMetaFor is the skin for one row, with the registry's own words filled
// in where the map left them out.
func settingMetaFor(row config.Setting) (settingMeta, bool) {
	meta, ok := settingUI[row.Key]
	if !ok {
		return settingMeta{}, false
	}
	if meta.label == "" {
		meta.label = row.Label
	}
	if meta.about == "" {
		meta.about = firstSentence(row.Hint)
	}
	return meta, true
}

// firstSentence is the registry hint cut to one line. A hint is written as
// prose for a sheet with room; a panel row has one line and takes the sentence
// that carries the meaning.
func firstSentence(hint string) string {
	hint = strings.TrimSpace(hint)
	if at := strings.IndexByte(hint, '.'); at > 0 {
		return hint[:at+1]
	}
	return hint
}

// sheetRows is how many list rows the panel wants at most. It is a ceiling and
// not a promise: the panel is fullscreen, so what it actually draws is whatever
// the terminal has after the head and the foot.
const sheetRows = 16

// sheetItem is one line of the list: a row, or — while a search is on — the
// faint tab heading a group of them sits under.
type sheetItem struct {
	head string
	row  config.Setting
	meta settingMeta
	// conn is set on the rows of the Connections tab, which are accounts rather
	// than registry rows (connectcaps.go). It hangs here so that the cursor
	// walk, the scroll, the pointer and the hover need to know nothing about
	// them: an item is an item, and only what DRAWS it and what ANSWERS it ask
	// which kind this one is.
	conn *connRow
}

func (i sheetItem) heading() bool { return i.head != "" }

// sheet is the panel's whole state. The zero value is closed and costs the
// frame nothing.
type sheet struct {
	open bool
	tab  int

	registry *config.Settings
	// conns is the door onto the accounts, for the Connections tab. It is the
	// surface's own door (app.conns) and not a second one: two readings of "is
	// this connected" is how a tab and a panel disagree about somebody's mail.
	conns Connections
	// conn is what that tab remembers between builds (connectcaps.go).
	conn connTab
	rows []config.Setting
	// defaults is every row's reading on a profile nobody has touched, so a row
	// that differs from it can be marked. See [settingDefaults].
	defaults map[string]string

	// items is the current list — one tab's rows, or every tab's matches under
	// their headings while a search is on. cursor indexes it and skips headings.
	items  []sheetItem
	cursor int
	top    int

	// query is the type-to-search box. It filters across ALL tabs; the tab bar
	// follows the first match so that leaving the search leaves you where the
	// thing you found lives.
	query editor

	// edit is the text submenu and sel the select submenu. At most one is open,
	// and while one is, it owns the keyboard.
	edit *sheetEdit
	sel  *sheetSelect

	// msg is the last refusal, in the registry's own words.
	msg string
}

// sheetEdit is the one-line text submenu.
type sheetEdit struct {
	key    string
	label  string
	secret bool
	box    editor
}

// sheetSelect is a model slot being answered: which registry row is being
// written, what it is called on the panel, and THE PICKER ITSELF — the same
// [picker] /model opens, not a copy of it.
//
// It was a list of id strings with a substring filter over it, which is to say
// a second, worse picker: no ranking, and rows that said nothing about the
// models they named. A person choosing which model does the careful work is
// asking the same three questions they ask in /model — how much does it hold,
// what does it cost, is it any good — and the answer is one component with two
// entry points.
type sheetSelect struct {
	key   string
	label string
	// keep is the QUESTION THIS ROW ASKS of a model (models.go's [modelFilter]),
	// chosen from the key by [filterFor]. It is held rather than applied and
	// forgotten because it is the row's own meaning: "looking" is not a slot
	// that happens to have been opened over a shorter list, it is a slot that
	// only models which can see may answer.
	keep modelFilter
	pick picker
}

// filterFor is the WHOLE map from a settings row to the question its picker
// asks, and it is deliberately one function: a second slot with a modality of
// its own is one case here, and a slot nobody thought about gets the general
// chat law rather than the whole catalog.
//
// THE FIVE MEDIA SLOTS ARE THE REASON THIS IS A MAP AND NOT AN IF. Every one of
// them was answered with [chatModel], which is not "a list that was too wide" —
// it is the exact complement of the right list, so "drawing" offered a picker in
// which no row could draw. The slot words are [config.ModelSlots]'s own
// (modelslots.go's mediaSlotWords), and the predicates are models.go's; a sixth
// modality is one line in each place and no new list anywhere.
func filterFor(key string) modelFilter {
	switch key {
	case config.KeyVisionModel:
		return inspectsImages
	case config.ModelSettingKey("image"):
		return drawsImages
	case config.ModelSettingKey("speech"):
		return speaksAloud
	case config.ModelSettingKey("music"):
		return composesMusic
	case config.ModelSettingKey("video"):
		return filmsVideo
	case config.ModelSettingKey("voice"):
		return hearsSpeech
	}
	return chatModel
}

// choice is the id under the cursor. It answers a STRING and not a [Model]
// because that is what the registry writer takes: a slot row holds an id.
func (s *sheetSelect) choice() (string, bool) {
	model, ok := s.pick.choice()
	return model.ID, ok
}

// ── opening, and the registry behind it ─────────────────────────────────────

// pristineProfile is a profile directory that does not exist, and is never
// created. [config] answers a missing config.json with an empty map, so a
// registry built over it reads every row's built-in default — which is exactly
// the comparison the changed-mark needs, without this package having to know
// what any default IS or where the file lives.
var pristineProfile = filepath.Join(os.TempDir(), "openaf-settings-defaults-do-not-create")

// settingDefaults is every row as a profile nobody has touched reads it.
func settingDefaults() map[string]string {
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: pristineProfile})
	rows := registry.Rows()
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value()
	}
	return out
}

// registry is the settings this panel edits: the one the door handed over, or
// one built here over the profile directory it named.
//
// The two live seams it wires are the two this surface can honestly answer. The
// conversation model is the model in the status line, and setting it is the
// same road /model takes ([app.switchModel]) — one door, one effect. Every
// other slot is answered somewhere this process cannot reach, and it says WHERE
// ([app.slotRefusal]) rather than writing a preference nothing here would read.
func (a *app) registry() *config.Settings {
	if a.settings != nil {
		return a.settings
	}
	a.settings = config.NewSettings(config.SettingsOptions{
		ProfileDir: a.profileDir,
		ModelValue: func(slot string) string {
			if slot == talkSlot {
				return a.model
			}
			return ""
		},
		SetModel: func(slot, slug string) error {
			if slot != talkSlot {
				return a.slotRefusal(slot)
			}
			a.switchModel(slug, 0)
			return nil
		},
		Applied: func(string) { a.touch() },
	})
	return a.settings
}

// talkSlot is the model slot this surface is: the conversation.
const talkSlot = "talk"

// slotRefusal is what a slot this surface cannot write answers, and it NAMES
// THE PLACE. The old sentence — "that model is chosen where its session is
// opened" — is true of the role slots and simply wrong about the five media
// ones: those are environment slots ([config.Setting.EnvDefault]), and a person
// told to open a session to change the drawing model has been sent to a door
// that does not exist. A refusal that cannot say where the value lives is a
// refusal that leaves somebody stuck, which is the one thing the registry's
// plain-language rule is for.
func (a *app) slotRefusal(slot string) error {
	// A ROLE belongs to a session and a MEDIA slot belongs to the environment,
	// which is the split [config.ModelSlot] already draws: a capability slot
	// carries no role. The environment variable is read off the registry row
	// rather than repeated here, so the sentence cannot name a variable the
	// registry has since renamed.
	if media, ok := config.ModelSlotFor(slot); ok && media.Role == "" && a.settings != nil {
		if row, found := a.settings.Row(config.ModelSettingKey(media.Slot)); found && row.EnvDefault != "" {
			return fmt.Errorf("%s is set with %s", media.Label, row.EnvDefault)
		}
	}
	return fmt.Errorf("that model is chosen where its session is opened")
}

// openSettings is /settings and ctrl+,.
func (a *app) openSettings() {
	a.sheet = sheet{
		open:     true,
		registry: a.registry(),
		conns:    a.conns,
		defaults: settingDefaults(),
	}
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
	a.touch()
}

func (a *app) closeSettings() {
	a.sheet = sheet{}
	a.touch()
}

func (s *sheet) searching() bool { return strings.TrimSpace(s.query.String()) != "" }

// build rebuilds the item list from the tab and the query.
//
// With no query it is one tab's rows in registry order. With one it is every
// tab's matches, each group under its own faint heading, and THE TAB FOLLOWS
// THE FIRST MATCH: a person who typed three letters and found the thing has
// already been told which tab it lives on, so backing the search out leaves
// them there instead of back where they started.
func (s *sheet) build() {
	s.items = s.items[:0]
	query := strings.ToLower(strings.TrimSpace(s.query.String()))
	// THE CONNECTIONS TAB BUILDS ITS OWN ROWS, from the engine rather than from
	// the registry (connectcaps.go). It is one branch and no second list: what
	// it appends is [sheetItem]s, so everything downstream of here — the cursor,
	// the window, the pointer, the hover — is the code that was already there.
	if s.onConnections() {
		s.buildConnections()
		return
	}
	if query == "" {
		for _, row := range s.rows {
			meta, ok := settingMetaFor(row)
			if !ok || meta.tab != settingTabs[s.tab] {
				continue
			}
			s.items = append(s.items, sheetItem{row: row, meta: meta})
		}
		s.cursor = s.clampCursor(s.cursor)
		return
	}
	first := -1
	for tab, title := range settingTabs {
		start := len(s.items)
		for _, row := range s.rows {
			meta, ok := settingMetaFor(row)
			if !ok || meta.tab != title || !settingMatches(row, meta, query) {
				continue
			}
			if len(s.items) == start {
				s.items = append(s.items, sheetItem{head: title})
			}
			s.items = append(s.items, sheetItem{row: row, meta: meta})
		}
		if len(s.items) > start && first < 0 {
			first, s.tab = start+1, tab
		}
	}
	if first >= 0 {
		s.cursor = first
	} else {
		s.cursor = 0
	}
	s.top = 0
}

// settingMatches is the search: the label, the key and the one-line description,
// case-folded, substring. The KEY is in it deliberately — a person who knows
// the registry knows "spendRail" and should not have to guess what it is called
// in the product's words.
func settingMatches(row config.Setting, meta settingMeta, query string) bool {
	for _, field := range []string{meta.label, row.Key, meta.about, row.Label} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// clampCursor keeps the cursor on a row and never on a heading.
func (s *sheet) clampCursor(at int) int {
	if len(s.items) == 0 {
		return 0
	}
	if at < 0 {
		at = 0
	}
	if at >= len(s.items) {
		at = len(s.items) - 1
	}
	if !s.items[at].heading() {
		return at
	}
	for i := at; i < len(s.items); i++ {
		if !s.items[i].heading() {
			return i
		}
	}
	for i := at; i >= 0; i-- {
		if !s.items[i].heading() {
			return i
		}
	}
	return at
}

// move walks the list by rows, stepping over the headings rather than landing
// on them: a heading is a label, and a cursor on a label is a cursor on nothing
// enter could do.
func (s *sheet) move(delta int) {
	if len(s.items) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	at := s.cursor
	for n := 0; n < abs(delta); n++ {
		next := at
		for {
			next += step
			if next < 0 || next >= len(s.items) {
				next = at
				break
			}
			if !s.items[next].heading() {
				break
			}
		}
		at = next
	}
	s.cursor = at
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// current is the row under the cursor.
func (s *sheet) current() (sheetItem, bool) {
	if s.cursor < 0 || s.cursor >= len(s.items) || s.items[s.cursor].heading() {
		return sheetItem{}, false
	}
	return s.items[s.cursor], true
}

// changed reports whether this row differs from a untouched profile's reading.
//
// Two kinds are skipped and the reason is the same for both: their reading
// comes from a LIVE SEAM rather than from the file — the model slots answer
// from the running session, the divider from the surface's own preference — so
// the pristine registry, which has no seams, would report every one of them as
// changed. A mark that is on for rows nobody touched is not a mark.
func (s *sheet) changed(item sheetItem) bool {
	switch item.row.Kind {
	case config.SettingModel, config.SettingPercent:
		return false
	}
	was, known := s.defaults[item.row.Key]
	return known && was != item.row.Value()
}

// ── the keyboard ────────────────────────────────────────────────────────────

// sheetKey routes one keypress while the panel is up. It reports whether it
// took the key; only ctrl+c is read before it (input.go), because leaving is
// never modal.
// It hands back a COMMAND as well, for the one row on this sheet whose answer
// leaves the process: a service on the Connections tab that is not connected
// yet starts the same browser trip /connect starts, and a sign-in is a thing
// that reaches the network (connectcaps.go).
func (a *app) sheetKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.sheet.open {
		return nil, false
	}
	s := &a.sheet
	defer a.touch()
	switch {
	case s.edit != nil:
		a.sheetEditKey(msg)
		return nil, true
	case s.sel != nil:
		a.sheetSelectKey(msg)
		return nil, true
	case s.conn.entry != nil:
		// AND THE KEY BOX ON THE ACCOUNTS TAB, on the same terms as the two
		// above it: a box that has the keyboard has ALL of it. Every other key
		// on this sheet types into the search box, and a surface that let a
		// pasted key narrow a list would be a surface putting half a secret in
		// the title bar (connectcaps.go).
		return a.connEntryKey(msg), true
	}

	switch msg.String() {
	case "esc":
		// esc backs out one layer at a time: the search first, then whatever the
		// tab on show has standing open — a confirmation, an expanded service —
		// and the panel after all of it. A key that closed the whole sheet from
		// inside a search would throw away the only thing on screen the person
		// typed, and one that closed it over an open account would take the page
		// away instead of the thing they were looking at.
		if s.searching() {
			s.query.reset()
			s.build()
			return nil, true
		}
		if a.connEsc() {
			return nil, true
		}
		a.closeSettings()
		return nil, true

	case "left", "shift+tab":
		s.tabBy(-1)
	case "right", "tab":
		s.tabBy(1)

	case "up", "ctrl+p":
		s.move(-1)
	case "down", "ctrl+n":
		s.move(1)
	case "pgup":
		s.move(-sheetRows)
	case "pgdown":
		s.move(sheetRows)
	case "home":
		s.cursor = s.clampCursor(0)
	case "end":
		s.cursor = s.clampCursor(len(s.items) - 1)

	case "enter", " ", "space":
		return a.activate(), true

	case "backspace":
		s.query.deleteBackward()
		s.build()
	case "ctrl+u":
		s.query.reset()
		s.build()
	case "ctrl+w":
		s.query.deleteWord()
		s.build()

	default:
		if text := msg.Key().Text; text != "" && text != " " {
			s.query.insert(text)
			s.build()
		}
	}
	return nil, true
}

// tabBy switches tabs, clamping rather than wrapping — the same rule every list
// on this surface walks by (palette.go). A search is dropped by it: the tabs
// and the search are two ways of asking the same question, and answering both
// at once would show a tab's name over rows from five of them.
func (s *sheet) tabBy(delta int) {
	if s.searching() {
		s.query.reset()
	}
	s.tab = moveCursor(s.tab, delta, len(settingTabs))
	s.cursor, s.top, s.msg = 0, 0, ""
	// A confirmation does not survive the page it was asked on: leaving the tab
	// is as much a way of not answering it as moving off the row is
	// (connectcaps.go). What stays open is the SERVICE, because coming back to a
	// tab you left half-read and finding it collapsed is the tab forgetting
	// where you were.
	s.conn.armed = false
	s.build()
}

// activate is enter on a row: flip it, cycle it, or open the submenu that
// answers it — and on the Connections tab, open an account, walk one of its
// answers, or start a sign-in, which is the one of them that needs a command.
func (a *app) activate() tea.Cmd {
	s := &a.sheet
	item, ok := s.current()
	if !ok {
		return nil
	}
	if item.conn != nil {
		return a.connAct(item.conn)
	}
	s.msg = ""
	switch item.meta.widget {
	case widgetToggle:
		next := "on"
		if item.row.Value() == "on" {
			next = "off"
		}
		a.applySetting(item, next)

	case widgetCycle:
		choices := item.row.Choices
		if len(choices) == 0 {
			return nil
		}
		at := 0
		for i, choice := range choices {
			if choice == item.row.Value() {
				at = (i + 1) % len(choices)
				break
			}
		}
		a.applySetting(item, choices[at])

	case widgetSelect:
		// The picker opens ON the id the row currently holds, the way /model
		// opens on the model in use: enter with nothing typed confirms rather
		// than changes. A row holding its empty label ("follows the
		// conversation") matches no id and the cursor stays at the top, which is
		// the honest reading of "this slot has not been set".
		sel := &sheetSelect{
			key: item.row.Key, label: item.meta.label,
			keep: filterFor(item.row.Key),
		}
		// The list is resolved through the SLOT'S OWN question, not through the
		// chat list narrowed afterwards ([app.modelsFor]). The predicate is
		// handed to the picker as well because that is the door every slot comes
		// through, and a second application of the same filter is a no-op.
		sel.pick.startFor(a.modelsFor(sel.keep), item.row.Value(), sel.keep)
		s.sel = sel

	default:
		value := item.row.Value()
		if value == item.row.EmptyLabel {
			// The empty label is what the row SAYS when it holds nothing
			// ("none", "follows the conversation"). Putting that word in the box
			// would offer the person a value to edit that they never set.
			value = ""
		}
		box := editor{}
		box.setText(value)
		s.edit = &sheetEdit{
			key: item.row.Key, label: item.meta.label,
			secret: item.row.Secret, box: box,
		}
	}
	return nil
}

// applySetting writes one row and keeps whatever the registry said about it.
func (a *app) applySetting(item sheetItem, raw string) {
	if err := item.row.Apply(raw); err != nil {
		a.sheet.msg = err.Error()
		return
	}
	a.sheet.msg = ""
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
}

// sheetEditKey drives the text submenu. enter saves, an empty box clears the
// row, esc leaves it exactly as it was.
func (a *app) sheetEditKey(msg tea.KeyPressMsg) {
	s := &a.sheet
	edit := s.edit
	switch msg.String() {
	case "esc":
		s.edit = nil
	case "enter":
		row, ok := s.registry.Row(edit.key)
		s.edit = nil
		if !ok {
			return
		}
		meta, _ := settingMetaFor(row)
		a.applySetting(sheetItem{row: row, meta: meta}, edit.box.String())
	case "backspace":
		edit.box.deleteBackward()
	case "delete":
		edit.box.deleteForward()
	case "ctrl+u":
		edit.box.killToStart()
	case "ctrl+w":
		edit.box.deleteWord()
	case "left", "ctrl+b":
		edit.box.left()
	case "right", "ctrl+f":
		edit.box.right()
	case "home", "ctrl+a":
		edit.box.home()
	case "end", "ctrl+e":
		edit.box.end()
	default:
		if text := msg.Key().Text; text != "" {
			edit.box.insert(text)
		}
	}
}

// sheetSelectKey drives the model picker while a slot row owns it. Only the two
// keys that MEAN something different here are handled: esc leaves the row as it
// was, and enter writes the chosen id through the registry rather than switching
// the conversation. Everything else — the walk, the scroll, the filter — is
// [picker.navigate], the same code /model runs.
func (a *app) sheetSelectKey(msg tea.KeyPressMsg) {
	s := &a.sheet
	sel := s.sel
	switch msg.String() {
	case "esc":
		s.sel = nil
	case "enter":
		chosen, ok := sel.choice()
		row, found := s.registry.Row(sel.key)
		s.sel = nil
		if !ok || !found {
			return
		}
		meta, _ := settingMetaFor(row)
		a.applySetting(sheetItem{row: row, meta: meta}, chosen)
	default:
		sel.pick.navigate(msg)
	}
}

// ── the pointer ─────────────────────────────────────────────────────────────

// sheetHitKind is what one screen row of the panel answers to a click.
type sheetHitKind uint8

const (
	sheetHitNone sheetHitKind = iota
	sheetHitTabs
	sheetHitRow
	// sheetHitOption is one row of the select submenu; index is its position in
	// that submenu's own hits.
	sheetHitOption
)

type sheetHit struct {
	kind  sheetHitKind
	index int
}

// sheetPress is a click inside the panel: a tab word switches tabs, a row
// selects and answers, anything else does nothing. It hands back a command for
// the reason [app.sheetKey] does — a sign-in reaches the network.
func (a *app) sheetPress(x, y int) tea.Cmd {
	if a.sheet.conn.entry != nil {
		// A BOX BEING TYPED INTO IS NOT A LIST. Every press is swallowed and none
		// of them acts — esc is the way out, which is the way out of every box on
		// this surface (connectpanel.go's [app.connectPanelPress] says it first).
		return nil
	}
	width, height := a.size()
	_, hits, _, _ := a.sheetFrame(width, height)
	if y < 0 || y >= len(hits) {
		return nil
	}
	switch hit := hits[y]; hit.kind {
	case sheetHitTabs:
		if tab, ok := tabAtColumn(x); ok {
			if a.sheet.searching() {
				a.sheet.query.reset()
			}
			a.sheet.tab = tab
			a.sheet.cursor, a.sheet.top, a.sheet.msg = 0, 0, ""
			a.sheet.conn.armed = false
			a.sheet.build()
			a.touch()
		}
	case sheetHitRow:
		// A click selects, and a click on the row already selected answers it.
		// One press cannot do both: a toggle that flipped the moment a pointer
		// touched it would change a setting the person was only reading — and on
		// the Connections tab it would be an account disconnected by a pointer
		// that was passing through.
		if a.sheet.cursor != hit.index {
			a.sheet.cursor = hit.index
			a.touch()
			return nil
		}
		cmd := a.activate()
		a.touch()
		return cmd

	case sheetHitOption:
		if a.sheet.sel == nil {
			return nil
		}
		if a.sheet.sel.pick.cursor != hit.index {
			a.sheet.sel.pick.cursor = hit.index
			a.touch()
			return nil
		}
		a.sheetSelectKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		a.touch()
	}
	return nil
}

// sheetHover records which row the pointer is over, repainting only when the
// answer changed (hover.go's rule, applied to the panel).
func (a *app) sheetHover(y int) {
	width, height := a.size()
	_, hits, _, _ := a.sheetFrame(width, height)
	next := hoverAt{}
	if y >= 0 && y < len(hits) && hits[y].kind == sheetHitRow {
		next = hoverAt{kind: hoverSheet, index: hits[y].index}
	}
	if next == a.hot {
		return
	}
	a.hot = next
	a.touch()
}

// ── the frame ───────────────────────────────────────────────────────────────

// sheetFrame is the whole screen while the panel is open: exactly height rows,
// what each of them answers to the pointer, and where the caret sits.
//
// It is ONE function for the reason [app.chrome] is: the frame draws these rows
// and the pointer resolves against them, and two answers to "where is the tab
// bar" is how a click lands on the wrong tab.
func (a *app) sheetFrame(width, height int) ([]string, []sheetHit, int, int) {
	s := &a.sheet
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]sheetHit, 0, height)
	add := func(text string, hit sheetHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}
	caretX, caretY := 0, 0

	add(sheetTitle(width, s, pal), sheetHit{})
	add("", sheetHit{})
	add(sheetTabBar(width, s.tab, pal), sheetHit{kind: sheetHitTabs})
	add(pal.dim(rule(width)), sheetHit{})
	add("", sheetHit{})

	// The foot is three rows and it is spoken for before the list is: a rule, a
	// line for what the panel has to say, and the keys.
	const foot = 3
	room := height - len(lines) - foot
	if room < 1 {
		room = 1
	}

	if s.sel != nil {
		body, at := s.selectLines(width, room, pal, a.reasoningFor)
		for i, line := range body {
			hit := sheetHit{}
			if at[i] >= 0 {
				hit = sheetHit{kind: sheetHitOption, index: at[i]}
			}
			add(line, hit)
		}
	} else {
		body, owner := s.listLines(width, pal, a.hoveredSheetRow())
		at := s.cursorLine(owner)
		// THE CURSOR'S ROW IS SCROLLED IN WHOLE. At [tierPhone] it is two lines —
		// the name and the value under it — and a window that pinned only the
		// first would push the value off the bottom edge, leaving a selection
		// band with one end cut off and the fact being changed off screen. The
		// last line is pinned first and the first line second, so a row taller
		// than the window still shows its name.
		if last := s.cursorLastLine(owner, at, width); last != at {
			s.top = listTop(last, s.top, len(body), room)
		}
		s.top = listTop(at, s.top, len(body), room)
		for i := 0; i < room; i++ {
			index := s.top + i
			if index >= len(body) {
				add("", sheetHit{})
				continue
			}
			hit := sheetHit{}
			if owner[index] >= 0 {
				hit = sheetHit{kind: sheetHitRow, index: owner[index]}
			}
			add(body[index], hit)
		}
	}

	add(pal.dim(rule(width)), sheetHit{})
	switch {
	case s.edit != nil:
		box, column := s.editLine(width, pal)
		caretX, caretY = column, len(lines)
		add(box, sheetHit{})
	case s.sel != nil:
		box, column := s.filterLine(width, pal)
		caretX, caretY = column, len(lines)
		add(box, sheetHit{})
	case s.msg != "":
		add(" "+pal.bad(fit(s.msg, width-2)), sheetHit{})
	default:
		add(" "+pal.dim(fit(s.footNote(), width-2)), sheetHit{})
	}
	add(" "+pal.dim(fit(s.keysLine(), width-2)), sheetHit{})

	// A terminal too short for the whole panel keeps its head and its foot:
	// what this is, and how to leave.
	if len(lines) > height {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
		caretY = height - 2
	}
	return lines, hits, caretX, caretY
}

func rule(width int) string {
	if width < 1 {
		return ""
	}
	return strings.Repeat("─", width)
}

// sheetTitle is the head: what this is on the left, how to leave on the right,
// and — while a search is on — what was typed, because a filtered list with no
// visible query is a list that lost rows for no reason a reader can see.
func sheetTitle(width int, s *sheet, pal palette) string {
	left := " " + pal.bold(pal.ink("settings"))
	plainLeft := " settings"
	if query := strings.TrimSpace(s.query.String()); query != "" {
		// THE BOX IS NAMED FOR WHAT IT DOES ON THE PAGE YOU ARE ON. Everywhere
		// else it searches the registry across the tabs; on the accounts tab it
		// narrows the catalog in front of you (connectcaps.go), and a heading
		// that called that a search would be promising a jump it will not make.
		word := "search · "
		if s.onConnections() {
			word = "filter · "
		}
		left += pal.dim("  " + word + query)
		plainLeft += "  " + word + query
	}
	right := "esc close"
	gap := width - ansi.StringWidth(plainLeft) - len(right) - 1
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + pal.dim(right)
}

// tabSpan is where one tab's CHIP sits on the bar, so the render and the click
// agree about it. It covers the chip's padding as well as its word: the cell
// beside "Context" is part of Context, because a one-cell miss between two words
// is a miss people make and a bar that answered it with nothing would be a bar
// that has to be aimed at.
type tabSpan struct{ from, to int }

const (
	// tabGap is what is left between two chips once each carries its own air.
	tabGap = 1
	// tabPad is that air, one cell each side — the same chip the task strip
	// wears (taskstrip.go), because the two rows are the same object in two
	// places and a person should not have to learn it twice.
	tabPad     = " "
	tabPadCols = 2
	// tabLead is the bar's left margin, which every other line of this panel
	// keeps as well.
	tabLead = 1
)

func tabSpans() []tabSpan {
	spans := make([]tabSpan, 0, len(settingTabs))
	at := tabLead
	for _, title := range settingTabs {
		width := len(title) + tabPadCols
		spans = append(spans, tabSpan{from: at, to: at + width})
		at += width + tabGap
	}
	return spans
}

func tabAtColumn(x int) (int, bool) {
	for i, span := range tabSpans() {
		if x >= span.from && x < span.to {
			return i, true
		}
	}
	return 0, false
}

// sheetTabBar is the one place this panel spends the accent: the tab you are
// on. Everything else on the bar is dim, which is what makes the one word read
// as a position rather than as a menu of five shouting words.
//
// THE ACCENT NOW ARRIVES ON A BAND, and the band is why the chips are padded.
// Five words in a row with one of them brighter is a sentence with an emphasis
// in it; five padded chips with one of them filled is a tab bar, and this panel
// IS a tab bar — the same object the task strip is, drawn the same way, so that
// "which page am I on" is one visual question across the app rather than two
// (taskstrip.go's [app.stripLabel]).
//
// There is one cursor here and not two. The tab a person has focused is the tab
// that is open — moving the focus switches the page — so the bar has nothing to
// say that the band does not already say, and it draws no second mark.
func sheetTabBar(width, active int, pal palette) string {
	line, plain := strings.Repeat(" ", tabLead), strings.Repeat(" ", tabLead)
	for i, title := range settingTabs {
		if i > 0 {
			line += strings.Repeat(" ", tabGap)
			plain += strings.Repeat(" ", tabGap)
		}
		chip := tabPad + title + tabPad
		if i == active {
			line += pal.band(pal.bold(pal.accent(chip)), len(title)+tabPadCols)
		} else {
			line += pal.dim(chip)
		}
		plain += chip
	}
	if ansi.StringWidth(plain) > width {
		return fit(line, width)
	}
	return line
}

// listLines is the rows, plus the ONE description this panel ever shows: the
// selected row's. omp shows a line under every row; this surface does not,
// because twenty-eight rows each carrying a sentence is a wall, and the
// sentence a person needs is the one about the row they are on.
//
// It returns the item each line belongs to (-1 for a heading or a gap), which
// is what the pointer resolves against.
func (s *sheet) listLines(width int, pal palette, hover int) ([]string, []int) {
	lines := make([]string, 0, len(s.items)+8)
	owner := make([]int, 0, len(s.items)+8)
	put := func(text string, at int) {
		lines = append(lines, text)
		owner = append(owner, at)
	}
	if len(s.items) == 0 {
		// The Connections tab has its own two sentences, because "nothing
		// matches" is an answer about a search and this tab is not searched
		// (connectcaps.go).
		word := "nothing matches"
		if s.onConnections() {
			word = s.connEmptyWord()
		}
		put(pal.dim("  "+word), -1)
		return lines, owner
	}
	for i, item := range s.items {
		if item.heading() {
			if len(lines) > 0 {
				put("", -1)
			}
			put(pal.dim("  "+item.head), -1)
			continue
		}
		// AN ACCOUNT IS A BLOCK AND A BLOCK HAS AIR OVER IT (connectcaps.go).
		// The blank belongs to no row, exactly as a heading's does, so the
		// pointer over it acts on nothing and the cursor cannot land on it.
		if item.conn != nil && item.conn.air && len(lines) > 0 {
			put("", -1)
		}
		for _, line := range s.rowLines(item, i == s.cursor, i == hover, width, pal) {
			put(line, i)
		}
		if i != s.cursor {
			continue
		}
		// THE ONE DESCRIPTION THIS PANEL EVER SHOWS is the selected row's, and
		// on the accounts tab it is the selected SERVICE'S — a catalog row's own
		// line about what connecting it buys, drawn under the row a person has
		// stopped on and under no other. Most rows of that tab have none: a
		// sentence explaining "read your mail" would be this surface saying the
		// same thing twice (connectcaps.go's [connAbout]).
		about := item.meta.about
		if item.conn != nil {
			about = connAbout(item.conn)
		}
		if about == "" {
			continue
		}
		for n, line := range wrap(about, width-6) {
			if n >= 2 {
				break
			}
			put("    "+pal.dim(line), i)
		}
	}
	return lines, owner
}

// cursorLine is the display row the cursor's item starts on.
func (s *sheet) cursorLine(owner []int) int {
	for i, at := range owner {
		if at == s.cursor {
			return i
		}
	}
	return 0
}

// cursorLastLine is the display row the cursor's ROW ends on — the wrapped
// value's line at [tierPhone], and the row itself everywhere else.
//
// It stops at the row and does not walk the whole item: the selected setting's
// description follows it under the same owner, and pinning that into the window
// would scroll the list by two rows the moment somebody moved the cursor.
func (s *sheet) cursorLastLine(owner []int, at, width int) int {
	if phoneList(width) && at+1 < len(owner) && owner[at+1] == s.cursor {
		return at + 1
	}
	return at
}

// changedMark is the one cell that says "you chose this". A glyph and not a
// colour, because the accent is already spent on the tab and the palette's own
// rule is that a distinction drawn in colour is drawn in text too (styles.go).
const changedMark = "•"

// rowLines is one setting: its name, and the value it is at. The two share a
// line on any frame with room for both and split at [tierPhone], where a value
// like "anthropic/claude-sonnet-4.5  set by AFORGE_MODEL" is the whole of what
// the row is about and the first thing a narrow row used to cut (palette.go's
// [overlayLines]). The pair stays ONE item to the pointer and to the cursor —
// [sheet.listLines] hands both lines the same owner.
func (s *sheet) rowLines(item sheetItem, selected, hovered bool, width int, pal palette) []string {
	if item.conn != nil {
		return s.connRowLines(item.conn, selected, hovered, width, pal)
	}
	value := item.row.Value()
	if value == "" {
		value = "—"
	}
	if s.changed(item) {
		mark := changedMark
		if pal.ascii {
			mark = "*"
		}
		value = mark + " " + value
	}
	if name, pinned := item.row.PinnedBy(); pinned {
		value += "  set by " + name
	}
	return overlayLines(item.meta.label, value, selected, false, hovered, width, pal)
}

// selectLines draws the model picker in the list's place — LITERALLY the picker's
// own rows ([picker.rows]), so a slot row offers the window, the price and the
// arena score the /model overlay offers, and the row in use is marked the same
// way. It returns each line's position in the picker's hits, or -1, so the
// pointer reaches it too.
//
// level is [app.reasoningFor], threaded through for the same reason the overlay
// threads it: the effort a model has been dialled to lives on the agent, and a
// list is not a thing that holds a session.
func (s *sheet) selectLines(width, room int, pal palette, level func(string) string) ([]string, []int) {
	sel := s.sel
	// The picker hands back the hit each LINE belongs to rather than a count to
	// add to its top: a row at [tierPhone] is two lines, and "line i is hit
	// top+i" would put every click one row further down the list than the one
	// that was pressed (palette.go's [picker.rowsOwned]).
	out, owner := sel.pick.rowsOwned(width, room, pal, -1, level)
	for len(out) < room {
		out = append(out, "")
		owner = append(owner, -1)
	}
	return out, owner
}

// editLine is the text submenu's box, and the column its caret sits in.
func (s *sheet) editLine(width int, pal palette) (string, int) {
	edit := s.edit
	shown := edit.box.String()
	if edit.secret {
		shown = strings.Repeat("•", len([]rune(shown)))
		if pal.ascii {
			shown = strings.Repeat("*", len([]rune(shown)))
		}
	}
	lead := " " + edit.label + " "
	line := " " + pal.dim(edit.label) + " " + pal.accent(prompt) + pal.ink(fit(shown, width-len(lead)-3))
	return line, ansi.StringWidth(lead+prompt) + ansi.StringWidth(shown)
}

// filterLine is the picker's filter box, drawn where the panel's foot line is.
func (s *sheet) filterLine(width int, pal palette) (string, int) {
	text := s.sel.pick.filter.String()
	lead := " " + s.sel.label + " "
	line := " " + pal.dim(s.sel.label) + " " + pal.accent(prompt) + pal.ink(fit(text, width-len(lead)-3))
	return line, ansi.StringWidth(lead+prompt) + ansi.StringWidth(text)
}

// footNote is what the panel says when it has nothing to complain about: where
// the writes land. It is one sentence and it is the truth people most often
// want from a settings panel they share between machines.
func (s *sheet) footNote() string {
	// The Connections tab writes somewhere else and answers a different
	// question, so it says its own line (connectcaps.go).
	if s.onConnections() {
		return s.connFootNote()
	}
	if item, ok := s.current(); ok {
		if name, pinned := item.row.PinnedBy(); pinned {
			return "held by " + name + " — unset it to change this here"
		}
	}
	return "saved to your profile · a project's own .openaf/config.json is a hand edit"
}

func (s *sheet) keysLine() string {
	switch {
	case s.edit != nil:
		return "enter save · empty clears · esc cancel"
	case s.sel != nil:
		return "↑↓ move · enter choose · esc cancel · type to filter"
	case s.onConnections():
		return s.connKeysLine()
	default:
		return "↑↓ move · ←→ tabs · enter change · type to search · esc close"
	}
}

// hoveredSheetRow is the item the pointer is over, or -1.
func (a *app) hoveredSheetRow() int {
	if a.hot.kind == hoverSheet {
		return a.hot.index
	}
	return -1
}
