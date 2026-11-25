package manual

import (
	"strings"
	"testing"
)

// HOSTED CHATS ARE ASKED ABOUT IN THREE VOICES: the person who runs `--org`, the person who
// worries about where their code went, and the operator who turns it on. Each question, in
// the words somebody would type, has to reach the page that answers it, and the page has to say
// the sentence the program really prints, so a person who pastes it finds its meaning.
func TestHostedChatQuestionsReachTheirPage(t *testing.T) {
	asked := []struct {
		question string
		says     []string
	}{
		{"where does my chat run when I use codeaf chat --org", []string{"organization keeps", "not on your computer"}},
		{"how do I open a chat on the organization's server", []string{"codeaf chat --org", "--session"}},
		{"Sign in to the server first: set CODEAF_ORG_TOKEN", []string{"CODEAF_ORG_TOKEN", "codeaf attach"}},
		{"is my code on the organization's server when I use --org", []string{"folder that belongs to that one chat"}},
		{"can other people on the server see my hosted chats", []string{"There is no such chat."}},
		{"can the operator of the server read my chat files", []string{"not private from them", "not encrypted"}},
		{"if I disconnect from a hosted chat on the server does it keep running", []string{"goes on running there"}},
		{"You already have chats running; stop one first", []string{"stop one first"}},
		{"Waiting for a free slot on the server", []string{"Waiting for a free slot"}},
		{"This server cannot keep your chat apart from other people's", []string{"did not start one"}},
		{"This computer cannot keep commands inside this chat's folder so no command was run", []string{"no command was run"}},
		{"how do I turn on hosted chats for my organization", []string{"CODEAF_HOSTED_URL", "codeaf sessionhost probe"}},
		{"how do I stop or delete a chat that runs on the server", []string{"every process it started"}},
		{"The organization's model access was refused by its provider, so this chat paused", []string{"Ask the organization's operator to check the model key", "Your own model key was refused by its provider"}},
		{"can someone I shared my hosted chat with send messages in it", []string{"also send messages, files and images", "interrupt"}},
		{"if I give someone run access to my hosted chat whose GitHub identity does it use", []string{"acts with your GitHub identity, your files and your spending limits"}},
		{"Only its owner can do that", []string{"Only its owner can do that"}},
		{"you were not given permission to send messages in it", []string{"you were not given permission to send messages in it", "keyboard"}},
		{"I can watch this chat but was not given permission to steer it", []string{"Ask its owner", "keyboard"}},
		{"Your access to this chat was removed.", []string{"Your access to this chat was removed."}},
		{"my hosted chat says it reached its spending limit and is paused, is my openrouter account out of credit", []string{"exactly as written", "never name a model service", "$0.001"}},
		{"who pays when a guest steers my hosted chat", []string{"approximate", "owner's spending limits"}},
		{"can the operator open my hosted chat", []string{"only metadata", "There is no such chat."}},
		{"can a bot start a hosted chat", []string{"a bot cannot use this yet", "recorded as made by the bot"}},
		{"may I still start a chat on my own computer after joining an organization", []string{"local_sessions", "switch to your personal account"}},
		{"can someone I shared my hosted chat with approve a command to run", []string{"approvals_by", "Only its owner can do that"}},
		{"how do I let people who can answer allow a tool call in a shared chat in the organization policy", []string{"approvals_by", "allow one command once"}},
		{"can a guest with view start my hosted chat when it is not running with codeaf chat --org --once", []string{"wake_by", "already open in a different interactive or headless mode"}},
		{"This chat is not running. Only its owner can start it.", []string{"wake_by", "Ask them to open it."}},
		{"how do I stop other people from starting a hosted chat that is not running in the organization policy", []string{"wake_by", "Starting it takes permission to send messages in it"}},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		var reached bool
		for _, section := range found {
			if section.Page != "organization-hosted-sessions" {
				continue
			}
			reached = true
			for _, needle := range ask.says {
				if !containsAny(found, "organization-hosted-sessions", needle) {
					t.Errorf("%q reached the hosted-chats page but none of its sections says %q", ask.question, needle)
				}
			}
			break
		}
		if !reached {
			pages := make([]string, 0, len(found))
			for _, section := range found {
				pages = append(pages, section.Page)
			}
			t.Errorf("%q should reach organization-hosted-sessions; it reached %v", ask.question, pages)
		}
	}
}

func containsAny(found []Section, page, needle string) bool {
	for _, section := range found {
		if section.Page == page && (strings.Contains(section.Body, needle) || strings.Contains(section.Title, needle)) {
			return true
		}
	}
	return false
}
