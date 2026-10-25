package clientrun

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"golang.org/x/crypto/nacl/box"
)

const recipientLabel = "codeaf-client-boxes-v1\n"

// SealForExecutors gives each eligible executor its own anonymous box without
// sharing private keys. The client's signed outer request covers all boxes;
// the relay's atomic claim still permits only one executor to run an any call.
func SealForExecutors(plain []byte, keys map[string][]byte) ([]byte, error) {
	if len(keys) == 0 || len(keys) > 8 {
		return nil, errors.New("choose one to eight executor keys")
	}
	sealed := map[string][]byte{}
	for id, key := range keys {
		if len(key) != 32 {
			return nil, errors.New("executor encryption key must be 32 bytes")
		}
		value, err := box.SealAnonymous(nil, plain, (*[32]byte)(key), rand.Reader)
		if err != nil {
			return nil, err
		}
		sealed[id] = value
	}
	raw, err := json.Marshal(sealed)
	if err != nil {
		return nil, err
	}
	raw = append([]byte(recipientLabel), raw...)
	if len(raw) > clientproto.MaxQueueBytes {
		return nil, errors.New("sealed call exceeds the queue payload limit")
	}
	return raw, nil
}

func executorBox(raw []byte, writer string) ([]byte, error) {
	if !bytes.HasPrefix(raw, []byte(recipientLabel)) {
		return raw, nil
	}
	var recipients map[string][]byte
	if err := strictJSON(raw[len(recipientLabel):], &recipients); err != nil {
		return nil, err
	}
	sealed, ok := recipients[writer]
	if !ok {
		return nil, errors.New("queued call is not sealed to this executor")
	}
	return sealed, nil
}
