package connections

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// SourceTrigger is the clock's request for authorized metadata. The clock owns
// cursor commits and run claims, while this service owns all delegated reads.
type SourceTrigger struct {
	On, App, Connection, Read, Filter string
	MinutesBefore                     int
	Now, Since                        time.Time
	Cursor                            string
}

type SourceTriggerBatch struct {
	Events  []SourceTriggerEvent
	Cursor  string
	HasMore bool
}

type SourceTriggerEvent struct {
	Key, Record, Type string
	Match             bool
}

func (s Service) PollGraphTrigger(ctx context.Context, trigger SourceTrigger) (SourceTriggerBatch, error) {
	c, err := s.scoped(trigger.App, trigger.Connection)
	if err != nil {
		return SourceTriggerBatch{}, err
	}
	read, ok := c.Reads[trigger.Read]
	if !ok || c.Kind != Microsoft365 || trigger.Since.IsZero() {
		return SourceTriggerBatch{}, ErrRejected
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	q := Query{App: trigger.App, Connection: trigger.Connection, Read: trigger.Read, Limit: maxRows}
	switch trigger.On {
	case schema.SourceMessageReceived:
		if read.Table != "outlook" || graphMappedName(read, "date") == "" {
			return SourceTriggerBatch{}, errors.New("a new-mail trigger needs an Outlook read with the message date mapped")
		}
		q.Cursor = trigger.Cursor
		page, err := s.Read(ctx, q)
		if errors.Is(err, ErrGraphCursor) && q.Cursor != "" {
			// A revoked or expired cursor must restart from a fresh baseline.
			// Previously claimed message IDs still cannot start a second run.
			q.Cursor = ""
			page, err = s.Read(ctx, q)
		}
		if err != nil {
			return SourceTriggerBatch{}, err
		}
		batch := SourceTriggerBatch{Cursor: page.Checkpoint, HasMore: page.Next != ""}
		if page.Next != "" {
			batch.Cursor = page.Next
		}
		for _, row := range page.Rows {
			if at := graphRowTime(row, graphMappedName(read, "date")); !at.IsZero() && !at.Before(trigger.Since) {
				subject, _ := row[graphMappedName(read, "subject")].(string)
				sender, _ := row[graphMappedName(read, "from")].(string)
				query := strings.ToLower(strings.TrimSpace(trigger.Filter))
				match := query == "" || strings.Contains(strings.ToLower(subject), query) || strings.Contains(strings.ToLower(sender), query)
				batch.Events = append(batch.Events, SourceTriggerEvent{Key: row.ID(), Record: row.ID(), Type: read.Type, Match: match})
			}
		}
		return batch, nil
	case schema.SourceMeetingStarting:
		if read.Table != "calendar" || trigger.MinutesBefore < 1 || trigger.MinutesBefore > schema.MaxMeetingLeadMinutes {
			return SourceTriggerBatch{}, ErrRejected
		}
		startField := graphMappedName(read, "start")
		if startField == "" {
			return SourceTriggerBatch{}, errors.New("a meeting trigger needs the meeting start mapped")
		}
		windowEnd := trigger.Now.Add(time.Duration(trigger.MinutesBefore) * time.Minute)
		q.Args, err = graphCalendarArgs(read, trigger.Now, windowEnd.Add(time.Second))
		if err != nil {
			return SourceTriggerBatch{}, err
		}
		rows, err := s.graphTriggerRows(ctx, q)
		if err != nil {
			return SourceTriggerBatch{}, err
		}
		var batch SourceTriggerBatch
		for _, row := range rows {
			start := graphRowTime(row, startField)
			if !start.Before(trigger.Now) && !start.After(windowEnd) {
				// A revised start can cross the threshold again, but an edit
				// to its title must never send a second reminder.
				key := row.ID() + "/" + start.UTC().Format(time.RFC3339Nano)
				batch.Events = append(batch.Events, SourceTriggerEvent{Key: key, Record: row.ID(), Type: read.Type, Match: true})
			}
		}
		return batch, nil
	case schema.SourceTranscriptCreated:
		if read.Table != "transcripts" || graphMappedName(read, "modified") == "" {
			return SourceTriggerBatch{}, errors.New("a transcript trigger needs the transcript creation time mapped")
		}
		if _, available, err := s.graphGrant(ctx, c); err != nil {
			return SourceTriggerBatch{}, err
		} else if !available {
			return SourceTriggerBatch{}, errTeamsGrant
		}
		meetingRead := c.Reads[read.GraphMeetingRead]
		meetingArg := graphParamName(read, "meeting")
		qCalendar := q
		qCalendar.Read = read.GraphMeetingRead
		qCalendar.Args, err = graphCalendarArgs(meetingRead, trigger.Now.Add(-24*time.Hour), trigger.Now)
		if err != nil || meetingArg == "" {
			return SourceTriggerBatch{}, errors.New("a transcript trigger needs declared meeting and calendar window arguments")
		}
		meetings, err := s.graphTriggerRows(ctx, qCalendar)
		if err != nil {
			return SourceTriggerBatch{}, err
		}
		var batch SourceTriggerBatch
		for _, meeting := range meetings {
			q.Args = Arguments{meetingArg: meeting.ID()}
			rows, err := s.graphTriggerRows(ctx, q)
			if errors.Is(err, ErrSourcePermission) && !errors.Is(err, errTeamsDisabled) && !errors.Is(err, errGraphConditional) {
				// One inaccessible meeting must not hide other transcripts.
				continue
			}
			if err != nil {
				return batch, err
			}
			for _, row := range rows {
				if at := graphRowTime(row, graphMappedName(read, "modified")); !at.IsZero() && !at.Before(trigger.Since) {
					batch.Events = append(batch.Events, SourceTriggerEvent{Key: row.ID(), Record: row.ID(), Type: read.Type, Match: true})
				}
			}
		}
		return batch, nil
	}
	return SourceTriggerBatch{}, ErrRejected
}

func (s Service) graphTriggerRows(ctx context.Context, q Query) ([]data.Row, error) {
	var rows []data.Row
	for range 12 {
		page, err := s.Read(ctx, q)
		if err != nil {
			return nil, err
		}
		rows = append(rows, page.Rows...)
		if len(rows) > maxRows {
			return nil, ErrTooLarge
		}
		if page.Next == "" {
			return rows, nil
		}
		q.Cursor = page.Next
	}
	return nil, ErrTooLarge
}

func graphCalendarArgs(read Read, from, to time.Time) (Arguments, error) {
	a, b := graphParamName(read, "from"), graphParamName(read, "to")
	if a == "" || b == "" {
		return nil, fmt.Errorf("the calendar read needs both from and to arguments")
	}
	return Arguments{a: from.UTC().Format(time.RFC3339Nano), b: to.UTC().Format(time.RFC3339Nano)}, nil
}

func graphRowTime(r data.Row, key string) time.Time {
	raw, _ := r[key].(string)
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}

func graphMappedName(read Read, providerName string) string {
	for name, column := range read.Fields {
		if column == providerName {
			return name
		}
	}
	return ""
}

func graphParamName(read Read, providerName string) string {
	for name, param := range read.Match {
		if param == providerName {
			return name
		}
	}
	return ""
}
