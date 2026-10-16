package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// The bounds of asking for several pictures in one request. Every one is a cap
// on work or memory, checked BEFORE the paid call or BEFORE any allocation, and
// each has one name so the tool's schema, its refusal and its tests cannot
// disagree about the figure.
const (
	// ImageVariantsMax is the most pictures one generate_image call may ask for.
	ImageVariantsMax = 10
	// imageDecodedMax and imageDecodedTotalMax bound what a reply holding SEVERAL
	// pictures may decode to, one picture and all of them. A reply holding one
	// picture is bounded by the provider client's own response cap and gets no
	// new limit, so every single-picture call that worked still works.
	imageDecodedMax      = 32 << 20
	imageDecodedTotalMax = 64 << 20
	// imageNameSuffixes bounds how many numbered names are looked at for a free one.
	imageNameSuffixes = 1000
)

// newBatchID mints the identity of ONE provider request, once, before its
// outputs are written; it is persisted with every file the request produced, so
// it is the same after a restart.
func newBatchID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		sum := sha256.Sum256([]byte(time.Now().String()))
		copy(raw[:], sum[:])
	}
	return "bat_" + hex.EncodeToString(raw[:])
}

// imageCount is the n a call means: absent is one, and anything outside
// 1..[ImageVariantsMax] is a refusal the caller can fix without paying.
func imageCount(n int) (int, string) {
	switch {
	case n == 0:
		return 1, ""
	case n < 0 || n > ImageVariantsMax:
		return 0, fmt.Sprintf("n must be between 1 and %d", ImageVariantsMax)
	}
	return n, ""
}

// imageReplyRefusal says whether a reply of several pictures is too large to
// decode, from the encoded lengths alone — nothing is allocated to find out.
// A single picture is never refused here.
func imageReplyRefusal(items []provider.GeneratedImage) string {
	if len(items) < 2 {
		return ""
	}
	var total int64
	for _, item := range items {
		size := int64(base64.StdEncoding.DecodedLen(len(item.Base64)))
		if size > imageDecodedMax {
			return fmt.Sprintf("one of the %d images is over %s", len(items), mediaByteSize(imageDecodedMax))
		}
		total += size
	}
	if total > imageDecodedTotalMax {
		return fmt.Sprintf("the %d images together are over %s", len(items), mediaByteSize(imageDecodedTotalMax))
	}
	return ""
}

var errUnreadableImage = errors.New("unreadable image")

// saveImageVariant decodes ONE picture and writes it, so no more than one
// decoded picture is alive at a time. A name that was reserved for it and then
// not used is given back, and nothing is left behind for a picture that did not
// decode.
func saveImageVariant(gen ImageGen, parsed GenerateImageArgs, numbered bool, variant int, item provider.GeneratedImage) (string, []byte, error) {
	data, err := base64.StdEncoding.DecodeString(item.Base64)
	if err != nil || len(data) == 0 {
		return "", nil, errUnreadableImage
	}
	var path string
	// A name is OWNED once this call reserved it: the placeholder is ours to
	// replace, and to remove if the write fails. An explicit single path is the
	// caller's own file and keeps the write tool's overwrite behaviour.
	owned := strings.TrimSpace(parsed.Path) == "" || numbered
	if numbered && strings.TrimSpace(parsed.Path) != "" {
		path, err = claimNumberedImage(gen.Workspace, parsed.Path, provider.ImageExtension(data, item.MediaType), variant)
	} else {
		path, err = imageDestinationFrom(gen.Workspace, parsed.Path, parsed.Prompt, data, item.MediaType, gen.Directory)
	}
	if err != nil {
		return "", nil, err
	}
	if err := writeImageAtomic(path, data); err != nil {
		if owned {
			_ = os.Remove(path)
		}
		return "", nil, err
	}
	return path, data, nil
}

// claimNumberedImage reserves "<stem>-<variant><ext>" with O_EXCL, trying
// "<stem>-<variant>-2" and onward when that name is taken. A file that is not
// ours is never opened for writing, so no unrelated file is overwritten.
func claimNumberedImage(workspace, asked, extension string, variant int) (string, error) {
	path := strings.TrimSpace(asked)
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	path = filepath.Clean(path)
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	for suffix := 0; suffix < imageNameSuffixes; suffix++ {
		name := fmt.Sprintf("%s-%d", stem, variant)
		if suffix > 0 {
			name += fmt.Sprintf("-%d", suffix+1)
		}
		name += extension
		file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				return "", closeErr
			}
			return name, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("too many files share the name %s-%d", stem, variant)
}

// writeImageAtomic puts data at path through a temporary file in the same
// folder and a rename, so a reader sees the old file or the whole new one and
// a failed write leaves no partial picture. A link at path is followed, as the
// write tool's plain write would.
func writeImageAtomic(path string, data []byte) error {
	target := path
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			target = resolved
		}
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".codeaf-image-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	_, writeErr := temp.Write(data)
	closeErr := temp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
