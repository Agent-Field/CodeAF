package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// THE DEDICATED MEDIA CATALOGS — what /models?output_modalities=all does not say.
//
// OpenRouter's general listing carries no video output at all, and only a
// sixteenth of the image generators: the rest are published by two sibling
// listings, GET /images/models (59 rows on 2026-10-15) and GET /videos/models
// (30). Their rows are shaped differently from a chat row — parameters are an
// object of enums and ranges, a video row has no architecture block, and
// prices are per unit of output (a second at 480p, a megapixel) rather than per
// token — so they are read here, merged into the one catalog every capability
// question already asks, and kept in their own cache compartment so a failure
// of one listing never costs the facts of another.
//
// WHAT THIS DOES NOT DO: it fetches no per-endpoint detail at startup (that is
// one request per model; see [ImageEndpointPrices] for the lazy, single-model
// door), it never posts anything, and it never speaks to a base URL that is not
// the OpenRouter source's own.

const (
	// MaxMediaCatalogBytes bounds one dedicated listing before it is decoded.
	// The live listings are 28 KB and 45 KB; this leaves two orders of
	// magnitude and still refuses a response that was never a catalog.
	MaxMediaCatalogBytes = 4 << 20
	// MaxMediaRows bounds how many rows one dedicated listing may carry. A
	// listing past it is refused whole rather than truncated: a partial video
	// list is not evidence of which videos exist.
	MaxMediaRows = 500
	// MaxEndpointBytes and MaxEndpointRows bound the single-model endpoint
	// detail the same way.
	MaxEndpointBytes = 256 << 10
	MaxEndpointRows  = 32

	// mediaRetry is how soon a dedicated listing that failed is tried again by
	// a load nobody explicitly asked to refresh, so an offline machine pays a
	// failed request at most once per interval rather than once per launch.
	mediaRetry = 15 * time.Minute

	kindImage = "image"
	kindVideo = "video"
)

// MediaPrice is one published price of a media model, in dollars per Unit.
// It is typed on purpose: a per-second video rate and a per-megapixel image
// rate are not per-token prices and must never be shown in a per-token column.
// An empty list on a model means NOBODY PUBLISHED ONE that this package can
// read, never that the model is free.
type MediaPrice struct {
	// Billable is what is charged for: "video_output", "image_output" or
	// "image_input".
	Billable string `json:"billable"`
	// Unit is what one USD figure buys: "second", "image" or "megapixel".
	Unit string `json:"unit"`
	// Variant narrows the rate, today only the output resolution ("480p").
	Variant string `json:"variant,omitempty"`
	// Provider is the endpoint that published it; empty for a model-level rate.
	Provider string  `json:"provider,omitempty"`
	USD      float64 `json:"usd"`
}

// MediaParams is what the dedicated listing publishes about the knobs of a
// media model. Every field is empty when unpublished, which means unknown.
type MediaParams struct {
	Resolutions  []string `json:"resolutions,omitempty"`
	AspectRatios []string `json:"aspect_ratios,omitempty"`
	// Durations are the output lengths in seconds a video model accepts.
	Durations []int `json:"durations,omitempty"`
	// FrameImages names the still frames a video model takes ("first_frame").
	FrameImages []string `json:"frame_images,omitempty"`
	// MaxInputReferences is how many reference images an image model takes.
	MaxInputReferences int `json:"max_input_references,omitempty"`
	// MaxOutputs is how many images one request may return.
	MaxOutputs int `json:"max_outputs,omitempty"`
}

// MediaKind is what a catalog knows about one dedicated listing.
//
// Known false means the listing has NEVER been read on this install, so no
// absence from the catalog proves anything. Stale means the facts are older
// than [TTL] or the last attempt to refresh them failed; a stale kind still
// serves its rows, dated.
type MediaKind struct {
	Known     bool
	Stale     bool
	FetchedAt time.Time
	// due says a load that nobody insisted on may try the listing again now.
	due bool
}

