package appwire

// FilePreviewParams names an attachment on a record the viewer can read.
type FilePreviewParams struct {
	DataScope
	Record DataRecordRef `json:"record"`
	Blob   string        `json:"blob"`
}

type FilePreviewResult struct {
	File  PreviewFile  `json:"file"`
	Pages []FileRaster `json:"pages"`
}

type PreviewFile struct {
	Blob string `json:"blob"`
	Name string `json:"name"`
	MIME string `json:"mime"`
}

type FileRaster struct {
	MIME  string `json:"mime"`
	Bytes []byte `json:"base64"`
}

func init() {
	methods["files.preview"] = typed(func(e Engine, p FilePreviewParams) (*FilePreviewResult, error) {
		reader, ok := e.(interface {
			FilePreview(FilePreviewParams) (*FilePreviewResult, error)
		})
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "Attachment previews are unavailable.")
		}
		return reader.FilePreview(p)
	})
}
