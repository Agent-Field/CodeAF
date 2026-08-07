package exec

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	maxImageInputBytes   = 10 << 20
	defaultImageQuestion = "Describe this image precisely: subject, composition, any text verbatim, and anything that looks wrong or malformed."
)

type MediaProvider interface {
	GenerateImage(context.Context, provider.ImageRequest) (*provider.ImageResponse, error)
	Speak(context.Context, provider.SpeechRequest) (*provider.SpeechResponse, error)
	GenerateVideo(context.Context, provider.VideoRequest) (*provider.VideoResponse, error)
}

type DocumentProvider interface {
	ParseDocument(context.Context, provider.DocumentRequest) (*provider.DocumentResponse, error)
}

type ModalityCatalog interface {
	Supports(modelID, direction, modality string) bool
}

// MediaTools is the leaf-wide capability bundle. BeforeSpend is the same
// policy gate used before ordinary work launches; its amount is a catalog
// estimate when one is known, or zero for the standard gate path. Nil means no
// dollar rail.
type MediaTools struct {
	Provider    MediaProvider
	Catalog     ModalityCatalog
	ImageModel  string
	SpeechModel string
	MusicModel  string
	VideoModel  string
	VisionModel string
	// VisionClient is a direct completion client. The selected VisionModel is
	// applied per request so capability resolution can stay live at leaf start.
	VisionClient Completer
	// DocumentClient owns the explicit OpenRouter file-parser completion. It is
	// separate from ordinary model turns so every request names its cost rung.
	DocumentClient DocumentProvider
	// DocumentEngine is auto, local, free, or ocr. Empty is auto for embedders
	// that construct MediaTools directly.
	DocumentEngine string
	// VideoPrice is the catalog's fixed per-request price when advertised.
	// Zero means the catalog had no trustworthy estimate; the rail still runs.
	VideoPrice   float64
	WorkingModel string
	BeforeSpend  func(context.Context, float64) error
}

func (t *Toolbox) generateImage(ctx context.Context, args map[string]any) Result {
	if t.media == nil || t.media.Provider == nil {
		return errorf("image generation is not configured")
	}
	prompt := strings.TrimSpace(stringArg(args, "prompt"))
	if prompt == "" {
		return errorf("generate_image needs prompt")
	}
	n := intArg(args, "n", 1)
	if n < 1 || n > 10 {
		return errorf("generate_image n must be between 1 and 10")
	}
	model := strings.TrimSpace(t.media.ImageModel)
	if model == "" {
		return errorf("no image-generation model is available")
	}
	references := stringsArg(args, "reference_paths")
	encoded := make([]string, 0, len(references))
	for _, path := range references {
		dataURL, refusal := t.workspaceImageDataURL(path)
		if refusal != "" {
			return errorf("reference %s", refusal)
		}
		encoded = append(encoded, dataURL)
	}
	if t.media.BeforeSpend != nil {
		if err := t.media.BeforeSpend(ctx, 0); err != nil {
			return errorf("image generation paused at the daily budget — approve it in chat to continue")
		}
	}
	size := strings.TrimSpace(stringArg(args, "size"))
	request := provider.ImageRequest{
		Model: model, Prompt: prompt, N: n, OutputFormat: "png", InputReferences: encoded,
	}
	if strings.Contains(size, ":") {
		request.AspectRatio = size
	} else {
		request.Size = size
	}
	response, err := t.media.Provider.GenerateImage(ctx, request)
	if err != nil || response == nil || len(response.Data) == 0 {
		return errorf("image generation failed — try another prompt or image model")
	}
	decoded := make([][]byte, len(response.Data))
	exts := make([]string, len(response.Data))
	for index, image := range response.Data {
		bytes, decodeErr := base64.StdEncoding.DecodeString(image.Base64)
		if decodeErr != nil || len(bytes) == 0 {
			return errorf("image generation returned an unreadable image — try another image model")
		}
		decoded[index] = bytes
		exts[index] = extensionForMediaType(image.MediaType)
		if exts[index] == "" {
			exts[index] = ".png"
		}
	}
	paths := make([]string, 0, len(decoded))
	for index, data := range decoded {
		relative, full, openErr := t.nextMediaPath(prompt, index+1, exts[index])
		if openErr != nil {
			return errorf("could not save the generated image")
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return errorf("could not save the generated image")
		}
		t.workspace.Record(t.nodeID, full)
		paths = append(paths, relative)
	}
	lines := make([]string, 0, len(paths)+1)
	for _, path := range paths {
		lines = append(lines, "⌾ "+filepath.ToSlash(path))
	}
	lines = append(lines, fmt.Sprintf("Generated %d image(s) for %s.", len(paths), oneLine(prompt, 100)))
	return Result{Content: strings.Join(lines, "\n"), Usage: mediaUsage(response.Usage)}
}

