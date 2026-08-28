package session

// The settings hands: the person's own configuration, read back and changed.
//
// "Use deepseek for planning", "stop drawing timestamps", "make my daily budget
// five dollars" are all sentences a person says to the chat and, until now, had
// to go and do themselves in /settings. These two tools close that: the model
// can read the sheet and write one row of it, permanently, into the same
// config.json the panel writes, so the change survives a restart and shows in
// the panel next time it is opened.
//
// THE REGISTRY IS THE ONLY VOCABULARY. Everything here goes through
// internal/config's settings registry — a row is found by its own key, read
// through its own reader, and written through its own validated writer
// ([config.Setting.Apply], which is the exact call the panel makes). This file
// contains no key names, no json field names, no parsers and no defaults. It
// CANNOT write a raw value into the config file and it cannot invent a row: a
// key the registry does not have is a refusal that names the near misses, the
// way a good CLI answers a mistyped flag, and never a write to a key nobody
// declared. The reason is one-source-of-truth rather than tidiness — a settings
// tool that wrote json would be a second definition of every knob in the
// product, drifting the day somebody widens a validator.
//
// THE SAFETY ROWS ARE NOT SELF-SERVICE, and the list of them lives beside the
// rows in internal/config's selfservice.go rather than here
// ([config.Setting.SelfService]). The tool gate, the shell rules, the spend
// rails, the machine ceilings, the work audit, the attribution trailer and every
// credential row are refused with a sentence naming the row and pointing at
// /settings.
//
// TWO TOOLS AND NOT ONE WITH ACTIONS, and this is the one decision here worth
// arguing. Every other multi-verb hand on this belt — jobs, tasks, use_service —
// is one tool with an action argument, and the shape is better for a model to
// hold. It is the wrong shape here for a reason that has nothing to do with
// ergonomics: THE APPROVAL GATE KEYS ON THE TOOL NAME. internal/approval's rules
// are per tool (config.KeyToolApprovals is `read:allow, bash:prompt`), so a
// single `settings` tool carrying both verbs would offer the person exactly one
// answer for two different acts — "settings:allow" to stop being asked about
// reading their own configuration would silently hand over the right to rewrite
// it. Split, the read can sit on the builtin floor (cmd/aforge's
// v3BuiltinApprovals) where manual and jobs already sit, and every write goes to
// the person like any other acting tool. One tool would have made that
// impossible to express.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const settingsToolName = "settings"

const changeSettingToolName = "change_setting"

// conversationSlot is the one model slot this surface's own /model owns. It is
// named once because two places ask about it: the live reader that lets the
// conversation's row show what it is on, and the refusal that sends a write to
// /model instead.
const conversationSlot = "talk"

// THESE FOUR STRINGS ARE PROMPT TEXT, BILLED ON EVERY REQUEST OF EVERY TURN, and
// they are written for density accordingly: a rule is stated once, in the one
// place the model is reading when it has to obey it. The read's preamble used to
// gloss `key` and `search` in full and then the schema glossed them again; only
// the schema does now. Neither hand describes the other's arguments any more.
//
// Nothing about the LAW changed. The read still says it only reads and names the
// hand that writes. The write still says all three of the things a model has to
// know before it calls it: the write goes through the REGISTRY and never through
// a hand-edited config file, a list row is replaced whole rather than appended
// to, and the rows that restrain this session are refused on purpose. What left
// the write was the ENUMERATION of those rows and the sentence sending the person
// to /settings — [config.Setting.SelfServiceRefusal] says both, by name, at the
// moment a model actually tries one, which is the only moment either matters.
const settingsDescription = "The person's aforge settings, the rows /settings shows under the same keys. No arguments lists them all. It only reads; " + changeSettingToolName + " writes one."

const changeSettingDescription = "Change one aforge setting permanently: the settings registry's own validated write into the profile's config.json, never a hand-edited file, so it survives a restart. Call " + settingsToolName + " first for the exact key. A LIST row is REPLACED WHOLE - write the whole list back. Rows that restrain this session are refused on purpose."

const settingsSchemaJSON = `{"type":"object","properties":{"key":{"type":"string","description":"One registry key, read in full"},"search":{"type":"string","description":"Filter the listing by key, label or hint"}},"additionalProperties":false}`

