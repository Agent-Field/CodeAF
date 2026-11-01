package identity_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// L4-A: Only the picker's own cache names are disposable startup state;
// similarly named personal files still keep both pairing checks conservative.
func TestL4Fix4PickerCacheIsStartupState(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"models.json", true},
		{"models-" + catalog.CacheKey("ollama", "http://localhost:11434/v1") + ".json", true},
		{"models-my-notes.json", false},
		{"models-0123456789abcdeF.json", false},
		{"models-0123.json", false},
		{"models-0123456789abcdef-more.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			startupFile(t, home, "v3/"+tc.name, `{}`)
			for _, check := range []func(string) (bool, error){identity.Joinable, identity.Pristine} {
				if got, err := check(home); err != nil || got != tc.want {
					t.Fatalf("startup check = %v, %v; want %v", got, err, tc.want)
				}
			}
		})
	}
}
