package plan

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type satisfiedClient struct {
	messages []ai.Message
	reply    string
}

func (c *satisfiedClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.messages = append([]ai.Message(nil), messages...)
	return response(c.reply), nil
}

// The cache shape is the whole economy of this call: static prompt, then the
// criterion — fixed for the job's lifetime — then the append-only landed table,
// and only then the part that changes when the job's shape does.
func TestSatisfiedAsksInTheCacheableOrder(t *testing.T) {
	client := &satisfiedClient{reply: `{"complete":false,"uncovered":[{"condition":"the table","missing":"the second option"}]}`}
	criterion := Done{
		Produces:   []string{"the comparison table"},
		Conditions: []Check{{Kind: CheckRead, Check: "the table", Expect: "it names both options"}},
	}
	verdict, usage, err := Satisfied(context.Background(), client, criterion,
		[]Landed{{Title: "First half", Result: "the first option is written up"}},
		[]Spec{{Instruction: "write the second option", Done: Done{Produces: []string{"the second write-up"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Complete || len(verdict.Uncovered) != 1 || verdict.Uncovered[0].Missing != "the second option" {
		t.Fatalf("verdict = %+v", verdict)
	}
	if usage.Calls != 1 {
		t.Fatalf("usage = %+v", usage)
	}
	if len(client.messages) != 4 || client.messages[0].Role != "system" {
		t.Fatalf("message shape = %d messages, first %q", len(client.messages), client.messages[0].Role)
	}
	if !strings.Contains(textOf(client.messages[0]), "You decide whether a job still needs work") {
		t.Fatalf("the static prompt is not first:\n%s", textOf(client.messages[0]))
	}
	for index, want := range []string{"it names both options", "the first option is written up", "write the second option"} {
		if !strings.Contains(textOf(client.messages[index+1]), want) {
			t.Fatalf("message %d does not carry %q:\n%s", index+1, want, textOf(client.messages[index+1]))
		}
	}
	if !strings.Contains(textOf(client.messages[3]), "the second write-up") {
		t.Fatalf("the in-flight commitment was dropped:\n%s", textOf(client.messages[3]))
	}
}

// A verdict of complete with named gaps is the model disagreeing with itself,
// and the safe reading of a disagreement about whether to stop is that it did
// not say stop.
func TestSatisfiedTreatsSelfContradictionAsIncomplete(t *testing.T) {
	client := &satisfiedClient{reply: `{"complete":true,"uncovered":[{"condition":"the table","missing":"everything"}]}`}
	verdict, _, err := Satisfied(context.Background(), client, Done{Produces: []string{"a table"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Complete {
		t.Fatalf("a self-contradicting verdict stopped a job: %+v", verdict)
	}
}

// Nothing is known about what done means, so nothing can be said about whether
// it is reached — and the answer that cannot truncate work is the one given.
func TestSatisfiedRefusesToAnswerWithoutACriterion(t *testing.T) {
	client := &satisfiedClient{reply: `{"complete":true,"uncovered":[]}`}
	verdict, usage, err := Satisfied(context.Background(), client, Done{}, nil, nil)
	if err != nil || verdict.Complete || usage.Calls != 0 || len(client.messages) != 0 {
		t.Fatalf("an empty criterion was answered: %+v %+v err=%v", verdict, usage, err)
	}
	if verdict, _, err := Satisfied(context.Background(), nil, Done{Produces: []string{"a table"}}, nil, nil); err != nil || verdict.Complete {
		t.Fatalf("a build with no client answered: %+v err=%v", verdict, err)
	}
}

// The two tables are bounded: a landed result's own summary is the expensive
// half and the least load-bearing, because the question is whether a condition
// is covered and not how well.
func TestSatisfiedBoundsTheTables(t *testing.T) {
	client := &satisfiedClient{reply: `{"complete":false,"uncovered":[]}`}
	landed := make([]Landed, 0, satisfiedLanded+5)
	for index := 0; index < satisfiedLanded+5; index++ {
		landed = append(landed, Landed{Title: "piece", Result: strings.Repeat("x", satisfiedResultBytes*2)})
	}
	if _, _, err := Satisfied(context.Background(), client, Done{Produces: []string{"a table"}}, landed, nil); err != nil {
		t.Fatal(err)
	}
	block := textOf(client.messages[2])
	if strings.Count(block, "- piece") != satisfiedLanded {
		t.Fatalf("the landed table is unbounded: %d entries", strings.Count(block, "- piece"))
	}
	if strings.Contains(block, strings.Repeat("x", satisfiedResultBytes+1)) {
		t.Fatal("a landed result was carried at full length")
	}
}
