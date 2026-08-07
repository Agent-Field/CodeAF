package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type cardState string

const (
	cardCompiling cardState = "compiling"
	cardWorking   cardState = "working"
	cardQuestion  cardState = "question"
	cardSettled   cardState = "settled"
)

type questionKind string

const (
	questionChoose  questionKind = "choose"
	questionConfirm questionKind = "confirm"
	questionText    questionKind = "text"

	stuckQuestionAfter = 2 * time.Minute
)

// jobCard is a presentation-only rollup of one top-level subtree. Everything
// here is rebuilt from durable messages, graph nodes, commands, and usage.
type jobCard struct {
	ID           string
	RootID       string
	State        cardState
	Title        string
	Ask          string
	Reading      string
	Receipt      string
	Latest       string
	QuestionKind questionKind
	Question     string
	Options      []questionOption
	Default      string
	AllowFree    bool
	QuestionAt   time.Time
	Outcome      string
	BirthSeq     int64
	CommandSeq   int64
	StartedAt    time.Time
	FinishedAt   time.Time
	Done         int
	Total        int
	Usage        store.JobUsage
	Messages     []store.Message
	Narration    []string
	Learning     []cardLearningMoment
	Parts        []cardPart
	Deliverable  *store.Message
	Failed       bool
}

type cardPart struct {
	NodeID  string
	Title   string
	Brief   string
	Status  store.Status
	Result  string
	TrialOf int64
}

type cardLearningMoment struct {
	Headline string
	Details  []string
}

// cardRow and cardPartRow map rendered lines back to the disclosure ladder.
// Dock rows are relative to the dock; chat rows are relative to chat content.
type cardRow struct {
	start  int
	end    int
	cardID string
	dock   bool
}

type cardPartRow struct {
	line   int
	cardID string
	nodeID string
	dock   bool
}

type cardCloseRow struct {
	line   int
	cardID string
	dock   bool
}

type questionOption struct {
	Number int
	Key    string
	Label  string
	Hint   string
	Reply  string
}

type cardOptionRow struct {
	line        int
	startX      int
	endX        int
	cardID      string
	optionIndex int
	dock        bool
}

type confirmOptionSpan struct {
	startX int
	endX   int
}

type questionComponent struct {
	Kind      questionKind
	Prompt    string
	Options   []questionOption
	Default   string
	AllowFree bool
}

type structuredQuestion struct {
	Kind      questionKind               `json:"kind"`
	Prompt    string                     `json:"prompt"`
	Options   []structuredQuestionOption `json:"options"`
	Default   any                        `json:"default"`
	AllowFree bool                       `json:"allowFree"`
}