// MediaState is the standing of both dedicated listings.
type MediaState struct {
	// Applicable says this catalog is one that reads the dedicated listings at
	// all (the OpenRouter source on its own base). False means the question
	// "was the video listing read?" has no answer here, and the general rows are
	// all there is; true with a kind that is not [MediaKind.Known] means the
	// listing has never been read on this install.
	Applicable     bool
	Images, Videos MediaKind
}

// unreadMedia is the standing of a catalog that is eligible for the dedicated
// listings and has read neither: the compiled-in fallback rows, and an install
// whose first fetch failed.
func unreadMedia(eligible bool) MediaState { return MediaState{Applicable: eligible} }

// Authoritative says the named kind ("image" or "video") was read and is
// current, so that a model's ABSENCE from the catalog may be reported as "no
// connected service offers it". Anything else — never read, failed, or old —
// must be reported as "could not confirm", because a transient failure is not
// proof of none.
func (s MediaState) Authoritative(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case kindImage, "images":
		return s.Images.Known && !s.Images.Stale
	case kindVideo, "videos":
		return s.Videos.Known && !s.Videos.Stale
	}
	return false
}

// due says some listing is worth another request now.
func (s MediaState) due() bool { return s.Images.due || s.Videos.due }

// mediaCache is the persisted half: the PURE dedicated rows, unmerged, beside
// when each listing was last read and last tried. Keeping them unmerged lets a
// later general refresh re-merge them over fresh chat rows without carrying
// forward a text capability the chat row has since lost. Absent in a cache
// written before this field existed, which reads as "never read".
type mediaCache struct {
	Images mediaKindCache `json:"images"`
	Videos mediaKindCache `json:"videos"`
	// General is the general /models rows exactly as they were fetched, BEFORE
	// any dedicated row was laid over them. The cache's own Models are the
	// merged result (an older reader wants one list), and a merged row cannot be
	// un-merged: a hybrid keeps its chat capabilities and a generator does not,
	// and nothing in the merged row says which part came from where. Every
	// re-merge therefore starts from this snapshot and the current dedicated
	// rows, so a model a listing retired is gone and a repriced one has ONE
	// price. Empty in a cache a build without this field wrote: see [generalOf].
	General []Model `json:"general,omitempty"`
}

type mediaKindCache struct {
	// FetchedAt is the last SUCCESS; zero is "never".
	FetchedAt time.Time `json:"fetched_at"`
	// TriedAt is the last attempt, success or not.
	TriedAt time.Time `json:"tried_at"`
	Rows    []Model   `json:"rows,omitempty"`
}

// mediaEligible is the one gate on every dedicated request. These two paths
// exist on OpenRouter and are not an OpenAI-compatible convention, so a custom
// endpoint, another source, and a Codex compartment never see them.
func mediaEligible(options Options, source, base string) bool {
	if options.NoDedicatedMedia || base != DefaultBaseURL {
		return false
	}
	return source == "" || strings.EqualFold(source, modelsource.DefaultID)
}

func mediaStateOf(cached *mediaCache, now time.Time) MediaState {
	if cached == nil {
		// Never read: due at once, once.
		cached = &mediaCache{}
	}
	return MediaState{
		Applicable: true,
		Images:     kindStateOf(cached.Images, now),
		Videos:     kindStateOf(cached.Videos, now),
	}
}

func kindStateOf(kind mediaKindCache, now time.Time) MediaKind {
	state := MediaKind{FetchedAt: kind.FetchedAt}
	if kind.FetchedAt.IsZero() {
		state.due = kind.TriedAt.IsZero() || !now.Before(kind.TriedAt.Add(mediaRetry))
		return state
	}
	state.Known = true
	failedSince := kind.TriedAt.After(kind.FetchedAt)
	state.Stale = failedSince || !now.Before(kind.FetchedAt.Add(TTL))
	state.due = state.Stale && (kind.TriedAt.IsZero() || !now.Before(kind.TriedAt.Add(mediaRetry)))
	return state
}

