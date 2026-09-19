package wscollab

import (
	"fmt"
	"strings"
)

// Message is one contribution as a trusted caller hands it to the router.
// Origin is not a field here: the Deliver door stamps it, so a model-supplied
// from_person flag has nowhere to live.
type Message struct {
	CauseID    string
	From       string
	ActorID    string
	Role       string
	Body       string
	Record     string
	SourceRef  string
	Discussion string
	// NoWake is an evidence cite, or any other line that must not start a turn.
	// The zero value wakes, because a delivered line is news.
	NoWake bool
	// Settled runs once the recipient's journal holds the line, never when the
	// queue took it. Nil on a resume that already recorded.
	Settled func()
}

func (m Message) validate() error {
	if strings.TrimSpace(m.From) == "" {
		return fmt.Errorf("%w: sender is empty", ErrInvalid)
	}
	if strings.TrimSpace(m.Body) == "" {
		return fmt.Errorf("%w: body is empty", ErrInvalid)
	}
	return nil
}

func (m Message) recordText() string {
	if m.Record != "" {
		return m.Record
	}
	return m.Body
}

func (m Message) envelope(origin, cause, pattern, dest, actor string) Envelope {
	return Envelope{
		ID:         mintDeliveryID(dest, cause, pattern),
		CauseID:    cause,
		Pattern:    pattern,
		Origin:     origin,
		ActorID:    actor,
		Role:       m.Role,
		From:       m.From,
		To:         dest,
		Discussion: m.Discussion,
		Body:       m.Body,
		Record:     m.recordText(),
		SourceRef:  m.SourceRef,
		Wake:       !m.NoWake,
		settled:    m.Settled,
	}
}

// Envelope is the durable outbox row. Pattern names how destinations were
// addressed, not which bus carried it.
type Envelope struct {
	ID         DeliveryID
	CauseID    string
	Pattern    string
	Origin     string
	ActorID    string
	Role       string
	From       string
	To         string
	Discussion string
	Body       string
	Record     string
	SourceRef  string
	Wake       bool
	settled    func()
}

// Receipt is the one answer a caller gets, with the three acks kept apart.
// Queue is never inferred from Recorded, and Processed is never inferred from
// either of the other two.
type Receipt struct {
	ID        DeliveryID
	To        string
	CauseID   string
	Pattern   string
	Queue     string
	Recorded  bool
	Processed bool
}

func pendingReceipt(env Envelope) Receipt {
	return Receipt{ID: env.ID, To: env.To, CauseID: env.CauseID, Pattern: env.Pattern, Queue: QueuePending}
}

func recordedReceipt(env Envelope, queue string, processed bool) Receipt {
	return Receipt{
		ID:        env.ID,
		To:        env.To,
		CauseID:   env.CauseID,
		Pattern:   env.Pattern,
		Queue:     queue,
		Recorded:  true,
		Processed: processed,
	}
}

// Citation is a chat named as evidence and nothing more. It is not a delivery.
type Citation struct {
	SessionID string
	Woke      bool
}

// Record is one outbox row plus the three acks. The schema lane persists this;
// tests keep it in memory until integrate.
type Record struct {
	Envelope
	Queue     string
	Recorded  bool
	Processed bool
}
