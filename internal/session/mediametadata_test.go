package session

// mediametadata_test.go pins the next stage of media source facts: the length
// of a sound and the frame of a clip read from headers, the document a model
// read, and WHO was working when a look began.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// wavFile builds a WAV by chunk: a fmt chunk of the given format and byte
// rate, then a data chunk that DECLARES declared bytes and carries have.
func wavFile(format uint16, byteRate uint32, declared uint32, have int) []byte {
	chunk := func(name string, payload []byte, size uint32) []byte {
		out := make([]byte, 8, 8+len(payload))
		copy(out, name)
		binary.LittleEndian.PutUint32(out[4:8], size)
		return append(out, payload...)
	}
	spec := make([]byte, 16)
	binary.LittleEndian.PutUint16(spec[0:2], format)
	binary.LittleEndian.PutUint16(spec[2:4], 1)
	binary.LittleEndian.PutUint32(spec[4:8], byteRate/2)
	binary.LittleEndian.PutUint32(spec[8:12], byteRate)
	binary.LittleEndian.PutUint16(spec[12:14], 2)
	binary.LittleEndian.PutUint16(spec[14:16], 16)
	body := append([]byte("WAVE"), chunk("fmt ", spec, 16)...)
	body = append(body, chunk("data", make([]byte, have), declared)...)
	head := make([]byte, 8)
	copy(head, "RIFF")
	binary.LittleEndian.PutUint32(head[4:8], uint32(len(body)))
	return append(head, body...)
}

// mp4Track is a trak with a track header (tkhd) stating a stored size and a
// matrix, and a handler.
func mp4Track(handler string, width, height uint32, rotated bool) []byte {
	tkhd := make([]byte, 84)
	matrix := tkhd[40:76]
	binary.BigEndian.PutUint32(matrix[0:4], 0x10000)
	binary.BigEndian.PutUint32(matrix[16:20], 0x10000)
	binary.BigEndian.PutUint32(matrix[32:36], 0x40000000)
	if rotated {
		binary.BigEndian.PutUint32(matrix[0:4], 0)
		binary.BigEndian.PutUint32(matrix[4:8], 0x10000)
		binary.BigEndian.PutUint32(matrix[12:16], 0xFFFF0000)
		binary.BigEndian.PutUint32(matrix[16:20], 0)
	}
	binary.BigEndian.PutUint32(tkhd[76:80], width<<16)
	binary.BigEndian.PutUint32(tkhd[80:84], height<<16)
	hdlr := make([]byte, 12)
	copy(hdlr[8:12], handler)
	return mp4TestBox("trak", append(mp4TestBox("tkhd", tkhd), mp4TestBox("mdia", mp4TestBox("hdlr", hdlr))...))
}

// ── the header parsers ──────────────────────────────────────────────────────

func TestWavLengthAndMp4FrameStateOnlyWhatTheHeaderStates(t *testing.T) {
	if length, ok := wavLength(wavFile(1, 32000, 64000, 64000)); !ok || length != 2*time.Second {
		t.Fatalf("a two second PCM WAV read as %v %v", length, ok)
	}
	for name, data := range map[string][]byte{
		"streaming sentinel":    wavFile(1, 32000, 0xFFFFFFFF, 100),
		"zero data":             wavFile(1, 32000, 0, 0),
		"claims more than held": wavFile(1, 32000, 64000, 100),
		"compressed encoding":   wavFile(0x55, 32000, 64000, 64000),
		"zero byte rate":        wavFile(1, 0, 64000, 64000),
		"not riff":              []byte("ID3\x04voice voice voice"),
		"empty":                 nil,
	} {
		if length, ok := wavLength(data); ok || length != 0 {
			t.Fatalf("%s read as %v: a header that does not state a length must stay unknown", name, length)
		}
	}

	film := mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 3000)), mp4Track("vide", 1280, 720, false), mp4TestTrack("soun"))
	if w, h, ok := mp4FrameSize(film); !ok || w != 1280 || h != 720 {
		t.Fatalf("frame = %dx%d %v", w, h, ok)
	}
	if length, sound, ok := mp4Facts(film); !ok || length != 5*time.Second || !sound {
		t.Fatalf("length = %v %v %v", length, sound, ok)
	}
	for name, data := range map[string][]byte{
		"rotated":      mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 3000)), mp4Track("vide", 1280, 720, true)),
		"audio only":   mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 3000)), mp4Track("soun", 1280, 720, false)),
		"zero size":    mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 3000)), mp4Track("vide", 0, 0, false)),
		"no tkhd":      mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 3000)), mp4TestTrack("vide")),
		"not an mp4":   []byte("not an mp4 at all"),
		"short header": mp4TestFile(mp4TestBox("trak", mp4TestBox("tkhd", []byte{0, 1, 2}))),
	} {
		if w, h, ok := mp4FrameSize(data); ok || w != 0 || h != 0 {
			t.Fatalf("%s read as %dx%d", name, w, h)
		}
	}
}

