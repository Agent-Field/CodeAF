//go:build !windows

package msgmodel

import "encoding/json"

// OpenRouter reports some provider failures in-band: a chunk whose `error`
// field is a bare object like
//
//	{"code":502,"message":"Network connection lost.",
//	 "metadata":{"error_type":"provider_unavailable"}}
//
// -- numeric `code`, no {"type":"error"} envelope, no nested `error` object.
// ParseStreamError cannot see it (it requires the envelope, with a string
// code), and when the same payload arrives wrapped in a Go error the
// `value.(error)` branch in FromError returns UnknownError before any parser
// runs. Either way the classification would not be an APIError, the run's
// structured classifier could not see a retryable provider failure, and one
// transient 502 would end the whole run.
//
// This recognizer classifies the shape; it deliberately sets no retry policy.
// StatusCode is carried through so the run layer can apply its bounded policy:
// transient statuses get a fresh turn while permanent 4xx errors fail fast.
func openRouterInBandAPIError(raw []byte) *APIError {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var probe struct {
		Code     *float64        `json:"code"`
		Message  *string         `json:"message"`
		Metadata json.RawMessage `json:"metadata"`
		// A {"type":...} or nested {"error":...} envelope means this is not
		// the bare in-band shape; leave those to ParseStreamError.
		Type  *string         `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	if probe.Code == nil || probe.Message == nil || probe.Type != nil || len(probe.Error) > 0 {
		return nil
	}
	code := *probe.Code
	if code != float64(uint64(code)) || code < 100 || code > 599 {
		return nil
	}
	status := uint64(code)
	body := string(raw)
	result := &APIError{
		Message:      *probe.Message,
		StatusCode:   &status,
		ResponseBody: &body,
	}
	if len(probe.Metadata) > 0 {
		result.Metadata = RawObject(probe.Metadata)
	}
	return result
}
