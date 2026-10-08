package session

// THE FOREMAN'S ONE DOOR: `factory_floor`, THE FLOOR READ WHOLE.
//
// The foreman is the floor's own conversation (`m` on the factory floor,
// cmd/codeaf's factory_foreman.go): the product-level judgment about what to
// take first, never the runner. It reads the floor through this one tool and
// proposes by SELECTING items; the person runs what is selected with `L` on
// the floor. ONLY THE FOREMAN CARRIES IT: cmd/codeaf sets [Config.Floor] on
// the conversation whose session file the floor names as its foreman and on
// no other, so an ordinary chat proposes through `factory_add` and an item's
// own conversation through `factory_item`.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - IT READS AND IT MARKS, AND NOTHING ELSE. There is no argument that
//     launches, stops, ships, posts or changes an item. A selection (the field is `mark`) is a proposal
//     the person sees on the floor as an accent lead, and takes or leaves.
//
//   - ONLY A NEW ITEM TAKES A MARK. The door refuses anything else, and the
//     tool says which items it left alone and why, so the model never believes
//     it selected a stream.
//
//   - ITEMS ARE NAMED BY THEIR REF. The table, the marks and the answers all
//     speak `#4`, which is what the person reads on the floor; the store's own
//     ids never reach the model.
//
//   - A CAPABILITY WITH NOTHING BEHIND IT IS ABSENT. With no [Config.Floor]
//     the tool is off the belt (every conversation but the foreman, --once,
//     a task node, a stage, --host, --at).

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// FloorDoor is the foreman's door onto the factory floor: the floor as the
// page reads it, and the floor's own marks. Both may touch disk; the tool calls
// them from its own goroutine, never from a surface's loop.
type FloorDoor interface {
	Snapshot(ctx context.Context) (factory.Snapshot, error)
	Mark(ctx context.Context, ids []int, on bool) error
}

// mayFloor says whether `factory_floor` belongs on this belt: there is a door
// behind it.
func (c Config) mayFloor() bool { return c.Floor != nil }

// floorTools is `factory_floor`, or nothing when there is no floor behind it.
func (a *Agent) floorTools() []bare.Tool {
	if !a.config.mayFloor() {
		return nil
	}
	return []bare.Tool{a.factoryFloorTool()}
}

// The asks the tool answers, spelled once for the schema and the switch.
const (
	floorAskWaiting  = "waiting"
	floorAskRisky    = "risky"
	floorAskCheapest = "cheapest"
	floorAskOldest   = "oldest"
	floorAskAll      = "all"
	floorAskItem     = "item"
)

// The floor's limits on one answer: how many rows a ranked ask lists, how many
// rows `all` lists before it says how many more there are, and how much of an
// item's body `item` quotes.
const (
	floorTopRows  = 10
	floorAllRows  = 50
	floorBodyCut  = 2000
	floorTitleCut = 80
)

// The sentences the model reads back. The manual quotes the handoff
// (internal/manual/chat/factory.md, `## the foreman — m`).
const (
	// FloorMarkedTail follows the refs a call selected: `selected #1 #4 #6 ·
	// press L on the floor to run them`.
	FloorMarkedTail = " · press L on the floor to run them"
	floorNothing    = "the floor has no items yet"
	floorNoneAsked  = "no items on the floor answer that ask"
)

const factoryFloorDescription = "Read the factory floor and propose what to take on by selecting items. " +
	"You are the floor's foreman: you read, rank and propose; the person runs what is selected with L on the floor; nothing you do here runs, ships or posts. " +
	"ask: waiting (items that need the person), risky, cheapest (ten, by estimate), oldest (ten), all, or item (one item in full: its read, facts, stages, question and body; set item). " +
	"item, mark and unmark take items by the number in their ref (#4 is 4). Only a new item can be selected; selections stay until the person runs or unselects them. The fields are named mark and unmark; the person calls them select and unselect. " +
	"Name items by their ref in everything you say."

func factoryFloorSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"ask":{"type":"string","enum":["` + strings.Join([]string{floorAskWaiting, floorAskRisky, floorAskCheapest, floorAskOldest, floorAskAll, floorAskItem}, `","`) + `"],"description":"What to read off the floor."},` +
		`"item":{"type":"integer","description":"With ask item: the number in the item's ref (#4 is 4)."},` +
		`"mark":{"type":"array","items":{"type":"integer"},"description":"Refs' numbers of new items to select for the person's next run."},` +
		`"unmark":{"type":"array","items":{"type":"integer"},"description":"Refs' numbers whose selection to take off."}` +
		`},"additionalProperties":false}`
}

// The gloss a person reads beside the call is what was asked.
func init() { glossField["factory_floor"] = "ask" }

// factoryFloorTool reads the floor, and marks or unmarks items on it.
func (a *Agent) factoryFloorTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_floor",
		Description: factoryFloorDescription,
		Schema:      json.RawMessage(factoryFloorSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Ask    string `json:"ask"`
				Item   int    `json:"item"`
				Mark   []int  `json:"mark"`
				Unmark []int  `json:"unmark"`
			}
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			ask := strings.ToLower(strings.TrimSpace(parsed.Ask))
			if ask == "" && len(parsed.Mark)+len(parsed.Unmark) == 0 {
				ask = floorAskAll
			}
			switch ask {
			case "", floorAskWaiting, floorAskRisky, floorAskCheapest, floorAskOldest, floorAskAll, floorAskItem:
			default:
				return "Invalid arguments: ask is one of waiting, risky, cheapest, oldest, all or item.", true, nil
			}
			if ask == floorAskItem && parsed.Item <= 0 {
				return "Invalid arguments: ask item needs item, the number in the item's ref.", true, nil
			}
			return a.floorRun(ctx, ask, parsed.Item, parsed.Mark, parsed.Unmark)
		},
	}
}

// floorRun does one call: the marks first, then the ask over the floor as it
// is after them, so a call that marks and reads sees its own marks.
func (a *Agent) floorRun(ctx context.Context, ask string, item int, mark, unmark []int) (string, bool, error) {
	door := a.config.Floor
	snap, err := door.Snapshot(ctx)
	if err != nil {
		return "the floor could not be read: " + oneLine(err.Error()), true, nil
	}
	var out []string
	if len(mark)+len(unmark) > 0 {
		said, failed := floorMarks(ctx, door, snap, mark, unmark)
		out = append(out, said...)
		if failed {
			return strings.Join(out, "\n"), true, nil
		}
		if ask != "" {
			if snap, err = door.Snapshot(ctx); err != nil {
				return strings.Join(out, "\n"), false, nil
			}
		}
	}
	if ask != "" {
		out = append(out, FloorAnswer(snap, ask, item))
	}
	return strings.Join(out, "\n\n"), false, nil
}

// floorMarks marks and unmarks through the door and says what happened in one
// line each: what was selected and the handoff, what was unselected, and every
// ref left alone with its reason. failed is a door that refused.
func floorMarks(ctx context.Context, door FloorDoor, snap factory.Snapshot, mark, unmark []int) (said []string, failed bool) {
	var notes []string
	resolve := func(nums []int, needNew bool) (ids []int, refs []string) {
		for _, n := range nums {
			found := floorFind(snap, n)
			switch {
			case len(found) == 0:
				notes = append(notes, fmt.Sprintf("there is no #%d on the floor", n))
			case len(found) > 1:
				notes = append(notes, fmt.Sprintf("#%d is on %d repositories (%s); left alone", n, len(found), floorRepos(found)))
			case needNew && found[0].State != factory.StateNew:
				notes = append(notes, fmt.Sprintf("%s is %s, and only a new item can be selected", found[0].Ref(), floorStateWords(found[0].State)))
			default:
				ids = append(ids, found[0].ID)
				refs = append(refs, found[0].Ref())
			}
		}
		return ids, refs
	}
	if ids, refs := resolve(unmark, false); len(ids) > 0 {
		if err := door.Mark(ctx, ids, false); err != nil {
			return append(said, "nothing unselected: "+oneLine(err.Error())), true
		}
		said = append(said, "unselected "+strings.Join(refs, " "))
	}
	if ids, refs := resolve(mark, true); len(ids) > 0 {
		if err := door.Mark(ctx, ids, true); err != nil {
			return append(said, "nothing selected: "+oneLine(err.Error())), true
		}
		said = append(said, "selected "+strings.Join(refs, " ")+FloorMarkedTail)
	}
	return append(said, notes...), false
}

// floorFind is every item on the floor whose ref is #n, dismissed ones left
// out.
func floorFind(snap factory.Snapshot, n int) []factory.Item {
	want := "#" + strconv.Itoa(n)
	var out []factory.Item
	for _, it := range snap.Items {
		if it.State != factory.StateDismissed && it.Ref() == want {
			out = append(out, it)
		}
	}
	return out
}

// floorStateWords is a state as a sentence says it: `#3 is running`, `#5 is
// waiting on the person`.
func floorStateWords(s factory.State) string {
	if s == factory.StateNeedsYou {
		return "waiting on the person"
	}
	return string(s)
}

