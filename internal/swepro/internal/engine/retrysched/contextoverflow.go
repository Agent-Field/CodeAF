package retrysched

import (
	"encoding/json"
	"regexp"
	"strings"
)

var contextOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),
	regexp.MustCompile(`(?i)input is too long for requested model`),
	regexp.MustCompile(`(?i)exceeds the context window`),
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),
	regexp.MustCompile(`(?i)reduce the length of the messages`),
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),
	regexp.MustCompile(`(?i)exceeds the limit of \d+`),
	regexp.MustCompile(`(?i)exceeds the available context size`),
	regexp.MustCompile(`(?i)greater than the context length`),
	regexp.MustCompile(`(?i)context window exceeds limit`),
	regexp.MustCompile(`(?i)exceeded model token limit`),
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),
	regexp.MustCompile(`(?i)request entity too large`),
	regexp.MustCompile(`(?i)context length is only \d+ tokens`),
	regexp.MustCompile(`(?i)input length.*exceeds.*context length`),
	regexp.MustCompile(`(?i)prompt too long; exceeded (max )?context length`),
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),
	regexp.MustCompile(`(?i)model_context_window_exceeded`),
}

var contextOverflowNoBody = regexp.MustCompile(`(?i)^4(00|13)\s*(status code)?\s*\(no body\)`)

// IsContextOverflow mirrors provider/error.ts:isOverflow and the two
// non-regex checks in parseAPICallError.
func IsContextOverflow(err Err) bool {
	if err.Name == "ContextOverflowError" {
		return true
	}
	if !err.IsAPIError() {
		return false
	}
	if err.Data.StatusCode != nil && *err.Data.StatusCode == 413 {
		return true
	}
	message := ""
	if err.Data.Message != nil {
		message = strings.TrimSpace(*err.Data.Message)
	}
	for _, pattern := range contextOverflowPatterns {
		if pattern.MatchString(message) {
			return true
		}
	}
	if contextOverflowNoBody.MatchString(message) {
		return true
	}
	if err.Data.ResponseBody == nil {
		return false
	}
	var body struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal([]byte(*err.Data.ResponseBody), &body) == nil &&
		body.Error != nil && body.Error.Code == "context_length_exceeded"
}
