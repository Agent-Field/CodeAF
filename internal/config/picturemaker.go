package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/env"
)

// THE PICTURE MAKER — the one model that draws, chosen at two reaches.
//
// A person can choose which model makes their pictures EVERYWHERE (the image
// capability slot the settings sheet already writes, read through
// [MediaSlotModelAt]) and, over that, for ONE CANVAS (a row in this file's own
// store, keyed by the canvas's working folder, because a canvas's agents run
// in that folder and it is the one thing the engine and the desk both know
// about a canvas). An empty choice at either reach is AUTOMATIC, never a
// blank: the route below picks for the person, and says which model it picked
// and why, so the desk can draw "Automatic · Seedream 5 Pro" instead of a
// field that looks unset.
//
// TWO LAWS GOVERN WHAT HAPPENS WHEN THE MODEL DOES NOT ANSWER, and the route
// carries what each one needs:
//
//   - AUTOMATIC FALLS THROUGH AND SAYS SO. Nobody chose the model, so the next
//     one on the same ladder may draw instead; [PictureRoute.Then] is that
//     ladder, already filtered for what the ask needs, and the result names the
//     model that drew and the one passed over.
//   - A CHOICE IS NEVER SWAPPED SILENTLY. A model somebody named is the only
//     model that may draw for that ask; when it does not answer, the ask comes
//     back as a question, and [PictureRoute.Alternative] is the model Automatic
//     would have used, so the question can name it.
//
// NO MODEL NAME IS WRITTEN HERE. The ladder is [curatedRoutes]'s preference
// order and then the listing's own published facts, the same ladder
// [DefaultMediaRoute] walks, so the first rung of [ImageLadder] and the
// default route can never name different models (a test pins it).

// Where a picture route's model came from. Only [PictureScopeAutomatic] may be
// replaced by another model when it does not answer.
const (
	// PictureScopeCall: the call itself named a model (generate_image's model
	// argument).
	PictureScopeCall = "call"
	// PictureScopeCanvas: the person chose a picture maker for this canvas.
	PictureScopeCanvas = "canvas"
	// PictureScopeEverywhere: the person chose a picture maker for everything
	// (the image slot, or its environment variable).
	PictureScopeEverywhere = "everywhere"
	// PictureScopePin: the operator's role pin for image generation.
	PictureScopePin = "pin"
	// PictureScopeAutomatic: nobody chose; the ladder picked.
	PictureScopeAutomatic = "automatic"
)

// The refusals a picture route can carry when it has no model. Each has its
// own sentence because each asks the person for a different thing.
const (
	// PictureNoneAdvertised: the listing is current and nothing in it draws.
	PictureNoneAdvertised = string(RouteNoneAdvertised)
	// PictureUnconfirmed: the listing was never read or is out of date.
	PictureUnconfirmed = string(RouteUnconfirmed)
	// PictureModelRequired: models draw, but none carries a published price,
	// so Automatic will not spend on anyone's behalf.
	PictureModelRequired = string(RouteModelRequired)
	// PictureCannotReadPhotos: the chosen model does not take a picture in,
	// and the ask changes or starts from one.
	PictureCannotReadPhotos = "cannot_read_photos"
	// PictureNoPhotoReader: nobody chose, and no model on the ladder takes a
	// picture in.
	PictureNoPhotoReader = "no_photo_reader"
	// PictureNotAPictureMaker: the chosen model is listed, and it does not draw.
	PictureNotAPictureMaker = "not_a_picture_maker"
	// PictureModelInvalid: the call's own model word matched nothing.
	PictureModelInvalid = "model_invalid"
)

// maxPictureLadder bounds how many models one Automatic ask may try. A model
// that does not answer usually costs nothing, but three in a row is an outage,
// and a fourth attempt would only make the person wait longer for the same news.
const maxPictureLadder = 3

// gemini image tokens: a picture billed per output-image token is estimated at
// the token count Google publishes for one 1K picture (1120 tokens for the
// Gemini 3 image models). That is the only unit here that needs a conversion
// figure, and the figure is named so it is never a magic number in a sum.
const tokensPerPicture = 1120

// megapixelsPerPicture is one 1024×1024 picture, the size a picture maker
// draws when nobody names one.
const megapixelsPerPicture = 1024 * 1024 / 1e6

