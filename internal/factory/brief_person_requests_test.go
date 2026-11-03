package factory_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	t.Run("sources and oldest first", func(t *testing.T) {
		job := factory.Item{Notes: []string{"first note", words, "last note"}, AnsweredQuestions: []factory.Question{
			{}, {Answer: &factory.Answer{Text: " continue "}}, {Answer: &factory.Answer{Text: "YES"}}, {Answer: &factory.Answer{Text: " No "}}, {Answer: &factory.Answer{Text: " "}},
			{FromStep: "proof", Answer: &factory.Answer{Text: "no, keep the extra line", At: at.Add(time.Hour)}},
			{FromStep: "test", Answer: &factory.Answer{Text: words, At: at}},
		}}
		requests := factory.BriefInputOf(job).PersonRequests
		var texts []string
		for _, r := range requests {
			texts = append(texts, r.Text)
		}
		if want := []string{"first note", "last note", words, "no, keep the extra line"}; !reflect.DeepEqual(texts, want) {
			t.Fatalf("requests = %q; want %q", texts, want)
		}
		if !requests[0].At.IsZero() || requests[0].Step != "" || requests[2].Step != "test" {
			t.Fatalf("invented or lost context: %+v", requests)
		}
	})
	t.Run("newest survive count and input cuts", func(t *testing.T) {
		job := factory.Item{}
		for i := range 20 {
			job.Notes = append(job.Notes, fmt.Sprintf("request %02d", i))
		}
		input := factory.BriefInputOf(job)
		if len(input.PersonRequests) != 12 || input.PersonRequests[0].Text != "request 08" || input.PersonRequests[11].Text != "request 19" {
			t.Fatalf("count cuts did not keep newest: %+v", input.PersonRequests)
		}
		want := append([]factory.BriefPersonRequest(nil), input.PersonRequests...)
		input.Plan = strings.Repeat("plan", 20000)
		input.Steps = []factory.BriefStepInput{{Output: strings.Repeat("output", 20000)}}
		input.Previous = &factory.BriefVersion{Verdict: strings.Repeat("previous", 20000)}
		raw := factory.BriefInputText(input)
		var decoded factory.BriefInput
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatal(err)
		}
		if len(raw) > factory.BriefInputMost || !reflect.DeepEqual(decoded.PersonRequests, want) {
			t.Fatalf("input cuts lost requests or exceeded cap: %d bytes, %+v", len(raw), decoded.PersonRequests)
		}
	})
	t.Run("text and encoded byte bounds", func(t *testing.T) {
		job := factory.Item{Notes: []string{strings.Repeat("界", 1000)}}
		requests := factory.BriefInputOf(job).PersonRequests
		if len(requests) != 1 || len(requests[0].Text) > 2<<10 || !utf8.ValidString(requests[0].Text) {
			t.Fatalf("invalid bounded text: %+v", requests)
		}
		job.Notes = nil
		for i := range 12 {
			job.Notes = append(job.Notes, fmt.Sprintf("%02d", i)+strings.Repeat("\x00", 2000))
		}
		requests = factory.BriefInputOf(job).PersonRequests
		raw, err := json.Marshal(requests)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > 16<<10 || len(requests) == 0 || !strings.HasPrefix(requests[len(requests)-1].Text, "11") {
			t.Fatalf("JSON byte bound lost newest: %d bytes, %d requests", len(raw), len(requests))
		}
	})
}
