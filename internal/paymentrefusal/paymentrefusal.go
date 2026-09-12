// Package paymentrefusal recognises the narrow response shape that proves a
// key authenticated but its account cannot fund a request.
package paymentrefusal

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the meaning of a payment-shaped vendor response. The numeric code
// decides before prose because a plan-window message may also say that the
// account has insufficient balance for EXTRA usage.
type Kind int

const (
	Unknown Kind = iota
	Payment
	WindowExhausted
	NoPlan
	Pacing
)

// Matches reports whether status and body describe an account that cannot pay.
// A bare 429 is pacing and stays false; only payment-bearing words or a known
// payment code turn that otherwise transient status into a terminal answer.
func Matches(status int, body []byte) bool {
	return Classify(status, body) == Payment
}

// Classify separates an empty wallet, an exhausted subscription window, a
// door the key cannot use, and ordinary pacing. A vendor code is the stronger
// fact and a body carrying one never falls through to ambiguous prose.
func Classify(status int, body []byte) Kind {
	if status == http.StatusPaymentRequired {
		return Payment
	}
	if status != http.StatusTooManyRequests {
		return Unknown
	}
	if code, ok := responseCode(body); ok {
		switch code {
		case "1113":
			return Payment
		case "1308", "1310", "1316", "1317", "1318", "1319", "1320", "1321":
			return WindowExhausted
		case "1309", "1311", "1315":
			return NoPlan
		case "1302", "1305":
			return Pacing
		default:
			return Unknown
		}
	}
	lower := strings.ToLower(string(body))
	for _, phrase := range []string{
		"insufficient balance", "insufficient quota", "no resource package",
		"recharge", "payment required", "billing", "out of credit", "no credit", "quota",
	} {
		if strings.Contains(lower, phrase) {
			return Payment
		}
	}
	return Unknown
}

func responseCode(body []byte) (string, bool) {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return "", false
	}
	return codeIn(value)
}

func codeIn(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if code, ok := typed["code"]; ok {
			switch scalar := code.(type) {
			case string:
				return strings.TrimSpace(scalar), true
			case float64:
				return strconv.FormatFloat(scalar, 'f', -1, 64), true
			}
		}
		for _, child := range typed {
			if code, ok := codeIn(child); ok {
				return code, true
			}
		}
	case []any:
		for _, child := range typed {
			if code, ok := codeIn(child); ok {
				return code, true
			}
		}
	}
	return "", false
}

var resetPattern = regexp.MustCompile(`(?i)(?:will\s+)?reset\s+at\s+([^.;}\"]+)`)

// ResetAt returns the vendor's reset value when a window refusal names one.
// It returns only the value after "reset at" so a surface can compose its own
// sentence without copying an entire error envelope onto the status line.
func ResetAt(body []byte) string {
	match := resetPattern.FindSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(string(match[1]))
}
