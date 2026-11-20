package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// THE ROUTED PICTURE — generate_image when the surface wired a picture maker
// route ([Config.PictureRoute]).
//
// The route says which model draws and whether anybody chose it
// (internal/config's picturemaker.go). This file is what the belt does with
// that answer, and it keeps two promises a person can see:
//
//   - AUTOMATIC FALLS THROUGH, AND SAYS SO. A model nobody chose that does not
//     answer (busy, unreachable, or no longer offered) is passed over for the
//     next one on the same ladder, at most three in all, and the picture that
//     comes back says what made it and what was passed over: "Made with X ·
//     Automatic · Y was busy". The render record carries the same facts
//     (picked, passed_over), so the desk never reads them out of prose.
//   - A CHOICE IS NEVER SWAPPED SILENTLY. A model somebody chose that does not
//     answer ends the call with nothing made, and the result is a question
//     for the person — the render record's refusal is "chosen_not_answering"
//     and its one option is the model Automatic would have used — so the desk
//     can ask "Seedream 5 Pro isn't answering · use Gemini 3 Pro Image for
//     these 4?". The model is told to ask, never to retry on its own.
//
// Only "did not answer" falls through. A refused prompt, a bad reference, an
// expired key or an empty account would be refused by the next model too, or
// is the person's to fix, and spending on a second model would only make the
// same news slower and dearer.

// The render refusals this file adds to the admission ones.
const (
	// pictureChosenNotAnswering: the chosen model did not answer and nothing
	// was swapped; Options holds the model the person may switch to.
	pictureChosenNotAnswering = "chosen_not_answering"
	// pictureNoneAnswered: Automatic tried every model it may try.
	pictureNoneAnswered = "none_answered"
)

// The three ways a model may not answer, as the record spells them.
const (
	pictureBusy        = "busy"
	pictureUnreachable = "unreachable"
	pictureGone        = "gone"
)

// pictureRouting is one routed call's state, shared by the attempts of one
// Automatic ladder.
type pictureRouting struct {
	route config.PictureRoute
	count int
	// picked is "chosen" or "automatic"; passedOver grows with each model
	// that did not answer.
	picked     string
	passedOver []RenderPassedOver
	// last is why the model just tried did not answer, empty when it did or
	// when it failed for any other reason.
	last string
	// more says another Automatic model is still to be tried after this one.
	more bool
}

// generateRoutedPicture is generate_image on a route: resolve, refuse what
// cannot be made before anything is paid for, then walk the ladder.
func (a *Agent) generateRoutedPicture(ctx context.Context, client MediaGenerator, parsed GenerateImageArgs) (string, bool) {
	route := a.config.PictureRoute(config.PictureAsk{
		Word:        parsed.Model,
		Workspace:   a.config.Workspace,
		NeedsPhotos: len(parsed.ReferencePaths) > 0,
	})
	if route.Model == "" {
		text, failed, _ := a.refuseMediaAdmission(ctx, modalityImage, MediaAdmission{
			Reason: route.Reason, Message: route.Message, Options: route.Options,
		})
		return text, failed
	}
	// The call's word is already this route's model; the road must not
	// resolve it a second time.
	parsed.Model = ""
	routing := &pictureRouting{route: route, count: max(parsed.N, 1), picked: "chosen"}
	if !route.Chosen() {
		routing.picked = "automatic"
	}
	ladder := append([]string{route.Model}, route.Then...)
	var text string
	var failed bool
	for i, model := range ladder {
		routing.last, routing.more = "", !route.Chosen() && i+1 < len(ladder)
		gen := a.imageGen(ctx, client, model)
		gen.routing = routing
		text, failed = GenerateImage(ctx, gen, parsed)
		if !failed || routing.last == "" || !routing.more || ctx.Err() != nil {
			return text, failed
		}
		routing.passedOver = append(routing.passedOver, RenderPassedOver{Model: model, Reason: routing.last})
	}
	return text, failed
}

