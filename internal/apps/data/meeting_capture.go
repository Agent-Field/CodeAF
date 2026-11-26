package data

import (
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

const meetingCaptureApp = "meeting-capture"

type MeetingNotes struct {
	Summary   string        `json:"summary"`
	Decisions []string      `json:"decisions"`
	Tasks     []MeetingTask `json:"tasks"`
	Subject   string        `json:"subject"`
	Draft     string        `json:"draft"`
}

type MeetingTask struct {
	Title    string `json:"title"`
	Assignee string `json:"assignee,omitempty"`
}

type MeetingCapture struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Started     time.Time    `json:"started_at"`
	Ended       time.Time    `json:"ended_at"`
	Calendar    string       `json:"calendar_event,omitempty"`
	Attendees   []string     `json:"attendees,omitempty"`
	Transcript  string       `json:"transcript"`
	PersonNotes string       `json:"person_notes"`
	Notes       MeetingNotes `json:"notes"`
	KeptAudio   bool         `json:"kept_audio"`
}

type MeetingCaptureReceipt struct {
	Meeting    string   `json:"meeting"`
	Transcript string   `json:"transcript"`
	Tasks      []string `json:"tasks"`
}

const meetingCaptureRecords = `{"id":"meeting-capture","records":{
	"person":{"common":"person","fields":{"name":{"type":"text"},"email":{"type":"email"}}},
	"document":{"common":"document","fields":{"title":{"type":"text","class":"pii"},"body":{"type":"longtext","class":"pii"}}},
	"task":{"common":"task","fields":{"title":{"type":"text","class":"pii"},"assignee":{"type":"ref","to":"person"},"done":{"type":"bool"}}},
	"meeting":{"common":"event","fields":{
		"title":{"type":"text","class":"pii"},"starts_at":{"type":"datetime"},"ends_at":{"type":"datetime"},
		"attendees":{"type":"refs","to":"person"},"calendar_event":{"type":"ref","to":"meeting"},"transcript":{"type":"ref","to":"document"},
		"capture_tasks":{"type":"refs","to":"task"},"capture_status":{"type":"text"},"capture_id":{"type":"text"},
		"person_notes":{"type":"longtext","class":"pii"},"capture_summary":{"type":"longtext","class":"pii"},
		"capture_decisions":{"type":"longtext","class":"pii"},"draft_subject":{"type":"text","class":"pii"},
		"draft_body":{"type":"longtext","class":"pii"},"kept_audio":{"type":"bool"}
	}}
},"capabilities":{"records":{"person":"r","document":"rw","task":"rw","meeting":"rw"}}}`

