package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type FileTextResponse struct {
	Path      string `json:"path"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}
type FileStat struct {
	Path       string     `json:"path"`
	Kind       string     `json:"kind" jsonschema:"enum=file,enum=folder,enum=repo"`
	Exists     bool       `json:"exists"`
	Size       int64      `json:"size,omitempty"`
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
}
type FileStatsResponse struct {
	Files []FileStat `json:"files"`
}
type FileFindResponse struct {
	Files     []FileStat `json:"files"`
	Truncated bool       `json:"truncated"`
}
type FileChange struct {
	Path    string `json:"path"`
	State   string `json:"state"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}
type FileChangesResponse struct {
	Files  []FileChange `json:"files"`
	Branch string       `json:"branch,omitempty"`
}
type FileDiffResponse struct {
	Path      string `json:"path"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

func fileContract() resourceContract {
	routes := append(append([]contractRoute(nil), fileContractRoutes...), imageContractRoutes...)
	return resourceContract{Name: "file", Types: []any{FileTextResponse{}, FileStat{}, FileStatsResponse{}, FileFindResponse{}, FileChange{}, FileChangesResponse{}, FileDiffResponse{}}, Routes: routes, Events: []eventContract{}}
}
