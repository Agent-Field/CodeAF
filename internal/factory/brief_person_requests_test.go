package factory_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestBriefPersonRequests(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	const words = "also add a second line: see you"
	it := factory.Item{Title: "Farewell", Body: "Add a line saying farewell"}
	before := factory.BriefInputHash(factory.BriefInputOf(it))
	it.AnsweredQuestions = []factory.Question{{FromStep: "test", Kind: "approve", Answer: &factory.Answer{Text: words, At: at}}}
	in := factory.BriefInputOf(it)
	var got struct {
		Requests []struct {
			Text string    `json:"text"`
			Step string    `json:"step"`
			Gate string    `json:"gate"`
			At   time.Time `json:"at"`
		} `json:"person_requests"`
	}
	if err := json.Unmarshal([]byte(factory.BriefInputText(in)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Requests) != 1 {
		t.Fatalf("writer input person_requests = %d; want later request %q", len(got.Requests), words)
	}
	r := got.Requests[0]
	if r.Text != words || r.Step != "test" || r.Gate != "approve" || !r.At.Equal(at) {
		t.Fatalf("later request lost words or context: %+v", r)
	}
	if factory.BriefInputHash(in) == before {
		t.Error("a person's later request did not change the brief input hash")
	}
	for _, rule := range []string{"issue plus person_requests", "later request wins", "never reported as a fault", "real miss"} {
		if !strings.Contains(factory.BriefSystem(), rule) {
			t.Errorf("BriefSystem missing request rule %q", rule)
		}
	}
}
