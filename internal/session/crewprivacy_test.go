package session

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestPaymentFailureKeepsTaskOffFreeRoutesWithoutConsent(t *testing.T) {
	stub := newCrewStub(t, http.StatusPaymentRequired, map[string]int{"z-ai/glm-5.3-flash:free": http.StatusOK})
	agent, dir := crewStubAgent(t, stub)
	run, err := askWorker(t, agent, 1)
	var stopped crewStopped
	if !errors.As(err, &stopped) || !strings.Contains(stopped.action, "add credit on openrouter") {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	if got := stub.models(); len(got) != 1 || crewroute.IsFree(got[0]) {
		t.Fatalf("task leaked to automatic free route: %v", got)
	}
	if config.CrewFreeRoutesAt(dir) {
		t.Fatal("task changed free-route choice")
	}
	if run.crew.current().Stopped == "" {
		t.Fatal("stopped work did not retain its actionable state")
	}
}

func TestOptingOutBlocksSavedAutomaticFreeSeatButKeepsExplicitPin(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "explicit pin"}[pinned], func(t *testing.T) {
			stub := newCrewStub(t, http.StatusPaymentRequired, map[string]int{"z-ai/glm-5.3-flash:free": http.StatusOK})
			agent, _ := crewStubAgent(t, stub)
			pick := crewroute.Pick{Seat: crewroute.Worker, Model: "z-ai/glm-5.3-flash", Send: "z-ai/glm-5.3-flash:free", Provider: "openrouter", Kind: crewroute.Free, Pinned: pinned}
			crew := &taskCrew{decision: crewroute.Decision{Crew: []crewroute.Pick{pick}}, original: map[crewroute.Seat]string{crewroute.Worker: pick.Send}}
			run := &beltRun{row: 1, crew: crew}
			_, err := (crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "private fixture task")}, ai.WithModel(pick.Send))
			if pinned {
				if err != nil || len(stub.models()) != 1 {
					t.Fatalf("explicit named consent refused: %v %v", stub.models(), err)
				}
			} else {
				if err == nil || len(stub.models()) != 0 || !strings.Contains(err.Error(), "free routes are off") {
					t.Fatalf("saved automatic free seat sent: %v %v", stub.models(), err)
				}
			}
		})
	}
}

func TestSmallPositiveBalanceStillUsesPaidRouteWhenFreeRoutesAreOff(t *testing.T) {
	stub := newCrewStub(t, http.StatusOK, map[string]int{"z-ai/glm-5.3-flash:free": http.StatusOK})
	agent, dir := crewStubAgent(t, stub)
	if err := config.WriteCreditsReading(dir, config.APIKeyAt(dir), credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := askWorker(t, agent, 1); err != nil {
		t.Fatalf("low positive balance was refused prematurely: %v", err)
	}
	if got := stub.models(); len(got) != 1 || crewroute.IsFree(got[0]) {
		t.Fatalf("low balance changed paid model without consent: %v", got)
	}
}

func TestSavedFreeFallbackIsSkippedAfterPaymentRefusalWhileOff(t *testing.T) {
	stub := newCrewStub(t, http.StatusPaymentRequired, map[string]int{"z-ai/glm-5.3-flash:free": http.StatusOK})
	agent, _ := crewStubAgent(t, stub)
	crew, err := agent.routeTaskCrew(t.Context(), 1, "fix: parser crashes on empty input", "Traceback")
	if err != nil {
		t.Fatal(err)
	}
	crew.ladders = map[crewroute.Seat][]crewroute.Pick{}
	crew.ladders[crewroute.Worker] = []crewroute.Pick{{Seat: crewroute.Worker, Model: "z-ai/glm-5.3-flash", Send: "z-ai/glm-5.3-flash:free", Provider: "openrouter", Kind: crewroute.Free}}
	run := &beltRun{row: 1, crew: crew}
	worker := crew.current().Seat(crewroute.Worker).Send
	_, err = (crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "private fixture task")}, ai.WithModel(worker))
	if err == nil || !strings.Contains(err.Error(), "add credit") {
		t.Fatalf("no actionable payment stop: %v", err)
	}
	if got := stub.models(); len(got) != 1 || crewroute.IsFree(got[0]) {
		t.Fatalf("saved free fallback received task: %v", got)
	}
}

func TestSharedFreeModelKeepsEachSeatsOwnPinConsent(t *testing.T) {
	for _, seat := range []crewroute.Seat{crewroute.Worker, crewroute.Checker} {
		t.Run(string(seat), func(t *testing.T) {
			model := "z-ai/glm-5.3-flash:free"
			stub := newCrewStub(t, http.StatusPaymentRequired, map[string]int{model: http.StatusOK})
			agent, _ := crewStubAgent(t, stub)
			crew := &taskCrew{decision: crewroute.Decision{Crew: []crewroute.Pick{
				{Seat: crewroute.Worker, Model: model, Send: model, Provider: "openrouter", Kind: crewroute.Free, Pinned: true},
				{Seat: crewroute.Checker, Model: model, Send: model, Provider: "openrouter", Kind: crewroute.Free},
			}}, original: map[crewroute.Seat]string{crewroute.Worker: model, crewroute.Checker: model}}
			run := &beltRun{row: 1, crew: crew}
			_, err := SeatCompleter(seat, crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "private task")}, ai.WithModel(model))
			if seat == crewroute.Worker {
				if err != nil || len(stub.models()) != 1 {
					t.Fatalf("explicit worker pin refused by automatic checker: %v calls=%v", err, stub.models())
				}
			} else {
				if err == nil || len(stub.models()) != 0 {
					t.Fatalf("automatic checker borrowed worker's consent: %v calls=%v", err, stub.models())
				}
			}
		})
	}
}

// CrewFactory resolves a probe's low-tier model when it constructs BashWorker.
// Its cached key can outlive a settings edit, unlike v3's ordinary live helpers.
func TestAlreadyCreatedHelperHonorsOptOutUnlessItsCurrentRowExplicitlyNamesFreeModel(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "old automatic choice", true: "current explicit helper row"}[explicit], func(t *testing.T) {
			stub := newCrewStub(t, http.StatusOK, map[string]int{"thinkingmachines/inkling-small:free": http.StatusOK})
			agent, dir := crewStubAgent(t, stub)
			if err := config.SetCrewFreeRoutes(dir, true); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteCreditsReading(dir, config.APIKeyAt(dir), credits.Reading{Known: true, Low: true}); err != nil {
				t.Fatal(err)
			}
			frozen := config.TierModelAt(dir, config.ModelTierLow)
			if !crewroute.IsFree(frozen) {
				t.Fatal("fixture did not construct a free probe")
			}
			crew := &taskCrew{original: map[crewroute.Seat]string{crewroute.Worker: "z-ai/glm-5.3-flash"}}
			run := &beltRun{row: 1, crew: crew}
			if err := config.SetCrewFreeRoutes(dir, false); err != nil {
				t.Fatal(err)
			}
			if explicit {
				agent.config.RolesSource = func(key string) (string, bool) { return frozen, key == roles.TierKey(roles.TierLow) }
			}
			_, err := (crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "private probe task")}, ai.WithModel(frozen))
			if explicit {
				if err != nil || len(stub.models()) != 1 {
					t.Fatalf("explicit helper choice refused: err=%v calls=%v", err, stub.models())
				}
			} else {
				if err == nil || len(stub.models()) != 0 || !strings.Contains(err.Error(), "free routes are off") {
					t.Fatalf("stale helper sent task: err=%v calls=%v", err, stub.models())
				}
			}
		})
	}
}
