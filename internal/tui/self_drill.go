package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

// A drill-in is always the same shape: the section's name, its explainer
// again, a counter that says how much there is against how much is on screen,
// and then rows. Every list goes through selfList, so the window, the filter,
// the week fold, and the empty state can only behave one way.

// CraftSummary is one learned workflow as Self lists it. The tui owns the
// shape so the craft repository stays a command-side dependency.
type CraftSummary struct {
	Name        string
	Description string
	Commit      string
	When        time.Time
	Steps       int
}

// CraftStep is one leaf of a workflow, flattened for reading.
type CraftStep struct {
	ID     string
	Brief  string
	Needs  []string
	Model  string
	Skill  string
	Verify string
}

// CraftVersion is one commit in a workflow's history.
type CraftVersion struct {
	Commit  string
	When    time.Time
	Subject string
}

// CraftDetail is one workflow opened: what it does, in what order, under what
// bounds, and how it got here.
type CraftDetail struct {
	Name        string
	Description string
	Commit      string
	Dir         string
	Steps       []CraftStep
	CostUSD     float64
	WallClock   time.Duration
	History     []CraftVersion
}

// selfListRow is one candidate row: what it shows, what a filter tests, what
// enter does, and whether it is old enough to fold.
type selfListRow struct {
	body  string
	match string
	row   selfRow
	older bool
}

func (m *Model) renderSelfDrill(width int) string {
	if m.selfRoute == selfRouteCrafts && m.selfCraftName != "" {
		return m.renderSelfCraftDetail(width)
	}
	if m.selfRoute == selfRoutePractice && m.selfPracticeKey != "" {
		return m.renderSelfPracticeDetail(width)
	}
	section := selfSectionFor(m.selfRoute)
	switch m.selfRoute {
	case selfRouteCrafts:
		return m.renderSelfList(section, m.selfCraftRows(), width)
	case selfRouteCompetence:
		return m.renderSelfList(section, m.selfCompetenceRows(), width)
	case selfRouteBeliefs:
		return m.renderSelfList(section, m.selfBeliefRows(width), width)
	case selfRouteSkills:
		return m.renderSelfList(section, m.selfSkillRows(), width)
	case selfRouteWatches:
		return m.renderSelfList(section, m.selfWatchRows(), width)
	case selfRouteServices:
		return m.renderSelfList(section, m.selfServiceRows(), width)
	case selfRoutePractice:
		return m.renderSelfList(section, m.selfPracticeRows(width), width)
	case selfRouteDials:
		return m.renderSelfDials(width)
	}
	return ""
}

// selfHeader is the two lines every drill-in opens with: where you are with a
// way back, and the same sentence the root row taught you.
func (m *Model) selfHeader(section selfSection, counter string, width int) []string {
	title := mutedStyle.Faint(true).Render("‹ self · ") + inputTextStyle.Render(section.title)
	lines := []string{truncate(overlayRight(title, mutedStyle.Faint(true).Render(counter), width), width)}
	for _, line := range strings.Split(wrapText(section.explain, max(8, width-2)), "\n") {
		lines = append(lines, truncate(mutedStyle.Faint(true).Render("  "+line), width))
	}
	return lines
}

