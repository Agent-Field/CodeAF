package apppackage

import (
	"embed"
	"io/fs"
)

//go:embed firstparty/* examples/*
var catalogFiles embed.FS

// CatalogFiles exposes one embedded catalog with its display companions.
func CatalogFiles(directory string) (fs.FS, error) {
	return fs.Sub(catalogFiles, directory)
}
