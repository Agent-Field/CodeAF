package session

import (
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// mentionTeamBudget and mentionChatBudget are how much of one reference the
// model is handed. A team is a short digest (members, handles, states, the
// recent traffic). A chat is its title, its state and an excerpt of its last
// reply, never the transcript. Both are rune budgets, the same cut the team
// digest already uses.
const (
	mentionTeamBudget = 800
	mentionChatBudget = 1536
	mentionReplyRunes = 400
)

// mentionNote is the block Submit appends when the person's words name a team
// or a conversation. It reads. It never messages, and it never wakes: no
// teamRouse, no AppendTraffic, no Submit on the conversation it names.
func (a *Agent) mentionNote(text string) string {
	if a == nil || (!strings.Contains(text, "●") && !strings.Contains(text, "@")) {
		return ""
	}
	teams, chats := mentionTokens(text)
	if len(teams) == 0 && len(chats) == 0 {
		return ""
	}
	profile := a.config.teamProfile()
	bucket := mentionBucket(a.config)
	self := a.config.transcriptPath()
	now := time.Now()
	var blocks []string
	seenTeam := map[string]bool{}
	for _, slug := range teams {
		if seenTeam[slug] {
			continue
		}
		seenTeam[slug] = true
		if block := mentionTeamBlock(profile, self, slug, now); block != "" {
			blocks = append(blocks, block)
		}
	}
	seenChat := map[string]bool{}
	for _, token := range chats {
		if seenChat[token] {
			continue
		}
		seenChat[token] = true
		if block := mentionChatBlock(profile, bucket, self, token, now); block != "" {
			blocks = append(blocks, block)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func mentionTeamBlock(profile, self, slug string, now time.Time) string {
	if profile == "" {
		return ""
	}
	file, err := teams.Load(profile)
	if err != nil || file == nil || len(file.Teams) == 0 {
		return ""
	}
	var team teams.Team
	found := false
	for _, t := range file.Teams {
		if TaskSlug(t.Name) == slug {
			team, found = t, true
			break
		}
	}
	if !found {
		return ""
	}
	log, _ := teams.ReadTraffic(profile, team.ID, "", 40)
	states := map[string]teams.MemberState{}
	for _, m := range team.Members {
		if m.Key == self || m.File == self {
			states[m.Key] = teams.MemberState{State: teams.StateRunning}
			continue
		}
		if st, ok := journalState(m.File, now); ok {
			states[m.Key] = st
		}
	}
	body := teams.Digest(team, states, recentOf(log), mentionTeamBudget)
	return "Team reference (" + team.Name + "):\n" + body
}

func mentionChatBlock(profile, bucket, self, token string, now time.Time) string {
	chat, ok := mentionFindChat(profile, bucket, self, token)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("Chat reference (@")
	b.WriteString(token)
	b.WriteString("):\n")
	if chat.title != "" {
		b.WriteString("Title: ")
		b.WriteString(chat.title)
		b.WriteByte('\n')
	}
	if st, ok := journalState(chat.file, now); ok && st.State != "" {
		b.WriteString("State: ")
		b.WriteString(st.State)
		b.WriteByte('\n')
	}
	if chat.reply != "" {
		b.WriteString("Last reply: ")
		b.WriteString(chat.reply)
		b.WriteByte('\n')
	}
	return mentionCut(strings.TrimRight(b.String(), "\n"), mentionChatBudget)
}

type mentionChatHit struct {
	file, title, reply string
}

func mentionFindChat(profile, bucket, self, token string) (mentionChatHit, bool) {
	if profile != "" {
		if file, err := teams.Load(profile); err == nil && file != nil {
			for _, t := range file.Teams {
				for _, m := range t.Members {
					if m.File == self || m.Key == self {
						continue
					}
					if mentionNames(token, m.Handle, m.Word) {
						return mentionHit(m.File, m.Word), true
					}
				}
			}
		}
	}
	if bucket == "" {
		return mentionChatHit{}, false
	}
	for _, row := range RecentSessions(bucket, 48) {
		file := row.File
		if file == self || file == "" {
			continue
		}
		if mentionNames(token, "", row.Title) {
			return mentionHit(file, row.Title), true
		}
	}
	return mentionChatHit{}, false
}

func mentionNames(token, handle, title string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	if handle != "" && strings.EqualFold(handle, token) {
		return true
	}
	return TaskSlug(title) == token
}

func mentionHit(file, title string) mentionChatHit {
	hit := mentionChatHit{file: file, title: strings.TrimSpace(title)}
	sum, ok := Peek(file)
	if !ok {
		return hit
	}
	if hit.title == "" {
		hit.title = strings.TrimSpace(sum.Title)
	}
	hit.reply = mentionCut(oneLine(sum.LastAssistant), mentionReplyRunes)
	return hit
}

func mentionBucket(c Config) string {
	if dir := strings.TrimSpace(c.Place.Dir); dir != "" {
		return filepath.Dir(dir)
	}
	file := strings.TrimSpace(c.SessionFile)
	if file == "" {
		return ""
	}
	dir := filepath.Dir(file)
	if filepath.Base(file) == placeTranscript {
		return filepath.Dir(dir)
	}
	return dir
}

func mentionTokens(text string) (teamSlugs, chatTokens []string) {
	const bullet = "●"
	for i := 0; i < len(text); i++ {
		if strings.HasPrefix(text[i:], bullet) && (i == 0 || !mentionWord(text[i-1])) {
			j := i + len(bullet)
			for j < len(text) && (mentionWord(text[j]) || text[j] == '-') {
				j++
			}
			slug := strings.ToLower(text[i+len(bullet) : j])
			if slug != "" {
				teamSlugs = append(teamSlugs, slug)
			}
			i = j - 1
			continue
		}
		if text[i] != '@' || (i > 0 && (mentionWord(text[i-1]) || text[i-1] == '.' || text[i-1] == '@')) {
			continue
		}
		j := i + 1
		for j < len(text) && (mentionWord(text[j]) || text[j] == '-' || text[j] == ':' || text[j] == '/') {
			j++
		}
		token := text[i+1 : j]
		if token == "" || strings.Contains(token, "/") {
			i = j - 1
			continue
		}
		low := strings.ToLower(token)
		if strings.HasPrefix(low, "team:") || strings.HasPrefix(low, "file:") {
			i = j - 1
			continue
		}
		low = strings.TrimPrefix(low, "chat:")
		if low != "" {
			chatTokens = append(chatTokens, low)
		}
		i = j - 1
	}
	return teamSlugs, chatTokens
}

func mentionWord(b byte) bool {
	return b == '_' || b == '-' ||
		(b >= '0' && b <= '9') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z')
}

func mentionCut(s string, budget int) string {
	if budget <= 0 || utf8.RuneCountInString(s) <= budget {
		return s
	}
	n := 0
	for i := range s {
		if n == budget-1 {
			return s[:i] + "…"
		}
		n++
	}
	return s
}