// notAnswering is the road's failure hook: it classifies why every request
// failed and, when the model did not answer, says what happens next. The
// note is appended to the result; refusal and options go on the record.
func (r *pictureRouting) notAnswering(model string, err error) (note, refusal string, options []string) {
	r.last = pictureNotAnswering(err)
	if r.last == "" || r.more {
		return "", "", nil
	}
	name := r.route.Name(model)
	if r.route.Chosen() {
		note = fmt.Sprintf("%s isn't answering (%s). Nothing was made, and no other picture maker was used, because %s was %s.",
			name, notAnsweringWords(r.last), name, chosenWhere(r.route.Scope))
		if alt := r.route.Alternative; alt != "" {
			note += fmt.Sprintf(" %s can make %s instead. Ask the person before switching; only if they say yes, call generate_image again with model %q.",
				r.route.Name(alt), theseWords(r.count), alt)
			return note, pictureChosenNotAnswering, []string{alt}
		}
		return note + " No other picture maker is connected.", pictureChosenNotAnswering, nil
	}
	tried := len(r.passedOver) + 1
	note = fmt.Sprintf("%s %s too: Automatic tried %d picture makers and none answered. Nothing was made; try again in a moment.",
		name, notAnsweringWords(r.last), tried)
	if tried == 1 {
		note = fmt.Sprintf("%s %s, and no other picture maker can make this. Nothing was made; try again in a moment.", name, notAnsweringWords(r.last))
	}
	return note, pictureNoneAnswered, nil
}

// madeNote is the closing line of a picture that was made: what made it, how
// it was picked, and what was passed over on the way.
func (r *pictureRouting) madeNote(model string) string {
	parts := []string{"Made with " + r.route.Name(model)}
	if r.route.Chosen() {
		parts = append(parts, chosenWhere(r.route.Scope))
	} else {
		parts = append(parts, "Automatic")
	}
	if len(r.passedOver) > 0 {
		var why []string
		for _, passed := range r.passedOver {
			why = append(why, r.route.Name(passed.Model)+" "+notAnsweringWords(passed.Reason))
		}
		parts = append(parts, strings.Join(why, ", "))
	}
	return strings.Join(parts, " · ") + "."
}

// pictureNotAnswering classifies a provider failure as one another model
// could repair: busy (429, 503, 529), unreachable (408, 500, 502, 504, the
// gateway's 52x) or gone (404: not offered right now). Everything else is
// empty. A connection that is down is NOT one of them: every model rides the
// same origin, and trying another would only wait out the same outage.
func pictureNotAnswering(err error) string {
	var failure *provider.APIError
	if !errors.As(err, &failure) {
		return ""
	}
	switch status := failure.Status; {
	case status == http.StatusTooManyRequests, status == http.StatusServiceUnavailable, status == 529:
		return pictureBusy
	case status == http.StatusNotFound:
		return pictureGone
	case status == http.StatusRequestTimeout, status == http.StatusInternalServerError,
		status == http.StatusBadGateway, status == http.StatusGatewayTimeout, status >= 520 && status <= 530:
		return pictureUnreachable
	}
	return ""
}

// notAnsweringWords is a reason in a person's words.
func notAnsweringWords(reason string) string {
	switch reason {
	case pictureBusy:
		return "was busy"
	case pictureGone:
		return "isn't offered right now"
	}
	return "didn't answer"
}

// chosenWhere is where a chosen model was chosen.
func chosenWhere(scope string) string {
	switch scope {
	case config.PictureScopeCanvas:
		return "chosen for this canvas"
	case config.PictureScopeEverywhere:
		return "chosen for everywhere"
	case config.PictureScopeCall:
		return "named for this picture"
	}
	return "set by the operator"
}

// theseWords is "this one" or "these 4".
func theseWords(n int) string {
	if n <= 1 {
		return "this one"
	}
	return fmt.Sprintf("these %d", n)
}

// pictureRouteFrom is a route that always asks about workspace, for a belt
// that works somewhere other than the folder the choice was made for.
func pictureRouteFrom(route func(config.PictureAsk) config.PictureRoute, workspace string) func(config.PictureAsk) config.PictureRoute {
	if route == nil {
		return nil
	}
	return func(ask config.PictureAsk) config.PictureRoute {
		ask.Workspace = workspace
		return route(ask)
	}
}
