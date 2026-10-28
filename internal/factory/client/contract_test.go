package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/factorytest"
)

type contractVerb struct {
	name string
	call func(factory.Seam) (any, error)
}

// contractVerbs spells both intents of Hold and both gates of approve. The
// coverage assertion below binds the cases to the API's one verb table.
func contractVerbs() []contractVerb {
	ctx := context.Background()
	id := func(door func(int) error, n int) (any, error) { return nil, door(n) }
	return []contractVerb{
		{"queue", func(s factory.Seam) (any, error) { return nil, loadContract(s) }},
		{"job", func(s factory.Seam) (any, error) { return nil, loadContract(s) }},
		{"repos", func(s factory.Seam) (any, error) {
			watched, available, err := s.Repos(ctx)
			return []any{watched, available}, err
		}},
		{"github", func(s factory.Seam) (any, error) { return s.GitHub(ctx) }},
		{"run", func(s factory.Seam) (any, error) { return id(s.Launch, 8) }},
		{"stop", func(s factory.Seam) (any, error) { return id(s.Stop, 2) }},
		{"pause", func(s factory.Seam) (any, error) { return nil, s.Hold(2, true) }},
		{"resume", func(s factory.Seam) (any, error) { return nil, s.Hold(2, false) }},
		{"drop", func(s factory.Seam) (any, error) { return id(s.Dismiss, 8) }},
		{"answer", func(s factory.Seam) (any, error) { return nil, s.Answer(1, false, "revise the plan") }},
		{"steer", func(s factory.Seam) (any, error) { return nil, s.Steer(2, "keep the tests") }},
		{"approve", func(s factory.Seam) (any, error) { return s.SignOff(9, true) }},
		{"approve_plan", func(s factory.Seam) (any, error) { return nil, s.Answer(1, true, "") }},
		{"request_changes", func(s factory.Seam) (any, error) { return nil, s.SendBack(9, "prove restart survival") }},
		{"recheck", func(s factory.Seam) (any, error) { return id(s.Reverify, 9) }},
		{"prioritize", func(s factory.Seam) (any, error) { return nil, s.Mark([]int{7, 8}, true) }},
		{"reply", func(s factory.Seam) (any, error) { return nil, s.Say(ctx, 2, "keep the tests") }},
		{"new_job", func(s factory.Seam) (any, error) { return s.New("owner/repo", "fix tests\n\nkeep the fixture") }},
		{"set_repos", func(s factory.Seam) (any, error) { return nil, s.SetRepos([]string{"owner/repo"}) }},
		{"connect_github", func(s factory.Seam) (any, error) { return nil, s.ConnectGitHub(ctx, "test-token") }},
	}
}

func loadContract(s factory.Seam) error {
	_, err := s.Load()
	return err
}

// The still fixture has no mutation doors. Recording taps make successful
// routing observable in its Snapshot without inventing another runner: a
// wrong door, argument, or missing call changes the notes and fails equality.
func recordingFixture(t *testing.T, refusal error) factory.Seam {
	t.Helper()
	s := factory.FixtureSeam(floorNow)
	snap, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	s.Load = func() (factory.Snapshot, error) {
		mu.Lock()
		defer mu.Unlock()
		return snap, nil
	}
	s.Repos = func(context.Context) ([]string, []factory.RepoInfo, error) {
		return []string{"owner/repo"}, []factory.RepoInfo{{Full: "owner/repo", Owner: "owner", Name: "repo", Pushed: floorNow, Dir: "/fixture/repo"}}, nil
	}
	s.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "fixture", Via: factory.ViaToken}, nil
	}
	v := reflect.ValueOf(&s).Elem()
	for _, verb := range api.Verbs {
		for _, name := range verb.Seam {
			field := v.FieldByName(name)
			if !field.IsNil() {
				continue
			}
			field.Set(recordingDoor(name, field.Type(), &mu, &snap, refusal))
		}
	}
	return s
}

// recordingDoor captures exactly what crossed the boundary. Context identity
// stays local to each HTTP request, so only the person's arguments are kept.
func recordingDoor(name string, typ reflect.Type, mu *sync.Mutex, snap *factory.Snapshot, refusal error) reflect.Value {
	return reflect.MakeFunc(typ, func(args []reflect.Value) []reflect.Value {
		out := make([]reflect.Value, typ.NumOut())
		for i := range out {
			out[i] = reflect.Zero(typ.Out(i))
		}
		if refusal != nil {
			out[len(out)-1] = reflect.ValueOf(refusal)
			return out
		}
		mu.Lock()
		defer mu.Unlock()
		recordContractIntent(snap, name, args)
		if name == "New" {
			out[0] = reflect.ValueOf(11)
		}
		return out
	})
}

