package head

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// shutdownInstruction is the verbatim reason journaled on every command the
// whole-workspace quiet-down issues.
const shutdownInstruction = "shut it all down"

// cancelAllOptionValue and keepWorkOptionValue are the two answers to the one
// consequence gate "shut it all down" is allowed to raise.
const (
	cancelAllOptionValue = "services:cancel-all"
	keepWorkOptionValue  = "services:keep-work"
)

func (h *Head) manageService(user store.Message) (bool, error) {
	if recognizesShutdownAll(user.Body) {
		return true, h.shutDownEverything(user)
	}
	action, reference, explicit := serviceManagement(user.Body)
	if action == "status" {
		services, err := h.store.ActiveServices()
		if err != nil {
			return true, err
		}
		if len(services) == 0 {
			return true, h.postAgent(user.SessionID, "No services are running.", 0)
		}
		lines := make([]string, 0, len(services))
		for _, service := range services {
			lines = append(lines, fmt.Sprintf("%s is %s · %s", service.Name, service.Status, service.Health.Suffix()))
		}
		return true, h.postAgent(user.SessionID, strings.Join(lines, "\n"), 0)
	}
	if action == "" {
		return false, nil
	}
	var matches []store.Service
	var err error
	if action == "restart" {
		matches, err = h.store.SearchRestartableServices(reference)
	} else {
		matches, err = h.store.SearchServices(reference)
	}
	if err != nil {
		return true, err
	}
	if len(matches) == 0 {
		if explicit {
			return true, h.postAgent(user.SessionID, "I couldn't match that to a running service.", 0)
		}
		return false, nil
	}
	kind, known := serviceCommandKind(action)
	if !known {
		return false, nil
	}
	if len(matches) > 1 {
		options := make([]store.QuestionOption, 0, len(matches))
		for _, service := range matches {
			options = append(options, store.QuestionOption{
				Label: service.Name + " · " + service.Health.Suffix(),
				Value: "service:" + action + ":" + service.ID,
			})
		}
		return true, h.postQuestion(user.SessionID, "Which service do you mean?", 0, options)
	}
	return true, h.requestServiceCommand(user, kind, matches[0], action)
}

// recognizesShutdownAll is terse on purpose: only unmistakably total phrasings
// take the whole-workspace path, so "stop the dev server" still means one
// service and nothing else is swept up with it.
func recognizesShutdownAll(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	for _, phrase := range []string{
		"shut it all down", "shut everything down", "shut down everything",
		"stop everything", "stop it all", "stop all services", "stop all the services",
		"stop all servers", "kill everything",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// shutDownEverything stops every live service unconditionally — they are the
// user's own persistent effects — and asks exactly once before touching work
// that is still spending, quoting what that costs.
func (h *Head) shutDownEverything(user store.Message) error {
	services, err := h.store.ActiveServices()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(services))
	var lastSeq int64
	for _, service := range services {
		command, requestErr := h.store.RequestCommand(store.Command{
			SessionID: user.SessionID, Kind: store.CommandServiceStop,
			Target: service.ID, Instruction: shutdownInstruction,
		})
		if requestErr != nil {
			continue
		}
		names = append(names, service.Name)
		lastSeq = command.Seq
	}
	receipt := ""
	if len(names) > 0 {
		receipt = "Stopping " + joinNames(names) + "."
	}
	targets, err := h.store.SearchSurgeryTargets("", false, store.Pending, store.Claimed, store.Running)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		if receipt == "" {
			return h.postAgent(user.SessionID, "Nothing is running.", 0)
		}
		return h.postAgent(user.SessionID, receipt, lastSeq)
	}
	impact := h.inFlightImpact(targets)
	prompt := strings.TrimSpace(fmt.Sprintf("%s %d %s still running — cancel them too? %s",
		receipt, len(targets), pluralWord(len(targets), "job is", "jobs are"),
		surgeryLoss(store.CommandCancel, impact)))
	allowFree := false
	options := []store.QuestionOption{
		{Label: "yes, cancel them", Value: cancelAllOptionValue},
		{Label: "keep them running", Value: keepWorkOptionValue},
	}
	_, err = h.store.PostMessage(store.Message{
		SessionID: user.SessionID, Role: store.RoleAgent,
		Body: store.QuestionMessageBody(prompt, options, store.QuestionConfig{
			Kind: store.QuestionConfirm, Default: "2", AllowFree: &allowFree,
		}),
		CommandSeq: lastSeq, Options: options,
	})
	return err
}

// inFlightImpact totals what cancelling everything would discard. Runtime is
// the longest single run rather than a sum: it is what the user would feel.
func (h *Head) inFlightImpact(targets []store.SurgeryTarget) store.SurgeryImpact {
	now := time.Now()
	var total store.SurgeryImpact
	for _, target := range targets {
		impact, err := h.store.Impact(target.Node.ID, now)
		if err != nil {
			continue
		}
		total.Nodes += impact.Nodes
		total.OpenNodes += impact.OpenNodes
		total.Running += impact.Running
		total.Cost += impact.Cost
		if impact.RunningFor > total.RunningFor {
			total.RunningFor = impact.RunningFor
		}
	}
	return total
}

