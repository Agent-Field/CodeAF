//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/council"
	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

const decideLiveModel = "deepseek/deepseek-v4.1-flash"

// decideLiveDoor checks the response identity and native bill, rather than
// treating a configured model or an unknown cost as proof of a live call.
type decideLiveDoor struct {
	t        *testing.T
	key      string
	source   roles.Source
	lastCost float64
	spent    float64
	calls    map[roles.Role]int
}

func (d *decideLiveDoor) complete(ctx context.Context, role roles.Role, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	model, err := roles.Resolve(d.source, role, decideLiveModel)
	if err != nil {
		return nil, err
	}
	if model != decideLiveModel {
		return nil, fmt.Errorf("role %s resolved unexpected model %q", role, model)
	}
	if d.spent >= council.SpendCapUSD {
		return nil, fmt.Errorf("live run budget reached: $%.6f", d.spent)
	}
	client, err := provider.NewClient(provider.Config{
		APIKey: d.key, BaseURL: "https://openrouter.ai/api/v1", Model: model,
		Timeout: 90 * time.Second,
		// Every outgoing attempt is checked, including provider retry roads.
		RouteGate: func(_ context.Context, model, _ string) error {
			if model != decideLiveModel {
				return fmt.Errorf("unexpected wire model %q", model)
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	ctx = provider.WithCallTag(provider.WithoutStream(ctx), string(role))
	response, err := client.CompleteWithMessages(ctx, messages, options...)
	if err != nil {
		return nil, fmt.Errorf("role %s: %w", role, err)
	}
	if response == nil || response.Model != decideLiveModel {
		return nil, fmt.Errorf("role %s: unexpected response identity: %+v", role, responseIdentity(response))
	}
	if response.Usage == nil || response.Usage.Cost == nil {
		return nil, fmt.Errorf("role %s: provider did not report cost", role)
	}
	cost := *response.Usage.Cost
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		return nil, fmt.Errorf("role %s: invalid cost", role)
	}
	d.lastCost = cost
	d.spent += cost
	d.calls[role]++
	d.t.Logf("LIVE role=%s requested=%s actual=%s cost=$%.6f total=$%.6f", role, model, response.Model, cost, d.spent)
	if d.spent >= council.SpendCapUSD {
		return nil, fmt.Errorf("live run exceeded $%.2f: $%.6f", council.SpendCapUSD, d.spent)
	}
	return response, nil
}

func responseIdentity(r *ai.Response) string {
	if r == nil {
		return "missing response"
	}
	return r.Model
}

func liveMessages(system, user string) []ai.Message {
	return []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: system}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: user}}},
	}
}

// The memory decider uses the reflex role's production parser and prompts.
type decideLiveMemory struct{ door *decideLiveDoor }

func (m decideLiveMemory) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	return m.door.complete(ctx, roles.RoleReflex, messages, options...)
}

// The sink records actual runner output; it supplies no scripted model replies.
type decideLiveCouncilSink struct{ speakers []string }

func (s *decideLiveCouncilSink) Say(_, speaker, _ string) error {
	s.speakers = append(s.speakers, speaker)
	return nil
}

func (*decideLiveCouncilSink) Escalate(_ context.Context, e council.Escalation) error {
	return fmt.Errorf("council escalated instead of deciding: reason=%s turns=%d spend=$%.6f", e.Reason, e.Turns, e.Spend)
}