func (t *Toolbox) speak(ctx context.Context, args map[string]any) Result {
	if t.media == nil || t.media.Provider == nil {
		return errorf("speech synthesis is not configured")
	}
	body := strings.TrimSpace(stringArg(args, "text"))
	if body == "" {
		return errorf("speak needs text")
	}
	model := strings.TrimSpace(t.media.SpeechModel)
	if model == "" {
		return errorf("no speech model is available")
	}
	if t.media.BeforeSpend != nil {
		if err := t.media.BeforeSpend(ctx, 0); err != nil {
			return errorf("speech synthesis paused at the daily budget — approve it in chat to continue")
		}
	}
	voice := strings.TrimSpace(stringArg(args, "voice"))
	if voice == "" {
		voice = "alloy"
	}
	response, err := t.media.Provider.Speak(ctx, provider.SpeechRequest{
		Model: model, Input: body, Voice: voice, ResponseFormat: "mp3",
	})
	if err != nil || response == nil || len(response.Audio) == 0 {
		return errorf("speech synthesis failed — try another voice or speech model")
	}
	relative, full, pathErr := t.nextMediaPath(body, 0, ".mp3")
	if pathErr != nil || os.WriteFile(full, response.Audio, 0o644) != nil {
		return errorf("could not save synthesized speech")
	}
	t.workspace.Record(t.nodeID, full)
	return Result{
		Content: "♪ " + filepath.ToSlash(relative) + "\nSynthesized speech for " + oneLine(body, 100) + ".",
		Usage:   mediaUsage(response.Usage),
	}
}

func (t *Toolbox) generateMusic(ctx context.Context, args map[string]any) Result {
	if t.media == nil || t.media.Provider == nil {
		return errorf("music generation is not configured")
	}
	prompt := strings.TrimSpace(stringArg(args, "prompt"))
	if prompt == "" {
		return errorf("generate_music needs prompt")
	}
	format := strings.ToLower(strings.TrimSpace(stringArg(args, "format")))
	if format == "" {
		format = "mp3"
	}
	if format != "mp3" {
		return errorf("generate_music format must be mp3")
	}
	model := strings.TrimSpace(t.media.MusicModel)
	if model == "" {
		return errorf("no music-generation model is available")
	}
	if t.media.BeforeSpend != nil {
		if err := t.media.BeforeSpend(ctx, 0); err != nil {
			return errorf("music generation paused at the daily budget — approve it in chat to continue")
		}
	}
	response, err := t.media.Provider.Speak(ctx, provider.SpeechRequest{
		Model: model, Input: prompt, ResponseFormat: format,
	})
	if err != nil || response == nil || len(response.Audio) == 0 {
		return errorf("music generation failed — try another prompt or music model")
	}
	relative, full, pathErr := t.nextMediaPath(prompt, 0, ".mp3")
	if pathErr != nil || os.WriteFile(full, response.Audio, 0o644) != nil {
		return errorf("could not save the generated music")
	}
	t.workspace.Record(t.nodeID, full)
	return Result{
		Content: "♪ " + filepath.ToSlash(relative) + "\nGenerated music for " + oneLine(prompt, 100) + ".",
		Usage:   mediaUsage(response.Usage),
	}
}