func floorRepos(items []factory.Item) string {
	var names []string
	for _, it := range items {
		names = append(names, it.Repo)
	}
	return strings.Join(names, ", ")
}

// FloorAnswer is one ask's text over a floor: a compact table, one item a
// line, or one item in full for `item`.
func FloorAnswer(snap factory.Snapshot, ask string, item int) string {
	now := snap.Now
	if now.IsZero() {
		now = time.Now()
	}
	var open []factory.Item
	for _, it := range snap.Items {
		if it.State != factory.StateDismissed && it.State != factory.StateShipped {
			open = append(open, it)
		}
	}
	if len(open) == 0 {
		return floorNothing
	}
	if ask == floorAskItem {
		found := floorFind(snap, item)
		switch len(found) {
		case 0:
			return fmt.Sprintf("there is no #%d on the floor", item)
		case 1:
			return FloorItemText(snap, found[0], now)
		}
		var parts []string
		for _, it := range found {
			parts = append(parts, FloorItemText(snap, it, now))
		}
		return strings.Join(parts, "\n\n")
	}
	rows, head := open, ""
	switch ask {
	case floorAskWaiting:
		rows = floorKeep(open, func(it factory.Item) bool { return it.State == factory.StateNeedsYou })
		head = "waiting on the person"
	case floorAskRisky:
		rows = floorKeep(open, factory.Match("risky"))
		head = "risky"
	case floorAskCheapest:
		rows = floorKeep(open, func(it factory.Item) bool { return it.Triage.Est > 0 })
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Triage.Est < rows[j].Triage.Est })
		rows = floorTop(rows, floorTopRows)
		head = "cheapest first"
	case floorAskOldest:
		rows = floorKeep(open, func(it factory.Item) bool { return !it.Created.IsZero() })
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created.Before(rows[j].Created) })
		rows = floorTop(rows, floorTopRows)
		head = "oldest first"
	default:
		head = "every open item"
	}
	if len(rows) == 0 {
		return floorNoneAsked + " (" + head + ")"
	}
	var b strings.Builder
	b.WriteString(floorSummary(open, len(rows), head))
	more := 0
	if len(rows) > floorAllRows {
		more = len(rows) - floorAllRows
		rows = rows[:floorAllRows]
	}
	for _, it := range rows {
		b.WriteByte('\n')
		b.WriteString(FloorRow(it, now))
	}
	if more > 0 {
		fmt.Fprintf(&b, "\nand %d more", more)
	}
	return b.String()
}

// floorSummary is the table's head: what was asked, how many rows answer it,
// and the floor's counts that matter to a morning's choice, each left out
// when it is nothing.
func floorSummary(open []factory.Item, rows int, head string) string {
	parts := []string{head, strconv.Itoa(rows) + " " + floorPlural(rows, "item", "items")}
	count := func(keep func(factory.Item) bool) int {
		return len(floorKeep(open, keep))
	}
	if n := count(func(it factory.Item) bool { return it.State == factory.StateNew }); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" new on the floor")
	}
	if n := count(func(it factory.Item) bool { return it.State == factory.StateNeedsYou }); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" waiting on the person")
	}
	if n := count(func(it factory.Item) bool { return it.Marked && it.State == factory.StateNew }); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" selected")
	}
	return strings.Join(parts, " · ")
}

