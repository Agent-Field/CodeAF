package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
)

// This file is the naming half of the model palette's candidacy seam. Slots
// answer "which models may occupy this role"; the functions here answer "which
// of them did the user mean" when they said gemini, opus, or best.

const (
	// BestModelWord is the one spelling that means "the strongest advertised
	// model for this modality", resolved from the catalog at call time.
	BestModelWord = "best"

	// Match tiers, best first. The score inside a tier is a length penalty, so
	// the shortest, most canonical slug leads.
	matchExact  = 0
	matchPrefix = 1
	matchInside = 2
)

// bestMediaPreferences documents the quality order per modality: strongest
// advertised model first. "best" walks the list and takes the first the
// catalog actually advertises. When none of them is advertised it falls back
// to the most expensive advertised model of that modality, because price is
// the only quality signal a catalog row carries.
var bestMediaPreferences = map[string][]string{
	"image": {"google/gemini-3-pro-image", "openai/gpt-image-1.5", preferredImageModel},
	// The three generation slots lead with the name the same slot's resolver
	// leads with, so "best" and an unset row cannot disagree about what this
	// build thinks the good model is. Underneath each of them the older order
	// is kept exactly as it stood.
	"speech": {preferredSpeechModel, hostedSpeechModel, kokoroSpeechModel},
	"music":  {preferredMusicModel, "google/lyria-3", fallbackMusicModel},
	"video":  {preferredVideoModel, "google/veo-3.5", fallbackVideoModel},
	// The four PERCEPTION words have preference orders too, and they are read
	// by [CandidateMediaModel] rather than by [BestMediaModel]: nobody asks a
	// tool argument for "the best pair of eyes", and the price tie-break the
	// generation slots fall back on is exactly wrong here — the most expensive
	// model that can see is not the one a fallback should quietly choose.
	"vision":     {preferredVisionModel, fallbackVisionModel},
	"voice":      {DefaultVoiceModel},
	"transcribe": {DefaultVoiceModel},
	"listen":     {preferredPerceptionModel},
	"watch":      {preferredPerceptionModel},
}

// curatedMediaModels is the LAST RUNG of the use-time resolver: the name this
// build remembers for a modality, for a machine whose catalog has told it
// nothing (docs/MULTIMODAL.md Decision 5).
//
// Every one of these is still capability-checked by the resolver before it is
// used, and that check survives a cold catalog because internal/catalog's own
// offline fallbacks publish exactly these capabilities for exactly these ids.
// Vision and the two perception words are the deliberate exceptions on a cold
// machine — no offline row publishes image, audio or video INPUT with text back
// — so those degrade to nothing rather than to a name nobody can vouch for,
// which is the same posture ResolveVisionModel has always kept.
var curatedMediaModels = map[string]string{
	"image":      preferredImageModel,
	"speech":     preferredSpeechModel,
	"music":      preferredMusicModel,
	"video":      preferredVideoModel,
	"vision":     preferredVisionModel,
	"voice":      DefaultVoiceModel,
	"transcribe": DefaultVoiceModel,
	"listen":     preferredPerceptionModel,
	"watch":      preferredPerceptionModel,
}

// CandidateMediaModel is the CATALOG rung of the use-time resolver: the best
// model the catalog advertises for one modality, by the documented preference
// order and then by the catalog's own order.
//
// It is deliberately not [BestMediaModel]. That one answers a person who typed
// "best" and falls back to the most expensive advertised row, because price is
// the only quality signal a catalog row carries and somebody who asked for the
// best has asked to be spent on. A fallback nobody asked for must not reach for
// the most expensive thing on the shelf.
func CandidateMediaModel(models *catalog.Catalog, modality string) string {
	modality = strings.ToLower(strings.TrimSpace(modality))
	candidates := ModelCandidates(models, modality)
	if len(candidates) == 0 {
		return ""
	}
	for _, preferred := range bestMediaPreferences[modality] {
		for _, candidate := range candidates {
			if candidate.ID == preferred {
				return candidate.ID
			}
		}
	}
	// The preference list missed — every curated name has left the catalog —
	// and the election falls to catalog order, which is newest-first. Newest
	// is where the experiments live: rows named -exp, -preview, :free, or a
	// stealth vendor, whose endpoints are often gated behind a data-policy
	// opt-in most accounts have not made, so a fallback nobody chose would
	// land every call on a 404 about privacy settings. A row that advertises
	// itself as provisional is passed over while any settled row exists; when
	// the whole modality is experiments, the first one is still the answer.
	for _, candidate := range candidates {
		if !provisionalModelID(candidate.ID) {
			return candidate.ID
		}
	}
	return candidates[0].ID
}

