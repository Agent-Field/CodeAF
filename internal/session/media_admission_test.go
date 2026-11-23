package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConnectedMediaRefusalIsAnActualUnpaidAttemptNotAProviderCall(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		t.Run(kind, func(t *testing.T) {
			media := &scriptedMedia{}
			agent, _ := newMediaAgent(t, media, func(c *Config) {
				c.MediaModel = nil
				c.MediaAdmission = func(modality, word string) MediaAdmission {
					return MediaAdmission{Reason: "model_required", Message: "Choose a model before generating.", Options: []string{"a/model", "b/model"}}
				}
			})
			ctx := withCallID(withRunIdentity(context.Background()), "actual_call")
			out, bad, err := beltTool(t, agent, "generate_"+kind).Execute(ctx, json.RawMessage(`{"prompt":"a genuine request"}`))
			if err != nil || !bad || !strings.Contains(out, "Choose a model") || len(media.seen) != 0 {
				t.Fatalf("refusal started a provider: bad%v err%v seen%v out%s", bad, err, media.seen, out)
			}
			page, err := agent.Renders(10, "")
			if err != nil || len(page.Renders) != 1 {
				t.Fatalf("missing actual refusal: %+v %v", page, err)
			}
			got := page.Renders[0]
			if got.Kind != kind || got.State != RenderFailed || got.Refusal != "model_required" || got.Charge != RenderNotCharged || got.Committed || got.Model != "" || got.Call != "actual_call" || got.Run != runIdentity(ctx) || len(got.Options) != 2 {
				t.Fatalf("invented or lost attempt facts: %+v", got)
			}
			got.Options[0] = "tampered"
			again, err := agent.Render(got.ID)
			if err != nil || again.Options[0] != "a/model" {
				t.Fatalf("caller changed history: %+v %v", again, err)
			}
		})
	}
}

func TestMediaAdmissionDoesNotOfferAClientItDoesNotHave(t *testing.T) {
	agent, _ := newMediaAgent(t, nil, func(c *Config) {
		c.Media = nil
		c.MediaModel = nil
		c.MediaAdmission = func(string, string) MediaAdmission { return MediaAdmission{Reason: "model_required"} }
	})
	for _, kind := range []string{"image", "video"} {
		if _, _, ready := agent.mediaHand(kind); ready {
			t.Fatalf("%s promised a missing client", kind)
		}
	}
}

func TestAdmissionRefusalReopensWithBoundedChoicesAndNoGuessedModel(t *testing.T) {
	path := t.TempDir() + "/renders.jsonl"
	ledger := openRenderLedger(path, "actual_session", time.Now())
	id, err := ledger.beginChecked(0, "image", "", "run", "call", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	options := []string{"a/model", "b/model", strings.Repeat("x", 1025), "d", "e", "f", "g", "h", "discarded"}
	ledger.settle(id, renderPatch{state: RenderFailed, refusal: "model_required", options: options, err: "Choose a model."}, time.Now())
	ledger.release(id)
	ledger.close()
	reopened := openRenderLedger(path, "actual_session", time.Now())
	defer reopened.close()
	rec, err := reopened.get(id)
	if err != nil || rec.Refusal != "model_required" || rec.Model != "" || len(rec.Options) != 7 || rec.Options[6] != "h" {
		t.Fatalf("bad persisted refusal: %+v %v", rec, err)
	}
}

func TestMediaAdmissionUsesTheInvocationRouteNotTheOldBeltDefault(t *testing.T) {
	media := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))}
	chosen := "actual/first"
	agent, _ := newMediaAgent(t, media, func(c *Config) {
		c.MediaModel = mediaModels(map[string]string{"image": "stale/default"})
		c.MediaAdmission = func(string, string) MediaAdmission { return MediaAdmission{Model: chosen} }
	})
	tool := beltTool(t, agent, "generate_image")
	for _, model := range []string{"actual/first", "actual/second"} {
		chosen = model
		out, bad, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"real route"}`))
		if err != nil || bad || media.seen[len(media.seen)-1].Model != model {
			t.Fatalf("ignored current route: bad%v err%v seen%+v out%s", bad, err, media.seen, out)
		}
	}
}
