package teams

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"
)

// Members preserve fields written by later builds, just as teams do. A missing
// join boundary stays absent instead of writing a misleading year-one timestamp.
type memberFields Member
type wireMember struct {
	memberFields
	JoinedAt *time.Time `json:"joined_at,omitempty"`
}

var knownMemberFields = map[string]bool{
	"key": true, "file": true, "where": true, "word": true, "handle": true,
	"home": true, "independent": true, "joined_at": true, "started": true, "handle_by": true,
}

func (m *Member) UnmarshalJSON(raw []byte) error {
	var w wireMember
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return err
	}
	*m = Member(w.memberFields)
	if w.JoinedAt != nil {
		m.JoinedAt = *w.JoinedAt
	}
	for k, v := range all {
		if !knownMemberFields[k] {
			if m.extra == nil {
				m.extra = map[string]json.RawMessage{}
			}
			m.extra[k] = v
		}
	}
	return nil
}

func (m Member) MarshalJSON() ([]byte, error) {
	w := wireMember{memberFields: memberFields(m)}
	if !m.JoinedAt.IsZero() {
		at := m.JoinedAt
		w.JoinedAt = &at
	}
	raw, err := json.Marshal(w)
	if err != nil || len(m.extra) == 0 {
		return raw, err
	}
	keys := make([]string, 0, len(m.extra))
	for k := range m.extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.Write(raw[:len(raw)-1])
	for _, k := range keys {
		name, _ := json.Marshal(k)
		b.WriteByte(',')
		b.Write(name)
		b.WriteByte(':')
		b.Write(m.extra[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