// provisionalModelID reads the markers vendors put in a slug to say "do not
// depend on this": experimental and preview suffixes, the free tier, alpha and
// beta tags, and the stealth vendor whose whole catalog is auditions. It is a
// PREFERENCE among capable rows and never a capability claim — the catalog
// stays the only authority on what a model can do; this only decides which of
// several equally capable rows a fallback nobody chose should reach for first.
func provisionalModelID(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	if strings.HasPrefix(id, "stealth/") {
		return true
	}
	if strings.HasSuffix(id, ":free") {
		return true
	}
	for _, token := range modelTokens(id) {
		switch token {
		case "exp", "experimental", "preview", "alpha", "beta":
			return true
		}
	}
	return false
}

// FallbackMediaModel is the curated rung, and empty for a modality this build
// has no remembered name for.
func FallbackMediaModel(modality string) string {
	return curatedMediaModels[strings.ToLower(strings.TrimSpace(modality))]
}

type scoredModel struct {
	id    string
	tier  int
	score int
}

// ModelMatches returns the models in slot that word could mean, best first and
// at most limit of them. Exactly one result is an unambiguous resolution; more
// than one means the word was genuinely ambiguous and the caller should ask.
// Scoring is contains-and-prefix on purpose: subsequence fuzziness is fine for
// a palette a human is watching, and far too loose for a word lifted out of a
// sentence.
func ModelMatches(models *catalog.Catalog, slot, word string, limit int) []string {
	word = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(word), "~")))
	if models == nil || word == "" || limit <= 0 {
		return nil
	}
	scored := make([]scoredModel, 0, 8)
	for _, candidate := range ModelCandidates(models, slot) {
		if tier, score, ok := modelWordScore(candidate, word); ok {
			scored = append(scored, scoredModel{id: candidate.ID, tier: tier, score: score})
		}
	}
	if len(scored) == 0 {
		return nil
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].tier != scored[j].tier {
			return scored[i].tier < scored[j].tier
		}
		if scored[i].score != scored[j].score {
			return scored[i].score < scored[j].score
		}
		return scored[i].id < scored[j].id
	})
	best := make([]string, 0, limit)
	for _, candidate := range scored {
		if candidate.tier != scored[0].tier || len(best) == limit {
			break
		}
		best = append(best, candidate.id)
	}
	// An exact hit is never ambiguous, whatever else shares its tier.
	if scored[0].tier == matchExact {
		return best[:1]
	}
	return best
}

func modelWordScore(model catalog.Model, word string) (int, int, bool) {
	id := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model.ID), "~"))
	base := id
	if index := strings.LastIndex(id, "/"); index >= 0 {
		base = id[index+1:]
	}
	switch {
	case id == word || base == word:
		return matchExact, 0, true
	case tokensLeadBase(base, word):
		return matchPrefix, len(base) - len(word), true
	case tokensAlignInside(id, word):
		return matchInside, strings.Index(id, word) + len(id) - len(word), true
	default:
		return 0, 0, false
	}
}

// modelTokens splits an id or a person's word on the separators model slugs
// actually use. A "word" here may itself be several tokens — "command-r",
// "deepseek-v4" — and alignment is judged token-sequence to token-sequence.
func modelTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune("/-._ :@", r)
	})
}

