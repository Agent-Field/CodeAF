package clientrun_test

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clientrun"
)

func TestRunnerStateExcludesAnotherLiveProcess(t *testing.T) {
	if path := os.Getenv("CODEAF_TEST_RUNNER_LOCK_HOME"); path != "" {
		_, err := clientrun.Open(clientrun.Config{Home: path})
		if err == nil || !strings.Contains(err.Error(), "already open") {
			t.Fatalf("second process opened runner state: %v", err)
		}
		return
	}
	g := newRig(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerStateExcludesAnotherLiveProcess$")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_RUNNER_LOCK_HOME="+g.state)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("exclusive runner state: %v\n%s", err, out)
	}
}

func TestRepeatDeviceClaimIsNotAFreshClaim(t *testing.T) {
	g := newRig(t)
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	path := "/v1/clients/" + g.client.dev.ID() + "/queue/1/claim"
	g.request(t, g.s, "POST", path, nil, nil, 204)
	g.request(t, g.s, "POST", path, nil, nil, 409)
}

func TestLostClaimResponseRecoversWithoutExecutingTwice(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err == nil && req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
			resp.Body.Close()
			return nil, errors.New("claim reply lost after relay committed")
		}
		return resp, err
	}), noopLog)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("lost reply was not reported")
	}
	if g.e.answers != 0 {
		t.Fatal("executed without confirmed ownership")
	}
	g.reopen(t, nil, noopLog)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("durable claim recovery: %v", err)
	}
	if g.e.answers != 1 {
		t.Fatalf("recovery executed %d times", g.e.answers)
	}
}
