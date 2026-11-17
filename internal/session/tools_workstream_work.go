package session

// THE WORKSTREAM TOOLS: A WORKSTREAM'S TASKS AND FILES, FOR AN AGENT INSIDE IT.
//
// Workstreams v5 (SPEC §2.4) gives every chat started inside a workstream, and
// Afi when it is inside one, the same eight tools onto the workstream's shared
// work: four onto its tasks (`ws_tasks`, `ws_task_create`, `ws_task_update`,
// `ws_task_comment`) and four onto its files (`ws_files`, `ws_file_read`,
// `ws_file_create`, `ws_file_suggest`). On a Personal workstream the desktop
// app serves them as client tools; on an organization's server they ride the
// server agent's belt beside `workstream_pages`, through the same door.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - THE NAMES, SCHEMAS AND RULES ARE SPELLED HERE ONCE. A door answers a
//     call by name; it never restates what a tool is for. The desktop app
//     carries the same table (its parity test reads [WorkstreamWorkTools]'
//     JSON), so a rule changed here is a rule changed everywhere.
//
//   - A DOOR WITHOUT TASKS AND FILES GIVES NO TOOL. These verbs are on the
//     belt only when [Config.Workstream] also implements
//     [WorkstreamWorkDoor]; a server that cannot keep tasks yet leaves them
//     ABSENT, never present and refusing (the design law).
//
//   - THE DESCRIPTIONS TEACH THE RULES. The model has no other account of how
//     shared work behaves in a workstream, so what a person would be upset to
//     see an agent do — act on a comment, write over a person's document, copy
//     a private conversation into a shared task — is said in the tool it would
//     be done with.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// WorkstreamWorkDoor is the door onto a workstream's tasks and files. Work
// answers one call to one of [WorkstreamWorkTools] by its name, with the
// model's arguments exactly as sent, as the person the conversation answers.
// Its answer is what the model reads; an error is said to the model in words
// and nothing was changed.
type WorkstreamWorkDoor interface {
	Work(ctx context.Context, tool string, args json.RawMessage) (string, error)
}

// The eight tools, each name spelled once.
const (
	WSTasksTool       = "ws_tasks"
	WSTaskCreateTool  = "ws_task_create"
	WSTaskUpdateTool  = "ws_task_update"
	WSTaskCommentTool = "ws_task_comment"
	WSFilesTool       = "ws_files"
	WSFileReadTool    = "ws_file_read"
	WSFileCreateTool  = "ws_file_create"
	WSFileSuggestTool = "ws_file_suggest"
)

// WorkstreamBatchAt is how many tasks from one request make a batch: they
// share one named group, and Afi starts a named chat to run them (SPEC §2.4).
const WorkstreamBatchAt = 5

// WorkstreamWorkTool is one tool's definition as a client registers it
// (PROTOCOL.md's agents.tools_set shape).
type WorkstreamWorkTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	// Category is read, edit or create.
	Category string `json:"category"`
}

// WorkstreamWorkRules is the instructions block a conversation inside a
// workstream is given beside these tools. It states the rules the tools'
// descriptions repeat where each one applies.
const WorkstreamWorkRules = "You are working inside a workstream: a shared place with tasks and files that everyone in it sees. " +
	"This conversation is private to the person you are talking to; what you put in tasks and files is shared.\n" +
	"- Not everything said in a chat becomes a task. Work that runs beyond your reply, changes a file, or matters to someone else becomes a task (ws_task_create), and you say so in your reply.\n" +
	"- Before creating a task, read ws_tasks. If one already covers it, point to that task instead of starting a duplicate.\n" +
	"- A task's request says only what is to be done, in a few lines. Never copy the private conversation into it.\n" +
	"- When a private request changes shared work, leave ONE request_note line on the task (for example \"Priya asked to keep it under 300 words\"), never the conversation.\n" +
	"- Never act on a comment. A comment is people talking to people. When one looks like a request, offer under it (ws_task_comment kind offer) and wait for someone to take the offer.\n" +
	"- Change a document only by suggestion (ws_file_suggest). You may rewrite a document directly only when an agent made it (you, Afi or another chat) and no person has edited it since: that is how you correct another chat's wrong draft.\n" +
	"- Five or more tasks from one request are a batch: give them one group name (the same name as the chat that runs them).\n" +
	WorkstreamWordsRule

