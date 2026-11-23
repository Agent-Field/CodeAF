package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

func TestMediaInputsFollowActualPreparedOccurrencesAndBoundKnownIDs(t *testing.T) {
	p := &presentationIndex{}
	var messages []ai.Message
	for i := range 80 {
		m := textMessage("user", "identical words")
		p.remember(m, &messagePresentation{Audience: audiencePerson, Composed: &ComposedMark{MessageID: fmt.Sprint("message-", i)}})
		messages = append(messages, m)
	}
	selected := append([]ai.Message(nil), messages[3:6]...)
	selected = append(selected, messages[4], textMessage("user", "identical words")) // repeated occurrence + unknown legacy
	got := p.inputMessages(selected)
	if !reflect.DeepEqual(got.IDs, []string{"message-3", "message-5", "message-4"}) || got.Omitted {
		t.Fatalf("prepared inputs guessed: %+v", got)
	}
	got = p.inputMessages(messages)
	if len(got.IDs) != 64 || got.IDs[0] != "message-16" || got.IDs[63] != "message-79" || !got.Omitted {
		t.Fatalf("unbounded/incorrect input snapshot: %+v", got)
	}
}

func TestMediaInputsOnGeneratedSourceMatchTheActualProviderRequest(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	media := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2)), mediaType: "image/png"}
	var agent *Agent
	var expected []string
	var asked string
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first answer"), nil },
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			expected = agent.preparedMediaInputs(messages).IDs
			// The message the person is answering now: the SECOND submit, whose
			// TaskSource.ID the media row must carry as its asking message.
			if source := agent.messageSource(); source != nil {
				asked = source.ID
			}
			return toolResponse("generation-call", "generate_image", `{"prompt":"a circle"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _ = newTestAgent(t, completer, func(c *Config) {
		c.ArtifactsIndex = index
		c.Media = media
		c.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
	})
	if _, err := agent.armFamily([]bare.Tool{beltTool(t, agent, "generate_image")}); err != nil {
		t.Fatal(err)
	}
	firstEvents, err := agent.Submit(context.Background(), "keep these instructions")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, firstEvents)
	events, err := agent.Submit(context.Background(), "make a circle")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	rows := ReadArtifacts(index)
	if len(expected) != 2 || len(rows) != 1 || rows[0].Media == nil || !reflect.DeepEqual(rows[0].Media.InputMessages, expected) || rows[0].Media.Tool != "generation-call" || rows[0].Media.Run == "" ||
		asked == "" || rows[0].Media.Message != asked || rows[0].Media.Message == rows[0].Media.Run {
		t.Fatalf("source did not preserve actual request: expected=%v rows=%+v", expected, rows)
	}
	rows[0].Media.InputMessages[0] = "changed returned copy"
	// A separately read journal fact is immutable and not backed by that slice.
	if again := ReadArtifacts(index); !reflect.DeepEqual(again[0].Media.InputMessages, expected) {
		t.Fatal("returned provenance mutated durable source")
	}
}

func TestMediaInputSnapshotsDoNotLeakAcrossCallsOrIntoEarlyContexts(t *testing.T) {
	early := context.Background()
	first := mediaInputMessages{IDs: []string{"first"}}
	batch := withMediaInputs(early, first)
	first.IDs[0] = "mutated caller"
	if got := mediaInputsFrom(batch); len(got.IDs) != 1 || got.IDs[0] != "first" {
		t.Fatalf("batch alias: %+v", got)
	}
	if got := mediaInputsFrom(early); len(got.IDs) != 0 {
		t.Fatal("early context was relabeled")
	}
	made := madeBy(batch, nil)
	next := withMediaInputs(early, mediaInputMessages{IDs: []string{"second"}})
	_ = madeBy(next, nil)
	if made.Messages.IDs[0] != "first" {
		t.Fatal("late first job inherited second request")
	}
}

func BenchmarkPreparedMediaInputs(b *testing.B) {
	for _, n := range []int{10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			p := &presentationIndex{}
			messages := make([]ai.Message, n)
			for i := range messages {
				messages[i] = textMessage("tool", "result")
			}
			messages[0] = textMessage("user", "request")
			p.remember(messages[0], &messagePresentation{Audience: audiencePerson, Composed: &ComposedMark{MessageID: "actual-user"}})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got := p.inputMessages(messages)
				if len(got.IDs) != 1 {
					b.Fatal("lost actual user")
				}
			}
		})
	}
}
