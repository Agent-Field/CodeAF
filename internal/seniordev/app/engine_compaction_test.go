//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
)

// recordingClient stands in for the OpenRouter transport: it keeps the
// request the summary processor built and answers with a scripted summary.
type recordingClient struct {
	params  []orclient.RequestParams
	summary string
}

func (client *recordingClient) Stream(
	_ context.Context, params orclient.RequestParams,
) (steploop.PartStream, error) {
	client.params = append(client.params, params)
	total := float64(31_000)
	out := float64(120)
	return &steploop.SliceStream{Parts: []orclient.StreamPart{
		orclient.TextStartPart{ID: "t1"},
		orclient.TextDeltaPart{ID: "t1", Delta: client.summary},
		orclient.TextEndPart{ID: "t1"},
		orclient.FinishPart{
			FinishReason: orclient.FinishReason{Unified: "stop"},
			Usage: calc.LanguageModelV3Usage{
				InputTokens:  calc.LanguageModelV3InputTokens{Total: &total},
				OutputTokens: calc.LanguageModelV3OutputTokens{Total: &out},
			},
		},
	}}, nil
}

func wireTestModel() compaction.Model {
	return compaction.Model{
		Message: msgmodel.Model{
			ProviderID: "openrouter", ID: "vendor/model",
			API: msgmodel.ModelAPI{Npm: "@openrouter/ai-sdk-provider", ID: "vendor/model"},
		},
		Overflow: overflow.Model{Limit: calc.ModelLimit{Context: 131_072, Output: 8_192}},
	}
}

type wireTestProvider struct{ model compaction.Model }

func (p wireTestProvider) GetModel(context.Context, string, string) (compaction.Model, error) {
	return p.model, nil
}

func (wireTestProvider) GetProvider(context.Context, string) (compaction.ProviderInfo, error) {
	return compaction.ProviderInfo{}, nil
}

func validWireSummary() string {
	return strings.Join([]string{
		"## Working State",
		"### Completed", "- parse() implemented in src/a.js",
		"### Current", "- (none)",
		"### Verification", "- npm test: 2 failing",
		"### Next", "- fix the failing assertions",
		"### Files", "- src/a.js",
	}, "\n")
}

