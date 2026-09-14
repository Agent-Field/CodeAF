package connect

import (
	"strings"
	"unicode"
)

const hiddenSignInFailure = "the sign-in came back wrong and nothing was connected"

// SignInFailureReason is owned here because both the session tool and the live
// surface cross this package, so there is exactly one place that decides which
// browser failure words may leave it. A SENTENCE NOBODY HERE WROTE IS NOT SHOWN.
func SignInFailureReason(err error) string {
	if err == nil {
		return hiddenSignInFailure
	}
	const vendorShape = "authorization error from server:"
	text := strings.TrimSpace(err.Error())
	lower := strings.ToLower(text)
	at := strings.LastIndex(lower, vendorShape)
	if at < 0 {
		return hiddenSignInFailure
	}
	reason := strings.TrimSpace(text[at+len(vendorShape):])
	code, description, _ := strings.Cut(reason, " ")
	description = strings.TrimSpace(description)
	if !plainVendorCode(code) || !plainVendorDescription(description) {
		return hiddenSignInFailure
	}
	code = strings.ReplaceAll(code, "_", " ")
	if description == "" {
		return code
	}
	return code + " " + description
}

func plainVendorCode(code string) bool {
	if code == "" || len(code) > 24 {
		return false
	}
	letter := false
	for _, r := range code {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r), r == '_', r == '-':
		default:
			return false
		}
	}
	return letter
}

func plainVendorDescription(description string) bool {
	if len(description) > 160 || strings.ContainsAny(description, "\r\n") {
		return false
	}
	run := 0
	for _, r := range description {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '-':
			run++
			if run > 24 {
				return false
			}
		case unicode.IsSpace(r), strings.ContainsRune(".,'’!?():;", r):
			run = 0
		default:
			return false
		}
	}
	return true
}
