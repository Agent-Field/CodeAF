package remote

// image.go is the one payload that cannot simply be handed on. Every other
// argument on this wire means the same thing on both machines; a picture's PATH
// does not. The surface read it off its own disk, and the engine has no such
// disk, so the bytes travel and the path is REMADE HERE.
//
// It matters because of a law in internal/session's image.go: a journal holds a
// REFERENCE to a picture and never the bytes, so that a transcript stays
// readable and a resumed session is not re-sending base64 forever. A reference
// is only worth writing if it names a file that exists on the machine that
// wrote it — so the engine writes the picture down before it submits it, and
// what lands in the journal is a path a person on THIS machine can open.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Where an uploaded picture lands is internal/session's answer and not this
// package's: [session.ImagesDir] is asked, so a picture that arrived over the
// wire and a picture the MODEL painted land in the same directory on the same
// machine. One folder for every picture a session touched is one gesture to
// delete them, and two packages with two opinions about that folder is how the
// gesture stops working.

// imageStampFormat is the sortable half of the name, as tools_image.go names
// its own: `ls` reads in the order the pictures arrived.
const imageStampFormat = "20060102-150405"

// imageExtensions is the same five formats the rest of the codebase accepts,
// read the other way round — from the type the surface declared to the suffix
// the file gets here.
var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// store writes every picture that arrived with bytes into the workspace and
// returns the images pointing at what it wrote.
//
// An image that arrived with a path and NO bytes is left exactly as it is: that
// is a caller naming a file on the engine's own disk, which is a legitimate
// thing to do and the session will read it itself.
func (s *server) store(images []session.Image) ([]session.Image, error) {
	if len(images) == 0 {
		return images, nil
	}
	s.state.Lock()
	workspace, place, index := s.engine.Workspace, s.engine.Place, s.engine.ArtifactsIndex
	s.state.Unlock()

	out := make([]session.Image, 0, len(images))
	for _, image := range images {
		if len(image.Bytes) == 0 {
			if strings.TrimSpace(image.Path) == "" {
				return nil, errors.New("engine: an attached picture arrived with neither bytes nor a path")
			}
			out = append(out, image)
			continue
		}
		path, err := writeImage(place, workspace, image)
		if err != nil {
			return nil, err
		}
		// A picture that arrived is a picture somebody may want back — it is on
		// the engine's disk and nowhere on theirs — so it earns its row in the
		// index the same way a painted one does (session's artifacts.go).
		session.RecordArtifact(index, session.Artifact{
			Path:    path,
			Session: session.PlaceSession(place),
			Title:   filepath.Base(path),
			Kind:    "image",
			Created: time.Now(),
		})
		// The bytes ride along rather than being dropped: internal/session
		// reads them instead of opening the file it is about to be told about,
		// so the picture is written once and read never.
		image.Path = path
		out = append(out, image)
	}
	return out, nil
}

// writeImage puts one picture on disk under a name that cannot collide: the
// moment it arrived, and the head of its own digest. Two identical pastes in
// the same second are the same file, which is the right answer to the only
// collision this naming can have.
func writeImage(place session.Place, workspace string, image session.Image) (string, error) {
	extension, err := imageExtension(image)
	if err != nil {
		return "", err
	}
	directory := session.ImagesDir(place, workspace)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("engine: create the image directory: %w", err)
	}
	sum := sha256.Sum256(image.Bytes)
	name := time.Now().Format(imageStampFormat) + "-" + hex.EncodeToString(sum[:4]) + extension
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, image.Bytes, 0o600); err != nil {
		return "", fmt.Errorf("engine: save the attached picture: %w", err)
	}
	return path, nil
}

// imageExtension names the file. The declared type wins, because it is what the
// surface actually looked at; the path the surface sent is consulted only when
// nothing was declared, and it is consulted for its SUFFIX alone — the rest of
// it is a directory on another machine.
//
// A picture whose type nobody can name is REFUSED rather than guessed at, which
// is the same refusal internal/session's imageMediaType makes and for the same
// reason: the wrong type is a provider error about a content part with nothing
// in it pointing back here.
func imageExtension(image session.Image) (string, error) {
	if declared := strings.ToLower(strings.TrimSpace(image.MIME)); declared != "" {
		if extension, ok := imageExtensions[declared]; ok {
			return extension, nil
		}
		return "", fmt.Errorf("engine: %s is not an image this surface can send — png, jpeg, webp and gif are", declared)
	}
	extension := strings.ToLower(filepath.Ext(image.Path))
	switch extension {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return extension, nil
	}
	return "", fmt.Errorf("engine: %s is not an image this surface can send — png, jpeg, webp and gif are", filepath.Base(image.Path))
}
