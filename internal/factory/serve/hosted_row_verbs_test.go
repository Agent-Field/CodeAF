package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// hostedRowVerbs reads the verbs a list row carries, by chat, for one person.
func hostedRowVerbs(t *testing.T, rg *hostedRig, who string) map[string][]string {
	t.Helper()
	var listed struct {
		Sessions []struct {
			ID    string
			Verbs []string
		}
		Shared []struct {
			ID    string
			Verbs []string
		}
	}
	rg.do(t, who, http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &listed)
	out := map[string][]string{}
	for _, row := range listed.Sessions {
		out[row.ID] = row.Verbs
	}
	for _, row := range listed.Shared {
		out[row.ID] = row.Verbs
	}
	return out
}

// narrowedPolicy is a policy that differs from the default where it matters: the edit role
// cannot steer, and the owner's own role cannot run, steer or manage.
func narrowedPolicy(p *acl.Policy) {
	without := func(role string, drop ...string) {
		for i := range p.Roles {
			if p.Roles[i].Name == role {
				p.Roles[i].Verbs = slices.DeleteFunc(p.Roles[i].Verbs, func(v string) bool { return slices.Contains(drop, v) })
			}
		}
	}
	without("edit", acl.VerbSteer)
	without("full", acl.VerbRun, acl.VerbSteer, acl.VerbManage)
}

// P2-2
// Contract 1: every row of the list, the caller's own and shared, carries the verbs the caller
// holds on that chat under the policy as it stands when the list is read.
func TestHostedListRowsCarryTheAskersVerbsFromTheLivePolicy(t *testing.T) {
	rg := newHostedRig(t)
	mine := rg.create(t, "abir")
	edited := rg.create(t, "santosh")
	commented := rg.create(t, "santosh")
	viewed := rg.create(t, "mia")
	hostedGrant(t, rg, "santosh", edited, "abir", "edit")
	hostedGrant(t, rg, "santosh", commented, "abir", "comment")
	hostedGrant(t, rg, "mia", viewed, "abir", "view")

	want := map[string][]string{
		mine:      acl.Verbs,
		edited:    {"view", "comment", "edit", "run", "steer", "answer"},
		commented: {"view", "comment"},
		viewed:    {"view"},
	}
	if got := hostedRowVerbs(t, rg, "abir"); !reflect.DeepEqual(got, want) {
		t.Fatalf("default policy rows = %v, want %v", got, want)
	}

	// The same server, with no restart, now answers from the policy an operator just saved.
	rg.setAccessPolicy(t, narrowedPolicy)
	want = map[string][]string{
		mine:      {"view", "comment", "edit", "answer", "share"},
		edited:    {"view", "comment", "edit", "run", "answer"},
		commented: {"view", "comment"},
		viewed:    {"view"},
	}
	if got := hostedRowVerbs(t, rg, "abir"); !reflect.DeepEqual(got, want) {
		t.Fatalf("narrowed policy rows = %v, want %v", got, want)
	}
}

// P2-2
// Contract 1: the answer that makes a chat carries its maker's verbs, so a window never has to
// guess them for the one chat it has just opened, and what it says is what the list says next.
func TestHostedCreateAnswerCarriesTheMakersVerbs(t *testing.T) {
	rg := newHostedRig(t)
	for _, tc := range []struct {
		name   string
		policy func(*acl.Policy)
		want   []string
	}{
		{"default policy", func(*acl.Policy) {}, acl.Verbs},
		{"owner role that cannot run, steer or manage", narrowedPolicy, []string{"view", "comment", "edit", "answer", "share"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rg.setAccessPolicy(t, tc.policy)
			var made struct {
				ID    string
				Verbs []string
			}
			rg.do(t, "abir", http.MethodPost, hostedSessionsPath, -1, nil, http.StatusCreated, &made)
			if !reflect.DeepEqual(made.Verbs, tc.want) {
				t.Fatalf("create answered verbs %v, want %v", made.Verbs, tc.want)
			}
			if listed := hostedRowVerbs(t, rg, "abir")[made.ID]; !reflect.DeepEqual(listed, tc.want) {
				t.Fatalf("list row for the new chat has %v, want %v", listed, tc.want)
			}
		})
	}
}