func (t *Toolbox) generateVideo(ctx context.Context, args map[string]any) Result {
	if t.media == nil || t.media.Provider == nil {
		return errorf("video generation is not configured")
	}
	prompt := strings.TrimSpace(stringArg(args, "prompt"))
	if prompt == "" {
		return errorf("generate_video needs prompt")
	}
	duration := intArg(args, "duration", 0)
	if duration < 0 {
		return errorf("generate_video duration must be a positive number of seconds")
	}
	model := strings.TrimSpace(t.media.VideoModel)
	if model == "" {
		return errorf("no video-generation model is available")
	}

	references := stringsArg(args, "reference_paths")
	frames := make([]provider.VideoImageReference, 0, min(2, len(references)))
	styles := make([]provider.VideoImageReference, 0, max(0, len(references)-2))
	for index, path := range references {
		dataURL, refusal := t.workspaceImageDataURL(path)
		if refusal != "" {
			return errorf("reference %s", refusal)
		}
		reference := provider.VideoImageReference{
			Type: "image_url", ImageURL: provider.VideoImageURL{URL: dataURL},
		}
		switch index {
		case 0:
			reference.FrameType = "first_frame"
			frames = append(frames, reference)
		case 1:
			reference.FrameType = "last_frame"
			frames = append(frames, reference)
		default:
			styles = append(styles, reference)
		}
	}
	if t.media.BeforeSpend != nil {
		if err := t.media.BeforeSpend(ctx, t.media.VideoPrice); err != nil {
			return errorf("video generation paused at the daily budget — approve it in chat to continue")
		}
	}
	response, err := t.media.Provider.GenerateVideo(ctx, provider.VideoRequest{
		Model: model, Prompt: prompt, Duration: duration,
		Resolution:  strings.TrimSpace(stringArg(args, "resolution")),
		AspectRatio: strings.TrimSpace(stringArg(args, "aspect_ratio")),
		FrameImages: frames, InputReferences: styles,
	})
	if err != nil {
		if errors.Is(err, provider.ErrVideoTimeout) {
			return errorf("video generation timed out — no video was saved")
		}
		return errorf("video generation failed — %s", oneLine(err.Error(), 180))
	}
	if response == nil || len(response.Video) == 0 {
		return errorf("video generation failed — the completed job returned no video")
	}
	relative, full, pathErr := t.nextMediaPath(prompt, 0, ".mp4")
	if pathErr != nil || os.WriteFile(full, response.Video, 0o644) != nil {
		return errorf("could not save the generated video")
	}
	t.workspace.Record(t.nodeID, full)
	return Result{
		Content: "▶ " + filepath.ToSlash(relative) + "\nGenerated video for " + oneLine(prompt, 100) + ".",
		Usage:   mediaUsage(response.Usage),
	}
}

func (t *Toolbox) viewImage(ctx context.Context, args map[string]any) Result {
	if t.media == nil || t.media.Catalog == nil {
		return errorf("image inspection is not configured")
	}
	model := strings.TrimSpace(t.media.WorkingModel)
	if call := provider.CallFrom(ctx); call != nil && strings.TrimSpace(call.Model()) != "" {
		model = call.Model()
	}
	workingModelSees := t.media.Catalog.Supports(model, "input", "image")
	visionModel := strings.TrimSpace(t.media.VisionModel)
	if !workingModelSees && visionModel == "" {
		return errorf("%s can't see images — continue with file metadata or use a vision model; no vision model available", model)
	}
	if !workingModelSees && t.media.VisionClient == nil {
		return errorf("image inspection is not configured")
	}
	path := strings.TrimSpace(stringArg(args, "path"))
	if path == "" {
		return errorf("view_image needs path")
	}
	dataURL, refusal := t.workspaceImageDataURL(path)
	if refusal != "" {
		return errorf("%s", refusal)
	}
	targetedQuestion := strings.TrimSpace(stringArg(args, "question"))
	if workingModelSees {
		label := "Image from " + filepath.ToSlash(path) + ":"
		if targetedQuestion != "" {
			label += "\nQuestion: " + targetedQuestion
		}
		return Result{
			Content: "⌾ " + filepath.ToSlash(path) + "\nImage loaded for the next turn.",
			Followup: []ai.ContentPart{
				{Type: "text", Text: label},
				{Type: "image_url", ImageURL: &ai.ImageURLData{URL: dataURL}},
			},
		}
	}
	question := targetedQuestion
	if question == "" {
		question = defaultImageQuestion
	}
	if t.media.BeforeSpend != nil {
		if err := t.media.BeforeSpend(ctx, 0); err != nil {
			return errorf("image inspection paused at the daily budget — approve it in chat to continue")
		}
	}
	response, err := t.media.VisionClient.CompleteWithMessages(ctx, []ai.Message{{
		Role: "user",
		Content: []ai.ContentPart{
			{Type: "text", Text: question},
			{Type: "image_url", ImageURL: &ai.ImageURLData{URL: dataURL}},
		},
	}}, ai.WithModel(visionModel))
	if err != nil || response == nil {
		return errorf("image inspection failed — try another question or vision model")
	}
	answer := strings.TrimSpace(response.Text())
	if answer == "" {
		return errorf("image inspection failed — the vision model returned no description")
	}
	return Result{
		Content: "seen by " + visionModelShort(visionModel) + ": " + answer,
		Usage:   mediaUsage(response.Usage),
	}
}

