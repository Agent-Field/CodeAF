package main

// chatv3_media.go is Decision 5 of docs/MULTIMODAL.md's v3 revision: ONE KNOB
// PER MODALITY, resolved AT USE TIME.
//
// The double-knob era is what this closes. The settings sheet carried a row for
// the drawing model that nothing read, beside a role pin that the engine obeyed
// and no surface showed; the looking row and roles.vision were two names for
// one question and disagreed on a fresh profile. So there is one resolver, it
// is asked at the moment something draws rather than at boot, and it walks one
// documented ladder:
//
//	1. THE SETTINGS SLOT — the row a person actually opened and chose, read
//	   from the profile at the moment of the call (config.MediaSlotModelAt, and
//	   config.VisionModelAt for looking). A choice made in the sheet is live in
//	   the running session, not on the next launch.
//	2. THE ROLE PIN — roles.<name>, the operator's free-text second rung.
//	3. THE CATALOG — the best model the catalog advertises publishing the
//	   capability (config.CandidateMediaModel).
//	4. THE CURATED NAME — what this build remembers (config.FallbackMediaModel),
//	   which is what makes a machine that has never opened settings still draw,
//	   speak and film out of the box.
//
// EVERY RUNG IS CAPABILITY-CHECKED against the catalog's published modalities.
// A slot or a pin naming a model that cannot do the job is passed over with one
// log line and the ladder carries on, so a renamed slug degrades to the best
// available model instead of arriving at a provider as a 404. This is the
// promise Decision 3 made and the first wiring never kept.
//
// THE SEVEN WORDS the resolver answers to, and what each one asks the catalog:
//
//	image       output image     — the model that draws
//	speech      output speech    — the model that speaks (the catalog aliases
//	                               audio and music onto this)
//	video       output video     — the model that films
//	vision      input image      — the model that looks at a picture and
//	                               answers in words
//	transcribe  input audio      — the ear: sound in, words back, on the
//	                               dedicated transcription endpoint
//	listen      input audio      — a CHAT model that can be handed a sound and
//	                               talk about it
//	watch       input video      — a CHAT model that can be handed a film
//
// The first four are session.Config.MediaModel's own contract; the last three
// are the perception belt's (the media/hear lane). The four INPUT words all
// demand text back as well, because a model that takes sound and answers in
// sound is a speaker rather than a listener.

