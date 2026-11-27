package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

// FilePreview serves the image only while the attachment is visible on the
// selected record. A hash on its own never grants access to the bytes.
func (b *appBackend) FilePreview(p appwire.FilePreviewParams) (*appwire.FilePreviewResult, error) {
	if p.App == "" || p.Record.Type == "" || p.Record.ID == "" {
		return nil, appwire.Errorf(appwire.CodeBadParams, "Choose a record attachment to preview.")
	}
	store, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	files, err := store.IntakeFiles(dataScope(p.DataScope), p.Record.Type, p.Record.ID)
	if err != nil {
		return nil, appDataError(err)
	}
	for _, file := range files {
		if blob := fmt.Sprintf("sha256:%x", sha256.Sum256(file.Bytes)); blob != p.Blob {
			continue
		}
		const maxPreviewPixels = 18_000_000
		if len(file.Bytes) > data.IntakeBytesMax {
			return nil, appwire.Errorf(appwire.CodeBadParams, "This attachment is too large to preview.")
		}
		dimensions, format, err := image.DecodeConfig(bytes.NewReader(file.Bytes))
		if err != nil || (format != "png" && format != "jpeg") || dimensions.Height < 1 || dimensions.Width > maxPreviewPixels/dimensions.Height {
			return nil, appwire.Errorf(appwire.CodeBadParams, "This attachment needs a PNG or JPEG preview.")
		}
		mime := "image/" + format
		return &appwire.FilePreviewResult{
			File:  appwire.PreviewFile{Blob: p.Blob, Name: file.Name, MIME: mime},
			Pages: []appwire.FileRaster{{MIME: mime, Bytes: file.Bytes}},
		}, nil
	}
	return nil, appwire.Errorf(appwire.CodeNotFound, "This attachment is unavailable.")
}