// generalOf is the pristine general rows a cache can vouch for. A cache that
// never merged a dedicated listing holds only general rows in Models. One that
// did holds them in the snapshot. One that merged WITHOUT keeping the snapshot
// (a build from before the field) cannot say which capabilities were the chat
// row's own, and the answer is "unknown" — the caller must refetch the general
// listing rather than pretend to recover them.
func generalOf(cached cache) ([]Model, bool) {
	if cached.Media == nil {
		return cached.Models, true
	}
	if len(cached.Media.General) > 0 {
		return cached.Media.General, true
	}
	return nil, false
}

// refreshMedia reads both dedicated listings (at most two requests, in
// parallel, joined before it returns) and merges them over the GENERAL rows
// given — never over rows already merged. A listing that fails keeps the
// previous rows and date of its compartment and is reported stale; it never
// empties the catalog. A listing that succeeds REPLACES its compartment.
func refreshMedia(ctx context.Context, options Options, general []Model, prior *mediaCache, now time.Time) ([]Model, *mediaCache, MediaState) {
	next := &mediaCache{}
	if prior != nil {
		*next = *prior
	}
	// The two requests share one deadline, so the whole top-up is bounded by a
	// single fetch timeout rather than two.
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	var (
		wg                   sync.WaitGroup
		imageRows, videoRows []Model
		imageErr, videoErr   error
	)
	// guard.Go carries the recover, so a fault in a listing's decoding reads as
	// that listing failing. The failure is pre-set for exactly that case: a
	// recovered goroutine returns nothing, and nothing must not read as success.
	run := func(kind string, rows *[]Model, failure *error) {
		*failure = errUnreadable
		guard.Go("catalog/media", func() {
			defer wg.Done()
			*rows, *failure = fetchMedia(ctx, options, kind)
		})
	}
	wg.Add(2)
	run(kindImage, &imageRows, &imageErr)
	run(kindVideo, &videoRows, &videoErr)
	wg.Wait()

	settle := func(kind *mediaKindCache, rows []Model, err error) {
		kind.TriedAt = now
		if err == nil {
			kind.FetchedAt, kind.Rows = now, rows
		}
	}
	settle(&next.Images, imageRows, imageErr)
	settle(&next.Videos, videoRows, videoErr)
	next.General = general
	return mergeMedia(general, next), next, mediaStateOf(next, now)
}

// mergeMedia lays the pure dedicated rows over the GENERAL rows. Its first
// argument must never be an earlier result of itself: the overlay adds and
// unions, so merging twice keeps what the first merge added.
//
// The dedicated listing is authoritative for what a media model takes and
// returns, so its modalities, parameters and prices win. The general row stays
// authoritative for being a CHAT model: a row that answers in text there keeps
// answering in text, and the chat parameters, token prices and context length
// it published are untouched. A pure generator — present only in a dedicated
// listing — is given exactly the outputs that listing implies, and never text.
func mergeMedia(models []Model, media *mediaCache) []Model {
	if media == nil {
		return models
	}
	merged := make([]Model, len(models), len(models)+len(media.Images.Rows)+len(media.Videos.Rows))
	copy(merged, models)
	index := make(map[string]int, len(merged))
	for i, model := range merged {
		if _, seen := index[normalizeID(model.ID)]; !seen {
			index[normalizeID(model.ID)] = i
		}
	}
	for _, dedicated := range append(append([]Model(nil), media.Images.Rows...), media.Videos.Rows...) {
		dedicated.ID = strings.TrimSpace(dedicated.ID)
		if dedicated.ID == "" || strings.HasSuffix(dedicated.ID, ":batch") {
			continue
		}
		at, found := index[normalizeID(dedicated.ID)]
		if !found {
			index[normalizeID(dedicated.ID)] = len(merged)
			merged = append(merged, cloneModel(dedicated))
			continue
		}
		merged[at] = overlayMedia(cloneModel(merged[at]), dedicated)
	}
	return merged
}