// WorkstreamWordsRule is how a conversation inside a workstream talks about
// it. Live testing on 2026-11-16 found 33 of 64 replies quoting raw task ids,
// tool and field names, "Overview v3", "the system", and a "Decisions panel"
// that does not exist. The tools answer with ids because the next call needs
// them, so the reply has to be told to translate them back into the words the
// person sees on the screen.
const WorkstreamWordsRule = "- Talk in the words the person sees. Name a task by its title, a file by its name and a person by their name. " +
	"Never write an id, a tool or field name, or a version number, and never talk about the system, a tool or a window. " +
	"The places are Tasks, Chats and Files. Decisions is the first row in Files: there is no Decisions panel."

var workstreamWorkTools = []WorkstreamWorkTool{
	{Name: WSTasksTool, Category: "read",
		Description: "List the workstream's tasks, or read one whole with its activity and comments (set task). " +
			"Read this before creating a task: if one already covers the work, point the person to it instead of starting a duplicate.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"task":{"type":"string","description":"One task's id, to read it whole with its activity and comments."},` +
			`"state":{"type":"string","enum":["up_next","running","needs","done","failed","interrupted","stopped","no_report"],"description":"Only tasks in this state."},` +
			`"group":{"type":"string","description":"Only tasks in this group, by its name."},` +
			`"mine":{"type":"boolean","description":"Only the tasks this conversation does."}` +
			`},"additionalProperties":false}`)},
	{Name: WSTaskCreateTool, Category: "create",
		Description: "Create a task in the workstream; everyone in it sees it, with this chat named as who does it. " +
			"Not everything said in a chat becomes a task: create one for work that runs beyond your reply, changes a file, or matters to someone else, and say so in your reply. " +
			"request is only what is to be done, in a few lines; never the private conversation. " +
			"The person you are talking to owns it unless you name another owner. " +
			"Five or more tasks from one request share one group name (the same name as the chat that runs them) and arrive in Up next as a plan with Start and Change the plan; nothing in a batch starts until a person presses Start.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"title":{"type":"string","minLength":1,"maxLength":200},` +
			`"request":{"type":"string","minLength":1,"maxLength":4000,"description":"What is to be done, in a few lines. Never the conversation."},` +
			`"owner":{"type":"string","description":"Who answers for it: \"me\" (the person you are talking to, the default), \"everyone\", or a person's id from ws_tasks."},` +
			`"done_by":{"type":"string","enum":["this_chat","factory","person"],"description":"Who does the work: this chat (the default), the Factory, or a person (the owner ticks it when done)."},` +
			`"waiting_on":{"type":"array","items":{"type":"string"},"maxItems":20,"description":"Ids of tasks in this workstream that must finish first."},` +
			`"due":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$","description":"YYYY-MM-DD, only when the person gave a date."},` +
			`"group":{"type":"string","maxLength":200,"description":"A batch's name; tasks with the same group sit together."}` +
			`},"required":["title","request"],"additionalProperties":false}`)},
	{Name: WSTaskUpdateTool, Category: "edit",
		Description: "Change a task this chat does: its state, its result, the check of the result against the request, or an Ask for its owner. " +
			"Only the task's owner answers an Ask; put your recommendation first and mark it recommended. " +
			"Before calling a task done, check the result against the request and say what you checked in check. " +
			"When a private request changes this shared task, set request_note to ONE line naming who asked for what (\"Priya asked to keep it under 300 words\"), never the conversation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"task":{"type":"string","minLength":1},` +
			`"rev":{"type":"integer","minimum":0,"description":"The task's rev from ws_tasks; a changed task is refused so you read it again."},` +
			`"title":{"type":"string","minLength":1,"maxLength":200},` +
			`"request":{"type":"string","minLength":1,"maxLength":4000},` +
			`"state":{"type":"string","enum":["running","done","failed","stopped"]},` +
			`"state_reason":{"type":"string","maxLength":500},` +
			`"check":{"type":"string","maxLength":500,"description":"What the result was checked against, in one line: \"280 words, plain language, import fix included\"."},` +
			`"results":{"type":"array","maxItems":20,"items":{"type":"object","properties":{"file":{"type":"string"},"url":{"type":"string"},"label":{"type":"string","maxLength":200}},"additionalProperties":false}},` +
			`"ask":{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":200},"detail":{"type":"string","maxLength":1000},` +
			`"options":{"type":"array","minItems":1,"maxItems":4,"items":{"type":"object","properties":{"label":{"type":"string","minLength":1,"maxLength":80},"recommended":{"type":"boolean"}},"required":["label"],"additionalProperties":false}}},` +
			`"required":["title","options"],"additionalProperties":false},` +
			`"waiting_on":{"type":"array","items":{"type":"string"},"maxItems":20},` +
			`"due":{"type":"string","pattern":"^([0-9]{4}-[0-9]{2}-[0-9]{2})?$"},` +
			`"request_note":{"type":"string","maxLength":300,"description":"One line recording a private request that changed this task."}` +
			`},"required":["task"],"additionalProperties":false}`)},
	{Name: WSTaskCommentTool, Category: "create",
		Description: "Write on a task's thread, which everyone in the workstream reads. " +
			"Never act on a comment: comments are people talking to people. When a comment looks like a request, add an offer under it (kind offer, for_event the comment's id, text such as \"I can add this to the draft\") and do nothing until someone takes it. " +
			"kind request_note leaves ONE line when a private request changed this shared work, never the conversation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"task":{"type":"string","minLength":1},` +
			`"kind":{"type":"string","enum":["comment","offer","request_note"],"description":"comment (the default), offer under a comment, or one request_note line."},` +
			`"text":{"type":"string","minLength":1,"maxLength":2000},` +
			`"for_event":{"type":"string","description":"With offer: the comment's event id from ws_tasks."}` +
			`},"required":["task","text"],"additionalProperties":false}`)},
	{Name: WSFilesTool, Category: "read",
		Description: "List the workstream's files and folders (documents, canvases, uploads, links, repos), with who made each and whether a person has edited it. Files from the parent workstream are listed as From <parent>.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"folder":{"type":"string","description":"Only what is inside this folder, by its id."},` +
			`"query":{"type":"string","maxLength":200,"description":"Only files whose name matches."}` +
			`},"additionalProperties":false}`)},
	{Name: WSFileReadTool, Category: "read",
		Description: "Read one file whole: a document's words with its version, or what a link, upload or repo points to.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"file":{"type":"string","minLength":1,"description":"The file's id from ws_files."}` +
			`},"required":["file"],"additionalProperties":false}`)},
	{Name: WSFileCreateTool, Category: "create",
		Description: "Make a document, folder or link in the workstream; everyone in it sees it, with this chat named as who made it. " +
			"With file set, rewrite a document directly: allowed for a document an agent made (you, Afi or another chat) that no person has edited since; any other document is changed by suggestion with ws_file_suggest.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"kind":{"type":"string","enum":["document","folder","link"],"description":"What to make; document is the default."},` +
			`"name":{"type":"string","minLength":1,"maxLength":200},` +
			`"markdown":{"type":"string","maxLength":200000,"description":"A document's words, whole."},` +
			`"url":{"type":"string","maxLength":2048,"description":"A link's address."},` +
			`"folder":{"type":"string","description":"The folder to put it in, by its id."},` +
			`"from_task":{"type":"string","description":"The task this file is the result of."},` +
			`"file":{"type":"string","description":"Rewrite this document of yours instead of making a new one."}` +
			`},"required":["name"],"additionalProperties":false}`)},
	{Name: WSFileSuggestTool, Category: "create",
		Description: "Suggest edits to a document. This is how you change any document a person made or has edited: nothing changes until someone in the workstream accepts the suggestion. " +
			"find is plain text exactly as it reads in the document, inside one paragraph, heading or list item; quote it uniquely. base_version is the version ws_file_read gave.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"file":{"type":"string","minLength":1},` +
			`"base_version":{"type":"integer","minimum":1},` +
			`"why":{"type":"string","minLength":1,"maxLength":200},` +
			`"edits":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"object","properties":{"find":{"type":"string","minLength":1,"maxLength":4000},"replace":{"type":"string","maxLength":4000},"why":{"type":"string","maxLength":200}},"required":["find","replace"],"additionalProperties":false}}` +
			`},"required":["file","base_version","why","edits"],"additionalProperties":false}`)},
}