// tokensLeadBase says the word's tokens are the leading whole tokens of the
// base name: "kimi" leads "kimi-k2", "command-r" leads "command-r-08-2024".
// A raw HasPrefix here once turned the word "comma" into a four-way Cohere
// menu, because "comma" is the first five letters of "command" — the official
// GAIA answer template says "comma separated list" and every question died at
// compile. Letters are not names: a word matches whole tokens or not at all.
func tokensLeadBase(base, word string) bool {
	baseTokens, wordTokens := modelTokens(base), modelTokens(word)
	if len(wordTokens) == 0 || len(wordTokens) > len(baseTokens) {
		return false
	}
	for i, token := range wordTokens {
		if baseTokens[i] != token {
			return false
		}
	}
	return true
}

// tokensAlignInside says the word's tokens appear as consecutive whole tokens
// somewhere in the id: "sonnet" inside "claude-sonnet-4", never "net".
func tokensAlignInside(id, word string) bool {
	idTokens, wordTokens := modelTokens(id), modelTokens(word)
	if len(wordTokens) == 0 || len(wordTokens) > len(idTokens) {
		return false
	}
	for start := 0; start+len(wordTokens) <= len(idTokens); start++ {
		matched := true
		for i, token := range wordTokens {
			if idTokens[start+i] != token {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// BestMediaModel resolves the documented preference order for one modality
// against what the catalog advertises right now.
func BestMediaModel(models *catalog.Catalog, modality string) string {
	modality = strings.ToLower(strings.TrimSpace(modality))
	candidates := ModelCandidates(models, modality)
	if len(candidates) == 0 {
		return ""
	}
	for _, preferred := range bestMediaPreferences[modality] {
		for _, candidate := range candidates {
			if candidate.ID == preferred {
				return candidate.ID
			}
		}
	}
	best, price := candidates[0].ID, modelPrice(candidates[0])
	for _, candidate := range candidates[1:] {
		if candidatePrice := modelPrice(candidate); candidatePrice > price {
			best, price = candidate.ID, candidatePrice
		}
	}
	return best
}

func modelPrice(model catalog.Model) float64 {
	price := model.RequestPrice
	if model.CompletionPrice > price {
		price = model.CompletionPrice
	}
	if model.PromptPrice > price {
		price = model.PromptPrice
	}
	return price
}

// ResolveMediaModel reads one media tool's model argument. An empty word means
// the caller keeps its slot default. "best" resolves the preference order. Any
// other word is resolved inside the modality, and a name that belongs to a
// different modality is refused by naming what it actually makes.
func ResolveMediaModel(models *catalog.Catalog, modality, word string) (string, error) {
	modality = strings.ToLower(strings.TrimSpace(modality))
	word = strings.TrimSpace(word)
	if word == "" {
		return "", nil
	}
	if strings.EqualFold(word, BestModelWord) {
		if best := BestMediaModel(models, modality); best != "" {
			return best, nil
		}
		return "", fmt.Errorf("no %s model is advertised right now", modality)
	}
	if matches := ModelMatches(models, modality, word, 1); len(matches) > 0 {
		return matches[0], nil
	}
	if other, elsewhere, ok := modelInAnotherModality(models, modality, word); ok {
		return "", fmt.Errorf("%s makes %s, not %s", other, elsewhere, modality)
	}
	return "", fmt.Errorf("no %s model matches %q", modality, word)
}

// mediaModalities is the search order for a wrong-modality refusal. It is only
// used to explain a mistake, never to substitute a model.
var mediaModalities = []string{"image", "speech", "music", "video", "voice"}

func modelInAnotherModality(models *catalog.Catalog, modality, word string) (string, string, bool) {
	for _, other := range mediaModalities {
		if other == modality {
			continue
		}
		if matches := ModelMatches(models, other, word, 1); len(matches) > 0 {
			return matches[0], other, true
		}
	}
	return "", "", false
}
