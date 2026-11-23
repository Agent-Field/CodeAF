package taskdoor_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// Contracts 3, 5 and 9 need the settled task inputs, exact worker model and
// per-call charge fields preserved on the task wire, independently of a chat.
func TestTheWirePreservesTaskInputsAndOneSpendLine(t *testing.T) {
	cases := []struct {
		payload any
		want    string
	}{
		{taskdoor.Start{Chat: "chat", Task: 9, Title: "title", Instruction: "brief", Model: "model", Effort: "high", MaxSteps: 17, NoProgress: 4, Input: taskdoor.Head{Head: "h", Cell: "c"}, Limits: taskdoor.Limits{USD: 2.5, Hours: 0.5}, OriginName: "origin"}, `{"t":"start","attempt":"att_0000000000000001","chat":"chat","task":9,"title":"title","instruction":"brief","model":"model","effort":"high","max_steps":17,"no_progress":4,"input":{"head":"h","cell":"c"},"limits":{"usd":2.5,"hours":0.5},"origin_name":"origin"}`},
		{taskdoor.Spend{Line: taskdoor.SpendLine{Model: "model", Seat: "worker", InputTokens: 100, OutputTokens: 20, CachedTokens: 7, USD: 0.03}}, `{"t":"spend","attempt":"att_0000000000000001","line":{"model":"model","seat":"worker","input_tokens":100,"output_tokens":20,"cached_tokens":7,"usd":0.03}}`},
		{taskdoor.Finished{Outcome: "done", Report: "report", Changed: []string{"out.txt"}, Model: "model", Result: &taskdoor.Head{Head: "result", Cell: "cell"}}, `{"t":"finished","attempt":"att_0000000000000001","outcome":"done","report":"report","changed":["out.txt"],"model":"model","result":{"head":"result","cell":"cell"}}`},
	}
	for _, test := range cases {
		frame, err := taskdoor.NewFrame(attempt(1), test.payload)
		if err != nil {
			t.Fatal(err)
		}
		var stream bytes.Buffer
		if err := taskdoor.WriteFrame(&stream, frame); err != nil {
			t.Fatal(err)
		}
		got, err := taskdoor.ReadFrame(&stream)
		if err != nil {
			t.Fatal(err)
		}
		var actual, expected any
		if err := json.Unmarshal(got.Payload, &actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(test.want), &expected); err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(expected)
		if !bytes.Equal(a, b) {
			t.Fatalf("wire = %s, want %s", a, b)
		}
	}
}
