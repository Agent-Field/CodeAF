package session

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

// The tests build their mp4s box by box, because the parser's whole job is the
// framing: a real render from the provider is megabytes of sample data around
// the same few dozen header bytes these fixtures state directly.

func mp4TestBox(kind string, payload []byte) []byte {
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
	copy(box[4:8], kind)
	copy(box[8:], payload)
	return box
}

// mp4TestMovieHeader is a version-0 mvhd payload: timescale 600 with a
// duration in those ticks, which is the layout every provider render so far
// has actually carried.
func mp4TestMovieHeader(timescale, duration uint32) []byte {
	payload := make([]byte, 20)
	binary.BigEndian.PutUint32(payload[12:16], timescale)
	binary.BigEndian.PutUint32(payload[16:20], duration)
	return payload
}

func mp4TestTrack(handler string) []byte {
	hdlr := make([]byte, 12)
	copy(hdlr[8:12], handler)
	return mp4TestBox("trak", mp4TestBox("mdia", mp4TestBox("hdlr", hdlr)))
}

func mp4TestFile(children ...[]byte) []byte {
	var moov []byte
	for _, child := range children {
		moov = append(moov, child...)
	}
	file := mp4TestBox("ftyp", []byte("isom0000"))
	file = append(file, mp4TestBox("moov", moov)...)
	return append(file, mp4TestBox("mdat", []byte("frames"))...)
}

func TestMP4FactsReadLengthAndSoundFromTheBoxes(t *testing.T) {
	film := mp4TestFile(
		mp4TestBox("mvhd", mp4TestMovieHeader(600, 6050)),
		mp4TestTrack("vide"),
		mp4TestTrack("soun"),
	)
	length, sound, ok := mp4Facts(film)
	if !ok {
		t.Fatal("a well-formed mp4 did not parse")
	}
	if !sound {
		t.Fatal("the sound track was not seen")
	}
	if want := 6050 * time.Second / 600; length != want {
		t.Fatalf("length = %v, want %v", length, want)
	}
}

func TestMP4FactsSayNoSoundWhenEveryTrackIsVideo(t *testing.T) {
	film := mp4TestFile(
		mp4TestBox("mvhd", mp4TestMovieHeader(600, 6050)),
		mp4TestTrack("vide"),
	)
	_, sound, ok := mp4Facts(film)
	if !ok || sound {
		t.Fatalf("ok=%v sound=%v, want a parsed and silent clip", ok, sound)
	}
}

func TestMP4FactsReadTheWideVersionOneHeader(t *testing.T) {
	payload := make([]byte, 32)
	payload[0] = 1
	binary.BigEndian.PutUint32(payload[20:24], 1000)
	binary.BigEndian.PutUint64(payload[24:32], 115125)
	film := mp4TestFile(mp4TestBox("mvhd", payload), mp4TestTrack("soun"))
	length, sound, ok := mp4Facts(film)
	if !ok || !sound {
		t.Fatalf("ok=%v sound=%v, want both", ok, sound)
	}
	if want := 115125 * time.Second / 1000; length != want {
		t.Fatalf("length = %v, want %v", length, want)
	}
}

