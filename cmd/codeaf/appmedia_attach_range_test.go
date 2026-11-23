package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// rangeWAVBytes is a PCM WAV of exactly ms milliseconds (8 kHz, mono, 8-bit), so
// the engine records a real length for it.
func rangeWAVBytes(ms int) []byte {
	data := make([]byte, 8*ms)
	for i := range data {
		data[i] = byte(i)
	}
	head := make([]byte, 44)
	copy(head, "RIFF")
	binary.LittleEndian.PutUint32(head[4:], uint32(36+len(data)))
	copy(head[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(head[16:], 16)
	binary.LittleEndian.PutUint16(head[20:], 1)
	binary.LittleEndian.PutUint16(head[22:], 1)
	binary.LittleEndian.PutUint32(head[24:], 8000)
	binary.LittleEndian.PutUint32(head[28:], 8000)
	binary.LittleEndian.PutUint16(head[32:], 1)
	binary.LittleEndian.PutUint16(head[34:], 8)
	copy(head[36:], "data")
	binary.LittleEndian.PutUint32(head[40:], uint32(len(data)))
	return append(head, data...)
}

// recordRange records a file the engine would have recorded and answers the
// source id and version the app reads back.
func recordRange(t *testing.T, b *appBackend, agent appwire.Agent, name, kind string, data []byte, duration int64) (source, version, path string) {
	t.Helper()
	id, _, err := b.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(agent.Workspace, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fact := session.GeneratedMedia(path, data, "")
	if duration >= 0 {
		fact.DurationMS = duration
	}
	source = session.RecordMedia(appMediaIndex(), session.Artifact{Path: path, Session: id, Title: name, Kind: kind, Created: time.Now()}, fact)
	got, err := b.GetMedia(agent.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	return source, got.Version, path
}

// rangeBackend is a backend with a recorded two-second WAV.
func rangeBackend(t *testing.T) (*appBackend, appwire.Agent, string, string, string, []byte) {
	t.Helper()
	b, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	a := b.agent(agent.ID)
	b.locked(func() { a.transcript = filepath.Join(t.TempDir(), "0123456789abcdef", "transcript.jsonl") })
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	data := rangeWAVBytes(2000)
	source, version, path := recordRange(t, b, agent, "standup.wav", "audio", data, -1)
	return b, agent, source, version, path, data
}

func rangeLeaks(t *testing.T, b *appBackend, agent string) {
	t.Helper()
	b.locked(func() {
		if b.referenceN != 0 || b.agents[agent].referenceN != 0 || b.referenceWorkers != 0 {
			t.Fatalf("admission leaked: %d %d %d", b.referenceN, b.agents[agent].referenceN, b.referenceWorkers)
		}
	})
}

// A RANGE IS AN ORDINARY FILE WITH TYPED PROVENANCE, never a picture.
func TestAppMediaAttachRangeIsAFileWithTheSourceVersionAndSpan(t *testing.T) {
	b, agent, source, version, original, data := rangeBackend(t)
	chip, err := b.AttachMediaRange(agent.ID, source, version, 250, 1750)
	if err != nil {
		t.Fatal(err)
	}
	m := chip.Media
	if chip.Kind != appwire.AttachFile || chip.Title != "standup.range-250-1750.wav" || filepath.Base(chip.Path) != chip.Title || m == nil || m.SHA256 == "" || m.Source == nil ||
		m.Source.SourceID != source || m.Source.Version != version || m.Source.Path != original ||
		m.Source.FromMS == nil || *m.Source.FromMS != 250 || m.Source.ToMS == nil || *m.Source.ToMS != 1750 || m.Source.AtMS != nil {
		t.Fatalf("the chip is %+v / %+v", chip, m)
	}
	if copied, err := os.ReadFile(chip.Path); err != nil || string(copied) != string(data) {
		t.Fatalf("the snapshot is not the recording: %v", err)
	}
	if err := os.WriteFile(original, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	if copied, _ := os.ReadFile(chip.Path); string(copied) != string(data) {
		t.Fatal("editing the original altered the snapshot")
	}
	if err := b.RemoveAttachment(agent.ID, chip.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chip.Path); !os.IsNotExist(err) {
		t.Fatal("an unsent range leaked")
	}
	if edited, _ := os.ReadFile(original); string(edited) != "edited" {
		t.Fatal("removing touched the original")
	}
	rangeLeaks(t, b, agent.ID)
}

func TestAppMediaAttachRangeSendKeepsTheSpanAndTheWordsExactly(t *testing.T) {
	b, agent, source, version, original, _ := rangeBackend(t)
	chip, err := b.AttachMediaRange(agent.ID, source, version, 500, 1500)
	if err != nil {
		t.Fatal(err)
	}
	words := "  what is said here?  \n"
	if _, err := b.SendWith(agent.ID, words, "range-client", []string{chip.ID}); err != nil {
		t.Fatal(err)
	}
	conv := b.agent(agent.ID).live.conn.Conv.(*fakeConversation)
	conv.mu.Lock()
	sent := append([]session.Submission(nil), conv.submissions...)
	conv.mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("the engine heard %d messages", len(sent))
	}
	got := sent[0]
	if len(got.Media) != 0 || got.Annotation != nil {
		t.Fatalf("an audio range became a picture: %+v", got.Media)
	}
	if got.ClientID != "range-client" || got.Said == nil || *got.Said != words || !strings.HasPrefix(got.Text, words+"\n\nAttached file: "+chip.Path) {
		t.Fatalf("the words, id or file line changed: %+v", got)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("described %+v", got.Attachments)
	}
	d := got.Attachments[0]
	if d.Kind != "file" || d.Path != chip.Path || d.Source == nil || d.Source.SourceID != source || d.Source.Version != version || d.Source.Path != original ||
		d.Source.FromMS == nil || *d.Source.FromMS != 500 || d.Source.ToMS == nil || *d.Source.ToMS != 1500 || d.Source.AtMS != nil {
		t.Fatalf("the described attachment lost the source or span: %+v", d)
	}
	if staged(b, agent.ID, chip.ID) {
		t.Fatal("an accepted ID is still staged")
	}
	if _, err := os.Stat(chip.Path); err != nil {
		t.Fatal("an accepted snapshot was deleted")
	}
	rangeLeaks(t, b, agent.ID)
}

func TestAppMediaAttachRangeNeverEntersAPictureSubmission(t *testing.T) {
	b, agent, source, version, _, _ := rangeBackend(t)
	chip, err := b.AttachMediaRange(agent.ID, source, version, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	a := b.agent(agent.ID)
	var attach appAttach
	b.locked(func() { attach = a.attach[chip.ID] })
	if images := appReferenceImages([]appAttach{attach}, nil); len(images) != 0 {
		t.Fatalf("a range reached the picture path: %+v", images)
	}
	composed, err := appComposedOf(agent.Workspace, []appAttach{attach}, nil)
	if err != nil || composed != nil {
		t.Fatalf("a plain send of a range composed pictures: %+v %v", composed, err)
	}
	_, err = appComposedOf(agent.Workspace, []appAttach{attach}, &appwire.MessageSubmission{Media: []appwire.SubmissionMedia{{ID: chip.ID}}})
	if wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("naming a range as a picture answered %v", err)
	}
	// Even a caller's own claim of a range on a picture is refused.
	from, to := int64(0), int64(5)
	if err := (&appwire.MessageSubmission{Media: []appwire.SubmissionMedia{{ID: "at1", Source: &appwire.MediaReference{FromMS: &from, ToMS: &to}}}}).Check([]string{"at1"}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a picture claimed a range: %v", err)
	}
}

func TestAppMediaAttachRangeRefusesAndLeavesNothingStaged(t *testing.T) {
	b, agent, source, version, original, data := rangeBackend(t)
	clip := append([]byte{0, 0, 0, 0x18}, []byte("ftypisom\x00\x00\x02\x00isomiso2")...)
	videoSource, videoVersion, _ := recordRange(t, b, agent, "no-length.mp4", "video", clip, 0)
	imagePath := filepath.Join(agent.Workspace, "pic.png")
	writePNG(t, imagePath, 2, 2)
	id, _, _ := b.mediaScope(agent.ID)
	imageSource := session.RecordMedia(appMediaIndex(), session.Artifact{Path: imagePath, Session: id, Title: "pic", Kind: "image", Created: time.Now()}, session.DerivedMedia(imagePath))
	foreign := session.RecordMedia(appMediaIndex(), session.Artifact{Path: original, Session: "ffffffffffffffff", Title: "foreign", Kind: "audio", Created: time.Now()}, session.GeneratedMedia(original, data, ""))
	imageVersion := func() string { got, _ := b.GetMedia(agent.ID, imageSource); return got.Version }()

	cases := []struct {
		name         string
		source, ver  string
		from, to     int64
		code, phrase string
	}{
		{"a stale version", source, "sha256:stale", 0, 1000, appwire.CodeBadParams, "no longer available"},
		{"an unknown source", "src_unknown000000000000000000", version, 0, 1000, appwire.CodeNotFound, ""},
		{"another conversation's source", foreign, version, 0, 1000, appwire.CodeNotFound, ""},
		{"an unknown length", videoSource, videoVersion, 0, 1000, appwire.CodeBadParams, "not recorded"},
		{"past the recorded length", source, version, 0, 2001, appwire.CodeBadParams, "past"},
		{"a picture", imageSource, imageVersion, 0, 1000, appwire.CodeBadParams, ""},
		{"an empty span", source, version, 700, 700, appwire.CodeBadParams, ""},
	}
	for _, c := range cases {
		_, err := b.AttachMediaRange(agent.ID, c.source, c.ver, c.from, c.to)
		if wireCode(err) != c.code || (c.phrase != "" && !strings.Contains(err.Error(), c.phrase)) {
			t.Fatalf("%s answered %v", c.name, err)
		}
		rangeLeaks(t, b, agent.ID)
	}
	if _, err := b.AttachMediaRange("no-such-agent", source, version, 0, 1000); err == nil {
		t.Fatal("a stranger's agent was served")
	}
	// The source changing after recording is refused, and the original stays.
	if err := os.WriteFile(original, rangeWAVBytes(1999), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AttachMediaRange(agent.ID, source, version, 0, 1000); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a changed source answered %v", err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AttachMediaRange(agent.ID, source, version, 0, 1000); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a deleted source answered %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(b.agent(agent.ID).transcript), "artifacts"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".reference-") {
			t.Fatalf("a refusal left %s", e.Name())
		}
	}
	rangeLeaks(t, b, agent.ID)
}

func TestAppMediaAttachRangeUsesTheSameAdmission(t *testing.T) {
	b, agent, source, version, _, _ := rangeBackend(t)
	for _, kind := range []string{"agent", "global", "workers"} {
		b.locked(func() {
			switch kind {
			case "agent":
				b.agents[agent.ID].referenceN = appwire.MaxAttachments
			case "global":
				b.referenceN = appReferenceMax
			case "workers":
				b.referenceWorkers = appReferenceWorkers
			}
		})
		if _, err := b.AttachMediaRange(agent.ID, source, version, 0, 1000); wireCode(err) != appwire.CodeBusy {
			t.Fatalf("%s not bounded: %v", kind, err)
		}
		b.locked(func() { b.agents[agent.ID].referenceN = 0; b.referenceN = 0; b.referenceWorkers = 0 })
	}
}

func TestAppMediaAttachRangeUncertainDeliveryKeepsTheFileAndCloseDropsUnsent(t *testing.T) {
	b, agent, source, version, _, _ := rangeBackend(t)
	uncertain, err := b.AttachMediaRange(agent.ID, source, version, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	held := b.agent(agent.ID)
	b.locked(func() { held.attach[uncertain.ID].reference.attempted = true })
	if err := b.RemoveAttachment(agent.ID, uncertain.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(uncertain.Path); err != nil {
		t.Fatal("a range whose delivery is unknown was removed")
	}
	unsent, err := b.AttachMediaRange(agent.ID, source, version, 100, 900)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unsent.Path); !os.IsNotExist(err) {
		t.Fatal("close retained a definitely unsent range")
	}
	if _, err := os.Stat(uncertain.Path); err != nil {
		t.Fatal("close removed an unknown-delivery range")
	}
}

func TestAppMediaAttachRangeKnownRefusalCanDiscardTheSnapshot(t *testing.T) {
	b, agent, source, version, _, _ := rangeBackend(t)
	chip, err := b.AttachMediaRange(agent.ID, source, version, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.SendWith(agent.ID, "x", "c", []string{chip.ID, "at99"}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("a send naming a missing chip answered %v", err)
	}
	if !staged(b, agent.ID, chip.ID) {
		t.Fatal("a refused send consumed the range")
	}
	var fresh []*appReference
	held := b.agent(agent.ID)
	b.locked(func() { fresh = []*appReference{held.attach[chip.ID].reference} })
	b.referenceAttempt([]appAttach{{reference: fresh[0]}})
	b.referenceRefused(fresh, appwire.Errorf(appwire.CodeBadParams, "refused"))
	if fresh[0].attempted {
		t.Fatal("an explicit refusal left the range uncertain")
	}
	if err := b.RemoveAttachment(agent.ID, chip.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chip.Path); !os.IsNotExist(err) {
		t.Fatal("a known-refused range leaked")
	}
}

// A CALLER CANNOT GIVE AN ORDINARY ATTACHMENT A SOURCE: it is dropped when the
// chip is staged, and the record's descriptor is built only from a snapshot the
// engine made.
func TestAnOrdinaryAttachmentCannotClaimARangeSource(t *testing.T) {
	b, agent, _, _, _, _ := rangeBackend(t)
	path := filepath.Join(agent.Workspace, "notes.md")
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	from, to := int64(1), int64(9)
	forged := &appwire.AttachmentMedia{SHA256: "forged", Source: &appwire.MediaReference{SourceID: "src_forged", Version: "sha256:forged", FromMS: &from, ToMS: &to}}
	chip, err := b.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachFile, Path: "notes.md", Media: forged}, "")
	if err != nil {
		t.Fatal(err)
	}
	if chip.Media != nil {
		t.Fatalf("a caller's media facts survived staging: %+v", chip.Media)
	}
	var attach appAttach
	held := b.agent(agent.ID)
	b.locked(func() { attach = held.attach[chip.ID] })
	attach.view.Media = forged
	if d := appDescribed(attach); d.Source != nil {
		t.Fatalf("an ordinary file was described with a source: %+v", d.Source)
	}
}

// A recorded line replays as it was written; one with no source shows none.
func TestAppAttachmentsOfReplaysTheTypedRange(t *testing.T) {
	from, to := int64(5), int64(9)
	got := appAttachmentsOf(&session.PersonMark{Attachments: []session.MessageAttachment{
		{ID: "at1", Kind: "file", Name: "a.range-5-9.wav", Path: "/o/a.range-5-9.wav", Source: &session.MediaReference{SourceID: "src_1", Path: "/w/a.wav", Version: "sha256:v", FromMS: &from, ToMS: &to}},
		{ID: "at2", Kind: "file", Name: "old.txt", Path: "old.txt"},
	}})
	if len(got) != 2 || got[0].Media == nil || got[0].Media.Source.SourceID != "src_1" || got[0].Media.Source.Version != "sha256:v" || *got[0].Media.Source.FromMS != 5 || *got[0].Media.Source.ToMS != 9 {
		t.Fatalf("the range was not replayed: %+v", got)
	}
	if got[1].Media != nil {
		t.Fatalf("an old line gained media: %+v", got[1].Media)
	}
	*got[0].Media.Source.FromMS = 99
	if from != 5 {
		t.Fatal("the replay shares the record's range")
	}
}

// The actual dispatch boundary refuses an older host before a range can be
// submitted. The staging lifecycle must know this was not an uncertain send.
type oldRangeConversation struct{ *fakeConversation }

func (*oldRangeConversation) AttachmentSourcesKnown() bool { return false }

func TestAppMediaRangeOldHostRefusalLeavesSnapshotRemovable(t *testing.T) {
	for _, how := range []string{"remove", "close"} {
		t.Run(how, func(t *testing.T) {
			b, agent, source, version, _, _ := rangeBackend(t)
			chip, err := b.AttachMediaRange(agent.ID, source, version, 0, 1000)
			if err != nil {
				t.Fatal(err)
			}
			var conv *fakeConversation
			b.locked(func() {
				a := b.agents[agent.ID]
				conv = a.live.conn.Conv.(*fakeConversation)
				a.live.conn.Conv = &oldRangeConversation{conv}
			})
			if _, err := b.SendWith(agent.ID, "0:00–0:01 in standup.wav: check this", "range-old", []string{chip.ID}); wireCode(err) != appwire.CodeUnknownMethod {
				t.Fatalf("old host refusal = %v", err)
			}
			conv.mu.Lock()
			submits := conv.submits
			conv.mu.Unlock()
			if submits != 0 {
				t.Fatal("an old host was sent the range")
			}
			if how == "remove" {
				if err := b.RemoveAttachment(agent.ID, chip.ID); err != nil {
					t.Fatal(err)
				}
			} else if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(chip.Path); !os.IsNotExist(err) {
				t.Fatal("a definitely-unsent snapshot was retained")
			}
			rangeLeaks(t, b, agent.ID)
			if how == "remove" {
				if _, err := b.SendWith(agent.ID, "ordinary message", "plain-old", nil); err != nil {
					t.Fatalf("the old host lost ordinary text support: %v", err)
				}
				conv.mu.Lock()
				submits = conv.submits
				conv.mu.Unlock()
				if submits != 1 {
					t.Fatalf("ordinary text submissions = %d", submits)
				}
			}
		})
	}
}

func TestTheWireAndSessionAgreeOnTheLongestMediaRange(t *testing.T) {
	if appwire.MaxMediaRangeMS != session.MaxMediaRangeMS {
		t.Fatalf("wire range cap %d differs from session %d", appwire.MaxMediaRangeMS, session.MaxMediaRangeMS)
	}
}