func (m *Model) renderSelfList(section selfSection, rows []selfListRow, width int) string {
	total := len(rows)
	query := strings.ToLower(strings.TrimSpace(m.selfQuery))
	if query != "" {
		kept := rows[:0:0]
		for _, row := range rows {
			if strings.Contains(row.match, query) {
				kept = append(kept, row)
			}
		}
		rows = kept
	}

	// The week fold only applies to an unfiltered list: a query is already a
	// statement that the user wants the whole history looked at.
	older := 0
	if query == "" && !m.selfShowOlder {
		recent := rows[:0:0]
		for _, row := range rows {
			if row.older {
				older++
				continue
			}
			recent = append(recent, row)
		}
		rows = recent
	}

	shown := min(max(selfWindow, m.selfShown), len(rows))
	counter := selfCountLabel(total, shown, query != "")
	lines := m.selfHeader(section, counter, width)
	if query != "" {
		lines = append(lines, truncate(mutedStyle.Faint(true).Render("  filter · ")+
			inputTextStyle.Render(m.selfQuery)+powderStyle.Render("▏"), width))
	}
	lines = append(lines, "")

	if len(rows) == 0 {
		empty := section.empty
		if query != "" {
			empty = "nothing here matches “" + m.selfQuery + "”"
		}
		if empty == "" {
			empty = "nothing here yet"
		}
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  "+empty))
		lines = append(lines, "")
		appendSelfPlain(&lines, width, m.selfFooterHint(false, width))
		return strings.Join(lines, "\n")
	}

	openable := false
	for index := 0; index < shown; index++ {
		openable = openable || rows[index].row.action != selfRowInert
		m.appendSelfRow(&lines, width, rows[index].body, rows[index].row)
	}
	if more := len(rows) - shown; more > 0 {
		m.appendSelfRow(&lines, width,
			mutedStyle.Faint(true).Render(fmt.Sprintf("show %d more", min(more, selfWindowStep))),
			selfRow{action: selfRowShowMore})
	}
	if older > 0 {
		m.appendSelfRow(&lines, width,
			mutedStyle.Faint(true).Render(fmt.Sprintf("%d older · type to search", older)),
			selfRow{action: selfRowUnfold})
	}
	lines = append(lines, "")
	appendSelfPlain(&lines, width, m.selfFooterHint(openable, width))
	return strings.Join(lines, "\n")
}

// selfFooterHint promises only what this list can actually do. A row of
// competence scopes has nothing behind it, and offering "enter opens" there
// would be the surface lying about itself.
func (m *Model) selfFooterHint(openable bool, width int) string {
	hint := "  ↑/↓ move"
	if openable {
		hint += " · enter opens"
	}
	hint += " · esc back"
	if m.selfFilterable() {
		hint += " · type to filter"
	}
	return truncate(mutedStyle.Faint(true).Render(hint), width)
}

// selfCountLabel is the six-month line: how many there are, how many are on
// screen, and — past the read ceiling — an honest "+" instead of a number the
// surface never actually counted.
func selfCountLabel(total, shown int, filtered bool) string {
	count := strconv.Itoa(total)
	if total >= selfBeliefScan {
		count = strconv.Itoa(selfBeliefScan) + "+"
	}
	label := count + " · showing " + strconv.Itoa(shown)
	if filtered {
		label += " · filtered"
	}
	return label
}

// Crafts.

func (m *Model) selfCraftRows() []selfListRow {
	uses := m.craftUses()
	now := m.standingTime()
	rows := make([]selfListRow, 0, len(m.selfCrafts))
	for _, entry := range m.selfCrafts {
		parts := []string{fmt.Sprintf("%d step%s", entry.Steps, plural(entry.Steps))}
		if use, ok := uses[entry.Name]; ok && use.runs > 0 {
			parts = append(parts, fmt.Sprintf("%d run%s", use.runs, plural(use.runs)))
			parts = append(parts, fmt.Sprintf("%d/%d survived", use.survived, use.runs))
		} else {
			parts = append(parts, "not run yet")
		}
		if version := shortCommit(entry.Commit); version != "" {
			parts = append(parts, version)
		}
		if !entry.When.IsZero() {
			parts = append(parts, "changed "+standingAge(entry.When, now))
		}
		body := inputTextStyle.Render(entry.Name) + mutedStyle.Faint(true).Render(" · "+strings.Join(parts, " · "))
		rows = append(rows, selfListRow{
			body:  body,
			match: strings.ToLower(entry.Name + " " + entry.Description),
			row:   selfRow{action: selfRowOpenCraft, key: entry.Name},
		})
	}
	return rows
}

// craftUse is survival as the graph already recorded it: a craft run is a
// subtree whose root names the workflow, and its status is the measurement.
// Nothing new is written to learn this — the provenance was already there.
type craftUse struct {
	runs     int
	survived int
}