func TestDecideLive(t *testing.T) {
	key := liveKey(t)
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	logPath := filepath.Join(root, "calls.jsonl")
	t.Setenv(calllog.EnvVar, logPath)
	t.Setenv(calllog.BodiesEnvVar, "")
	// No settings are borrowed from the person's profile: every tier, role pin
	// and session floor resolves to the same requested live model.
	source := roles.Source(func(string) (string, bool) { return decideLiveModel, true })
	door := &decideLiveDoor{t: t, key: key, source: source, calls: map[roles.Role]int{}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Run("every_role", func(t *testing.T) {
		// This is routing proof for every registered seat, not an assertion
		// that a text model supports image, speech or video generation.
		for _, role := range roles.Registered() {
			if role == roles.RoleDeciding || role == roles.RoleReflex || role == roles.RoleCouncil {
				continue
			}
			if _, err := door.complete(ctx, role, liveMessages("Reply with the single word ready.", "Check this model connection.")); err != nil {
				t.Fatal(err)
			}
		}
	})
	if t.Failed() {
		return
	}

	t.Run("memory_decider", func(t *testing.T) {
		result, err := reflex.Decide(ctx, decideLiveMemory{door}, reflex.ExtractResult{
			Type: "preference", Scope: "user", Title: "Formatter", Text: "I prefer gofmt for Go files.",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Op != "add" {
			t.Fatalf("new memory with no neighbours: op=%q, want add", result.Op)
		}
		t.Logf("MEMORY model=%s op=%s", decideLiveModel, result.Op)
	})
	if t.Failed() {
		return
	}

	places, err := placegraph.Open(placegraph.Options{Path: filepath.Join(root, "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := places.CreatePlace(placegraph.NewPlace{Name: "Marketing"})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := places.CreatePlace(placegraph.NewPlace{Name: "Software"})
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := decide.Open(filepath.Join(root, "decisions"), first.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	place := session.Place{Dir: filepath.Join(root, "sessions", session.NewSessionID()), Workspace: workspace}
	agent, err := session.New(session.Config{
		APIKey: key, BaseURL: "https://openrouter.ai/api/v1", Model: decideLiveModel,
		RolesSource: source, Workspace: workspace, Place: place, SessionFile: place.Transcript(), AskConsent: true,
		PlaceGraph: &session.PlaceGraphDoor{Path: filepath.Join(root, "places.json")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if _, _, err := places.AddChat(place.ID(), first.ID, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	snapshot, err := places.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("learn_propose_decide", func(t *testing.T) {
		judge, err := decide.NewJudge(func(ctx context.Context, req decide.JudgeRequest) (decide.JudgeAnswer, error) {
			response, err := door.complete(ctx, req.Role, liveMessages(req.System, req.User))
			if err != nil {
				return decide.JudgeAnswer{}, err
			}
			return decide.JudgeAnswer{Text: response.Text(), CostUSD: door.lastCost}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		pick, err := judge.Judge(ctx, decide.JudgeQuestion{
			PlaceID: first.ID, PlaceName: first.Name, Mode: decide.ModeLearning,
			Prompt:  "May we run gofmt on the Go file? The person explicitly requested formatting it and prefers gofmt.",
			Stakes:  decide.StakesReversible,
			Options: []decide.Option{{Key: "1", Label: "Allow once"}, {Key: "3", Label: "Deny"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if pick.Key != "1" {
			t.Fatalf("judge did not propose the requested formatter: %+v", pick)
		}
		gate := &session.DecideGate{
			Graph: snapshot, ChatID: place.ID(), OpenStore: func(string) (*decide.Store, error) { return ledger, nil },
			SubjectClass: func(session.Question) string { return "shell-read" },
			Score: func(_ string, q session.Question) (decide.Result, string, error) {
				state, err := ledger.Mode(decide.KindKey(string(q.Ask), "shell-read"))
				if err != nil {
					return decide.Result{}, "", err
				}
				if state.Mode == decide.ModeLearning {
					return decide.Result{Percent: pick.Percent, Because: pick.Reason}, pick.Key, nil
				}
				var answers []decide.Answer
				for i, outcome := range state.Recent {
					choice := "3"
					if outcome.Agreed {
						choice = pick.Key
					}
					answers = append(answers, decide.Answer{ID: fmt.Sprint(i), Kind: string(q.Ask), Subject: "gofmt", Choice: choice})
				}
				return decide.Score(decide.Proposal{Kind: string(q.Ask), Subject: "gofmt", Choice: pick.Key, Said: "allowed", Stakes: string(q.Stakes), PlaceName: first.Name}, decide.Evidence{Answers: answers}), pick.Key, nil
			},
		}
		agent.SetDecideGate(gate)
		questions, unwatch := agent.WatchQuestions()
		defer unwatch()
		for i := 0; i < decide.RingSize; i++ {
			q := decideLivePermission(uint64(i + 1))
			withdraw, err := agent.AskQuestion(q)
			if err != nil {
				t.Fatal(err)
			}
			// Generic public askers publish on the question stream. OpenQuestions
			// enumerates lane-owned waiters, which this asker does not install.
			raised := nextDecideLiveQuestion(t, questions)
			if raised.ID != q.ID || raised.Proposal == nil || raised.Pick == nil || raised.Pick.Key != pick.Key {
				t.Fatalf("learning question %d lacks live proposal: %+v", i+1, raised)
			}
			chosen := pick.Key
			if i < decide.RingSize-decide.GraduateAt {
				chosen = "3"
			}
			if err := agent.ResolveQuestion(session.Answer{ID: q.ID, Kind: q.Kind, Ask: q.Ask, Key: chosen, Picked: []string{chosen}, DecidedBy: session.DecidedByPerson, At: time.Now()}); err != nil {
				t.Fatal(err)
			}
			withdraw()
			state, err := ledger.Mode(decide.KindKey(string(q.Ask), "shell-read"))
			if err != nil {
				t.Fatal(err)
			}
			if i < decide.RingSize-1 && state.Mode != decide.ModeLearning {
				t.Fatal("place graduated before twenty answers")
			}
		}
		state, err := ledger.Mode(decide.KindKey(string(session.AskPermission), "shell-read"))
		if err != nil || state.Mode != decide.ModeDeciding || decide.Agreements(state) != decide.GraduateAt {
			t.Fatalf("graduation: %+v err=%v", state, err)
		}
		t.Logf("GRADUATED agreements=%d/%d mode=%s", decide.Agreements(state), decide.RingSize, state.Mode)
		// Graduation earns the mode, not confidence. The smoothed 18/20 score
		// stays below the default threshold, so one more agreement is needed.
		q := decideLivePermission(decide.RingSize + 1)
		q.Pick = &session.Pick{Key: pick.Key, Percent: pick.Percent, Reason: pick.Reason}
		withdraw, err := agent.AskQuestion(q)
		if err != nil {
			t.Fatal(err)
		}
		raised := nextDecideLiveQuestion(t, questions)
		if raised.ID != q.ID || len(gate.Receipts()) != 0 {
			t.Fatal("graduation answered a below-threshold permission")
		}
		if err := agent.ResolveQuestion(session.Answer{ID: q.ID, Kind: q.Kind, Ask: q.Ask, Key: pick.Key, Picked: []string{pick.Key}, DecidedBy: session.DecidedByPerson, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
		withdraw()
		state, err = ledger.Mode(decide.KindKey(string(q.Ask), "shell-read"))
		if err != nil || decide.Agreements(state) != decide.GraduateAt+1 {
			t.Fatalf("extra agreement: %+v err=%v", state, err)
		}
		q = decideLivePermission(decide.RingSize + 2)
		withdraw, err = agent.AskQuestion(q)
		if err != nil {
			t.Fatal(err)
		}
		defer withdraw()
		if len(agent.OpenQuestions()) != 0 || len(gate.Receipts()) != 1 {
			t.Fatalf("graduated permission was not answered: open=%+v receipts=%+v", agent.OpenQuestions(), gate.Receipts())
		}
		rows, err := ledger.List()
		if err != nil || len(rows) != 1 || !rows[0].Reversible || rows[0].By != first.ID {
			t.Fatalf("decision ledger: %+v err=%v", rows, err)
		}
		t.Logf("PLACE agreements=%d/%d mode=%s receipt=%q", decide.Agreements(state), decide.RingSize, state.Mode, gate.Receipts()[0].Text)
	})
	if t.Failed() {
		return
	}

	t.Run("two_place_council", func(t *testing.T) {
		for _, p := range []placegraph.Place{first, second} {
			if _, _, err := places.AddLine(placegraph.Line{PlaceID: p.ID, Text: "The release date is Friday. We want to announce the release on Friday.", Source: placegraph.LineSource{Kind: placegraph.LineYouWrote}}); err != nil {
				t.Fatal(err)
			}
		}
		store, err := council.Open(council.Options{Path: filepath.Join(root, "councils.json"), SessionsDir: filepath.Join(root, "sessions"), Places: places})
		if err != nil {
			t.Fatal(err)
		}
		sink := &decideLiveCouncilSink{}
		runner, err := council.NewRunner(store, places, func(ctx context.Context, req council.Request) (council.Answer, error) {
			response, err := door.complete(ctx, req.Role, liveMessages(req.System, req.User))
			if err != nil {
				return council.Answer{}, err
			}
			return council.Answer{Text: response.Text(), CostUSD: door.lastCost}, nil
		}, sink)
		if err != nil {
			t.Fatal(err)
		}
		result, err := runner.Start(ctx, first.ID, second.ID, "On which day should we announce the release?")
		if err != nil {
			t.Fatal(err)
		}
		if result.State != council.StateDecided || result.Turns < 2 || result.Turns > council.TurnCap || result.Spend <= 0 || result.Spend >= council.SpendCapUSD {
			t.Fatalf("council did not decide under budget: %+v", result)
		}
		if len(sink.speakers) != result.Turns || sink.speakers[0] != first.Name || sink.speakers[1] != second.Name {
			t.Fatalf("both places did not speak: %+v", sink.speakers)
		}
		snapshot, err := places.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []placegraph.Place{first, second} {
			found := false
			for _, line := range snapshot.Knowledge(p.ID) {
				if line.Text == result.Outcome && line.Source.ChatID == result.ChatID && line.Source.Kind == placegraph.LineSaidInChat {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s lacks council outcome provenance", p.Name)
			}
		}
		t.Logf("COUNCIL model=%s turns=%d spend=$%.6f state=%s outcome=%q", decideLiveModel, result.Turns, result.Spend, result.State, result.Outcome)
	})
	if t.Failed() {
		return
	}
	for _, role := range roles.Registered() {
		if door.calls[role] == 0 {
			t.Errorf("role %s made no live call", role)
		}
	}
	assertDecideLiveLog(t, logPath)
}

func decideLivePermission(id uint64) session.Question {
	return session.Question{ID: id, Kind: session.QuestionConsent, Ask: session.AskPermission,
		Head: fmt.Sprintf("May it run gofmt on file-%d.go?", id), Reason: "the person requested formatting the file",
		Stakes: session.StakesReversible, Options: session.AnswerOptions(session.QuestionConsent)}
}

func nextDecideLiveQuestion(t *testing.T, events <-chan session.Event) session.Question {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("question stream closed")
			}
			if event.Kind == session.EventQuestion && event.Question != nil {
				return *event.Question
			}
		case <-deadline.C:
			t.Fatal("no raised permission on the question stream")
		}
	}
}

func assertDecideLiveLog(t *testing.T, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	completed := 0
	total := 0.0
	for scan.Scan() {
		var row calllog.Record
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Model != decideLiveModel {
			t.Fatalf("attempt log contains unexpected model %q", row.Model)
		}
		if row.Phase != calllog.PhaseStart {
			completed++
			total += row.Cost
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if completed == 0 || total <= 0 || total >= council.SpendCapUSD {
		t.Fatalf("attempt bill: completed=%d total=$%.6f", completed, total)
	}
	t.Logf("ATTEMPTS model=%s completed=%d spend=$%.6f", decideLiveModel, completed, total)
}
