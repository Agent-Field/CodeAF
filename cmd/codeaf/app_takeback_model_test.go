package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestAppTakeC1bSavedModelTravelsBack(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(map[bool]string{true: "shared provider", false: "B-only provider"}[available], func(t *testing.T) {
			celltest.RequireEngine(t)
			_, workspace := wakeEnvironment(t, e2TempDir(t))
			model := appTwoMessageSource(t, "echo takeback-model")
			t.Cleanup(model.Close)
			requested := make(chan string, 1024)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Model string }
				if r.Method == http.MethodPost {
					// Restore the body so the ordinary scripted provider handles it.
					raw := json.RawMessage{}
					if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
						t.Error(err)
						return
					}
					_ = json.Unmarshal(raw, &body)
					r.Body = io.NopCloser(strings.NewReader(string(raw)))
					requested <- body.Model
				}
				model.Config.Handler.ServeHTTP(w, r)
			}))
			t.Cleanup(endpoint.Close)
			t.Setenv("CODEAF_BASE_URL", endpoint.URL)
			relay := e2WireRelay(t)
			homeA, homeB := outageHomes(t, relay.URL)
			const x = "fixture/model-x"
			y := "fixture/model-y"
			if !available {
				y = "ollama/model-y"
				if err := config.WriteSources(homeB, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: endpoint.URL}}); err != nil {
					t.Fatal(err)
				}
			}
			for _, dir := range []string{homeA, homeB} {
				if err := config.WriteChatModel(dir, x); err != nil {
					t.Fatal(err)
				}
			}
			a, b := e2StartApp(t, homeA), e2StartApp(t, homeB)
			created := machineCall[struct{ Agent appwire.Agent }](t, a.w, "agents.create", map[string]any{"workspace": workspace, "model": x}).Agent
			e2Turn(t, a, created.ID, "A on X")
			row := outageChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.Title != "" && r.CanTake && r.Seen.Head != "" })
			fresh := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
			if fresh.Model != x {
				t.Fatalf("C1b fresh receiver used %q, want %q", fresh.Model, x)
			}
			assertC1bNotices(t, b, fresh.ID, 0, "")
			const fallback = "fixture/fallback-y"
			machineCall[any](t, b.w, "agents.set_models", map[string]any{"id": fresh.ID, "main": y, "fallback": fallback})
			e2Turn(t, b, fresh.ID, "B on Y")
			// B saves a turn in several publications. Take back only what A has
			// seen of B's last one: a head that both computers report, twice.
			var back appwire.ChatRow
			settled := ""
			outageWait(t, "C1b A sees B's last save", func() bool {
				mine := outageChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head != row.Seen.Head })
				back = outageChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return r.CanTake && r.Seen.Head != row.Seen.Head })
				if back.Seen.Head != mine.Seen.Head {
					settled = ""
					return false
				}
				if settled != back.Seen.Head {
					settled = back.Seen.Head
					time.Sleep(time.Second)
					return false
				}
				return true
			})
			cursor := func() int {
				a.mu.Lock()
				defer a.mu.Unlock()
				return len(a.events)
			}()
			taken := machineCall[appwire.TakeResult](t, a.w, "agents.take", appwire.TakeParams{Cell: back.Cell, Seen: back.Seen}).Agent
			want, notices, line := y, 0, ""
			if !available {
				want, notices = x, 1
				line = "This chat used " + y + " on B. It isn't set up here, so it continues with " + x + "."
			}
			if taken.Model != want || taken.ModelIdentity == nil || taken.ModelIdentity.Models == nil || taken.ModelIdentity.Models.Main != want || taken.ModelIdentity.Models.Fallback != fallback {
				t.Fatalf("C1b take back: model=%q identity=%+v, want main=%s fallback=%s", taken.Model, taken.ModelIdentity, want, fallback)
			}
			for len(requested) > 0 {
				<-requested
			}
			e2Turn(t, a, taken.ID, "A after taking back")
			var seen []string
			for len(requested) > 0 {
				seen = append(seen, <-requested)
			}
			wireModel := strings.TrimPrefix(want, "ollama/")
			found := false
			for _, model := range seen {
				found = found || model == wireModel
				if model == "model-y" && !available {
					t.Fatal("C1b unavailable model reached A's provider")
				}
			}
			if !found {
				t.Fatalf("C1b turn did not use %s: %q", want, seen)
			}
			assertC1bNotices(t, a, taken.ID, cursor, line, notices)
		})
	}
}

func assertC1bNotices(t *testing.T, a *e2App, id string, cursor int, line string, counts ...int) {
	t.Helper()
	want := 0
	if len(counts) > 0 {
		want = counts[0]
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var notices []string
	for _, frame := range a.events[cursor:] {
		var added appwire.EntryAdded
		if frame.Event == appwire.EventEntryAdded && json.Unmarshal(frame.Data, &added) == nil && added.Entry.Agent == id && (added.Entry.NoticeKind == "model_switch" || strings.HasPrefix(added.Entry.Text, "This chat used ")) {
			notices = append(notices, added.Entry.Text)
		}
	}
	if len(notices) != want || want == 1 && notices[0] != line {
		t.Fatalf("C1b notices = %q, want %d copies of %q", notices, want, line)
	}
}
