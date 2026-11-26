package appwire

import "github.com/Agent-Field/codeaf/internal/apps/data"

type MeetingNotesParams struct {
	Transcript  string             `json:"transcript"`
	PersonNotes string             `json:"person_notes"`
	Previous    *data.MeetingNotes `json:"previous,omitempty"`
	Calendar    string             `json:"calendar_event,omitempty"`
}

type meetingCaptureEngine interface {
	MeetingNotes(MeetingNotesParams) (*data.MeetingNotes, error)
	StartingMeetings() ([]data.MeetingStarting, error)
	TranscribeMeeting(MeetingAudioParams) (*MeetingAudioResult, error)
	IngestMeeting(data.MeetingCapture) (*data.MeetingCaptureReceipt, error)
}

type StartingMeetingsResult struct {
	Meetings []data.MeetingStarting `json:"meetings"`
}

type MeetingAudioParams struct {
	Wave []byte `json:"wave"`
}

type MeetingAudioResult struct {
	Text string `json:"text"`
}

func init() {
	methods["meetings.transcribe"] = typed(func(e Engine, p MeetingAudioParams) (*MeetingAudioResult, error) {
		if capture, ok := e.(meetingCaptureEngine); ok {
			return capture.TranscribeMeeting(p)
		}
		return nil, Errorf(CodeUnknownMethod, "This engine does not serve meeting transcription.")
	})
	methods["meetings.starting"] = typed(func(e Engine, _ struct{}) (*StartingMeetingsResult, error) {
		if capture, ok := e.(meetingCaptureEngine); ok {
			meetings, err := capture.StartingMeetings()
			return &StartingMeetingsResult{Meetings: meetings}, err
		}
		return nil, Errorf(CodeUnknownMethod, "This engine does not check the meeting calendar.")
	})
	methods["meetings.notes"] = typed(func(e Engine, p MeetingNotesParams) (*data.MeetingNotes, error) {
		if capture, ok := e.(meetingCaptureEngine); ok {
			return capture.MeetingNotes(p)
		}
		return nil, Errorf(CodeUnknownMethod, "This engine does not serve meeting notes.")
	})
	methods["meetings.ingest"] = typed(func(e Engine, p data.MeetingCapture) (*data.MeetingCaptureReceipt, error) {
		if capture, ok := e.(meetingCaptureEngine); ok {
			return capture.IngestMeeting(p)
		}
		return nil, Errorf(CodeUnknownMethod, "This engine does not accept desk recordings.")
	})
}