func (m *Model) craftUses() map[string]craftUse {
	byID := make(map[string]store.Node)
	for _, snapshot := range []store.Snapshot{m.cardSnapshot, m.snapshot} {
		for _, node := range snapshot.Nodes {
			if _, seen := byID[node.ID]; !seen {
				byID[node.ID] = node
			}
		}
	}
	uses := make(map[string]craftUse)
	for _, node := range byID {
		name := craftRefName(node.Provenance.Craft)
		if name == "" {
			continue
		}
		// Only the top of a compiled subtree counts: every step below it
		// carries the same reference and would multiply one run into many.
		if parent, ok := byID[node.Parent]; ok && craftRefName(parent.Provenance.Craft) == name {
			continue
		}
		use := uses[name]
		use.runs++
		if node.Status == store.Done {
			use.survived++
		}
		uses[name] = use
	}
	return uses
}

func craftRefName(reference string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(reference), "@")
	return name
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// openSelfCraft reads the craft repository the commander holds. Without a
// commander the Crafts row still renders — as an honest zero — rather than the
// place refusing to open, and a shelf that has never heard of this name says so
// with the bool rather than by not having the method.
func (m *Model) openSelfCraft(name string) {
	if m.commander == nil {
		return
	}
	detail, found := m.commander.CraftDetail(name)
	if !found {
		return
	}
	m.selfCraftName = name
	m.selfCraftDetail = detail
	m.selfSelection = 0
	m.self.SetYOffset(0)
	m.refreshSelf()
}

func (m *Model) renderSelfCraftDetail(width int) string {
	detail := m.selfCraftDetail
	title := mutedStyle.Faint(true).Render("‹ self · know-how · ") + inputTextStyle.Render(detail.Name)
	counter := mutedStyle.Faint(true).Render(shortCommit(detail.Commit))
	lines := []string{truncate(overlayRight(title, counter, width), width)}
	if description := strings.TrimSpace(detail.Description); description != "" {
		for _, line := range strings.Split(wrapText(description, max(8, width-2)), "\n") {
			appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  "+line))
		}
	}
	lines = append(lines, "")

	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  steps"))
	if len(detail.Steps) == 0 {
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("    none"))
	}
	for index, step := range detail.Steps {
		head := fmt.Sprintf("    %d. ", index+1)
		meta := make([]string, 0, 4)
		if len(step.Needs) > 0 {
			meta = append(meta, "needs "+strings.Join(step.Needs, ", "))
		}
		if step.Model != "" {
			meta = append(meta, step.Model)
		}
		if step.Skill != "" {
			meta = append(meta, "⚒ "+step.Skill)
		}
		if step.Verify != "" {
			meta = append(meta, "verify "+step.Verify)
		}
		line := mutedStyle.Faint(true).Render(head) + inputTextStyle.Render(step.ID)
		if len(meta) > 0 {
			line += mutedStyle.Faint(true).Render(" · " + strings.Join(meta, " · "))
		}
		appendSelfPlain(&lines, width, line)
		if brief := strings.TrimSpace(step.Brief); brief != "" {
			for _, wrapped := range strings.Split(wrapText(brief, max(8, width-8)), "\n") {
				appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("       "+wrapped))
			}
		}
	}

	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  limits"))
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render(fmt.Sprintf(
		"    ≤%s per run · ≤%s wall clock",
		formatSelfDollars(detail.CostUSD), shortDuration(detail.WallClock))))

	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  history"))
	if len(detail.History) == 0 {
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("    no versions recorded"))
	}
	now := m.standingTime()
	for _, version := range detail.History {
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("    ")+
			mutedStyle.Faint(true).Render(padANSI(shortCommit(version.Commit), 8))+
			inputTextStyle.Render(truncate(version.Subject, max(1, width-22)))+
			mutedStyle.Faint(true).Render(" · "+standingAge(version.When, now)))
	}
	if dir := strings.TrimSpace(detail.Dir); dir != "" {
		lines = append(lines, "")
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  kept in "+dir))
	}
	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  esc back"))
	return strings.Join(lines, "\n")
}

// Competence.

