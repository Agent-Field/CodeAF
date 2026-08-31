package provider

import (
	"context"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE ROLE ON THE CONTEXT, HELD TO ITS THREE PROMISES ─────────────────────
//
// roles.go carries a role from the caller to the funnel and derives the old
// routing intent from it. That is two lines of code and three promises, and
// every one of them is the kind that fails silently:
//
//   - a role that does not survive the trip is a call priced as a background
//     errand while a person waits for it, or the other way round;
//   - an intent that does not follow the role is a knob a dozen call sites still
//     set, disagreeing with the table about the same fact;
//   - a role in the table with no facts behind it is four routing numbers read
//     as zero, which is a free, instant, unimportant call — the most dangerous
//     set of defaults there is.
//
// None of the three shows up as a failure anywhere: the answer still arrives,
// on some lane, at some price.

// TestARoleSurvivesTheTripAndAnUnsaidOneIsUnknown is the first promise.
//
// The unset half matters as much as the set half. A CALL THAT NAMES NO ROLE IS
// LEGAL, and it has to read as [lane.RoleUnknown] — a hidden background errand —
// rather than as anything cheerier, because the whole build was made of such
// calls the day before the table existed and most of them still are.
func TestARoleSurvivesTheTripAndAnUnsaidOneIsUnknown(t *testing.T) {
	if role := RoleFrom(context.Background()); role != lanes.RoleUnknown {
		t.Errorf("a context that named no role reads as %q; it has to read as the unknown role, which is the conservative one", role)
	}
	for _, role := range lanes.Roles() {
		if role == lanes.RoleUnknown {
			// WithRole treats the empty role as "say nothing", so a context
			// stamped with it is indistinguishable from one that never was —
			// which is the correct behaviour and not a round trip to assert.
			continue
		}
		ctx := WithRole(context.Background(), role)
		if got := RoleFrom(ctx); got != role {
			t.Errorf("a context stamped %q reads back as %q", role, got)
		}
		// And it survives being wrapped, which is the only reason it is a
		// context value rather than an option: a role belongs to the errand, and
		// the errand goes through a completer wrapper, a retry, a relax rung and
		// a hedge arm without anybody re-stating it.
		wrapped, stop := context.WithCancel(WithoutStream(ctx))
		defer stop()
		if got := RoleFrom(wrapped); got != role {
			t.Errorf("role %q did not survive being wrapped; it read back as %q", role, got)
		}
	}
}

// TestTheRoleDecidesTheRoutingIntent is the second promise.
//
// [WithRoutingIntent] is not going away — it is what `provider.sort` is built
// from and what a dozen call sites still say — so the two knobs coexist, and
// the question this test settles is what happens when they disagree. THE ROLE
// WINS: it is the more specific claim, it names the errand rather than a
// two-valued reading of it, and it is the one the table can explain.
//
// The contradiction is not hypothetical. A call site that said
// `IntentInteractive` years ago and has since become a background errand is
// exactly the drift the table was written to end, and it is fixed by naming the
// role rather than by hunting down the older line.
func TestTheRoleDecidesTheRoutingIntent(t *testing.T) {
	for _, probe := range []struct {
		name string
		ctx  context.Context
		want RoutingIntent
	}{
		{
			name: "a hidden errand routes as background",
			ctx:  WithRole(context.Background(), lanes.RoleMemory),
			want: IntentBackground,
		},
		{
			name: "a turn somebody is reading routes as interactive",
			ctx:  WithRole(context.Background(), lanes.RoleTalk),
			want: IntentInteractive,
		},
		{
			name: "a talk role beats an intent that says nobody is waiting",
			ctx:  WithRole(WithRoutingIntent(context.Background(), IntentBackground), lanes.RoleTalk),
			want: IntentInteractive,
		},
		{
			name: "a hidden role beats an intent that claims somebody is",
			ctx:  WithRole(WithRoutingIntent(context.Background(), IntentInteractive), lanes.RoleAuxiliary),
			want: IntentBackground,
		},
		{
			name: "with no role said, the intent is still the answer",
			ctx:  WithRoutingIntent(context.Background(), IntentBackground),
			want: IntentBackground,
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if got := routingIntentFrom(probe.ctx); got != probe.want {
				t.Errorf("routed as %v, wanted %v", got, probe.want)
			}
		})
	}
}