// Pinned end to end through the real summary factory, the real step
// processor, and the real request-body builder: the bytes that would leave
// for OpenRouter must carry the flattened transcript.
func TestSummaryRequestReachesTheWireWithTheTranscript(t *testing.T) {
	store := newTurnStore()
	client := &recordingClient{summary: validWireSummary()}
	var decisions []compaction.CompactionDecision
	budget := float64(0)
	service := compaction.NewService(compaction.Dependencies{
		Store: store,
		Config: compaction.ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
			return overflow.Config{Compaction: &overflow.CompactionConfig{
				PreserveRecentTokens: &budget,
			}}, nil
		}),
		Agents: compaction.AgentProviderFunc(func(context.Context, string) (compaction.Agent, error) {
			return compaction.Agent{Name: "compaction"}, nil
		}),
		Provider:   wireTestProvider{model: wireTestModel()},
		Processors: seniorDevSummaryFactory{store: store, client: client},
		Evidence:   compaction.FallbackEvidenceSelector{},
		Decisions: compaction.DecisionSinkFunc(func(d compaction.CompactionDecision) {
			decisions = append(decisions, d)
		}),
		Instance: compaction.InstanceContext{Directory: t.TempDir()},
		NewID:    steploop.NewAscendingID,
	})

	const goal = "Fix parse() in src/a.js so nested refs resolve."
	ctx := context.Background()
	user := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: "u0", SessionID: "ses"},
		Agent:       "coder",
		Model:       msgmodel.UserModel{ProviderID: "openrouter", ModelID: "vendor/model"},
	}
	finish := "tool_calls"
	messages := []msgmodel.WithParts{
		{Info: user, Parts: msgmodel.Parts{msgmodel.TextPart{
			PartBase: msgmodel.PartBase{ID: "p0", SessionID: "ses", MessageID: "u0"}, Text: goal,
		}}},
		{Info: msgmodel.Assistant{
			MessageBase: msgmodel.MessageBase{ID: "a0", SessionID: "ses"}, ParentID: "u0",
			ModelID: "vendor/model", ProviderID: "openrouter", Finish: &finish,
		}, Parts: msgmodel.Parts{msgmodel.TextPart{
			PartBase: msgmodel.PartBase{ID: "p1", SessionID: "ses", MessageID: "a0"},
			Text:     "Reading src/a.js before editing.",
		}}},
		{Info: msgmodel.Assistant{
			MessageBase: msgmodel.MessageBase{ID: "a1", SessionID: "ses"}, ParentID: "u0",
			ModelID: "vendor/model", ProviderID: "openrouter", Finish: &finish,
		}, Parts: msgmodel.Parts{msgmodel.TextPart{
			PartBase: msgmodel.PartBase{ID: "p2", SessionID: "ses", MessageID: "a1"},
			Text:     "Newest message, kept verbatim.",
		}}},
		{Info: msgmodel.User{
			MessageBase: msgmodel.MessageBase{ID: "uc", SessionID: "ses"}, Agent: "coder",
			Model: user.Model,
		}, Parts: msgmodel.Parts{msgmodel.CompactionPart{
			PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses", MessageID: "uc"}, Auto: true,
		}}},
	}
	for _, message := range messages {
		if err := store.UpdateMessage(ctx, message.Info); err != nil {
			t.Fatal(err)
		}
		for _, part := range message.Parts {
			if err := store.UpdatePart(ctx, part); err != nil {
				t.Fatal(err)
			}
		}
	}

	result, err := service.Process(ctx, compaction.ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses", Auto: true,
	})
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if len(client.params) != 1 {
		t.Fatalf("summary calls = %d, want 1", len(client.params))
	}
	params := client.params[0]
	if params.Tools != nil || params.ToolChoice != nil {
		t.Fatalf("summary request carried tools: %#v", params.Tools)
	}
	body, err := orclient.BuildRequestBody(params)
	if err != nil {
		t.Fatalf("the summary request does not build a request body: %v", err)
	}
	var decoded struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body shape: %v\n%s", err, body)
	}
	if len(decoded.Messages) != 1 || decoded.Messages[0].Role != "user" {
		t.Fatalf("wire messages = %#v", decoded.Messages)
	}
	content := decoded.Messages[0].Content
	for _, want := range []string{
		"<conversation>", "[User]: " + goal, "[Assistant]: Reading src/a.js before editing.",
		"</conversation>", compaction.SummaryTemplate,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("wire content missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "Newest message, kept verbatim.") {
		t.Fatalf("the verbatim tail was sent to the summarizer:\n%s", content)
	}
	if len(decisions) != 1 || decisions[0].SummaryStatus != "valid" ||
		decisions[0].SummaryPromptTokens != 31_000 || decisions[0].SummaryOutputTokens != 120 ||
		decisions[0].PromptChars < len(content) {
		t.Fatalf("decision = %#v", decisions[0])
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{
		"-c", "user.name=t", "-c", "user.email=t@example.com",
	}, args...)...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestChangedFilesRecordDiffsAgainstTheStartRefAndListsStatus(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "update-ref", soloStartRef, gitIn(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := strings.Join(seniorDevChangedFiles(context.Background(), dir), "\n")
	for _, want := range []string{
		"git diff --stat " + soloStartRef, "a.txt | 1 +", "git status --short", "?? probe.txt",
	} {
		if !strings.Contains(record, want) {
			t.Fatalf("record missing %q:\n%s", want, record)
		}
	}
	if got := seniorDevChangedFiles(context.Background(), t.TempDir()); got != nil {
		t.Fatalf("non-repository produced a record: %v", got)
	}
}
