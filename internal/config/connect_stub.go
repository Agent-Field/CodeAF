// STUB(connect): replaced by the owner branch on merge.
package config

// The two rows a Google connection is signed with, and the reader that resolves
// them out of a profile. The registry rows themselves — labels, hints, the
// secret flag that masks the second one — belong to the owner branch; this file
// is the keys and the accessor, so the settings skin over them (internal/tui3's
// settings.go) can name them.

const (
	// KeyGoogleOAuthClient identifies aforge to Google when a person connects an
	// account.
	KeyGoogleOAuthClient = "google_oauth_client"
	// KeyGoogleOAuthSecret is the secret that goes with it. It is a credential,
	// so the registry row carries Secret and this profile never shows it twice.
	KeyGoogleOAuthSecret = "google_oauth_secret"
)

// GoogleOAuthClientAt is the pair as this profile has it, empty when it has
// neither.
func GoogleOAuthClientAt(profileDir string) (id, secret string) { return "", "" }