import (
	"log"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// v3MediaSlot is the settings slot each modality's first rung reads. Vision is
// the one that is not a capability slot: "looking" has always been its own
// registry row (config.KeyVisionModel), and UNIFYING THE DOUBLE KNOB means the
// resolver reads exactly that row rather than inventing a sixth slot beside it.
// The three perception words share the voice slot or have no slot at all, which
// is stated by their absence here.
var v3MediaSlot = map[string]string{
	"image":      "image",
	"speech":     "speech",
	"video":      "video",
	"transcribe": "voice",
}

// v3MediaPin is the role whose pin is the second rung. Three of the seven have
// one; the perception words have none yet, and a pin nobody can write is a rung
// that would only ever be skipped.
var v3MediaPin = map[string]roles.Role{
	"image":  roles.RoleImageGen,
	"speech": roles.RoleSpeech,
	"video":  roles.RoleVideo,
	"vision": roles.RoleVision,
}

// v3MediaVerb is what the modality is called in a log line — the plain word,
// because a line a person reads while wondering why their pin was ignored is a
// person-facing string.
var v3MediaVerb = map[string]string{
	"image":      "draw",
	"speech":     "speak",
	"video":      "film",
	"vision":     "see",
	"transcribe": "transcribe",
	"listen":     "listen",
	"watch":      "watch",
}

// v3MediaModel is session.Config.MediaModel: one modality word in, one slug
// out, or "" when this install has no capable model for it at all.
//
// The empty answer is load-bearing and never a failure — session.Config states
// the law: a modality with no model keeps that modality's tools OFF THE BELT,
// so the model does not have a verb it would only be refused on (CLAUDE.md's
// absent-not-broken law).
//
// IT MAY WAIT, and that is the one thing it does differently from every other
// closure this door hands the session. The catalog questions on the message
// path — the vision gate, the fallback chain — are answered from rows already
// in memory because somebody is watching a turn. This one is asked when a tool
// is about to spend ten seconds generating a picture, and on a cold cache the
// honest choice is a fifteen-second fetch that resolves a capable model over an
// instant answer of "nothing can draw here".
//
// profileDir and source are the two rungs the catalog cannot answer, and they
// are read at CALL time rather than closed over as values: a person who picks a
// drawing model in the settings sheet expects the next picture to come from it,
// not the next launch.
func v3MediaModel(models *catalog.Catalog, profileDir string, source roles.Source) func(string) string {
	return func(modality string) string {
		modality = strings.ToLower(strings.TrimSpace(modality))
		if models == nil || v3MediaVerb[modality] == "" {
			return ""
		}
		// able is one rung: a name, checked, and either taken or passed over
		// out loud. The log line names the rung because the three rungs fail
		// for different reasons — a slot is a person's own stale choice, a pin
		// is an operator's, and a curated name is this build being out of date.
		able := func(rung, id string) string {
			if id = strings.TrimSpace(id); id == "" {
				return ""
			}
			if !v3MediaCapable(models, modality, id) {
				log.Printf("media: the %s %s names %s, which cannot %s — passing over it",
					modality, rung, id, v3MediaVerb[modality])
				return ""
			}
			return id
		}

		if slot, ok := v3MediaSlot[modality]; ok {
			if id := able("slot", config.MediaSlotModelAt(profileDir, slot)); id != "" {
				return id
			}
		}
		if modality == "vision" {
			// The looking row, which is the vision slot under its own name.
			if id := able("slot", config.VisionModelAt(profileDir)); id != "" {
				return id
			}
		}
		if role, pinnable := v3MediaPin[modality]; pinnable {
			if pinned, set := roles.Pinned(source, role); set {
				if id := able("pin", pinned); id != "" {
					return id
				}
			}
		}
		if id := able("catalog", config.CandidateMediaModel(models, modality)); id != "" {
			return id
		}
		return able("fallback", config.FallbackMediaModel(modality))
	}
}

// v3MediaClient is session.Config.Media: the one client that reaches every
// generation endpoint, or nil when this install cannot build one.
//
// THE TWO-LINE DANCE IS GO'S AND NOT A CHOICE, the same one v3Connections does
// and for the same reason: a nil *provider.MediaClient assigned straight into
// the interface field is a NON-nil interface holding nothing, and session's
// absence law is a plain nil check. Without it, a build that could not make a
// client would put every generation tool on the belt and fail each one on its
// first call — the belt that lies, which is the thing the law exists to stop.
func v3MediaClient(settings config.Config) session.MediaGenerator {
	client, err := settings.MediaClient()
	if err != nil || client == nil {
		if err != nil {
			log.Printf("media: no generation endpoint on this install: %v", err)
		}
		return nil
	}
	return client
}

// v3MediaCapable is the published-facts test one modality applies to one slug.
//
// It asks the CATALOG and nothing else — no id patterns, no vendor guesses —
// for the reason the vision gate does: a model that says it draws draws, and
// anything else is a "no" this door can defend to somebody whose pin was
// ignored. A slug the catalog has never heard of fails every question here,
// which is the honest reading of "nobody can vouch for this".
func v3MediaCapable(models *catalog.Catalog, modality, id string) bool {
	switch modality {
	case "image":
		return models.Supports(id, "output", "image")
	case "speech":
		return models.Supports(id, "output", "speech")
	case "video":
		return models.Supports(id, "output", "video")
	case "vision":
		return models.Supports(id, "input", "image") && v3MediaAnswersInWords(models, id)
	case "transcribe":
		// The ear is the one input word that does NOT demand a conversation
		// back: a transcription row answers in text, or in the "transcription"
		// modality a provider that uses that vocabulary publishes, and neither
		// of them is a model you can talk to.
		return models.Supports(id, "input", "audio") &&
			(models.Supports(id, "output", "text") || models.Supports(id, "output", "transcription"))
	case "listen":
		return models.Supports(id, "input", "audio") && v3MediaAnswersInWords(models, id)
	case "watch":
		return models.Supports(id, "input", "video") && v3MediaAnswersInWords(models, id)
	}
	return false
}

// v3MediaAnswersInWords is the second half of every perception question: the
// model has to answer in text AND IN NOTHING ELSE. A drawing model that captions
// what it paints publishes ["image","text"] out and would sail through a rule
// that only asked whether text was in the list — and it cannot answer "what is
// in this photo", which is the only thing a perception slot is ever asked.
func v3MediaAnswersInWords(models *catalog.Catalog, id string) bool {
	row, known := models.Model(id)
	if !known {
		return false
	}
	return v3AnswersText(row.OutputModalities) && len(row.OutputModalities) > 0
}
