package factory

import (
	"fmt"
	"strings"
	"time"
)

// ThreadMost and MessageTextMost bound each job's replicated conversation.
const (
	ThreadMost      = 200
	MessageTextMost = 16 * 1024
	AnswersMost     = 100
)

// Message is a spoken manager line or a person's reply. Pending replies stay
// on the job until its worker acknowledges delivery, including across expiry.
type Message struct {
	At    time.Time `json:"at"`
	From  string    `json:"from"`
	Text  string    `json:"text"`
	ID    string    `json:"id,omitempty"`
	State string    `json:"state,omitempty"`
}

// Question keeps an open ask or its answered history in the same wire shape.
type Question struct {
	Text     string     `json:"text"`
	About    string     `json:"about,omitempty"`
	FromStep string     `json:"from_step,omitempty"`
	RoutedTo string     `json:"routed_to,omitempty"`
	State    string     `json:"state"`
	Answer   *Answer    `json:"answer,omitempty"`
	Kind     string     `json:"kind,omitempty"`
	With     string     `json:"with,omitempty"`
	Options  []string   `json:"options,omitempty"`
	Pick     string     `json:"pick,omitempty"`
	Why      string     `json:"why,omitempty"`
	At       *time.Time `json:"at,omitempty"`
	// Limit, Spent and NextLimit are a cap question's numbers, derived when
	// the job is read. NextLimit is the cap a yes would allow.
	Limit     float64 `json:"limit,omitempty"`
	Spent     float64 `json:"spent,omitempty"`
	NextLimit float64 `json:"next_limit,omitempty"`
}

// Answer preserves the person, words, clock and receipt after an ask closes.
type Answer struct {
	By    string    `json:"by"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	Event string    `json:"event,omitempty"`
}

// MergeThread accepts spoken lines and delivery receipts, never a worker's
// claim to have queued a person's reply. IDs make repeated reports harmless.
func MergeThread(it *Item, messages []Message, delivered []string) error {
	for _, m := range messages {
		if (m.ID == "" || len(m.ID) > 128) || (m.From != "manager" && m.From != "user") || strings.TrimSpace(m.Text) == "" {
			return fmt.Errorf("thread message requires id, speaker and text")
		}
		found := false
		for _, held := range it.Thread {
			if held.ID == m.ID {
				found = true
				break
			}
		}
		if found {
			continue
		}
		m.State = ""
		m.Text = boundedMessage(m.Text)
		it.Thread = append(it.Thread, m)
	}
	for _, id := range delivered {
		for i := range it.Thread {
			if it.Thread[i].ID == id && it.Thread[i].State == "pending" {
				it.Thread[i].State = "delivered"
			}
		}
	}
	// Pending words cannot scroll out before the worker hears them.
	for len(it.Thread) > ThreadMost {
		i := 0
		for i < len(it.Thread) && it.Thread[i].State == "pending" {
			i++
		}
		if i == len(it.Thread) {
			return fmt.Errorf("job has too many pending replies")
		}
		it.Thread = append(it.Thread[:i], it.Thread[i+1:]...)
	}
	return nil
}

func boundedMessage(s string) string {
	if len(s) > MessageTextMost {
		return strings.ToValidUTF8(s[:MessageTextMost], "")
	}
	return s
}
