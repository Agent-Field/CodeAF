package serve

import (
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// THE VALIDATION CONTRACT (a bot never acts beyond its own scope, nor beyond its creator):
//
//  1. A bot whose scope leaves `run` out (view, comment) cannot start a hosted chat. It is
//     refused with the server's sentence, the chats' host is never asked, and no chat is
//     registered for its creator.
//  2. A bot whose scope holds `run` (edit, full) within what its creator holds still starts one,
//     and the chat is its creator's, recorded as made by the bot.
//  3. The organization's policy narrows a bot too: when the policy's agent scope drops `run`, a
//     bot named `full` is refused as well.
//  4. A person is not asked any of this: the creator starts a chat as before.
func TestABotNarrowedBelowRunCannotStartAChat(t *testing.T) {
	rg := newHostedRig(t)
	view := rg.contractBot(t, "abir", "View bot", "view")
	comment := rg.contractBot(t, "abir", "Comment bot", "comment")
	edit := rg.contractBot(t, "abir", "Edit bot", "edit")
	full := rg.contractBot(t, "abir", "Full bot", "full")

	before := rg.host.count()
	for name, token := range map[string]string{"view": view.Token, "comment": comment.Token} {
		w := rg.botDo(t, token, http.MethodPost, hostedSessionsPath, -1, nil, http.StatusForbidden, nil)
		assertBotRefusal(t, w, hostedBotNeedsRun)
		if got := rg.host.count(); got != before {
			t.Fatalf("the %s bot's refused start reached the chats' host: %d requests", name, got-before)
		}
	}
	var mine struct{ Sessions, Shared []struct{ ID string } }
	rg.do(t, "abir", http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &mine)
	if len(mine.Sessions)+len(mine.Shared) != 0 {
		t.Fatalf("a refused start left a chat behind: %+v", mine)
	}

	for name, bot := range map[string]string{"edit": edit.Token, "full": full.Token} {
		var made struct{ ID string }
		rg.botDo(t, bot, http.MethodPost, hostedSessionsPath, -1, nil, http.StatusCreated, &made)
		row, found, err := rg.st.Resource(store.SessionKind, made.ID)
		if err != nil || !found || row.Owner != (acl.Subject{Kind: "person", ID: "abir"}) || row.CreatedBy.Kind != "bot" || row.CreatedBy.Sponsor != "abir" {
			t.Fatalf("the %s bot's chat = %+v, found %v, %v", name, row, found, err)
		}
	}

	rg.setAccessPolicy(t, func(p *acl.Policy) { p.Agents.DefaultScope = "view" })
	w := rg.botDo(t, full.Token, http.MethodPost, hostedSessionsPath, -1, nil, http.StatusForbidden, nil)
	assertBotRefusal(t, w, hostedBotNeedsRun)

	rg.do(t, "abir", http.MethodPost, hostedSessionsPath, -1, nil, http.StatusCreated, nil)
}
