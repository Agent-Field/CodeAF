package appwire

// Unread (PROTOCOL.md): an agent says when it has said something new to the
// person since they last saw it, so a window can draw a dot on its row.
// agents.mark_seen is how the window says the person saw it. The mark is only
// written: it never opens or warms the agent, and never takes one out of
// Closed or the archive.

// FeatureAgentUnread names an engine that serves agents.mark_seen and fills
// Agent.Unread.
const FeatureAgentUnread = "agents.mark_seen"

// SeenMarker records that the person has seen an agent up to now.
type SeenMarker interface {
	MarkSeen(id string) error
}

func withUnreadFeature(h Hello, e Engine) Hello {
	if _, ok := e.(SeenMarker); ok {
		return withFeature(h, FeatureAgentUnread)
	}
	return h
}

func init() {
	methods["agents.mark_seen"] = typed(func(e Engine, p IDParams) (*Empty, error) {
		m, ok := e.(SeenMarker)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not keep seen marks")
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(m.MarkSeen(id))
	})
}
