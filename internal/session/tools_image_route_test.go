package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// THE ROUTED PICTURE: Automatic falls through a model that does not answer and
// says so; a chosen model is never swapped, and the call comes back as a
// question; a model that cannot read a photo is refused before anything is paid.

// byModelPainter paints, except for the models it was told to fail.
type byModelPainter struct {
	*scriptedMedia
	mu    sync.Mutex
	fail  map[string]error
	asked []string
}

func (p *byModelPainter) GenerateImage(ctx context.Context, request provider.ImageRequest) (*provider.ImageResponse, error) {
	p.mu.Lock()
	p.asked = append(p.asked, request.Model)
	err := p.fail[request.Model]
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return p.scriptedMedia.GenerateImage(ctx, request)
}

func routedPainter(t *testing.T, fail map[string]error) *byModelPainter {
	t.Helper()
	return &byModelPainter{
		scriptedMedia: &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 4, 4)), mediaType: "image/png"},
		fail:          fail,
	}
}

var pictureNames = map[string]string{"seed/pro": "Seedream 5 Pro", "gem/image": "Gemini 3 Pro Image", "krea/turbo": "Krea 2 Turbo"}

func routedAgent(t *testing.T, painter *byModelPainter, route func(config.PictureAsk) config.PictureRoute) *Agent {
	t.Helper()
	agent, _ := newMediaAgent(t, painter.scriptedMedia, func(c *Config) {
		c.Media = painter
		c.MediaModel = nil
		c.PictureRoute = func(ask config.PictureAsk) config.PictureRoute {
			got := route(ask)
			got.Names = pictureNames
			return got
		}
	})
	return agent
}

func runRouted(t *testing.T, agent *Agent, args string) (string, bool) {
	t.Helper()
	ctx := withCallID(withRunIdentity(context.Background()), "routed_call")
	out, bad, err := beltTool(t, agent, "generate_image").Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return out, bad
}

func busy() error { return &provider.APIError{Status: 503, Message: "no capacity"} }

func TestAutomaticFallsThroughAModelThatIsBusyAndSaysSo(t *testing.T) {
	painter := routedPainter(t, map[string]error{"seed/pro": busy()})
	agent := routedAgent(t, painter, func(config.PictureAsk) config.PictureRoute {
		return config.PictureRoute{Model: "seed/pro", Scope: config.PictureScopeAutomatic, Then: []string{"gem/image", "krea/turbo"}}
	})
	out, bad := runRouted(t, agent, `{"prompt":"a lighthouse"}`)
	if bad {
		t.Fatalf("the fall-through failed: %s", out)
	}
	if strings.Join(painter.asked, ",") != "seed/pro,gem/image" {
		t.Errorf("asked %v, want the busy model then the next one only", painter.asked)
	}
	if !strings.Contains(out, "Made with Gemini 3 Pro Image · Automatic · Seedream 5 Pro was busy.") {
		t.Errorf("result does not say what made it and what was passed over: %s", out)
	}
	page, err := agent.Renders(10, "")
	if err != nil || len(page.Renders) != 2 {
		t.Fatalf("renders = %+v %v", page, err)
	}
	made := page.Renders[0]
	if made.State != RenderFinished || made.Model != "gem/image" || made.Picked != "automatic" ||
		len(made.PassedOver) != 1 || made.PassedOver[0] != (RenderPassedOver{Model: "seed/pro", Reason: "busy"}) {
		t.Errorf("the finished render = %+v", made)
	}
	if first := page.Renders[1]; first.State != RenderFailed || first.Model != "seed/pro" || first.Refusal != "" {
		t.Errorf("the passed-over render = %+v", first)
	}
}

func TestAChosenModelThatDoesNotAnswerIsNeverSwapped(t *testing.T) {
	painter := routedPainter(t, map[string]error{"seed/pro": busy()})
	agent := routedAgent(t, painter, func(config.PictureAsk) config.PictureRoute {
		return config.PictureRoute{Model: "seed/pro", Scope: config.PictureScopeCanvas, Alternative: "gem/image"}
	})
	out, bad := runRouted(t, agent, `{"prompt":"four lighthouses","n":4}`)
	if !bad {
		t.Fatalf("a chosen model that did not answer reported success: %s", out)
	}
	for _, asked := range painter.asked {
		if asked != "seed/pro" {
			t.Fatalf("another model was asked: %v", painter.asked)
		}
	}
	for _, want := range []string{"Seedream 5 Pro isn't answering (was busy)", "chosen for this canvas", "Gemini 3 Pro Image can make these 4 instead", "Ask the person before switching", `"gem/image"`} {
		if !strings.Contains(out, want) {
			t.Errorf("result does not say %q: %s", want, out)
		}
	}
	page, err := agent.Renders(10, "")
	if err != nil || len(page.Renders) != 1 {
		t.Fatalf("renders = %+v %v", page, err)
	}
	if got := page.Renders[0]; got.Refusal != "chosen_not_answering" || got.Picked != "chosen" || len(got.Options) != 1 || got.Options[0] != "gem/image" {
		t.Errorf("render = %+v", got)
	}
}