func overlayMedia(general, dedicated Model) Model {
	chat := hasModality(general.OutputModalities, "text")
	outputs := append([]string(nil), dedicated.OutputModalities...)
	if chat && !hasModality(outputs, "text") {
		outputs = append([]string{"text"}, outputs...)
	}
	inputs := append([]string(nil), dedicated.InputModalities...)
	// A video row publishes no architecture, so its inputs are only inferred
	// (see [videoRow]) and must not displace inputs the general row published:
	// a video model that takes sound or footage says so there, not here.
	inferred := false
	for _, kind := range dedicated.Dedicated {
		inferred = inferred || kind == kindVideo
	}
	if chat || inferred {
		inputs = append(append([]string(nil), general.InputModalities...), inputs...)
	}
	general.InputModalities = cleanLowerList(inputs)
	general.OutputModalities = cleanLowerList(outputs)
	general.Dedicated = unionStrings(general.Dedicated, dedicated.Dedicated)
	if dedicated.Media != nil {
		general.Media = mergeParams(general.Media, dedicated.Media)
	}
	general.MediaPrices = unionPrices(general.MediaPrices, dedicated.MediaPrices)
	if general.Name == "" {
		general.Name = dedicated.Name
	}
	if general.Created == 0 {
		general.Created = dedicated.Created
	}
	return general
}

func mergeParams(have, add *MediaParams) *MediaParams {
	if have == nil {
		copied := *add
		return &copied
	}
	merged := *have
	if len(add.Resolutions) > 0 {
		merged.Resolutions = add.Resolutions
	}
	if len(add.AspectRatios) > 0 {
		merged.AspectRatios = add.AspectRatios
	}
	if len(add.Durations) > 0 {
		merged.Durations = add.Durations
	}
	if len(add.FrameImages) > 0 {
		merged.FrameImages = add.FrameImages
	}
	if add.MaxInputReferences > 0 {
		merged.MaxInputReferences = add.MaxInputReferences
	}
	if add.MaxOutputs > 0 {
		merged.MaxOutputs = add.MaxOutputs
	}
	return &merged
}

