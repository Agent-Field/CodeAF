package session

import (
	"errors"
	"io"
	"os"
)

var errObservedMediaLarge = errors.New("the media file is over its read limit")

// The identity comes from the SAME open file as the bytes, before a model can
// wait on them. A replacement during that wait must be changed, never shown as
// the file whose description was recorded. A growing file is bounded on read.
func readObservedMedia(path string, ceiling int64) ([]byte, MediaFact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, MediaFact{}, err
	}
	if !info.Mode().IsRegular() {
		return nil, MediaFact{}, errors.New("the media path is not a regular file")
	}
	if info.Size() > ceiling {
		return nil, MediaFact{}, errObservedMediaLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, MediaFact{}, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, MediaFact{}, errors.New("the media file could not be inspected")
	}
	if info.Size() > ceiling {
		return nil, MediaFact{}, errObservedMediaLarge
	}
	fact := MediaFact{Canonical: canonicalFile(path), Bytes: info.Size(), ModNS: info.ModTime().UnixNano()}
	fact.CtimeNS, fact.Dev, fact.Ino = fileIdentity(info)
	data, err := io.ReadAll(io.LimitReader(file, ceiling+1))
	if err != nil {
		return nil, MediaFact{}, err
	}
	if int64(len(data)) > ceiling {
		return nil, MediaFact{}, errObservedMediaLarge
	}
	fact.Bytes = int64(len(data))
	return data, fact, nil
}