func (m *Model) selfCompetenceRows() []selfListRow {
	order := map[store.CompetenceClass]int{
		store.CompetenceFrontier: 0, store.CompetenceWeak: 1, store.CompetenceStrong: 2,
	}
	scopes := append([]store.ScopeCompetence(nil), m.selfCompetence.Scopes...)
	sort.SliceStable(scopes, func(i, j int) bool { return order[scopes[i].Class] < order[scopes[j].Class] })
	rows := make([]selfListRow, 0, len(scopes))
	for _, scope := range scopes {
		glyphStyle, glyph := mutedStyle.Faint(true), "·"
		if scope.Class == store.CompetenceFrontier {
			glyphStyle, glyph = peachStyle, "◆"
		}
		name := strings.TrimPrefix(scope.Scope, "profile:")
		if scope.Kind == store.CompetenceProfile {
			name += " work"
		}
		number := fmt.Sprintf("%d%% failed", int(scope.FailureRate*100+0.5))
		if scope.Samples == 0 {
			number = "0 runs"
		} else {
			number += fmt.Sprintf(" of %d", scope.Samples)
		}
		body := glyphStyle.Render(glyph) + " " + inputTextStyle.Render(name) +
			mutedStyle.Faint(true).Render(" · "+string(scope.Class)+" · "+number)
		rows = append(rows, selfListRow{body: body, match: strings.ToLower(name + " " + string(scope.Class))})
	}
	return rows
}

// Beliefs. The notebook is not rebuilt here — the drill-in draws each belief
// with the notebook's own row renderer, and its search is the notebook's BM25
// retrieval, so the two surfaces can never describe a belief differently.

func (m *Model) loadSelfBeliefs() {
	query := strings.TrimSpace(m.selfQuery)
	if query == "" {
		m.selfBeliefHits = nil
		return
	}
	if m.commander != nil {
		m.selfBeliefHits = m.commander.SearchNotebook(query, selfWindow*2)
	}
}

func (m *Model) selfBeliefRows(width int) []selfListRow {
	facts := m.selfFacts
	// A retrieved set is already ranked by relevance; presenting it through
	// the substring filter as well would drop the matches BM25 found by stem
	// rather than by spelling.
	if strings.TrimSpace(m.selfQuery) != "" && len(m.selfBeliefHits) > 0 {
		facts = m.selfBeliefHits
	}
	now := m.standingTime()
	rows := make([]selfListRow, 0, len(facts))
	for _, fact := range facts {
		body := notebookFactLine(m.icons, fact, now, max(1, width-2))
		rows = append(rows, selfListRow{
			body:  body,
			match: strings.ToLower(fact.Body + " " + fact.Scope + " " + string(fact.Kind)),
			older: !fact.Time.IsZero() && now.Sub(fact.Time) > selfFoldAge,
		})
	}
	return rows
}

// Skills.

func (m *Model) selfSkillRows() []selfListRow {
	now := m.standingTime()
	rows := make([]selfListRow, 0, len(m.selfSkills))
	for _, fact := range m.selfSkills {
		state := fact.Status
		if state == store.FactActive {
			state = "verified"
		}
		meta := []string{state}
		if artifact := strings.TrimSpace(fact.Artifact); artifact != "" {
			meta = append(meta, artifact)
		}
		if age := store.AgeLabel(fact.Time, now); age != "" {
			meta = append(meta, age)
		}
		body := mutedStyle.Faint(true).Render("⚒ ") +
			notebookFactStyle(fact).Render(firstLine(fact.Body)) +
			mutedStyle.Faint(true).Render(" · "+strings.Join(meta, " · "))
		rows = append(rows, selfListRow{
			body:  body,
			match: strings.ToLower(fact.Body + " " + fact.Artifact + " " + fact.Status),
			older: !fact.Time.IsZero() && now.Sub(fact.Time) > selfFoldAge,
		})
	}
	return rows
}

// Watches. The charter card, its firing history, and its actions already live
// on the board; this list says which watches exist and hands the selected one
// to the surface that owns it.

