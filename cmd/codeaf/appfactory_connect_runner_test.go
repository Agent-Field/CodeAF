package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// whoGitHubSays is a floor link that names the person only while the app holds
// a GitHub key, as the real one does, and counts how often it was asked.
func whoGitHubSays(asks *atomic.Int32) func(factory.Seam) factory.Seam {
	return func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
			defer asks.Add(1)
			if key, _ := config.AppCredentialSource().Key("github"); key == "" {
				return factory.GitHubLink{}, nil
			}
			return factory.GitHubLink{Login: "owner"}, nil
		}
		return seam
	}
}

// waitForAsk reads status until GitHub has been asked at least once, so the
// factory has heard "nobody" and is waiting before it asks again.
func waitForAsk(t *testing.T, s *localSetup, asks *atomic.Int32) {
	t.Helper()
	eventually(t, "the factory asked GitHub who the person is", func() bool {
		var st appwire.FactoryLocalStatus
		s.must("factory.local_status", map[string]any{"caller": "w1"}, &st)
		return asks.Load() > 0
	})
	settle()
}

// A PERSON WHO HAS JUST CONNECTED GITHUB IS KNOWN AT ONCE. The factory asks who
// the person is before setup, hears nobody, and waits longer between asks. A
// connection made in setup ends that wait: the runner comes on within a read or
// two, not a couple of minutes later, and the header never says "Not running
// jobs" to someone who has just connected.
func TestLocalFactoryKnowsThePersonAsSoonAsSetupConnectsGitHub(t *testing.T) {
	s := newLocalSetup(t)
	var asks atomic.Int32
	s.backend.local.seamHook = whoGitHubSays(&asks)
	if st := s.start(); st.Runner {
		t.Fatalf("the runner was on before anyone connected GitHub: %+v", st)
	}
	waitForAsk(t, s, &asks)

	if v := s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret}); !v.GitHub.Connected {
		t.Fatalf("connect: %+v", v)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var st appwire.FactoryLocalStatus
		s.must("factory.local_status", map[string]any{"caller": "w1"}, &st)
		if st.Runner {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("5s after connecting, the factory still does not know the person: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A KEY THE APP HANDS OVER LATER ENDS THE WAIT THE SAME WAY: keys.set at launch
// can arrive after the first ask. Another service's key does not disturb it.
func TestLocalFactoryAsksAgainOnlyWhenTheGitHubKeyChanges(t *testing.T) {
	s := newLocalSetup(t)
	var asks atomic.Int32
	s.backend.local.seamHook = whoGitHubSays(&asks)
	s.start()
	waitForAsk(t, s, &asks)

	config.AppCredentialSource().Set("openrouter", "not-github")
	before := asks.Load()
	var st appwire.FactoryLocalStatus
	s.must("factory.local_status", map[string]any{"caller": "w1"}, &st)
	settle()
	if got := asks.Load(); got != before || st.Runner {
		t.Fatalf("another service's key asked GitHub again: asks %d then %d, %+v", before, got, st)
	}

	config.AppCredentialSource().Set("github", setupSecret)
	eventually(t, "the runner came on after the GitHub key arrived", func() bool {
		var now appwire.FactoryLocalStatus
		s.must("factory.local_status", map[string]any{"caller": "w1"}, &now)
		return now.Runner
	})
}