// ── generated media: measured, never requested ──────────────────────────────

// A rendered clip's length and frame come from ITS OWN boxes. The request asked
// for eight seconds at 1080p; the file says five at 1280x720; the record says
// what the file says. A file that states nothing records nothing.
func TestGeneratedWavAndMp4RecordMeasuredFactsNotRequested(t *testing.T) {
	film := mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 3000)), mp4Track("vide", 1280, 720, false))
	tune := wavFile(1, 32000, 96000, 96000)
	media := &scriptedMedia{music: tune, musicFormat: "wav", video: film}
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, _ := newMediaAgent(t, media, func(config *Config) { config.ArtifactsIndex = index })
	if _, bad := runTool(t, agent, "generate_music", `{"prompt":"a tune"}`); bad {
		t.Fatal("generate_music failed")
	}
	if _, bad := runTool(t, agent, "generate_video", `{"prompt":"a ferry","duration":8,"resolution":"1080p"}`); bad {
		t.Fatal("generate_video failed")
	}
	waitFor(t, "both renders to land", func() bool { return len(ReadArtifacts(index)) == 2 })
	for _, row := range ReadArtifacts(index) {
		switch row.Kind {
		case "audio":
			if row.Media.DurationMS != 3000 || row.Media.Width != 0 {
				t.Fatalf("music = %+v", row.Media)
			}
		case "video":
			if row.Media.DurationMS != 5000 || row.Media.Width != 1280 || row.Media.Height != 720 {
				t.Fatalf("video = %+v: the request's 8s and 1080p are not the file's", row.Media)
			}
		}
	}

	// An mp3 has no parser here, and a file that cannot be measured says nothing.
	bare := &scriptedMedia{audio: []byte("ID3\x04voice"), video: []byte("not an mp4")}
	other := filepath.Join(t.TempDir(), "artifacts.jsonl")
	spoken, _ := newMediaAgent(t, bare, func(config *Config) { config.ArtifactsIndex = other })
	if _, bad := runTool(t, spoken, "speak", `{"text":"hello"}`); bad {
		t.Fatal("speak failed")
	}
	if rows := ReadArtifacts(other); len(rows) != 1 || rows[0].Media.DurationMS != 0 {
		t.Fatalf("a spoken mp3 invented a length: %+v", rows)
	}
}

// A sound and a clip the agent perceives carry the length the container states.
func TestEvidenceCarriesMeasuredLengthOnlyWhereTheContainerStatesIt(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ear := &scriptedEar{text: "Meet me by the north gate at nine, and bring the folder with the signed copies."}
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ArtifactsIndex = index
		config.Media = ear
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model"})
	})
	wav := dropFile(t, workspace, "memo.wav", wavFile(1, 32000, 64000, 64000))
	mp3 := dropFile(t, workspace, "memo.mp3", mp3Bytes(64))
	for _, name := range []string{wav, mp3} {
		if text, bad := readTool(t, agent, map[string]any{"path": name}); bad {
			t.Fatal(text)
		}
	}
	got := map[string]int64{}
	for _, row := range ReadArtifacts(index) {
		got[row.Title] = row.Media.DurationMS
	}
	if len(got) != 2 || got["memo.wav"] != 2000 || got["memo.mp3"] != 0 {
		t.Fatalf("durations = %v", got)
	}
}

// ── who was working ─────────────────────────────────────────────────────────