func (m *Model) selfWatchRows() []selfListRow {
	fired := make(map[string]standingCharter)
	for _, charter := range m.standingCharters() {
		fired[charter.ID] = charter
	}
	rows := make([]selfListRow, 0, len(m.selfCharters))
	for _, charter := range m.selfLiveCharters() {
		grade := "tenured"
		if charter.Autonomy != store.CharterTenured {
			grade = fmt.Sprintf("probation %d/%d",
				min(selfTenureAfter, max(0, charter.GreenFirings)), selfTenureAfter)
		}
		history := fired[charter.ID]
		last := "never"
		if !history.LastFired.IsZero() {
			last = history.LastFired.Local().Format("Jan 2 15:04")
		}
		name := charterShortName(charter.Invariant, charter.ID)
		meta := fmt.Sprintf(" · %s · last %s · %d today", grade, last, history.Today)
		if charter.Status == store.CharterProposed {
			meta += " · proposed"
		}
		rows = append(rows, selfListRow{
			body:  inputTextStyle.Render(name) + mutedStyle.Faint(true).Render(meta),
			match: strings.ToLower(charter.Invariant + " " + charter.ID),
			row:   selfRow{action: selfRowOpenCharter, key: charter.ID},
		})
	}
	return rows
}

// Services.

func (m *Model) selfServiceRows() []selfListRow {
	now := m.standingTime()
	services := m.activeServices()
	rows := make([]selfListRow, 0, len(services))
	for _, service := range services {
		state := "up " + serviceAge(service.StartedAt, now)
		if service.Status != store.ServiceRunning {
			state = string(service.Status)
		}
		body := inputTextStyle.Render(service.Name) +
			mutedStyle.Faint(true).Render(" · "+state+" · "+service.Health.Suffix())
		rows = append(rows, selfListRow{
			body:  body,
			match: strings.ToLower(service.Name),
			row:   selfRow{action: selfRowOpenService, key: service.ID},
		})
	}
	return rows
}

// Practice. The receipt log is where a flat surface fails first: the same goal
// is tried six times, and six full-width copies of one sentence are not a
// report, they are a wall. So attempts of one goal are one row — the goal
// clipped to its column, the count, the summed cost, and what came of it — and
// the row opens onto the attempts themselves.

// practiceGroup is repeated attempts at one goal, gathered. Grouping is a
// presentation decision, not a store one: the receipts stay one per attempt,
// and the detail view shows them as they were written.
type practiceGroup struct {
	key      string
	glyph    string
	goal     string
	kind     string
	attempts []store.SelfReceipt
	cost     float64
	last     time.Time
	learned  int
	surprise float64
	live     bool
	older    bool
}

// practicePrefixGlyphs replaces the machine word a receipt's origin carries
// with the glyph grammar the rest of the surface already uses. A row that
// starts with the literal text "revision:" is the internals leaking.
var practicePrefixGlyphs = map[string]string{
	"revision": "↻",
	"revise":   "↻",
	"practice": "◇",
	"trigger":  "⏱",
	"charter":  "⏱",
	"audit":    "⚖",
}

func practiceGoalText(raw string) (string, string) {
	text := oneLineSelfReceipt(raw)
	head, rest, found := strings.Cut(text, ":")
	if !found {
		return "·", text
	}
	glyph, known := practicePrefixGlyphs[strings.ToLower(strings.TrimSpace(head))]
	if !known {
		return "·", text
	}
	if trimmed := strings.TrimSpace(rest); trimmed != "" {
		return glyph, trimmed
	}
	return glyph, text
}