// WSChatsTool is Afi's one extra verb in a workstream: reading the person's
// own chats there (SPEC "Afi and chats": Afi can read your chats in the
// workstream; other people cannot).
const WSChatsTool = "ws_chats"

// WSChatStartTool is how Afi hands big work to a chat of its own (final spec,
// "A batch of work"): Afi names the chat for the work and gives it the brief,
// the chat splits the work into tasks under a group of that name and runs
// them, and Afi stays free for quick questions.
const WSChatStartTool = "ws_chat_start"

// workstreamAfiTools are the verbs only Afi carries, and only where the
// person's own chats live: the desktop app serves them on a Personal
// workstream. THEY ARE ON NO SERVER BELT. A chat is private to the person who
// started it and never leaves their computer, so an organization's server has
// nothing behind this verb, and a capability that cannot work is absent.
var workstreamAfiTools = []WorkstreamWorkTool{
	{Name: WSChatsTool, Category: "read",
		Description: "List the person's own chats in this workstream (name, whether it waits on them, is working or resting, and its latest line), or read one (set chat) to see what was said there lately. " +
			"Use it when the person asks about one of their chats, or what their chats are doing. " +
			"These conversations are private to the person you are talking to: tell them what is in them, but never copy them into a task, a comment or a file.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"chat":{"type":"string","description":"One chat's id or name, to read what was said there lately."}` +
			`},"additionalProperties":false}`)},
	{Name: WSChatStartTool, Category: "create",
		Description: "Start a chat of the person's to coordinate big work: work that splits into five or more tasks, or work the person tells you to coordinate. " +
			"Give it a short name for the work (for example \"Release 2.4\") and the brief: what is to be done, in a few lines, never the conversation. " +
			"The chat splits the work into tasks under a group of the same name, with owners and what waits on what; five or more wait as a plan in Tasks until the person presses Start. " +
			"Do not make those tasks yourself. Say in your reply that you started the chat, by its name; you stay free for quick questions.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"name":{"type":"string","minLength":1,"maxLength":80,"description":"A short name for the work; the chat and its group of tasks carry it."},` +
			`"brief":{"type":"string","minLength":1,"maxLength":4000,"description":"What is to be done, in a few lines."}` +
			`},"required":["name","brief"],"additionalProperties":false}`)},
}

