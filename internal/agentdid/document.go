package agentdid

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// Version is the identity document format understood by this build.
const Version = "v1"

// Key describes a public key without exposing its private half.
type Key struct {
	Type   string `json:"type"`
	Public string `json:"public"`
}

// Document keeps public keys and encrypted custody together as sealed truth.
// Unknown documents retain their original bytes, including future statements.
type Document struct {
	V                  string  `json:"v,omitempty"`
	DID                string  `json:"did"`
	Session            string  `json:"session"`
	Signing            *Key    `json:"signing,omitempty"`
	Encryption         *Key    `json:"encryption,omitempty"`
	Custody            custody `json:"custody,omitempty"`
	SigningUnavailable bool    `json:"signing_unavailable,omitempty"`
	pending            *Credentials
	raw                json.RawMessage
}

// custody uses URL-safe encoding so opaque ciphertext never resembles a path.
type custody []byte

func (c custody) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.RawURLEncoding.EncodeToString(c))
}

func (c *custody) UnmarshalJSON(raw []byte) error {
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	*c = decoded
	return nil
}

type documentWire Document

// UnmarshalJSON reads routing fields while retaining the complete document.
func (d *Document) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*d = Document{raw: append([]byte(nil), raw...)}
	_ = json.Unmarshal(fields["did"], &d.DID)
	_ = json.Unmarshal(fields["session"], &d.Session)
	if v, ok := fields["v"]; ok {
		if json.Unmarshal(v, &d.V) != nil || d.V == "" {
			d.V = "unknown"
		}
	}
	if d.V != "" && d.V != Version {
		return nil
	}
	var wire documentWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	wire.raw = d.raw
	*d = Document(wire)
	return nil
}

// MarshalJSON never translates a document supplied by a newer writer.
func (d Document) MarshalJSON() ([]byte, error) {
	if d.Unknown() && len(d.raw) != 0 {
		return append([]byte(nil), d.raw...), nil
	}
	if d.V == Version && len(d.raw) != 0 {
		// Keep additive fields even when this version does not interpret them.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(d.raw, &fields); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(documentWire(d))
		if err != nil {
			return nil, err
		}
		var known map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &known); err != nil {
			return nil, err
		}
		for k, v := range known {
			fields[k] = v
		}
		return json.Marshal(fields)
	}
	return json.Marshal(documentWire(d))
}

// Unknown reports a version that this build must preserve without interpreting.
func (d Document) Unknown() bool { return d.V != "" && d.V != Version }

// PreserveJSON restores the original future document after an enclosing JSON
// encoder has indented it. This keeps its value byte-identical across meta writes.
func (d Document) PreserveJSON(encoded []byte) []byte {
	if !d.Unknown() || len(d.raw) == 0 {
		return encoded
	}
	marker := []byte(`"agent_identity":`)
	start := bytes.Index(encoded, marker)
	if start < 0 {
		return encoded
	}
	start += len(marker)
	for start < len(encoded) && (encoded[start] == ' ' || encoded[start] == '\n' || encoded[start] == '\t') {
		start++
	}
	var value json.RawMessage
	if json.NewDecoder(bytes.NewReader(encoded[start:])).Decode(&value) != nil {
		return encoded
	}
	result := append([]byte(nil), encoded[:start]...)
	result = append(result, d.raw...)
	return append(result, encoded[start+len(value):]...)
}

var (
	// ErrUnavailable means the identity cannot prove ownership of its signing key.
	ErrUnavailable = errors.New("agent identity: signing key unavailable")
	// ErrDocument means a future or inconsistent document cannot be used for custody.
	ErrDocument = errors.New("agent identity: unsupported or inconsistent document")
)

// NeedsProtection identifies documents that need their first custody write.
func (d Document) NeedsProtection() bool { return d.V == "" || (d.V == Version && len(d.Custody) == 0) }

// Persist returns an encrypted document without mutating a running agent's copy.
func (d Document) Persist(cellKey []byte) (Document, error) {
	if !d.NeedsProtection() {
		return d, nil
	}
	keys := d.pending
	if d.V == "" {
		var err error
		d, keys, err = Upgrade(d)
		if err != nil {
			return Document{}, err
		}
	}
	if err := d.Protect(keys, cellKey); err != nil {
		return Document{}, err
	}
	d.pending = nil
	return d, nil
}
