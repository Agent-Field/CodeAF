// UTF-8 BOM helpers — port of src/util/bom.ts:4-30
// (swe-pro 3b25a1a).
package util

import (
	"os"
	"strings"
)

const bom = "\ufeff"

type BOMText struct {
	BOM  bool   `json:"bom"`
	Text string `json:"text"`
}

func SplitBOM(text string) BOMText {
	if !strings.HasPrefix(text, bom) {
		return BOMText{BOM: false, Text: text}
	}
	return BOMText{BOM: true, Text: strings.TrimPrefix(text, bom)}
}

func JoinBOM(text string, include bool) string {
	stripped := SplitBOM(text).Text
	if !include {
		return stripped
	}
	return bom + stripped
}

func ReadBOMFile(path string) (BOMText, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BOMText{}, err
	}
	return SplitBOM(string(data)), nil
}

func SyncBOMFile(path string, include bool) (string, error) {
	current, err := ReadBOMFile(path)
	if err != nil {
		return "", err
	}
	if current.BOM == include {
		return current.Text, nil
	}
	if err := Write(path, []byte(JoinBOM(current.Text, include))); err != nil {
		return "", err
	}
	return current.Text, nil
}
