package resident

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Learning visibility is a projection of journal writes, never another
// learning policy. A tick batches projections by top-level job so a landing
// can teach several things without turning the conversation into a log.
type learningMomentItem struct {
	headline string
}

type pendingLearningMoment struct {
	nodeID    string
	sessionID string
	items     []learningMomentItem
}

func learnedFactMoment(fact store.Fact) learningMomentItem {
	return learningMomentItem{headline: "· learned — " + firstLine(fact.Body)}
}

func settledTrialMoment(fact store.Fact, trials int) learningMomentItem {
	return learningMomentItem{headline: fmt.Sprintf("⚖ settled: %s · %d %s",
		firstLine(fact.Body), trials, plural(trials, "trial", "trials"))}
}

func forgedSkillMoment(name string) learningMomentItem {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "learned skill"
	}
	return learningMomentItem{headline: "⚒ forged: " + name + " — proved twice, now available"}
}

// forgedCraftMoment is the skill line's sibling for know-how that is a shape
// rather than an executable. A forged craft has never run, so it promises the
// next time rather than this one.
func forgedCraftMoment(name string, refined bool) learningMomentItem {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "learned"
	}
	verb := "forged"
	if refined {
		verb = "refined"
	}
	return learningMomentItem{headline: "⚒ " + verb + ": how to do " + name + " — I'll work this way next time"}
}

func letGoMoment(body string) learningMomentItem {
	return learningMomentItem{headline: "· let go — " + firstLine(body)}
}

func (r *Reconciler) queueLearningMoment(nodeID string, item learningMomentItem) {
	if r == nil || r.store == nil || strings.TrimSpace(item.headline) == "" {
		return
	}
	root, sessionID, ok := r.learningMomentAnchor(nodeID)
	if !ok {
		return
	}
	if r.learningMoments == nil {
		r.learningMoments = make(map[string]*pendingLearningMoment)
	}
	pending := r.learningMoments[root]
	if pending == nil {
		pending = &pendingLearningMoment{nodeID: root, sessionID: sessionID}
		r.learningMoments[root] = pending
	}
	// Moments now survive the tick that composed them, so a pass replayed after
	// a panic — the settle cursor only advances past an event it finished — must
	// not stack the same line twice on the way out.
	for _, existing := range pending.items {
		if existing.headline == item.headline {
			return
		}
	}
	pending.items = append(pending.items, item)
}

func (r *Reconciler) learningMomentAnchor(nodeID string) (string, string, bool) {
	node, ok, err := r.store.Node(strings.TrimSpace(nodeID))
	if err != nil || !ok || node.ID == store.RootID {
		return "", "", false
	}
	sessionID := node.Provenance.SessionID
	for node.Parent != store.RootID {
		if node.Parent == "" {
			return "", "", false
		}
		parent, found, err := r.store.Node(node.Parent)
		if err != nil || !found || store.IsOrganizationalGroup(parent.Group) {
			break
		}
		node = parent
		if sessionID == "" {
			sessionID = node.Provenance.SessionID
		}
	}
	if sessionID == "" {
		return "", "", false
	}
	return node.ID, sessionID, true
}

func (r *Reconciler) surfaceAttached() bool {
	seen, found, err := r.store.LastSeen()
	return err == nil && found && seen.State == store.SeenAttached
}

// learningMomentsHeld bounds the carry. A machine that learns for a week with
// nobody watching should arrive with news, not with a week of it; the journal
// keeps every fact either way, and the arrival brief reads the journal.
const learningMomentsHeld = 24

