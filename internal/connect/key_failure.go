package connect

import (
	"errors"
	"net"
	"strings"
)

// KeyFailure is a connection answer that a surface can render beside its field.
type KeyFailure struct {
	Kind  string
	Words string
}

func (e *KeyFailure) Error() string { return e.Words }

func keyFailure(kind string, err error, secret string) error {
	var network net.Error
	if errors.As(err, &network) {
		kind = "no_answer"
	}
	words := err.Error()
	if secret != "" {
		words = strings.ReplaceAll(words, secret, "[key]")
	}
	if kind == "no_answer" {
		words = "The service did not answer · nothing was saved"
	}
	runes := []rune(words)
	if len(runes) > 120 {
		words = string(runes[:120])
	}
	return &KeyFailure{Kind: kind, Words: words}
}

// ValidKeyEnv accepts the same names as the terminal's key reference reader.
func ValidKeyEnv(name string) bool {
	_, ok := envReference("$" + name)
	return ok
}
