// Data URL decoder — port of src/util/data-url.ts:1-8
// (swe-pro 3b25a1a).
package util

import (
	"encoding/base64"
	"net/url"
	"strings"
)

func DecodeDataURL(value string) (string, error) {
	index := strings.IndexByte(value, ',')
	if index < 0 {
		return "", nil
	}
	head, body := value[:index], value[index+1:]
	if strings.Contains(head, ";base64") {
		decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(body, "="))
		if err != nil {
			// Buffer.from is permissive about whitespace and junk padding.
			decoded, err = base64.StdEncoding.DecodeString(body)
		}
		return string(decoded), err
	}
	return url.PathUnescape(body)
}
