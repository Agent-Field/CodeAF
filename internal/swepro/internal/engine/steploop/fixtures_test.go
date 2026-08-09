package steploop

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtureLines(t *testing.T) []fixtureLine {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var fixtures []fixtureLine
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func fixtureStringify(t *testing.T, value any) string {
	t.Helper()
	raw, err := jscompat.Stringify(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type exitFixtureArgs struct {
	Finish           *string         `json:"finish"`
	PendingToolParts bool            `json:"pendingToolParts"`
	Ordering         string          `json:"ordering"`
	AssistantPresent *bool           `json:"assistantPresent"`
	ProviderExecuted json.RawMessage `json:"providerExecuted"`
	UserID           string          `json:"userID"`
	AssistantID      string          `json:"assistantID"`
}

func runExitFixture(t *testing.T, raw string) bool {
	t.Helper()
	var args exitFixtureArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	userID := args.UserID
	if userID == "" {
		switch args.Ordering {
		case "lt":
			userID = "msg_0001"
		case "eq":
			userID = "msg_0002"
		default:
			userID = "msg_0003"
		}
	}
	assistantID := args.AssistantID
	if assistantID == "" {
		assistantID = "msg_0002"
	}
	user := msgmodel.User{MessageBase: msgmodel.MessageBase{ID: userID}}
	assistant := msgmodel.Assistant{MessageBase: msgmodel.MessageBase{ID: assistantID}, Finish: args.Finish}
	msgs := []msgmodel.WithParts{}
	if args.AssistantPresent == nil || *args.AssistantPresent {
		parts := msgmodel.Parts{}
		if args.PendingToolParts {
			var metadata msgmodel.RawObject
			if len(args.ProviderExecuted) > 0 {
				metadata = msgmodel.RawObject(append(append([]byte(`{"providerExecuted":`), args.ProviderExecuted...), '}'))
			}
			parts = append(parts, msgmodel.ToolPart{
				PartBase: msgmodel.PartBase{ID: "prt_1", MessageID: assistantID},
				CallID:   "call_1",
				Tool:     "fake",
				State:    msgmodel.PendingToolState(),
				Metadata: metadata,
			})
		}
		msgs = append(msgs, msgmodel.WithParts{Info: assistant, Parts: parts})
	}
	return ShouldExit(&user, &assistant, msgs)
}

type simplePart struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic *bool  `json:"synthetic"`
	Ignored   *bool  `json:"ignored"`
}

type simpleInfo struct {
	Role string `json:"role"`
	ID   string `json:"id"`
}

type simpleMessage struct {
	Info  simpleInfo   `json:"info"`
	Parts []simplePart `json:"parts"`
}

func buildSimpleMessages(input []simpleMessage) []msgmodel.WithParts {
	out := make([]msgmodel.WithParts, 0, len(input))
	for _, message := range input {
		var info msgmodel.Info
		if message.Info.Role == "assistant" {
			info = msgmodel.Assistant{MessageBase: msgmodel.MessageBase{ID: message.Info.ID}}
		} else {
			info = msgmodel.User{MessageBase: msgmodel.MessageBase{ID: message.Info.ID}}
		}
		parts := msgmodel.Parts{}
		for index, part := range message.Parts {
			base := msgmodel.PartBase{ID: "prt_" + jscompat.FormatNumber(float64(index)), MessageID: message.Info.ID}
			if part.Type == msgmodel.PartTypeText {
				parts = append(parts, msgmodel.TextPart{
					PartBase: base, Text: part.Text, Synthetic: part.Synthetic, Ignored: part.Ignored,
				})
			} else {
				parts = append(parts, msgmodel.ReasoningPart{
					PartBase: base, Text: part.Text, Time: msgmodel.TimeStartEnd{},
				})
			}
		}
		out = append(out, msgmodel.WithParts{Info: info, Parts: parts})
	}
	return out
}

func numberSpecValue(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	if len(raw) > 0 && raw[0] == '"' {
		var sentinel string
		if err := json.Unmarshal(raw, &sentinel); err != nil {
			t.Fatal(err)
		}
		switch sentinel {
		case "@@num:NaN":
			return math.NaN()
		case "@@num:Infinity":
			return math.Inf(1)
		case "@@num:-Infinity":
			return math.Inf(-1)
		case "@@num:-0":
			return math.Copysign(0, -1)
		}
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

type recoverySpec struct {
	TaskID             string          `json:"taskID"`
	Title              string          `json:"title"`
	Reason             string          `json:"reason"`
	Bugs               []FailureBug    `json:"bugs"`
	RepairHints        []string        `json:"repairHints"`
	BlockedDescendants json.RawMessage `json:"blockedDescendants"`
}

func TestFixtures(t *testing.T) {
	fixtures := loadFixtureLines(t)
	counts := map[string]int{}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			var got string
			switch fixture.Fn {
			case "shouldExit":
				got = fixtureStringify(t, runExitFixture(t, fixture.ArgsJSON))
			case "planDBInfoFromMessages":
				var args struct {
					Msgs []simpleMessage `json:"msgs"`
				}
				if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
					t.Fatal(err)
				}
				got = fixtureStringify(t, PlanDBInfoFromMessages(buildSimpleMessages(args.Msgs)))
			case "renderRecoveryReminder":
				var args struct {
					Open []recoverySpec `json:"open"`
				}
				if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
					t.Fatal(err)
				}
				open := make([]OpenFailure, 0, len(args.Open))
				for _, spec := range args.Open {
					open = append(open, OpenFailure{
						TaskID: spec.TaskID, Title: spec.Title, Reason: spec.Reason,
						Bugs: spec.Bugs, RepairHints: spec.RepairHints,
						BlockedDescendants: numberSpecValue(t, spec.BlockedDescendants),
					})
				}
				got = fixtureStringify(t, RenderRecoveryReminder(open))
			case "wrapLateUserText":
				var args struct {
					LastFinishedID string          `json:"lastFinishedID"`
					Msgs           []simpleMessage `json:"msgs"`
				}
				if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
					t.Fatal(err)
				}
				msgs := buildSimpleMessages(args.Msgs)
				WrapLateUserText(msgs, msgmodel.Assistant{MessageBase: msgmodel.MessageBase{ID: args.LastFinishedID}})
				texts := []string{}
				for _, message := range msgs {
					for _, raw := range message.Parts {
						if part, ok := raw.(msgmodel.TextPart); ok {
							texts = append(texts, part.Text)
						}
					}
				}
				got = fixtureStringify(t, texts)
			default:
				t.Fatalf("unknown fixture fn %q", fixture.Fn)
			}
			if got != fixture.OutJSON {
				t.Fatalf("mismatch\nwant: %s\n got: %s", fixture.OutJSON, got)
			}
		})
		counts[fixture.Fn]++
	}
	if len(fixtures) < 80 {
		t.Fatalf("fixture corpus truncated: %d", len(fixtures))
	}
	for _, fn := range []string{"shouldExit", "planDBInfoFromMessages", "renderRecoveryReminder", "wrapLateUserText"} {
		if counts[fn] == 0 {
			t.Fatalf("no fixtures for %s", fn)
		}
	}
}