const changeSettingSchemaJSON = `{"type":"object","properties":{"key":{"type":"string","description":"The row's exact registry key"},"value":{"type":"string","description":"New value as text; empty clears it to the default"}},"required":["key","value"],"additionalProperties":false}`

// settingsTools is the pair, or nothing at all inside a task node.
//
// NOT INSIDE A TASK NODE, however the node's profile directory is set. The
// node's agent is a copy of the conversation's config (task_run.go), so this is
// the gate that keeps a worktree worker out of the person's settings: a node
// runs with nobody watching, its notes reach no transcript, and a permanent
// change to the person's machine that they never saw made is the one outcome
// this pair must not be able to produce.
func (a *Agent) settingsTools() []bare.Tool {
	// THE EMPTY PROFILE DIRECTORY IS THE ORDINARY ONE, and gating on it turned
	// this pair off for very nearly everybody. Config.ProfileDir carries
	// AFORGE_PROFILE_DIR, which almost nobody sets, and every reader below it
	// treats "" as "the default location" — config.BudgetConfigPath("") answers
	// ~/.aforge/config.json, which is the same file /settings has always
	// written. So an empty string is not "there is no profile", it is "the
	// profile where it always is", and the tools belong on the belt either way.
	dir := strings.TrimSpace(a.config.ProfileDir)
	if a.config.InTask {
		return nil
	}
	// One registry, built once and shared by both hands. Every row reads its
	// value off disk at the moment it is asked ([config.Setting.Value]), so a
	// registry held across turns never goes stale — what is built once here is
	// the LIST of rows, which changes only when the binary does.
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: dir,
		// THE ONE LIVE SEAM, AND IT IS A READER. Without it the conversation's
		// model row reads BLANK — the registry has no way to ask a running
		// session what it is on — and a sheet whose first row is empty is a
		// sheet that looks broken. There is deliberately no writer beside it:
		// changing the model this conversation rides is /model's job, and the
		// row's own refusal says so ([modelSlotRefusal]).
		ModelValue: func(slot string) string {
			if slot != conversationSlot {
				return ""
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			return a.model
		},
		// AND THE ONE LIVE SEAM THAT IS A HAND. The `background checks` row is
		// the switch on this machine's own timer, and the row cannot be built
		// without the timer to read and turn (internal/config's backgroundRow).
		// This session already holds it — it is what the first standing item
		// installs — so handing it over is what makes "turn off the background
		// checks" a sentence the chat can act on rather than one it has to send
		// somebody to /settings for. A session with no ambient side hands nil
		// and the row is simply not there.
		BackgroundChecks: a.standingWatch(),
	})
	return []bare.Tool{a.settingsTool(registry), a.changeSettingTool(registry)}
}