// flushLearningMoments posts what the tick learned, and keeps what it could not
// post. The map used to be wiped unconditionally, so a moment composed while no
// surface happened to be attached was destroyed outright — the whole `aforge
// wake` path, where overnight work learns things nobody is ever told about.
// Holding the undelivered ones costs a bounded map and delivers them the moment
// somebody is there to read them.
func (r *Reconciler) flushLearningMoments() {
	if len(r.learningMoments) == 0 {
		return
	}
	keys := make([]string, 0, len(r.learningMoments))
	for nodeID := range r.learningMoments {
		keys = append(keys, nodeID)
	}
	sort.Strings(keys)
	attached := r.surfaceAttached()
	for _, nodeID := range keys {
		pending := r.learningMoments[nodeID]
		if pending == nil || len(pending.items) == 0 {
			delete(r.learningMoments, nodeID)
			continue
		}
		if !attached {
			continue
		}
		body := pending.items[0].headline
		if len(pending.items) > 1 {
			var detail strings.Builder
			fmt.Fprintf(&detail, "· learned %d things ▸", len(pending.items))
			for _, item := range pending.items {
				detail.WriteString("\n  ")
				detail.WriteString(item.headline)
			}
			body = detail.String()
		}
		if _, err := r.store.PostMessage(store.Message{
			SessionID: pending.sessionID,
			Role:      store.RoleSystem,
			Body:      boundMessage(body),
			NodeID:    pending.nodeID,
		}); err == nil {
			delete(r.learningMoments, nodeID)
		}
	}
	// Oldest first by job id is the only ordering available here, and it is the
	// right one: the news a user has been waiting longest for is the news that
	// has most likely already been superseded by the notebook itself.
	for len(r.learningMoments) > learningMomentsHeld {
		oldest := ""
		for nodeID := range r.learningMoments {
			if oldest == "" || nodeID < oldest {
				oldest = nodeID
			}
		}
		delete(r.learningMoments, oldest)
	}
}

func (r *Reconciler) latestEventSeq() int64 {
	seq, err := r.store.LatestEventSeq()
	if err != nil {
		return 0
	}
	return seq
}

type retrospectiveCategory struct {
	firstSeq int64
	count    int
	label    func(int) string
}

