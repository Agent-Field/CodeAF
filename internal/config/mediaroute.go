package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/catalog"
)

// THE DEFAULT ROUTE FOR DRAWING AND FILMING — chosen, or refused out loud.
//
// A person who has named a model gets that model, whatever the catalog says
// (Config.ImageModel and VideoModel are their decision and nothing here
// substitutes for it). A person who has named none gets a DEFAULT, and a default
// is spending someone else's money on their behalf: it is therefore only ever a
// model this build already remembers (the curated constants below, which this
// file never reorders or replaces), and only when the catalog both advertises it
// for the job and has published a price for it in a unit a person can be told.
// It is never the first row, the newest row, the cheapest-looking row, or a row
// that merely appeared in a listing: the dedicated image and video listings put
// dozens of new rows in the catalog, and "whichever is first" is a different
// bill every time the provider reorders.
//
// WHAT COMES BACK WHEN THERE IS NO DEFAULT is a [MediaRoute] that says which of
// three different things is true, because they need three different sentences:
//
//   - RouteModelRequired: models ARE advertised, but none this build may pick
//     for you. The person is told to choose one and is shown what is available.
//   - RouteNoneAdvertised: the listing was read and is current, and nothing in
//     it makes this. Only this reason may say that no connected service offers it.
//   - RouteUnconfirmed: the listing was never read, failed, or is out of date.
//     A transient failure is not evidence of absence, so nothing is claimed.
//
// Music and speech are deliberately NOT routed through here: the dedicated
// listings add no rows to either, so their defaults are unchanged.

// RouteReason says why a media route has no model, or how it got one.
type RouteReason string

const (
	// RouteCurated: the model is this build's remembered default, advertised
	// for the job and carrying a published price.
	RouteCurated RouteReason = "curated"
	// RouteConfigured: the person's own row. No catalog fact can overrule it.
	RouteConfigured RouteReason = "configured"
	// RouteModelRequired: choose a model; see MediaRoute.Options.
	RouteModelRequired RouteReason = "model_required"
	// RouteNoneAdvertised: the current listing offers nothing for this job.
	RouteNoneAdvertised RouteReason = "none_advertised"
	// RouteUnconfirmed: the listing could not be confirmed, so nothing is claimed.
	RouteUnconfirmed RouteReason = "unconfirmed"
)

// Sentinels for errors.Is. A *MediaRouteError matches the one for its reason.
var (
	ErrModelRequired  = errors.New("a model must be chosen")
	ErrNoneAdvertised = errors.New("no service advertises this")
	ErrUnconfirmed    = errors.New("the model list could not be confirmed")
)

// maxRouteOptions bounds how many available models an error names.
const maxRouteOptions = 8

// MediaRoute is the answer to "which model should draw or film for someone who
// chose none".
type MediaRoute struct {
	Modality string
	Model    string
	Reason   RouteReason
	// Options are the advertised models for the job, sorted by id and bounded
	// by maxRouteOptions; Total counts them all. Sorted by name on purpose:
	// an order that carried a judgement (cheapest, newest) would be a default.
	Options []string
	Total   int
}

// Err is nil when the route has a model and a *MediaRouteError otherwise.
func (r MediaRoute) Err() error {
	if r.Model != "" {
		return nil
	}
	return &MediaRouteError{Route: r}
}

// MediaRouteError is the one error a tool or a setup screen shows when there is
// no default. Its text is a sentence for a person; nothing retries on it.
type MediaRouteError struct{ Route MediaRoute }

func (e *MediaRouteError) Error() string {
	noun := e.Route.Modality
	article := "a"
	if noun == "image" {
		article = "an"
	}
	switch e.Route.Reason {
	case RouteNoneAdvertised:
		return fmt.Sprintf("no connected service advertises %s generation", noun)
	case RouteUnconfirmed:
		return fmt.Sprintf("the %s model list could not be confirmed (it has not been read, or it is out of date), so no default was chosen. Choose %s %s model, or try again later%s",
			noun, article, noun, e.available())
	default:
		return fmt.Sprintf("there is no default %s model with a published price. Choose %s %s model in settings or name one in the request%s",
			noun, article, noun, e.available())
	}
}

func (e *MediaRouteError) available() string {
	if len(e.Route.Options) == 0 {
		return ""
	}
	text := ". Advertised: " + strings.Join(e.Route.Options, ", ")
	if more := e.Route.Total - len(e.Route.Options); more > 0 {
		text += fmt.Sprintf(" (and %d more)", more)
	}
	return text
}

// Is lets errors.Is tell the three reasons apart.
func (e *MediaRouteError) Is(target error) bool {
	switch e.Route.Reason {
	case RouteModelRequired:
		return target == ErrModelRequired
	case RouteNoneAdvertised:
		return target == ErrNoneAdvertised
	case RouteUnconfirmed:
		return target == ErrUnconfirmed
	}
	return false
}

// curatedRoutes are the remembered defaults, in the order they are tried.
var curatedRoutes = map[string][]string{
	"image": {preferredImageModel, fallbackImageModel},
	"video": {preferredVideoModel, fallbackVideoModel},
}

// DefaultMediaRoute is the default for "image" or "video" with no configured
// choice in play. Any other modality has no route here and answers an empty
// RouteModelRequired.
func DefaultMediaRoute(models *catalog.Catalog, modality string) MediaRoute {
	modality = strings.ToLower(strings.TrimSpace(modality))
	route := MediaRoute{Modality: modality, Reason: RouteModelRequired}
	curated, routed := curatedRoutes[modality]
	if models == nil || !routed {
		route.Reason = RouteUnconfirmed
		return route
	}
	candidates := ModelCandidates(models, modality)
	route.Options, route.Total = routeOptions(candidates)

	// A catalog that reads the dedicated listings must have read THIS one, and
	// recently, before it may choose for anyone or claim there is nothing. One
	// that does not (another service) has only its general listing, which is
	// current only if it was ever read.
	state := models.MediaState()
	switch {
	case state.Applicable && !state.Authoritative(modality):
		route.Reason = RouteUnconfirmed
		return route
	case !state.Applicable && models.FetchedAt().IsZero():
		route.Reason = RouteUnconfirmed
		return route
	}
	if len(candidates) == 0 {
		route.Reason = RouteNoneAdvertised
		return route
	}
	for _, preferred := range curated {
		for _, candidate := range candidates {
			if candidate.ID == preferred && candidate.PublishedPrice(modality) {
				route.Model, route.Reason = candidate.ID, RouteCurated
				return route
			}
		}
	}
	return route
}

func routeOptions(candidates []catalog.Model) ([]string, int) {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	sort.Strings(ids)
	total := len(ids)
	if len(ids) > maxRouteOptions {
		ids = ids[:maxRouteOptions]
	}
	return ids, total
}

// ResolveImageRoute is [Config.ResolveImageModel] with the reason attached.
func (c Config) ResolveImageRoute(models *catalog.Catalog) MediaRoute {
	if configured := strings.TrimSpace(c.ImageModel); configured != "" {
		return MediaRoute{Modality: "image", Model: configured, Reason: RouteConfigured}
	}
	return DefaultMediaRoute(models, "image")
}

// ResolveVideoRoute is [Config.ResolveVideoModel] with the reason attached.
func (c Config) ResolveVideoRoute(models *catalog.Catalog) MediaRoute {
	if configured := strings.TrimSpace(c.VideoModel); configured != "" {
		return MediaRoute{Modality: "video", Model: configured, Reason: RouteConfigured}
	}
	return DefaultMediaRoute(models, "video")
}
