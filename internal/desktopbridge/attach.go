package desktopbridge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	maxTotalAttachBytes = 20 << 20
	maxPictureBytes     = 10 << 20
	// Base64 inflates by a third; the extra covers the JSON around it.
	maxTurnBody = (maxTotalAttachBytes+2<<20)*4/3 + 1<<20
)

type outgoingFile struct {
	Name       string `json:"name"`
	Mime       string `json:"mime"`
	DataBase64 string `json:"dataBase64"`
}

type turnAsk struct {
	Text  string         `json:"text"`
	Mode  string         `json:"mode"`
	Files []outgoingFile `json:"files"`
}

type attachments struct {
	files  []remote.WireFile
	images []session.Image
}

func (a attachments) any() bool { return len(a.files)+len(a.images) > 0 }

var pictureTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

// attachments decodes the files and sorts pictures from other files. The
// status is the HTTP code for the error.
func (t turnAsk) attachments() (attachments, int, error) {
	var out attachments
	total := 0
	for _, f := range t.Files {
		if f.Name == "" {
			return out, 400, errors.New("every attached file needs a name")
		}
		data, err := base64.StdEncoding.DecodeString(f.DataBase64)
		if err != nil {
			return out, 400, fmt.Errorf("%s could not be read as an attachment", f.Name)
		}
		picture := pictureTypes[f.Mime]
		if picture && len(data) > maxPictureBytes {
			return out, 413, fmt.Errorf("%s is over the %dMB limit for one picture", f.Name, maxPictureBytes>>20)
		}
		total += len(data)
		if total > maxTotalAttachBytes {
			return out, 413, fmt.Errorf("these attachments are over the %dMB one message may carry; send them across a few messages", maxTotalAttachBytes>>20)
		}
		if picture {
			out.images = append(out.images, session.Image{Path: f.Name, MIME: f.Mime, Bytes: data})
		} else {
			out.files = append(out.files, remote.WireFile{Name: f.Name, MIME: f.Mime, Bytes: data})
		}
	}
	return out, 0, nil
}

// submit opens a turn, with attachments when the message carries any.
func (s *conversation) submit(text string, a attachments) (<-chan session.Event, error) {
	if !a.any() {
		return s.conn.Agent.Submit(context.Background(), text)
	}
	door, ok := s.conn.Agent.(interface {
		SubmitFiles(context.Context, string, []remote.WireFile, []session.Image) (<-chan session.Event, error)
	})
	if !ok {
		return nil, errors.New("this engine cannot take attachments")
	}
	return door.SubmitFiles(context.Background(), text, a.files, a.images)
}
