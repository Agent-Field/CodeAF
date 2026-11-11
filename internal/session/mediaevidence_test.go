package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A look is recorded as evidence from the values the look held: the file, the
// bytes' hash, the looking model's words verbatim, who looked and who was
// working — and a repeat answered from memory is not a second look.
func TestViewImageAndReadLeaveEvidenceSources(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("a bar chart with the legend cut off"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.ArtifactsIndex = index
		config.MediaModel = senseResolver(map[string]string{"vision": "vendor/slot-eyes"})
	})
	writeImage(t, workspace, "chart.png", "PHOTOBYTES")
	ctx := withCallID(withRunIdentity(context.Background()), "call_view")
	out, bad, err := viewTool(t, agent).Execute(ctx, []byte(`{"path":"chart.png"}`))
	if err != nil || bad {
		t.Fatalf("view_image: %v %s", err, out)
	}
	rows := ReadArtifacts(index)
	if len(rows) != 1 || rows[0].Media == nil {
		t.Fatalf("rows = %+v", rows)
	}
	fact := rows[0].Media
	if fact.Origin != MediaEvidence || fact.Verb != EvidenceLookedAt || fact.Saw != "a bar chart with the legend cut off" ||
		fact.SeenBy != "vendor/slot-eyes" || fact.Working != agent.Model() || fact.Tool != "call_view" || fact.Run == "" ||
		fact.Bytes != int64(len("PHOTOBYTES")) || len(fact.SHA256) != 64 || rows[0].Kind != "image" {
		t.Fatalf("evidence = %+v", fact)
	}
	if fact.Charge != "" || fact.Model != "" {
		t.Fatalf("a look claims a charge or a made-by model: %+v", fact)
	}
	// Listed, it is an evidence source, and the file is the workspace's own.
	row, ok := mediaRowOf(rows[0])
	if !ok || row.Fact.Origin != MediaEvidence || !strings.HasPrefix(row.Fact.ID, "src_") {
		t.Fatalf("row = %+v %v", row, ok)
	}
	if live := ResolveMedia(row, []string{workspace}); live.State != MediaAvailable {
		t.Fatalf("live = %+v", live)
	}
	// An unlookable path records nothing.
	if _, bad, _ := viewTool(t, agent).Execute(ctx, []byte(`{"path":"missing.png"}`)); !bad {
		t.Fatal("a missing file was looked at")
	}
	if len(ReadArtifacts(index)) != 1 {
		t.Fatal("a failed look left evidence")
	}
}

func TestReadTranscriptIsEvidenceOnceAndMemoIsNotALook(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	media := &scriptedEar{text: "The stand-up is moved to ten past nine tomorrow morning, in the small room."}
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ArtifactsIndex = index
		config.Media = media
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model", "listen": "hear/model"})
	})
	name := dropFile(t, workspace, "memo.mp3", mp3Bytes(64))
	for i := 0; i < 2; i++ {
		if text, bad := readTool(t, agent, map[string]any{"path": name}); bad {
			t.Fatal(text)
		}
	}
	rows := ReadArtifacts(index)
	if len(rows) != 1 {
		t.Fatalf("%d evidence rows for one look and a memo hit", len(rows))
	}
	fact := rows[0].Media
	if rows[0].Kind != "audio" || fact.Verb != EvidenceListenedTo || !fact.Transcribed || fact.SeenBy != "ears/model" ||
		!strings.Contains(fact.Saw, "ten past nine") || fact.DurationMS != 0 {
		t.Fatalf("evidence = %+v", fact)
	}
}