// IngestMeetingCapture commits the transcript, tasks, and final Meeting together.
// A retry takes the original record links and never emits a second completion.
func (s *Store) IngestMeetingCapture(in MeetingCapture) (*MeetingCaptureReceipt, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	scope := Scope{App: meetingCaptureApp, Cause: "meeting.ended", RunWriteKey: in.ID}
	var result MeetingCaptureReceipt
	err := s.writeAs(scope, func(w *writeTx) error {
		current := w.schema.appType(meetingCaptureApp, "meeting")
		if current == nil || current.field("capture_id") == nil || current.field("calendar_event") == nil {
			def, err := ParseApp([]byte(meetingCaptureRecords))
			if err != nil {
				return err
			}
			for alias, typ := range def.Records {
				if bound := w.schema.common[typ.Common]; bound != nil && bound.DefinedBy != meetingCaptureApp {
					typ.Uses = true
					def.Records[alias] = typ
				}
			}
			next, err := registerApp(w, def, nil)
			if err != nil {
				return err
			}
			w.schema, w.next = next, next
		}
		meeting, err := w.schema.allow(scope, "meeting", true)
		if err != nil {
			return err
		}
		const op = OpCreate
		var batch recordBatch
		saved, err := w.loadRunWrite(scope, meeting, op, &batch)
		if err != nil {
			return err
		}
		if saved {
			if len(batch.rows) != 1 {
				return errorf(CodeNotFound, "the saved meeting is no longer available")
			}
			row := batch.rows[0]
			result.Meeting, result.Transcript = row.ID, asText(row.Data["transcript"])
			result.Tasks = append([]string{}, asStrings(row.Data["capture_tasks"])...)
			return nil
		}
		if in.Calendar != "" {
			event, err := loadRecord(w.tx, w.schema, in.Calendar)
			if err != nil {
				return err
			}
			if event == nil || event.Type != meeting.Name || event.DeletedAt != "" || !meeting.rowAllowed(Scope{}, w.schema.person, event.Data) {
				return errorf(CodeNotFound, "the calendar meeting is no longer available")
			}
			in.Attendees = asStrings(event.Data["attendees"])
		}
		if batch.batch, err = w.newBatch(scope, op, nil); err != nil {
			return err
		}
		create := func(alias string, provenance Scope, fields map[string]any) (string, error) {
			typ, err := w.schema.allow(provenance, alias, true)
			if err != nil {
				return "", err
			}
			row, err := w.createOne(typ, provenance, fields, op)
			if err != nil {
				return "", err
			}
			return row.ID, nil
		}
		if result.Transcript, err = create("document", scope, map[string]any{
			"title": in.Title + " transcript", "body": in.Transcript,
		}); err != nil {
			return err
		}
		ai := scope
		ai.AI = true
		result.Tasks = []string{}
		for _, task := range in.Notes.Tasks {
			fields := map[string]any{"title": task.Title, "done": false}
			if task.Assignee != "" && slices.Contains(in.Attendees, task.Assignee) {
				fields["assignee"] = task.Assignee
			}
			id, err := create("task", ai, fields)
			if err != nil {
				return err
			}
			result.Tasks = append(result.Tasks, id)
		}
		// AI and person contributions have their own fields, and the person
		// value is byte-for-byte the text submitted by the desk.
		decisions, err := json.Marshal(in.Notes.Decisions)
		if err != nil {
			return err
		}
		fields := map[string]any{
			"title": in.Title, "starts_at": in.Started.UTC().Format(time.RFC3339Nano),
			"ends_at": in.Ended.UTC().Format(time.RFC3339Nano), "attendees": in.Attendees,
			"calendar_event": in.Calendar, "capture_id": in.ID, "transcript": result.Transcript, "capture_status": "ended", "capture_tasks": result.Tasks,
			"person_notes": in.PersonNotes, "capture_summary": in.Notes.Summary,
			"capture_decisions": string(decisions),
			"draft_subject":     in.Notes.Subject, "draft_body": in.Notes.Draft, "kept_audio": in.KeptAudio,
		}
		provenance := scope
		provenance.aiFields = map[string]bool{
			"capture_summary": true, "capture_decisions": true, "draft_subject": true, "draft_body": true,
		}
		row, err := w.createOne(meeting, provenance, fields, op)
		if err != nil {
			return err
		}
		result.Meeting = row.ID
		batch.rows = []*record{row}
		return w.saveRunWrite(scope, meeting, op, batch)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (in MeetingCapture) validate() error {
	if _, err := hex.DecodeString(in.ID); !validID(in.ID) && (err != nil || len(in.ID) != 32) {
		return errorf(CodeInvalid, "the desk recording needs a capture ID")
	}
	if in.Started.IsZero() || in.Ended.Before(in.Started) || in.Ended.Sub(in.Started) > 5*time.Hour {
		return errorf(CodeInvalid, "the recording needs valid start and end times")
	}
	if strings.TrimSpace(in.Title) == "" || len(in.Title) > 256 {
		return errorf(CodeInvalid, "enter a meeting title of up to 256 bytes")
	}
	if len(in.Transcript) > 256<<10 || len(in.PersonNotes) > 32<<10 || len(in.Attendees) > IDsMax ||
		len(in.Notes.Tasks) > IDsMax || len(in.Notes.Decisions) > IDsMax ||
		len(in.Notes.Summary) > 16<<10 || len(in.Notes.Subject) > 1024 || len(in.Notes.Draft) > 32<<10 ||
		len(strings.Join(in.Notes.Decisions, "\n")) > 48<<10 {
		return errorf(CodeInvalid, "the meeting is too large to save")
	}
	for _, task := range in.Notes.Tasks {
		if strings.TrimSpace(task.Title) == "" || len(task.Title) > 1024 {
			return errorf(CodeInvalid, "a meeting task needs a short title")
		}
	}
	return nil
}

func asText(value any) string {
	text, _ := value.(string)
	return text
}

func asStrings(value any) []string {
	raw, _ := json.Marshal(value)
	var list []string
	_ = json.Unmarshal(raw, &list)
	return list
}