type structuredQuestionOption struct {
	Key   any    `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// deriveJobCards is the living-card query. It intentionally accepts store
// values rather than a database handle: the TUI remains a replayable lens and
// tests can exercise the same derivation with a seeded store.
func deriveJobCards(
	sessionID string,
	snapshot store.Snapshot,
	messages []store.Message,
	pending []store.Command,
	usage map[string]store.JobUsage,
	commands map[int64]store.Command,
) []jobCard {
	byID := make(map[string]store.Node, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}

	rootOf := make(map[string]string, len(snapshot.Nodes))
	var resolveRoot func(string) string
	resolveRoot = func(nodeID string) string {
		if rootID, ok := rootOf[nodeID]; ok {
			return rootID
		}
		node, ok := byID[nodeID]
		if !ok || node.ID == store.RootID || node.Parent == "" {
			return ""
		}
		if node.Parent == store.RootID {
			rootOf[nodeID] = nodeID
			return nodeID
		}
		rootID := resolveRoot(node.Parent)
		rootOf[nodeID] = rootID
		return rootID
	}
	for nodeID := range byID {
		resolveRoot(nodeID)
	}

	nodesByRoot := make(map[string][]store.Node)
	for _, node := range snapshot.Nodes {
		if rootID := rootOf[node.ID]; rootID != "" {
			nodesByRoot[rootID] = append(nodesByRoot[rootID], node)
		}
	}
	messagesByRoot := make(map[string][]store.Message)
	for _, message := range messages {
		if message.NodeID == "" || (sessionID != "" && message.SessionID != "" && message.SessionID != sessionID) {
			continue
		}
		if rootID := rootOf[message.NodeID]; rootID != "" {
			messagesByRoot[rootID] = append(messagesByRoot[rootID], message)
		}
	}

	cards := make([]jobCard, 0, len(nodesByRoot)+len(pending))
	matchedCommands := make(map[int64]bool)
	for _, root := range snapshot.Nodes {
		if root.Parent != store.RootID ||
			(sessionID != "" && root.Provenance.SessionID != "" && root.Provenance.SessionID != sessionID) {
			continue
		}
		// A territory is the retrospective packing history, not a job anyone
		// asked for. The moment one formed mid-session it rendered as a
		// freshly settled card — the user saw a "finished task" they never
		// requested, wearing another job's digest. Organizational nodes
		// belong to the rail; cards are conversation.
		if root.Group == store.TerritoryGroup || root.Group == charterGroupMarker {
			continue
		}
		nodes := nodesByRoot[root.ID]
		card := jobCard{
			ID:        root.ID,
			RootID:    root.ID,
			State:     cardWorking,
			Title:     nodeLabel(root, root),
			Ask:       strings.TrimSpace(root.Provenance.Intent),
			Reading:   strings.TrimSpace(root.Brief),
			BirthSeq:  root.CreatedSeq,
			Usage:     usage[root.ID],
			Messages:  append([]store.Message(nil), messagesByRoot[root.ID]...),
			Failed:    root.Status == store.Failed || root.Status == store.Cancelled,
			StartedAt: root.StartedAt,
		}
		if card.Ask == "" {
			card.Ask = card.Reading
		}

		for _, node := range nodes {
			if nodeSettled(node) {
				card.Done++
			}
			if !node.StartedAt.IsZero() && (card.StartedAt.IsZero() || node.StartedAt.Before(card.StartedAt)) {
				card.StartedAt = node.StartedAt
			}
			if !node.FinishedAt.IsZero() && node.FinishedAt.After(card.FinishedAt) {
				card.FinishedAt = node.FinishedAt
			}
			result := strings.TrimSpace(node.Summary)
			if node.Status == store.Failed || node.Status == store.Cancelled {
				result = strings.TrimSpace(node.Error)
			}
			card.Parts = append(card.Parts, cardPart{
				NodeID:  node.ID,
				Title:   nodeLabel(node, root),
				Brief:   strings.TrimSpace(node.Brief),
				Status:  node.Status,
				Result:  firstLine(result),
				TrialOf: node.Provenance.TrialOf,
			})
		}
		card.Total = max(len(nodes), card.Usage.NodeCount)
		if card.Total == 0 {
			card.Total = 1
		}

		var fallbackLatest string
		for index := range card.Messages {
			message := card.Messages[index]
			if card.BirthSeq == 0 || (message.Seq != 0 && message.Seq < card.BirthSeq) {
				card.BirthSeq = message.Seq
			}
			switch {
			case message.Role == store.RoleAgent:
				if component, ok := readQuestionComponent(message.Body); ok {
					card.applyQuestion(component, message.Time)
					continue
				}
				line := firstLine(message.Body)
				if line != "" {
					card.Narration = append(card.Narration, line)
					card.Latest = line
				}
			case message.Role == store.RoleSystem && message.CommandSeq != 0:
				// Plan-progress posts are anchored to the job node and stamped
				// with their command. They are card state — the current line and
				// the running summary — never stream blocks.
				if line := firstLine(message.Body); line != "" {
					card.Narration = append(card.Narration, line)
					card.Latest = line
				}
			case message.Role == store.RoleSystem:
				if moment, ok := readLearningMoment(message.Body); ok {
					card.Learning = append(card.Learning, moment)
					continue
				}
				fallbackLatest = firstLine(message.Body)
			case message.Role != store.RoleUser:
				fallbackLatest = firstLine(message.Body)
			}
		}
		if card.Latest == "" {
			card.Latest = fallbackLatest
		}

		commandSeq := matchingCommandSeq(root, commands, matchedCommands)
		if commandSeq != 0 {
			matchedCommands[commandSeq] = true
			card.CommandSeq = commandSeq
		}
		for _, message := range messages {
			// The receipt is the command's un-anchored system post; node-anchored
			// posts with the same command seq are plan progress, not the receipt.
			if commandSeq != 0 && message.CommandSeq == commandSeq &&
				message.Role == store.RoleSystem && message.NodeID == "" {
				card.Receipt = strings.TrimSpace(message.Body)
			}
		}
		if command, ok := commands[commandSeq]; ok && card.StartedAt.IsZero() {
			card.StartedAt = command.Time
		}

		if subtreeCardSettled(nodes) {
			card.State = cardSettled
			card.Deliverable = cardDeliverable(root, card.Messages)
			switch {
			case card.Deliverable != nil:
				card.Outcome = firstLine(card.Deliverable.Body)
			case card.Failed:
				card.Outcome = firstLine(root.Error)
			default:
				card.Outcome = firstLine(root.Summary)
			}
			if card.Outcome == "" {
				card.Outcome = string(root.Status)
			}
		} else if card.Question != "" {
			card.State = cardQuestion
			card.Latest = firstLine(card.Question)
		} else if card.Latest == "" {
			card.Latest = firstLine(card.Reading)
		}
		cards = append(cards, card)
	}

	pendingCommands := make(map[int64]bool)
	for _, command := range pending {
		pendingCommands[command.Seq] = true
		if command.Kind != store.CommandSplice ||
			(sessionID != "" && command.SessionID != "" && command.SessionID != sessionID) ||
			matchedCommands[command.Seq] {
			continue
		}
		card := jobCard{
			ID:         fmt.Sprintf("command:%d", command.Seq),
			State:      cardCompiling,
			Title:      firstLine(command.Instruction),
			Ask:        strings.TrimSpace(command.Instruction),
			BirthSeq:   command.Seq,
			CommandSeq: command.Seq,
			StartedAt:  command.Time,
		}
		for _, message := range messages {
			if message.CommandSeq != command.Seq {
				continue
			}
			switch {
			case message.Role == store.RoleAgent:
				// The compiler's own words are how the ask was read.
				card.Reading = firstLine(message.Body)
			case message.Role == store.RoleSystem && message.NodeID != "":
				// Node-anchored plan progress: the compiling card's live status,
				// replacing in place tick by tick, accumulated for the expanded
				// running summary — never a thread block.
				if line := firstLine(message.Body); line != "" {
					card.Narration = append(card.Narration, line)
					card.Latest = line
				}
			case message.Role == store.RoleSystem:
				card.Receipt = strings.TrimSpace(message.Body)
			}
		}
		cards = append(cards, card)
	}

	for seq, command := range commands {
		if command.Kind != store.CommandSplice || command.Status != store.CommandRejected ||
			matchedCommands[seq] || pendingCommands[seq] ||
			(sessionID != "" && command.SessionID != "" && command.SessionID != sessionID) {
			continue
		}
		var question store.Message
		for _, message := range messages {
			if message.CommandSeq == seq && isQuestionMessage(message) {
				question = message
			}
		}
		if question.Body == "" {
			continue
		}
		answered := false
		for _, message := range messages {
			if message.Role == store.RoleUser && message.NodeID == "" && message.Seq > question.Seq {
				answered = true
				break
			}
		}
		if answered {
			continue
		}
		component, _ := readQuestionComponent(question.Body)
		cards = append(cards, jobCard{
			ID:           fmt.Sprintf("command:%d", seq),
			State:        cardQuestion,
			Title:        firstLine(command.Instruction),
			Ask:          strings.TrimSpace(command.Instruction),
			QuestionKind: component.Kind,
			Question:     component.Prompt,
			Options:      component.Options,
			Default:      component.Default,
			AllowFree:    component.AllowFree,
			QuestionAt:   question.Time,
			Latest:       firstLine(component.Prompt),
			BirthSeq:     command.Seq,
			CommandSeq:   command.Seq,
			StartedAt:    command.Time,
		})
	}

	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].BirthSeq == cards[j].BirthSeq {
			return cards[i].ID < cards[j].ID
		}
		return cards[i].BirthSeq < cards[j].BirthSeq
	})
	return cards
}

// matchingCommandSeq pairs a job root with the command that asked for it.
// Roots are visited in creation order and each command matches at most one
// root (used tracks consumption), so two identical asks in flight keep their
// own receipts instead of both attaching to the later command.
func matchingCommandSeq(root store.Node, commands map[int64]store.Command, used map[int64]bool) int64 {
	var matched int64
	for seq, command := range commands {
		if used[seq] || command.Kind != store.CommandSplice ||
			command.SessionID != root.Provenance.SessionID ||
			command.Instruction != root.Provenance.Intent ||
			(root.CreatedSeq != 0 && seq > root.CreatedSeq) {
			continue
		}
		if matched == 0 || seq < matched {
			matched = seq
		}
	}
	return matched
}

func subtreeCardSettled(nodes []store.Node) bool {
	if len(nodes) == 0 {
		return false
	}
	for _, node := range nodes {
		if !nodeSettled(node) {
			return false
		}
	}
	return true
}

func cardDeliverable(root store.Node, messages []store.Message) *store.Message {
	if !root.FinishedAt.IsZero() {
		// The deliverable is the FIRST root-anchored system message at or after
		// the finish — the landing itself. Later system posts (a detached
		// recalibration report, follow-up notes) must not replace the answer.
		for index := range messages {
			message := &messages[index]
			if message.NodeID == root.ID && message.Role == store.RoleSystem &&
				!isLearningMoment(message.Body) &&
				(message.Time.IsZero() || !message.Time.Before(root.FinishedAt)) {
				return message
			}
		}
	}
	if root.FinishedAt.IsZero() {
		for index := len(messages) - 1; index >= 0; index-- {
			message := &messages[index]
			if message.NodeID == root.ID && message.Role == store.RoleSystem && !isLearningMoment(message.Body) {
				return message
			}
		}
	}
	if strings.TrimSpace(root.Summary) == "" && strings.TrimSpace(root.Error) == "" {
		return nil
	}
	body := root.Summary
	if root.Status == store.Failed || root.Status == store.Cancelled {
		body = root.Error
	}
	return &store.Message{
		SessionID: root.Provenance.SessionID,
		Role:      store.RoleSystem,
		Body:      body,
		NodeID:    root.ID,
	}
}

func readLearningMoment(body string) (cardLearningMoment, bool) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return cardLearningMoment{}, false
	}
	headline := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(headline, "· learned — ") &&
		!strings.HasPrefix(headline, "· learned ") &&
		!strings.HasPrefix(headline, "⚒ forged: ") &&
		!strings.HasPrefix(headline, "⚖ settled: ") &&
		!strings.HasPrefix(headline, "· let go — ") {
		return cardLearningMoment{}, false
	}
	moment := cardLearningMoment{Headline: headline}
	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line != "" {
			moment.Details = append(moment.Details, line)
		}
	}
	return moment, true
}

func isLearningMoment(body string) bool {
	_, ok := readLearningMoment(body)
	return ok
}

func isQuestionMessage(message store.Message) bool {
	if message.Role != store.RoleAgent {
		return false
	}
	_, ok := readQuestionComponent(message.Body)
	return ok
}

func (card *jobCard) applyQuestion(component questionComponent, at time.Time) {
	card.QuestionKind = component.Kind
	card.Question = component.Prompt
	card.Options = component.Options
	card.Default = component.Default
	card.AllowFree = component.AllowFree
	card.QuestionAt = at
	card.Latest = firstLine(component.Prompt)
}

// readQuestionComponent prefers a structured JSON payload, whether it is the
// whole body, a fenced block, or an object embedded in explanatory prose. The
// numbered ▸ spelling remains the compatibility seam for already-landed heads.
func readQuestionComponent(body string) (questionComponent, bool) {
	for _, candidate := range jsonObjects(body) {
		var payload structuredQuestion
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			continue
		}
		component, ok := normalizeStructuredQuestion(payload)
		if ok {
			return component, true
		}
	}
	prompt, options := numberedQuestionPayload(body)
	if len(options) == 0 && !strings.HasSuffix(strings.TrimSpace(prompt), "?") {
		return questionComponent{}, false
	}
	kind := questionText
	if len(options) > 0 {
		kind = questionChoose
	}
	return questionComponent{Kind: kind, Prompt: prompt, Options: options, AllowFree: true}, true
}

// questionPayload is kept as a narrow test/compatibility helper for callers
// that only understand the original prompt-plus-options shape.
func questionPayload(body string) (string, []questionOption) {
	component, _ := readQuestionComponent(body)
	return component.Prompt, component.Options
}

func normalizeStructuredQuestion(payload structuredQuestion) (questionComponent, bool) {
	payload.Kind = questionKind(strings.ToLower(strings.TrimSpace(string(payload.Kind))))
	switch payload.Kind {
	case questionChoose, questionConfirm, questionText:
	default:
		return questionComponent{}, false
	}
	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		return questionComponent{}, false
	}
	options := make([]questionOption, 0, len(payload.Options))
	seen := make(map[string]bool)
	for index, rawOption := range payload.Options {
		key := strings.TrimSpace(fmt.Sprint(rawOption.Key))
		if rawOption.Key == nil || key == "<nil>" {
			key = ""
		}
		option := questionOption{
			Key:   key,
			Label: strings.TrimSpace(rawOption.Label), Hint: strings.TrimSpace(rawOption.Hint),
		}
		if option.Key == "" {
			option.Key = strconv.Itoa(index + 1)
		}
		if option.Label == "" || seen[option.Key] {
			continue
		}
		seen[option.Key] = true
		option.Number = len(options) + 1
		option.Reply = option.Key
		options = append(options, option)
	}
	if payload.Kind == questionText {
		options = nil
	} else if len(options) == 0 {
		return questionComponent{}, false
	}
	defaultKey := strings.TrimSpace(fmt.Sprint(payload.Default))
	if payload.Default == nil || defaultKey == "<nil>" {
		defaultKey = ""
	}
	return questionComponent{
		Kind: payload.Kind, Prompt: prompt, Options: options,
		Default: defaultKey, AllowFree: payload.AllowFree,
	}, true
}

// jsonObjects returns balanced object candidates while respecting quoted
// braces. Trying every object makes the reader tolerant of markdown fences and
// metadata envelopes without coupling the TUI to one head serialization.
func jsonObjects(body string) []string {
	var objects []string
	for start := 0; start < len(body); start++ {
		if body[start] != '{' {
			continue
		}
		depth := 0
		quoted, escaped := false, false
		for end := start; end < len(body); end++ {
			switch character := body[end]; {
			case escaped:
				escaped = false
			case quoted && character == '\\':
				escaped = true
			case character == '"':
				quoted = !quoted
			case !quoted && character == '{':
				depth++
			case !quoted && character == '}':
				depth--
				if depth == 0 {
					objects = append(objects, body[start:end+1])
					end = len(body)
				}
			}
		}
	}
	return objects
}

// numberedQuestionPayload reads the generic numbered-option spelling carried
// by the existing message body payload. Options may share a line or use one
// line each.
func numberedQuestionPayload(body string) (string, []questionOption) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	promptLines := make([]string, 0, len(lines))
	options := make([]questionOption, 0)
	seen := make(map[int]bool)
	for _, line := range lines {
		first := strings.Index(line, "▸")
		if first < 0 {
			promptLines = append(promptLines, line)
			continue
		}
		prefix := strings.TrimSpace(line[:first])
		rest := line[first:]
		parsed := make([]questionOption, 0)
		valid := true
		for rest != "" {
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "▸"))
			next := strings.Index(rest, "▸")
			segment := rest
			if next >= 0 {
				segment, rest = rest[:next], rest[next:]
			} else {
				rest = ""
			}
			option, ok := parseQuestionOptionSegment(segment)
			if !ok {
				valid = false
				break
			}
			parsed = append(parsed, option)
		}
		if !valid || len(parsed) == 0 {
			promptLines = append(promptLines, line)
			continue
		}
		if prefix != "" {
			promptLines = append(promptLines, prefix)
		}
		for _, option := range parsed {
			number, _ := strconv.Atoi(option.Key)
			if !seen[number] {
				seen[number] = true
				options = append(options, option)
			}
		}
	}
	return strings.TrimSpace(strings.Join(promptLines, "\n")), options
}

func parseQuestionOptionSegment(segment string) (questionOption, bool) {
	segment = strings.TrimSpace(segment)
	fields := strings.Fields(segment)
	if len(fields) < 2 {
		return questionOption{}, false
	}
	number, err := strconv.Atoi(strings.TrimRight(fields[0], ".):"))
	if err != nil || number < 1 {
		return questionOption{}, false
	}
	text := strings.TrimSpace(segment[len(fields[0]):])
	if text == "" {
		return questionOption{}, false
	}
	return questionOption{Number: number, Key: strconv.Itoa(number), Label: text, Reply: text}, true
}

func placeJobCards(cards []jobCard) (active, settled []jobCard) {
	for _, card := range cards {
		if card.State == cardSettled {
			settled = append(settled, card)
		} else {
			active = append(active, card)
		}
	}
	return active, settled
}

// dockJobCards promotes only stale unanswered questions. Fresh questions keep
// their conversational birth order, so the quiet signal appears only after the
// user has genuinely had time to miss one.
func dockJobCards(cards []jobCard, now time.Time) []jobCard {
	active, _ := placeJobCards(cards)
	sort.SliceStable(active, func(i, j int) bool {
		leftStale := questionIsStuck(active[i], now)
		rightStale := questionIsStuck(active[j], now)
		if leftStale != rightStale {
			return leftStale
		}
		if leftStale && !active[i].QuestionAt.Equal(active[j].QuestionAt) {
			return active[i].QuestionAt.Before(active[j].QuestionAt)
		}
		return false
	})
	return active
}

func questionIsStuck(card jobCard, now time.Time) bool {
	return card.State == cardQuestion && !card.QuestionAt.IsZero() &&
		now.Sub(card.QuestionAt) >= stuckQuestionAfter
}

func (m *Model) rebuildCards() {
	previous := m.cards
	next := deriveJobCards(m.sessionID, m.cardSnapshot, m.messages, m.pending, m.jobUsage, m.commands)
	for _, old := range previous {
		if old.CommandSeq == 0 {
			continue
		}
		for _, card := range next {
			if card.CommandSeq != old.CommandSeq || card.ID == old.ID {
				continue
			}
			if m.cardExpanded[old.ID] {
				m.cardExpanded[card.ID] = true
				delete(m.cardExpanded, old.ID)
			}
			if m.selectedCardID == old.ID {
				m.selectedCardID = card.ID
			}
			break
		}
	}
	m.cards = next
	if m.selectedCardID != "" && m.cardByID(m.selectedCardID) == nil {
		m.selectedCardID = ""
	}
	if m.focus != focusCards {
		return
	}
	if selected := m.cardByID(m.selectedCardID); selected != nil && selected.State == cardSettled {
		m.focus = focusChat
		return
	}
	if m.activeCardCount() == 0 {
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
		return
	}
	m.ensureCardSelection()
}

func (m *Model) cardByID(cardID string) *jobCard {
	for index := range m.cards {
		if m.cards[index].ID == cardID {
			return &m.cards[index]
		}
	}
	return nil
}

func (m *Model) cardForNodeID(nodeID string) *jobCard {
	for index := range m.cards {
		card := &m.cards[index]
		for _, part := range card.Parts {
			if part.NodeID == nodeID {
				return card
			}
		}
	}
	return nil
}

// cardForMessage resolves the card that owns a node-anchored message. During
// planning the job's subtree is not spliced yet, so the node id resolves
// nothing — the command seq still names the compiling card.
func (m *Model) cardForMessage(message store.Message) *jobCard {
	if message.NodeID != "" {
		if card := m.cardForNodeID(message.NodeID); card != nil {
			return card
		}
	}
	if message.CommandSeq != 0 {
		for index := range m.cards {
			if m.cards[index].CommandSeq == message.CommandSeq {
				return &m.cards[index]
			}
		}
	}
	return nil
}

// planProgressMessage recognises the planner's node-anchored, command-stamped
// status posts. By THREAD-UX law they mutate card state and never enter the
// stream — even in the poll window where neither the pending command nor the
// spliced subtree is visible yet.
func planProgressMessage(message store.Message) bool {
	return message.Role == store.RoleSystem && message.NodeID != "" && message.CommandSeq != 0
}

func (m *Model) streamMessage(message store.Message) bool {
	if message.Role == store.RoleUser && message.NodeID != "" {
		return false
	}
	if planProgressMessage(message) {
		return false
	}
	if message.NodeID == "" || m.cardForMessage(message) == nil {
		return true
	}
	if isQuestionMessage(message) {
		return true
	}
	if node, ok := m.cardNode(message.NodeID); ok && node.Status == store.Failed {
		return true
	}
	return false
}

func (m *Model) cardNode(nodeID string) (store.Node, bool) {
	for _, node := range m.cardSnapshot.Nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return store.Node{}, false
}

func (m *Model) attentionMessage(message store.Message) bool {
	if message.Role == store.RoleUser && message.NodeID != "" {
		return false
	}
	if planProgressMessage(message) {
		return false
	}
	if message.NodeID == "" {
		return true
	}
	card := m.cardForMessage(message)
	return card == nil || card.State == cardSettled || isQuestionMessage(message) ||
		func() bool {
			node, ok := m.cardNode(message.NodeID)
			return ok && node.Status == store.Failed
		}()
}

func (m *Model) cardDockHeight() int {
	content := m.renderActivityDock(false)
	if content == "" {
		return 0
	}
	return lipgloss.Height(content)
}

func (m *Model) limitCardDock(content string) string {
	return m.clampCardDock(content, 0)
}

func (m *Model) clampCardDock(content string, lineCap int) string {
	lines := strings.Split(content, "\n")
	// Keep the minimum three-line conversation viewport and the fixed frame,
	// input, and hint rows. A very detailed card yields with an honest tail.
	limit := max(1, m.height-7-m.inputSurfaceHeight())
	if lineCap > 0 {
		limit = min(limit, lineCap)
	}
	if len(lines) <= limit {
		return content
	}
	hidden := len(lines) - limit + 1
	lines = lines[:limit]
	lines[limit-1] = mutedStyle.Faint(true).Render(fmt.Sprintf("… %d more card lines", hidden))
	return strings.Join(lines, "\n")
}

// dockOverflowLimit is how many active cards render directly before the dock
// collapses to a one-line summary.
const dockOverflowLimit = 3

func dockSummaryCounts(active []jobCard) (running, waiting int) {
	for _, card := range active {
		if card.State == cardQuestion {
			waiting++
		} else {
			running++
		}
	}
	return running, waiting
}

func dockSummaryText(running, waiting int) string {
	line := fmt.Sprintf("%d running", running)
	if waiting > 0 {
		line += fmt.Sprintf(" · %d waiting ⚑", waiting)
	}
	return line
}

func (m *Model) dockOverflowOpen() bool {
	return m.dockExpanded || m.focus == focusCards
}

func (m *Model) renderCardDock(track bool) string {
	active := dockJobCards(m.cards, m.standingTime())
	if track {
		m.cardDockRows = m.cardDockRows[:0]
		kept := m.cardPartRows[:0]
		for _, row := range m.cardPartRows {
			if !row.dock {
				kept = append(kept, row)
			}
		}
		m.cardPartRows = kept
		keptOptions := m.cardOptionRows[:0]
		for _, row := range m.cardOptionRows {
			if !row.dock {
				keptOptions = append(keptOptions, row)
			}
		}
		m.cardOptionRows = keptOptions
		m.dockSummaryLine = -1
	}
	keptClose := m.cardCloseRows[:0]
	for _, row := range m.cardCloseRows {
		if !row.dock {
			keptClose = append(keptClose, row)
		}
	}
	m.cardCloseRows = keptClose
	if len(active) == 0 {
		// Zero active cards: the dock is absent unless a graph-only store has
		// live work to summarise.
		return m.renderLegacyActivityBar()
	}
	overflow := len(active) > dockOverflowLimit
	running, waiting := dockSummaryCounts(active)
	if overflow && !m.dockOverflowOpen() {
		if questionIsStuck(active[0], m.standingTime()) {
			chip := m.renderCompactCard(active[0], m.width)
			summary := mutedStyle.Render("▸ ") +
				lipgloss.NewStyle().Foreground(peach).Render(dockSummaryText(running, waiting))
			if track {
				m.cardDockRows = append(m.cardDockRows, cardRow{
					start: 0, end: 0, cardID: active[0].ID, dock: true,
				})
				m.dockSummaryLine = 1
			}
			return truncate(chip, m.width) + "\n" + truncate(summary, m.width)
		}
		if track {
			m.dockSummaryLine = 0
		}
		line := mutedStyle.Render("▸ ") +
			lipgloss.NewStyle().Foreground(peach).Render(dockSummaryText(running, waiting))
		return truncate(line, m.width)
	}

	lines := make([]string, 0, len(active)+1)
	atLine := 0
	if overflow {
		// The open overflow list leads with its flipped affordance; clicking it
		// (or esc) collapses back to the summary.
		if track {
			m.dockSummaryLine = 0
		}
		header := mutedStyle.Render("▾ ") +
			lipgloss.NewStyle().Foreground(peach).Render(dockSummaryText(running, waiting))
		lines = append(lines, truncate(header, m.width))
		atLine = 1
	}
	for _, card := range active {
		expanded := m.cardExpanded[card.ID]
		rendered := m.renderJobCard(card, m.width, expanded, atLine, true, track)
		if track {
			m.cardDockRows = append(m.cardDockRows, cardRow{
				start: atLine, end: atLine + lipgloss.Height(rendered) - 1, cardID: card.ID, dock: true,
			})
		}
		lines = append(lines, rendered)
		atLine += lipgloss.Height(rendered)
	}
	lineCap := 0
	if overflow {
		// The expanded overflow list stays a dock, not a takeover: cap it near
		// two fifths of the terminal and keep the honest tail.
		lineCap = max(4, m.height*2/5)
	}
	return m.clampCardDock(strings.Join(lines, "\n"), lineCap)
}

func (m *Model) renderJobCard(card jobCard, width int, expanded bool, atLine int, dock, track bool) string {
	width = max(12, width)
	if dock && !expanded && !(card.State == cardQuestion && card.QuestionKind != questionText) {
		return m.renderCompactCard(card, width)
	}

	glyph := m.cardGlyph(card)
	meta := m.cardMeta(card, time.Now())
	titleWidth := max(1, width-lipgloss.Width(glyph)-lipgloss.Width(meta)-7)
	titleStyle := lipgloss.NewStyle().Foreground(ink).Bold(true)
	if dock && questionIsStuck(card, m.standingTime()) {
		glyph = questionStyle.Bold(true).Render("?")
		titleStyle = questionStyle.Bold(true)
	}
	title := titleStyle.Render(truncate(card.Title, titleWidth))
	header := mutedStyle.Faint(true).Render("╭─ ") + glyph + " " + title
	if meta != "" {
		header += mutedStyle.Render(" · " + meta)
	}
	lines := []string{truncate(header, width)}
	addText := func(text string, style lipgloss.Style) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for _, line := range strings.Split(wrapText(text, max(1, width-4)), "\n") {
			lines = append(lines, mutedStyle.Faint(true).Render("│ ")+style.Render(line))
		}
	}

	if expanded {
		if card.Ask != "" {
			addText("asked · "+card.Ask, mutedStyle)
		}
		if card.Reading != "" && strings.TrimSpace(card.Reading) != strings.TrimSpace(card.Ask) {
			addText("reading this as · "+firstLine(card.Reading), mutedStyle)
		}
		if card.State == cardQuestion {
			addText(card.Question, questionStyle)
		}
		for _, assumption := range cardAssumptions(card.Receipt) {
			addText(assumption, mutedStyle)
		}
		if len(card.Narration) > 0 {
			lines = append(lines, mutedStyle.Faint(true).Render("│   running summary"))
			for _, narration := range card.Narration {
				addText("· "+narration, inputTextStyle)
			}
		}
		if len(card.Parts) > 0 {
			lines = append(lines, mutedStyle.Faint(true).Render("│   parts"))
			parts := make([]cardPart, 0, len(card.Parts))
			for _, part := range card.Parts {
				if part.NodeID != card.RootID {
					parts = append(parts, part)
				}
			}
			if len(parts) == 0 {
				parts = card.Parts
			}
			for _, part := range parts {
				glyph := cardPartGlyph(part.Status)
				result := part.Result
				if result == "" {
					result = string(part.Status)
				}
				line := "│   " + glyph + " " + lipgloss.NewStyle().Foreground(ink).Bold(true).Render(part.Title) +
					mutedStyle.Render(" — "+result)
				lines = append(lines, truncate(line, width))
				if track {
					m.cardPartRows = append(m.cardPartRows, cardPartRow{
						line: atLine + len(lines) - 1, cardID: card.ID, nodeID: part.NodeID, dock: dock,
					})
				}
				if part.TrialOf > 0 {
					approach := firstWords(part.Brief, 8)
					if approach == "" {
						approach = firstWords(part.Title, 8)
					}
					line := mutedStyle.Faint(true).Render("│     ⚖ trial · testing " + approach)
					lines = append(lines, truncate(line, width))
				}
			}
		}
		if card.Usage.PromptTokens+card.Usage.CompletionTokens > 0 {
			addText(fmt.Sprintf("cost · %s · %s tokens", formatCardCost(card.Usage.Cost),
				humanizeTokens(card.Usage.PromptTokens+card.Usage.CompletionTokens)), mutedStyle)
		}
	} else if card.State == cardQuestion {
		addText(card.Question, questionStyle)
	}
	if card.State == cardQuestion {
		m.appendQuestionComponent(&lines, card, width, atLine, dock, track)
	}

	if card.State == cardSettled && card.Deliverable != nil {
		if expanded && card.Outcome != "" {
			addText("outcome · "+card.Outcome, mutedStyle)
		}
		rendered, foldedAnswer := m.renderAnswerFold(*card.Deliverable, max(1, width-4))
		bodyStart := atLine + len(lines)
		for _, line := range strings.Split(rendered, "\n") {
			lines = append(lines, mutedStyle.Faint(true).Render("│ ")+line)
		}
		if track && !dock && card.Deliverable.Seq != 0 {
			m.chatMessageRows = append(m.chatMessageRows, chatMessageRow{
				start: bodyStart, end: bodyStart + lipgloss.Height(rendered) - 1, seq: card.Deliverable.Seq,
			})
			if foldedAnswer {
				m.chatExpandRows = append(m.chatExpandRows, chatExpandRow{
					line:   bodyStart + lipgloss.Height(rendered) - 1,
					action: chatExpandMessage,
					seq:    card.Deliverable.Seq,
				})
			}
		}
	}
	for _, moment := range card.Learning {
		headline := moment.Headline
		if expanded && len(moment.Details) > 0 {
			headline = strings.TrimSuffix(headline, "▸") + "▾"
		}
		lines = append(lines, truncate(mutedStyle.Faint(true).Render("│ "+headline), width))
		if expanded {
			for _, detail := range moment.Details {
				for _, line := range strings.Split(wrapText(detail, max(1, width-4)), "\n") {
					lines = append(lines, truncate(mutedStyle.Faint(true).Render("│   "+line), width))
				}
			}
		}
	}

	hint := "▸ details"
	if expanded {
		hint = "⟨×⟩ close · compiling the job graph…"
		if card.RootID != "" {
			hint = "⟨×⟩ close · ▸ job graph"
		}
	}
	lines = append(lines, mutedStyle.Faint(true).Render("╰─ "+hint))
	if track && expanded {
		m.cardCloseRows = append(m.cardCloseRows, cardCloseRow{
			line: atLine + len(lines) - 1, cardID: card.ID, dock: dock,
		})
	}
	return strings.Join(lines, "\n")
}

func firstWords(value string, limit int) string {
	words := strings.Fields(firstLine(value))
	if len(words) > limit {
		words = words[:limit]
	}
	return strings.Join(words, " ")
}

func (m *Model) appendQuestionComponent(
	lines *[]string, card jobCard, width, atLine int, dock, track bool,
) {
	switch card.QuestionKind {
	case questionConfirm:
		m.appendConfirmQuestion(lines, card, width, atLine, dock, track)
	case questionChoose, "":
		m.appendChooseQuestion(lines, card, width, atLine, dock, track)
	}
}

func (m *Model) appendChooseQuestion(
	lines *[]string, card jobCard, width, atLine int, dock, track bool,
) {
	selected := m.questionOptionIndex(card)
	for index, option := range card.Options {
		prefix := mutedStyle.Faint(true).Render("│   ")
		markerStyle := mutedStyle
		if index == selected {
			markerStyle = lipgloss.NewStyle().Foreground(powder).Bold(true)
		}
		text := strconv.Itoa(option.Number) + " " + option.Label
		available := max(1, width-lipgloss.Width(prefix)-2)
		label := questionStyle.Render(truncate(text, available))
		if option.Hint != "" {
			remaining := available - lipgloss.Width(text) - 3
			if remaining > 0 {
				label += mutedStyle.Faint(true).Render(" · " + truncate(option.Hint, remaining))
			}
		}
		line := prefix + markerStyle.Render("▸ ") + label
		if index == selected {
			line = lipgloss.NewStyle().Background(selectionBand).Width(width).Render(line)
		}
		*lines = append(*lines, truncate(line, width))
		if track {
			m.cardOptionRows = append(m.cardOptionRows, cardOptionRow{
				line: atLine + len(*lines) - 1, startX: 0, endX: width, cardID: card.ID,
				optionIndex: index, dock: dock,
			})
		}
	}
}

func (m *Model) appendConfirmQuestion(
	lines *[]string, card jobCard, width, atLine int, dock, track bool,
) {
	if len(card.Options) == 0 {
		return
	}
	prefix := mutedStyle.Faint(true).Render("│   ")
	component := questionComponent{Kind: questionConfirm, Options: card.Options, Default: card.Default}
	line, spans := renderConfirmOptions(component, m.questionOptionIndex(card), width, prefix)
	for index, span := range spans {
		if track {
			m.cardOptionRows = append(m.cardOptionRows, cardOptionRow{
				line: atLine + len(*lines), startX: span.startX, endX: span.endX,
				cardID: card.ID, optionIndex: index, dock: dock,
			})
		}
	}
	*lines = append(*lines, line)
}

// renderConfirmOptions is the shared option-question primitive used by job
// askbacks and the inline notebook's reversible retract choice.
func renderConfirmOptions(component questionComponent, selected, width int, prefix string) (string, []confirmOptionSpan) {
	line := prefix
	x := lipgloss.Width(prefix)
	spans := make([]confirmOptionSpan, 0, len(component.Options))
	for index, option := range component.Options {
		if index > 0 {
			separator := mutedStyle.Faint(true).Render(" · ")
			line += separator
			x += lipgloss.Width(separator)
		}
		markerStyle := mutedStyle
		if index == selected {
			markerStyle = lipgloss.NewStyle().Foreground(powder).Bold(true)
		}
		segment := markerStyle.Render("▸ ") + questionStyle.Render(strconv.Itoa(option.Number)+" "+option.Label)
		startX := x
		line += segment
		x += lipgloss.Width(segment)
		spans = append(spans, confirmOptionSpan{startX: startX, endX: x})
	}
	return truncate(line, width), spans
}

func (m *Model) questionOptionIndex(card jobCard) int {
	if len(card.Options) <= 0 {
		return 0
	}
	if m.questionSelection == nil {
		m.questionSelection = make(map[string]int)
	}
	index, selected := m.questionSelection[card.ID]
	if !selected {
		index = defaultQuestionOption(card)
	}
	index = max(0, min(index, len(card.Options)-1))
	m.questionSelection[card.ID] = index
	return index
}

func defaultQuestionOption(card jobCard) int {
	wanted := strings.TrimSpace(card.Default)
	for index, option := range card.Options {
		if strings.EqualFold(option.Key, wanted) || strings.EqualFold(option.Label, wanted) ||
			strconv.Itoa(option.Number) == wanted {
			return index
		}
	}
	return 0
}

func (m *Model) questionCardWithOptions() *jobCard {
	if card := m.selectedQuestionCardWithOptions(); card != nil {
		return card
	}
	for index := len(m.cards) - 1; index >= 0; index-- {
		card := &m.cards[index]
		if card.State == cardQuestion && len(card.Options) > 0 {
			return card
		}
	}
	return nil
}

func (m *Model) selectedQuestionCardWithOptions() *jobCard {
	card := m.cardByID(m.selectedCardID)
	if card != nil && card.State == cardQuestion && len(card.Options) > 0 {
		return card
	}
	return nil
}

func (m *Model) moveQuestionSelection(cardID string, delta int) {
	card := m.cardByID(cardID)
	if card == nil || len(card.Options) == 0 {
		return
	}
	index := m.questionOptionIndex(*card)
	m.questionSelection[cardID] = max(0, min(len(card.Options)-1, index+delta))
	m.setSize(m.width, m.height)
}

func (m *Model) submitSelectedQuestionOption(cardID string) tea.Cmd {
	card := m.cardByID(cardID)
	if card == nil || len(card.Options) == 0 {
		return nil
	}
	return m.submitQuestionOption(cardID, m.questionOptionIndex(*card))
}

func (m *Model) submitQuestionOptionNumber(cardID string, number int) (tea.Cmd, bool) {
	card := m.cardByID(cardID)
	if card == nil {
		return nil, false
	}
	for index, option := range card.Options {
		if option.Number == number {
			m.questionSelection[cardID] = index
			return m.submitQuestionOption(cardID, index), true
		}
	}
	return nil, false
}

func (m *Model) submitQuestionOption(cardID string, optionIndex int) tea.Cmd {
	card := m.cardByID(cardID)
	if card == nil || optionIndex < 0 || optionIndex >= len(card.Options) {
		return nil
	}
	m.input.Reset()
	m.focus = focusInput
	m.inputFocused = true
	_ = m.input.Focus()
	reply := card.Options[optionIndex].Reply
	if reply == "" {
		reply = card.Options[optionIndex].Key
	}
	return m.postUserMessage(reply)
}

func (m *Model) renderCompactCard(card jobCard, width int) string {
	glyph := m.cardGlyph(card)
	title := card.Title
	if title == "" {
		title = card.Ask
	}
	meta := m.cardMeta(card, time.Now())
	if card.State == cardQuestion && card.Latest != "" {
		meta = card.Latest
	}
	marker := ""
	if m.focus == focusCards && m.selectedCardID == card.ID {
		marker = lipgloss.NewStyle().Foreground(powder).Render("▸ ")
	}
	titleStyle := lipgloss.NewStyle().Foreground(ink).Bold(true)
	if questionIsStuck(card, m.standingTime()) {
		glyph = questionStyle.Bold(true).Render("?")
		titleStyle = questionStyle.Bold(true)
	}
	line := marker + glyph + " " + titleStyle.Render(title)
	if meta != "" {
		line += mutedStyle.Render(" · " + meta)
	}
	// The compact card is clickable (it expands); the grammar glyph rides the
	// end of the line and survives truncation.
	return truncate(line, max(1, width-2)) + mutedStyle.Faint(true).Render(" ▸")
}

// renderBrief keeps the arrival ritual to one sentence until the reader asks
// for it. The expanded form is deliberately only a stack of slim rows: this is
// orientation, not another dashboard or a second job graph.
func (m *Model) renderBrief(message store.Message, width, atLine int, track bool) string {
	if message.Brief == nil {
		return ""
	}
	width = max(8, width)
	expanded := m.briefExpanded[message.Seq]
	marker := "▸ "
	if expanded {
		marker = "▾ "
	}
	headline := oneSentence(message.Body)
	line := mutedStyle.Render(marker) + aforgeLabelStyle.Render(truncate(headline, max(1, width-2)))
	lines := []string{truncate(line, width)}
	if track {
		m.chatExpandRows = append(m.chatExpandRows, chatExpandRow{
			line: atLine, action: chatExpandBrief, seq: message.Seq,
		})
	}
	if !expanded {
		return lines[0]
	}
	for _, item := range message.Brief.Items {
		prefix := mutedStyle.Faint(true).Render("  " + briefItemGlyph(item.Kind) + " ")
		available := max(1, width-lipgloss.Width(prefix))
		body := inputTextStyle.Render(truncate(oneSentence(item.Body), available))
		lines = append(lines, truncate(prefix+body, width))
	}
	return strings.Join(lines, "\n")
}

func briefItemGlyph(kind store.BriefItemKind) string {
	switch kind {
	case store.BriefDone:
		return lipgloss.NewStyle().Foreground(mint).Render("✓")
	case store.BriefFailure:
		return lipgloss.NewStyle().Foreground(rose).Render("✗")
	case store.BriefCancelled:
		return mutedStyle.Render("–")
	case store.BriefQuestion:
		return questionStyle.Render("?")
	case store.BriefCharter:
		return lipgloss.NewStyle().Foreground(peach).Render("↻")
	case store.BriefSkill:
		return lipgloss.NewStyle().Foreground(powder).Render("◇")
	case store.BriefSpend:
		return mutedStyle.Render("$")
	default:
		return mutedStyle.Render("·")
	}
}

func oneSentence(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	for index, r := range value {
		if r != '.' && r != '?' && r != '!' {
			continue
		}
		next := index + 1
		if next == len(value) || (next < len(value) && value[next] == ' ') {
			return strings.TrimSpace(value[:next])
		}
	}
	return value
}

func (m *Model) collapseSelectedBrief() bool {
	if m.selectedBriefSeq == 0 || !m.briefExpanded[m.selectedBriefSeq] {
		return false
	}
	m.briefExpanded[m.selectedBriefSeq] = false
	m.refreshChat()
	return true
}

func (m *Model) cardGlyph(card jobCard) string {
	switch card.State {
	case cardCompiling:
		return lipgloss.NewStyle().Foreground(butter).Render("◌")
	case cardQuestion:
		return questionStyle.Render("⚑")
	case cardSettled:
		if card.Failed {
			return lipgloss.NewStyle().Foreground(rose).Render("✗")
		}
		return lipgloss.NewStyle().Foreground(mint).Render("✓")
	default:
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return lipgloss.NewStyle().Foreground(peach).Render(frame)
	}
}

func (m *Model) cardMeta(card jobCard, now time.Time) string {
	parts := make([]string, 0, 4)
	if card.State == cardCompiling {
		parts = append(parts, "compiling")
		// The live planning line replaces in place, tick by tick.
		switch {
		case card.Latest != "":
			parts = append(parts, card.Latest)
		case card.Reading != "":
			parts = append(parts, card.Reading)
		}
	}
	if card.State != cardCompiling && card.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", card.Done, card.Total))
	}
	if !card.StartedAt.IsZero() {
		end := now
		if card.State == cardSettled && !card.FinishedAt.IsZero() {
			end = card.FinishedAt
		}
		if end.After(card.StartedAt) {
			parts = append(parts, formatElapsed(end.Sub(card.StartedAt)))
		}
	}
	if card.State != cardCompiling {
		parts = append(parts, formatCardCost(card.Usage.Cost))
	}
	if card.State == cardWorking && card.Latest != "" {
		parts = append(parts, card.Latest)
	}
	if card.State == cardSettled && card.Outcome != "" {
		parts = append(parts, card.Outcome)
	}
	return strings.Join(parts, " · ")
}

func cardAssumptions(receipt string) []string {
	var assumptions []string
	for _, line := range strings.Split(strings.ReplaceAll(receipt, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Assumed:") {
			assumptions = append(assumptions, line)
		}
	}
	return assumptions
}

func cardPartGlyph(status store.Status) string {
	switch status {
	case store.Done:
		return lipgloss.NewStyle().Foreground(mint).Render("✓")
	case store.Failed, store.Cancelled:
		return lipgloss.NewStyle().Foreground(rose).Render("✗")
	case store.Running, store.Claimed:
		return lipgloss.NewStyle().Foreground(peach).Render("◐")
	default:
		return mutedStyle.Render("○")
	}
}

func formatCardCost(cost float64) string {
	if cost < 1 {
		return fmt.Sprintf("%.0f¢", cost*100)
	}
	return fmt.Sprintf("$%.2f", cost)
}

func (m *Model) advanceCard(cardID string, returnFocus paneFocus) tea.Cmd {
	card := m.cardByID(cardID)
	if card == nil {
		return nil
	}
	m.selectedCardID = cardID
	if !m.cardExpanded[cardID] {
		m.cardExpanded[cardID] = true
		m.focus = returnFocus
		m.inputFocused = false
		m.input.Blur()
		m.setSize(m.width, m.height)
		return nil
	}
	if card.RootID == "" {
		return nil
	}
	m.graphScopeID = card.RootID
	m.cardReturnFocus = returnFocus
	m.graphOpen = true
	m.selectedNodeID = card.RootID
	m.focus = focusGraph
	m.inputFocused = false
	m.input.Blur()
	m.setSize(m.width, m.height)
	return nil
}

func (m *Model) ensureCardSelection() {
	active := dockJobCards(m.cards, m.standingTime())
	for _, card := range active {
		if card.ID == m.selectedCardID {
			return
		}
	}
	if len(active) > 0 {
		m.selectedCardID = active[0].ID
	}
}

func (m *Model) moveCardSelection(delta int) {
	active := dockJobCards(m.cards, m.standingTime())
	if len(active) == 0 {
		return
	}
	m.ensureCardSelection()
	selected := 0
	for index, card := range active {
		if card.ID == m.selectedCardID {
			selected = index
			break
		}
	}
	selected = max(0, min(len(active)-1, selected+delta))
	m.selectedCardID = active[selected].ID
}

func (m *Model) focusCardDock() {
	m.ensureCardSelection()
	m.focus = focusCards
	m.inputFocused = false
	m.input.Blur()
	m.setSize(m.width, m.height)
}

func (m *Model) activeCardCount() int {
	active, _ := placeJobCards(m.cards)
	return len(active)
}

func (m *Model) pendingQuestionCard() *jobCard {
	for _, dockCard := range dockJobCards(m.cards, m.standingTime()) {
		if dockCard.State == cardQuestion {
			return m.cardByID(dockCard.ID)
		}
	}
	return nil
}

func (m *Model) hasPendingQuestion() bool { return m.pendingQuestionCard() != nil }

func (m *Model) activeTextQuestion() *jobCard {
	if m.paletteOpen() || m.nodeViewID != "" {
		return nil
	}
	for index := len(m.cards) - 1; index >= 0; index-- {
		card := &m.cards[index]
		if card.State == cardQuestion && card.QuestionKind == questionText &&
			!m.questionDismissed[card.ID] {
			return card
		}
	}
	return nil
}

func (m *Model) dismissTextQuestion() bool {
	card := m.activeTextQuestion()
	if card == nil {
		return false
	}
	if m.questionDismissed == nil {
		m.questionDismissed = make(map[string]bool)
	}
	m.questionDismissed[card.ID] = true
	m.setSize(m.width, m.height)
	return true
}

// inputSurfaceHeight is the whole input block: the text input itself, the
// attachment chip rows (plus a warning line when the talk model cannot see
// images), and the inline text-question line when one is active.
func (m *Model) inputSurfaceHeight() int {
	height := m.input.LineCount() + len(m.attachments)
	if len(m.attachments) > 0 {
		if _, supported := m.imageInputSupport(); !supported {
			height++
		}
	}
	if m.activeTextQuestion() != nil {
		height++
	}
	return height
}

func (m *Model) focusPendingQuestion() bool {
	card := m.pendingQuestionCard()
	if card == nil {
		return false
	}
	if m.nodeViewID != "" {
		m.closeNodeView()
	}
	if m.graphVisible() {
		m.graphOpen = false
		m.graphScopeID = ""
		m.charterCardID = ""
	}
	m.selectedCardID = card.ID
	if m.activeCardCount() > dockOverflowLimit {
		m.dockExpanded = true
	}
	m.focusCardDock()
	return true
}

func (m *Model) collapseSelectedCard() bool {
	if m.selectedCardID == "" || !m.cardExpanded[m.selectedCardID] {
		return false
	}
	m.cardExpanded[m.selectedCardID] = false
	m.setSize(m.width, m.height)
	return true
}

func (m *Model) closeScopedGraph() bool {
	if m.graphScopeID == "" {
		return false
	}
	m.graphScopeID = ""
	if m.charterCardID != "" {
		m.graphOpen = true
		m.focus = focusGraph
	} else {
		m.graphOpen = false
		m.focus = m.cardReturnFocus
	}
	m.inputFocused = m.focus == focusInput
	if m.inputFocused {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.setSize(m.width, m.height)
	return true
}
