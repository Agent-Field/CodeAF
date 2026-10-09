package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// The desktop's model roles.
//
// The desktop app asks the person one question about models: "which model does
// each kind of job use?". The engine's own vocabulary answers it at a finer
// grain (a registry of two dozen auxiliary calls, grouped into tiers), which is
// right for a terminal user who tunes a crew and wrong for a settings screen.
// So the desktop has its own short list, in plain words, and each entry owns a
// fixed set of engine roles. A choice made here is written once, under one key,
// and read back by the engine as a pin on every role the entry owns.

// DesktopDefaultModel is what every desktop role runs on until its owner
// chooses otherwise.
const DesktopDefaultModel = "deepseek/deepseek-v4.1-flash"

// DesktopRoleConversation is the role that answers what the person types.
const DesktopRoleConversation = "conversation"

// DesktopRole is one thing the person can choose a model for.
type DesktopRole struct {
	// ID is the stable word the routes and the stored key use.
	ID string `json:"id"`
	// Name is the plain label a person reads.
	Name string `json:"name"`
	// Controls says in one sentence what the role's model does.
	Controls string `json:"controls"`
	// Engine lists the engine roles this entry pins. Conversation owns none:
	// it is the live model of the open chat.
	Engine []roles.Role `json:"-"`
}

var desktopRoles = []DesktopRole{
	{ID: DesktopRoleConversation, Name: "Conversation",
		Controls: "Answers what you type in a chat and decides when to start a task."},
	{ID: "tasks", Name: "Tasks",
		Controls: "Does the steps of a task: reading, editing, running commands.",
		Engine:   []roles.Role{roles.RoleWorker, roles.RoleCareful, roles.RoleDivision}},
	{ID: "planning", Name: "Planning",
		Controls: "Plans a task before it starts and rewrites the plan as steps finish.",
		Engine:   []roles.Role{roles.RolePlanner, roles.RoleDesigner, roles.RoleShaper}},
	{ID: "checking", Name: "Checking",
		Controls: "Decides whether finished-looking work is really finished, and has a second go when it is not.",
		Engine:   []roles.Role{roles.RoleAuditor, roles.RoleRepair}},
	{ID: "naming", Name: "Titles and summaries",
		Controls: "Writes the short names for chats, tasks and background jobs, and the one-line step captions.",
		Engine:   []roles.Role{roles.RoleTitle, roles.RoleTaskName, roles.RoleJobName, roles.RoleCaption}},
	{ID: "memory", Name: "Memory",
		Controls: "Reads each turn for things worth remembering and tidies what has been remembered.",
		Engine:   []roles.Role{roles.RoleReflex, roles.RoleConsolidate}},
	{ID: "routing", Name: "Turn routing",
		Controls: "Judges whether a message should become a task, reads long answers for parts, and fills in forms from what was said.",
		Engine: []roles.Role{roles.RoleRouter, roles.RoleRouterConfirm, roles.RoleMarkReader,
			roles.RoleHandoff, roles.RoleSpellOut, roles.RoleIntake}},
	{ID: "safety", Name: "Safety checks",
		Controls: "Reads one tool call and says whether it is safe to run without asking you, and decides what is worth telling you about.",
		Engine:   []roles.Role{roles.RoleGuardian, roles.RoleSentinel}},
}

// DesktopRoles lists the roles in the order a settings screen shows them.
func DesktopRoles() []DesktopRole { return append([]DesktopRole(nil), desktopRoles...) }

// DesktopRoleFor finds one role by its ID.
func DesktopRoleFor(id string) (DesktopRole, bool) {
	for _, role := range desktopRoles {
		if role.ID == id {
			return role, true
		}
	}
	return DesktopRole{}, false
}

// ErrUnknownDesktopRole is returned for an ID that names no role.
var ErrUnknownDesktopRole = errors.New("unknown model role")

func desktopRoleKey(id string) string { return "desktop.roles." + id }

// DesktopRoleChoice is what a role runs on: the model, the effort word (empty
// means the model's own default), and whether the person chose it.
func DesktopRoleChoice(profileDir, id string) (model, effort string, chosen bool) {
	if value, ok := persistedString(profileDir, desktopRoleKey(id)); ok && strings.TrimSpace(value) != "" {
		model, effort = roles.SplitEffort(value)
		return model, effort, true
	}
	if id == DesktopRoleConversation {
		if saved := ChatModelAt(profileDir); saved != "" {
			return saved, "", true
		}
	}
	return DesktopDefaultModel, "", false
}

// WriteDesktopRole records a choice. An empty model puts the role back on the
// default. The write is one transaction through the profile's one writer, so
// the rest of the file survives; the conversation role also mirrors the model
// into the key the terminal reads.
func WriteDesktopRole(profileDir, id, model, effort string) error {
	if _, ok := DesktopRoleFor(id); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownDesktopRole, id)
	}
	model, effort = strings.TrimSpace(model), strings.ToLower(strings.TrimSpace(effort))
	if effort != "" && !roles.ValidEffort(effort) {
		return fmt.Errorf("effort must be one of %s", strings.Join(roles.Efforts, ", "))
	}
	if strings.ContainsAny(model, " \t\r\n:") {
		return errors.New("that is not a model name")
	}
	updates := map[string]any{}
	if model == "" {
		updates[desktopRoleKey(id)] = removeProfileKey
		if id == DesktopRoleConversation {
			updates[KeyChatModel] = removeProfileKey
		}
		return writeProfileValues(profileDir, updates)
	}
	value := model
	if effort != "" {
		value += ":" + effort
	}
	updates[desktopRoleKey(id)] = value
	if id == DesktopRoleConversation {
		updates[KeyChatModel] = model
	}
	return writeProfileValues(profileDir, updates)
}

// DesktopRolesSource is the engine's role ladder for a desktop conversation:
// every engine role a desktop role owns answers with its owner's choice, or the
// default when nobody chose. Roles nothing owns are absent, so they fall to the
// ladder's floor, the conversation's model. It reads the profile's file on
// every call through the file memo, so a choice written by another process (the
// desktop bridge) reaches the next call of the role.
func DesktopRolesSource(profileDir string) func(string) (string, bool) {
	owner := map[string]string{}
	for _, role := range desktopRoles {
		for _, engine := range role.Engine {
			owner[roles.PinKey(engine)] = role.ID
		}
	}
	return func(key string) (string, bool) {
		id, ok := owner[key]
		if !ok {
			return "", false
		}
		model, effort, _ := DesktopRoleChoice(profileDir, id)
		if effort != "" {
			model += ":" + effort
		}
		return model, true
	}
}