func recordContractIntent(snap *factory.Snapshot, name string, args []reflect.Value) {
	// Mark batches become one request per job; both routes keep each intent.
	if name == "Mark" {
		for _, id := range args[0].Interface().([]int) {
			appendContractNote(snap, []any{name, []int{id}, args[1].Bool()})
		}
		return
	}
	words := []any{name}
	for _, arg := range args {
		if _, ctx := arg.Interface().(context.Context); !ctx {
			words = append(words, arg.Interface())
		}
	}
	appendContractNote(snap, words)
}

func appendContractNote(s *factory.Snapshot, words []any) {
	raw, _ := json.Marshal(words)
	s.Items[0].Notes = append(s.Items[0].Notes, string(raw))
}

func TestContractFixtureEqualsClientServeFixture(t *testing.T) {
	for _, refused := range []bool{false, true} {
		var refusal error
		name := "accepted"
		if refused {
			name, refusal = "refused", errors.New("the fixture refuses this intent")
		}
		t.Run(name, func(t *testing.T) {
			for _, verb := range contractVerbs() {
				t.Run(verb.name, func(t *testing.T) {
					direct := recordingFixture(t, refusal)
					remote := factorytest.ServeInProcess(t, recordingFixture(t, refusal))
					want, directErr := verb.call(direct)
					got, remoteErr := callOverWire(verb, remote)
					if errorWords(directErr) != errorWords(remoteErr) {
						t.Fatalf("direct error %v; client error %v", directErr, remoteErr)
					}
					wantJSON, _ := json.Marshal(want)
					gotJSON, _ := json.Marshal(got)
					if string(wantJSON) != string(gotJSON) {
						t.Fatalf("direct result %s; client result %s", wantJSON, gotJSON)
					}
					wantSnap, err := direct.Load()
					if err != nil {
						t.Fatal(err)
					}
					gotSnap, err := remote.Load()
					if err != nil {
						t.Fatal(err)
					}
					sameFloor(t, gotSnap, throughWire(t, wantSnap))
				})
			}
		})
	}
}

func callOverWire(verb contractVerb, s factory.Seam) (any, error) {
	if verb.name == "approve_plan" {
		_, err := s.SignOff(1, false)
		return nil, err
	}
	return verb.call(s)
}

func errorWords(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Presence is additive: contract is 2, and the fields already on
// capacity are still there beside the new counts.
func TestPresenceContractStaysAdditive(t *testing.T) {
	if api.Contract != 2 {
		t.Fatalf("contract bumped to %d; machine presence is additive", api.Contract)
	}
	tags := func(v any) map[string]bool {
		typ := reflect.TypeOf(v)
		out := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			if name != "" && name != "-" {
				out[name] = true
			}
		}
		return out
	}
	for _, name := range []string{"name", "os", "codeaf_version", "status", "job", "last_heartbeat"} {
		if !tags(api.Machine{})[name] {
			t.Errorf("machine missing %s", name)
		}
	}
	for _, name := range []string{"machine", "handle", "heartbeat_at", "since"} {
		if !tags(api.HeldBy{})[name] {
			t.Errorf("held_by missing %s", name)
		}
	}
	got := tags(api.Capacity{})
	for _, name := range []string{"benches", "daily", "rail", "queued", "workers", "machines_online", "busy"} {
		if !got[name] {
			t.Errorf("capacity missing %s", name)
		}
	}
}

func TestContractCoversEverySeamVerb(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range contractVerbs() {
		covered[tc.name] = true
	}
	for _, verb := range api.Verbs {
		if len(verb.Seam) == 0 {
			// Take and release are owned by the lease desk and have no Seam
			// equivalent; the serve tests cover them against the real store.
			// Merge is the factory's gate and the lease's signal, covered the
			// same way (internal/factory/serve/merge_test.go). Close and reopen
			// keep a job's work locally and are the floor's own
			// (internal/factory/serve/close_test.go).
			if !strings.Contains(" take release retry_publish merge model start_now run_again restore dismiss assign_to_me change_assignee unassign nudge priority raise_budget undo close reopen ", " "+verb.Name+" ") {
				t.Errorf("new server-owned verb %q needs a contract", verb.Name)
			}
			continue
		}
		if !covered[verb.Name] {
			t.Errorf("%s has no direct/client contract case", verb.Name)
		}
	}
}
