package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestPrepareModelN1OneSpecificNoticeAndFallbackRules(t *testing.T) {
	for _, fallback := range []string{"", "local/default"} {
		t.Run("fallback="+fallback, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "transcript.jsonl")
			const notice = "This chat used remote/model on B. It isn't set up here, so it continues with local/default."
			if err := SaveMeta(filepath.Dir(file), Meta{ID: "n1-chat", Model: "remote/model", ModelIdentity: &ModelIdentity{
				Selected: "remote/model", Requested: "remote/model", Last: "remote/model",
				Models: &ConversationModels{Main: "remote/model", Fallback: fallback},
			}}); err != nil {
				t.Fatal(err)
			}
			a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.SessionFile = file
				c.Place = Place{Dir: filepath.Dir(file)}
				c.PrepareModel = func(string, modelsource.Set) (string, string, error) {
					return "local/default", notice, nil
				}
			})
			events, err := a.Submit(t.Context(), "continue here")
			if err != nil {
				t.Fatal(err)
			}
			var published []ModelSwitch
			for event := range events {
				if event.ModelSwitch != nil {
					published = append(published, *event.ModelSwitch)
				}
			}
			a.SettleWrites()
			kept := ReadTranscript(file).ModelSwitches
			want := 1
			if fallback != "" {
				want++
			}
			if len(kept) != want || len(published) != want || kept[0].Text != notice || published[0].Text != notice {
				t.Fatalf("N1 notices: kept=%+v published=%+v", kept, published)
			}
			if pair, ok := a.ConversationModelPair(); !ok || pair.Main != "local/default" || pair.Fallback != "" {
				t.Fatalf("N1 model rules: %+v", pair)
			}
		})
	}
}