// The emptiness law's exits: bytes that are not an mp4, a header that states
// no duration or an unbelievable one, the spec's unknown sentinels, and a
// track list the walk could not read whole all answer not-ok, so the note
// carries no fact nobody measured. The damaged-track cases are the ones that
// bite: "no sound track seen" must never masquerade as "without sound".
func TestMP4FactsRefuseWhatTheyCannotMeasure(t *testing.T) {
	lyingBox := []byte{0, 0, 0, 200, 'f', 'r', 'e', 'e', 1, 2, 3}
	sentinelV1 := make([]byte, 32)
	sentinelV1[0] = 1
	binary.BigEndian.PutUint32(sentinelV1[20:24], 1000)
	binary.BigEndian.PutUint64(sentinelV1[24:32], 0xFFFFFFFF)

	cases := map[string][]byte{
		"not an mp4 at all":                  []byte("MP4 and then some bytes standing in for a render"),
		"empty":                              nil,
		"no movie header":                    mp4TestFile(mp4TestTrack("soun")),
		"zero duration":                      mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 0))),
		"zero timescale":                     mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(0, 6050))),
		"unknown duration":                   mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 0xFFFFFFFF))),
		"32-bit sentinel in the wide header": mp4TestFile(mp4TestBox("mvhd", sentinelV1), mp4TestTrack("soun")),
		"over an hour":                       mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 600*4000)), mp4TestTrack("soun")),
		"under a tenth of a second":          mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 30)), mp4TestTrack("soun")),
		"truncated header":                   mp4TestFile(mp4TestBox("mvhd", make([]byte, 6))),
		"lying box size":                     {0, 0, 0, 200, 'm', 'o', 'o', 'v', 1, 2, 3},
		"size under its own header":          {0, 0, 0, 3, 'm', 'o', 'o', 'v', 0, 0, 0, 0},
		"no tracks at all":                   mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 6050))),
		"track without a handler": mp4TestFile(
			mp4TestBox("mvhd", mp4TestMovieHeader(600, 6050)),
			mp4TestBox("trak", mp4TestBox("mdia", nil))),
		"damage hiding a later sound track": mp4TestFile(
			mp4TestBox("mvhd", mp4TestMovieHeader(600, 6050)),
			mp4TestTrack("vide"),
			lyingBox,
			mp4TestTrack("soun")),
	}
	for name, data := range cases {
		if _, _, ok := mp4Facts(data); ok {
			t.Errorf("%s parsed as a measurable mp4", name)
		}
	}
}

func TestMediaLengthReadsLikeAClock(t *testing.T) {
	cases := map[time.Duration]string{
		time.Duration(10.083 * float64(time.Second)): "10.1s",
		2 * time.Second: "2.0s",
		time.Duration(115.125 * float64(time.Second)): "1m55s",
		10 * time.Minute: "10m00s",
		// The branch is taken on the rounded tenths, so nothing between 59.95
		// and 60 can print the "60.0s" the sub-minute rule forbids.
		time.Duration(59.96 * float64(time.Second)): "1m00s",
		time.Duration(59.94 * float64(time.Second)): "59.9s",
	}
	for length, want := range cases {
		if got := mediaLength(length); got != want {
			t.Errorf("mediaLength(%v) = %q, want %q", length, got, want)
		}
	}
}

// The note's whole sentence, both ways: measured facts spliced in when the
// bytes answer, and the plain sentence — no half-empty clause — when they do
// not.
func TestVideoNoteCarriesMeasuredFactsOnlyWhenMeasured(t *testing.T) {
	film := mp4TestFile(
		mp4TestBox("mvhd", mp4TestMovieHeader(600, 6050)),
		mp4TestTrack("vide"),
		mp4TestTrack("soun"),
	)
	note := describeGeneratedVideo("/work", "/work/clip.mp4", film, "film/model")
	if !strings.Contains(note, "10.1s with sound") {
		t.Fatalf("note %q does not carry the measured facts", note)
	}

	silent := mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 6050)), mp4TestTrack("vide"))
	note = describeGeneratedVideo("/work", "/work/clip.mp4", silent, "film/model")
	if !strings.Contains(note, "10.1s without sound") {
		t.Fatalf("note %q does not say the clip is silent", note)
	}

	note = describeGeneratedVideo("/work", "/work/clip.mp4", []byte("not an mp4"), "film/model")
	if strings.Contains(note, "with sound") || strings.Contains(note, "without sound") || strings.Contains(note, ", ,") {
		t.Fatalf("note %q states facts nobody measured", note)
	}
	if !strings.Contains(note, "of mp4 video, filmed on film/model") {
		t.Fatalf("note %q lost its plain shape", note)
	}
}
