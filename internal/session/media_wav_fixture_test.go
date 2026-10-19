package session

import "encoding/binary"

// C10 keeps this shared fixture available to Windows tests without the Unix FIFO tests.
// rangeWAV is a PCM WAV of exactly ms milliseconds at 8 kHz mono 8-bit.
func rangeWAV(ms int) []byte {
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
