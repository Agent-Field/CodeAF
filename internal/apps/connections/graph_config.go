package connections

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

func graphCommon(object string) data.CommonTypeKey {
	switch object {
	case "outlook":
		return data.CommonMessage
	case "calendar":
		return data.CommonEvent
	case "drive", "transcripts":
		return data.CommonDocument
	case "person":
		return data.CommonPerson
	case "company":
		return data.CommonOrganization
	}
	return ""
}

var graphFields = map[string]string{
	"outlook":     "thread date subject from to cc labels has_attachments is_draft link snippet attachments body",
	"calendar":    "title description location conference link start end start_date end_date attendees organizer",
	"drive":       "title mime_type modified link description shared sharing transcript text",
	"transcripts": "title mime_type modified link description shared sharing transcript text meeting",
	"person":      "email upn name domain",
	"company":     "domain name",
}

func graphFieldAllowed(object, field string) bool {
	return strings.Contains(" "+graphFields[object]+" ", " "+field+" ")
}

func (c Config) validateGraphRead(r Read) error {
	if graphCommon(r.Table) == "" || r.Key != "id" || (r.Version != "" && r.Version != "version") ||
		r.Path != "" || r.Rows != "" || r.OrderBy != "" || r.Residency != nil {
		return errors.New("a Microsoft 365 read needs a Graph object, id key, and optional version")
	}
	if r.GraphDrive != "" && (r.Table != "drive" || !graphIDValid(r.GraphDrive)) {
		return errors.New("graph_drive needs a reviewed OneDrive or SharePoint drive ID")
	}
	if r.GraphStart != "" || r.GraphEnd != "" {
		if _, _, err := graphWindow(r, nil); err != nil || r.Table != "calendar" {
			return errors.New("graph_start and graph_end need an absolute calendar window")
		}
	}
	if r.Table == "transcripts" {
		if r.GraphMeetingRead == "" || c.Reads[r.GraphMeetingRead].Table != "calendar" {
			return errors.New("a transcript read needs a declared calendar read to link the meeting")
		}
	} else if r.GraphMeetingRead != "" {
		return errors.New("graph_meeting_read is for Teams transcripts")
	}
	if (r.Table == "person" || r.Table == "company") && r.Version != "" {
		return errors.New("Microsoft directory lookups do not return source versions")
	}
	for _, field := range r.Fields {
		if !graphFieldAllowed(r.Table, field) {
			return errors.New("a Microsoft 365 field must name a supported Graph mapping")
		}
	}
	params := map[string]string{"calendar": "from to", "transcripts": "meeting", "person": "email", "company": "domain"}
	for _, target := range r.Match {
		if !strings.Contains(" "+params[r.Table]+" ", " "+target+" ") {
			return errors.New("a Microsoft 365 read argument must name an approved Graph parameter")
		}
	}
	return nil
}

const (
	graphDraftAction       = "create-draft"
	graphEventCreateAction = "create-event"
	graphEventUpdateAction = "update-event"
)

func validateGraphAction(operation string, read Read, write Write) error {
	if write.Path != "" || len(write.Response) != 0 {
		return errors.New("Microsoft 365 Actions use a fixed Graph route and receipt")
	}
	fields := ""
	switch operation {
	case graphDraftAction:
		if read.Table == "outlook" {
			fields = "to cc subject body"
		}
	case graphEventCreateAction, graphEventUpdateAction:
		if read.Table == "calendar" && (operation != graphEventUpdateAction || read.Version == "version") {
			fields = "title start end attendees location description"
		}
	}
	if fields == "" {
		return errors.New("Microsoft 365 Actions support an Outlook draft or a Calendar create or update")
	}
	for _, name := range write.Fields {
		if !strings.Contains(" "+fields+" ", " "+read.Fields[name]+" ") {
			return errors.New("the Graph Action cannot write that field")
		}
	}
	return nil
}
