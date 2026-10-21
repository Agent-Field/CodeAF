package pair

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// C1 and C10 keep a custom service's invitation and approval on that service,
// including a reverse proxy prefix and the app link emitted by its page.
func TestC10SelfHostedInvitationNeverNamesHostedPage(t *testing.T) {
	ref := LinkRef{Code: "k7m2q9xd", Key: bytes.Repeat([]byte{1}, linkKeySize)}
	for _, via := range []string{"http://127.0.0.1:8787", "https://sync.example/fabric"} {
		text := InviteLines(Invite{Ref: ref, Via: via, Check: "4821", ExpiresIn: 10 * time.Minute})
		if strings.Contains(text, LinkOrigin) || !strings.Contains(text, ref.URLAt(via)) || !strings.Contains(text, ApproveCommand(ref, via)) {
			t.Fatalf("custom invitation leaves the selected service: %s", text)
		}
		for _, link := range []string{ref.URLAt(via), "codeaf://pair?code=" + ref.Code + "&via=" + via + "#" + b64u.EncodeToString(ref.Key)} {
			got, err := ReadLink(link)
			if err != nil || got.Code != ref.Code || !bytes.Equal(got.Key, ref.Key) || LinkRelay(link) != via {
				t.Fatalf("custom page link %q: ref=%+v relay=%q err=%v", link, got, LinkRelay(link), err)
			}
		}
	}
}

// C10 leaves only the terminal token when an address cannot serve a web page.
func TestC10NonWebInvitationHasOnlyTerminalToken(t *testing.T) {
	ref := LinkRef{Code: "k7m2q9xd", Key: make([]byte, linkKeySize)}
	text := InviteLines(Invite{Ref: ref, Via: "not-a-web-address"})
	if strings.Contains(text, LinkOrigin) || strings.Contains(text, "Open this link") || !strings.Contains(text, ref.Token()) {
		t.Fatalf("non-web invitation printed a page: %s", text)
	}
}
