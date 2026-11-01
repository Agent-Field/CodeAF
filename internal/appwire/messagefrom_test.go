package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// A SEND ON THE PERSON'S BEHALF REACHES THE MESSAGE DOOR WITH WHO SENT IT,
// trimmed; a sender's name that is not one short line is refused; and an
// engine with no door for it refuses rather than dropping the name.
func TestASendFromSomeoneReachesTheMessageDoor(t *testing.T) {
	send := methods["agents.send"]
	eng := &messageEngine{}
	if _, err := send.call(eng, json.RawMessage(`{"id":"a1","text":"tidy","from":" afi "}`)); err != nil || eng.message.From != "afi" || eng.message.Text != "tidy" {
		t.Fatalf("the send answered %v, the engine heard %+v", err, eng.message)
	}
	for _, bad := range []string{`"a\nb"`, `"` + strings.Repeat("x", MaxMessageFromBytes+1) + `"`} {
		if _, err := send.call(eng, json.RawMessage(`{"id":"a1","text":"x","from":`+bad+`}`)); codeOf(err) != CodeBadParams {
			t.Fatalf("from %s answered %v", bad, err)
		}
	}
	if _, err := send.call(&sendEngine{}, json.RawMessage(`{"id":"a1","text":"x","from":"afi"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without the door answered %v", err)
	}
	message := methods["crews.message"]
	crew := &sendEngine{}
	if _, err := message.call(crew, json.RawMessage(`{"crew":"c1","text":"hi","from":" afi "}`)); err != nil || crew.crew.From != "afi" {
		t.Fatalf("crews.message answered %v, the engine heard %+v", err, crew.crew)
	}
	if _, err := message.call(crew, json.RawMessage(`{"crew":"c1","text":"hi","from":"a\nb"}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("a bad crew from answered %v", err)
	}
}