func TestAFailureAnotherModelCannotFixIsNotPassedOn(t *testing.T) {
	painter := routedPainter(t, map[string]error{"seed/pro": &provider.APIError{Status: 400, Message: "prompt refused"}})
	agent := routedAgent(t, painter, func(config.PictureAsk) config.PictureRoute {
		return config.PictureRoute{Model: "seed/pro", Scope: config.PictureScopeAutomatic, Then: []string{"gem/image"}}
	})
	out, bad := runRouted(t, agent, `{"prompt":"a lighthouse"}`)
	if !bad || strings.Join(painter.asked, ",") != "seed/pro" || strings.Contains(out, "Made with") {
		t.Fatalf("a refused prompt was sent to another model: asked %v, %s", painter.asked, out)
	}
}

func TestAutomaticThatRunsOutOfModelsSaysNoneAnswered(t *testing.T) {
	painter := routedPainter(t, map[string]error{"seed/pro": busy(), "gem/image": &provider.APIError{Status: 502}})
	agent := routedAgent(t, painter, func(config.PictureAsk) config.PictureRoute {
		return config.PictureRoute{Model: "seed/pro", Scope: config.PictureScopeAutomatic, Then: []string{"gem/image"}}
	})
	out, bad := runRouted(t, agent, `{"prompt":"a lighthouse"}`)
	if !bad || !strings.Contains(out, "Automatic tried 2 picture makers and none answered") {
		t.Fatalf("result = %s", out)
	}
	page, _ := agent.Renders(10, "")
	if got := page.Renders[0]; got.Refusal != "none_answered" || len(got.PassedOver) != 1 {
		t.Errorf("render = %+v", got)
	}
}

func TestChangingAPhotoAsksTheRouteForAReaderAndRefusesBeforePaying(t *testing.T) {
	painter := routedPainter(t, nil)
	var asked config.PictureAsk
	agent := routedAgent(t, painter, func(ask config.PictureAsk) config.PictureRoute {
		asked = ask
		return config.PictureRoute{Scope: config.PictureScopeEverywhere, Reason: config.PictureCannotReadPhotos,
			Message: "Krea 2 Turbo can't read a photo, and you're changing one.", Options: []string{"gem/image"}}
	})
	writeFile(t, filepath.Join(agent.config.Workspace, "photo.png"), string(pngOfSize(t, 4, 4)))
	out, bad := runRouted(t, agent, `{"prompt":"make it dusk","reference_paths":["photo.png"],"model":"krea"}`)
	if !bad || !strings.Contains(out, "can't read a photo") || len(painter.asked) != 0 {
		t.Fatalf("refusal = %s, asked %v", out, painter.asked)
	}
	if !asked.NeedsPhotos || asked.Word != "krea" || asked.Workspace != agent.config.Workspace {
		t.Errorf("the route was asked %+v", asked)
	}
	page, _ := agent.Renders(10, "")
	if got := page.Renders[0]; got.Refusal != "cannot_read_photos" || got.Charge != RenderNotCharged {
		t.Errorf("render = %+v", got)
	}
}

func TestAPictureMadeOnAChoiceSaysWhereItWasChosen(t *testing.T) {
	painter := routedPainter(t, nil)
	agent := routedAgent(t, painter, func(config.PictureAsk) config.PictureRoute {
		return config.PictureRoute{Model: "krea/turbo", Scope: config.PictureScopeEverywhere}
	})
	out, bad := runRouted(t, agent, `{"prompt":"a lighthouse"}`)
	if bad || !strings.Contains(out, "Made with Krea 2 Turbo · chosen for everywhere.") {
		t.Fatalf("result = %s", out)
	}
}

func TestATaskNodeAsksAboutTheConversationsFolder(t *testing.T) {
	var asked string
	route := pictureRouteFrom(func(ask config.PictureAsk) config.PictureRoute { asked = ask.Workspace; return config.PictureRoute{} }, "/canvas/work")
	route(config.PictureAsk{Workspace: "/tmp/worktree"})
	if asked != "/canvas/work" {
		t.Fatalf("asked about %q", asked)
	}
}
