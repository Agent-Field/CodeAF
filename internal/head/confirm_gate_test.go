package head

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The key path may not buy what a sentence has to ask for. ConfirmSurgery is
// the same gate, the same durable question, and the same encoded option that
// replays the command when the answer comes back — asked of the head that is
// already in the process rather than reimplemented beside it.
func TestConfirmSurgeryGatesTheKeyPathExactlyAsASentenceIsGated(t *testing.T) {
	t.Run("small work is simply done", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "cheap", "Cheap job", "rename a file")
		asked, err := New(&fakeClient{}, graph).ConfirmSurgery("keys", store.CommandCancel, "cheap")
		if err != nil {
			t.Fatal(err)
		}
		if asked {
			t.Fatal("a cheap young job was gated")
		}
		questions, err := graph.OpenQuestions("keys", 10)
		if err != nil || len(questions) != 0 {
			t.Fatalf("open questions = %+v err=%v", questions, err)
		}
	})

	t.Run("a large loss asks and journals nothing", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "expensive", "Expensive job", "render the whole catalogue")
		// The loss is spelled FROM THE GATE and not as a literal, so raising the
		// gate cannot quietly turn this case into the cheap one above it and
		// leave the test passing about nothing.
		loss := store.SurgerySpendGateUSD * 7
		if err := graph.RecordUsage(store.NodeUsage{NodeID: "expensive", Cost: loss}); err != nil {
			t.Fatal(err)
		}
		asked, err := New(&fakeClient{}, graph).ConfirmSurgery("keys", store.CommandCancel, "expensive")
		if err != nil {
			t.Fatal(err)
		}
		if !asked {
			t.Fatalf("$%.2f of work was thrown away without a word", loss)
		}
		questions, err := graph.OpenQuestions("keys", 10)
		if err != nil || len(questions) != 1 {
			t.Fatalf("open questions = %+v err=%v", questions, err)
		}
		question := questions[0]
		if question.OriginNodeID != "expensive" || question.Urgency != store.QuestionBlocking {
			t.Fatalf("confirm question = %+v", question)
		}
		if !strings.Contains(question.Text, "Cancel Expensive job") ||
			!strings.Contains(question.Text, fmt.Sprintf("%s spent", moneyUSD(loss))) {
			t.Fatalf("the question does not name the loss: %q", question.Text)
		}
		// The answer is what journals the command, through the same encoded
		// option a spoken confirm produces.
		action, kind, target, _, ok := decodeSurgeryOption(question.Options[0].Value)
		if !ok || action != "apply" || kind != store.CommandCancel || target != "expensive" {
			t.Fatalf("confirm option = %+v ok=%t", question.Options[0], ok)
		}
		pending, err := graph.PendingCommands(0)
		if err != nil || len(pending) != 0 {
			t.Fatalf("a gated key press journalled %+v err=%v", pending, err)
		}
	})
}
