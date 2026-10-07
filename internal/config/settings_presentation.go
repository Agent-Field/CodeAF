package config

// SettingPresentation describes the live chat settings surface. It deliberately
// does not change the shared registry or persisted keys: resident users and old
// profiles keep the same readers, validators and writers.
type SettingPresentation struct {
	Category    string
	Label       string
	Description string
	Aliases     []string
	Advanced    bool
	Hidden      bool
	Scope       string
	Activation  string
}

// ChatPresentation is a pure view of a setting, never a configuration migration.
// Empty Label or Description lets a surface retain its existing wording. Timing
// is stated only where the consumer establishes it; no blanket "applies now".
func (s Setting) ChatPresentation() SettingPresentation {
	p := SettingPresentation{Scope: "This profile", Aliases: []string{s.Label, s.Category, s.Key}}
	switch s.Category {
	case CategoryModels:
		p.Category = "Models"
	case CategorySpending:
		p.Category = "Spending"
	case CategorySafety:
		p.Category = "Permissions"
	case CategoryTasks:
		p.Category = "Tasks"
	case CategoryTeams:
		p.Category = "AI teams"
		p.Scope = "Default for AI teams; teams can override it"
	case CategoryPractice, CategoryInterface:
		p.Category = "General"
	}
	if ProjectKeyAllowed(s.Key) {
		p.Scope = "Profile default; a project can override it"
	}
	if s.Kind == SettingModel && s.Slot == "talk" {
		p.Scope = "This chat and the profile default"
	}
	switch s.Key {
	case KeyPracticeIdle, KeyPracticeBudget, KeyBriefAfter, KeyTenureAfter, KeySplitPct:
		p.Hidden = true // Resident-only controls have no live chat consumer.
	case KeyMemoryEnabled:
		p.Category, p.Label = "Memory", "remember across chats"
		p.Description = "Learns useful preferences and corrections from conversations and recalls relevant memories. /memories lets you inspect what is saved. Turning this off does not delete saved memories."
		p.Activation = "Restart the CLI to apply this to already-open chats"
	case KeyStandingBackground:
		p.Label = "background reminders"
		p.Description = "Check reminders, watches and routines while the CLI is closed. Uses a timer under your login; the machine must be awake and you must be logged in."
	case KeyUpdateAuto:
		p.Label = "automatic updates"
	case KeyMouse:
		p.Label = "mouse interaction"
	case KeyWork:
		p.Label = "completed tool details"
		p.Description = "Collapsed keeps completed tool steps behind one summary. Expanded keeps those details visible."
	case KeyIcons:
		p.Label = "tool icons"
	case KeyTaskColumn:
		p.Label = "task sidebar"
	case KeyQuickSwitch:
		p.Label = "switch chats immediately"
	case KeyHints:
		p.Label = "show hints"
		p.Description = "Show contextual tips while you work."
	case KeyAttributionModel:
		p.Label = "include model in commit attribution"
	case KeyHistoryEnabled:
		p.Category, p.Label = "Privacy", "save input history"
	case KeyDraftPersist:
		p.Category, p.Label = "Privacy", "save unfinished drafts"
	case KeyTelemetry:
		p.Category, p.Label = "Privacy", "share anonymous usage reports"
	case KeyModelPool:
		p.Category, p.Label = "Privacy", "shared model recommendations"
		p.Description = "Use and contribute receives model recommendations and shares text-free performance measurements. Use only sends nothing. Off does neither. Code, prompts and file paths are never included."
	case KeyModelPoolPublicKey:
		p.Category, p.Label, p.Advanced = "Privacy", "model pool signing key", true
	case KeyToolApprovalMode:
		p.Label = "tool approvals"
		p.Description = "Allow by default runs tools without asking. Ask each time requests approval. Block by default refuses tools. Tool exceptions override this choice; dangerous shell commands still require approval."
	case KeyToolApprovals:
		p.Label = "tool exceptions"
	case KeyGuardian:
		p.Label = "AI approval screening"
	case KeyConsentTimeout:
		p.Label, p.Advanced = "pause approval timer after", true
	case KeyBashBackgroundAfter:
		p.Category, p.Label = "Tasks", "background shell commands after"
	case KeyTaskAutoApprove:
		p.Category, p.Label = "Permissions", "start proposed tasks after"
	case KeyTaskSettle:
		p.Category, p.Label = "Tasks", "when task results need a decision"
		p.Description = "Ask me leaves the decision with you. Let the chat decide reviews the work and asks you only when it cannot decide."
	case KeyTaskStart:
		p.Label = "before starting a task"
		p.Description = "Assess the brief checks its scope before starting a worker. Start directly skips that check. Either choice starts one worker; /task solo explicitly requests solo work."
	case KeyTaskAudit:
		p.Label = "review task results"
	case KeyTaskRepairRounds:
		p.Label = "repair attempts"
	case KeyTaskParallel:
		p.Label = "concurrent tasks"
	case KeyTaskMaxLoad:
		p.Label, p.Advanced = "maximum load per CPU core", true
	case KeyTaskMinFreeMB:
		p.Label, p.Advanced = "minimum available RAM", true
	case KeyTeamsQuestionsUp:
		p.Label = "ask the AI manager first"
	case KeyTeamsWake:
		p.Label = "run when team messages arrive"
	case KeyTeamsCapUSDDay:
		p.Label = "daily spending limit"
	case KeyTeamsDepthLimit:
		p.Label, p.Advanced = "maximum team nesting", true
	case KeyTeamsSubSharePct:
		p.Label, p.Advanced = "new subteam budget share", true
		p.Activation = "Applies to newly created subteams only"
	case KeyDailyBudget:
		p.Label = "daily spending limit"
	case KeySpendRail:
		p.Label = "per-chat spending limit"
	case KeyPlanConsent:
		p.Label = "ask before a plan costs more than"
	case KeyVisionModel:
		p.Label = "image understanding model"
	case KeyDocumentEngine:
		p.Label = "document reader"
	case KeyEffort:
		p.Label = "reasoning effort"
	case KeySearchProvider:
		p.Category, p.Label = "Connections", "web search provider"
	case KeyExaKey, KeyFirecrawlKey, KeyJinaKey:
		p.Category = "Connections"
	case KeyAPIKey:
		p.Category = "Connections"
	case KeyGoogleOAuthClient, KeyGoogleOAuthSecret, KeySlackOAuthClient:
		p.Category, p.Advanced = "Connections", true
	case KeySSHControlPersist:
		p.Category, p.Label, p.Advanced = "Connections", "SSH connection reuse", true
	case KeySSHServerAlive:
		p.Category, p.Label, p.Advanced = "Connections", "SSH keepalive interval", true
	case KeySSHServerMisses:
		p.Category, p.Label, p.Advanced = "Connections", "SSH missed keepalives", true
	case KeySSHIPQoS:
		p.Category, p.Label, p.Advanced = "Connections", "SSH traffic priority", true
	case KeyContextFill:
		p.Label, p.Advanced = "compact context at", true
	case KeyCompletionReserve:
		p.Label, p.Advanced = "tokens reserved for replies", true
	case KeyWorkingSet:
		p.Label, p.Advanced = "working context limit", true
	case KeyContextReuse:
		p.Label, p.Advanced = "context reuse limit", true
	case KeyRouting, KeyPromptProfile, KeyLaneGuard, KeyModelRoles, KeyTierReflexModel, KeyTierLowModel, KeyTierWorkerModel, KeyTierHighModel, KeyTierMastermindModel, KeyReplyGuard:
		p.Advanced = true
	}
	// Preserve the former navigation names as search aliases for people who
	// learned them before the categories were reorganized.
	switch s.Category {
	case CategoryModels:
		p.Aliases = append(p.Aliases, "Providers", "Context")
	case CategorySafety:
		p.Aliases = append(p.Aliases, "Safety")
	case CategoryInterface:
		p.Aliases = append(p.Aliases, "Display", "Workspace")
	case CategoryPractice:
		p.Aliases = append(p.Aliases, "Session", "Workspace")
	}
	if s.Key == LaneSettingKey(LaneSlotTalk) {
		p.Label = "model provider"
		p.Advanced = true
	}
	if s.Kind == SettingModel {
		switch s.Slot {
		case "talk":
			p.Label = "chat model"
		case "image":
			p.Label = "image generation"
		case "speech":
			p.Label = "speech generation"
		case "music":
			p.Label = "music generation"
		case "video":
			p.Label = "video generation"
		case "voice":
			p.Label = "audio transcription"
		}

		if slot, ok := ModelSlotFor(s.Slot); ok && slot.Role != "" && s.Slot != "talk" {
			p.Hidden = true // Legacy role picker cannot apply to a v3 engine.
		}
	}
	switch s.Key {
	case KeyEffort, KeyModelFallbacks, KeyTaskAutoApprove, KeyBashBackgroundAfter,
		KeyTaskModel, KeyTaskAudit, KeyAttributionModel, KeyReplyGuard,
		KeyPromptProfile, KeyTaskSettle, KeyTaskRepairRounds, KeyTaskParallel,
		KeyHistoryEnabled, KeyDraftPersist, KeyDocumentEngine, KeyTaskColumn,
		KeyGoogleOAuthClient, KeyGoogleOAuthSecret, KeySlackOAuthClient,
		KeySSHControlPersist, KeySSHServerAlive, KeySSHServerMisses, KeySSHIPQoS:
		p.Activation = "Restart the CLI to apply this to already-open chats"
	case KeyTelemetry:
		p.Activation = "Off stops sending now; turning on applies next CLI launch"
	case KeySearchProvider, KeyExaKey, KeyFirecrawlKey, KeyJinaKey:
		p.Activation = "Applies on the next web search"
	}
	switch s.Key {
	case KeyMemoryEnabled:
		p.Aliases = append(p.Aliases, "remember", "learn", "forget", "recall")
	case KeyTelemetry:
		p.Aliases = append(p.Aliases, "analytics", "tracking", "privacy", "data sharing")
	case KeyTaskMinFreeMB:
		p.Aliases = append(p.Aliases, "RAM", "memory", "resources")
	case KeyTaskMaxLoad:
		p.Aliases = append(p.Aliases, "CPU", "resources")
	case KeyUpdateAuto:
		p.Aliases = append(p.Aliases, "updates", "upgrade", "version")
	case KeyAPIKey, KeyExaKey, KeyFirecrawlKey, KeyJinaKey:
		p.Aliases = append(p.Aliases, "API key", "credentials")
	case KeyDailyBudget, KeySpendRail, KeyTeamsCapUSDDay, KeyPlanConsent:
		p.Aliases = append(p.Aliases, "budget", "cost", "limit")
	case KeyHistoryEnabled, KeyDraftPersist:
		p.Aliases = append(p.Aliases, "privacy", "saved messages")
	}
	return p
}
