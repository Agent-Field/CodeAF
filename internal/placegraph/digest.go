package placegraph

// The "Since yesterday" roll-up of a place's Home.
//
// THIS FILE CALLS NO MODEL AND READS NO FILE. It is a pure function from a typed
// list of recent conversations — each carrying the recap the conversation wrote
// of itself, if it ever did — to the few sentences a Home shows. The words are
// the conversations' own: a recap line is quoted, trimmed and ordered here, and
// never paraphrased, so the paragraph is exactly as true as the recaps under it.
//
// A conversation with no recap contributes nothing and is COUNTED instead
// ([Digest.Unsummarised]); the roll-up never writes a sentence out of a title,
// because a title names a topic and says nothing about what came of it.

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DigestWindow is how far back "Since yesterday" looks: a rolling day, so the
	// same instant gives the same words in every window and timezone.
	DigestWindow = 24 * time.Hour
	// DigestLabel is the section's heading.
	DigestLabel = "Since yesterday"
	// DigestItemsMax bounds the conversations named in the structured list and
	// DigestSentences those quoted in the paragraph.
	DigestItemsMax  = 12
	DigestSentences = 4
	// DigestTextMax bounds the paragraph, in characters, and DigestLineMax each
	// quoted line.
	DigestTextMax = 480
	DigestLineMax = 220
	// digestSkew is how far past "now" a recap's stamp may sit before it is
	// treated as damaged (a clock set wrong, a hand-edited file) and dropped.
	digestSkew = 5 * time.Minute
)

// DigestRecap is the part of a conversation's persisted recap a roll-up reads.
type DigestRecap struct {
	Line      string
	Outcome   string
	UpdatedAt time.Time
	Messages  int
}

// DigestChat is one conversation inside the place, as the roll-up sees it.
type DigestChat struct {
	ID        string
	Title     string
	PlaceID   string
	PlaceName string
	// Archived chats are put away and never news.
	Archived bool
	NeedsYou bool
	Running  bool
	// FailedTasks is history, not news by itself; it only breaks ties.
	FailedTasks int
	// Recap is nil for a conversation nobody has summarised, or whose recap could
	// not be read.
	Recap *DigestRecap
}

// DigestInput is everything the roll-up needs, including the clock.
type DigestInput struct {
	Now   time.Time
	Chats []DigestChat
}

// DigestItem is one conversation's contribution to the roll-up.
type DigestItem struct {
	ChatID    string    `json:"chatId"`
	ChatTitle string    `json:"chatTitle"`
	PlaceID   string    `json:"placeId,omitempty"`
	PlaceName string    `json:"placeName,omitempty"`
	At        time.Time `json:"at"`
	Line      string    `json:"line"`
	Outcome   string    `json:"outcome,omitempty"`
	// Attention is "needsYou", "running" or "" — why this conversation ranked
	// where it did.
	Attention string `json:"attention,omitempty"`
}

// Digest is the roll-up. A zero Digest (no Items) means there is nothing to say
// and the Home draws no section.
type Digest struct {
	Label string `json:"label"`
	Text  string `json:"text"`
	// Since is the start of the window the evidence was drawn from.
	Since time.Time `json:"since"`
	// Chats is how many conversations had a usable recap in the window, and
	// Items names up to DigestItemsMax of them in the order the text quotes.
	Chats int          `json:"chats"`
	Items []DigestItem `json:"items"`
	// Unsummarised counts waiting or running conversations that have no
	// readable recap, so a quiet paragraph is never mistaken for a quiet place.
	Unsummarised int `json:"unsummarised"`
}

// Empty reports that there is nothing worth a section.
func (d Digest) Empty() bool { return len(d.Items) == 0 }

// RollUp builds the digest. It is deterministic: the same input gives the same
// bytes regardless of the order the chats arrive in.
func RollUp(in DigestInput) Digest {
	since := in.Now.Add(-DigestWindow)
	d := Digest{Label: DigestLabel, Since: since, Items: []DigestItem{}}
	seen := map[string]bool{}
	var items []DigestItem
	for _, c := range in.Chats {
		// A diamond's shared chat can arrive twice; it is one conversation.
		if c.ID == "" || c.Archived || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		r := c.Recap
		line := cleanLine(recapLine(r), DigestLineMax)
		if r == nil || line == "" || r.UpdatedAt.IsZero() || r.UpdatedAt.After(in.Now.Add(digestSkew)) {
			if c.NeedsYou || c.Running {
				d.Unsummarised++
			}
			continue
		}
		if r.UpdatedAt.Before(since) {
			continue
		}
		it := DigestItem{ChatID: c.ID, ChatTitle: c.Title, PlaceID: c.PlaceID, PlaceName: c.PlaceName, At: r.UpdatedAt.UTC(), Line: line, Outcome: cleanLine(r.Outcome, DigestLineMax)}
		switch {
		case c.NeedsYou:
			it.Attention = "needsYou"
		case c.Running:
			it.Attention = "running"
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return d
	}
	rank := func(a string) int {
		switch a {
		case "needsYou":
			return 0
		case "running":
			return 1
		}
		return 2
	}
	failed := map[string]int{}
	for _, c := range in.Chats {
		failed[c.ID] = c.FailedTasks
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ra, rb := rank(a.Attention), rank(b.Attention); ra != rb {
			return ra < rb
		}
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		if failed[a.ChatID] != failed[b.ChatID] {
			return failed[a.ChatID] > failed[b.ChatID]
		}
		return a.ChatID < b.ChatID
	})
	d.Chats = len(items)
	if len(items) > DigestItemsMax {
		items = items[:DigestItemsMax]
	}
	d.Items = items
	d.Text = digestText(items, d.Chats)
	return d
}

func recapLine(r *DigestRecap) string {
	if r == nil {
		return ""
	}
	return r.Line
}

// digestText quotes up to DigestSentences lines, each as a sentence, and says
// how many more there were. The total never passes DigestTextMax.
func digestText(items []DigestItem, total int) string {
	var parts []string
	used := 0
	quoted := 0
	for _, it := range items {
		if quoted == DigestSentences {
			break
		}
		s := sentence(it.Line)
		extra := utf8.RuneCountInString(s)
		if len(parts) > 0 {
			extra++
		}
		if used+extra > DigestTextMax {
			break
		}
		parts = append(parts, s)
		used += extra
		quoted++
	}
	if quoted == 0 { // a single line longer than the bound: cut it, never drop the section
		return cleanLine(sentence(items[0].Line), DigestTextMax)
	}
	text := strings.Join(parts, " ")
	if more := total - quoted; more > 0 {
		tail := " " + strconv.Itoa(more) + " more in other chats."
		if more == 1 {
			tail = " 1 more in another chat."
		}
		if utf8.RuneCountInString(text)+utf8.RuneCountInString(tail) <= DigestTextMax {
			text += tail
		}
	}
	return text
}

func sentence(s string) string {
	if s == "" {
		return s
	}
	switch s[len(s)-1] {
	case '.', '!', '?', ':':
		return s
	}
	return s + "."
}

// cleanLine collapses whitespace and control characters (a recap is model text
// kept in a file anyone can edit) and cuts to max characters with an ellipsis.
func cleanLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
