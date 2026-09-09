package workspace

// ResolvedRef is a reading of a referenced record, never a second owner of its
// lifecycle. Adapters fill it from the conversation, task or standing store.
type ResolvedRef struct {
	Ref         Ref    `json:"ref"`
	Title       string `json:"title,omitempty"`
	State       string `json:"state,omitempty"`
	Phase       string `json:"phase,omitempty"`
	Location    string `json:"location,omitempty"`
	Available   bool   `json:"available"`
	Unavailable string `json:"unavailable,omitempty"`
}