// A model switched while a look is in flight does not relabel it: the record
// says who was working when the look BEGAN.
func TestWorkingModelIsCapturedBeforeTheNetworkCall(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	var agent *Agent
	switchTo := "switched/after-the-call-began"
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			agent.SetModel(switchTo)
			return textResponse("a harbour at dusk"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.ArtifactsIndex = index
		config.MediaModel = senseResolver(map[string]string{"vision": "vendor/slot-eyes"})
	})
	before := agent.Model()
	if before == switchTo {
		t.Fatal("the fixture starts on the model it switches to")
	}
	writeImage(t, workspace, "harbour.png", "PHOTOBYTES")
	if out, bad, err := viewTool(t, agent).Execute(context.Background(), []byte(`{"path":"harbour.png"}`)); err != nil || bad {
		t.Fatalf("view_image: %v %s", err, out)
	}
	if agent.Model() != switchTo {
		t.Fatalf("the switch did not happen (%s): the test proves nothing", agent.Model())
	}
	fact := ReadArtifacts(index)[0].Media
	if fact.Working != before || fact.SeenBy != "vendor/slot-eyes" {
		t.Fatalf("working = %q seen by %q, want %q", fact.Working, fact.SeenBy, before)
	}

	// The same holds for a sound and a clip, and for a document.
	hear := &switchingEar{scriptedEar: scriptedEar{text: "The delivery is rescheduled to Thursday afternoon, after lunch."}, agent: &agent, to: switchTo + "/2"}
	agent.SetModel(before)
	agent.config.Media = hear
	agent.config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model"})
	name := dropFile(t, workspace, "memo.mp3", mp3Bytes(64))
	if text, bad := readTool(t, agent, map[string]any{"path": name}); bad {
		t.Fatal(text)
	}
	if agent.Model() != switchTo+"/2" {
		t.Fatal("the transcription did not switch the model")
	}
	var memo *MediaFact
	for _, row := range ReadArtifacts(index) {
		if row.Title == "memo.mp3" {
			memo = row.Media
		}
	}
	if memo == nil || memo.Working != before || memo.SeenBy != "ears/model" {
		t.Fatalf("audio evidence = %+v", memo)
	}
}

type switchingEar struct {
	scriptedEar
	agent **Agent
	to    string
}

func (e *switchingEar) Transcribe(ctx context.Context, request provider.TranscriptionRequest) (*provider.TranscriptionResponse, error) {
	(*e.agent).SetModel(e.to)
	return e.scriptedEar.Transcribe(ctx, request)
}

// ── a document the model read ───────────────────────────────────────────────

// switchingParser changes the session's model while the parse is in flight.
type switchingParser struct {
	scriptedParser
	agent **Agent
	to    string
}

func (p *switchingParser) ParseDocument(ctx context.Context, request provider.DocumentRequest) (*provider.DocumentResponse, error) {
	(*p.agent).SetModel(p.to)
	return p.scriptedParser.ParseDocument(ctx, request)
}

func documentEvidence(t *testing.T, index string) []Artifact {
	t.Helper()
	var out []Artifact
	for _, row := range ReadArtifacts(index) {
		if row.Kind == mediaDocument {
			out = append(out, row)
		}
	}
	return out
}

