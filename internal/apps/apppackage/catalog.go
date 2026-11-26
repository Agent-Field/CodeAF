package apppackage

import (
	"embed"
	"io/fs"
)

//go:embed firstparty/*
var catalogFiles embed.FS

// CatalogFiles exposes the release archives and their display companions.
func CatalogFiles() (fs.FS, error) {
	return fs.Sub(catalogFiles, "firstparty")
}
