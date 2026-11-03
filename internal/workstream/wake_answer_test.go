package workstream

import (
	"strings"
	"testing"
)

func TestWakeC_ParseAnswer(t *testing.T) {
	answer, err := ParseWakeAnswer("Words ```json\n" + `{"summary":"Next steps","ignored":true,"items":[{"kind":"move","factory":"f","job":1,"to_wave":3,"why":"one\nline"},{"kind":"draft","title":"Follow up","budget_usd":3},{"kind":"draft","title":"Invalid","budget_usd":0},{"kind":"page","page_id":"p","markdown":"Updated"},{"kind":"decision","text":"Keep this"},{"kind":"unknown"}]}` + "\n```")
	if err != nil || len(answer.Items) != 4 || answer.Items[0].Why != "one line" {
		t.Fatalf("answer=%+v err=%v", answer, err)
	}
	if _, err := ParseWakeAnswer("no answer"); err == nil {
		t.Fatal("unreadable answer accepted")
	}
	if _, err := ParseWakeAnswer(`{"summary":"oops"`); err == nil {
		t.Fatal("partial answer accepted")
	}
}

func TestWakeC_ParseBoundsAndOrder(t *testing.T) {
	text := `{"summary":"` + strings.Repeat("é", 1100) + `","items":[`
	for i := 0; i < 12; i++ {
		if i > 0 {
			text += ","
		}
		text += `{"kind":"question","text":"What next?","why":"` + strings.Repeat("é", 110) + `"}`
	}
	answer, err := ParseWakeAnswer(text + `],"unknown":123}`)
	if err != nil || len(answer.Items) != 10 || len(answer.Summary) > 2000 || len(answer.Items[0].Why) > 200 {
		t.Fatalf("bounds: %+v %v", answer, err)
	}
}

func TestWakeE_AnswerFieldsCannotChooseAnotherKindsUndoTarget(t *testing.T) {
	answer, err := ParseWakeAnswer(`{"summary":"Next","items":[{"kind":"draft","title":"Follow up","factory":"f","job":7,"draft_id":"existing","page_id":"person","budget_usd":3},{"kind":"decision","text":"Review","page_id":"person"}]}`)
	if err != nil || len(answer.Items) != 2 {
		t.Fatal(answer, err)
	}
	p := answer.Items[0]
	if p.Factory != "" || p.Job != 0 || p.DraftID != "" || p.PageID != "" || answer.Items[1].PageID != "" {
		t.Fatal("foreign kind fields survived", answer)
	}
}
