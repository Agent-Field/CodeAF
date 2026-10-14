package config

import "github.com/Agent-Field/codeaf/internal/roles"

// FreeChatModel is the build's chat bottom rung while a known account is low.
const FreeChatModel = "qwen/qwen3.8-27b:free"

// freeHelperModels are the free defaults for helper rows while a known
// account is low. Reflex needs the live conversation model as well: it must
// not substitute another free model when the chat already uses one.
// The table is hand-picked because an ordinary pick rejects zero tariffs.
//
// The three crew seats are not here: they are routed per task, and a known-low
// account reaches the router as an OpenRouter account out of credit
// ([crewHealthAt]), which moves the seats onto free routes with the router's
// own notice.
var freeHelperModels = map[string]string{
	ModelTierReflex: "nvidia/nemotron-3.5-lightning:free",
	ModelTierLow:    "thinkingmachines/inkling-small:free",
}

// freeTierModelAt is the only low-credit check beneath an unwritten helper row.
func freeTierModelAt(profileDir, tier string) string {
	if useFreeDefaultsAt(profileDir) {
		if model := freeHelperModels[tier]; model != "" {
			return model
		}
	}
	return builtinTierModel(tier)
}

// freeReflexModelAt keeps the low-credit escape beside a paid conversation,
// never beside a chat already on a free route. Unknown prices are not free.
func freeReflexModelAt(profileDir, conversation string) string {
	if conversation == "" || !useFreeDefaultsAt(profileDir) || freeConversationModel(conversation) {
		return ""
	}
	return freeHelperModels[ModelTierReflex]
}

// freeConversationModel recognizes a free route or known zero prices without
// fetching a catalogue. A cached row with unknown prices cannot claim free.
func freeConversationModel(model string) bool {
	if IsFreeModel(model, false, 0, 0, 0) {
		return true
	}
	bare, _ := roles.SplitEffort(model)
	for _, row := range crewCatalogRows() {
		if row.ID == stripCrewRoute(bare) {
			return IsFreeModel(model, !row.PriceUnknown, row.PromptPrice, row.CompletionPrice, row.RequestPrice)
		}
	}
	return false
}