func (h *Head) cancelInFlightWork(user store.Message) error {
	targets, err := h.store.SearchSurgeryTargets("", false, store.Pending, store.Claimed, store.Running)
	if err != nil {
		return err
	}
	cancelled := 0
	var lastSeq int64
	for _, target := range targets {
		command, requestErr := h.store.RequestCommand(store.Command{
			SessionID: user.SessionID, Kind: store.CommandCancel,
			Target: target.Node.ID, Instruction: shutdownInstruction,
		})
		if requestErr != nil {
			continue
		}
		cancelled++
		lastSeq = command.Seq
	}
	if cancelled == 0 {
		return h.postAgent(user.SessionID, "That work already finished.", 0)
	}
	return h.postAgent(user.SessionID,
		fmt.Sprintf("Cancelling %d %s.", cancelled, pluralWord(cancelled, "job", "jobs")), lastSeq)
}

// applyServiceOption routes every service-shaped durable option: the two
// shutdown answers, the once-only hygiene nudge, and the ordinary per-service
// acts. Reporting handled=false leaves the option to the other families.
func (h *Head) applyServiceOption(user store.Message, option store.QuestionOption) (bool, error) {
	switch option.Value {
	case cancelAllOptionValue:
		return true, h.cancelInFlightWork(user)
	case keepWorkOptionValue:
		return true, h.postAgent(user.SessionID, "Leaving the running work alone.", 0)
	}
	parts := strings.Split(option.Value, ":")
	if len(parts) != 3 || parts[0] != "service" {
		return false, nil
	}
	service, found, err := h.store.Service(parts[2])
	if err != nil {
		return true, err
	}
	if !found {
		return true, h.postAgent(user.SessionID, "That service is no longer available.", 0)
	}
	if parts[1] == "hygiene-keep" {
		return true, h.postAgent(user.SessionID, "Keeping "+service.Name+" running.", 0)
	}
	kind, ok := serviceCommandKind(parts[1])
	if !ok {
		return false, nil
	}
	return true, h.requestServiceCommand(user, kind, service, parts[1])
}

func serviceManagement(message string) (action, reference string, explicit bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	explicit = strings.Contains(lower, "service") || strings.Contains(lower, "server") ||
		strings.Contains(lower, "running app")
	switch {
	case explicit && (strings.Contains(lower, "what's running") || strings.Contains(lower, "what is running") ||
		strings.Contains(lower, "list services") || strings.Contains(lower, "which services")):
		action = "status"
	case strings.HasPrefix(lower, "stop ") || strings.Contains(lower, "stop the "):
		action = "stop"
	case strings.HasPrefix(lower, "restart ") || strings.Contains(lower, "restart the "):
		action = "restart"
	case strings.Contains(lower, "start it again") || strings.Contains(lower, "start the server again"):
		action = "restart"
	case strings.Contains(lower, "auto-restart") || strings.Contains(lower, "auto restart"):
		action = "auto-restart"
		if strings.Contains(lower, "disable") || strings.Contains(lower, "turn off") {
			action = "disable-auto-restart"
		}
	default:
		return "", "", explicit
	}
	reference = lower
	for _, noise := range []string{"auto-restart", "auto restart", "enable", "disable", "restart", "start", "stop", "again", "please"} {
		reference = strings.ReplaceAll(reference, noise, " ")
	}
	return action, strings.TrimSpace(reference), explicit
}

func serviceCommandKind(action string) (store.CommandKind, bool) {
	switch action {
	case "stop", "hygiene-stop":
		return store.CommandServiceStop, true
	case "restart":
		return store.CommandServiceRestart, true
	case "auto-restart", "disable-auto-restart":
		return store.CommandServiceAutoRestart, true
	default:
		return "", false
	}
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
	}
}

func (h *Head) requestServiceCommand(user store.Message, kind store.CommandKind, service store.Service, instruction string) error {
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind, Target: service.ID, Instruction: instruction,
	})
	if err != nil {
		return err
	}
	reply := "Stopping " + service.Name + "."
	if kind == store.CommandServiceRestart {
		reply = "Restarting " + service.Name + "."
	} else if kind == store.CommandServiceAutoRestart {
		reply = "Enabling auto-restart for " + service.Name + "."
		if strings.Contains(instruction, "disable") {
			reply = "Disabling auto-restart for " + service.Name + "."
		}
	}
	return h.postAgent(user.SessionID, reply, command.Seq)
}

func renderServices(graphStore *store.Store) string {
	if graphStore == nil {
		return ""
	}
	services, err := graphStore.ActiveServices()
	if err != nil || len(services) == 0 {
		return ""
	}
	var lines []string
	for _, service := range services {
		lines = append(lines, fmt.Sprintf("- service %s | %s | %s | command: %s",
			service.Name, service.Status, service.Health.Suffix(), service.Command))
	}
	return strings.Join(lines, "\n")
}
