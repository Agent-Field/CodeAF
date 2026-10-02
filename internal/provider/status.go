package provider

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// KeyExpiredFrom keeps the expiry fact after the engine wire has flattened a
// refusal into text, so hosted and local conversations refresh the same reading.
func KeyExpiredFrom(err error) bool {
	if err == nil {
		return false
	}
	if refusal, ok := RefusalFrom(err); ok {
		return refusal.KeyExpired()
	}
	status, ok := statusFromText(err.Error())
	return ok && status == http.StatusUnauthorized && strings.Contains(strings.ToLower(err.Error()), "expired")
}

// StatusOf returns the HTTP status a provider failure carried, including the
// status in the legacy `API error (N)` spelling. The fallback is centralized
// here because an in-band or wrapped failure can lose its structured status;
// retry and response boundaries must still make the same decision.
func StatusOf(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	if refusal, ok := RefusalFrom(err); ok && refusal.Status >= 100 && refusal.Status <= 599 {
		return refusal.Status, true
	}
	var statusErr interface{ ErrorStatusCode() (float64, bool) }
	if errors.As(err, &statusErr) {
		status, ok := statusErr.ErrorStatusCode()
		if ok && status >= 100 && status <= 599 && status == float64(int(status)) {
			return int(status), true
		}
	}
	text := err.Error()
	var detail interface{ ErrorDetail() string }
	if errors.As(err, &detail) {
		text += " " + detail.ErrorDetail()
	}
	return statusFromText(text)
}

func statusFromText(text string) (int, bool) {
	const marker = "api error ("
	text = strings.ToLower(text)
	at := strings.Index(text, marker)
	if at < 0 {
		return 0, false
	}
	rest := text[at+len(marker):]
	end := strings.IndexByte(rest, ')')
	if end <= 0 || end > 3 {
		return 0, false
	}
	status, err := strconv.Atoi(rest[:end])
	if err != nil || status < 100 || status > 599 {
		return 0, false
	}
	return status, true
}