// The local extractor states a count and a text layer; the evidence keeps them
// and the words exactly as the model was given them, and no model is credited.
func TestReadDocumentLocalPDFIsDocumentEvidenceWithParserFacts(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	parser := &scriptedParser{}
	agent, workspace := newDocumentAgent(t, parser, nil, func(config *Config) { config.ArtifactsIndex = index })
	name := dropFile(t, workspace, "memo.pdf", onePageTextPDF("Section 4 begins here and runs for several honest lines of text"))
	if text, bad := documentTool(t, agent, map[string]any{"path": name}); bad {
		t.Fatal(text)
	}
	rows := documentEvidence(t, index)
	if len(rows) != 1 {
		t.Fatalf("%d document rows", len(rows))
	}
	fact := rows[0].Media
	if fact.Origin != MediaEvidence || fact.Verb != EvidenceRead || fact.MIME != "application/pdf" ||
		!strings.Contains(fact.Saw, "Section 4 begins here") || fact.PageCount != 1 ||
		fact.TextLayer == nil || !*fact.TextLayer || fact.SeenBy != "" || fact.Working != agent.Model() || len(parser.requests()) != 0 {
		t.Fatalf("evidence = %+v", fact)
	}
	// Listed, it is a document source, and the full words are retrievable.
	row, ok := mediaRowOf(rows[0])
	if !ok || row.Kind != mediaDocument {
		t.Fatalf("row = %+v %v", row, ok)
	}
	// The agent's own door has no journal id in a test; the record is moved to a
	// session and read back through the same lookup the wire uses.
	moved := filepath.Join(t.TempDir(), "artifacts.jsonl")
	id := RecordMedia(moved, Artifact{Path: rows[0].Path, Session: "s1", Title: rows[0].Title, Kind: rows[0].Kind, Created: time.Now()}, *fact)
	found, err := FindMedia(moved, "s1", id, time.Now().Add(time.Second))
	if err != nil || found.Kind != mediaDocument || found.Fact.Saw != fact.Saw || found.Fact.PageCount != 1 ||
		found.Fact.TextLayer == nil || !*found.Fact.TextLayer {
		t.Fatalf("found = %+v %v", found, err)
	}
	if live := ResolveMedia(row, []string{workspace}); live.State != MediaAvailable {
		t.Fatalf("live = %+v", live)
	}
}

// A scan has a known page count and a known absence of a text layer; the billed
// native rung is a MODEL that read it. An engine pinned to a billed parser has
// neither fact, and says so by saying nothing — never a zero or a false.
func TestReadDocumentScanAndBilledParseKeepOnlyWhatWasReported(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	var agent *Agent
	parser := &switchingParser{
		scriptedParser: scriptedParser{answers: map[provider.DocumentParseEngine]string{
			provider.DocumentParseNative:     "Invoice 114, total due thirty days after delivery of the goods.",
			provider.DocumentParseMistralOCR: "Receipt 9, paid in full on the first of the month, thank you.",
		}},
		agent: &agent, to: "switched/mid-parse",
	}
	original := newDocClient
	newDocClient = func(Config) (DocumentParser, error) { return parser, nil }
	t.Cleanup(func() { newDocClient = original })
	var workspace string
	agent, workspace = newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.ArtifactsIndex = index })
	before := agent.Model()
	scan := dropFile(t, workspace, "scan.pdf", scannedPDF())
	if text, bad := documentTool(t, agent, map[string]any{"path": scan}); bad {
		t.Fatal(text)
	}
	rows := documentEvidence(t, index)
	if len(rows) != 1 {
		t.Fatalf("%d document rows", len(rows))
	}
	fact := rows[0].Media
	if fact.PageCount != 1 || fact.TextLayer == nil || *fact.TextLayer || fact.SeenBy != before || fact.Working != before ||
		!strings.Contains(fact.Saw, "Invoice 114") || len(fact.SHA256) != 64 {
		t.Fatalf("scan evidence = %+v", fact)
	}
	if agent.Model() != "switched/mid-parse" {
		t.Fatal("the parse did not switch the model")
	}

	// Pinned to the OCR parser: the local extractor never ran, so no count and
	// no verdict on a text layer exist, and neither is serialized.
	agent.SetModel(before)
	agent.config.DocumentEngine = "ocr"
	other := dropFile(t, workspace, "second.pdf", scannedPDF()[:len(scannedPDF())-1])
	if text, bad := documentTool(t, agent, map[string]any{"path": other}); bad {
		t.Fatal(text)
	}
	var billed *MediaFact
	for _, row := range documentEvidence(t, index) {
		if row.Title == "second.pdf" {
			billed = row.Media
		}
	}
	if billed == nil || billed.PageCount != 0 || billed.TextLayer != nil || billed.SeenBy != "" || !strings.Contains(billed.Saw, "Receipt 9") {
		t.Fatalf("billed evidence = %+v", billed)
	}
	raw, _ := json.Marshal(billed)
	if strings.Contains(string(raw), "page_count") || strings.Contains(string(raw), "text_layer") {
		t.Fatalf("unknown facts reached the record: %s", raw)
	}
}

