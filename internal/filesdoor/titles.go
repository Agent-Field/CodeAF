package filesdoor

import (
	"context"
	"encoding/json"
	"slices"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// MaxChatTitles bounds a title request and the work needed to answer it.
const MaxChatTitles = 32

const maxChatTitleBytes = 512

func validTitleIDs(ids []string) bool {
	if len(ids) == 0 || len(ids) > MaxChatTitles {
		return false
	}
	for _, id := range ids {
		if !cell.ValidID(id) {
			return false
		}
	}
	return true
}

func (d *door) titles(ctx context.Context, body []byte) (reply, bool) {
	var ids []string
	if len(body) > MaxChatTitles*32 || json.Unmarshal(body, &ids) != nil || !validTitleIDs(ids) {
		return errorReply(codeBadRequest), false
	}
	titles := make(map[string]string, len(ids))
	if d.cfg.ChatTitle != nil {
		for _, id := range ids {
			if ctx.Err() != nil {
				return errorReply(codeInternal), false
			}
			title := d.cfg.ChatTitle(id)
			if len(title) > maxChatTitleBytes {
				end := maxChatTitleBytes
				for !utf8.RuneStart(title[end]) {
					end--
				}
				title = title[:end]
			}
			if title != "" {
				titles[id] = title
			}
		}
	}
	answer, err := json.Marshal(titles)
	if err != nil {
		return errorReply(codeInternal), false
	}
	return reply{typ: typeChatTitles | answerBit, parts: [][]byte{answer}}, false
}

// ChatTitles reads names over the same authenticated encrypted tunnel as a
// chat's frames. It neither opens a chat nor asks the relay to store a name.
func (s *Source) ChatTitles(ctx context.Context, ids []string) (map[string]string, error) {
	if !validTitleIDs(ids) {
		return nil, protocolf("invalid chat title request")
	}
	body, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	var titles map[string]string
	err = s.exchange(ctx, typeChatTitles, body, func(answer []byte) error {
		// JSON can use up to six bytes for each byte in a title.
		if len(answer) > MaxChatTitles*(32+6*maxChatTitleBytes) || json.Unmarshal(answer, &titles) != nil {
			return protocolf("invalid title response")
		}
		for id, title := range titles {
			if !slices.Contains(ids, id) || len(title) > maxChatTitleBytes {
				return protocolf("unexpected title in response")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return titles, nil
}