// PictureMaker is one model that draws, as the catalogue describes it. Every
// field is a published fact or empty: an unpublished capability is false and
// an unpublished price is not Priced, never a guess.
type PictureMaker struct {
	ID   string
	Name string
	// PricePerPicture is the cost of one picture at the model's default size,
	// valid only when Priced. PriceUnit says what the listing actually priced
	// ("image", "request", "megapixel" or "token"); the last two are converted
	// at a 1K picture and are marked Estimated.
	PricePerPicture float64
	Priced          bool
	PriceUnit       string
	Estimated       bool
	// ReadsPhotos: the model takes a picture in, so it can change or start
	// from one. MasksEdits: it takes a real mask and changes only that area.
	ReadsPhotos bool
	MasksEdits  bool
	// MaxPictures is how many pictures one request returns (zero: unpublished;
	// several requests are sent when more are asked for). MaxPhotos is how
	// many pictures it takes in.
	MaxPictures int
	MaxPhotos   int
	// Shapes are the width:height frames it draws; empty means unpublished.
	Shapes []string
	// Transparent: the listing publishes a transparent background option.
	Transparent bool
	// Automatic says Automatic may pick this model: it carries a published
	// price and takes words.
	Automatic bool
	// Unavailable is why this model cannot make a picture now, empty when it
	// can: "no_prompt" (it takes no words) or "not_listed" (a chosen model no
	// connected provider lists).
	Unavailable string
}

