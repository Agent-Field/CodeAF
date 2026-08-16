package contextrefs

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type refOp struct {
	Op      string `json:"op"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
	RefID   string `json:"refId"`
}

type embedCall struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

type storeOp struct {
	Op             string `json:"op"`
	ConversationID string `json:"conversationId"`
	Kind           string `json:"kind"`
	Content        string `json:"content"`
}

type refsScenarioResult struct {
	ConversationID string       `json:"conversationId"`
	Results        []any        `json:"results"`
	Stats          ContextStats `json:"stats"`
}

type embedScenarioResult struct {
	Outputs []string     `json:"outputs"`
	Stats   ContextStats `json:"stats"`
}

type storeScenarioResult struct {
	Results []any      `json:"results"`
	Stats   StoreStats `json:"stats"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()
	var fixtures []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return fixtures
}

func argsOf(t *testing.T, raw string) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	return args
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode argument: %v", err)
	}
	return value
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	args := argsOf(t, fx.ArgsJSON)
	switch fx.Fn {
	case "createContextRefs":
		conversationID := decode[string](t, args[0])
		ops := decode[[]refOp](t, args[1])
		refs := CreateContextRefs(conversationID)
		results := []any{}
		for _, op := range ops {
			switch op.Op {
			case "register":
				results = append(results, refs.Register(op.Kind, op.Content))
			case "render":
				results = append(results, refs.Render(op.RefID))
			case "stats":
				results = append(results, refs.Stats())
			default:
				t.Fatalf("unknown ref op %q", op.Op)
			}
		}
		return refsScenarioResult{ConversationID: refs.ConversationID, Results: results, Stats: refs.Stats()}
	case "embedContextBlock":
		refs := CreateContextRefs(decode[string](t, args[0]))
		calls := decode[[]embedCall](t, args[1])
		outputs := []string{}
		for _, call := range calls {
			outputs = append(outputs, EmbedContextBlock(refs, call.Kind, call.Content))
		}
		return embedScenarioResult{Outputs: outputs, Stats: refs.Stats()}
	case "createContextRefsStore":
		ops := decode[[]storeOp](t, args[0])
		store := CreateContextRefsStore()
		first := map[string]*ContextRefs{}
		results := []any{}
		for _, op := range ops {
			switch op.Op {
			case "for":
				refs := store.ForConversation(op.ConversationID)
				if first[op.ConversationID] == nil {
					first[op.ConversationID] = refs
				}
				results = append(results, refs.ConversationID)
			case "register":
				results = append(results,
					store.ForConversation(op.ConversationID).Register(op.Kind, op.Content))
			case "embed":
				results = append(results,
					EmbedContextBlock(store.ForConversation(op.ConversationID), op.Kind, op.Content))
			case "refsStats":
				results = append(results, store.ForConversation(op.ConversationID).Stats())
			case "storeStats":
				results = append(results, store.Stats())
			case "same":
				results = append(results, first[op.ConversationID] == store.ForConversation(op.ConversationID))
			default:
				t.Fatalf("unknown store op %q", op.Op)
			}
		}
		return storeScenarioResult{Results: results, Stats: store.Stats()}
	default:
		t.Fatalf("unknown fixture function %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixtures, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fx := range loadFixtures(t) {
		seen[fx.Fn]++
	}
	for _, fn := range []string{"createContextRefs", "embedContextBlock", "createContextRefsStore"} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
