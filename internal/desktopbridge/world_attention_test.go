package desktopbridge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestWorldAttentionPresenceFields(t *testing.T) {
	now := time.Now().UTC()
	dir := t.TempDir()
	full := &session.Question{ID: 7, Kind: "consent", Head: "Allow the port?", Stakes: session.StakesCostly,
		Blocking: session.Blocking{Turn: true, Tasks: []string{"Port parser", "Release"}},
		Options:  []session.AnswerOption{{Key: "yes", Label: "Allow"}},
		Pick:     &session.Pick{Key: "yes", Percent: 92, Reason: "Keeps the release moving"},
	}
	graph, err := placegraph.Open(placegraph.Options{Path: filepath.Join(dir, "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	place, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Parser"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := graph.AddChat("chat", place.ID, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	for _, rich := range []bool{true, false} {
		t.Run(map[bool]string{true: "full", false: "legacy"}[rich], func(t *testing.T) {
			presence := session.SessionPresence{Schema: 1, SessionID: "chat", UpdatedAt: now, State: session.PresenceWaiting,
				Question: session.PresenceQuestion{Kind: "consent", ID: 7, Text: "Allow the port?", Options: full.Options},
			}
			if rich {
				presence.Question.Full = full
			}
			raw, err := json.Marshal(presence)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "presence.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			persisted, live := session.ReadSessionPresence(dir, now)
			if !live {
				t.Fatal("presence was not readable")
			}
			feed := NewWorldFeed(func() session.World {
				return session.World{Projects: []session.Project{{Sessions: []session.SessionRow{{ID: "chat", Dir: dir, Live: live, Presence: persisted}}}}}
			})
			defer feed.Close()
			bridge := New(worldToken, nil)
			bridge.UseWorld(feed)
			bridge.UsePlaces(NewPlaces(graph))
			defer bridge.Close()
			server := httptest.NewServer(bridge.Handler())
			defer server.Close()
			srv := server.URL
			request, _ := http.NewRequest(http.MethodGet, srv+"/api/engine/world", nil)
			request.Header.Set("Authorization", "Bearer "+worldToken)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("world: %d %s", response.StatusCode, body)
			}
			var payload struct {
				Items []json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Items) != 1 {
				t.Fatalf("items = %s", body)
			}
			want := `{"key":"chat:consent:7","session":"chat","kind":"consent","id":7,"text":"Allow the port?","sourceFolders":[],"answerable":true}`
			if !rich {
				if string(payload.Items[0]) != want {
					t.Fatalf("legacy bytes changed:\n%s\nwant %s", payload.Items[0], want)
				}
				return
			}
			var item AttentionItem
			if err := json.Unmarshal(payload.Items[0], &item); err != nil {
				t.Fatal(err)
			}
			if item.Blocking == nil || !item.Blocking.Turn || !reflect.DeepEqual(item.Blocking.Tasks, full.Blocking.Tasks) || item.Stakes != full.Stakes ||
				!reflect.DeepEqual(item.HoldingUp, full.Blocking.Tasks) || !reflect.DeepEqual(item.PlaceIDs, []string{place.ID}) || !reflect.DeepEqual(item.PlaceNames, []string{"Parser"}) ||
				!reflect.DeepEqual(item.Suggestion, &AttentionSuggestion{Key: "yes", Label: "Allow", Percent: 92, Reason: full.Pick.Reason}) {
				t.Fatalf("full projection = %s", payload.Items[0])
			}
		})
	}
}

func TestWorldAttentionOmitsWithdrawnAndUnknownDetails(t *testing.T) {
	row := asking("chat", "Choose")
	row.Presence.Question.Full = &session.Question{Withdrawn: &session.Withdrawal{Reason: "Already decided"}}
	_, items := projectWorld(session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}, "")
	if len(items) != 0 {
		t.Fatalf("withdrawn question still present: %+v", items)
	}
	row.Presence.Question.Full = &session.Question{}
	raw, err := json.Marshal(attentionFor(row, projectRow(session.Project{}, row)))
	if err != nil {
		t.Fatal(err)
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"stakes", "suggestion", "placeIds", "placeNames", "holdingUp"} {
		if _, exists := item[field]; exists {
			t.Errorf("unknown %s was invented: %s", field, raw)
		}
	}
}

func TestWorldAttentionOmitsRecordedAnswers(t *testing.T) {
	row := asking("chat", "Choose")
	row.Dir = t.TempDir()
	row.Presence.Question.Full = &session.Question{ID: 7, Kind: "consent"}
	record := session.DecisionRecord{ID: 7, Kind: "consent", By: session.DecidedByAsker}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.DecisionsPath(row.Dir), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}
	_, items := projectWorld(world, "")
	if len(items) != 0 {
		t.Fatalf("answered question still present: %+v", items)
	}
	row.Presence.Question.Full.Head = "A different question using the same token"
	world.Projects[0].Sessions[0] = row
	_, items = projectWorld(world, "")
	if len(items) != 1 {
		t.Fatal("an old answer hid a different question")
	}
	row.Presence.Question.Full.Head = ""
	row.Presence.Question.Full.Kind = "task"
	world.Projects[0].Sessions[0] = row
	_, items = projectWorld(world, "")
	if len(items) != 1 {
		t.Fatal("answer to another kind hid the question")
	}
}

func TestWorldAttentionSuggestionUsesCanonicalConfidence(t *testing.T) {
	row := asking("chat", "Choose")
	pick := &session.Pick{Key: "yes", Confidence: session.ConfidenceSure}
	row.Presence.Question.Full = &session.Question{Pick: pick}
	item := attentionFor(row, projectRow(session.Project{}, row))
	if item.Suggestion.Percent != pick.ResolvedPercent() || item.Suggestion.Label != "" {
		t.Fatalf("suggestion invented evidence: %+v", item.Suggestion)
	}
}