// TestEveryRoleInTheTableHasFacts is the third promise.
//
// [lane.Role.Facts] answers for a role nobody registered by handing back the
// unknown role's row, so a name absent from the table behaves like a background
// errand rather than like a free one. That fallback makes a MISSING row
// invisible: a role listed by [lane.Roles] but not answered by the map would
// route silently as something else, and the only symptom would be a bill.
//
// So every listed role is asked, and the zero struct is the failure: a role
// whose verb is empty is one whose phase would draw nothing, whose horizon is
// zero calls to learn from, and whose stream nobody would watch.
func TestEveryRoleInTheTableHasFacts(t *testing.T) {
	if len(lanes.Roles()) == 0 {
		t.Fatal("the roles table is empty, so this law passed vacuously")
	}
	for _, role := range lanes.Roles() {
		facts := role.Facts()
		if facts == (lanes.RoleFacts{}) {
			t.Errorf("role %q has no facts behind it, so it would route as free, instant and unimportant", role)
			continue
		}
		if facts.Verb == "" {
			t.Errorf("role %q has no phase verb, so a person watching it would be shown nothing while it worked", role)
		}
		if facts.Horizon < 0 {
			t.Errorf("role %q expects %d further calls, which is not a number of calls", role, facts.Horizon)
		}
		if facts.QualityNeed < 0 || facts.QualityNeed > 1 {
			t.Errorf("role %q needs %v of its answers usable, which is not a share", role, facts.QualityNeed)
		}
	}
	// And the unknown role really is the conservative one, since every reading
	// above falls back to it: nobody is waiting, nobody is watching, and the
	// answer still has to be usable most of the time.
	unknown := lanes.RoleUnknown.Facts()
	if unknown.Interactive || unknown.Visible {
		t.Errorf("the unknown role reads as interactive=%v visible=%v; a call that named nothing must not claim a person is waiting on it",
			unknown.Interactive, unknown.Visible)
	}
}

// TestEveryRoleGetsMeasuredAndNarrated is the fourth structural law, run rather
// than read: a completion made under each role really does produce a sighting
// for the ledger and a phase story for the surface.
//
// IT IS ONE TABLE AND NOT ELEVEN TESTS because the claim is about the FUNNEL:
// every role goes through the same function, so a role that measured nothing
// would mean a call site had found a way around it. Media is the one exclusion
// and it is stated rather than skipped — it produces no token stream at all, so
// there is no first token to time and no gap to judge, which is what
// [lane.RoleFacts.Streams] says about it.
func TestEveryRoleGetsMeasuredAndNarrated(t *testing.T) {
	for _, role := range lanes.Roles() {
		if role == lanes.RoleUnknown || !role.Facts().Streams {
			continue
		}
		t.Run(string(role), func(t *testing.T) {
			told := listen(t)
			rig := newLaneRig(t, "roles/"+string(role),
				lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 12}},
				lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 12}},
			)
			rig.believes("A", 2, 2000)

			ctx := WithRole(context.Background(), role)
			ctx = WithLaneChoice(ctx, choiceFor(rig.model, 500*time.Millisecond))
			if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
				t.Fatal(err)
			}
			if len(told.story()) == 0 {
				t.Fatalf("a completion under %q told the surface nothing at all", role)
			}
			for _, news := range told.all() {
				if news.Role != role {
					t.Fatalf("a phase carried role %q, want %q — a role lost on the way is a status line drawing somebody else's errand", news.Role, role)
				}
			}
			// The sighting is the ledger's half. It is written by the ordinary
			// finished-stream path, which is the path every role shares.
			waitFor(t, func() bool {
				_, ok := rig.ledger.sightingFor("A")
				return ok
			})
		})
	}
}
