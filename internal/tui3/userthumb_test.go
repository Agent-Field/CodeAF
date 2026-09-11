package tui3

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func userEntryRows(t *testing.T, a *app, width int) []string {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryUser {
			a.entries[i].stale = true
			rows := append([]string(nil), a.entryRows(a.conversation(), i, width)...)
			for _, r := range a.mediaRows(&a.entries[i], i, width, userLead) {
				rows = append(rows, r.text)
			}
			return rows
		}
	}
	t.Fatal("no user entry")
	return nil
}

func solidTestPicture(shade color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, shade)
		}
	}
	return img
}

func TestAUsersPictureMarkerRespectsDisabledPathLinks(t *testing.T) {
	dir := t.TempDir()
	path := writePicture(t, dir, "shot.png", wideTestPicture())
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.pathLinks = false
	e := entry{kind: entryUser, text: "look [#1 shot.png]", pictures: []string{path}, picturesHere: true}
	body := strings.Join(a.renderEntry(0, &e, 60), "\n")
	if strings.Contains(body, "\x1b]8;;") {
		t.Fatalf("a disabled marker became a terminal door: %q", body)
	}
	for _, r := range body {
		if r >= 0xE000 && r <= 0xF8FF {
			t.Fatalf("a disabled marker leaked its private mask: %q", body)
		}
	}
}

func strconvQuote(text string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(text, `\`, `\\`), `"`, `\"`) + `"`
}