func (m *Model) practiceGroups() []practiceGroup {
	now := m.standingTime()
	groups := make([]practiceGroup, 0, 8)
	index := make(map[string]int, 8)

	for _, node := range m.practiceRoots() {
		if nodeSettled(node) {
			continue
		}
		label := m.nodeLabelIn(node, m.snapshot.Nodes)
		groups = append(groups, practiceGroup{
			key: "live:" + node.ID, glyph: "⠋", goal: label, kind: "practice", live: true, last: now,
		})
		index[strings.ToLower(label)] = len(groups) - 1
	}

	for position := len(m.selfReceipts) - 1; position >= 0; position-- {
		receipt := m.selfReceipts[position]
		source := receipt.Origin
		if strings.TrimSpace(source) == "" {
			source = receipt.Scope
		}
		glyph, goal := practiceGoalText(source)
		kind := "self work"
		if selfReceiptIsPractice(receipt, m.cardSnapshot, m.snapshot) {
			kind = "practice"
		}
		key := strings.ToLower(goal)
		at, ok := index[key]
		if !ok {
			groups = append(groups, practiceGroup{key: key, glyph: glyph, goal: goal, kind: kind})
			at = len(groups) - 1
			index[key] = at
		}
		group := &groups[at]
		group.attempts = append(group.attempts, receipt)
		group.cost += receipt.Cost
		group.learned += len(receipt.FactIDs) + len(receipt.SkillIDs)
		if receipt.SurpriseDelta != nil && *receipt.SurpriseDelta > group.surprise {
			group.surprise = *receipt.SurpriseDelta
		}
		if receipt.Time.After(group.last) {
			group.last = receipt.Time
		}
	}
	for position := range groups {
		group := &groups[position]
		group.older = !group.live && !group.last.IsZero() && now.Sub(group.last) > selfFoldAge
	}
	return groups
}

// practiceOutcome is the short column: what the attempts produced, in three
// words at most, because the column has to survive a narrow frame.
func practiceOutcome(group practiceGroup) string {
	switch {
	case group.live:
		return "running"
	case group.learned > 0:
		return fmt.Sprintf("%d learned", group.learned)
	case group.surprise > 0:
		return fmt.Sprintf("surprise −%.0f%%", 100*group.surprise)
	default:
		return "nothing yet"
	}
}

func (m *Model) selfPracticeRows(width int) []selfListRow {
	groups := m.practiceGroups()
	// Real columns, measured once for the whole list: the count and the cost
	// are right-aligned so the eye reads down them, and the goal takes what is
	// left rather than pushing everything else off the frame.
	countWidth, costWidth := 5, 8
	outcomeWidth := min(14, max(8, width/5))
	goalWidth := max(12, width-4-countWidth-costWidth-outcomeWidth-3)
	rows := make([]selfListRow, 0, len(groups))
	for _, group := range groups {
		glyph := mutedStyle.Faint(true).Render(group.glyph)
		if group.live {
			glyph = peachStyle.Render(spinnerFrames[m.spinnerFrame%len(spinnerFrames)])
		}
		count := ""
		if len(group.attempts) > 1 {
			count = "×" + strconv.Itoa(len(group.attempts))
		}
		cost := ""
		if group.cost > 0 {
			cost = formatSelfDollars(group.cost)
		}
		body := glyph + " " +
			inputTextStyle.Render(padANSI(truncate(group.goal, goalWidth), goalWidth)) +
			mutedStyle.Faint(true).Render(" "+padLeftANSI(count, countWidth)) +
			mutedStyle.Faint(true).Render(" "+padLeftANSI(cost, costWidth)) +
			mutedStyle.Faint(true).Render(" "+truncate(practiceOutcome(group), outcomeWidth))
		row := selfRow{key: group.key}
		if !group.live {
			row.action = selfRowOpenPractice
		}
		rows = append(rows, selfListRow{
			body:  body,
			match: strings.ToLower(group.goal + " " + group.kind),
			row:   row,
			older: group.older,
		})
	}
	return rows
}

func padLeftANSI(value string, width int) string {
	value = truncate(value, width)
	if gap := width - lipgloss.Width(value); gap > 0 {
		return strings.Repeat(" ", gap) + value
	}
	return value
}

func (m *Model) openSelfPractice(key string) {
	for _, group := range m.practiceGroups() {
		if group.key == key {
			m.selfPracticeKey = key
			m.selfSelection = 0
			m.self.SetYOffset(0)
			m.refreshSelf()
			return
		}
	}
}

