// Package paymentrefusal recognises the narrow response shape that proves a
// key authenticated but its account cannot fund a request.
package paymentrefusal

import (
	"net/http"
	"strings"
)

// Matches reports whether status and body describe an account that cannot pay.
// A bare 429 is pacing and stays false; only payment-bearing words or a known
// payment code turn that otherwise transient status into a terminal answer.
func Matches(status int, body []byte) bool {
	if status == http.StatusPaymentRequired {
		return true
	}
	if status != http.StatusTooManyRequests {
		return false
	}
	lower := strings.ToLower(string(body))
	compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(lower)
	for _, code := range []string{`"code":"1113"`, `"code":1113`} {
		if strings.Contains(compact, code) {
			return true
		}
	}
	for _, phrase := range []string{
		"insufficient balance", "insufficient quota", "no resource package",
		"recharge", "payment required", "billing", "out of credit", "no credit", "quota",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
