package tokens

import "testing"

// TestProgressCellGlyphs pins the four meanings in the run progress row to
// their geometric floor, Font Awesome spelling, and one-byte ASCII floor.
func TestProgressCellGlyphs(t *testing.T) {
	cases := []struct {
		name      string
		id        GlyphID
		plain     string
		nerd      string
		nfName    string
		ascii     string
		usualTint Token
	}{
		{"done", GDoneCell, "●", "\uF111", "nf-fa-circle", "#", Green},
		{"running", GRunningCell, "◐", "\uF042", "nf-fa-adjust", ">", Cyan},
		{"empty", GEmptyCell, "○", "\uF10C", "nf-fa-circle_o", ".", TextTertiary},
		{"failed", GFailedCell, "✘", "\uF05C", "nf-fa-times_circle_o", "x", Coral},
	}
	byID := make(map[GlyphID]GlyphBinding, len(Vocabulary()))
	for _, binding := range Vocabulary() {
		byID[binding.ID] = binding
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			binding, ok := byID[c.id]
			if !ok {
				t.Fatalf("slot %d is absent from Vocabulary", c.id)
			}
			if binding.Plain != c.plain || Plain.Glyph(c.id) != c.plain {
				t.Errorf("plain = %q / %q, want %q", binding.Plain, Plain.Glyph(c.id), c.plain)
			}
			if binding.NerdFont != c.nerd || NerdFont.Glyph(c.id) != c.nerd || binding.NFName != c.nfName {
				t.Errorf("nerd font = %q / %q (%s), want %q (%s)", binding.NerdFont, NerdFont.Glyph(c.id), binding.NFName, c.nerd, c.nfName)
			}
			if binding.ASCII != c.ascii || ASCII.Glyph(c.id) != c.ascii || len(c.ascii) != 1 {
				t.Errorf("ASCII = %q / %q, want one character %q", binding.ASCII, ASCII.Glyph(c.id), c.ascii)
			}
			if binding.Geometry {
				t.Error("progress cell carries state meaning and must be an icon slot, not geometry")
			}
			if binding.UsualTint != c.usualTint {
				t.Errorf("usual tint = %v, want %v", binding.UsualTint, c.usualTint)
			}
		})
	}
}