func visionModelShort(model string) string {
	model = strings.TrimPrefix(strings.TrimSpace(model), "~")
	if index := strings.LastIndex(model, "/"); index >= 0 && index+1 < len(model) {
		model = model[index+1:]
	}
	return model
}

func (t *Toolbox) workspaceImageDataURL(path string) (string, string) {
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return "", err.Error()
	}
	ext := strings.ToLower(filepath.Ext(full))
	mediaType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif"}[ext]
	if mediaType == "" {
		return "", "only png, jpeg, webp, and gif images can be viewed"
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return "", "could not read " + filepath.ToSlash(path)
	}
	if info.Size() > maxImageInputBytes {
		return "", fmt.Sprintf("%s is over the 10 MB image limit", filepath.ToSlash(path))
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", "could not read " + filepath.ToSlash(path)
	}
	if len(data) > maxImageInputBytes {
		return "", fmt.Sprintf("%s is over the 10 MB image limit", filepath.ToSlash(path))
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), ""
}

func imageParts(paths []string) []ai.ContentPart {
	parts := make([]ai.ContentPart, 0, len(paths)*2)
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(path))
		mediaType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif"}[ext]
		if mediaType == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() > maxImageInputBytes {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxImageInputBytes {
			continue
		}
		parts = append(parts,
			ai.ContentPart{Type: "text", Text: "Attached image " + filepath.Base(path) + ":"},
			ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)}},
		)
	}
	return parts
}

var mediaSlugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// MediaSlug is exported for deterministic filename tests and integrations
// that want to preview where an artifact will land.
func MediaSlug(prompt string) string {
	slug := strings.Trim(mediaSlugUnsafe.ReplaceAllString(strings.ToLower(prompt), "-"), "-")
	if slug == "" {
		slug = "media"
	}
	if len(slug) > 56 {
		slug = strings.Trim(slug[:56], "-")
	}
	return slug
}

func (t *Toolbox) nextMediaPath(source string, index int, extension string) (string, string, error) {
	base := MediaSlug(source)
	if index > 0 {
		base += fmt.Sprintf("-%d", index)
	}
	for collision := 0; collision < 1000; collision++ {
		name := base
		if collision > 0 {
			name += fmt.Sprintf("-%d", collision+1)
		}
		relative := filepath.Join("media", name+extension)
		full, err := t.workspace.Resolve(relative)
		if err != nil {
			return "", "", err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", "", err
		}
		file, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				return "", "", closeErr
			}
			return relative, full, nil
		}
		if !os.IsExist(err) {
			return "", "", err
		}
	}
	return "", "", fmt.Errorf("too many media files share this name")
}

func mediaUsage(usage *ai.Usage) Usage {
	recorded := Usage{Calls: 1}
	if usage == nil {
		return recorded
	}
	recorded.PromptTokens = usage.PromptTokens
	recorded.CompletionTokens = usage.CompletionTokens
	recorded.CachedTokens = usage.CacheReadTokens()
	if usage.Cost != nil {
		recorded.Cost = *usage.Cost
	}
	return recorded
}

func extensionForMediaType(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > limit {
		return text[:limit-1] + "…"
	}
	return text
}
