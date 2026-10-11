package steer

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// KindBatch is how many chats one kind-of-work question labels.
const KindBatch = 20

// titleRunes bounds a title shown to the model.
const titleRunes = 80

// kindLabels labels every member with a kind of work, KindBatch at a time.
// The model's label wins when it is in the closed list; a chat it skipped,
// or a whole batch it failed, takes the verb heuristic.
func kindLabels(ctx context.Context, chats map[string]placeorg.Chat, members []string, judge placeorg.Judge) map[string]string {
	out := map[string]string{}
	for start := 0; start < len(members); start += KindBatch {
		batch := members[start:min(start+KindBatch, len(members))]
		titles := make([]string, len(batch))
		rule := make([]string, len(batch))
		for i, id := range batch {
			titles[i] = titleOf(chats[id])
			rule[i] = verbKind(titles[i])
		}
		pick := judge.Write(ctx, placeorg.Question{
			Role:      placeorg.RolePlaceKind,
			Purpose:   "Label each chat with one kind of work.",
			Context:   kindContext(titles),
			MaxTokens: 8 * KindBatch,
		}, parseKinds(len(batch)), kindLines(rule))
		written, _ := readKinds(pick.Written, len(batch))
		for i, id := range batch {
			label := written[i]
			if label == "" {
				label = rule[i]
			}
			out[id] = label
		}
	}
	return out
}

// kindContext lists the numbered titles and the closed list of labels.
func kindContext(titles []string) string {
	var b strings.Builder
	b.WriteString("Kinds: " + strings.Join(placeorg.KindsOfWork, ", ") + ".\nAnswer one line per chat as `number: kind`.\n")
	for i, t := range titles {
		fmt.Fprintf(&b, "%d. %s\n", i+1, t)
	}
	return b.String()
}

// kindLines writes labels as the answer format; unlabelled chats are left
// out, and an empty answer is "none" because a fallback cannot be empty.
func kindLines(labels []string) string {
	var lines []string
	for i, l := range labels {
		if l != "" {
			lines = append(lines, fmt.Sprintf("%d: %s", i+1, l))
		}
	}
	if len(lines) == 0 {
		return "none"
	}
	return strings.Join(lines, "\n")
}

var kindLine = regexp.MustCompile(`^\s*(\d+)\s*[:.)-]\s*([A-Za-z ]+?)\s*$`)

// readKinds reads the answer format into one label per chat ("" for a chat
// with no valid line) and says whether any line was valid.
func readKinds(raw string, n int) ([]string, bool) {
	labels := make([]string, n)
	found := false
	for _, line := range strings.Split(raw, "\n") {
		m := kindLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		i, _ := strconv.Atoi(m[1])
		label := strings.ToLower(m[2])
		if i >= 1 && i <= n && inList(label) {
			labels[i-1], found = label, true
		}
	}
	return labels, found
}

// parseKinds is the Validator: an answer with no valid line is unusable.
func parseKinds(n int) placeorg.Validator {
	return func(raw string) (string, bool) {
		labels, ok := readKinds(raw, n)
		return kindLines(labels), ok
	}
}

func inList(label string) bool {
	for _, k := range placeorg.KindsOfWork {
		if k == label {
			return true
		}
	}
	return false
}

// titleOf is what the model and the heuristic read of a chat.
func titleOf(c placeorg.Chat) string {
	t := strings.TrimSpace(c.Title)
	if t == "" {
		t = strings.TrimSpace(c.Text)
	}
	if r := []rune(t); len(r) > titleRunes {
		t = string(r[:titleRunes])
	}
	return strings.ReplaceAll(t, "\n", " ")
}

// verbKinds maps the verb a title opens with to a kind of work. It is a
// closed English vocabulary of verbs, not a list of this person's subjects.
var verbKinds = map[string]string{
	"build": "build", "add": "build", "implement": "build", "create": "build", "make": "build", "port": "build", "wire": "build",
	"fix": "fix", "debug": "fix", "repair": "fix", "resolve": "fix",
	"review": "review", "audit": "review",
	"research": "research", "investigate": "research", "explore": "research", "find": "research", "compare": "research",
	"write": "write", "draft": "write", "rewrite": "write", "edit": "write", "document": "write",
	"plan": "plan", "design": "plan", "outline": "plan", "propose": "plan",
	"set": "set up", "setup": "set up", "install": "set up", "configure": "set up", "deploy": "set up",
	"check": "check", "verify": "check", "test": "check", "validate": "check",
	"what": "ask", "how": "ask", "why": "ask", "can": "ask", "does": "ask", "should": "ask", "explain": "ask",
}

// verbKind is the heuristic: the title's first word, or "" when it is none
// of the closed verbs. A question mark makes an unlabelled title an ask.
func verbKind(title string) string {
	first := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) })
	if len(first) > 0 {
		if k, ok := verbKinds[first[0]]; ok {
			return k
		}
	}
	if strings.HasSuffix(strings.TrimSpace(title), "?") {
		return "ask"
	}
	return ""
}
