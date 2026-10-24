package cas

import "testing"

// K3/K4 keep recorded source paths in their source grammar, independent of
// the computer that reads the attachment after a move.
func TestK3ReferenceKeepsSourcePathGrammar(t *testing.T) {
	ref := DigestBytes([]byte("carried bytes"))
	for _, source := range []string{"/Users/me/a.pdf", "C:/Users/me/a.pdf", "//server/share/a.pdf"} {
		encoded := Reference(ref, source)
		gotRef, got, ok := ParseReference(encoded)
		if !ok || gotRef != ref || got != source {
			t.Fatalf("round trip %q = %q, %q, %v", source, gotRef, got, ok)
		}
	}
}