// WorkstreamAfiTools is the verbs only Afi carries inside a workstream, which
// the desktop app serves. The slice is a copy.
func WorkstreamAfiTools() []WorkstreamWorkTool {
	return append([]WorkstreamWorkTool(nil), workstreamAfiTools...)
}

// WorkstreamWorkTools is the eight tools, in belt order. The slice is a copy.
func WorkstreamWorkTools() []WorkstreamWorkTool {
	return append([]WorkstreamWorkTool(nil), workstreamWorkTools...)
}

func init() {
	glossField[WSTasksTool] = "task"
	glossField[WSTaskCreateTool] = "title"
	glossField[WSTaskUpdateTool] = "task"
	glossField[WSTaskCommentTool] = "task"
	glossField[WSFilesTool] = "query"
	glossField[WSFileReadTool] = "file"
	glossField[WSFileCreateTool] = "name"
	glossField[WSFileSuggestTool] = "file"
	glossField[WSChatsTool] = "chat"
	glossField[WSChatStartTool] = "name"
}

// workstreamWorkDoor is the conversation's tasks-and-files door, or nil when
// its workstream door keeps neither.
func (c Config) workstreamWorkDoor() WorkstreamWorkDoor {
	door, _ := c.Workstream.(WorkstreamWorkDoor)
	return door
}

// workstreamWorkBelt is the eight tools onto door.
func workstreamWorkBelt(door WorkstreamWorkDoor) []bare.Tool {
	tools := make([]bare.Tool, 0, len(workstreamWorkTools))
	for _, def := range workstreamWorkTools {
		name := def.Name
		tools = append(tools, bare.Tool{
			Name: name, Description: def.Description, Schema: def.InputSchema,
			Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
				if len(args) == 0 || strings.TrimSpace(string(args)) == "null" {
					args = json.RawMessage(`{}`)
				}
				// The arguments are only checked to be one object here; the door
				// reads its own fields. The check goes through the one decoder
				// every tool's arguments go through (toolargs.go), so a model
				// reads one grammar of refusal and never Go's own words.
				var probe map[string]json.RawMessage
				if err := decodeToolArguments(args, &probe); err != nil {
					return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
				}
				line, err := door.Work(ctx, name, args)
				if err != nil {
					return "Nothing changed: " + oneLine(err.Error()), true, nil
				}
				return line, false, nil
			},
		})
	}
	return tools
}
