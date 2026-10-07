package main

import "testing"

// The launch boundary must carry the resolved preference to read_document;
// merely saving and displaying it made the old settings row ineffective.
func TestSettingsDocumentReaderReachesLiveChat(t *testing.T) {
	for _, engine := range []string{"auto", "local", "free", "ocr"} {
		t.Run(engine, func(t *testing.T) {
			proc := v3TestProcess(t)
			proc.Settings.DocumentEngine = engine
			launch, err := openV3Launch(proc, v3Options{Workspace: t.TempDir(), Model: "test/model", OneModel: true})
			if err != nil {
				t.Fatal(err)
			}
			if launch.Config.DocumentEngine != engine {
				t.Fatalf("document reader = %q, want %q", launch.Config.DocumentEngine, engine)
			}
		})
	}
}
