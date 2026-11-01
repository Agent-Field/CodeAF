package remote

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestWriterInstallationWakesEveryWaitingSend(t *testing.T) {
	agent := &Agent{c: &Client{}}
	first, second := agent.WriterChanges(), agent.WriterChanges()
	// Existing facts are the only carrier; subscribing emits no request.
	if !agent.c.facts.takePush(FactsPush{Rev: 1, Facts: session.Facts{Renewal: &session.RenewalFact{Period: time.Second, Held: true}}}) {
		t.Fatal("facts refused")
	}
	for _, changed := range []<-chan struct{}{first, second} {
		select {
		case <-changed:
		default:
			t.Fatal("writer change did not broadcast")
		}
	}
	next := agent.WriterChanges()
	select {
	case <-next:
		t.Fatal("new subscription was already spent")
	default:
	}
	if _, period, held := agent.RenewalStatus(); period != time.Second || !held {
		t.Fatal("pushed writer authority did not arrive")
	}
	if agent.c.facts.takePush(FactsPush{Rev: 1}) {
		t.Fatal("old facts landed")
	}
	select {
	case <-next:
		t.Fatal("old facts woke a new send")
	default:
	}
}