// P2-5
// Contract 2 and 4: under a narrowed policy, each verb a row names is a call the guest's wire
// forwards and each verb it leaves out is a call the wire refuses in place. The row is display
// only: the wire decides exactly as it did before.
func TestHostedRowVerbsMatchWhatTheWireEnforces(t *testing.T) {
	rg := newHostedRig(t)
	mine := rg.create(t, "abir")
	shared := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", shared, "abir", "edit")
	rg.setAccessPolicy(t, narrowedPolicy)
	rows := hostedRowVerbs(t, rg, "abir")

	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	refusal := map[string]string{acl.VerbRun: guestRunWords, acl.VerbSteer: guestSteerWords, acl.VerbAnswer: guestAnswerWords}
	// A call is the method each verb gates on the wire.
	calls := []struct{ method, verb string }{{"Submit", acl.VerbRun}, {"Steer", acl.VerbSteer}, {"ResolveQuestion", acl.VerbAnswer}}
	for name, chat := range map[string]string{"shared at edit": shared, "own, with an owner role that cannot run": mine} {
		client, host := guestConnect(t, srv, opened, rg.tokens["abir"], chat)
		for i, call := range calls {
			guestWrite(t, client, guestCall(i+1, call.method))
			if slices.Contains(rows[chat], call.verb) {
				if got := guestHostLine(t, host); got != guestCall(i+1, call.method) {
					t.Errorf("%s: the row names %s but the wire forwarded %q", name, call.verb, got)
				}
			} else {
				guestRefusal(t, client, i+1, refusal[call.verb])
			}
		}
	}
	// The narrowing is real: neither chat lets all three through.
	for _, chat := range []string{shared, mine} {
		if slices.Contains(rows[chat], acl.VerbSteer) {
			t.Errorf("row for %s still names steer: %v", chat, rows[chat])
		}
	}
}

// P2-2
// Contract 1: a bot's rows hold what its person holds, narrowed by its scope, the way its wire does.
// (A view bot can no longer START a chat, because starting one needs run; so the chat a view bot sees
// is one its person's other bot made, and the view bot's own rows are read from it.)
func TestHostedRowVerbsOfABotAreItsScope(t *testing.T) {
	rg := newHostedRig(t)
	editBot := rg.contractBot(t, "abir", "Edit bot", "edit")
	viewBot := rg.contractBot(t, "abir", "View bot", "view")
	var made struct {
		ID    string
		Verbs []string
	}
	rg.botDo(t, editBot.Token, http.MethodPost, hostedSessionsPath, -1, nil, http.StatusCreated, &made)
	// The chat is its person's, who holds every verb on it, so the edit bot's answer is its scope alone:
	// run and what rides with it, and never share or manage.
	if got := slices.Sorted(slices.Values(made.Verbs)); !reflect.DeepEqual(got, []string{"answer", "comment", "edit", "run", "steer", "view"}) {
		t.Fatalf("an edit bot's new chat answered verbs %v, want its scope's", made.Verbs)
	}
	shared := rg.create(t, "mia")
	hostedGrant(t, rg, "mia", shared, "abir", "edit")
	var listed struct {
		Sessions, Shared []struct {
			ID    string
			Verbs []string
		}
	}
	rg.botDo(t, viewBot.Token, http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != made.ID || len(listed.Shared) != 1 ||
		!reflect.DeepEqual(listed.Sessions[0].Verbs, []string{"view"}) || !reflect.DeepEqual(listed.Shared[0].Verbs, []string{"view"}) {
		t.Fatalf("a view bot's rows = %+v", listed)
	}
}

// Contract 4: the chats' host never decides a person's access, so a verbs field of its own on a row
// is replaced and every other field of the row is kept as it was.
func TestHostedWithVerbsReplacesTheHostsOwnField(t *testing.T) {
	row := []byte(`{"id":"hs_x","state":"running","verbs":["manage"],"extra":{"n":1}}`)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(hostedWithVerbs(row, []string{"view"}), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["verbs"]) != `["view"]` || string(got["id"]) != `"hs_x"` || string(got["state"]) != `"running"` || string(got["extra"]) != `{"n":1}` || len(got) != 4 {
		t.Fatalf("row = %v", got)
	}
	if out := hostedWithVerbs([]byte(`not a row`), []string{"view"}); string(out) != `not a row` {
		t.Fatalf("a body that is not a row was changed to %q", out)
	}
}
