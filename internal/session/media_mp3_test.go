package session

import "testing"

func TestRawMP3SpeechIsRecognizedFromConsecutiveFrameHeadersNotItsName(t *testing.T) {
	// fff3c4c4 is the actual Kokoro MP3 header observed in the private live probe:
	// MPEG2 LayerIII,128kbit/s,24kHz. Only the public header is used here; these
	// zero-filled frame payloads test structural recognition, not audio decoding.
	header := []byte{0xff, 0xf3, 0xc4, 0xc4}
	data := make([]byte, 768)
	copy(data, header)
	copy(data[384:], header)
	if got := sniffMedia(data); got != "audio/mpeg" {
		t.Fatalf("raw frames unknown: %q", got)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"one complete frame", data[:384]}, {"truncated second frame", data[:767]},
		{"sync-like binary", []byte{0xff, 0xf3, 0, 0}}, {"file extension is not content", []byte("hello.mp3")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if mp3Frames(test.data) {
				t.Fatal("invented MP3 content")
			}
		})
	}
	for _, bad := range []byte{0x04, 0xf4, 0xcc} {
		changed := append([]byte(nil), data...)
		changed[2] = bad
		if mp3Frames(changed) {
			t.Fatalf("accepted invalid/free/unsupported frame bits %x", bad)
		}
	}
	changed := append([]byte(nil), data...)
	changed[385] = 0xfb // second frame claims a different MPEG version
	if mp3Frames(changed) {
		t.Fatal("unrelated headers were treated as consecutive audio")
	}
}
