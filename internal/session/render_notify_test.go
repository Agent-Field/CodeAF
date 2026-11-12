package session

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

// refusingImageAgent is a session whose image route refuses every call before
// any provider request and whose video route admits one, so a refusal and a
// real render can race in one ledger.
func refusingImageAgent(t *testing.T, media MediaGenerator) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Media = media
		c.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model", modalityVideo: "film/model"})
		c.MediaAdmission = func(modality, _ string) MediaAdmission {
			if modality == modalityVideo {
				return MediaAdmission{Model: "film/model"}
			}
			return MediaAdmission{Reason: "none_advertised", Message: "No image model is advertised."}
		}
	})
	return agent
}

// A REFUSAL IS NEWS FROM THE FIRST RECORD. Before the notifier was installed at
// construction, a session that had not started a video or music render saved
// the refused record and told nobody: render.changed arrived only for the
// sessions that happened to have rendered before.
func TestARefusalInAFreshSessionIsNewsBeforeAnyRender(t *testing.T) {
	media := &scriptedMedia{}
	agent := refusingImageAgent(t, media)
	events, stop := agent.WatchTaskUpdates()
	defer stop()
	ctx := withCallID(withRunIdentity(context.Background()), "fresh_call")
	out, bad, err := beltTool(t, agent, "generate_image").Execute(ctx, json.RawMessage(`{"prompt":"a lighthouse"}`))
	if err != nil || !bad || len(media.seen) != 0 {
		t.Fatalf("refusal reached a provider or did not refuse: %q bad=%v err=%v seen=%d", out, bad, err, len(media.seen))
	}
	heard := renderEventsUntil(t, events, 2)
	if heard[0].State != RenderRunning || heard[1].State != RenderFailed || heard[1].Refusal != "none_advertised" ||
		heard[1].Charge != RenderNotCharged || heard[1].Call != "fresh_call" || heard[1].Run != runIdentity(ctx) || heard[0].ID != heard[1].ID {
		t.Fatalf("refusal events = %+v", heard)
	}
}

// The notifier is written once, before the ledger is published, so a refusal's
// event and a first video's start cannot race on it. Every ending emits just
// after the ledger's lock is let go; the former install-on-first-render wrote
// the field under that lock while those emits read it with none, which -race
// reports here.
func TestRefusalEventsAndAFirstRenderDoNotRaceOnTheNotifier(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), false)
	agent := refusingImageAgent(t, media)
	ledger := agent.jobs.renderLedger()
	if ledger.notify == nil {
		t.Fatal("the ledger was published before its notifier was installed")
	}
	var done atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !done.Load() {
			ledger.emit(RenderRecord{ID: "rnd_refused", Kind: "image", State: RenderFailed})
		}
	}()
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	done.Store(true)
	wg.Wait()
	<-media.arrived
	close(media.release)
	<-renderJob(t, agent, 1).done
}