// FloorRow is one item in one line: its ref and title, then every fact the
// floor knows about it, each labelled, and NOTHING FOR WHAT IS NOT KNOWN (the
// emptiness law) — an item with no estimate says no estimate.
func FloorRow(it factory.Item, now time.Time) string {
	parts := []string{it.Ref() + " " + floorCut(oneLine(it.Title), floorTitleCut)}
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	add(string(it.Kind))
	add(it.Repo)
	if s := it.Triage.Size; s != "" {
		add("size " + s)
	}
	if it.Triage.Est > 0 {
		add("est " + floorDollars(it.Triage.Est))
	}
	add(floorPriority(it.Triage))
	state := string(it.State)
	if it.Marked && it.State == factory.StateNew {
		state += ", selected"
	}
	add(state)
	if age := floorAge(now, it.Created); age != "" {
		add(age + " old")
	}
	if len(it.Triage.Risk) > 0 {
		add("risk: " + strings.Join(it.Triage.Risk, ", "))
	}
	return strings.Join(parts, " · ")
}

// FloorItemText is one item in full: its row, then its read, its facts, its
// stages, its question and its body cut to [floorBodyCut] characters. Every
// line is left out when there is nothing to say.
func FloorItemText(snap factory.Snapshot, it factory.Item, now time.Time) string {
	var b strings.Builder
	line := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	line(FloorRow(it, now))
	if r := oneLine(it.Triage.Read); r != "" {
		line("read: " + r)
	}
	var facts []string
	fact := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			facts = append(facts, label+value)
		}
	}
	fact("type ", it.Triage.Type)
	fact("area ", it.Triage.Area)
	if it.Triage.Readiness > 0 {
		fact("readiness ", strconv.Itoa(it.Triage.Readiness))
	}
	fact("author ", it.Author)
	fact("tier ", string(it.Tier))
	fact("ask me at ", floorGateWord(it.Gate))
	if it.Cap > 0 {
		fact("budget ", floorDollars(it.Cap))
	}
	fact("may repeat ", it.Triage.Dup)
	if len(it.Labels) > 0 {
		fact("labels ", strings.Join(it.Labels, ", "))
	}
	fact("checks ", it.Checks)
	line(strings.Join(facts, " · "))
	for _, q := range it.Triage.Questions {
		if q = oneLine(q); q != "" {
			line("open question from the read: " + q)
		}
	}
	stages := it.Stages
	if len(stages) == 0 {
		if repo, ok := snap.RepoNamed(it.Repo); ok {
			stages = repo.Recipe.For(it.Kind)
		}
	}
	if lines := factory.StageLines(stages); len(lines) > 0 {
		line("stages:")
		for _, l := range lines {
			line(l)
		}
	}
	if q := oneLine(it.Question); q != "" {
		line("waiting on the person for: " + q)
	}
	if body := strings.TrimSpace(it.Body); body != "" {
		line("body:")
		line(floorCut(body, floorBodyCut))
	}
	return strings.TrimSpace(b.String())
}

func floorKeep(items []factory.Item, keep func(factory.Item) bool) []factory.Item {
	var out []factory.Item
	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out
}

func floorTop(items []factory.Item, n int) []factory.Item {
	if len(items) > n {
		return items[:n]
	}
	return items
}

// floorPriority is `p1 money is wrong`, or "" when the read gave none.
func floorPriority(t factory.Triage) string {
	if t.Priority <= 0 {
		return ""
	}
	p := "p" + strconv.Itoa(t.Priority)
	if r := oneLine(t.Reason); r != "" {
		p += " " + r
	}
	return p
}

// floorDollars is an amount the way the floor writes an estimate: cents, and
// a tenth of a cent under one.
func floorDollars(usd float64) string {
	if usd < 0.01 {
		return "$" + strconv.FormatFloat(usd, 'f', 4, 64)
	}
	return "$" + strconv.FormatFloat(usd, 'f', 2, 64)
}

// floorAge is how long ago at was, in the floor's own short words (`40m`,
// `5h`, `3d`), or "" when it is not known.
func floorAge(now, at time.Time) string {
	if at.IsZero() || now.Before(at) {
		return ""
	}
	d := now.Sub(at)
	switch {
	case d < time.Hour:
		return strconv.Itoa(max(int(d/time.Minute), 1)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

func floorPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// floorCut cuts s to n characters, on a rune boundary, with an ellipsis.
func floorCut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// floorGateWord is a stored gate in the floor's screen words (`plan`,
// `pull request`, `never`), the same as the card's ([ItemGateWord]).
func floorGateWord(g factory.Gate) string {
	if g == "" {
		return ""
	}
	return ItemGateWord(string(g))
}