// renderSelfPracticeDetail is the way in the flat log never had: the whole
// goal rather than its first sixty columns, and every attempt with its clock,
// its cost, and what it came back with.
func (m *Model) renderSelfPracticeDetail(width int) string {
	var group practiceGroup
	found := false
	for _, candidate := range m.practiceGroups() {
		if candidate.key == m.selfPracticeKey {
			group, found = candidate, true
			break
		}
	}
	if !found {
		m.selfPracticeKey = ""
		return m.renderSelfList(selfSectionFor(selfRoutePractice), m.selfPracticeRows(width), width)
	}

	counter := fmt.Sprintf("%d attempt%s · %s",
		len(group.attempts), plural(len(group.attempts)), formatSelfDollars(group.cost))
	title := mutedStyle.Faint(true).Render("‹ self · practice · ") +
		inputTextStyle.Render(practiceTitle(group.goal))
	lines := []string{truncate(overlayRight(title, mutedStyle.Faint(true).Render(counter), width), width)}
	lines = append(lines, "")
	// The whole goal, wrapped rather than clipped: the list column owed the
	// user a readable row, and this view owes them the sentence itself.
	for index, line := range strings.Split(wrapText(group.goal, max(8, width-6)), "\n") {
		prefix := "  " + group.glyph + " "
		if index > 0 {
			prefix = "    "
		}
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render(prefix)+inputTextStyle.Render(line))
	}
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("    "+group.kind))

	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  attempts"))
	now := m.standingTime()
	for _, receipt := range group.attempts {
		when := "—"
		if !receipt.Time.IsZero() {
			when = receipt.Time.Local().Format("Jan 2 15:04")
		}
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("    ")+
			mutedStyle.Faint(true).Render(padANSI(when, 14))+
			mutedStyle.Faint(true).Render(padLeftANSI(formatSelfDollars(receipt.Cost), 8)+"  ")+
			inputTextStyle.Render(truncate(selfReceiptLearning(receipt), max(8, width-28))))
		if scope := strings.TrimSpace(receipt.Scope); scope != "" && scope != group.goal {
			appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("      scope · "+truncate(scope, max(8, width-16))))
		}
	}

	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  outcome"))
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("    ")+
		inputTextStyle.Render(practiceOutcome(group))+
		mutedStyle.Faint(true).Render(" · last "+standingAge(group.last, now)))
	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  esc back"))
	return strings.Join(lines, "\n")
}

// Dials. Read-only on purpose: the sheet owns every knob, and a second editor
// is a second place for the value to be wrong.

func (m *Model) renderSelfDials(width int) string {
	section := selfSectionFor(selfRouteDials)
	lines := m.selfHeader(section, "", width)
	lines = append(lines, "")
	rows := m.selfDialRows()
	if len(rows) == 0 {
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  dials unavailable — no settings registry"))
	}
	labelWidth := min(settingsLabelWidth, max(12, width/3))
	for _, row := range rows {
		appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  ")+
			mutedStyle.Faint(true).Render(padANSI(truncate(row.Label, labelWidth-1), labelWidth))+
			inputTextStyle.Render(row.Value()))
		if hint := strings.TrimSpace(row.Hint); hint != "" {
			for _, line := range strings.Split(wrapText(hint, max(8, width-6)), "\n") {
				appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("      "+line))
			}
		}
	}
	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  change these in settings (⚙) · esc back"))
	return strings.Join(lines, "\n")
}

func (m *Model) selfDialRows() []config.Setting {
	if !m.settingsAvailable() {
		return nil
	}
	rows := make([]config.Setting, 0, 4)
	for _, group := range m.settingsRegistry.Groups() {
		if group.Title != config.CategoryLearning {
			continue
		}
		rows = append(rows, group.Rows...)
	}
	return rows
}

// practiceTitle names a detail view after its goal without the header
// becoming the goal. The list column already showed the whole first line.
func practiceTitle(goal string) string {
	short := firstWords(goal, selfDetailTitleWords)
	if len(strings.Fields(goal)) > selfDetailTitleWords {
		short += "…"
	}
	return short
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