// PictureMakers lists every model the catalogue says draws, sorted by name
// (an order with no judgement in it: a list sorted by price or score would be
// a recommendation). Nil catalog, nil list.
func PictureMakers(models *catalog.Catalog) []PictureMaker {
	if models == nil {
		return nil
	}
	candidates := ModelCandidates(models, "image")
	out := make([]PictureMaker, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, pictureMakerOf(candidate))
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// PictureMakerOf is one model's row, or a row that says it is not listed.
func PictureMakerOf(models *catalog.Catalog, id string) PictureMaker {
	id = strings.TrimSpace(id)
	if models != nil {
		if row, ok := models.Model(id); ok {
			return pictureMakerOf(row)
		}
	}
	return PictureMaker{ID: id, Name: PictureMakerName(models, id), Unavailable: "not_listed"}
}

func pictureMakerOf(row catalog.Model) PictureMaker {
	maker := PictureMaker{ID: row.ID, Name: displayName(row)}
	maker.PricePerPicture, maker.PriceUnit, maker.Priced = PicturePrice(row)
	maker.Estimated = maker.Priced && (maker.PriceUnit == "megapixel" || maker.PriceUnit == "token")
	maker.ReadsPhotos = readsPhotos(row)
	if row.Media != nil {
		maker.MasksEdits = row.Media.Mask
		maker.MaxPictures = row.Media.MaxOutputs
		maker.MaxPhotos = row.Media.MaxInputReferences
		maker.Shapes = append([]string(nil), row.Media.AspectRatios...)
		maker.Transparent = row.Media.Transparent
	}
	maker.Automatic = row.PublishedPrice("image") && takesPrompt(row)
	if !takesPrompt(row) {
		maker.Unavailable = "no_prompt"
	}
	return maker
}

// PicturePrice is one picture's price from the listing's own figures: per
// image or per request as published, per megapixel at a 1024×1024 picture,
// per output-image token at [tokensPerPicture]. The model's own rate wins over
// an endpoint's, and among several endpoints the lowest is the "from" price.
func PicturePrice(row catalog.Model) (usd float64, unit string, ok bool) {
	best := math.Inf(1)
	bestUnit := ""
	modelLevel := false
	for _, price := range row.MediaPrices {
		if price.Billable != "image_output" || !finitePrice(price.USD) {
			continue
		}
		var each float64
		switch price.Unit {
		case "image":
			each = price.USD
		case "megapixel":
			each = price.USD * megapixelsPerPicture
		case "token":
			each = price.USD * tokensPerPicture
		default:
			continue
		}
		own := price.Provider == ""
		switch {
		case own && !modelLevel, own == modelLevel && each < best:
			best, bestUnit, modelLevel = each, price.Unit, own
		}
	}
	if bestUnit != "" {
		return best, bestUnit, true
	}
	if row.RequestPrice > 0 && finitePrice(row.RequestPrice) {
		return row.RequestPrice, "request", true
	}
	return 0, "", false
}

func finitePrice(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// readsPhotos says the row takes a picture in. A row that publishes no inputs
// at all is not claimed to.
func readsPhotos(row catalog.Model) bool {
	for _, input := range row.InputModalities {
		if strings.EqualFold(strings.TrimSpace(input), "image") {
			return true
		}
	}
	return false
}

// PictureMakerName is the name a person reads for a model: the listing's own
// name without its vendor prefix ("ByteDance Seed: Seedream 5.0 Pro" reads
// "Seedream 5.0 Pro"), or the id's last segment when the listing names none.
func PictureMakerName(models *catalog.Catalog, id string) string {
	id = strings.TrimSpace(id)
	if models != nil {
		if row, ok := models.Model(id); ok {
			return displayName(row)
		}
	}
	return idTail(id)
}

func displayName(row catalog.Model) string {
	name := strings.TrimSpace(row.Name)
	if _, after, found := strings.Cut(name, ": "); found && strings.TrimSpace(after) != "" {
		name = strings.TrimSpace(after)
	}
	if name == "" {
		return idTail(row.ID)
	}
	return name
}

func idTail(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

// ImageLadder is Automatic's order for one ask: the remembered defaults that
// are advertised and priced, then every other priced model that takes words,
// best published Design Arena score first and then by id — the same two rungs
// [DefaultMediaRoute] walks, so its model is this ladder's first. When the ask
// changes or starts from a picture, only models that read one are on it. It is
// empty when the listing is not current enough to choose for anyone.
func ImageLadder(models *catalog.Catalog, needsPhotos bool) []string {
	route := DefaultMediaRoute(models, "image")
	if route.Reason == RouteUnconfirmed || route.Reason == RouteNoneAdvertised {
		return nil
	}
	candidates := ModelCandidates(models, "image")
	usable := func(row catalog.Model) bool {
		return row.PublishedPrice("image") && takesPrompt(row) && (!needsPhotos || readsPhotos(row))
	}
	var ladder []string
	seen := map[string]bool{}
	for _, preferred := range curatedRoutes["image"] {
		for _, row := range candidates {
			if row.ID == preferred && !seen[row.ID] && usable(row) {
				ladder, seen[row.ID] = append(ladder, row.ID), true
			}
		}
	}
	var rest []catalog.Model
	for _, row := range candidates {
		if !seen[row.ID] && usable(row) {
			rest = append(rest, row)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].ArenaElo != rest[j].ArenaElo {
			return rest[i].ArenaElo > rest[j].ArenaElo
		}
		return rest[i].ID < rest[j].ID
	})
	for _, row := range rest {
		ladder = append(ladder, row.ID)
	}
	return ladder
}

// PictureAsk is what one picture needs to be routed: the call's own model word
// (empty for none), the folder the asking agent works in (a canvas's working
// folder), whether it starts from or changes a picture, and the operator's
// role pin.
type PictureAsk struct {
	Word        string
	Workspace   string
	NeedsPhotos bool
	Pin         string
}

// PictureRoute is the answer: the model that draws and where it came from,
// or, with no model, the refusal and a sentence a person can act on.
type PictureRoute struct {
	Model string
	Scope string
	// Reason is how Automatic chose (curated or listed), or why there is no
	// model (a Picture* refusal).
	Reason string
	// Then is Automatic's remaining ladder, at most [maxPictureLadder]-1
	// models, tried in order when Model does not answer. Always empty for a
	// chosen model.
	Then []string
	// Alternative is, for a chosen model, the model Automatic would use for
	// this ask: the one a "use X instead?" question names. Empty when
	// Automatic would pick the same model or nothing.
	Alternative string
	// Message is the person-facing sentence when there is no model.
	Message string
	Options []string
	// Names are the person-facing names of every model the route mentions
	// (Model, Then, Alternative, Options), so a sentence about one never has
	// to show an id.
	Names map[string]string
}

// Name is the person-facing name of one model the route mentions.
func (r PictureRoute) Name(id string) string {
	if name := strings.TrimSpace(r.Names[id]); name != "" {
		return name
	}
	return idTail(id)
}

// Chosen says somebody named the model, so it may not be swapped.
func (r PictureRoute) Chosen() bool { return r.Model != "" && r.Scope != PictureScopeAutomatic }

// ResolvePicture routes one picture: the call's word, then this canvas's
// choice, then Everywhere, then the operator's pin, then Automatic.
//
// A CHOSEN MODEL IS CHECKED, NOT REPLACED. One the listing knows does not draw
// is refused; one that cannot read a photo is refused for an ask that changes
// one, naming a model that can; one the listing does not know is still sent,
// because the provider is the only one who can say no to a name a person typed.
func ResolvePicture(models *catalog.Catalog, profileDir string, ask PictureAsk) PictureRoute {
	route := resolvePicture(models, profileDir, ask)
	route.Names = map[string]string{}
	for _, id := range append(append([]string{route.Model, route.Alternative}, route.Then...), route.Options...) {
		if id != "" {
			route.Names[id] = PictureMakerName(models, id)
		}
	}
	return route
}

func resolvePicture(models *catalog.Catalog, profileDir string, ask PictureAsk) PictureRoute {
	ladder := ImageLadder(models, ask.NeedsPhotos)
	if word := strings.TrimSpace(ask.Word); word != "" {
		if models == nil {
			return PictureRoute{Reason: PictureUnconfirmed, Message: "The list of picture makers couldn't be read, so the model this call named couldn't be found."}
		}
		model, err := ResolveMediaModel(models, "image", word)
		if err != nil {
			return PictureRoute{Reason: PictureModelInvalid, Message: err.Error()}
		}
		if model != "" {
			return chosenPicture(models, model, PictureScopeCall, ask, ladder)
		}
	}
	if model, _ := CanvasPictureMakerAt(profileDir, ask.Workspace); model != "" {
		return chosenPicture(models, model, PictureScopeCanvas, ask, ladder)
	}
	if model := MediaSlotModelAt(profileDir, "image"); model != "" {
		return chosenPicture(models, model, PictureScopeEverywhere, ask, ladder)
	}
	if pin := strings.TrimSpace(ask.Pin); pin != "" {
		return chosenPicture(models, pin, PictureScopePin, ask, ladder)
	}
	return automaticPicture(models, ask, ladder)
}

func chosenPicture(models *catalog.Catalog, model, scope string, ask PictureAsk, ladder []string) PictureRoute {
	route := PictureRoute{Model: model, Scope: scope, Reason: string(RouteConfigured)}
	name := PictureMakerName(models, model)
	if models != nil {
		if row, known := models.Model(model); known {
			if !models.Supports(model, "output", "image") {
				return PictureRoute{Scope: scope, Reason: PictureNotAPictureMaker, Options: firstOf(ladder, 1),
					Message: fmt.Sprintf("%s doesn't make pictures. Choose a picture maker; no other model was used.", name)}
			}
			if ask.NeedsPhotos && len(row.InputModalities) > 0 && !readsPhotos(row) {
				refusal := PictureRoute{Scope: scope, Reason: PictureCannotReadPhotos, Options: firstOf(ladder, 1)}
				refusal.Message = fmt.Sprintf("%s can't read a photo, and you're changing one.", name)
				if len(ladder) > 0 {
					refusal.Message += fmt.Sprintf(" %s can; choose it for this, or start from words.", PictureMakerName(models, ladder[0]))
				} else {
					refusal.Message += " No connected picture maker reads photos."
				}
				return refusal
			}
		}
	}
	for _, other := range ladder {
		if other != model {
			route.Alternative = other
			break
		}
	}
	return route
}

func automaticPicture(models *catalog.Catalog, ask PictureAsk, ladder []string) PictureRoute {
	route := DefaultMediaRoute(models, "image")
	if len(ladder) > 0 {
		out := PictureRoute{Model: ladder[0], Scope: PictureScopeAutomatic, Reason: string(RouteListed), Then: firstOf(ladder[1:], maxPictureLadder-1)}
		if route.Model == ladder[0] {
			out.Reason = string(route.Reason)
		} else {
			// Only the photo filter moves the first rung off the default, and
			// the remembered list is still the first part of the ladder.
			for _, preferred := range curatedRoutes["image"] {
				if preferred == ladder[0] {
					out.Reason = string(RouteCurated)
				}
			}
		}
		return out
	}
	if ask.NeedsPhotos && route.Model != "" {
		return PictureRoute{Scope: PictureScopeAutomatic, Reason: PictureNoPhotoReader,
			Message: "No connected picture maker can read a photo, so this picture can't be changed. Start from words instead."}
	}
	out := PictureRoute{Scope: PictureScopeAutomatic, Reason: string(route.Reason), Options: route.Options}
	out.Message = PictureRefusalSentence(out.Reason)
	return out
}

// PictureRefusalSentence is the sentence for a route with no model that the
// person can act on without knowing anything about catalogues.
func PictureRefusalSentence(reason string) string {
	switch reason {
	case PictureNoneAdvertised:
		return "No connected provider makes pictures. Connect one in Settings → Models."
	case PictureUnconfirmed:
		return "The list of picture makers couldn't be read just now, so none was chosen. Try again in a moment, or choose a picture maker in Settings → Models."
	case PictureModelRequired:
		return "No picture maker has a published price, so Automatic won't choose one for you. Choose a picture maker in Settings → Models."
	}
	return ""
}

func firstOf(list []string, n int) []string {
	if len(list) > n {
		list = list[:n]
	}
	if len(list) == 0 {
		return nil
	}
	return append([]string(nil), list...)
}

// ── the two reaches ──────────────────────────────────────────────────────────

// EverywherePictureMaker is the picture maker chosen for everything, and the
// environment variable that set it when it was not the settings row (a
// variable outranks the row, so the desk must say where to change it).
func EverywherePictureMaker(profileDir string) (model, variable string) {
	if name, ok := modelSlotEnvDefault("image"); ok {
		if raw := strings.TrimSpace(env.Value(name)); raw != "" {
			return raw, name
		}
	}
	return MediaSlotModelAt(profileDir, "image"), ""
}

// SetEverywherePictureMaker writes the image slot's row; empty is Automatic,
// and takes the row out rather than writing a blank.
func SetEverywherePictureMaker(profileDir, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return writeProfileValues(profileDir, map[string]any{ModelSettingKey("image"): removeProfileKey})
	}
	return writeText(profileDir, ModelSettingKey("image"), model)
}

// canvasPicturesFile is the store of per-canvas picture makers, in the
// profile beside config.json. It is its own file because its keys are folders,
// which are not setting names.
const canvasPicturesFile = "canvas-pictures.json"

// maxCanvasPictureRows bounds the store: a desk with more canvases than this
// with their own picture maker is a store that is being written in a loop.
const maxCanvasPictureRows = 4096

var canvasPicturesMu sync.Mutex

type canvasPictures struct {
	Canvases map[string]string `json:"canvases"`
}

// ErrCanvasFolder is a canvas folder that is not an absolute path.
var ErrCanvasFolder = errors.New("a canvas is named by its working folder, as an absolute path")

func canvasFolder(folder string) (string, bool) {
	folder = strings.TrimSpace(folder)
	if folder == "" || !filepath.IsAbs(folder) {
		return "", false
	}
	return filepath.Clean(folder), true
}

func readCanvasPictures(profileDir string) canvasPictures {
	var store canvasPictures
	raw, err := os.ReadFile(filepath.Join(profileDir, canvasPicturesFile))
	if err == nil {
		_ = json.Unmarshal(raw, &store)
	}
	if store.Canvases == nil {
		store.Canvases = map[string]string{}
	}
	return store
}

// CanvasPictureMakerAt is the picture maker chosen for the canvas whose folder
// holds workspace (the folder itself or one above it), and that folder; empty
// when none was chosen there.
func CanvasPictureMakerAt(profileDir, workspace string) (model, folder string) {
	dir, ok := canvasFolder(workspace)
	if !ok || strings.TrimSpace(profileDir) == "" {
		return "", ""
	}
	store := readCanvasPictures(profileDir)
	if len(store.Canvases) == 0 {
		return "", ""
	}
	for {
		if model := strings.TrimSpace(store.Canvases[dir]); model != "" {
			return model, dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

// SetCanvasPictureMakerAt chooses the picture maker for the canvas at folder;
// empty gives the canvas back to Everywhere.
func SetCanvasPictureMakerAt(profileDir, folder, model string) error {
	dir, ok := canvasFolder(folder)
	if !ok {
		return ErrCanvasFolder
	}
	canvasPicturesMu.Lock()
	defer canvasPicturesMu.Unlock()
	store := readCanvasPictures(profileDir)
	if model = strings.TrimSpace(model); model == "" {
		delete(store.Canvases, dir)
	} else {
		if _, exists := store.Canvases[dir]; !exists && len(store.Canvases) >= maxCanvasPictureRows {
			return fmt.Errorf("too many canvases have their own picture maker (%d)", maxCanvasPictureRows)
		}
		store.Canvases[dir] = model
	}
	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(profileDir, canvasPicturesFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(append(encoded, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), filepath.Join(profileDir, canvasPicturesFile))
}