func unionStrings(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, value := range b {
		found := false
		for _, have := range out {
			if have == value {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}

func unionPrices(a, b []MediaPrice) []MediaPrice {
	out := append([]MediaPrice(nil), a...)
	for _, price := range b {
		found := false
		for _, have := range out {
			if have == price {
				found = true
				break
			}
		}
		if !found {
			out = append(out, price)
		}
	}
	return out
}

// fetchMedia reads one dedicated listing. Every failure is an error and none
// is an empty result: a listing that parsed to no usable row is a listing that
// could not be read, not proof that the service offers none.
func fetchMedia(ctx context.Context, options Options, kind string) ([]Model, error) {
	path := "/images/models"
	if kind == kindVideo {
		path = "/videos/models"
	}
	body, err := getBounded(ctx, options, strings.TrimSuffix(strings.TrimSpace(options.BaseURL), "/")+path, MaxMediaCatalogBytes)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) > MaxMediaRows {
		return nil, &statusError{status: "dedicated listing too long"}
	}
	rows := make([]Model, 0, len(payload.Data))
	for _, raw := range payload.Data {
		var row Model
		var ok bool
		if kind == kindVideo {
			row, ok = videoRow(raw)
		} else {
			row, ok = imageRow(raw)
		}
		if ok {
			rows = append(rows, row)
		}
	}
	rows = cleanModels(rows)
	if len(rows) == 0 {
		return nil, &statusError{status: "empty dedicated listing"}
	}
	return rows, nil
}

// getBounded is one GET with the catalog's headers, its client and a hard
// ceiling on the bytes read. It reads one byte past the ceiling so a response
// that is too large is refused rather than silently cut into invalid JSON.
func getBounded(ctx context.Context, options Options, endpoint string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(options.APIKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: FetchTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, &statusError{status: response.Status}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, &statusError{status: "response too large"}
	}
	return body, nil
}

// videoRow reads one row of GET /videos/models. The row has no architecture
// block, so its modalities are the ones its fields imply: it returns video and
// nothing else, takes a prompt, and takes a picture only if it lists frame
// images. Each field is decoded on its own so one drifted field costs itself.
func videoRow(raw json.RawMessage) (Model, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Model{}, false
	}
	id := rawString(fields["id"])
	if id == "" {
		return Model{}, false
	}
	// A row that publishes none of the video fields is not a video row — it is
	// a chat row that reached the wrong URL.
	known := false
	for _, key := range []string{"supported_resolutions", "supported_aspect_ratios", "supported_durations", "supported_frame_images", "pricing_skus"} {
		if len(fields[key]) > 0 && string(fields[key]) != "null" {
			known = true
		}
	}
	if !known {
		return Model{}, false
	}
	params := MediaParams{
		Resolutions:  cleanList(rawStrings(fields["supported_resolutions"])),
		AspectRatios: cleanList(rawStrings(fields["supported_aspect_ratios"])),
		Durations:    rawInts(fields["supported_durations"]),
		FrameImages:  cleanList(rawStrings(fields["supported_frame_images"])),
	}
	inputs := []string{"text"}
	if len(params.FrameImages) > 0 {
		inputs = append(inputs, "image")
	}
	return Model{
		ID: id, Name: rawString(fields["name"]), Created: rawInt(fields["created"]),
		// The token price is unknown, not zero: this model is billed per second.
		PriceUnknown:    true,
		InputModalities: inputs, OutputModalities: []string{"video"},
		Media: &params, MediaPrices: videoPrices(fields["pricing_skus"]),
		Dedicated: []string{kindVideo},
	}, true
}

// videoPrices reads the pricing_skus this package can state the unit of, which
// is the cents-per-unit family:
//
//	cents_per_video_output_second_480p  → video output, per second, at 480p
//	cents_per_second_output[_720p]      → the same, spelled the other way
//	cents_per_image_input               → a still given to the model, per image
//
// Every other sku — duration_seconds_*, video_tokens_*, the megapixel and
// continuation rates, a minimum charge — is NOT interpreted: guessing a unit
// for a number that will be shown beside a "$" is how a surface tells someone
// a video costs a hundredth of what it does. They are left unknown.
func videoPrices(raw json.RawMessage) []MediaPrice {
	var skus map[string]json.RawMessage
	if json.Unmarshal(raw, &skus) != nil {
		return nil
	}
	var prices []MediaPrice
	for key, value := range skus {
		rest, ok := strings.CutPrefix(key, "cents_per_")
		if !ok {
			continue
		}
		cents, ok := rawNumber(value)
		if !ok || cents < 0 {
			continue
		}
		price := MediaPrice{USD: cents / 100}
		switch {
		case rest == "image_input":
			price.Billable, price.Unit = "image_input", "image"
		case strings.HasPrefix(rest, "video_output_second"):
			price.Billable, price.Unit = "video_output", "second"
			price.Variant, ok = resolutionSuffix(strings.TrimPrefix(rest, "video_output_second"))
		case strings.HasPrefix(rest, "second_output"):
			price.Billable, price.Unit = "video_output", "second"
			price.Variant, ok = resolutionSuffix(strings.TrimPrefix(rest, "second_output"))
		default:
			ok = false
		}
		if ok {
			prices = append(prices, price)
		}
	}
	sortPrices(prices)
	return prices
}

// resolutionSuffix accepts "" or "_480p" / "_2k" and nothing else, so an
// unfamiliar qualifier ("_continuation_2k") makes the whole sku unknown.
func resolutionSuffix(suffix string) (string, bool) {
	if suffix == "" {
		return "", true
	}
	variant, ok := strings.CutPrefix(suffix, "_")
	if !ok || len(variant) < 2 || len(variant) > 5 {
		return "", false
	}
	last := variant[len(variant)-1] | 0x20
	if last != 'p' && last != 'k' {
		return "", false
	}
	if _, err := strconv.Atoi(variant[:len(variant)-1]); err != nil {
		return "", false
	}
	return strings.ToLower(variant), true
}

func sortPrices(prices []MediaPrice) {
	for i := 1; i < len(prices); i++ {
		for j := i; j > 0 && priceKey(prices[j]) < priceKey(prices[j-1]); j-- {
			prices[j], prices[j-1] = prices[j-1], prices[j]
		}
	}
}

func priceKey(p MediaPrice) string {
	return p.Billable + "\x00" + p.Unit + "\x00" + p.Variant + "\x00" + p.Provider
}

// imageRow reads one row of GET /images/models. That listing publishes
// modalities and parameters and NO price: image prices live per endpoint.
func imageRow(raw json.RawMessage) (Model, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Model{}, false
	}
	id := rawString(fields["id"])
	if id == "" {
		return Model{}, false
	}
	var architecture struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	}
	_ = json.Unmarshal(fields["architecture"], &architecture)
	outputs := cleanLowerList(architecture.Output)
	if len(outputs) == 0 {
		// Served by the image listing, so it draws; nothing else is implied.
		outputs = []string{"image"}
	}
	if !hasModality(outputs, "image") {
		return Model{}, false
	}
	inputs := cleanLowerList(architecture.Input)
	if len(inputs) == 0 {
		inputs = []string{"text"}
	}
	params := imageParams(fields["supported_parameters"])
	return Model{
		ID: id, Name: rawString(fields["name"]), Created: rawInt(fields["created"]),
		PriceUnknown:    true,
		InputModalities: inputs, OutputModalities: outputs,
		Media: params, Dedicated: []string{kindImage},
	}, true
}

