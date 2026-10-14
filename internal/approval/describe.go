package approval

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Description is what a permission card says about one call: the exact
// command and what it touches.
//
// IT IS DISPLAY ONLY. [Policy.Check] decides allow, prompt or deny and nothing
// on any allow path reads a Description; this only names the call so a person
// reads the command they are agreeing to instead of the tool's name.
type Description struct {
	Command string
	Touches []string
}

// commandLimit keeps a card readable and a presence file small. The text is
// cut, never rewritten, so what is shown is a prefix of what runs.
const commandLimit = 2000

// argsLimit is the tighter cut for a non-bash tool's raw arguments, which can
// carry a whole file body.
const argsLimit = 300

// Describe names one tool call for a permission card.
func Describe(tool string, args json.RawMessage) Description {
	tool = strings.TrimSpace(tool)
	if tool != ToolBash {
		return describeTool(tool, args)
	}
	command, ok := bashCommand(args)
	if !ok {
		return Description{Command: tool}
	}
	d := Description{Command: clip(command, commandLimit)}
	segments, _ := splitSegments(command)
	if cli, ok := accountCLI(segments); ok {
		d.Touches = []string{accountTouch(cli)}
	}
	return d
}

// describeTool covers every tool that is not bash: the tool name and its
// arguments, and the path or host the arguments name.
func describeTool(tool string, args json.RawMessage) Description {
	text := strings.TrimSpace(string(args))
	command := tool
	if text != "" && text != "null" && text != "{}" {
		command = tool + " " + clip(text, argsLimit)
	}
	d := Description{Command: command}
	var fields struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		URL      string `json:"url"`
	}
	if json.Unmarshal(args, &fields) != nil {
		return d
	}
	how := " (writes)"
	if ReadOnly(tool, args) {
		how = " (reads)"
	}
	for _, path := range []string{fields.Path, fields.FilePath} {
		if strings.TrimSpace(path) != "" {
			d.Touches = append(d.Touches, path+how)
		}
	}
	if u, err := url.Parse(fields.URL); err == nil && u.Host != "" {
		d.Touches = append(d.Touches, u.Host+" · network")
	}
	return d
}

func clip(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// accountHosts names where an account CLI acts, in the words a card shows.
var accountHosts = map[string]string{
	"gh": "github.com", "hub": "github.com", "glab": "gitlab.com",
	"aws": "AWS", "gcloud": "Google Cloud", "gsutil": "Google Cloud", "bq": "Google Cloud",
	"az": "Azure", "doctl": "DigitalOcean",
}

// accountTouch says an account CLI acts as the person, and where.
func accountTouch(cli string) string {
	where := accountHosts[cli]
	if where == "" {
		where = cli
	}
	return where + " · as you"
}
