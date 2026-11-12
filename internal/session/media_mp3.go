package session

// mp3Frames recognizes consecutive complete MPEG Layer III frame headers.
// Raw speech streams often have no ID3 tag; neither their extension nor a
// requested response format proves their content. Two frames at their computed
// boundaries avoid treating a lone sync-like byte pair as recorded audio.
func mp3Frames(data []byte) bool {
	first, version, rate, ok := mp3Frame(data)
	if !ok || first+4 > len(data) {
		return false
	}
	second, nextVersion, nextRate, ok := mp3Frame(data[first:])
	return ok && nextVersion == version && nextRate == rate && first+second <= len(data)
}
func mp3Frame(data []byte) (length, version, rate int, ok bool) {
	if len(data) < 4 || data[0] != 0xff || data[1]&0xe0 != 0xe0 {
		return
	}
	version = int(data[1] >> 3 & 3)
	if version == 1 || data[1]>>1&3 != 1 || data[3]&3 == 2 {
		return
	} // reserved version/layer/emphasis
	bitrate := int(data[2] >> 4)
	sample := int(data[2] >> 2 & 3)
	if bitrate == 0 || bitrate == 15 || sample == 3 {
		return
	}
	rates := [3]int{44100, 48000, 32000}
	rate = rates[sample]
	v1 := [15]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	v2 := [15]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	scale, kbps := 144000, v1[bitrate]
	if version != 3 {
		scale, kbps = 72000, v2[bitrate]
		rate /= 2
		if version == 0 {
			rate /= 2
		}
	}
	length = scale*kbps/rate + int(data[2]>>1&1)
	ok = length >= 4 && length <= len(data)
	return
}