// imageParams reads the supported_parameters object: each knob is an enum with
// values, a range with bounds, or a boolean.
func imageParams(raw json.RawMessage) *MediaParams {
	var knobs map[string]struct {
		Values []string `json:"values"`
		Max    *float64 `json:"max"`
	}
	if json.Unmarshal(raw, &knobs) != nil || len(knobs) == 0 {
		return nil
	}
	params := MediaParams{
		Resolutions:  cleanList(knobs["resolution"].Values),
		AspectRatios: cleanList(knobs["aspect_ratio"].Values),
	}
	if max := knobs["input_references"].Max; max != nil && *max > 0 && *max < 1000 {
		params.MaxInputReferences = int(*max)
	}
	if max := knobs["n"].Max; max != nil && *max > 0 && *max < 1000 {
		params.MaxOutputs = int(*max)
	}
	return &params
}

// ImageEndpointPrices is the lazy, single-model price door for an image
// generator: ONE GET of /images/models/{id}/endpoints, made only when a caller
// needs the price of one model it is about to show or use.
//
// The prices there are per endpoint and per unit — dollars per image or per
// megapixel — and different endpoints of one model may differ, so every
// published rate comes back with the provider that published it and the caller
// decides how to present a range. It is bounded like every other read here, it
// is never part of startup, and it answers an error rather than an empty list
// when nothing was published.
func ImageEndpointPrices(ctx context.Context, options Options, modelID string) ([]MediaPrice, error) {
	source := strings.TrimSpace(options.Source)
	if !mediaEligible(options, source, normalizeBase(options.BaseURL)) {
		return nil, &statusError{status: "no dedicated image catalog for this service"}
	}
	segments := strings.Split(strings.TrimSpace(modelID), "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, "?#\\") {
			return nil, errors.New("model catalog: not a model id")
		}
		if len(segment) > 128 {
			return nil, errors.New("model catalog: not a model id")
		}
	}
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	endpoint := strings.TrimSuffix(strings.TrimSpace(options.BaseURL), "/") + "/images/models/" + strings.Join(segments, "/") + "/endpoints"
	body, err := getBounded(ctx, options, endpoint, MaxEndpointBytes)
	if err != nil {
		return nil, err
	}
	return endpointPrices(body)
}

