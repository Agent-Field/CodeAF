package delegate

import (
	"encoding/json"
	"testing"
)

// SEC'S "NOTHING TO AUDIT" PASS IS A FACT, NOT A SENTENCE. The ending sec
// writes when an audit of the changes finds no change carries
// nothing_to_audit:true, and the line and the page keep it beside the status
// and nothing else: no count is invented for an audit that did not run.
func TestNothingToAuditIsGoldenOnTheWire(t *testing.T) {
	end := Ending{Status: StatusPass, Message: "it had nothing to audit: nothing has changed",
		Deliverable: "Security audit of the changes: nothing has changed, so there was nothing to audit.",
		Extra:       map[string]any{"status": "pass", "nothing_to_audit": true}}
	matchGolden(t, "sec-nothing", endLineAndPage(t, "sec", end))
}

// THE FACT IS TRUE-ONLY, AND AN OLD ENDING IS NOT READ FROM ITS WORDS. sec
// never writes false, so a false, a null or any other shape is absent; and a
// line written before the fact existed — the same status, the same sentence —
// says nothing about it, because no sentence is parsed for a fact.
func TestNothingToAuditIsTrueOnlyAndNeverReadFromTheSentence(t *testing.T) {
	for _, raw := range []string{`false`, `null`, `"true"`, `1`, `{}`} {
		facts := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{
			"nothing_to_audit": json.RawMessage(raw), "status": json.RawMessage(`"pass"`)}}).Ending()
		if facts == nil || facts.NothingToAudit != nil {
			t.Errorf("nothing_to_audit %s read as %+v", raw, facts)
		}
	}
	old := Action{At: endAt, Kind: ActionEnd, Status: StatusPass, Message: "it had nothing to audit: nothing has changed",
		Data: json.RawMessage(`{"status":"pass"}`)}
	shown, ok := Delegate{Name: "sec"}.Reader()(old)
	if !ok || shown.Ending == nil || shown.Ending.NothingToAudit != nil {
		t.Fatalf("an old ending was read from its sentence: %+v", shown.Ending)
	}
	bare := Action{At: endAt, Kind: ActionEnd, Status: StatusPass, Message: "it had nothing to audit: nothing has changed"}
	if bare.Ending() != nil {
		t.Fatalf("an ending with no data read as %+v", bare.Ending())
	}
	alone := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{"nothing_to_audit": json.RawMessage(`true`)}})
	if string(alone.Data) != `{"nothing_to_audit":true}` || alone.Ending() == nil || !*alone.Ending().NothingToAudit {
		t.Fatalf("the fact alone wrote %s", alone.Data)
	}
}