// postRetrospectiveDigest reads only the journal interval written by the
// consolidator, reflector, territory pass, and skill forge. The checkpoint by
// itself is intentionally invisible: reflection with no durable delta emits
// no message and therefore no extra rendered bytes.
func (r *Reconciler) postRetrospectiveDigest(afterSeq int64) {
	events, err := r.store.Events(afterSeq, 0)
	if err != nil || len(events) == 0 {
		return
	}
	seen, found, err := r.store.LastSeen()
	if err != nil || !found || seen.State != store.SeenAttached || strings.TrimSpace(seen.SessionID) == "" {
		return
	}

	learned := make(map[int64]store.Fact)
	learnedOrder := make([]int64, 0)
	replacements := make(map[int64]bool)
	categories := make(map[string]*retrospectiveCategory)
	details := make([]string, 0)
	var tuning string
	addCategory := func(key string, seq int64, count int, label func(int) string) {
		if count <= 0 {
			return
		}
		category := categories[key]
		if category == nil {
			category = &retrospectiveCategory{firstSeq: seq, label: label}
			categories[key] = category
		}
		category.count += count
	}
	addDetail := func(line string) {
		line = strings.TrimSpace(line)
		if line != "" {
			details = append(details, line)
		}
	}

	for _, event := range events {
		switch event.Kind {
		case store.EventParameterChanged:
			var change store.ParameterChange
			if tuning == "" && json.Unmarshal(event.Payload, &change) == nil {
				tuning = strings.TrimSpace(change.Phrase)
			}
		case store.EventFactLearned:
			fact, ok, readErr := r.store.FactBySeq(event.Seq)
			if readErr == nil && ok && fact.Kind != store.FactTrait {
				learned[fact.Seq] = fact
				learnedOrder = append(learnedOrder, fact.Seq)
			}
		case store.EventFactSuperseded:
			var payload struct {
				FactSeq int64 `json:"fact_seq"`
				BySeq   int64 `json:"by_seq"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && payload.FactSeq > 0 {
				fact, ok, readErr := r.store.FactBySeq(payload.FactSeq)
				if readErr == nil && ok && fact.Kind != store.FactSkill {
					addCategory("merged", event.Seq, 1, func(count int) string {
						return fmt.Sprintf("%d %s merged", count, plural(count, "belief", "beliefs"))
					})
				}
				if payload.BySeq > 0 {
					replacements[payload.BySeq] = true
				}
			}
		case store.EventFactQuarantined:
			var payload struct {
				FactSeq int64 `json:"fact_seq"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil {
				fact, ok, readErr := r.store.FactBySeq(payload.FactSeq)
				if readErr == nil && ok {
					addCategory("aged", event.Seq, 1, func(count int) string {
						return fmt.Sprintf("%d %s aged", count, plural(count, "belief", "beliefs"))
					})
					addDetail(letGoMoment(fact.Body).headline)
				}
			}
		case store.EventScopeAliased:
			var payload struct {
				From string `json:"from_scope"`
				To   string `json:"to_scope"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil {
				addCategory("scopes", event.Seq, 1, func(count int) string {
					return fmt.Sprintf("%d %s merged", count, plural(count, "topic", "topics"))
				})
				addDetail("~ " + payload.From + " · " + payload.To)
			}
		case store.EventSubtreeSpliced:
			node, ok, readErr := r.store.Node(event.NodeID)
			if readErr == nil && ok && node.Group == store.TerritoryGroup && node.Provenance.Origin == store.OriginSelf {
				addCategory("territories", event.Seq, 1, func(count int) string {
					return fmt.Sprintf("%d %s formed", count, plural(count, "area", "areas"))
				})
				addDetail("~ " + firstLine(node.Title))
			}
		case store.EventCharterCreated:
			charter, ok, readErr := r.store.Charter(event.NodeID)
			if readErr == nil && ok && charter.Status == store.CharterProposed {
				addCategory("proposals", event.Seq, 1, func(count int) string {
					return fmt.Sprintf("%d %s made", count, plural(count, "proposal", "proposals"))
				})
				addDetail("~ " + firstLine(charter.Invariant))
			}
		case store.EventFactActivated:
			var payload struct {
				FactSeq int64 `json:"fact_seq"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil {
				fact, ok, readErr := r.store.FactBySeq(payload.FactSeq)
				if readErr == nil && ok {
					addCategory("skills", event.Seq, 1, func(count int) string {
						return fmt.Sprintf("%d %s promoted", count, plural(count, "skill", "skills"))
					})
					addDetail("⚒ " + filepath.Base(fact.Artifact))
				}
			}
		}
	}

	for _, seq := range learnedOrder {
		fact := learned[seq]
		if replacements[seq] {
			addDetail("· #" + fmt.Sprint(fact.Seq) + " " + firstLine(fact.Body))
			continue
		}
		if fact.Kind == store.FactSkill && fact.Status == store.FactCandidate {
			continue
		}
		addCategory("learned", seq, 1, func(count int) string {
			return fmt.Sprintf("%d %s learned", count, plural(count, "belief", "beliefs"))
		})
		addDetail("· #" + fmt.Sprint(fact.Seq) + " " + firstLine(fact.Body))
	}
	if len(categories) == 0 && tuning == "" {
		return
	}

	ordered := make([]*retrospectiveCategory, 0, len(categories))
	for _, category := range categories {
		ordered = append(ordered, category)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].firstSeq < ordered[j].firstSeq })
	if len(ordered) > 3 {
		ordered = ordered[:3]
	}
	parts := make([]string, 0, len(ordered))
	for _, category := range ordered {
		parts = append(parts, category.label(category.count))
	}
	if tuning != "" {
		parts = append(parts, tuning)
	}
	body := "· reflected — " + strings.Join(parts, ", ")
	for _, detail := range details {
		body += "\n  " + detail
	}
	_, _ = r.store.PostMessage(store.Message{
		SessionID: seen.SessionID,
		Role:      store.RoleSystem,
		Body:      boundMessage(body),
	})
}
