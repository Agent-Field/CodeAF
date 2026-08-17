package session

// tools_media.go is what the three MAKING verbs share — generate_image
// (tools_image.go), speak (tools_speak.go) and generate_video (tools_video.go).
//
// Each of them asks the same three questions and must answer them the same way,
// which is why the answers are here and not copied into three files:
//
//   - IS THIS VERB ON THE BELT AT ALL? [Agent.mediaHand] is the absence law in
//     one call. A nil Media is no hand; a resolver that answers "" for the
//     modality is a hand with nothing to ask for. Either way the tool is left
//     OFF rather than added and made to refuse (tools_search.go states the law
//     at length).
//
//   - WHAT DOES A REFERENCE PATH MEAN? A picture the model points at is read off
//     the disk and encoded as a data URL, exactly as internal/exec/media.go's
//     leaf tools do it, with the same five formats and the same 10MB ceiling the
//     rest of this binary enforces (image.go). One vocabulary for "which
//     pictures can be used", wherever a picture is handed in.
//
//   - WHERE DO THE BYTES LAND? [Agent.mediaDestination] is one collision-safe
//     naming rule for every modality: a path the model gave, or a timestamp, a
//     slug of the prompt, and a suffix if that name is already taken.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// mediaStampFormat is the sortable half of a generated file's name, and
// mediaSlugWords / mediaSlugLimit bound the readable half. Seconds are enough
// resolution because the slug and the collision suffix carry the rest; the
// point of the stamp is that `ls` reads in the order the files were made.
const (
	mediaStampFormat = "20060102-150405"
	mediaSlugWords   = 6
	mediaSlugLimit   = 48
)

// mediaHand answers the two things a generation verb needs before it can exist:
// the client that carries the request, and the model the request names.
//
// The bool is the belt's answer. It is false when the surface wired no media
// client, when it wired no resolver, and when the resolver says this machine has
// no model for this modality — three different absences that mean exactly one
// thing to the model, which is that it does not have the verb.
func (a *Agent) mediaHand(modality string) (MediaGenerator, string, bool) {
	client := a.config.Media
	if client == nil || a.config.MediaModel == nil {
		return nil, "", false
	}
	model := strings.TrimSpace(a.config.MediaModel(modality))
	if model == "" {
		return nil, "", false
	}
	return client, model, true
}

// mediaReference reads one picture the model pointed at and encodes it as the
// data URL the wire carries. The second string is a REFUSAL in the model's own
// terms — the path it named and what was wrong with it — and is empty on
// success.
//
// The path is resolved the way every other path on this belt is (tools_pdf.go's
// [resolveInWorkspace]): relative to the workspace, with ~ and a leading @
// understood, absolute taken as given. The size is checked against the stat
// BEFORE the read, for image.go's reason: a limit enforced after the read has
// already pulled the file into memory to discover it was too big.
func (a *Agent) mediaReference(path string) (string, string) {
	named := filepath.ToSlash(strings.TrimSpace(path))
	if named == "" {
		return "", "a reference path is empty"
	}
	full := resolveInWorkspace(path, a.config.Workspace)
	mediaType := imageMediaTypes[strings.ToLower(filepath.Ext(full))]
	if mediaType == "" {
		return "", named + " is not a picture that can be used as a reference — png, jpeg, webp and gif are"
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return "", "could not read " + named
	}
	if info.Size() > maxImageBytes {
		return "", fmt.Sprintf("%s is over the %s image limit", named, byteLimit(maxImageBytes))
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", "could not read " + named
	}
	// Checked again: the file could have grown between the stat and the read.
	if len(data) > maxImageBytes {
		return "", fmt.Sprintf("%s is over the %s image limit", named, byteLimit(maxImageBytes))
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), ""
}

// mediaReferences resolves a whole list and stops at the FIRST one it cannot
// read. A partial set is worse than a refusal: a model that asked for three
// references and silently got two would be told its picture combined three
// things when it combined two.
func (a *Agent) mediaReferences(paths []string) ([]string, string) {
	if len(paths) == 0 {
		return nil, ""
	}
	encoded := make([]string, 0, len(paths))
	for _, path := range paths {
		dataURL, refusal := a.mediaReference(path)
		if refusal != "" {
			return nil, refusal
		}
		encoded = append(encoded, dataURL)
	}
	return encoded, ""
}

// mediaDestination is the absolute path a generated file is about to be written
// to, for any modality.
//
// A path the model gave is taken as given — relative to the workspace, the way
// every other tool on this belt resolves one, and overwritten if it exists the
// way write does. Only its extension is corrected, because a name with none is a
// file nothing on the machine will open by double-clicking it.
//
// A path the model did not give is derived: the directory, the timestamp, the
// prompt's slug, and a suffix if a file of that name is already there. The
// collision loop matters more than it looks — two files of the same prompt in
// the same second is what "make me three of these" produces, and the third one
// silently overwriting the second would lose work nobody asked to lose.
func (a *Agent) mediaDestination(asked, prompt, extension, directory string) (string, error) {
	if asked = strings.TrimSpace(asked); asked != "" {
		path := asked
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.config.Workspace, path)
		}
		path = filepath.Clean(path)
		if filepath.Ext(path) == "" {
			path += extension
		}
		return path, nil
	}

	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	base := time.Now().Format(mediaStampFormat)
	if slug := mediaSlug(prompt); slug != "" {
		base += "-" + slug
	}
	for collision := 0; collision < 1000; collision++ {
		name := base
		if collision > 0 {
			name += fmt.Sprintf("-%d", collision+1)
		}
		path := filepath.Join(directory, name+extension)
		// The name is CLAIMED, not merely looked at. A tool batch runs its calls
		// in parallel (loop.go), so two generations of the same prompt in the
		// same second are two goroutines racing for one name — and a check that
		// only asked whether the file existed would hand both the same answer.
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				return "", closeErr
			}
			return path, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("too many files share the name %s", base)
}

// mediaSlug turns a prompt into the readable half of a file name: the first few
// words, lowercased, with everything that is not a letter or a digit collapsed
// to a single hyphen. "Sunset over the harbour, 35mm" becomes
// "sunset-over-the-harbour-35mm".
func mediaSlug(prompt string) string {
	words := strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) > mediaSlugWords {
		words = words[:mediaSlugWords]
	}
	slug := strings.Join(words, "-")
	if len(slug) > mediaSlugLimit {
		slug = strings.TrimRight(slug[:mediaSlugLimit], "-")
	}
	return slug
}

// mediaTitle is what a picker row says about one generated file: the prompt's
// own slug, which is the phrase a person would search for, and the file's name
// when the prompt made no slug at all — a prompt of punctuation, or a file that
// arrived rather than being asked for.
func mediaTitle(prompt, path string) string {
	if slug := mediaSlug(prompt); slug != "" {
		return strings.ReplaceAll(slug, "-", " ")
	}
	return filepath.Base(path)
}

// displayMediaPath is the path as the model should say it back: relative to the
// workspace when it is inside it, so the read tool and the person's own shell
// both accept the string verbatim.
func displayMediaPath(workspace, path string) string {
	relative, err := filepath.Rel(workspace, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

// mediaByteSize is how big a generated file is, in the one place all three
// verbs read it from.
func mediaByteSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