// settingsTool is the read.
func (a *Agent) settingsTool(registry *config.Settings) bare.Tool {
	return bare.Tool{
		Name:        settingsToolName,
		Description: settingsDescription,
		Schema:      json.RawMessage(settingsSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Key    string `json:"key"`
				Search string `json:"search"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			if key := strings.TrimSpace(parsed.Key); key != "" {
				row, found := registry.Row(key)
				if !found {
					return unknownSettingKey(registry, key), true, nil
				}
				return settingDetail(row), false, nil
			}
			return settingListing(registry, parsed.Search), false, nil
		},
	}
}

// changeSettingTool is the write, and every refusal it can make is in it.
func (a *Agent) changeSettingTool(registry *config.Settings) bare.Tool {
	return bare.Tool{
		Name:        changeSettingToolName,
		Description: changeSettingDescription,
		Schema:      json.RawMessage(changeSettingSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			key := strings.TrimSpace(parsed.Key)
			if key == "" {
				return "Invalid arguments: key is required — the row's registry key, as the " + settingsToolName + " tool lists it", true, nil
			}
			row, found := registry.Row(key)
			if !found {
				return unknownSettingKey(registry, key), true, nil
			}
			// THE TWO REFUSALS, and they are different in kind. The first is a
			// rule — the row restrains this session and widening it is not this
			// session's to do. The second is a fact about the machinery — the
			// role model slots are held by a running engine and there is nothing
			// in the profile to write — and it says where the effect the model
			// was reaching for actually lives.
			if refusal := row.SelfServiceRefusal(); refusal != "" {
				return refusal, true, nil
			}
			if refusal := modelSlotRefusal(row); refusal != "" {
				return refusal, true, nil
			}
			before := row.Value()
			if err := row.Apply(parsed.Value); err != nil {
				// The registry's own wording, verbatim. It is the sentence the
				// panel shows for the same mistake, and a second phrasing of
				// "that's not a dollar amount" would be this file inventing a
				// vocabulary the product does not have.
				return err.Error(), true, nil
			}
			return a.settingChanged(registry, key, before), false, nil
		},
	}
}

// settingChanged is what a landed write says, in the two places it belongs.
//
// TWO LANES, ONE FACT, the arrangement harness_build.go states: the event is for
// the PERSON — a dim line in the transcript, now, saying which row moved and
// from what to what — and the returned sentence is for the MODEL, which has to
// be able to tell the person the same thing without asking again. A setting that
// changed silently is a setting nobody can trust, and the model's own summary is
// not the surface saying it happened.
//
// The new reading is taken from the registry rather than from what was written:
// a row normalizes ("5" becomes "$5", "ON" becomes "on"), and the person is owed
// the value that is actually in force.
func (a *Agent) settingChanged(registry *config.Settings, key, before string) string {
	row, found := registry.Row(key)
	if !found {
		// Unreachable — the row was found a moment ago and the list does not
		// change at runtime — but a lookup that answers nothing must not print
		// an empty value as though it were the new reading.
		return "changed " + key
	}
	after := row.Value()
	if after == before {
		a.noteSetting(fmt.Sprintf("settings · %s is already %s", row.Label, settingReading(after)))
		return fmt.Sprintf("%s (%s) was already %s; nothing changed.", row.Label, key, settingReading(after))
	}
	a.noteSetting(fmt.Sprintf("settings · %s · %s → %s", row.Label, settingReading(before), settingReading(after)))
	return fmt.Sprintf("%s (%s) is now %s, saved to the profile — it will still be set the next time aforge starts. It was %s.%s",
		row.Label, key, settingReading(after), settingReading(before), projectOverrideWarning(a.config.Workspace, key))
}

// settingReading is one value as a sentence can carry it. THE EMPTINESS LAW: a
// row with no reading and no empty label says "nothing", because a sentence
// ending in a blank is a sentence that lost a word.
func settingReading(value string) string {
	if strings.TrimSpace(value) == "" {
		return "nothing"
	}
	return value
}

// projectOverrideWarning is the one thing a successful write may still have to
// admit: the profile was written, and this repository answers the same row.
//
// The project layer outranks the profile (internal/config's projectconfig.go,
// law 1), so for the handful of rows a repository may answer the write lands in
// the file and changes nothing in this workspace. The panel says the same thing
// in its foot line; a tool that stayed quiet would leave the model reporting a
// change that is not in force. Only ten rows can ever hit this and only when the
// file exists, so an unreadable or missing project file simply says nothing.
func projectOverrideWarning(workspace, key string) string {
	if !config.ProjectKeyAllowed(key) {
		return ""
	}
	project, err := config.LoadProjectConfig(workspace)
	if err != nil || !project.Has(key) {
		return ""
	}
	return " This project answers that row too, in " + project.Path() + ", and a project's answer outranks the profile — so nothing changes here until that file is edited by hand."
}

// noteSetting puts one line in front of the person, on the turn's own fan-out.
//
// It is the turn's hub and not a standing lane because a settings change happens
// INSIDE a tool call, which is inside a turn by construction — the same read
// tools_connect.go's sendConnect makes, under the same lock, for the same
// reason. With nobody watching (a headless run, --once) there is no hub and the
// line is simply not drawn; the model still gets its sentence back.
func (a *Agent) noteSetting(text string) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub != nil {
		hub.send(Event{Kind: EventNotice, Text: text})
	}
}

// modelSlotRefusal is what a MODEL SLOT the profile does not hold answers, and
// the empty string for every other row.
//
// The five role slots — the conversation, planning, execution, verification,
// naming — are bindings a running engine holds rather than values in the
// config.json this door writes, which is why the panel refuses them too
// (internal/tui3's slotRefusal). Answering "model switching is unavailable here",
// which is what the registry would say, would be true and useless: it names no
// road. So this names the roads that exist — /model for the conversation, and
// the tier and pin rows for the auxiliary calls a person usually means when they
// say "use this model for planning".
//
// The five CAPABILITY slots — drawing, speaking, composing, filming, voice — are
// not refused: since docs/MULTIMODAL.md Decision 5 they are written into the
// profile by the registry and read back by the use-time resolver, so they are
// ordinary rows and this returns nothing for them.
func modelSlotRefusal(row config.Setting) string {
	if row.Kind != config.SettingModel || row.Slot == "" {
		return ""
	}
	slot, known := config.ModelSlotFor(row.Slot)
	if !known || slot.Role == "" {
		return ""
	}
	if slot.Slot == conversationSlot {
		return fmt.Sprintf("%q (%s) is the model this conversation is running on, which is not a row in the profile: change it with /model, or on the Providers tab of /settings.", row.Label, row.Key)
	}
	return fmt.Sprintf("%q (%s) is a binding the running session holds rather than a value in the profile, so neither this hand nor the /settings panel can write it. To send aforge's own auxiliary calls to a particular model, set %q (%s) or %q (%s), or pin one role in %q (%s).",
		row.Label, row.Key,
		"careful work", config.KeyTierHighModel,
		"small work", config.KeyTierLowModel,
		"pinned roles", config.KeyModelRoles)
}

// ── reading the sheet ───────────────────────────────────────────────────────

// settingListing is every row, or every row that matches a search, grouped the
// way the sheet groups them.
//
// One line per row and no column padding: the reader is a model, the categories
// are already the structure, and a hundred rows aligned into columns is a
// screenful of spaces bought at the prompt price. What each line carries is what
// a model needs to decide whether to look closer — the key it would name, the
// word the person would use, and what the row says right now.
func settingListing(registry *config.Settings, search string) string {
	needle := strings.ToLower(strings.TrimSpace(search))
	var out strings.Builder
	matched := 0
	for _, group := range registry.Groups() {
		var lines []string
		for _, row := range group.Rows {
			if needle != "" && !settingMatches(row, needle) {
				continue
			}
			lines = append(lines, "  "+settingLine(row))
		}
		if len(lines) == 0 {
			continue
		}
		matched += len(lines)
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(group.Title + "\n" + strings.Join(lines, "\n") + "\n")
	}
	if matched == 0 {
		return fmt.Sprintf("No setting mentions %q. Call %s with no arguments to see every row.", search, settingsToolName)
	}
	head := fmt.Sprintf("%d settings, as they read now. Change one with %s, naming the key on the left.\n\n", matched, changeSettingToolName)
	if needle != "" {
		head = fmt.Sprintf("%d settings mentioning %q. Change one with %s, naming the key on the left.\n\n", matched, search, changeSettingToolName)
	}
	return head + out.String()
}

// settingLine is one row of the listing: key, label, reading, and whatever is
// true about who may write it. Empty parts are dropped rather than printed as
// separators with nothing between them — the emptiness law, one line at a time.
func settingLine(row config.Setting) string {
	parts := []string{row.Key, row.Label}
	if value := strings.TrimSpace(row.Value()); value != "" {
		parts = append(parts, value)
	}
	if name, pinned := row.PinnedBy(); pinned {
		parts = append(parts, "set by "+name)
	}
	if !row.SelfService() {
		parts = append(parts, "yours to change")
	} else if modelSlotRefusal(row) != "" {
		parts = append(parts, "set elsewhere")
	}
	return strings.Join(parts, " · ")
}

// settingDetail is one row read in full: everything the panel would show if the
// cursor were on it, plus the one thing the panel does not have to say — whether
// this session may write it.
func settingDetail(row config.Setting) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s · %s · %s\n", row.Label, row.Key, row.Category)
	reading := settingReading(row.Value())
	if receipt := row.Receipt(); receipt != "" {
		reading += " · " + receipt
	}
	fmt.Fprintf(&out, "now: %s\ntakes: %s\n", reading, row.Accepts())
	if hint := strings.TrimSpace(row.Hint); hint != "" {
		out.WriteString(hint + "\n")
	}
	if name, pinned := row.PinnedBy(); pinned {
		fmt.Fprintf(&out, "%s is set in the environment by %s, so nothing can change it here.\n", row.Label, name)
		return out.String()
	}
	switch {
	case row.SelfServiceRefusal() != "":
		out.WriteString(row.SelfServiceRefusal() + "\n")
	case modelSlotRefusal(row) != "":
		out.WriteString(modelSlotRefusal(row) + "\n")
	default:
		fmt.Fprintf(&out, "I can change this one with %s.\n", changeSettingToolName)
	}
	return out.String()
}

// settingMatches is the search: the same four fields the panel's own type-to-
// search box matches on (internal/tui3's settings.go), so a word that finds a
// row in the panel finds it here.
func settingMatches(row config.Setting, needle string) bool {
	haystack := strings.ToLower(row.Key + " " + row.Label + " " + row.Hint + " " + row.Category)
	return strings.Contains(haystack, needle)
}

// ── the honest refusal ──────────────────────────────────────────────────────

// unknownSettingKey is what a key the registry does not have gets back.
//
// IT NAMES THE NEAR MISSES, which is the whole of the difference between a
// refusal a model can act on and one it will simply try again in different
// words. A key is a made-up thing to a model that has not read the listing —
// "budget", "daily_budget", "tools.approval_mode" are all sentences somebody
// would write — and answering "no such setting" alone spends another call to
// learn something this one already knew.
func unknownSettingKey(registry *config.Settings, asked string) string {
	near := nearSettingKeys(registry.Rows(), asked)
	if len(near) == 0 {
		return fmt.Sprintf("No setting is called %q. Call %s with no arguments to see every row, or with a search word to narrow it.", asked, settingsToolName)
	}
	return fmt.Sprintf("No setting is called %q. Did you mean %s? Call %s with no arguments to see every row.",
		asked, strings.Join(near, ", "), settingsToolName)
}

// nearSettingKeys are the rows a wrong key most plausibly meant, best first and
// at most four.
//
// The scoring is deliberately crude, because the failure it repairs is crude: a
// key is written wrong by dropping a segment (`budget`), by guessing a
// separator (`tools.approval_mode`), or by writing the label instead of the key
// (`daily budget`). Splitting both sides into words and counting overlaps
// catches all three, and a whole-string match on the key outranks any of them
// because a person who typed a real prefix meant that row. No edit distance:
// there is nothing here it would find that the words do not, and a fuzzy match
// that offered `task.model` for `task.audit` would be a suggestion that reads as
// confident and is wrong.
func nearSettingKeys(rows []config.Setting, asked string) []string {
	words := settingWords(asked)
	if len(words) == 0 {
		return nil
	}
	whole := strings.ToLower(strings.TrimSpace(asked))
	type hit struct {
		key   string
		score int
	}
	var hits []hit
	for _, row := range rows {
		key := strings.ToLower(row.Key)
		haystack := key + " " + strings.ToLower(row.Label)
		score := 0
		for _, word := range words {
			if strings.Contains(haystack, word) {
				score++
			}
		}
		if strings.Contains(key, whole) {
			score += 2
		}
		if score > 0 {
			hits = append(hits, hit{row.Key, score})
		}
	}
	// Stable by score, and the registry's own order underneath it: the rows are
	// built in the order a person reads them, and two equally plausible
	// suggestions offered in a different order on every call would be a refusal
	// nobody could learn from.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	keys := make([]string, 0, 4)
	for _, found := range hits {
		if len(keys) == 4 {
			break
		}
		keys = append(keys, found.key)
	}
	return keys
}

// settingWords splits a key or a label into the pieces both spellings share.
// One-character pieces are dropped: they match everything and mean nothing.
func settingWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' ' || r == '/'
	})
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) > 1 {
			words = append(words, field)
		}
	}
	return words
}
