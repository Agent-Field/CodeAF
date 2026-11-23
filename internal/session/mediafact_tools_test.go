package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The tools write typed facts beside their unchanged prose, at the sites that
// already recorded a row.
func TestGenerateToolsRecordStructuredFacts(t *testing.T) {
	zero, paid := 0.0, 0.25
	picture := pngOfSize(t, 40, 30)
	media := &scriptedMedia{
		base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png", usage: &ai.Usage{Cost: &paid},
		audio: []byte("ID3\x04voice"), speechCost: &ai.Usage{Cost: &zero},
		music: []byte("ID3\x04tune"), musicFormat: "mp3",
		video: []byte("not an mp4 at all"),
	}
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newMediaAgent(t, media, func(config *Config) { config.ArtifactsIndex = index })
	ref := writeReference(t, workspace, "ref.png", pngOfSize(t, 2, 2))

	// Each call runs on a context that carries what the loop gives it: the
	// turn's run id and the call's own id.
	run := func(name, args, call string) {
		t.Helper()
		ctx := withCallID(withRunIdentity(context.Background()), call)
		out, bad, err := beltTool(t, agent, name).Execute(ctx, json.RawMessage(args))
		if err != nil || bad {
			t.Fatalf("%s: %v %s", name, err, out)
		}
	}
	run("generate_image", `{"prompt":"a harbour","reference_paths":["`+ref+`"]}`, "call_img")
	run("speak", `{"text":"Good morning"}`, "call_say")
	run("generate_music", `{"prompt":"a tune"}`, "call_mus")
	run("generate_video", `{"prompt":"a ferry","reference_paths":["`+ref+`"]}`, "call_vid")
	waitFor(t, "both jobs to land", func() bool { return len(ReadArtifacts(index)) == 4 })

	byKind := map[string][]Artifact{}
	for _, row := range ReadArtifacts(index) {
		byKind[row.Kind] = append(byKind[row.Kind], row)
		if row.Media == nil || !validMediaID(row.Media.ID) || row.Media.Origin != MediaGenerated {
			t.Fatalf("row %+v has no minted fact", row)
		}
		if _, err := os.Stat(row.Media.Canonical); err != nil {
			t.Fatalf("recorded canonical path is not the file: %v", err)
		}
	}
	image := byKind["image"][0].Media
	if image.Width != 40 || image.Height != 30 || image.MIME != "image/png" || image.Model != "paint/model" ||
		image.Charge != MediaCharged || image.CostUSD == nil || *image.CostUSD != paid ||
		len(image.References) != 1 || image.References[0] != ref || image.Bytes != int64(len(picture)) || image.SHA256 == "" {
		t.Fatalf("image fact = %+v", image)
	}
	var voice, tune *MediaFact
	for _, row := range byKind["audio"] {
		if row.Media.Said != "" {
			voice = row.Media
		} else {
			tune = row.Media
		}
	}
	if voice == nil || voice.Said != "Good morning" || voice.Charge != MediaNotCharged || voice.CostUSD != nil || voice.Width != 0 {
		t.Fatalf("speech fact = %+v", voice)
	}
	if tune == nil || tune.Charge != MediaChargeUnknown || tune.Model != "compose/model" {
		t.Fatalf("music fact = %+v", tune)
	}
	film := byKind["video"][0].Media
	if film.DurationMS != 0 || film.Width != 0 || film.Charge != MediaChargeUnknown || len(film.References) != 1 {
		t.Fatalf("video fact invents what it did not read: %+v", film)
	}
	// Provenance is read off the call itself, never from prose: each file has
	// the id of the tool call that made it and a run id, and no message id (the
	// submission lane has not introduced one).
	for kind, want := range map[string]string{"image": "call_img", "video": "call_vid"} {
		fact := byKind[kind][0].Media
		if fact.Tool != want || fact.Run == "" || fact.Message != "" {
			t.Fatalf("%s provenance = run %q tool %q message %q", kind, fact.Run, fact.Tool, fact.Message)
		}
	}
	if voice.Tool != "call_say" || tune.Tool != "call_mus" || voice.Run == "" {
		t.Fatalf("audio provenance: %+v %+v", voice, tune)
	}
	if image.Requested != 1 || voice.Requested != 0 {
		t.Fatalf("requested counts = %d %d: only the request's own N is recorded", image.Requested, voice.Requested)
	}
	// A call with no identity (a door with no turn) leaves it unset.
	if _, bad := runTool(t, agent, "speak", `{"text":"anon"}`); bad {
		t.Fatal("speak failed")
	}
	for _, row := range ReadArtifacts(index) {
		if row.Media.Said == "anon" && (row.Media.Tool != "" || row.Media.Run != "") {
			t.Fatalf("identity invented for a call that had none: %+v", row.Media)
		}
	}
	// The prose the model reads is unchanged: no typed field leaked into it.
	if out, _ := runTool(t, agent, "speak", `{"text":"again"}`); strings.Contains(out, "src_") || strings.Contains(out, "sha256") {
		t.Fatalf("typed facts leaked into the prose: %q", out)
	}
}