// A repeat answered from the memo is not a second look, a failed read leaves
// nothing, and a document the road does not read as a PDF is not document
// evidence; another deliverable that is called a document is not a source.
func TestDocumentEvidenceIsFreshPDFReadsOnly(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	parser := &scriptedParser{answers: map[provider.DocumentParseEngine]string{
		provider.DocumentParseMistralOCR: "Receipt 9, paid in full on the first of the month, thank you.",
	}}
	agent, workspace := newDocumentAgent(t, parser, nil, func(config *Config) {
		config.ArtifactsIndex = index
		config.DocumentEngine = "ocr"
	})
	scan := dropFile(t, workspace, "scan.pdf", scannedPDF())
	for i := 0; i < 2; i++ {
		if text, bad := documentTool(t, agent, map[string]any{"path": scan}); bad {
			t.Fatal(text)
		}
	}
	if rows := documentEvidence(t, index); len(rows) != 1 {
		t.Fatalf("%d evidence rows for one read and a memo hit", len(rows))
	}
	office := dropFile(t, workspace, "plan.docx", []byte("PK not really a docx"))
	_, _ = documentTool(t, agent, map[string]any{"path": office})
	failing := &scriptedParser{err: context.DeadlineExceeded}
	agent.docs.parser, agent.docs.err = failing, nil
	if text, bad := documentTool(t, agent, map[string]any{"path": dropFile(t, workspace, "other.pdf", append(scannedPDF(), ' '))}); !bad {
		t.Fatalf("a failing parse was read as success: %s", text)
	}
	if rows := documentEvidence(t, index); len(rows) != 1 {
		t.Fatalf("a docx or a failed read left evidence: %d rows", len(rows))
	}

	// A "document" deliverable of the old kind is not a media source, however it
	// is dressed; only the engine's own evidence row is.
	export := Artifact{Path: scan, Session: "s", Title: "export", Kind: mediaDocument, Created: time.Now()}
	if _, ok := mediaRowOf(export); ok {
		t.Fatal("a plain document deliverable became a media source")
	}
	export.Media = &MediaFact{Origin: MediaGenerated, MIME: "application/pdf"}
	if _, ok := mediaRowOf(export); ok {
		t.Fatal("a generated 'document' became a media source")
	}
}

func TestPerceptionKeepsTheVersionReadBeforeAFileChangesDuringTheCall(t *testing.T) {
	for _, tool := range []string{"view_image", "read"} {
		t.Run(tool, func(t *testing.T) {
			index := filepath.Join(t.TempDir(), "artifacts.jsonl")
			var workspace string
			original := []byte("PHOTOBYTES")
			completer := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
				path := filepath.Join(workspace, "scene.png")
				if err := os.WriteFile(path+".replacement", []byte("OTHERBYTES"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path+".replacement", path); err != nil {
					t.Fatal(err)
				}
				return textResponse("the original scene"), nil
			}}}
			agent, dir := newTestAgent(t, completer, func(c *Config) {
				c.ArtifactsIndex = index
				c.MediaModel = senseResolver(map[string]string{"vision": "eyes/model"})
			})
			workspace = dir
			writeImage(t, workspace, "scene.png", string(original))
			if tool == "view_image" {
				if text, bad, err := viewTool(t, agent).Execute(context.Background(), []byte(`{"path":"scene.png"}`)); err != nil || bad {
					t.Fatalf("look: %s %v", text, err)
				}
			} else {
				if text, bad := readTool(t, agent, map[string]any{"path": "scene.png"}); bad {
					t.Fatal(text)
				}
			}
			artifacts := ReadArtifacts(index)
			if len(artifacts) != 1 {
				t.Fatalf("evidence count %d", len(artifacts))
			}
			row, ok := mediaRowOf(artifacts[0])
			if !ok {
				t.Fatal("missing source")
			}
			sum := sha256.Sum256(original)
			if row.Fact.SHA256 != hex.EncodeToString(sum[:]) || row.Fact.Saw != "the original scene" {
				t.Fatalf("look bytes or words changed: %#v", row.Fact)
			}
			if live := ResolveMedia(row, []string{workspace}); live.State != MediaChanged {
				t.Fatalf("replacement presented as original: %#v", live)
			}
		})
	}
}