func endpointPrices(body []byte) ([]MediaPrice, error) {
	var payload struct {
		Endpoints []json.RawMessage `json:"endpoints"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Endpoints) > MaxEndpointRows {
		return nil, &statusError{status: "endpoint listing too long"}
	}
	var prices []MediaPrice
	for _, raw := range payload.Endpoints {
		var endpoint struct {
			Slug    string `json:"provider_slug"`
			Name    string `json:"provider_name"`
			Pricing []struct {
				Billable string   `json:"billable"`
				Unit     string   `json:"unit"`
				Cost     *float64 `json:"cost_usd"`
			} `json:"pricing"`
		}
		if json.Unmarshal(raw, &endpoint) != nil {
			continue
		}
		provider := strings.TrimSpace(endpoint.Slug)
		if provider == "" {
			provider = strings.TrimSpace(endpoint.Name)
		}
		for _, entry := range endpoint.Pricing {
			unit := strings.ToLower(strings.TrimSpace(entry.Unit))
			billable := strings.ToLower(strings.TrimSpace(entry.Billable))
			if entry.Cost == nil || *entry.Cost < 0 || math.IsNaN(*entry.Cost) || math.IsInf(*entry.Cost, 0) {
				continue
			}
			if unit != "image" && unit != "megapixel" {
				continue
			}
			if billable != "output_image" && billable != "input_image" {
				continue
			}
			prices = append(prices, MediaPrice{
				Billable: map[string]string{"output_image": "image_output", "input_image": "image_input"}[billable],
				Unit:     unit, Provider: provider, USD: *entry.Cost,
			})
		}
	}
	if len(prices) == 0 {
		return nil, &statusError{status: "no published price"}
	}
	sortPrices(prices)
	return prices, nil
}

func rawString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func rawStrings(raw json.RawMessage) []string {
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

func rawInt(raw json.RawMessage) int64 {
	number, ok := rawNumber(raw)
	if !ok || number < 0 || number > 1<<53 {
		return 0
	}
	return int64(number)
}

// rawNumber reads a JSON number or a numeric string, because pricing_skus
// publishes its figures as strings and a drift to numbers must not cost them.
func rawNumber(raw json.RawMessage) (float64, bool) {
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func rawInts(raw json.RawMessage) []int {
	var values []float64
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value >= 1 && value <= 3600 && value == math.Trunc(value) {
			out = append(out, int(value))
		}
	}
	return out
}

func cleanList(values []string) []string {
	seen := make(map[string]bool, len(values))
	var cleaned []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

// MediaState is the standing of the dedicated image and video listings. Like
// every capability question it waits for the catalog to resolve.
func (c *Catalog) MediaState() MediaState {
	resolved := c.rows()
	if resolved == nil {
		return MediaState{}
	}
	return resolved.media
}

// MediaStateNow is [Catalog.MediaState] without waiting: a catalog still
// warming answers "never read", which is the honest word for it.
func (c *Catalog) MediaStateNow() MediaState {
	resolved := c.rowsNow()
	if resolved == nil {
		return MediaState{}
	}
	return resolved.media
}

// PublishedPrice says the dedicated listing, or the general one, genuinely
// published a finite price for what this model makes ("image" or "video"), in
// a unit a person can be told. It is the test a DEFAULT must pass before it is
// chosen for someone who chose nothing.
//
// Prompt/completion token zeros do not establish a generation tariff. Some
// image models really bill output tokens, but this catalog does not yet carry
// that output tariff separately; it remains unknown here. Unreadable units
// remain unknown too. A positive published per-request price counts for an
// image, while an explicit dedicated per-image zero remains a known zero.
func (m Model) PublishedPrice(output string) bool {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case kindVideo:
		for _, price := range m.MediaPrices {
			if price.Billable == "video_output" && price.Unit == "second" && finitePrice(price.USD) {
				return true
			}
		}
	case kindImage:
		for _, price := range m.MediaPrices {
			if price.Billable == "image_output" && (price.Unit == "image" || price.Unit == "megapixel") && finitePrice(price.USD) {
				return true
			}
		}
		return m.RequestPrice > 0 && finitePrice(m.RequestPrice)
	}
	return false
}

func finitePrice(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// MediaPrices answers the published per-unit prices of one model, and nil when
// none were published. Nil is "unknown", never "free".
func (c *Catalog) MediaPrices(modelID string) []MediaPrice {
	model, ok := c.Model(modelID)
	if !ok {
		return nil
	}
	return model.MediaPrices
}

// MediaParams answers the dedicated listing's knobs for one model.
func (c *Catalog) MediaParams(modelID string) (MediaParams, bool) {
	model, ok := c.Model(modelID)
	if !ok || model.Media == nil {
		return MediaParams{}, false
	}
	return *model.Media, true
}
